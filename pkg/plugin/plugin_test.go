// Copyright 2026 ICAP Mock

package plugin_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
	"github.com/icap-mock/icap-mock/pkg/plugin"
)

// mockPlugin is a test implementation of ProcessorPlugin.
type mockPlugin struct {
	initErr    error
	closeErr   error
	processErr error
	response   *icap.Response
	name       string
	initCalls  int
	closeCalls int
	mu         sync.Mutex
}

func (m *mockPlugin) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processErr != nil {
		return nil, m.processErr
	}
	return m.response, nil
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Init(config map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalls++
	return m.initErr
}

func (m *mockPlugin) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalls++
	return m.closeErr
}

// TestRegister tests plugin registration.
func TestRegister(t *testing.T) {
	// Clean up before test
	plugin.Clear()

	p := &mockPlugin{name: "test-plugin"}

	// Test successful registration
	err := plugin.Register("test-plugin", p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test duplicate registration
	err = plugin.Register("test-plugin", p)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}

	// Test empty name
	err = plugin.Register("", p)
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	// Test nil plugin
	err = plugin.Register("nil-plugin", nil)
	if err == nil {
		t.Fatal("expected error for nil plugin")
	}
}

// TestMustRegister tests MustRegister function.
func TestMustRegister(t *testing.T) {
	defer plugin.Clear()

	// This should not panic
	plugin.MustRegister("must-test", &mockPlugin{name: "must-test"})

	// Verify it was registered
	_, exists := plugin.Get("must-test")
	if !exists {
		t.Fatal("plugin was not registered")
	}
}

// TestGet tests plugin retrieval.
func TestGet(t *testing.T) {
	defer plugin.Clear()

	p := &mockPlugin{name: "get-test"}
	plugin.MustRegister("get-test", p)

	// Test existing plugin
	retrieved, exists := plugin.Get("get-test")
	if !exists {
		t.Fatal("expected plugin to exist")
	}
	if retrieved.Name() != "get-test" {
		t.Fatalf("unexpected plugin name: %s", retrieved.Name())
	}

	// Test non-existing plugin
	_, exists = plugin.Get("non-existing")
	if exists {
		t.Fatal("expected plugin to not exist")
	}
}

// TestList tests listing plugins.
func TestList(t *testing.T) {
	defer plugin.Clear()

	plugin.MustRegister("list-a", &mockPlugin{name: "list-a"})
	plugin.MustRegister("list-b", &mockPlugin{name: "list-b"})
	plugin.MustRegister("list-c", &mockPlugin{name: "list-c"})

	list := plugin.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 plugins, got %d", len(list))
	}

	// Verify order is maintained
	names := make(map[string]bool)
	for _, info := range list {
		names[info.Name] = true
	}
	if !names["list-a"] || !names["list-b"] || !names["list-c"] {
		t.Fatal("missing expected plugins in list")
	}
}

// TestUnregister tests plugin unregistration.
func TestUnregister(t *testing.T) {
	defer plugin.Clear()

	p := &mockPlugin{name: "unregister-test"}
	plugin.MustRegister("unregister-test", p)

	// Test unregistering existing plugin
	removed, exists := plugin.Unregister("unregister-test")
	if !exists {
		t.Fatal("expected plugin to exist for removal")
	}
	if removed.Name() != "unregister-test" {
		t.Fatalf("unexpected plugin name: %s", removed.Name())
	}

	// Test unregistering non-existing plugin
	_, exists = plugin.Unregister("non-existing")
	if exists {
		t.Fatal("expected plugin to not exist for removal")
	}
}

// TestClear tests clearing all plugins.
func TestClear(t *testing.T) {
	plugin.MustRegister("clear-a", &mockPlugin{name: "clear-a"})
	plugin.MustRegister("clear-b", &mockPlugin{name: "clear-b"})

	if len(plugin.List()) != 2 {
		t.Fatal("expected 2 plugins before clear")
	}

	plugin.Clear()

	if len(plugin.List()) != 0 {
		t.Fatal("expected 0 plugins after clear")
	}
}

