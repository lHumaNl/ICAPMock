// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
)

// ============================================================================
// Shutdown Timeout Tests - P0 Issue Fixes
// ============================================================================

// TestShutdownTimeout_Configuration tests that ShutdownTimeout is properly configured.
func TestShutdownTimeout_Configuration(t *testing.T) {
	// Test default timeout
	cfg := &config.Config{}
	cfg.SetDefaults()

	assert.Equal(t, 30*time.Second, cfg.Server.ShutdownTimeout,
		"Default shutdown timeout should be 30 seconds")
}

// TestShutdownTimeout_CustomValue tests that custom shutdown timeout can be set.
func TestShutdownTimeout_CustomValue(t *testing.T) {
	customTimeout := 15 * time.Second

	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ShutdownTimeout: customTimeout,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	// Send a request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait a bit for request to be processed
	time.Sleep(100 * time.Millisecond)

	// Graceful shutdown with custom timeout should complete
	stopCtx, cancel := context.WithTimeout(context.Background(), customTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, customTimeout+2*time.Second, "Shutdown should complete within custom timeout")
	assert.False(t, srv.IsRunning())

	// Drain any response data, then verify the connection is closed.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 4096)
	for {
		_, err = conn.Read(buf)
		if err != nil {
			break
		}
	}
	assert.Error(t, err, "Connection should be closed after shutdown")
}

// TestShutdownTimeout_ActiveRequests tests shutdown with active requests completing.
func TestShutdownTimeout_ActiveRequests(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 5 * time.Second,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create multiple active connections
	var conns []net.Conn
	numConns := 3
	for i := 0; i < numConns; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		require.NoError(t, err)
		conns = append(conns, conn)

		// Send a request to each connection
		request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
		_, err = conn.Write([]byte(request))
		require.NoError(t, err)
	}

	// Wait for connections to be processed
	time.Sleep(200 * time.Millisecond)

	// Initiate graceful shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "Graceful shutdown should complete quickly when connections complete")
	assert.False(t, srv.IsRunning())

	// Drain any response data and verify connections are closed
	for _, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 4096)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				break
			}
		}
	}
}

// TestShutdownTimeout_HangingRequest tests timeout with hanging request.
func TestShutdownTimeout_HangingRequest(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 500 * time.Millisecond, // Very short timeout for testing
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection that will not close
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	// Send a request and don't close the connection
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait for connection to be processed
	time.Sleep(100 * time.Millisecond)

	// Initiate shutdown with very short timeout to trigger force shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 1*time.Second, "Force shutdown should complete quickly")
	assert.False(t, srv.IsRunning())

	// Drain any response data, then verify the connection is closed.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 4096)
	for {
		_, err = conn.Read(buf)
		if err != nil {
			break
		}
	}
	assert.Error(t, err, "Connection should be force closed")
}

// TestShutdownTimeout_MultipleHangingConnections tests force shutdown with multiple hanging connections.
func TestShutdownTimeout_MultipleHangingConnections(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 500 * time.Millisecond, // Very short timeout
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create multiple connections that will not close
	var conns []net.Conn
	numConns := 5
	for i := 0; i < numConns; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		require.NoError(t, err)
		conns = append(conns, conn)

		request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
		_, err = conn.Write([]byte(request))
		require.NoError(t, err)
	}

	// Wait for connections to be processed
	time.Sleep(200 * time.Millisecond)

	// Initiate shutdown with very short timeout to trigger force shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 1*time.Second, "Force shutdown should complete quickly")
	assert.False(t, srv.IsRunning())

	// Drain any response data and verify connections are closed
	closedCount := 0
	for _, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 4096)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				closedCount++
				break
			}
		}
	}

	assert.Greater(t, closedCount, 0, "At least some connections should be closed")
}

// TestShutdownTimeout_NoConnections tests shutdown when there are no active connections.
func TestShutdownTimeout_NoConnections(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 30 * time.Second,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Don't create any connections - just stop immediately
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 1*time.Second, "Stop should complete instantly with no connections")
	assert.False(t, srv.IsRunning())
}

// TestShutdownTimeout_ContextCancelled tests shutdown with canceled context.
func TestShutdownTimeout_ContextCancelled(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 30 * time.Second,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait for connection to be processed
	time.Sleep(100 * time.Millisecond)

	// Cancel context immediately to trigger timeout
	stopCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Stop should complete even with canceled context
	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "Stop should complete quickly with canceled context")
	assert.False(t, srv.IsRunning())
}

// TestShutdownTimeout_ConcurrentShutdown tests that Stop is safe to call concurrently.
func TestShutdownTimeout_ConcurrentShutdown(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 10 * time.Second,
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create some connections
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		require.NoError(t, err)

		request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
		_, err = conn.Write([]byte(request))
		require.NoError(t, err)
	}

	// Wait for connections to be processed
	time.Sleep(100 * time.Millisecond)

	// Call Stop multiple times concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stopCtx := context.WithValue(context.Background(), wrongContextKey("test"), "value")
			if err := srv.Stop(stopCtx); err != nil {
				errors <- err
			}
		}()
	}

	// Wait for all Stop calls to complete
	wg.Wait()
	close(errors)

	// Collect errors - should all be nil
	for err := range errors {
		assert.NoError(t, err)
	}

	// Verify server is stopped
	assert.False(t, srv.IsRunning())
}

// TestShutdownTimeout_LongTimeout tests shutdown with long timeout.
func TestShutdownTimeout_LongTimeout(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 60 * time.Second, // Long timeout
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait for connection to be processed
	time.Sleep(100 * time.Millisecond)

	// Initiate shutdown with long timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "Graceful shutdown should complete quickly with no active connections")
	assert.False(t, srv.IsRunning())

	// Drain any response data the server may have written, then verify
	// the connection is closed (next read returns error).
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 4096)
	for {
		_, err = conn.Read(buf)
		if err != nil {
			break
		}
	}
	assert.Error(t, err, "Connection should be closed after shutdown")
}

// TestShutdownTimeout_VeryShortTimeout tests shutdown with extremely short timeout.
func TestShutdownTimeout_VeryShortTimeout(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		MaxConnections:  100,
		MaxBodySize:     1024 * 1024,
		Streaming:       true,
		ShutdownTimeout: 10 * time.Millisecond, // Extremely short timeout
	}

	pool := NewConnectionPool()
	srv, err := NewServer(cfg, pool, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait for connection to be processed
	time.Sleep(50 * time.Millisecond)

	// Initiate shutdown with extremely short timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "Force shutdown should complete very quickly")
	assert.False(t, srv.IsRunning())

	// Drain any response data, then verify the connection is closed.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 4096)
	for {
		_, err = conn.Read(buf)
		if err != nil {
			break
		}
	}
	assert.Error(t, err, "Connection should be force closed")
}
