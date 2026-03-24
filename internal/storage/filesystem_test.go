// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestFileStorage_SaveRequest tests saving requests to file storage.
func TestFileStorage_SaveRequest(t *testing.T) {
	// Create temp directory
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

	// Create a sample request
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost:1344/reqmod",
		Header: icap.NewHeader(),
	}
	req.Header.Set("Host", "localhost:1344")
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "GET",
		URI:    "http://example.com/test",
		Proto:  "HTTP/1.1",
		Header: icap.NewHeader(),
	}

	sr := FromICAPRequest(req, 204, 5*time.Millisecond)
	sr.ID = "req-20240115-001"
	sr.Timestamp = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// Save the request
	err = store.SaveRequest(context.Background(), sr)
	if err != nil {
		t.Errorf("SaveRequest() error = %v", err)
	}

	// Wait for async write
	time.Sleep(100 * time.Millisecond)

	// Verify file was created
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	if len(files) == 0 {
		t.Error("Expected at least one file to be created")
	}
}

// TestFileStorage_GetRequest tests retrieving requests by ID.
func TestFileStorage_GetRequest(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create and save a request
	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost:1344/reqmod",
		Header:   icap.NewHeader(),
		ClientIP: "192.168.1.1",
	}
	req.Header.Set("Host", "localhost:1344")

	sr := FromICAPRequest(req, 204, 5*time.Millisecond)
	sr.ID = "req-20240115-001"
	sr.Timestamp = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// Write directly to a file with the expected name
	filename := filepath.Join(tmpDir, "2024-01-15_001.json")
	data, _ := serializeStoredRequest(sr)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Retrieve the request
	retrieved, err := store.GetRequest(context.Background(), sr.ID)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if retrieved.ID != sr.ID {
		t.Errorf("GetRequest() ID = %v, want %v", retrieved.ID, sr.ID)
	}
	if retrieved.Method != sr.Method {
		t.Errorf("GetRequest() Method = %v, want %v", retrieved.Method, sr.Method)
	}
	if retrieved.ClientIP != sr.ClientIP {
		t.Errorf("GetRequest() ClientIP = %v, want %v", retrieved.ClientIP, sr.ClientIP)
	}
}

// TestFileStorage_GetRequest_NotFound tests retrieving non-existent request.
func TestFileStorage_GetRequest_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	_, err = store.GetRequest(context.Background(), "req-99999999-999")
	if !errors.Is(err, ErrRequestNotFound) {
		t.Errorf("GetRequest() error = %v, want %v", err, ErrRequestNotFound)
	}
}

