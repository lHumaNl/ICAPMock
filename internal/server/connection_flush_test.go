// Package server provides tests for connection Close() flush behavior.
// These tests verify that Close() flushes data before closing.
package server

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestCloseFlushesWriter verifies that Close() flushes the buffered writer
// before closing the underlying connection.
// WAVE-001/WAVE-002: Flush prevents data loss on client disconnect.
func TestCloseFlushesWriter(t *testing.T) {
	t.Parallel()

	serverConn, clientConn := net.Pipe()

	config := &ConnectionConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxBodySize:  1024 * 1024,
		Streaming:    true,
	}

	conn := newConnection(serverConn, config)

	// Read from client side in a goroutine first
	var received bytes.Buffer
	var readErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer clientConn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := clientConn.Read(buf)
			if n > 0 {
				received.Write(buf[:n])
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				readErr = err
				return
			}
		}
	}()

	// Write test data
	testData := []byte("ICAP/1.0 200 OK\r\n\r\n")
	n, err := conn.Write(testData)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Write() returned %d bytes, want %d", n, len(testData))
	}

	// Close the connection - this should flush any buffered data
	err = conn.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for read to complete
	wg.Wait()

	if readErr != nil {
		t.Errorf("Client read error = %v", readErr)
	}

	// Verify data was received
	if !bytes.Equal(received.Bytes(), testData) {
		t.Errorf("Received data mismatch: got %q, want %q", received.Bytes(), testData)
	}

	// Verify connection state is closed
	if !conn.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}

	if conn.State() != ConnStateClosed {
		t.Errorf("State() = %v, want %v", conn.State(), ConnStateClosed)
	}
}

// TestCloseIsIdempotent verifies that calling Close() multiple times is safe.
func TestCloseIsIdempotent(t *testing.T) {
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

	// First close
	err := conn.Close()
	if err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should not panic or error
	err = conn.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v", err)
	}

	// Third close
	err = conn.Close()
	if err != nil {
		t.Errorf("Third Close() error = %v", err)
	}
}
