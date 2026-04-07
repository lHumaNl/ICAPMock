// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestGenerateRequestID_Basic tests request ID generation.
func TestGenerateRequestID_Basic(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
	id := GenerateRequestID(t1)

	if id == "" {
		t.Error("GenerateRequestID() returned empty string")
	}

	expected := "req-20240115-103000.123"
	if id != expected {
		t.Errorf("GenerateRequestID() = %v, want %v", id, expected)
	}
}

// =============================================================================
// Interface Segregation Principle (ISP) Compliance Tests
// =============================================================================

// TestRequestReader_ISPCompliance verifies that RequestReader interface
// contains only read and query operations.
func TestRequestReader_ISPCompliance(t *testing.T) {
	// Create a test storage instance
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

	// Verify FileStorage implements RequestReader
	var _ RequestReader = store

	// Create a test request
	sr := &StoredRequest{
		ID:             GenerateRequestID(time.Now()),
		Timestamp:      time.Now(),
		Method:         "REQMOD",
		URI:            "icap://localhost/reqmod",
		ClientIP:       "192.168.1.1",
		ResponseStatus: 204,
	}

	// Save a request (using writer interface)
	ctx := context.Background()
	if err := store.SaveRequest(ctx, sr); err != nil {
		t.Fatalf("SaveRequest() error = %v", err)
	}

	// Flush to ensure data is written before reading
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	// Test RequestReader operations
	t.Run("GetRequest", func(t *testing.T) {
		got, err := store.GetRequest(ctx, sr.ID)
		if err != nil {
			t.Errorf("GetRequest() error = %v", err)
		}
		if got == nil {
			t.Error("GetRequest() returned nil")
		} else if got.ID != sr.ID {
			t.Errorf("GetRequest() ID = %v, want %v", got.ID, sr.ID)
		}
	})

	t.Run("ListRequests", func(t *testing.T) {
		requests, err := store.ListRequests(ctx, RequestFilter{})
		if err != nil {
			t.Errorf("ListRequests() error = %v", err)
		}
		if len(requests) != 1 {
			t.Errorf("ListRequests() returned %d requests, want 1", len(requests))
		}
	})

	t.Run("DeleteRequest", func(t *testing.T) {
		// Create another request to delete
		sr2 := &StoredRequest{
			ID:             GenerateRequestID(time.Now()),
			Timestamp:      time.Now(),
			Method:         "RESPMOD",
			URI:            "icap://localhost/respmod",
			ResponseStatus: 204,
		}
		if err := store.SaveRequest(ctx, sr2); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}

		// Flush to ensure data is written
		if err := store.Flush(ctx); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}

		// Delete using RequestWriter interface
		if err := store.DeleteRequest(ctx, sr2.ID); err != nil {
			t.Errorf("DeleteRequest() error = %v", err)
		}

		// Verify deletion
		_, err := store.GetRequest(ctx, sr2.ID)
		if !errors.Is(err, ErrRequestNotFound) {
			t.Errorf("GetRequest() after delete should return ErrRequestNotFound, got %v", err)
		}
	})
}

// TestRequestWriter_ISPCompliance verifies that RequestWriter interface
// contains only write and lifecycle operations.
func TestRequestWriter_ISPCompliance(t *testing.T) {
	// Create a test storage instance
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

	// Verify FileStorage implements RequestWriter
	var _ RequestWriter = store

	ctx := context.Background()

	t.Run("SaveRequest", func(t *testing.T) {
		sr := &StoredRequest{
			ID:             GenerateRequestID(time.Now()),
			Timestamp:      time.Now(),
			Method:         "REQMOD",
			URI:            "icap://localhost/reqmod",
			ResponseStatus: 204,
		}
		if err := store.SaveRequest(ctx, sr); err != nil {
			t.Errorf("SaveRequest() error = %v", err)
		}
	})

	t.Run("Flush", func(t *testing.T) {
		if err := store.Flush(ctx); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}

		// Verify storage is closed
		sr := &StoredRequest{
			ID:             GenerateRequestID(time.Now()),
			Timestamp:      time.Now(),
			Method:         "REQMOD",
			URI:            "icap://localhost/reqmod",
			ResponseStatus: 204,
		}
		err := store.SaveRequest(ctx, sr)
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("SaveRequest() after close should return ErrStorageClosed, got %v", err)
		}
	})
}

// TestStorage_ISPCompliance verifies that Storage interface composes
// both RequestReader and RequestWriter.
func TestStorage_ISPCompliance(t *testing.T) {
	// Create a test storage instance
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

	// Verify FileStorage implements full Storage interface
	var _ Storage = store
}