// TestFileStorage_ListRequests tests listing requests with filters.
func TestFileStorage_ListRequests(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create multiple requests
	now := time.Now()
	requests := []*StoredRequest{
		{
			ID:        "req-20240115-001",
			Timestamp: now.Add(-2 * time.Hour),
			Method:    icap.MethodREQMOD,
			URI:       "icap://localhost/reqmod",
			ClientIP:  "192.168.1.1",
		},
		{
			ID:        "req-20240115-002",
			Timestamp: now.Add(-1 * time.Hour),
			Method:    icap.MethodRESPMOD,
			URI:       "icap://localhost/respmod",
			ClientIP:  "192.168.1.2",
		},
		{
			ID:        "req-20240115-003",
			Timestamp: now,
			Method:    icap.MethodREQMOD,
			URI:       "icap://localhost/reqmod",
			ClientIP:  "192.168.1.1",
		},
	}

	// Write all requests
	for i, sr := range requests {
		filename := filepath.Join(tmpDir, fmt.Sprintf("2024-01-15_%03d.json", i+1))
		data, _ := serializeStoredRequest(sr)
		if err := os.WriteFile(filename, data, 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	tests := []struct {
		name    string
		filter  RequestFilter
		wantLen int
	}{
		{
			name:    "all requests",
			filter:  RequestFilter{},
			wantLen: 3,
		},
		{
			name: "filter by method",
			filter: RequestFilter{
				Method: icap.MethodREQMOD,
			},
			wantLen: 2,
		},
		{
			name: "filter by client IP",
			filter: RequestFilter{
				ClientIP: "192.168.1.1",
			},
			wantLen: 2,
		},
		{
			name: "with limit",
			filter: RequestFilter{
				Limit: 1,
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.ListRequests(context.Background(), tt.filter)
			if err != nil {
				t.Fatalf("ListRequests() error = %v", err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("ListRequests() got %d results, want %d", len(results), tt.wantLen)
			}
		})
	}
}

// TestFileStorage_DeleteRequest tests deleting requests.
func TestFileStorage_DeleteRequest(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create and save a request
	sr := &StoredRequest{
		ID:        "req-20240115-001",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Method:    icap.MethodREQMOD,
		URI:       "icap://localhost/reqmod",
	}

	filename := filepath.Join(tmpDir, "2024-01-15_001.json")
	data, _ := serializeStoredRequest(sr)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Delete the request
	err = store.DeleteRequest(context.Background(), sr.ID)
	if err != nil {
		t.Fatalf("DeleteRequest() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted")
	}
}

// TestFileStorage_Disabled tests storage when disabled.
func TestFileStorage_Disabled(t *testing.T) {
	cfg := config.StorageConfig{
		Enabled: false,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)

	err = store.SaveRequest(context.Background(), sr)
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("SaveRequest() error = %v, want %v", err, ErrStorageDisabled)
	}

	_, err = store.GetRequest(context.Background(), "test")
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("GetRequest() error = %v, want %v", err, ErrStorageDisabled)
	}

	_, err = store.ListRequests(context.Background(), RequestFilter{})
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("ListRequests() error = %v, want %v", err, ErrStorageDisabled)
	}

	err = store.DeleteRequest(context.Background(), "test")
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("DeleteRequest() error = %v, want %v", err, ErrStorageDisabled)
	}
}

// TestFileStorage_Close tests closing storage.
func TestFileStorage_Close(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Close once
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Close again (should be idempotent)
	if err := store.Close(); err != nil {
		t.Errorf("Close() second call error = %v", err)
	}

	// Operations after close should fail
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)

	err = store.SaveRequest(context.Background(), sr)
	if !errors.Is(err, ErrStorageClosed) {
		t.Errorf("SaveRequest() after close error = %v, want %v", err, ErrStorageClosed)
	}
}

// TestFileStorage_ThreadSafety tests concurrent access.
func TestFileStorage_ThreadSafety(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := FromICAPRequest(req, 204, time.Millisecond)
			sr.ID = fmt.Sprintf("req-concurrent-%d", n)
			_ = store.SaveRequest(context.Background(), sr)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestFromICAPRequest tests conversion from icap.Request to StoredRequest.
func TestFromICAPRequest(t *testing.T) {
	req := &icap.Request{
		Method:     icap.MethodREQMOD,
		URI:        "icap://localhost:1344/reqmod",
		Header:     icap.NewHeader(),
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}
	req.Header.Set("Host", "localhost:1344")
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "GET",
		URI:    "http://example.com/test",
		Proto:  "HTTP/1.1",
		Header: icap.NewHeader(),
		Body:   []byte("test body"),
	}
	req.HTTPRequest.Header.Set("User-Agent", "test")

	sr := FromICAPRequest(req, 204, 10*time.Millisecond)

	if sr.Method != icap.MethodREQMOD {
		t.Errorf("Method = %v, want %v", sr.Method, icap.MethodREQMOD)
	}
	if sr.URI != "icap://localhost:1344/reqmod" {
		t.Errorf("URI = %v, want icap://localhost:1344/reqmod", sr.URI)
	}
	if sr.ClientIP != "192.168.1.1" {
		t.Errorf("ClientIP = %v, want 192.168.1.1", sr.ClientIP)
	}
	if sr.ProcessingTimeMs != 10 {
		t.Errorf("ProcessingTimeMs = %v, want 10", sr.ProcessingTimeMs)
	}
	if sr.ResponseStatus != 204 {
		t.Errorf("ResponseStatus = %v, want 204", sr.ResponseStatus)
	}
	if sr.HTTPRequest == nil {
		t.Fatal("HTTPRequest should not be nil")
	}
	if sr.HTTPRequest.Method != "GET" {
		t.Errorf("HTTPRequest.Method = %v, want GET", sr.HTTPRequest.Method)
	}
}

