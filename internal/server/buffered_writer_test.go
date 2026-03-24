// Package server provides tests for bufferedWriter buffering behavior.
// These tests verify that the bufferedWriter properly buffers small writes
// and reduces syscall overhead.
package server

import (
	"bytes"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

// TestBufferedWriter_BuffersSmallWrites verifies that small writes are
// accumulated in the buffer and not immediately written to the underlying writer.
func TestBufferedWriter_BuffersSmallWrites(t *testing.T) {
	t.Parallel()

	// Create a mock writer that tracks write calls
	var mu sync.Mutex
	var writeCalls int
	var totalBytes int
	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			mu.Lock()
			defer mu.Unlock()
			writeCalls++
			totalBytes += len(p)
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// Write multiple small chunks
	chunks := []string{"ICAP/1.0 ", "200 ", "OK\r\n", "Host: ", "localhost\r\n", "\r\n"}
	for _, chunk := range chunks {
		n, err := bw.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("Write(%q) error: %v", chunk, err)
		}
		if n != len(chunk) {
			t.Errorf("Write(%q) returned %d, want %d", chunk, n, len(chunk))
		}
	}

	// Before flush, no writes should have gone to underlying writer
	mu.Lock()
	callsBeforeFlush := writeCalls
	mu.Unlock()

	if callsBeforeFlush != 0 {
		t.Errorf("Expected no writes to underlying writer before flush, got %d", callsBeforeFlush)
	}

	// Flush should cause one write
	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	mu.Lock()
	callsAfterFlush := writeCalls
	mu.Unlock()

	if callsAfterFlush != 1 {
		t.Errorf("Expected 1 write to underlying writer after flush, got %d", callsAfterFlush)
	}

	// Verify all data was written
	expected := ""
	for _, chunk := range chunks {
		expected += chunk
	}
	if written.String() != expected {
		t.Errorf("Written data = %q, want %q", written.String(), expected)
	}
}

// TestBufferedWriter_LargeWriteBypassesBuffer verifies that writes larger
// than the buffer capacity bypass the buffer and go directly to the underlying writer.
func TestBufferedWriter_LargeWriteBypassesBuffer(t *testing.T) {
	t.Parallel()

	var writeCalls int
	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			writeCalls++
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// First, buffer some small data
	bw.Write([]byte("small"))

	// Write a large chunk (>8KB, buffer size)
	largeData := make([]byte, 10*1024) // 10KB
	for i := range largeData {
		largeData[i] = 'X'
	}

	n, err := bw.Write(largeData)
	if err != nil {
		t.Fatalf("Write(large) error: %v", err)
	}
	if n != len(largeData) {
		t.Errorf("Write(large) returned %d, want %d", n, len(largeData))
	}

	// The large write should have triggered:
	// 1. A flush of the small buffered data
	// 2. A direct write of the large data
	if writeCalls != 2 {
		t.Errorf("Expected 2 writes (flush + direct), got %d", writeCalls)
	}

	// Verify the small data was written first
	if !bytes.HasPrefix(written.Bytes(), []byte("small")) {
		t.Error("Expected small data to be written first")
	}
}

// TestBufferedWriter_FlushOnClose verifies that close() flushes any remaining data.
func TestBufferedWriter_FlushOnClose(t *testing.T) {
	t.Parallel()

	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)

	// Write some data but don't flush
	testData := "test data to be flushed on close"
	bw.Write([]byte(testData))

	// close() should flush the data
	bw.close()

	if written.String() != testData {
		t.Errorf("Data after close = %q, want %q", written.String(), testData)
	}
}

// TestBufferedWriter_EmptyFlush verifies that flushing an empty buffer is a no-op.
func TestBufferedWriter_EmptyFlush(t *testing.T) {
	t.Parallel()

	writeCalled := false

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			writeCalled = true
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// Flush empty buffer
	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	if writeCalled {
		t.Error("Flush of empty buffer should not call underlying writer")
	}
}

// TestBufferedWriter_WriteString verifies WriteString method works correctly.
func TestBufferedWriter_WriteString(t *testing.T) {
	t.Parallel()

	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	testStr := "Hello, World!"
	n, err := bw.WriteString(testStr)
	if err != nil {
		t.Fatalf("WriteString() error: %v", err)
	}
	if n != len(testStr) {
		t.Errorf("WriteString() returned %d, want %d", n, len(testStr))
	}

	// Flush to write data
	if err := bw.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	if written.String() != testStr {
		t.Errorf("Written = %q, want %q", written.String(), testStr)
	}
}

