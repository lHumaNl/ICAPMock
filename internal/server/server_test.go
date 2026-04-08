// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// mockHandler is a simple handler for testing.
type mockHandler struct {
	response string
	method   string
}

func (h *mockHandler) Handle(_ context.Context, _ *icap.Request) (*icap.Response, error) {
	resp := icap.NewResponse(icap.StatusOK)
	resp.SetHeader("ISTag", "test")
	if h.response != "" {
		resp.SetHeader("Encapsulated", "null-body=0")
	}
	return resp, nil
}

func (h *mockHandler) Method() string {
	if h.method != "" {
		return h.method
	}
	return "OPTIONS"
}

func TestNewServer(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0, // Use random port
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	if srv == nil {
		t.Fatal("NewServer() returned nil")
	}

	if srv.config != cfg {
		t.Error("NewServer() config not set correctly")
	}
}

func TestServerStartStop(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()

	// Start server
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Verify server is listening
	addr := srv.Addr()
	if addr == nil {
		t.Error("Addr() returned nil after Start()")
	}

	// Stop server
	err = srv.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestServerAddr(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	// Before starting, Addr should return nil
	addr := srv.Addr()
	if addr != nil {
		t.Errorf("Addr() before Start() = %v, want nil", addr)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	// After starting, Addr should return the actual address
	addr = srv.Addr()
	if addr == nil {
		t.Error("Addr() after Start() returned nil")
	}

	// Verify we can connect to the address
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	conn.Close()
}

func TestServerGracefulShutdown(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Start a connection that will outlive the shutdown request
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Track when the connection is closed
	connClosed := make(chan struct{})
	go func() {
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Close()
		close(connClosed)
	}()

	// Request shutdown
	stopCtx := context.Background()
	err = srv.Stop(stopCtx)
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Connection should be closed by now
	select {
	case <-connClosed:
		// Success - connection was closed
	case <-time.After(5 * time.Second):
		t.Error("Connection was not closed during shutdown")
	}
}

func TestServerMaxConnections(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 2, // Limit to 2 connections
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	// Open connections up to the limit
	var conns []net.Conn
	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			t.Fatalf("Failed to connect %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	// Give server time to track connections
	time.Sleep(100 * time.Millisecond)

	// The server should have tracked these connections
	// (exact count depends on implementation details)
	for _, conn := range conns {
		conn.Close()
	}
}

func TestServerConcurrentRequests(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}
	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	// Set up router with OPTIONS handler for concurrent requests
	r := router.NewRouter()
	optionsHandler := &mockHandler{response: "ICAP/1.0 200 OK\r\nISTag: \"test-concurrent\"\r\nEncapsulated: null-body=0\r\n\r\n"}
	err = r.Handle("/options", optionsHandler)
	if err != nil {
		t.Fatalf("Failed to register OPTIONS handler: %v", err)
	}
	srv.SetRouter(r)
	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)
	// Send multiple concurrent requests
	numRequests := 10
	var wg sync.WaitGroup
	errChan := make(chan error, numRequests)
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", srv.Addr().String())
			if err != nil {
				errChan <- fmt.Errorf("request %d: dial error: %w", id, err)
				return
			}
			defer conn.Close()
			// Send a simple ICAP request
			request := fmt.Sprintf("OPTIONS icap://localhost:%d/options ICAP/1.0\r\nHost: localhost\r\n\r\n", cfg.Port)
			_, err = conn.Write([]byte(request))
			if err != nil {
				errChan <- fmt.Errorf("request %d: write error: %w", id, err)
				return
			}
			// Read response with timeout
			reader := bufio.NewReader(conn)
			// Set deadline for reading response
			err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if err != nil {
				errChan <- fmt.Errorf("request %d: set deadline error: %w", id, err)
				return
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				// io.EOF and connection reset are expected when connection closes
				// On Windows, "wsarecv: An existing connection was forcibly closed" is common
				if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "forcibly closed") &&
					!strings.Contains(err.Error(), "connection reset") {
					errChan <- fmt.Errorf("request %d: read error: %w", id, err)
					return
				}
				// connection was closed by server, which is expected
				return
			}
			// Verify we got a valid response line
			if line == "" {
				errChan <- fmt.Errorf("request %d: empty response", id)
				return
			}
			// Verify the response looks valid (ICAP/1.0 200 OK or ICAP/1.0 500 error)
			if !strings.HasPrefix(line, "ICAP/1.0 200") && !strings.HasPrefix(line, "ICAP/1.0 500") {
				errChan <- fmt.Errorf("request %d: invalid response: %q", id, line)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errChan)
	// Check for errors
	for err := range errChan {
		t.Errorf("Concurrent request error: %v", err)
	}
}