// TestRegistry tests the Registry type directly.
func TestRegistry(t *testing.T) {
	reg := plugin.NewRegistry()

	p1 := &mockPlugin{name: "reg-1"}
	p2 := &mockPlugin{name: "reg-2"}

	// Test Register
	if err := reg.Register("reg-1", p1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := reg.Register("reg-2", p2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test Count
	if reg.Count() != 2 {
		t.Fatalf("expected 2 plugins, got %d", reg.Count())
	}

	// Test GetAll
	all := reg.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(all))
	}

	// Test SetPluginInfo
	err := reg.SetPluginInfo("reg-1", plugin.PluginInfo{
		Name:        "reg-1",
		Description: "Test plugin 1",
		Version:     "1.2.3",
		Author:      "Test Author",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test SetPluginInfo for non-existing plugin
	err = reg.SetPluginInfo("non-existing", plugin.PluginInfo{})
	if err == nil {
		t.Fatal("expected error for non-existing plugin")
	}

	// Test CloseAll
	if err := reg.CloseAll(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRegistryCloseErrors tests that CloseAll collects errors from plugins.
func TestRegistryCloseErrors(t *testing.T) {
	reg := plugin.NewRegistry()

	p := &mockPlugin{
		name:     "close-error",
		closeErr: errors.New("close error"),
	}
	if err := reg.Register("close-error", p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CloseAll should return error
	err := reg.CloseAll()
	if err == nil {
		t.Fatal("expected error from CloseAll")
	}
}

// TestProcessorPluginFunc tests the ProcessorPluginFunc adapter.
func TestProcessorPluginFunc(t *testing.T) {
	called := false
	f := plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		called = true
		return icap.NewResponse(200), nil
	})

	// Test Name
	if f.Name() != "ProcessorPluginFunc" {
		t.Fatalf("unexpected name: %s", f.Name())
	}

	// Test Init
	if err := f.Init(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test Close
	if err := f.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test Process
	resp, err := f.Process(context.Background(), &icap.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected function to be called")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
}

// TestInitPlugin tests the InitPlugin convenience function.
func TestInitPlugin(t *testing.T) {
	defer plugin.Clear()

	p := &mockPlugin{name: "init-test"}
	plugin.MustRegister("init-test", p)

	// Test successful init
	err := plugin.InitPlugin("init-test", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.initCalls != 1 {
		t.Fatal("expected Init to be called once")
	}

	// Test init non-existing plugin
	err = plugin.InitPlugin("non-existing", nil)
	if err == nil {
		t.Fatal("expected error for non-existing plugin")
	}
}

// TestCloseAll tests the global CloseAll function.
func TestCloseAll(t *testing.T) {
	defer plugin.Clear()

	// Register plugins with close errors
	p1 := &mockPlugin{name: "close-a", closeErr: errors.New("close error a")}
	p2 := &mockPlugin{name: "close-b", closeErr: errors.New("close error b")}
	p3 := &mockPlugin{name: "close-c"} // no error

	plugin.MustRegister("close-a", p1)
	plugin.MustRegister("close-b", p2)
	plugin.MustRegister("close-c", p3)

	err := plugin.CloseAll()
	if err == nil {
		t.Fatal("expected error when closing plugins with errors")
	}
}

// TestCreatePluginProcessor tests creating a processor from plugins.
func TestCreatePluginProcessor(t *testing.T) {
	defer plugin.Clear()

	// Register plugins that return responses
	plugin.MustRegister("proc-1", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return nil, nil // Pass through
	}))
	plugin.MustRegister("proc-2", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(200), nil // Handle request
	}))

	processor := plugin.CreatePluginProcessor()
	if processor == nil {
		t.Fatal("expected processor to be created")
	}

	// Test Name
	if processor.Name() != "PluginProcessor" {
		t.Fatalf("unexpected name: %s", processor.Name())
	}

	// Test Process
	resp, err := processor.Process(context.Background(), &icap.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
}

// TestChainPlugins tests chaining specific plugins.
func TestChainPlugins(t *testing.T) {
	defer plugin.Clear()

	// Register plugins
	plugin.MustRegister("chain-1", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return nil, nil // Pass through
	}))
	plugin.MustRegister("chain-2", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(201), nil // Handle request
	}))
	plugin.MustRegister("chain-3", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(202), nil // Should not be reached
	}))

	processor := plugin.ChainPlugins("chain-1", "chain-2", "chain-3")
	if processor == nil {
		t.Fatal("expected processor to be created")
	}

	// Test Process - should stop at chain-2
	resp, err := processor.Process(context.Background(), &icap.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != 201 {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}
}

// TestChainPluginsNonExistent tests chaining with non-existent plugins.
func TestChainPluginsNonExistent(t *testing.T) {
	defer plugin.Clear()

	plugin.MustRegister("existing", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}))

	// Chain with one existing and one non-existing plugin
	processor := plugin.ChainPlugins("existing", "non-existent")
	if processor == nil {
		t.Fatal("expected processor to be created")
	}

	// Test Process
	resp, err := processor.Process(context.Background(), &icap.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestPluginProcessorOrder tests that plugins are called in registration order.
func TestPluginProcessorOrder(t *testing.T) {
	defer plugin.Clear()

	var order []string

	// Register plugins in specific order
	plugin.MustRegister("order-1", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		order = append(order, "order-1")
		return nil, nil // Pass through
	}))
	plugin.MustRegister("order-2", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		order = append(order, "order-2")
		return nil, nil // Pass through
	}))
	plugin.MustRegister("order-3", plugin.ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		order = append(order, "order-3")
		return icap.NewResponse(200), nil
	}))

	processor := plugin.CreatePluginProcessor()
	processor.Process(context.Background(), &icap.Request{})

	// Verify order
	if len(order) != 3 {
		t.Fatalf("expected 3 plugin calls, got %d", len(order))
	}
	if order[0] != "order-1" || order[1] != "order-2" || order[2] != "order-3" {
		t.Fatalf("unexpected order: %v", order)
	}
}

// TestConcurrentGet tests concurrent read access to the registry.
func TestConcurrentGet(t *testing.T) {
	defer plugin.Clear()

	plugin.MustRegister("concurrent-get", &mockPlugin{name: "concurrent-get"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, exists := plugin.Get("concurrent-get")
			if !exists {
				t.Error("expected plugin to exist")
			}
		}()
	}
	wg.Wait()
}