// TestBufferedWriter_WriteStringLarge verifies WriteString handles large strings
// that exceed buffer capacity, matching the behavior of Write for large data.
func TestBufferedWriter_WriteStringLarge(t *testing.T) {
	t.Parallel()

	var writeCalls int
	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			writeCalls++
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// First, buffer some small data
	bw.WriteString("small")

	// Write a large string (>8KB, exceeding buffer capacity)
	largeStr := string(make([]byte, 10*1024)) // 10KB of null bytes
	for i := range []byte(largeStr) {
		_ = i
	}
	// Build a large string of 'Y' characters
	largeBytes := make([]byte, 10*1024)
	for i := range largeBytes {
		largeBytes[i] = 'Y'
	}
	largeStr = string(largeBytes)

	n, err := bw.WriteString(largeStr)
	if err != nil {
		t.Fatalf("WriteString(large) error: %v", err)
	}
	if n != len(largeStr) {
		t.Errorf("WriteString(large) returned %d, want %d", n, len(largeStr))
	}

	// The large write should have triggered:
	// 1. A flush of the small buffered data
	// 2. A direct write of the large string
	if writeCalls != 2 {
		t.Errorf("Expected 2 writes (flush + direct), got %d", writeCalls)
	}

	// Verify the small data was written first, followed by large data
	if !bytes.HasPrefix(written.Bytes(), []byte("small")) {
		t.Error("Expected small data to be written first")
	}
	if written.Len() != 5+len(largeStr) {
		t.Errorf("Total written = %d, want %d", written.Len(), 5+len(largeStr))
	}

	// Test WriteString with string larger than available but smaller than capacity
	writeCalls = 0
	written.Reset()

	bw2 := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw2.close()

	// Fill most of the buffer
	filler := strings.Repeat("A", pool.SizeMedium-100)
	bw2.WriteString(filler)

	// Write a string that's 200 bytes - larger than available (100) but smaller than capacity
	mediumStr := strings.Repeat("B", 200)
	n, err = bw2.WriteString(mediumStr)
	if err != nil {
		t.Fatalf("WriteString(medium) error: %v", err)
	}
	if n != 200 {
		t.Errorf("WriteString(medium) returned %d, want 200", n)
	}

	// Should have flushed the filler, then buffered the medium string
	if writeCalls != 1 {
		t.Errorf("Expected 1 flush call, got %d", writeCalls)
	}

	// Flush remaining
	bw2.Flush()
	if written.Len() != len(filler)+200 {
		t.Errorf("Total written = %d, want %d", written.Len(), len(filler)+200)
	}

	// Test empty WriteString is a no-op
	writeCalls = 0
	written.Reset()
	bw3 := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw3.close()
	n, err = bw3.WriteString("")
	if err != nil {
		t.Fatalf("WriteString empty error: %v", err)
	}
	if n != 0 {
		t.Errorf("WriteString empty returned %d, want 0", n)
	}
}

// TestBufferedWriter_ConnectionIntegration tests the buffered writer with a real connection.
func TestBufferedWriter_ConnectionIntegration(t *testing.T) {
	t.Parallel()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(serverConn, config)

	// Read from client side
	var received bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(&received, clientConn)
	}()

	// Write multiple small chunks via connection writer
	writer := conn.Writer()
	chunks := []string{
		"ICAP/1.0 200 OK\r\n",
		"Host: localhost\r\n",
		"Connection: keep-alive\r\n",
		"\r\n",
	}
	for _, chunk := range chunks {
		_, err := writer.WriteString(chunk)
		if err != nil {
			t.Fatalf("WriteString() error: %v", err)
		}
	}

	// Explicitly flush
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	// Close connection
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Wait for read to complete
	wg.Wait()

	// Verify all chunks were received
	expected := ""
	for _, chunk := range chunks {
		expected += chunk
	}
	if received.String() != expected {
		t.Errorf("Received = %q, want %q", received.String(), expected)
	}
}

