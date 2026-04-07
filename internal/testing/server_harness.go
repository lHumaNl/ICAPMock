// Copyright 2026 ICAP Mock

package testing

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	prometheus "github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/internal/server"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// getFreePort returns a free TCP port that can be used for testing.
// This is a non-testing version that doesn't require *testing.T.
func getFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("Failed to get free port: %v", err))
	}
	defer l.Close() //nolint:errcheck

	addr := l.Addr().(*net.TCPAddr) //nolint:errcheck
	return addr.Port
}

// ServerHarness provides a test harness for running an ICAP server in tests.
// It handles server lifecycle (start/stop), configuration, and provides
// utilities for sending requests to the server.
//
// Usage:
//
//	harness := NewServerHarness(t, testConfig)
//	harness.Start()
//	defer harness.Stop(ctx)
//
//	resp, err := harness.SendRequest(req)
type ServerHarness struct {
	t       testing.TB
	server  server.Server
	config  *config.ServerConfig
	addr    string
	mu      sync.Mutex
	started bool
	stopped bool
}

// NewServerHarness creates a new ServerHarness with the given configuration.
//
// Parameters:
//   - t: Testing instance
//   - cfg: Server configuration (uses defaults if nil)
//
// Returns:
//   - A new ServerHarness instance
//
// Example:
//
//	cfg := &config.ServerConfig{
//	    Host:        "127.0.0.1",
//	    Port:        0, // Use random port
//	    MaxConnections: 100,
//	}
//	harness := NewServerHarness(t, cfg)
func NewServerHarness(t testing.TB, cfg *config.ServerConfig) *ServerHarness {
	t.Helper()

	if cfg == nil {
		cfg = &config.ServerConfig{}
	}

	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}

	// Leave Port=0 so the server picks a free port via the OS.
	// This avoids race conditions from getFreePort().

	h := &ServerHarness{
		t:      t,
		config: cfg,
	}

	h.t.Cleanup(func() {
		if h.started && !h.stopped {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.Stop(ctx); err != nil {
				t.Errorf("Failed to stop server harness: %v", err)
			}
		}
	})

	return h
}

// Start starts the ICAP server in the harness.
//
// Returns:
//   - error if the server fails to start
//
// Example:
//
//	if err := harness.Start(); err != nil {
//	    t.Fatalf("Failed to start harness: %v", err)
//	}
func (h *ServerHarness) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return fmt.Errorf("server already started")
	}

	pool := server.NewConnectionPool()
	var err error

	icapServer, err := server.NewServer(h.config, pool, nil)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Set up a default router with an OPTIONS handler so the server
	// can handle basic requests instead of closing connections immediately.
	r := router.NewRouter()
	optionsHandler := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		Methods: []string{"REQMOD", "RESPMOD"},
	})
	if regErr := r.Handle("/options", optionsHandler); regErr != nil {
		return fmt.Errorf("failed to register default handler: %w", regErr)
	}
	icapServer.SetRouter(r)
	h.server = icapServer

	ctx := context.Background()
	if err := h.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	h.started = true
	h.addr = h.server.Addr().String()

	return nil
}

// Stop stops the ICAP server in the harness.
//
// Parameters:
//   - ctx: Context for timeout handling
//
// Returns:
//   - error if the server fails to stop
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := harness.Stop(ctx); err != nil {
//	    t.Fatalf("Failed to stop harness: %v", err)
//	}
func (h *ServerHarness) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.started {
		return fmt.Errorf("server not started")
	}

	if h.stopped {
		return nil
	}

	if err := h.server.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	h.stopped = true
	h.started = false
	return nil
}

// Addr returns the server's listening address.
//
// Returns:
//   - Server address in "host:port" format
//
// Example:
//
//	addr := harness.Addr()
//	conn, err := net.Dial("tcp", addr)
func (h *ServerHarness) Addr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.addr
}

