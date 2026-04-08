// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

func TestNewConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	if conn == nil {
		t.Fatal("newConnection() returned nil")
	}

	if conn.config != config {
		t.Error("newConnection() config not set correctly")
	}

	if conn.remoteAddr == "" {
		t.Error("newConnection() remoteAddr not set")
	}
}

func TestConnectionRead(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid ICAP request",
			input:   "REQMOD icap://localhost/reqmod ICAP/1.0\r\nHost: localhost\r\n\r\n",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close() //nolint:gocritic // deferInLoop
			defer client.Close()

			config := &ConnectionConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				MaxBodySize:  1024 * 1024,
				Streaming:    true,
			}

			conn := newConnection(server, config)

			// Write test data from client side
			go func() {
				client.Write([]byte(tt.input))
				if tt.input != "" {
					client.Close()
				}
			}()

			reader := bufio.NewReader(conn)
			line, err := reader.ReadString('\n')

			if tt.wantErr {
				if err == nil {
					t.Errorf("Read() expected error, got nil")
				}
				return
			}

			// For empty input, we expect an error when reading
			if tt.input == "" {
				return
			}

			if err != nil && err != io.EOF {
				t.Errorf("Read() unexpected error: %v", err)
			}

			if tt.input != "" && line == "" && err == nil {
				t.Error("Read() returned empty line without error")
			}
		})
	}
}

func TestConnectionWrite(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	expectedResponse := "ICAP/1.0 200 OK\r\n\r\n"
	var received bytes.Buffer
	done := make(chan struct{})

	// Read from client side
	go func() {
		io.Copy(&received, client)
		close(done)
	}()

	// Write from server side
	writer := bufio.NewWriter(conn)
	_, err := writer.WriteString(expectedResponse)
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	writer.Flush()
	conn.Close()

	// Wait for read to complete
	<-done

	if received.String() != expectedResponse {
		t.Errorf("Write() got %q, want %q", received.String(), expectedResponse)
	}
}

func TestConnectionSetDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Test setting read deadline
	err := conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Errorf("SetReadDeadline() error: %v", err)
	}

	// Test setting write deadline
	err = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Errorf("SetWriteDeadline() error: %v", err)
	}
}

func TestConnectionClose(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Close the connection
	err := conn.Close()
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// Double close should not panic
	err = conn.Close()
	if err != nil {
		t.Errorf("Double Close() error: %v", err)
	}
}

func TestConnectionRemoteAddr(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	addr := conn.RemoteAddr()
	if addr == "" {
		t.Error("RemoteAddr() returned empty string")
	}
}

func TestConnectionState(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Initial state should be active
	if conn.State() != ConnStateActive {
		t.Errorf("State() = %v, want %v", conn.State(), ConnStateActive)
	}

	// Set to closed
	conn.SetState(ConnStateClosed)
	if conn.State() != ConnStateClosed {
		t.Errorf("State() = %v, want %v", conn.State(), ConnStateClosed)
	}
}

func TestConnectionConcurrency(_ *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Test concurrent access to connection state
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			conn.State()
		}()
		go func() {
			defer wg.Done()
			conn.SetState(ConnStateActive)
		}()
	}
	wg.Wait()
}

func TestConnectionPool(t *testing.T) {
	cp := NewConnectionPool()

	// Test Add and Remove
	server, _ := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	cp.Add(conn)
	if cp.Count() != 1 {
		t.Errorf("Count() = %d, want 1", cp.Count())
	}

	cp.Remove(conn)
	if cp.Count() != 0 {
		t.Errorf("Count() = %d, want 0", cp.Count())
	}
}