// TestFileStorageCloseRaceCondition tests concurrent Close() and SaveRequest()
// to ensure no "send on closed channel" panic occurs.
// Run with: go test -race -v ./internal/storage/
func TestFileStorageCloseRaceCondition(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Create a channel to signal when to start closing
	startClose := make(chan struct{})
	// Track errors from SaveRequest goroutines
	errCh := make(chan error, 100)

	// Start multiple SaveRequest goroutines
	numGoroutines := 50
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			// Wait for signal
			<-startClose

			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := FromICAPRequest(req, 204, time.Millisecond)
			sr.ID = fmt.Sprintf("req-race-%d", n)

			// This should either succeed or return ErrStorageClosed,
			// but NEVER panic with "send on closed channel"
			if err := store.SaveRequest(context.Background(), sr); err != nil {
				if !errors.Is(err, ErrStorageClosed) && !errors.Is(err, ErrStorageDisabled) {
					errCh <- fmt.Errorf("SaveRequest() unexpected error: %w", err)
					return
				}
			}
			errCh <- nil
		}(i)
	}

	// Signal all goroutines to start and immediately close
	close(startClose)

	// Small delay to increase chance of race
	time.Sleep(time.Microsecond)

	// Close the storage - this should NOT panic
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

// TestFileStorageDoubleClose tests that calling Close() twice is safe.
func TestFileStorageDoubleClose(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// First close
	if err := store.Close(); err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should not panic and should return nil
	if err := store.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}

	// Third close for good measure
	if err := store.Close(); err != nil {
		t.Errorf("Third Close() error = %v", err)
	}
}

// TestFileStorageConcurrentClose tests concurrent Close() calls.
func TestFileStorageConcurrentClose(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Start multiple Close() goroutines
	numClosers := 10
	errCh := make(chan error, numClosers)

	for i := 0; i < numClosers; i++ {
		go func() {
			if err := store.Close(); err != nil {
				errCh <- fmt.Errorf("Close() error: %w", err)
				return
			}
			errCh <- nil
		}()
	}

	// Collect results
	for i := 0; i < numClosers; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

// TestFileStorageOperationsAfterClose tests all operations after Close().
func TestFileStorageOperationsAfterClose(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Close the storage
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ctx := context.Background()

	// Test SaveRequest
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	sr := FromICAPRequest(req, 204, time.Millisecond)

	if err := store.SaveRequest(ctx, sr); !errors.Is(err, ErrStorageClosed) {
		t.Errorf("SaveRequest() error = %v, want %v", err, ErrStorageClosed)
	}

	// Test GetRequest
	if _, err := store.GetRequest(ctx, "req-20240101-001"); !errors.Is(err, ErrStorageClosed) {
		t.Errorf("GetRequest() error = %v, want %v", err, ErrStorageClosed)
	}

	// Test ListRequests
	if _, err := store.ListRequests(ctx, RequestFilter{}); !errors.Is(err, ErrStorageClosed) {
		t.Errorf("ListRequests() error = %v, want %v", err, ErrStorageClosed)
	}

	// Test DeleteRequest
	if err := store.DeleteRequest(ctx, "req-20240101-001"); !errors.Is(err, ErrStorageClosed) {
		t.Errorf("DeleteRequest() error = %v, want %v", err, ErrStorageClosed)
	}
}

// Helper function to serialize StoredRequest to JSON
func serializeStoredRequest(sr *StoredRequest) ([]byte, error) {
	return json.MarshalIndent(sr, "", "  ")
}

// TestStorageRotation verifies that file rotation happens after RotateAfter requests.
// CRIT-008: Tests the rotation functionality.
func TestStorageRotation(t *testing.T) {
	tmpDir := t.TempDir()

	// Set low rotation threshold for testing
	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 5, // Rotate after 5 requests
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write 12 requests (should cause 2 rotations: 0->1 at request 5, 1->2 at request 10)
	for i := 0; i < 12; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-rotation-%d", i)
		sr.Timestamp = time.Now().Add(time.Duration(i) * time.Second)

		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Flush to ensure all async writes complete
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	// Check that multiple batch files were created
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	batchFiles := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFiles++
		}
	}

	// Should have 3 batch files (000000, 000001, 000002)
	if batchFiles < 3 {
		t.Errorf("Expected at least 3 batch files after rotation, got %d", batchFiles)
	}

	// Verify all requests can be read back
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != 12 {
		t.Errorf("ListRequests() returned %d requests, want 12", len(results))
	}
}