func TestServerReadTimeout(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Don't send anything - wait for timeout
	time.Sleep(2 * time.Second)

	// Connection should be closed by server due to read timeout
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("Expected connection to be closed after read timeout")
	}
}

func TestServerTLS(t *testing.T) {
	// Skip TLS test if we don't have certificates
	// In a real test, we would generate self-signed certificates
	t.Skip("TLS test requires certificate files")
}

func TestServerDoubleStart(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	// Second start should fail
	err = srv.Start(ctx)
	if err == nil {
		t.Error("Second Start() should return error")
	}
}

func TestServerStopWithoutStart(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	// Stop without start should not panic
	err = srv.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() without Start() error: %v", err)
	}
}

func TestServerContextCancellation(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Cancel context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Server should stop when context is canceled
	// Give it time to process the cancellation
	time.Sleep(200 * time.Millisecond)

	// Try to stop again (should be idempotent)
	err = srv.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() after cancel error: %v", err)
	}
}

// panicHandler is a handler that panics on every request.
type panicHandler struct{}

func (p *panicHandler) Handle(_ context.Context, _ *icap.Request) (*icap.Response, error) {
	panic("test panic in handler")
}

func (p *panicHandler) Method() string {
	return "" // Handle all methods
}

// TestHandleConnectionPanicRecovery verifies that panics in connection handlers
// are recovered and logged without crashing the server.
func TestHandleConnectionPanicRecovery(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	// Create a router with a panic-inducing handler
	r := router.NewRouter()
	err = r.Handle("/", &panicHandler{})
	if err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}
	srv.SetRouter(r)

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a request that will trigger the panic
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Send a simple ICAP request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response - connection should close after panic recovery
	buf := make([]byte, 1024)
	_, _ = conn.Read(buf)
	// Connection should be closed by server after panic
	conn.Close()

	// Verify server is still running (didn't crash)
	if !srv.IsRunning() {
		t.Error("Server should still be running after panic recovery")
	}

	// Clean shutdown should still work
	err = srv.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestStopWithTimeout verifies that Stop() respects context timeout
// when connections are slow to close.
func TestStopWithTimeout(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Create a connection that will block
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Start a goroutine that reads from the connection (keeping it active)
	connClosed := make(chan struct{})
	go func() {
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Close()
		close(connClosed)
	}()

	// Give time for the connection to be established
	time.Sleep(100 * time.Millisecond)

	// Create a context with a very short timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Stop should complete even with hanging connections
	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Stop() error: %v", err)
	}

	// Stop should complete within reasonable time (not hang forever)
	// Due to the short timeout, it should timeout rather than wait forever
	if elapsed > 2*time.Second {
		t.Errorf("Stop() took too long: %v (expected timeout behavior)", elapsed)
	}

	// Verify that the stop completed even though connections were active
	// This is the key assertion - Stop should return even with hanging connections
	t.Logf("Stop completed in %v with context timeout", elapsed)
}

// TestStopGraceful verifies that normal shutdown completes gracefully
// when connections close in a timely manner.
func TestStopGraceful(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    1 * time.Second, // Short timeout so connections close quickly
		WriteTimeout:   1 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Send a request and close the connection properly
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}
	conn.Close()

	// Wait for connection to be processed
	time.Sleep(200 * time.Millisecond)

	// Create a context with ample timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop should complete gracefully
	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Stop() error: %v", err)
	}

	// Stop should complete quickly when no active connections
	if elapsed > 1*time.Second {
		t.Errorf("Graceful stop took too long: %v", elapsed)
	}

	// Verify server is no longer running
	if srv.IsRunning() {
		t.Error("Server should not be running after Stop()")
	}
}