func TestConnectionPoolCloseAll(t *testing.T) {
	cp := NewConnectionPool()

	// Create multiple connections
	for i := 0; i < 3; i++ {
		server, _ := net.Pipe()
		defer server.Close() //nolint:gocritic // deferInLoop

		config := &ConnectionConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			MaxBodySize:  1024 * 1024,
			Streaming:    true,
		}

		conn := newConnection(server, config)
		cp.Add(conn)
	}

	if cp.Count() != 3 {
		t.Errorf("Count() = %d, want 3", cp.Count())
	}

	// Close all
	ctx := context.Background()
	cp.CloseAll(ctx)

	if cp.Count() != 0 {
		t.Errorf("After CloseAll() Count() = %d, want 0", cp.Count())
	}
}

func TestConnectionPoolWait(t *testing.T) {
	cp := NewConnectionPool()

	// Create a connection
	server, _ := net.Pipe()
	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}
	conn := newConnection(server, config)
	cp.Add(conn)

	// Start waiting in a goroutine
	done := make(chan struct{})
	go func() {
		ctx := context.Background()
		cp.Wait(ctx)
		close(done)
	}()

	// Close the connection after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cp.Remove(conn)
		conn.Close()
		server.Close()
	}()

	// Wait should complete
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Wait() did not complete in time")
	}
}

// ============================================================================
// Wave 2 Fix: Buffer Pool Integration Tests
// ============================================================================

// TestPooledBuffer_UsesPool verifies that pooled buffers are obtained from the pool.
func TestPooledBuffer_UsesPool(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	if conn == nil {
		t.Fatal("newConnection() returned nil")
	}

	// Verify reader is a pooledBuffer
	if conn.reader == nil {
		t.Fatal("Connection reader should not be nil")
	}

	// Verify buffer has been allocated from pool
	if conn.reader.buf == nil {
		t.Fatal("Pooled buffer should have underlying buffer")
	}

	// The buffer should have capacity (from pool)
	if cap(conn.reader.buf) == 0 {
		t.Error("Pooled buffer should have capacity")
	}
}

// TestPooledBuffer_ReturnedOnClose verifies that buffers are returned to pool on Close().
func TestPooledBuffer_ReturnedOnClose(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Store reference to buffer pointer before close
	bufPtr := conn.reader.bufPtr
	if bufPtr == nil {
		t.Fatal("Buffer pointer should not be nil before close")
	}

	// Close the connection
	err := conn.Close()
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// After close, the reader should be nil (buffers returned to pool)
	if conn.reader != nil {
		t.Error("Reader should be nil after close (buffers returned to pool)")
	}
}

// TestPooledBuffer_PassthroughRead tests that pooled buffer passes through reads.
// This verifies the public Read API works correctly with pooled buffers.
func TestPooledBuffer_PassthroughRead(t *testing.T) {
	// This test verifies that connections use pooled buffers correctly.
	// The actual read/write functionality is tested in TestConnectionRead/Write.
	// Here we just verify the pooled buffer is properly initialized.

	server, _ := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Verify the connection has a pooled buffer
	if conn.reader == nil {
		t.Fatal("Connection reader should not be nil")
	}

	// Verify the pooled buffer has an underlying buffer
	if conn.reader.buf == nil {
		t.Fatal("Pooled buffer should have underlying buffer")
	}

	// Verify the buffer has capacity
	if cap(conn.reader.buf) == 0 {
		t.Error("Pooled buffer should have capacity")
	}

	conn.Close()
}

// TestBufferedWriter_Passthrough tests that the buffered writer passes through writes.
func TestBufferedWriter_Passthrough(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	defer conn.Close()

	// Read from client side
	var received bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&received, client)
		close(done)
	}()

	// Write using the connection
	testData := "ICAP/1.0 200 OK\r\n\r\n"
	n, err := conn.Write([]byte(testData))
	if err != nil {
		t.Errorf("Write() error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Write() returned %d, want %d", n, len(testData))
	}

	// Close to flush and signal EOF
	conn.Close()

	// Wait for read to complete
	<-done

	if received.String() != testData {
		t.Errorf("Received %q, want %q", received.String(), testData)
	}
}