// TestStorageRotationDisabled verifies no rotation when RotateAfter = 0.
// CRIT-008: Tests that rotation can be disabled.
func TestStorageRotationDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 0, // Disable rotation
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write many requests
	for i := 0; i < 50; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := FromICAPRequest(req, 204, time.Millisecond)
		sr.ID = fmt.Sprintf("req-no-rotation-%d", i)

		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Flush to ensure all async writes complete
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	// Check that only one batch file exists (no rotation)
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	batchFiles := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFiles++
		}
	}

	if batchFiles != 1 {
		t.Errorf("Expected 1 batch file when rotation disabled, got %d", batchFiles)
	}
}

// TestStorageRotationDuringWrite verifies rotation doesn't lose data.
// CRIT-008: Tests data integrity during concurrent rotation.
func TestStorageRotationDuringWrite(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 3, // Very low threshold to trigger rotation frequently
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write requests concurrently
	numRequests := 100
	errCh := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(n int) {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := FromICAPRequest(req, 204, time.Millisecond)
			sr.ID = fmt.Sprintf("req-concurrent-%d", n)
			sr.Timestamp = time.Now()

			errCh <- store.SaveRequest(context.Background(), sr)
		}(i)
	}

	// Collect errors
	for i := 0; i < numRequests; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("SaveRequest() error = %v", err)
		}
	}

	// Flush to ensure all async writes complete
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	// Verify all requests were saved
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != numRequests {
		t.Errorf("ListRequests() returned %d requests, want %d (rotation lost data)", len(results), numRequests)
	}

	// Verify all IDs are present
	idSet := make(map[string]bool)
	for _, sr := range results {
		idSet[sr.ID] = true
	}

	for i := 0; i < numRequests; i++ {
		id := fmt.Sprintf("req-concurrent-%d", i)
		if !idSet[id] {
			t.Errorf("Missing request with ID %s (data lost during rotation)", id)
		}
	}
}

// TestStorageBackwardCompatibility verifies reading old format files.
func TestStorageBackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create an old-format file manually
	sr := &StoredRequest{
		ID:        "req-20240115-001",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Method:    icap.MethodREQMOD,
		URI:       "icap://localhost/reqmod",
		ClientIP:  "192.168.1.1",
	}

	filename := filepath.Join(tmpDir, "2024-01-15_001.json")
	data, _ := serializeStoredRequest(sr)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Read using the new storage implementation
	retrieved, err := store.GetRequest(context.Background(), sr.ID)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if retrieved.ID != sr.ID {
		t.Errorf("GetRequest() ID = %v, want %v", retrieved.ID, sr.ID)
	}
	if retrieved.Method != sr.Method {
		t.Errorf("GetRequest() Method = %v, want %v", retrieved.Method, sr.Method)
	}

	// Also test ListRequests
	results, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("ListRequests() returned %d results, want 1", len(results))
	}
}
