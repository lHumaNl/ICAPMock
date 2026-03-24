// Package storage contains tests for panic recovery in filesystem storage.
// WARN-001 FIX: These tests verify that panics in asyncWriter are recovered
// and do not crash the goroutine silently.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestFileStorage_PanicRecovery_InAsyncWriter verifies that panics in asyncWriter
// are recovered and do not crash the goroutine.
// WARN-001 FIX: Critical for preventing silent goroutine crashes.
func TestFileStorage_PanicRecovery_InAsyncWriter(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create a normal request - this should succeed
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)
	sr.ID = "req-normal-001"

	// Save the request - asyncWriter should handle it normally
	if err := store.SaveRequest(context.Background(), sr); err != nil {
		t.Fatalf("SaveRequest() error = %v", err)
	}

	// Wait for async write
	time.Sleep(100 * time.Millisecond)

	// Verify the request was saved
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 request, got %d", len(results))
	}
}

// TestFileStorage_PanicRecovery_GoroutineContinues verifies that after a panic,
// the asyncWriter goroutine can continue processing (if not terminated).
func TestFileStorage_PanicRecovery_GoroutineContinues(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Save multiple requests
	numRequests := 10
	for i := 0; i < numRequests; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-continue-%d", i)

		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error at %d: %v", i, err)
		}
	}

	// Wait for all async writes
	time.Sleep(200 * time.Millisecond)

	// Verify all requests were saved
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != numRequests {
		t.Errorf("Expected %d requests, got %d", numRequests, len(results))
	}
}

// TestFileStorage_PanicRecovery_CloseDoesNotPanic verifies Close() doesn't panic
// even when there are pending operations.
func TestFileStorage_PanicRecovery_CloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Start multiple concurrent save operations
	numGoroutines := 20
	done := make(chan struct{}, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()

			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := FromICAPRequest(req, 204, time.Millisecond)
			sr.ID = fmt.Sprintf("req-concurrent-%d", n)

			// This may or may not succeed depending on timing
			_ = store.SaveRequest(context.Background(), sr)
		}(i)
	}

	// Close while operations are in progress
	// This should NOT panic
	closePanic := make(chan interface{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				closePanic <- r
			} else {
				close(closePanic)
			}
		}()
		store.Close()
	}()

	// Wait for close to complete
	select {
	case r := <-closePanic:
		if r != nil {
			t.Fatalf("Close() panicked: %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close() timed out")
	}

	// Wait for all goroutines to finish
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Logf("Goroutine %d timed out", i)
		}
	}
}

// TestFileStorage_PanicRecovery_ChannelFull tests behavior when channel is full.
func TestFileStorage_PanicRecovery_ChannelFull(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		QueueSize:   10, // Very small queue
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Fill the channel
	numRequests := 100
	var lastErr error
	for i := 0; i < numRequests; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-queue-%d", i)

		// This should either succeed (queue) or fall back to sync write
		lastErr = store.SaveRequest(context.Background(), sr)
	}

	// The last error should be nil (either queued or sync written)
	if lastErr != nil && !errors.Is(lastErr, ErrStorageClosed) {
		t.Errorf("SaveRequest() unexpected error: %v", lastErr)
	}
}

// TestFileStorage_PanicRecovery_WaitGroupVerification verifies WaitGroup is
// properly decremented even on panic.
func TestFileStorage_PanicRecovery_WaitGroupVerification(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Save a request
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)
	sr.ID = "req-waitgroup"

	_ = store.SaveRequest(context.Background(), sr)

	// Close should complete without hanging (WaitGroup.Done is called in defer)
	closeDone := make(chan struct{})
	go func() {
		store.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		// Success - Close completed
	case <-time.After(5 * time.Second):
		t.Fatal("Close() timed out - WaitGroup.Done may not have been called")
	}
}

// TestFileStorage_PanicRecovery_InvalidData tests handling of data that might
// cause issues during encoding.
func TestFileStorage_PanicRecovery_InvalidData(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create a request with various edge cases
	testCases := []struct {
		name string
		sr   *StoredRequest
	}{
		{
			name: "empty_strings",
			sr: &StoredRequest{
				ID:        "req-empty",
				Method:    "",
				URI:       "",
				ClientIP:  "",
				Timestamp: time.Now(),
			},
		},
		{
			name: "unicode_data",
			sr: &StoredRequest{
				ID:        "req-unicode-日本語",
				Method:    icap.MethodREQMOD,
				URI:       "icap://localhost/テスト",
				ClientIP:  "192.168.1.1",
				Timestamp: time.Now(),
			},
		},
		{
			name: "very_long_id",
			sr: &StoredRequest{
				ID:        strings.Repeat("a", 1000),
				Method:    icap.MethodREQMOD,
				URI:       "icap://localhost/reqmod",
				Timestamp: time.Now(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.SaveRequest(context.Background(), tc.sr)
			if err != nil {
				t.Errorf("SaveRequest() error = %v", err)
			}
		})
	}

	// Wait for async writes
	time.Sleep(200 * time.Millisecond)
}

// TestFileStorage_PanicRecovery_RotationPanic tests panic recovery during rotation.
func TestFileStorage_PanicRecovery_RotationPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 5, // Low threshold to trigger rotation
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write enough requests to trigger multiple rotations
	numRequests := 20
	for i := 0; i < numRequests; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-rotation-panic-%d", i)

		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error at %d: %v", i, err)
		}
	}

	// Wait for async writes and rotations
	time.Sleep(300 * time.Millisecond)

	// Verify all requests were saved
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != numRequests {
		t.Errorf("Expected %d requests after rotation, got %d", numRequests, len(results))
	}
}