// SendRequest sends an ICAP request to the server and returns the response.
// This is a convenience method for simple request/response testing.
//
// Parameters:
//   - req: ICAP request to send
//
// Returns:
//   - resp: ICAP response from server
//   - error: if the request fails
//
// Example:
//
//	req := BuildICAPRequest("OPTIONS", "icap://localhost/options", nil, nil)
//	resp, err := harness.SendRequest(req)
//	require.NoError(t, err)
//	assert.Equal(t, 200, resp.StatusCode)
func (h *ServerHarness) SendRequest(req *icap.Request) (*icap.Response, error) {
	h.mu.Lock()
	addr := h.addr
	h.mu.Unlock()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	// Set a read deadline to avoid hanging
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := req.WriteTo(conn); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Parse only the status line from the response instead of using
	// io.ReadAll (which hangs on persistent connections until server closes).
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	statusLine = strings.TrimRight(statusLine, "\r\n")
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid status line: %s", statusLine)
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}

	resp := icap.NewResponse(statusCode)
	resp.Proto = parts[0]

	// Read remaining headers (not needed for basic tests, just drain)
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}

	return resp, nil
}

// SendRawRequest sends a raw ICAP request string and parses the response.
// This avoids potential issues with icap.Request.WriteTo serialization.
func (h *ServerHarness) SendRawRequest(rawRequest string) (*icap.Response, error) {
	h.mu.Lock()
	addr := h.addr
	h.mu.Unlock()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte(rawRequest)); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	statusLine = strings.TrimRight(statusLine, "\r\n")
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid status line: %s", statusLine)
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}

	resp := icap.NewResponse(statusCode)
	resp.Proto = parts[0]

	// Drain remaining headers
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}

	return resp, nil
}

// Server returns the underlying server instance.
// This can be used for advanced testing scenarios.
//
// Returns:
//   - The server.Server instance
func (h *ServerHarness) Server() server.Server {
	return h.server
}

// Config returns the server configuration.
//
// Returns:
//   - The ServerConfig instance
func (h *ServerHarness) Config() *config.ServerConfig {
	return h.config
}

// IsStarted returns whether the server has been started.
//
// Returns:
//   - true if the server is started
func (h *ServerHarness) IsStarted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.started
}

// IsStopped returns whether the server has been stopped.
//
// Returns:
//   - true if the server is stopped
func (h *ServerHarness) IsStopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// Restart stops and starts the server.
// This is useful for testing configuration changes.
//
// Parameters:
//   - ctx: Context for timeout handling
//
// Returns:
//   - error if restart fails
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := harness.Restart(ctx); err != nil {
//	    t.Fatalf("Failed to restart harness: %v", err)
//	}
func (h *ServerHarness) Restart(ctx context.Context) error {
	if err := h.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop during restart: %w", err)
	}

	// Reset stopped flag so Start can proceed.
	// Start already resets started via the started=false set in Stop.
	h.mu.Lock()
	h.stopped = false
	h.mu.Unlock()

	if err := h.Start(); err != nil {
		return fmt.Errorf("failed to start during restart: %w", err)
	}

	return nil
}

// MemoryServerHarness provides an in-memory server harness for faster unit tests.
// It doesn't create actual network connections but simulates request/response handling.
//
// This is useful for tests that don't need actual network I/O.
//
// Usage:
//
//	harness := NewMemoryServerHarness(t)
//	resp, err := harness.Handle(ctx, req)
type MemoryServerHarness struct {
	t       testing.TB
	handler handler.HandlerFunc
}

// NewMemoryServerHarness creates a new memory server harness.
//
// Parameters:
//   - t: Testing instance
//
// Returns:
//   - A new MemoryServerHarness instance
//
// Example:
//
//	harness := NewMemoryServerHarness(t)
//	harness.SetHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	    return icap.NewResponse(200), nil
//	})
func NewMemoryServerHarness(t testing.TB) *MemoryServerHarness {
	t.Helper()

	return &MemoryServerHarness{
		t: t,
	}
}

// SetHandler sets the request handler for the memory server.
//
// Parameters:
//   - handler: Handler function to process requests
//
// Example:
//
//	harness.SetHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	    return icap.NewResponse(200), nil
//	})
func (h *MemoryServerHarness) SetHandler(handler handler.HandlerFunc) {
	h.handler = handler
}