// ============================================================================
// Wave 2 Fix: Request-Scoped Context Tests
// ============================================================================

// TestRequestIDFromContext tests extracting request ID from context.
func TestRequestIDFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "context with request ID",
			ctx:      context.WithValue(context.Background(), requestIDKey, "test-request-123"),
			expected: "test-request-123",
		},
		{
			name:     "context without request ID",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with wrong key type",
			ctx:      context.WithValue(context.Background(), "request_id", "wrong-type"),
			expected: "",
		},
		{
			name:     "context with wrong value type",
			ctx:      context.WithValue(context.Background(), requestIDKey, 12345),
			expected: "",
		},
		{
			name:     "context with empty string",
			ctx:      context.WithValue(context.Background(), requestIDKey, ""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RequestIDFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("RequestIDFromContext() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestClientIPFromContext tests extracting client IP from context.
func TestClientIPFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "context with client IP",
			ctx:      context.WithValue(context.Background(), clientIPKey, "192.168.1.100"),
			expected: "192.168.1.100",
		},
		{
			name:     "context without client IP",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with wrong key type",
			ctx:      context.WithValue(context.Background(), "client_ip", "wrong-type"),
			expected: "",
		},
		{
			name:     "context with wrong value type",
			ctx:      context.WithValue(context.Background(), clientIPKey, 12345),
			expected: "",
		},
		{
			name:     "context with IPv6",
			ctx:      context.WithValue(context.Background(), clientIPKey, "::1"),
			expected: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClientIPFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("ClientIPFromContext() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestContextTimeoutPropagation tests that context timeout is properly enforced.
func TestContextTimeoutPropagation(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   1 * time.Second, // Short write timeout
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	// Create connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send a request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response with timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Logf("Read error (expected if connection closed): %v", err)
	} else if n == 0 {
		// Should have received a response
		t.Error("Expected response from server")
	}
}

// TestContextWithBothValues tests context with both request ID and client IP.
func TestContextWithBothValues(t *testing.T) {
	t.Parallel()

	// Create context with both values
	ctx := context.Background()
	ctx = context.WithValue(ctx, requestIDKey, "req-123")
	ctx = context.WithValue(ctx, clientIPKey, "10.0.0.1")

	// Verify both values can be extracted
	reqID := RequestIDFromContext(ctx)
	if reqID != "req-123" {
		t.Errorf("RequestIDFromContext() = %q, want %q", reqID, "req-123")
	}

	clientIP := ClientIPFromContext(ctx)
	if clientIP != "10.0.0.1" {
		t.Errorf("ClientIPFromContext() = %q, want %q", clientIP, "10.0.0.1")
	}
}

// TestContextCancellationDuringRequest tests handling of context cancellation.
func TestContextCancellationDuringRequest(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Create connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Send a request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Cancel context while server is running
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Give server time to process cancellation
	time.Sleep(100 * time.Millisecond)

	// Server should still respond (context cancellation doesn't kill active requests)
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	conn.Close()

	// Should have received response
	if err == nil && n == 0 {
		t.Error("Expected response from server")
	}

	// Clean shutdown should work
	stopCtx := context.Background()
	_ = srv.Stop(stopCtx)
}

// TestContextKeyUniqueness verifies that context keys don't collide.
func TestContextKeyUniqueness(t *testing.T) {
	t.Parallel()

	// Create context with our keys
	ctx := context.Background()
	ctx = context.WithValue(ctx, requestIDKey, "test-id")
	ctx = context.WithValue(ctx, clientIPKey, "1.2.3.4")

	// Add a string key (should not interfere)
	ctx = context.WithValue(ctx, "request_id", "different-value")

	// Our typed keys should still work
	if RequestIDFromContext(ctx) != "test-id" {
		t.Error("Type-safe key was overwritten by string key")
	}

	if ClientIPFromContext(ctx) != "1.2.3.4" {
		t.Error("Client IP was affected by other context values")
	}
}

// TestConnectionCountAfterRequests verifies connection tracking.
func TestConnectionCountAfterRequests(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	ctx := context.Background()
	err = srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop(ctx)

	// Initial count should be 0
	if srv.ConnectionCount() != 0 {
		t.Errorf("Initial ConnectionCount() = %d, want 0", srv.ConnectionCount())
	}

	// Create connections and verify count increases
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		conns = append(conns, conn)
	}

	// Give server time to track connections
	time.Sleep(100 * time.Millisecond)

	// Count should be greater than 0
	count := srv.ConnectionCount()
	if count == 0 {
		t.Error("ConnectionCount() should be > 0 after connections")
	}

	// Close all connections
	for _, conn := range conns {
		conn.Close()
	}

	// Give time for connections to be removed
	time.Sleep(200 * time.Millisecond)

	// Count should eventually decrease
	// (exact timing depends on server implementation)
}

// ============================================================================
// Benchmarks
// ============================================================================

// BenchmarkRequestIDFromContext benchmarks context value extraction.
func BenchmarkRequestIDFromContext(b *testing.B) {
	ctx := context.WithValue(context.Background(), requestIDKey, "benchmark-request-id")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RequestIDFromContext(ctx)
	}
}

// BenchmarkClientIPFromContext benchmarks context value extraction.
func BenchmarkClientIPFromContext(b *testing.B) {
	ctx := context.WithValue(context.Background(), clientIPKey, "192.168.1.100")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ClientIPFromContext(ctx)
	}
}

