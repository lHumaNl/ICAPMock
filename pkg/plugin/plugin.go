// Copyright 2026 ICAP Mock

// Package plugin provides a plugin loading and management system.
package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// ProcessorPlugin defines the interface for ICAP processor plugins.
// Plugins must implement this interface to be registered with the plugin system.
//
// All methods must be safe for concurrent use as they may be called from
// multiple goroutines simultaneously.
type ProcessorPlugin interface {
	// Process handles an ICAP request and returns a response.
	// The context can be used for cancellation and timeout handling.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadline propagation
	//   - req: The ICAP request to process
	//
	// Returns:
	//   - resp: The ICAP response (may be nil to pass to next processor)
	//   - err: An error if processing failed
	Process(ctx context.Context, req *icap.Request) (*icap.Response, error)

	// Name returns the plugin's unique identifier.
	// This name is used for registration and lookup.
	Name() string

	// Init initializes the plugin with optional configuration.
	// This is called once when the plugin is registered or loaded.
	// The config map can contain arbitrary configuration values.
	Init(config map[string]interface{}) error

	// Close releases any resources held by the plugin.
	// This is called during server shutdown.
	Close() error
}

// ProcessorPluginFunc is an adapter type that allows using ordinary functions
// as ProcessorPlugins. This is useful for simple plugins or testing.
//
// Example:
//
//	plugin := ProcessorPluginFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	    return icap.NewResponse(204), nil
//	})
type ProcessorPluginFunc func(ctx context.Context, req *icap.Request) (*icap.Response, error)

// Process implements ProcessorPlugin.Process.
func (f ProcessorPluginFunc) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return f(ctx, req)
}

// Name implements ProcessorPlugin.Name.
func (f ProcessorPluginFunc) Name() string {
	return "ProcessorPluginFunc"
}

// Init implements ProcessorPlugin.Init.
func (f ProcessorPluginFunc) Init(_ map[string]interface{}) error {
	return nil
}

// Close implements ProcessorPlugin.Close.
func (f ProcessorPluginFunc) Close() error {
	return nil
}

// PluginInfo contains metadata about a plugin.
type PluginInfo struct {
	// Name is the unique identifier for the plugin.
	Name string
	// Description is a human-readable description.
	Description string
	// Version is the plugin version string.
	Version string
	// Author is the plugin author.
	Author string
	// Path is the path to the plugin file (empty for built-in plugins).
	Path string
}

// PluginWithError wraps a plugin with its associated error (if any).
type PluginWithError struct {
	Plugin ProcessorPlugin
	Error  error
}

// globalRegistry is the default global plugin registry.
var globalRegistry = NewRegistry()

// Register registers a plugin with the global registry.
// If a plugin with the same name already exists, it returns an error.
//
// This function is safe for concurrent use.
//
// Example:
//
//	func init() {
//	    if err := plugin.Register("my-plugin", &MyPlugin{}); err != nil {
//	        panic(err)
//	    }
//	}
func Register(name string, p ProcessorPlugin) error {
	return globalRegistry.Register(name, p)
}

// MustRegister registers a plugin with the global registry.
// It panics if registration fails.
//
// This is useful for plugins that must be registered during initialization.
func MustRegister(name string, p ProcessorPlugin) {
	if err := Register(name, p); err != nil {
		panic(fmt.Sprintf("failed to register plugin %q: %v", name, err))
	}
}

// Get retrieves a plugin by name from the global registry.
// Returns the plugin and true if found, nil and false otherwise.
//
// This function is safe for concurrent use.
func Get(name string) (ProcessorPlugin, bool) {
	return globalRegistry.Get(name)
}

// List returns all registered plugins from the global registry.
//
// This function is safe for concurrent use.
func List() []PluginInfo {
	return globalRegistry.List()
}

// Unregister removes a plugin from the global registry.
// Returns the removed plugin and true if found, nil and false otherwise.
//
// This function is safe for concurrent use.
func Unregister(name string) (ProcessorPlugin, bool) {
	return globalRegistry.Unregister(name)
}

// Clear removes all plugins from the global registry.
//
// This function is safe for concurrent use.
func Clear() {
	globalRegistry.Clear()
}

// InitPlugin initializes a plugin with the given configuration.
// This is a convenience function that calls the plugin's Init method.
func InitPlugin(name string, config map[string]interface{}) error {
	p, exists := Get(name)
	if !exists {
		return fmt.Errorf("plugin %q not found", name)
	}
	return p.Init(config)
}

// CloseAll closes all registered plugins in the global registry.
// Errors are collected and returned as a single error.
func CloseAll() error {
	return globalRegistry.CloseAll()
}

// CreatePluginProcessor creates a processor that chains all registered plugins.
// The plugins are called in registration order until one returns a non-nil response.
// This allows plugins to be used seamlessly with the existing processor chain.
func CreatePluginProcessor() interface {
	Process(ctx context.Context, req *icap.Request) (*icap.Response, error)
	Name() string
} {
	return &pluginProcessor{registry: globalRegistry}
}