// Handle processes a request through the handler.
//
// Parameters:
//   - ctx: Context for the request
//   - req: ICAP request to handle
//
// Returns:
//   - resp: ICAP response
//   - error: if handling fails
//
// Example:
//
//	req := BuildICAPRequest("OPTIONS", "icap://localhost/options", nil, nil)
//	resp, err := harness.Handle(ctx, req)
//	require.NoError(t, err)
func (h *MemoryServerHarness) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	if h.handler == nil {
		return nil, fmt.Errorf("handler not set")
	}

	return h.handler(ctx, req)
}

// MetricsServerHarness provides a harness for testing metrics collection.
//
// Usage:
//
//	harness := NewMetricsServerHarness(t)
//	harness.RecordRequest("REQMOD")
//	metrics := harness.GetMetrics()
type MetricsServerHarness struct {
	t         testing.TB
	collector *metrics.Collector
}

// NewMetricsServerHarness creates a new metrics server harness.
//
// Parameters:
//   - t: Testing instance
//
// Returns:
//   - A new MetricsServerHarness instance
//
// Example:
//
//	harness := NewMetricsServerHarness(t)
//	harness.RecordRequest("REQMOD")
func NewMetricsServerHarness(t testing.TB) *MetricsServerHarness {
	t.Helper()

	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	return &MetricsServerHarness{
		t:         t,
		collector: collector,
	}
}

// Collector returns the metrics collector.
//
// Returns:
//   - The Collector instance
func (h *MetricsServerHarness) Collector() *metrics.Collector {
	return h.collector
}

// RecordRequest records a request metric.
//
// Parameters:
//   - method: ICAP method (REQMOD, RESPMOD, OPTIONS)
//
// Example:
//
//	harness.RecordRequest("REQMOD")
func (h *MetricsServerHarness) RecordRequest(method string) {
	h.collector.RecordRequest(method)
}

// RecordRequestDuration records a request duration metric.
//
// Parameters:
//   - method: ICAP method
//   - duration: Request duration
//
// Example:
//
//	harness.RecordRequestDuration("REQMOD", time.Since(start))
func (h *MetricsServerHarness) RecordRequestDuration(method string, duration time.Duration) {
	h.collector.RecordRequestDuration(method, duration)
}

// StorageServerHarness provides a harness for testing storage operations.
//
// Usage:
//
//	harness := NewStorageServerHarness(t)
//	err := harness.StoreRequest(req)
type StorageServerHarness struct {
	t       testing.TB
	storage storage.Storage
	tempDir string
}

// NewStorageServerHarness creates a new storage server harness.
//
// Parameters:
//   - t: Testing instance
//
// Returns:
//   - A new StorageServerHarness instance
//
// Example:
//
//	harness := NewStorageServerHarness(t)
//	defer harness.Cleanup()
func NewStorageServerHarness(t testing.TB) *StorageServerHarness {
	t.Helper()

	tempDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tempDir,
		MaxFileSize: 1048576,
		RotateAfter: 100,
		Workers:     4,
		QueueSize:   100,
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          false,
			MaxFailures:      5,
			ResetTimeout:     30 * time.Second,
			SuccessThreshold: 3,
		},
	}

	stor, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	h := &StorageServerHarness{
		t:       t,
		storage: stor,
		tempDir: tempDir,
	}

	h.t.Cleanup(func() {
		if err := stor.Close(); err != nil {
			t.Errorf("Failed to stop storage: %v", err)
		}
	})

	return h
}

// Storage returns the storage instance.
//
// Returns:
//   - The Storage instance
func (h *StorageServerHarness) Storage() storage.Storage {
	return h.storage
}

// StoreRequest stores an ICAP request.
//
// Parameters:
//   - req: ICAP request to store
//
// Returns:
//   - error if storage fails
//
// Example:
//
//	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	err := harness.StoreRequest(req)
//	require.NoError(t, err)
func (h *StorageServerHarness) StoreRequest(req *icap.Request) error {
	sr := storage.FromICAPRequest(req, 204, 0)
	ctx := context.Background()
	return h.storage.SaveRequest(ctx, sr)
}

// Cleanup cleans up the storage harness.
//
// Example:
//
//	defer harness.Cleanup()
func (h *StorageServerHarness) Cleanup() error {
	return h.storage.Close()
}

// TempDir returns the temporary directory used by the storage.
//
// Returns:
//   - Temporary directory path
func (h *StorageServerHarness) TempDir() string {
	return h.tempDir
}