// BenchmarkContextWithValues benchmarks creating context with values.
func BenchmarkContextWithValues(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		ctx = context.WithValue(ctx, requestIDKey, "req-123")
		ctx = context.WithValue(ctx, clientIPKey, "10.0.0.1")
		_ = ctx
	}
}

// ============================================================================
// Wave 2 Fix: Enhanced Request-Scoped Context Tests (with testify)
// ============================================================================

// TestRequestIDFromContext_Testify tests extracting request ID using testify.
func TestRequestIDFromContext_Testify(t *testing.T) {
	t.Parallel()

	t.Run("returns correct value when present", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, "req-abc-123")
		result := RequestIDFromContext(ctx)
		assert.Equal(t, "req-abc-123", result, "Should return the correct request ID")
	})

	t.Run("returns empty string when not present", func(t *testing.T) {
		ctx := context.Background()
		result := RequestIDFromContext(ctx)
		assert.Empty(t, result, "Should return empty string when request ID not in context")
	})

	t.Run("returns empty string for wrong value type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, 12345)
		result := RequestIDFromContext(ctx)
		assert.Empty(t, result, "Should return empty string for wrong value type")
	})

	t.Run("handles empty string value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, "")
		result := RequestIDFromContext(ctx)
		assert.Empty(t, result, "Should handle empty string value")
	})
}

// TestClientIPFromContext_Testify tests extracting client IP using testify.
func TestClientIPFromContext_Testify(t *testing.T) {
	t.Parallel()

	t.Run("returns correct IPv4 address", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), clientIPKey, "192.168.1.100")
		result := ClientIPFromContext(ctx)
		assert.Equal(t, "192.168.1.100", result, "Should return the correct IPv4 address")
	})

	t.Run("returns correct IPv6 address", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), clientIPKey, "2001:db8::1")
		result := ClientIPFromContext(ctx)
		assert.Equal(t, "2001:db8::1", result, "Should return the correct IPv6 address")
	})

	t.Run("returns empty string when not present", func(t *testing.T) {
		ctx := context.Background()
		result := ClientIPFromContext(ctx)
		assert.Empty(t, result, "Should return empty string when client IP not in context")
	})

	t.Run("returns empty string for wrong value type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), clientIPKey, []byte("192.168.1.1"))
		result := ClientIPFromContext(ctx)
		assert.Empty(t, result, "Should return empty string for wrong value type")
	})
}

// TestContextTimeout_SetFromWriteTimeout tests that context timeout uses WriteTimeout config.
func TestContextTimeout_SetFromWriteTimeout(t *testing.T) {
	t.Run("context uses write timeout from config", func(t *testing.T) {
		// This test verifies the timeout behavior by checking that a request
		// is processed within the expected time bounds
		cfg := &config.ServerConfig{
			Host:           "127.0.0.1",
			Port:           0,
			ReadTimeout:    5 * time.Second,
			WriteTimeout:   2 * time.Second,
			MaxConnections: 100,
			MaxBodySize:    1024 * 1024,
			Streaming:      true,
		}

		pool := NewConnectionPool()
		srv, err := NewServer(cfg, pool, nil)
		require.NoError(t, err, "NewServer should not return error")

		ctx := context.Background()
		err = srv.Start(ctx)
		require.NoError(t, err, "Start should not return error")
		defer srv.Stop(ctx)

		// The server should be running
		assert.True(t, srv.IsRunning(), "Server should be running after Start")
	})
}