// TestRequestReader_DeleteRequests tests the bulk delete functionality.
func TestRequestReader_DeleteRequests(t *testing.T) {
	// Create a test storage instance
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

	ctx := context.Background()

	// Create multiple requests
	for i := 0; i < 5; i++ {
		sr := &StoredRequest{
			ID:             GenerateRequestID(time.Now()),
			Timestamp:      time.Now(),
			Method:         "REQMOD",
			URI:            "icap://localhost/reqmod",
			ClientIP:       "192.168.1.1",
			ResponseStatus: 204,
		}
		if err := store.SaveRequest(ctx, sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure unique timestamps
	}

	// Verify we have 5 requests
	requests, err := store.ListRequests(ctx, RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}
	if len(requests) != 5 {
		t.Fatalf("ListRequests() returned %d requests, want 5", len(requests))
	}

	// Delete all REQMOD requests
	deleted, err := store.DeleteRequests(ctx, RequestFilter{Method: "REQMOD"})
	if err != nil {
		t.Fatalf("DeleteRequests() error = %v", err)
	}
	if deleted != 5 {
		t.Errorf("DeleteRequests() deleted %d requests, want 5", deleted)
	}

	// Verify all deleted
	requests, err = store.ListRequests(ctx, RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}
	if len(requests) != 0 {
		t.Errorf("ListRequests() returned %d requests after delete, want 0", len(requests))
	}
}

// TestRequestWriter_Clear tests the clear functionality.
func TestRequestWriter_Clear(t *testing.T) {
	// Create a test storage instance
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

	ctx := context.Background()

	// Create multiple requests
	for i := 0; i < 3; i++ {
		sr := &StoredRequest{
			ID:             GenerateRequestID(time.Now()),
			Timestamp:      time.Now(),
			Method:         "REQMOD",
			URI:            "icap://localhost/reqmod",
			ResponseStatus: 204,
		}
		if err := store.SaveRequest(ctx, sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Clear all requests
	cleared, err := store.Clear(ctx)
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if cleared != 3 {
		t.Errorf("Clear() cleared %d requests, want 3", cleared)
	}

	// Verify all cleared
	files, err := filepath.Glob(filepath.Join(tmpDir, "*.json*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Found %d files after clear, want 0", len(files))
	}
}

// TestRequestWriter_Flush tests the flush functionality.
func TestRequestWriter_Flush(t *testing.T) {
	// Create a test storage instance
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

	ctx := context.Background()

	// Create a request
	sr := &StoredRequest{
		ID:             GenerateRequestID(time.Now()),
		Timestamp:      time.Now(),
		Method:         "REQMOD",
		URI:            "icap://localhost/reqmod",
		ResponseStatus: 204,
	}
	if err := store.SaveRequest(ctx, sr); err != nil {
		t.Fatalf("SaveRequest() error = %v", err)
	}

	// Flush should succeed
	if err := store.Flush(ctx); err != nil {
		t.Errorf("Flush() error = %v", err)
	}

	// Verify file exists
	files, err := filepath.Glob(filepath.Join(tmpDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) == 0 {
		t.Error("No files found after flush")
	}
}

// TestDisabledStorage_ISPCompliance verifies ISP compliance for disabled storage.
func TestDisabledStorage_ISPCompliance(t *testing.T) {
	cfg := config.StorageConfig{
		Enabled:     false,
		RequestsDir: "",
	}

	store, err := NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}

	// Verify it still implements all interfaces
	var _ RequestReader = store
	var _ RequestWriter = store
	var _ Storage = store

	ctx := context.Background()

	// SaveRequest should return ErrStorageDisabled
	err = store.SaveRequest(ctx, &StoredRequest{ID: "test"})
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("SaveRequest() on disabled storage should return ErrStorageDisabled, got %v", err)
	}

	// GetRequest should return ErrStorageDisabled
	_, err = store.GetRequest(ctx, "test")
	if !errors.Is(err, ErrStorageDisabled) {
		t.Errorf("GetRequest() on disabled storage should return ErrStorageDisabled, got %v", err)
	}

	// Clear should return 0, nil
	cleared, err := store.Clear(ctx)
	if err != nil {
		t.Errorf("Clear() on disabled storage error = %v", err)
	}
	if cleared != 0 {
		t.Errorf("Clear() on disabled storage cleared = %d, want 0", cleared)
	}

	// Close should succeed
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestClosedStorage_Operations tests that closed storage returns proper errors.
func TestClosedStorage_Operations(t *testing.T) {
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

	// Close the storage
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ctx := context.Background()

	// All operations should return ErrStorageClosed
	t.Run("SaveRequest", func(t *testing.T) {
		err := store.SaveRequest(ctx, &StoredRequest{ID: "test"})
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("GetRequest", func(t *testing.T) {
		_, err := store.GetRequest(ctx, "test")
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("ListRequests", func(t *testing.T) {
		_, err := store.ListRequests(ctx, RequestFilter{})
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("DeleteRequest", func(t *testing.T) {
		err := store.DeleteRequest(ctx, "test")
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("DeleteRequests", func(t *testing.T) {
		_, err := store.DeleteRequests(ctx, RequestFilter{})
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		_, err := store.Clear(ctx)
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})

	t.Run("Flush", func(t *testing.T) {
		err := store.Flush(ctx)
		if !errors.Is(err, ErrStorageClosed) {
			t.Errorf("expected ErrStorageClosed, got %v", err)
		}
	})
}
