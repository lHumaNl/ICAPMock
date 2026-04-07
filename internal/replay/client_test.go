// Copyright 2026 ICAP Mock

package replay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestClientDoContextCancellation tests that the Do method properly responds
// to context cancellation without leaking goroutines.
func TestClientDoContextCancellation(t *testing.T) {
	// Create a client with a long timeout
	client := NewClient(30 * time.Second)

	// Use a non-routable IP address to ensure dial hangs
	// 198.51.100.1 is in TEST-NET-2 (documentation range) and won't respond
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := icap.NewRequest(icap.MethodOPTIONS, "icap://198.51.100.1:1344/options")
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Record goroutines before
	runtime.GC()
	time.Sleep(10 * time.Millisecond) // Let any pending cleanup complete
	initialGoroutines := runtime.NumGoroutine()

	// This should timeout quickly due to context
	start := time.Now()
	_, err = client.Do(ctx, "icap://198.51.100.1:1344/options", req)
	elapsed := time.Since(start)

	// Verify we got an error (context timeout or network timeout)
	if err == nil {
		t.Error("Expected error from canceled context, got nil")
	}

	// Verify it timed out within reasonable time (not the full 30s dial timeout)
	// This proves context cancellation was respected
	if elapsed > 500*time.Millisecond {
		t.Errorf("Dial took too long: %v (context should have canceled it)", elapsed)
	}

	// Wait for any cleanup
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Verify no goroutine leak
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+1 { // Allow 1 for test variance
		t.Errorf("Goroutine leak detected: before=%d, after=%d", initialGoroutines, finalGoroutines)
	}
}

// TestClientDialContextCancellation verifies that DialContext properly cancels
// the dial operation when context is canceled.
func TestClientDialContextCancellation(t *testing.T) {
	client := NewClient(30 * time.Second)

	// Create a context that we'll cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req, err := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost:1344/options")
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Dial should fail immediately due to canceled context
	start := time.Now()
	_, err = client.Do(ctx, "icap://localhost:1344/options", req)
	elapsed := time.Since(start)

	// Should fail with context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}

	// Should fail quickly (within 100ms), not wait for the 30s timeout
	if elapsed > 100*time.Millisecond {
		t.Errorf("Dial took too long with canceled context: %v", elapsed)
	}
}

// TestClientNoGoroutineLeakOnMultipleCancellations runs multiple concurrent
// dial attempts with canceled contexts to verify no goroutine leaks.
func TestClientNoGoroutineLeakOnMultipleCancellations(t *testing.T) {
	const numAttempts = 10

	client := NewClient(30 * time.Second)

	// Record goroutines before
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	var wg sync.WaitGroup
	wg.Add(numAttempts)

	for i := 0; i < numAttempts; i++ {
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://198.51.100.1:1344/options")
			_, _ = client.Do(ctx, "icap://198.51.100.1:1344/options", req)
		}()
	}

	wg.Wait()

	// Wait for cleanup
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Verify no goroutine leak
	finalGoroutines := runtime.NumGoroutine()
	leakedGoroutines := finalGoroutines - initialGoroutines

	if leakedGoroutines > 2 { // Allow some variance for test runtime
		t.Errorf("Goroutine leak detected: initial=%d, final=%d, leaked=%d",
			initialGoroutines, finalGoroutines, leakedGoroutines)
	}
}

// TestClientSuccessfulDial verifies that successful connections still work
// after the fix.
func TestClientSuccessfulDial(t *testing.T) {
	// Start a mock ICAP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	// Handle connections in goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleMockConnectionSimple(conn)
		}
	}()

	client := NewClient(5 * time.Second)
	ctx := context.Background()

	req, err := icap.NewRequest(icap.MethodOPTIONS, fmt.Sprintf("icap://%s/options", listener.Addr().String()))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, fmt.Sprintf("icap://%s/options", listener.Addr().String()), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestClientContextDeadlineRespected verifies that context deadlines are
// respected during dial.
func TestClientContextDeadlineRespected(t *testing.T) {
	client := NewClient(30 * time.Second)

	// Create context with a short deadline
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://198.51.100.1:1344/options")
	_, err := client.Do(ctx, "icap://198.51.100.1:1344/options", req)
	elapsed := time.Since(start)

	// Should get an error (timeout or network error)
	if err == nil {
		t.Error("Expected error, got nil")
	}

	// Verify it timed out close to our deadline (within 200ms tolerance)
	// This proves the context deadline was respected, not the 30s dial timeout
	if elapsed > 300*time.Millisecond {
		t.Errorf("Dial took too long: %v (expected ~100ms)", elapsed)
	}
}

// handleMockConnectionSimple handles a mock ICAP connection (helper for tests).
func handleMockConnectionSimple(conn net.Conn) {
	defer conn.Close()

	// Read request
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)

	// Send mock response
	response := "ICAP/1.0 200 OK\r\n" +
		"ISTag: \"test\"\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	_, _ = conn.Write([]byte(response))
}