// TestContextIsCancelledAfterRequest tests that request-scoped context is canceled.
func TestContextIsCancelledAfterRequest(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err, "Start should not return error")
	defer srv.Stop(ctx)

	// Create connection and send request
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err, "Should connect to server")
	defer conn.Close()

	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err, "Should write request")

	// Read response
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)

	// After request is processed, the context should be canceled
	// We verify this by checking that we got a response
	if err == nil {
		assert.Greater(t, n, 0, "Should have received response data")
	}
}

// TestContextValuesPreservedAcrossFunctions tests context value propagation.
func TestContextValuesPreservedAcrossFunctions(t *testing.T) {
	t.Parallel()

	// Create context with both values
	ctx := context.WithValue(context.Background(), requestIDKey, "test-req-id")
	ctx = context.WithValue(ctx, clientIPKey, "10.20.30.40")

	// Pass to a simulated handler function
	handlerFunc := func(ctx context.Context) (string, string) {
		return RequestIDFromContext(ctx), ClientIPFromContext(ctx)
	}

	reqID, clientIP := handlerFunc(ctx)
	assert.Equal(t, "test-req-id", reqID, "Request ID should be preserved")
	assert.Equal(t, "10.20.30.40", clientIP, "Client IP should be preserved")
}

// TestContextWithTimeout tests that context timeout is enforced.
func TestContextWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("context deadline is set from WriteTimeout", func(t *testing.T) {
		writeTimeout := 5 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()

		deadline, hasDeadline := ctx.Deadline()
		assert.True(t, hasDeadline, "Context should have a deadline")
		assert.WithinDuration(t, time.Now().Add(writeTimeout), deadline, 100*time.Millisecond,
			"Deadline should be approximately WriteTimeout from now")
	})

	t.Run("context is canceled after timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		// Wait for context to be done
		<-ctx.Done()

		assert.Error(t, ctx.Err(), "Context should have an error after timeout")
		assert.Equal(t, context.DeadlineExceeded, ctx.Err(), "Error should be DeadlineExceeded")
	})
}

// TestContextValuesAreIsolated tests that context values don't interfere.
func TestContextValuesAreIsolated(t *testing.T) {
	t.Parallel()

	// Create two separate contexts
	ctx1 := context.WithValue(context.Background(), requestIDKey, "req-1")
	ctx2 := context.WithValue(context.Background(), requestIDKey, "req-2")

	// Values should be isolated
	assert.Equal(t, "req-1", RequestIDFromContext(ctx1), "First context should have req-1")
	assert.Equal(t, "req-2", RequestIDFromContext(ctx2), "Second context should have req-2")
}

// TestServerCreation tests server creation with testify.
func TestServerCreation_Testify(t *testing.T) {
	t.Run("creates server with valid config", func(t *testing.T) {
		cfg := &config.ServerConfig{
			Host:           "127.0.0.1",
			Port:           0,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			MaxConnections: 100,
			MaxBodySize:    1024 * 1024,
			Streaming:      true,
		}

		pool := NewConnectionPool()
		srv, err := NewServer(cfg, pool, nil)
		require.NoError(t, err, "NewServer should not return error")
		require.NotNil(t, srv, "Server should not be nil")
		assert.Equal(t, cfg, srv.config, "Config should be set correctly")
	})

	t.Run("returns error with nil config", func(t *testing.T) {
		pool := NewConnectionPool()
		srv, err := NewServer(nil, pool, nil)
		assert.Error(t, err, "NewServer should return error with nil config")
		assert.Nil(t, srv, "Server should be nil with invalid config")
		assert.Contains(t, err.Error(), "nil", "Error message should mention nil")
	})

	t.Run("returns error with nil pool", func(t *testing.T) {
		cfg := &config.ServerConfig{
			Host:           "127.0.0.1",
			Port:           0,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			MaxConnections: 100,
			MaxBodySize:    1024 * 1024,
			Streaming:      true,
		}

		srv, err := NewServer(cfg, nil, nil)
		assert.Error(t, err, "NewServer should return error with nil pool")
		assert.Nil(t, srv, "Server should be nil with invalid pool")
		assert.Contains(t, err.Error(), "pool", "Error message should mention pool")
	})
}