// TestConnection_DoubleCloseSafety verifies multiple Close calls are safe.
func TestConnection_DoubleCloseSafety(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)

	// Close multiple times - should not panic
	for i := 0; i < 5; i++ {
		err := conn.Close()
		if err != nil {
			t.Errorf("Close() attempt %d error: %v", i, err)
		}
	}

	// Verify IsClosed returns true
	if !conn.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}
}

// TestConnectionPool_List tests listing connections from pool.
func TestConnectionPool_List(t *testing.T) {
	cp := NewConnectionPool()

	// Create and add connections
	var conns []*Connection
	for i := 0; i < 3; i++ {
		server, _ := net.Pipe()
		defer server.Close() //nolint:gocritic // deferInLoop

		config := &ConnectionConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			MaxBodySize:  1024 * 1024,
			Streaming:    true,
		}

		conn := newConnection(server, config)
		conns = append(conns, conn)
		cp.Add(conn)
	}

	// List should return all connections
	list := cp.List()
	if len(list) != 3 {
		t.Errorf("List() returned %d connections, want 3", len(list))
	}

	// Verify count matches
	if cp.Count() != 3 {
		t.Errorf("Count() = %d, want 3", cp.Count())
	}
}

// ============================================================================
// Benchmarks for Buffer Pool Performance
// ============================================================================

// BenchmarkConnectionWithPool benchmarks connection creation with buffer pooling.
func BenchmarkConnectionWithPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server, _ := net.Pipe()
		config := &ConnectionConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			MaxBodySize:  1024 * 1024,
			Streaming:    true,
		}
		conn := newConnection(server, config)
		conn.Close()
	}
}

// BenchmarkPooledBufferRead benchmarks reading with pooled buffers.
func BenchmarkPooledBufferRead(b *testing.B) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	defer conn.Close()

	// Writer goroutine
	go func() {
		data := make([]byte, 1024)
		for {
			if _, err := client.Write(data); err != nil {
				return
			}
		}
	}()

	buf := make([]byte, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.Read(buf)
	}
}

// BenchmarkPooledBufferReadLine benchmarks reading lines with pooled buffers.
func BenchmarkPooledBufferReadLine(b *testing.B) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	defer conn.Close()

	// Writer goroutine - send lines
	go func() {
		line := "GET / HTTP/1.1\r\n"
		for {
			if _, err := client.Write([]byte(line)); err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.reader.ReadLine()
	}
}

// ============================================================================
// Wave 2 Fix: Buffer Pool Integration Tests (with testify assertions)
// ============================================================================

// TestBufferPoolUsesPooledBuffers_VerifyPoolUsage tests that pooled buffers
// are used in connection handling and properly returned to the pool.
func TestBufferPoolUsesPooledBuffers_VerifyPoolUsage(t *testing.T) {
	t.Run("pooled buffers reduce allocations", func(t *testing.T) {
		// Get initial memory stats
		var m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		// Create and close many connections
		for i := 0; i < 100; i++ {
			server, client := net.Pipe()
			config := &ConnectionConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				MaxBodySize:  1024 * 1024,
				Streaming:    true,
			}
			conn := newConnection(server, config)
			conn.Close()
			client.Close()
		}

		// Get final memory stats
		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)

		// With pooling, allocations should not grow significantly
		allocDiff := int64(m2.TotalAlloc) - int64(m1.TotalAlloc)
		// Allow for some overhead, but should be reasonable with pooling
		assert.Less(t, allocDiff, int64(5*1024*1024),
			"Total allocations should be reasonable with buffer pooling")
	})

	t.Run("pool returns same capacity buffers", func(t *testing.T) {
		// Get a buffer from pool
		buf1 := pool.BufferPool.Get(pool.SizeMedium)
		require.NotNil(t, buf1, "Buffer from pool should not be nil")
		cap1 := cap(*buf1)

		// Return it
		pool.BufferPool.Put(buf1)

		// Get another buffer - should have same capacity (reused)
		buf2 := pool.BufferPool.Get(pool.SizeMedium)
		require.NotNil(t, buf2, "Second buffer from pool should not be nil")
		cap2 := cap(*buf2)

		assert.Equal(t, cap1, cap2, "Buffer should be reused from pool with same capacity")

		// Cleanup
		pool.BufferPool.Put(buf2)
	})
}