// pluginProcessor wraps the plugin registry to implement the Processor interface.
type pluginProcessor struct {
	registry *Registry
}

// Process implements the Processor interface.
func (p *pluginProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	plugins := p.registry.GetAll()

	for _, plugin := range plugins {
		resp, err := plugin.Process(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil
		}
	}

	// No plugin handled the request
	return nil, nil
}

// Name implements the Processor interface.
func (p *pluginProcessor) Name() string {
	return "PluginProcessor"
}

// ChainPlugins creates a processor that chains the specified plugins.
// Plugins are called in order until one returns a non-nil response.
func ChainPlugins(names ...string) interface {
	Process(ctx context.Context, req *icap.Request) (*icap.Response, error)
	Name() string
} {
	var plugins []ProcessorPlugin
	for _, name := range names {
		if p, exists := Get(name); exists {
			plugins = append(plugins, p)
		}
	}
	return &chainedPluginProcessor{plugins: plugins}
}

// chainedPluginProcessor chains a specific set of plugins.
type chainedPluginProcessor struct {
	plugins []ProcessorPlugin
}

// Process implements the Processor interface.
func (p *chainedPluginProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	for _, plugin := range p.plugins {
		resp, err := plugin.Process(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil
		}
	}
	return nil, nil
}

// Name implements the Processor interface.
func (p *chainedPluginProcessor) Name() string {
	return "ChainedPluginProcessor"
}

// Ensure Registry implements PluginRegistry interface.
var _ PluginRegistry = (*Registry)(nil)

// PluginRegistry defines the interface for plugin registries.
type PluginRegistry interface {
	// Register adds a plugin to the registry.
	Register(name string, p ProcessorPlugin) error
	// Get retrieves a plugin by name.
	Get(name string) (ProcessorPlugin, bool)
	// List returns information about all registered plugins.
	List() []PluginInfo
	// Unregister removes a plugin from the registry.
	Unregister(name string) (ProcessorPlugin, bool)
	// Clear removes all plugins from the registry.
	Clear()
	// GetAll returns all registered plugins.
	GetAll() []ProcessorPlugin
	// CloseAll closes all plugins and returns any errors.
	CloseAll() error
}

// Registry provides a thread-safe plugin registry implementation.
type Registry struct {
	plugins map[string]ProcessorPlugin
	info    map[string]PluginInfo
	order   []string
	mu      sync.RWMutex
}

// NewRegistry creates a new empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]ProcessorPlugin),
		info:    make(map[string]PluginInfo),
		order:   make([]string, 0),
	}
}

// Register adds a plugin to the registry.
// Returns an error if a plugin with the same name already exists.
func (r *Registry) Register(name string, p ProcessorPlugin) error {
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if p == nil {
		return fmt.Errorf("plugin cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}

	r.plugins[name] = p
	r.info[name] = PluginInfo{
		Name:        name,
		Description: fmt.Sprintf("Plugin: %s", name),
		Version:     "1.0.0",
	}
	r.order = append(r.order, name)

	return nil
}

// Get retrieves a plugin by name.
func (r *Registry) Get(name string) (ProcessorPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.plugins[name]
	return p, exists
}

// List returns information about all registered plugins.
func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PluginInfo, 0, len(r.order))
	for _, name := range r.order {
		if info, exists := r.info[name]; exists {
			result = append(result, info)
		}
	}
	return result
}

// Unregister removes a plugin from the registry.
func (r *Registry) Unregister(name string) (ProcessorPlugin, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.plugins[name]
	if !exists {
		return nil, false
	}

	delete(r.plugins, name)
	delete(r.info, name)

	// Remove from order slice
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	return p, true
}

// Clear removes all plugins from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.plugins = make(map[string]ProcessorPlugin)
	r.info = make(map[string]PluginInfo)
	r.order = make([]string, 0)
}

// GetAll returns all registered plugins in registration order.
func (r *Registry) GetAll() []ProcessorPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugins := make([]ProcessorPlugin, 0, len(r.order))
	for _, name := range r.order {
		if p, exists := r.plugins[name]; exists {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// CloseAll closes all plugins and collects any errors.
func (r *Registry) CloseAll() error {
	r.mu.RLock()
	plugins := make([]ProcessorPlugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		plugins = append(plugins, p)
	}
	r.mu.RUnlock()

	var errs []error
	for _, p := range plugins {
		if err := p.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing plugin %s: %w", p.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing plugins: %v", errs)
	}
	return nil
}

// SetPluginInfo sets custom metadata for a registered plugin.
func (r *Registry) SetPluginInfo(name string, info PluginInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; !exists {
		return fmt.Errorf("plugin %q not found", name)
	}

	info.Name = name // Ensure name matches
	r.info[name] = info
	return nil
}

// Count returns the number of registered plugins.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}