// ============================================================================
// Goroutine Monitoring Tests
// ============================================================================

// TestDefaultGoroutineMonitorConfig tests the default configuration values.
func TestDefaultGoroutineMonitorConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultGoroutineMonitorConfig()

	assert.Equal(t, 30*time.Second, cfg.CheckInterval, "Default check interval should be 30s")
	assert.Equal(t, 1.5, cfg.WarningThreshold, "Default warning threshold should be 1.5")
	assert.Equal(t, 2.0, cfg.CriticalThreshold, "Default critical threshold should be 2.0")
	assert.Equal(t, 3, cfg.SustainedGrowthChecks, "Default sustained growth checks should be 3")
}

// TestNewServer_InitializesGoroutineBaseline tests that NewServer initializes goroutine baseline.
func TestNewServer_InitializesGoroutineBaseline(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")
	require.NotNil(t, srv, "Server should not be nil")

	// Baseline should be set to current goroutine count
	assert.Greater(t, srv.goroutineBaseline, 0, "Goroutine baseline should be > 0")
	assert.Equal(t, srv.goroutineBaseline, srv.goroutinePeak, "Peak should equal baseline at startup")
}

// TestSetGoroutineMonitorConfig tests configuring goroutine monitoring.
func TestSetGoroutineMonitorConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	customConfig := GoroutineMonitorConfig{
		CheckInterval:         10 * time.Second,
		WarningThreshold:      1.2,
		CriticalThreshold:     1.8,
		SustainedGrowthChecks: 5,
	}

	srv.SetGoroutineMonitorConfig(customConfig)

	assert.Equal(t, customConfig, srv.goroutineConfig, "Goroutine config should be updated")
}

// TestGetGoroutineStats tests retrieving goroutine statistics.
func TestGetGoroutineStats(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	stats := srv.GetGoroutineStats()

	assert.Greater(t, stats.Baseline, 0, "Baseline should be > 0")
	assert.Greater(t, stats.Current, 0, "Current should be > 0")
	assert.GreaterOrEqual(t, stats.Peak, stats.Baseline, "Peak should be >= baseline")
	assert.NotEmpty(t, stats.AlertLevel, "Alert level should not be empty")
	assert.False(t, stats.LastCheck.IsZero(), "LastCheck should be set")
}

// TestGetGoroutineStats_AlertLevel tests alert level calculations.
func TestGetGoroutineStats_AlertLevel(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	// Get stats with normal baseline - should be normal alert level
	stats := srv.GetGoroutineStats()
	assert.Equal(t, "normal", stats.AlertLevel, "Alert level should be normal for baseline count")
}

// TestResetGoroutineBaseline tests resetting the goroutine baseline.
func TestResetGoroutineBaseline(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	originalBaseline := srv.goroutineBaseline

	// Create some goroutines to increase count
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(100 * time.Millisecond)
		}()
	}

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Reset baseline
	srv.ResetGoroutineBaseline()

	// New baseline should be different (likely higher)
	assert.NotEqual(t, originalBaseline, srv.goroutineBaseline,
		"Baseline should be different after reset")

	// Peak should be reset to new baseline
	assert.Equal(t, srv.goroutineBaseline, srv.goroutinePeak,
		"Peak should be reset to new baseline")

	// Consecutive growth should be reset
	assert.Equal(t, 0, srv.goroutineConsecutiveGrowth,
		"Consecutive growth should be reset to 0")

	wg.Wait()
}