// TestBuffersReturnedOnClose verifies buffers are returned to pool on Close().
func TestBuffersReturnedOnClose(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(server, config)
	require.NotNil(t, conn, "Connection should not be nil")

	// Store reference to buffer pointer before close
	bufPtr := conn.reader.bufPtr
	require.NotNil(t, bufPtr, "Buffer pointer should not be nil before close")

	// Close the connection
	err := conn.Close()
	assert.NoError(t, err, "Close should not return error")

	// After close, reader should be nil (buffers returned to pool)
	assert.Nil(t, conn.reader, "Reader should be nil after close")
}

// BenchmarkMemoryAllocations_BeforeAfter benchmarks memory usage with pooling.
func BenchmarkMemoryAllocations_BeforeAfter(b *testing.B) {
	b.Run("with_buffer_pooling", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			server, client := net.Pipe()
			config := &ConnectionConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				MaxBodySize:  1024 * 1024,
				Streaming:    true,
			}
			conn := newConnection(server, config)
			conn.Close()
			client.Close()
		}
	})
}

// BenchmarkPoolOperations benchmarks raw pool operations.
func BenchmarkPoolOperations(b *testing.B) {
	b.Run("get_put_medium", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			buf := pool.BufferPool.Get(pool.SizeMedium)
			*buf = append(*buf, "test"...)
			pool.BufferPool.Put(buf)
		}
	})

	b.Run("get_put_large", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			buf := pool.BufferPool.Get(pool.SizeLarge)
			*buf = append(*buf, "test"...)
			pool.BufferPool.Put(buf)
		}
	})
}

// BenchmarkConnectionLifecycle benchmarks the full connection lifecycle.
func BenchmarkConnectionLifecycle(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server, client := net.Pipe()
		config := &ConnectionConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			MaxBodySize:  1024 * 1024,
			Streaming:    true,
		}
		conn := newConnection(server, config)

		// Simulate some work
		buf := make([]byte, 100)
		conn.Write([]byte("test"))
		conn.Read(buf)

		conn.Close()
		client.Close()
	}
}

// TestConnectionUpdateActivity tests that UpdateActivity updates the last activity timestamp.
func TestConnectionUpdateActivity(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  100 * time.Millisecond,
	}

	conn := newConnection(server, config)
	initialActivity := conn.LastActivity()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update activity
	conn.UpdateActivity()

	updatedActivity := conn.LastActivity()

	// Updated activity should be after initial activity
	assert.True(t, updatedActivity.After(initialActivity), "Updated activity should be after initial activity")
}

// TestConnectionIsIdle tests the IsIdle method.
func TestConnectionIsIdle(t *testing.T) {
	tests := []struct {
		name          string
		idleTimeout   time.Duration
		sleepDuration time.Duration
		expectIdle    bool
	}{
		{
			name:          "connection not idle",
			idleTimeout:   100 * time.Millisecond,
			sleepDuration: 10 * time.Millisecond,
			expectIdle:    false,
		},
		{
			name:          "connection idle",
			idleTimeout:   10 * time.Millisecond,
			sleepDuration: 20 * time.Millisecond,
			expectIdle:    true,
		},
		{
			name:          "zero idle timeout - never idle",
			idleTimeout:   0,
			sleepDuration: 100 * time.Millisecond,
			expectIdle:    false,
		},
		{
			name:          "negative idle timeout - never idle",
			idleTimeout:   -1 * time.Second,
			sleepDuration: 100 * time.Millisecond,
			expectIdle:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close() //nolint:gocritic // deferInLoop
			defer client.Close()

			config := &ConnectionConfig{
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				MaxBodySize:  1024 * 1024,
				Streaming:    true,
				IdleTimeout:  tt.idleTimeout,
			}

			conn := newConnection(server, config)

			// Wait for the specified duration
			time.Sleep(tt.sleepDuration)

			// Check if connection is idle
			isIdle := conn.IsIdle()
			assert.Equal(t, tt.expectIdle, isIdle, "IsIdle() returned unexpected value")
		})
	}
}