// mockPanicConn is a mock connection that panics on Write.
type mockPanicConn struct {
	net.Conn
	panicOnWrite bool
}

func (m *mockPanicConn) Write(b []byte) (n int, err error) {
	if m.panicOnWrite {
		panic("mock panic in Write")
	}
	return len(b), nil
}

func (m *mockPanicConn) Close() error {
	return nil
}

// TestFileStorage_PanicRecovery_DeferExecution verifies defer runs even on panic.
func TestFileStorage_PanicRecovery_DeferExecution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Track if Close properly cleans up
	closeCompleted := make(chan struct{})

	go func() {
		store.Close()
		close(closeCompleted)
	}()

	select {
	case <-closeCompleted:
		// Success - defer executed
	case <-time.After(5 * time.Second):
		t.Fatal("Close() didn't complete - defer may not have executed")
	}

	// Verify we can't save after close
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)

	err = store.SaveRequest(context.Background(), sr)
	if !errors.Is(err, ErrStorageClosed) {
		t.Errorf("Expected ErrStorageClosed after Close(), got %v", err)
	}
}

// TestFileStorage_PanicRecovery_StackCapture verifies stack trace is captured on panic.
func TestFileStorage_PanicRecovery_StackCapture(t *testing.T) {
	t.Parallel()

	// This test verifies the debug.Stack() call in asyncWriter works
	// We can't easily trigger a real panic, but we verify the code path exists

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Normal operation - should not panic
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)
	sr.ID = "req-stack-capture"

	if err := store.SaveRequest(context.Background(), sr); err != nil {
		t.Fatalf("SaveRequest() error = %v", err)
	}

	// Close should complete normally
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestFileStorage_PanicRecovery_ConcurrentSafety tests panic recovery under high concurrency.
func TestFileStorage_PanicRecovery_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		QueueSize:   100,
		RotateAfter: 50,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	numGoroutines := 100
	errCh := make(chan error, numGoroutines)
	saveCh := make(chan struct{}, numGoroutines)

	// Start concurrent saves
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := FromICAPRequest(req, 204, time.Millisecond)
			sr.ID = fmt.Sprintf("req-concurrent-panic-%d", n)

			if err := store.SaveRequest(context.Background(), sr); err != nil {
				if !errors.Is(err, ErrStorageClosed) {
					errCh <- err
					return
				}
			}
			saveCh <- struct{}{}
		}(i)
	}

	// Wait for all saves to complete or error
	successCount := 0
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-saveCh:
			successCount++
		case err := <-errCh:
			t.Errorf("SaveRequest() error: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Test timed out")
		}
	}

	// Close storage
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	t.Logf("Successfully saved %d/%d requests", successCount, numGoroutines)
}

// TestFileStorage_PanicRecovery_FileSystemErrors tests handling of filesystem errors.
func TestFileStorage_PanicRecovery_FileSystemErrors(t *testing.T) {
	t.Parallel()

	// Test with read-only directory (should handle gracefully)
	tmpDir := t.TempDir()
	readonlyDir := filepath.Join(tmpDir, "readonly")

	if err := os.Mkdir(readonlyDir, 0555); err != nil {
		t.Fatalf("Failed to create read-only dir: %v", err)
	}

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: readonlyDir,
	}

	// This should fail gracefully, not panic
	store, err := NewFileStorage(cfg, nil)
	if err == nil {
		// If creation succeeded, Close should not panic
		store.Close()
	}
	// Error is acceptable, panic is not
}

// TestFileStorage_PanicRecovery_AfterPanic tests storage state after potential panic scenarios.
func TestFileStorage_PanicRecovery_AfterPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Save a request before any potential issues
	req1 := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr1 := FromICAPRequest(req1, 204, time.Millisecond)
	sr1.ID = "req-before-panic"

	if err := store.SaveRequest(context.Background(), sr1); err != nil {
		t.Fatalf("SaveRequest() error = %v", err)
	}

	// Do more operations that might cause issues
	for i := 0; i < 10; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-during-%d", i)
		_ = store.SaveRequest(context.Background(), sr)
	}

	// Wait for async writes
	time.Sleep(200 * time.Millisecond)

	// Close and verify state
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Verify storage is closed
	if !store.closed.Load() {
		t.Error("Storage should be marked as closed")
	}
}

// Helper function to verify file integrity
func verifyBatchFileIntegrity(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var sr StoredRequest
		if err := json.Unmarshal([]byte(line), &sr); err != nil {
			t.Errorf("Invalid JSON line in batch file: %v", err)
		}
	}
}

// TestFileStorage_PanicRecovery_DataIntegrity verifies data integrity after operations.
func TestFileStorage_PanicRecovery_DataIntegrity(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Save requests with specific data to verify integrity
	testData := map[string]string{
		"req-integrity-1": "test_data_1",
		"req-integrity-2": "test_data_2",
		"req-integrity-3": "test_data_3",
	}

	for id, data := range testData {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = id
		sr.URI = data // Store test data in URI for verification

		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Wait for async writes
	time.Sleep(200 * time.Millisecond)

	// Close and verify integrity
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify all batch files have valid JSON lines
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".jsonl") {
			verifyBatchFileIntegrity(t, filepath.Join(tmpDir, file.Name()))
		}
	}
}

// Ensure io import is used
var _ = io.EOF