// TestBufferedWriter_MultipleFlushes tests that multiple flush cycles work correctly.
func TestBufferedWriter_MultipleFlushes(t *testing.T) {
	t.Parallel()

	var writeCalls int
	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			writeCalls++
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// First batch
	bw.Write([]byte("batch1-"))
	bw.Flush()

	// Second batch
	bw.Write([]byte("batch2-"))
	bw.Flush()

	// Third batch
	bw.Write([]byte("batch3"))
	bw.Flush()

	// Should have 3 write calls (one per flush)
	if writeCalls != 3 {
		t.Errorf("Expected 3 write calls, got %d", writeCalls)
	}

	expected := "batch1-batch2-batch3"
	if written.String() != expected {
		t.Errorf("Written = %q, want %q", written.String(), expected)
	}
}

// TestBufferedWriter_BufferFillAndFlush tests writing exactly the buffer size.
func TestBufferedWriter_BufferFillAndFlush(t *testing.T) {
	t.Parallel()

	var writeCalls int
	var written bytes.Buffer

	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			writeCalls++
			written.Write(p)
			return len(p), nil
		},
	}

	bw := newBufferedWriter(mockWriter, pool.BufferPool)
	defer bw.close()

	// TestBufferedWriter_BufferFillAndFlush tests writing and filling the buffer.
	// The tests verify that when the buffer is exactly full (8KB), it data is
	// flushed, and when an extra byte triggers another flush cycle.
	//
	// Buffer size: 8KB (8192 bytes)
	// Chunks: 1 byte each (8192 chunks to fill exactly 8192 bytes)
	// Total expected: 8192 + 1 = 8193 bytes
	//
	// This test is sensitive to the buffer size calculation and needs to match
	// implementation more precisely.
	chunkSize := 1
	numChunks := pool.SizeMedium / chunkSize // 8192 chunks fills exactly 8192 bytes
	totalWritten := numChunks * chunkSize    // 8192 bytes total in chunks

	for i := 0; i < numChunks; i++ {
		chunk := make([]byte, chunkSize)
		for j := range chunk {
			chunk[j] = byte(i % 256)
		}
		bw.Write(chunk)
	}

	// Write one more byte to trigger flush
	bw.Write([]byte("X"))

	// Should have written the buffered data
	if writeCalls < 1 {
		t.Error("Expected at least one write call after buffer fill")
	}

	// Final flush
	bw.Flush()

	// Verify total bytes written
	expectedSize := totalWritten + 1 // +1 for the extra byte
	if written.Len() != expectedSize {
		t.Errorf("Total bytes written = %d, want %d", written.Len(), expectedSize)
	}
}

// mockTrackingWriter is a mock io.Writer that tracks write calls.
type mockTrackingWriter struct {
	writeFunc func(p []byte) (int, error)
}

func (m *mockTrackingWriter) Write(p []byte) (int, error) {
	if m.writeFunc == nil {
		return len(p), nil
	}
	return m.writeFunc(p)
}

// BenchmarkBufferedWriter_SmallWrites benchmarks buffering small writes.
func BenchmarkBufferedWriter_SmallWrites(b *testing.B) {
	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			return len(p), nil
		},
	}

	chunks := [][]byte{
		[]byte("ICAP/1.0 200 OK\r\n"),
		[]byte("Host: localhost\r\n"),
		[]byte("Connection: keep-alive\r\n"),
		[]byte("\r\n"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bw := newBufferedWriter(mockWriter, pool.BufferPool)
		for _, chunk := range chunks {
			bw.Write(chunk)
		}
		bw.Flush()
		bw.close()
	}
}

// BenchmarkBufferedWriter_LargeWrites benchmarks large write handling.
func BenchmarkBufferedWriter_LargeWrites(b *testing.B) {
	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			return len(p), nil
		},
	}

	largeData := make([]byte, 16*1024) // 16KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bw := newBufferedWriter(mockWriter, pool.BufferPool)
		bw.Write(largeData)
		bw.close()
	}
}

// BenchmarkBufferedWriter_MixedWrites benchmarks mixed small and large writes.
func BenchmarkBufferedWriter_MixedWrites(b *testing.B) {
	mockWriter := &mockTrackingWriter{
		writeFunc: func(p []byte) (int, error) {
			return len(p), nil
		},
	}

	smallChunk := []byte("ICAP/1.0 200 OK\r\n")
	largeChunk := make([]byte, 10*1024) // 10KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bw := newBufferedWriter(mockWriter, pool.BufferPool)
		bw.Write(smallChunk)
		bw.Write(largeChunk)
		bw.Write(smallChunk)
		bw.Flush()
		bw.close()
	}
}