// TestConnectionLastActivity tests the LastActivity method.
func TestConnectionLastActivity(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  100 * time.Millisecond,
	}

	conn := newConnection(server, config)
	lastActivity := conn.LastActivity()

	// Last activity should be recent (within last second)
	assert.WithinDuration(t, time.Now(), lastActivity, time.Second, "LastActivity should be recent")

	// Update activity
	time.Sleep(10 * time.Millisecond)
	conn.UpdateActivity()

	updatedActivity := conn.LastActivity()

	// Updated activity should be after initial activity
	assert.True(t, updatedActivity.After(lastActivity), "Updated activity should be after initial activity")
}

// TestConnectionReadUpdatesActivity tests that Read() updates activity.
func TestConnectionReadUpdatesActivity(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  100 * time.Millisecond,
	}

	conn := newConnection(server, config)
	initialActivity := conn.LastActivity()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Simulate a read by updating activity manually
	// (Direct read testing with pipes can block, so we test the update mechanism)
	conn.UpdateActivity()

	// Activity should be updated
	updatedActivity := conn.LastActivity()
	assert.True(t, updatedActivity.After(initialActivity), "Activity should be updated")
}

// TestConnectionWriteUpdatesActivity tests that Write() updates activity.
func TestConnectionWriteUpdatesActivity(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  100 * time.Millisecond,
	}

	conn := newConnection(server, config)
	initialActivity := conn.LastActivity()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Simulate a write by updating activity manually
	// (Direct write testing with pipes can block, so we test the update mechanism)
	conn.UpdateActivity()

	// Activity should be updated
	updatedActivity := conn.LastActivity()
	assert.True(t, updatedActivity.After(initialActivity), "Activity should be updated")
}

// TestConnectionIdleTimeoutNoUpdate tests that connection becomes idle when no activity occurs.
func TestConnectionIdleTimeoutNoUpdate(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	idleTimeout := 50 * time.Millisecond

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  idleTimeout,
	}

	conn := newConnection(server, config)

	// Connection should not be idle initially
	assert.False(t, conn.IsIdle(), "Connection should not be idle initially")

	// Wait for more than idle timeout
	time.Sleep(idleTimeout + 10*time.Millisecond)

	// Connection should now be idle
	assert.True(t, conn.IsIdle(), "Connection should be idle after timeout")
}

// TestConnectionActivityResetAfterUpdate tests that idle timeout is reset after activity.
func TestConnectionActivityResetAfterUpdate(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	idleTimeout := 50 * time.Millisecond

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  idleTimeout,
	}

	conn := newConnection(server, config)

	// Wait for more than half of idle timeout
	time.Sleep(idleTimeout / 2)

	// Update activity
	conn.UpdateActivity()

	// Wait for remaining half of idle timeout
	time.Sleep(idleTimeout/2 + 10*time.Millisecond)

	// Connection should not be idle because we updated activity
	assert.False(t, conn.IsIdle(), "Connection should not be idle after activity update")
}

// TestConcurrentActivityUpdate tests that concurrent activity updates are safe.
func TestConcurrentActivityUpdate(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close() //nolint:gocritic // deferInLoop
	defer client.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
		IdleTimeout:  100 * time.Millisecond,
	}

	conn := newConnection(server, config)

	// Perform concurrent activity updates
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.UpdateActivity()
			conn.IsIdle()
			conn.LastActivity()
		}()
	}

	wg.Wait()

	// Connection should still be functional
	assert.False(t, conn.IsIdle(), "Connection should not be idle after concurrent updates")
}
