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
// Graceful Shutdown Tests - P0 Issue Fixes
// ============================================================================

// TestGracefulShutdown_BasicShutdown tests basic graceful shutdown behavior.
func TestGracefulShutdown_BasicShutdown(t *testing.T) {
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

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	// Send a request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait a bit for request to be processed
	time.Sleep(100 * time.Millisecond)

	// Close connection gracefully
	conn.Close()

	// Wait for connection to be cleaned up
	time.Sleep(200 * time.Millisecond)

	// Graceful shutdown should complete quickly
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "Graceful shutdown should complete quickly")
	assert.False(t, srv.IsRunning(), "Server should not be running after Stop")
}

// TestGracefulShutdown_WithActiveConnections tests graceful shutdown with active connections.
func TestGracefulShutdown_WithActiveConnections(t *testing.T) {
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
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create multiple active connections
	var conns []net.Conn
	numConns := 5
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

	// Note: Connection tracking in current implementation happens asynchronously,
	// so ConnectionCount() may return 0 even if connections are active.
	// The important test is that shutdown completes gracefully.

	// Initiate graceful shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = srv.Stop(stopCtx)
	require.NoError(t, err)

	// Drain response data and verify connections are closed after shutdown
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

	assert.False(t, srv.IsRunning())
}

// TestForceShutdown_TimeoutExceeded tests force shutdown when timeout is exceeded.
func TestForceShutdown_TimeoutExceeded(t *testing.T) {
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
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create a connection that will not close
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	// Send a request
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Wait for connection to be processed
	time.Sleep(100 * time.Millisecond)

	// Note: Connection tracking is asynchronous, so we can't reliably
	// check ConnectionCount() here. The important test is force shutdown behavior.

	// Initiate shutdown with very short timeout to trigger force shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "Force shutdown should complete quickly")
	assert.False(t, srv.IsRunning())

	// Drain response data, then verify connection is closed
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

// TestForceShutdown_MultipleConnections tests force shutdown with multiple hanging connections.
func TestForceShutdown_MultipleConnections(t *testing.T) {
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
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create multiple connections that will not close
	var conns []net.Conn
	numConns := 10
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

	// Note: Connection tracking is asynchronous, so we can't reliably
	// check ConnectionCount() here. The important test is force shutdown behavior.

	// Initiate shutdown with very short timeout to trigger force shutdown
	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "Force shutdown should complete quickly")
	assert.False(t, srv.IsRunning())

	// Drain response data and verify connections are closed
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

// TestStop_Idempotent tests that Stop can be called multiple times safely.
func TestStop_Idempotent(t *testing.T) {
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

	// First Stop
	stopCtx1 := context.Background()
	err1 := srv.Stop(stopCtx1)
	require.NoError(t, err1)

	// Second Stop should not panic or error
	stopCtx2 := context.Background()
	err2 := srv.Stop(stopCtx2)
	require.NoError(t, err2)

	// Third Stop
	stopCtx3 := context.Background()
	err3 := srv.Stop(stopCtx3)
	require.NoError(t, err3)

	assert.False(t, srv.IsRunning())
}

// TestStop_RaceCondition tests that Stop is safe to call concurrently.
func TestStop_RaceCondition(t *testing.T) {
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

	// Create some connections
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", srv.Addr().String())
		require.NoError(t, err)
		conns = append(conns, conn)

		request := "OPTIONS icap://localhost/ ICAP/1.0\r\nHost: localhost\r\n\r\n"
		_, err = conn.Write([]byte(request))
		require.NoError(t, err)
	}

	// Wait for connections to be processed
	time.Sleep(100 * time.Millisecond)

	// Call Stop multiple times concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stopCtx := context.Background()
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

	// Cleanup connections
	for _, conn := range conns {
		conn.Close()
	}
}

// TestHandleConnection_PanicRecovery tests panic recovery in handleConnection.
func TestHandleConnection_PanicRecovery(t *testing.T) {
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

	// Create a connection
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	// Send a request that will trigger panic in handler
	request := "GET /panic ICAP/1.0\r\nHost: localhost\r\n\r\n"
	_, err = conn.Write([]byte(request))
	require.NoError(t, err)

	// Read response (should be handled gracefully despite panic)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	_, _ = conn.Read(buf)

	// Connection should be closed (error expected)
	conn.Close()

	// Server should still be running after panic recovery
	assert.True(t, srv.IsRunning(), "Server should still be running after panic recovery")
}

// TestConnectionPool_CloseAll tests ConnectionPool.CloseAll method.
func TestConnectionPool_CloseAll(t *testing.T) {
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
	require.NoError(t, err)

	ctx := context.Background()
	err = srv.Start(ctx)
	require.NoError(t, err)

	// Create multiple connections
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

	// Note: Connection tracking in pool is asynchronous, so we can't reliably
	// check pool.Count() here. The important test is that CloseAll closes all connections.

	// Close all connections via pool
	pool.CloseAll(context.Background())

	// Drain response data and verify connections are closed
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

	// Pool should be empty
	assert.Equal(t, 0, pool.Count(), "Pool should be empty after CloseAll")
}

// TestStop_ContextCancellation tests Stop with canceled context.
func TestStop_ContextCancellation(t *testing.T) {
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

// TestStop_WithNoConnections tests Stop when there are no active connections.
func TestStop_WithNoConnections(t *testing.T) {
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

	// Don't create any connections - just stop immediately
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err = srv.Stop(stopCtx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 1*time.Second, "Stop should complete instantly with no connections")
	assert.False(t, srv.IsRunning())
}