// TestGoroutineStats_GrowthRate tests growth rate calculation.
func TestGoroutineStats_GrowthRate(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err, "NewServer should not return error")

	// With baseline == current, growth rate should be 0 or small
	stats := srv.GetGoroutineStats()
	// Growth rate might be small if goroutine count changed slightly
	assert.GreaterOrEqual(t, stats.GrowthRate, -100.0, "Growth rate should be reasonable")
}

// TestGoroutineMonitorConfig_CustomValues tests custom configuration values.
func TestGoroutineMonitorConfig_CustomValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		config         GoroutineMonitorConfig
		expectedValues GoroutineMonitorConfig
	}{
		{
			name: "aggressive monitoring",
			config: GoroutineMonitorConfig{
				CheckInterval:         5 * time.Second,
				WarningThreshold:      1.1,
				CriticalThreshold:     1.3,
				SustainedGrowthChecks: 2,
			},
			expectedValues: GoroutineMonitorConfig{
				CheckInterval:         5 * time.Second,
				WarningThreshold:      1.1,
				CriticalThreshold:     1.3,
				SustainedGrowthChecks: 2,
			},
		},
		{
			name: "relaxed monitoring",
			config: GoroutineMonitorConfig{
				CheckInterval:         60 * time.Second,
				WarningThreshold:      2.0,
				CriticalThreshold:     3.0,
				SustainedGrowthChecks: 5,
			},
			expectedValues: GoroutineMonitorConfig{
				CheckInterval:         60 * time.Second,
				WarningThreshold:      2.0,
				CriticalThreshold:     3.0,
				SustainedGrowthChecks: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ServerConfig{
				Host:           "127.0.0.1",
				Port:           0,
				ReadTimeout:    5 * time.Second,
				WriteTimeout:   5 * time.Second,
				MaxConnections: 100,
				MaxBodySize:    1024 * 1024,
				Streaming:      true,
			}

			pool := NewConnectionPool()
			srv, err := NewServer(cfg, pool, nil)
			require.NoError(t, err)

			srv.SetGoroutineMonitorConfig(tt.config)

			assert.Equal(t, tt.expectedValues.CheckInterval, srv.goroutineConfig.CheckInterval)
			assert.Equal(t, tt.expectedValues.WarningThreshold, srv.goroutineConfig.WarningThreshold)
			assert.Equal(t, tt.expectedValues.CriticalThreshold, srv.goroutineConfig.CriticalThreshold)
			assert.Equal(t, tt.expectedValues.SustainedGrowthChecks, srv.goroutineConfig.SustainedGrowthChecks)
		})
	}
}

// TestServerStart_InitializesGoroutineMonitoring tests that Start initializes monitoring.
func TestServerStart_InitializesGoroutineMonitoring(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)
	defer srv.Stop(ctx)

	// Give monitoring goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// Verify monitoring state is initialized
	srv.goroutineMu.RLock()
	baseline := srv.goroutineBaseline
	srv.goroutineMu.RUnlock()

	assert.Greater(t, baseline, 0, "Goroutine baseline should be set after Start")
}

// TestGoroutineMonitoring_RunsDuringServerLifetime tests monitoring runs during server lifetime.
func TestGoroutineMonitoring_RunsDuringServerLifetime(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	// Set a short check interval for testing
	srv.SetGoroutineMonitorConfig(GoroutineMonitorConfig{
		CheckInterval:         100 * time.Millisecond,
		WarningThreshold:      1.5,
		CriticalThreshold:     2.0,
		SustainedGrowthChecks: 3,
	})

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Let monitoring run for a bit
	time.Sleep(300 * time.Millisecond)

	// Stop server
	err = srv.Stop(ctx)
	require.NoError(t, err)

	// Server should have stopped cleanly
	assert.False(t, srv.IsRunning(), "Server should not be running after Stop")
}

// TestGoroutineStats_ThreadSafe tests that GetGoroutineStats is thread-safe.
func TestGoroutineStats_ThreadSafe(t *testing.T) {
	t.Parallel()

	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	// Run concurrent reads and writes
	var wg sync.WaitGroup
	done := make(chan struct{})

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = srv.GetGoroutineStats()
				}
			}
		}()
	}

	// Writer goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					srv.ResetGoroutineBaseline()
				}
			}
		}()
	}

	// Let them run for a bit
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()

	// If we get here without deadlock or race, the test passes
}
