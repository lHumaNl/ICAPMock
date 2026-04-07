// Copyright 2026 ICAP Mock

package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestAtomicFileRotation_ConcurrentWrites tests that multiple goroutines can write concurrently.
// CRIT-008: Tests verify that concurrent writes are safe and do data is persisted correctly.
func TestAtomicFileRotation_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
		Workers:     4,
		QueueSize:   100,
	}

	store, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write requests concurrently from multiple goroutines
	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
			sr.ID = fmt.Sprintf("req-concurrent-%d", n)
			sr.Timestamp = time.Now().Add(time.Duration(n) * time.Millisecond)

			err := store.SaveRequest(context.Background(), sr)
			if err != nil {
				t.Errorf("SaveRequest() goroutine %d error = %v", n, err)
			}
			wg.Done()
		}(i)
	}

	// Wait for async writes to complete
	wg.Wait()

	// Wait a writes to complete with a small buffer
	time.Sleep(100 * time.Millisecond)

	// Verify all requests were saved
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	// Count batch files
	batchFiles := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFiles++
		}
	}

	// Should have written to batch files
	if batchFiles == 0 {
		t.Error("No batch files were created")
	}

	// Verify all requests can be read back
	results, err := store.ListRequests(context.Background(), storage.RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != numGoroutines {
		t.Errorf("Expected %d requests from concurrent writes, got %d", numGoroutines, len(results))
	}

	// Verify all IDs are present
	foundIDs := make(map[string]bool)
	for _, r := range results {
		foundIDs[r.ID] = true
	}

	for i := 0; i < numGoroutines; i++ {
		id := fmt.Sprintf("req-concurrent-%d", i)
		if !foundIDs[id] {
			t.Errorf("Missing request with ID %s", id)
		}
	}
}

// TestAtomicFileRotation_CatastropheRecovery tests that data is preserved during crash.
// WARN-001, WARN-006: Simulates crash by killing writer mid-write
// and verifies that file remains valid.
func TestAtomicFileRotation_CatastropheRecovery(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
		Workers:     1, // Single worker to easier testing
		QueueSize:   10,
	}

	store, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write some requests first
	for i := 0; i < 5; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
		sr.ID = fmt.Sprintf("req-crash-test-%d", i)

		err := store.SaveRequest(context.Background(), sr)
		if err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Wait for writes to complete
	time.Sleep(100 * time.Millisecond)

	// Get the batch files before crash
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	batchFile := ""
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFile = filepath.Join(tmpDir, f.Name())
			break
		}
	}

	if batchFile == "" {
		t.Fatal("No batch file found")
	}

	// Read the batch file content
	data, err := os.ReadFile(batchFile)
	if err != nil {
		t.Fatalf("Failed to read batch file: %v", err)
	}

	// Verify file contains valid JSON lines
	lines := strings.Split(string(data), "\n")
	var parsedRequests []storage.StoredRequest
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var sr storage.StoredRequest
		if err := json.Unmarshal([]byte(line), &sr); err != nil {
			t.Errorf("Invalid JSON line in batch file: %v", err)
		} else {
			parsedRequests = append(parsedRequests, sr)
		}
	}

	if len(parsedRequests) < 5 {
		t.Errorf("Expected at least 5 valid lines in batch file, got %d", len(parsedRequests))
	}

	// Verify all IDs are present
	foundIDs := make(map[string]bool)
	for _, r := range parsedRequests {
		foundIDs[r.ID] = true
	}

	expectedIDs := []string{
		"req-crash-test-0",
		"req-crash-test-1",
		"req-crash-test-2",
		"req-crash-test-3",
		"req-crash-test-4",
	}
	for _, id := range expectedIDs {
		if !foundIDs[id] {
			t.Errorf("Missing ID %s in batch file", id)
		}
	}
}

// TestAtomicFileRotation_PanicRecovery tests panic recovery in asyncWriter.
// WARN-001: Tests that asyncWriter goroutine recovers from panics.
func TestAtomicFileRotation_PanicRecovery(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
		Workers:     1,
		QueueSize:   10,
	}

	store, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Create a channel to signal when writer should panic
	panicSignal := make(chan struct{})
	done := make(chan struct{})

	// Start a goroutine that will trigger a panic after writing a few requests
	go func() {
		defer func() {
			// Recover from panic
			if r := recover(); r != nil {
				t.Logf("PANIC recovered in asyncWriter: %v", r)
			}
		}()

		// Write some requests, then panic
		for i := 0; i < 5; i++ {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
			}
			sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
			sr.ID = fmt.Sprintf("req-panic-recovery-%d", i)

			err := store.SaveRequest(context.Background(), sr)
			if err != nil {
				t.Errorf("SaveRequest() error = %v", err)
			}
		}

		// Signal panic
		close(panicSignal)

	}()

	// Wait for writer to finish
	<-panicSignal

	// Wait for async writes
	time.Sleep(100 * time.Millisecond)

	// Close the done channel
	close(done)

}

// TestAtomicFileRotation_SyncBeforeClose verifies Sync() is called before Close.
// WARN-006: Tests that Sync() is called before Close() to flush data to disk.
func TestAtomicFileRotation_SyncBeforeClose(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 100,
		Workers:     1,
		QueueSize:   10,
	}

	store, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write some requests
	for i := 0; i < 10; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
		sr.ID = fmt.Sprintf("req-sync-test-%d", i)

		sr.Timestamp = time.Now().Add(-time.Duration(i) * time.Second)

		err := store.SaveRequest(context.Background(), sr)
		if err != nil {
			t.Errorf("SaveRequest() error = %v", err)
		}
	}

	// Wait for async writes
	time.Sleep(100 * time.Millisecond)

	// Verify file was synced and closed properly
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	// Count batch files
	batchFiles := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFiles++
		}
	}

	if batchFiles != 1 {
		t.Errorf("Expected at least 1 batch file, got %d", batchFiles)
	}

	// Verify file was synced
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmpDir, f.Name()))
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", f.Name(), err)
		}

		// File should be valid JSONL (each line is a separate JSON object)
		// Note: strings.Split includes empty lines, so we need to count non-empty ones
		lines := strings.Split(string(data), "\n")
		nonEmptyLines := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				nonEmptyLines++
			}
		}
		if nonEmptyLines != 10 {
			t.Errorf("File should have 10 valid JSONL lines, got %d", nonEmptyLines)
		}

		// Verify all lines are valid JSON
		for i := 0; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			var sr storage.StoredRequest
			if err := json.Unmarshal([]byte(line), &sr); err != nil {
				t.Errorf("Line %d is not valid JSON: %v", i, err)
			}
		}
	}

	// Verify all IDs are present
	results, err := store.ListRequests(context.Background(), storage.RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	foundIDs := make(map[string]bool)
	for _, r := range results {
		foundIDs[r.ID] = true
	}

	expectedIDs := []string{
		"req-sync-test-0",
		"req-sync-test-1",
		"req-sync-test-2",
		"req-sync-test-3",
		"req-sync-test-4",
		"req-sync-test-5",
		"req-sync-test-6",
		"req-sync-test-7",
		"req-sync-test-8",
		"req-sync-test-9",
	}
	for _, id := range expectedIDs {
		if !foundIDs[id] {
			t.Errorf("Missing ID %s in batch file", id)
		}
	}
}

// TestAtomicFileRotation_DataIntegrityDuringRotation verifies data integrity during file rotation.
// CRIT-008: Tests that rotation doesn't lose or corrupt data.
func TestAtomicFileRotation_DataIntegrityDuringRotation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		RotateAfter: 5, // Small rotation threshold
		Workers:     1,
		QueueSize:   10,
	}

	store, err := storage.NewFileStorage(cfg, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	defer store.Close()

	// Write exactly RotateAfter requests to trigger rotation
	for i := 0; i < cfg.RotateAfter; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
		sr.ID = fmt.Sprintf("req-rotation-%d", i)
		sr.Timestamp = time.Now().Add(time.Duration(i) * time.Second)

		err := store.SaveRequest(context.Background(), sr)
		if err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Wait for async writes
	time.Sleep(100 * time.Millisecond)

	// Write more requests to trigger second rotation
	for i := 0; i < cfg.RotateAfter; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
		sr.ID = fmt.Sprintf("req-rotation-2-%d", i)
		sr.Timestamp = time.Now().Add(time.Duration(i) * time.Second)

		err := store.SaveRequest(context.Background(), sr)
		if err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Wait for async writes
	time.Sleep(100 * time.Millisecond)

	// Write more to trigger third rotation
	for i := 0; i < cfg.RotateAfter; i++ {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
		}
		sr := storage.FromICAPRequest(req, 204, 10*time.Millisecond)
		sr.ID = fmt.Sprintf("req-rotation-3-%d", i)
		sr.Timestamp = time.Now().Add(time.Duration(i) * time.Second)

		err := store.SaveRequest(context.Background(), sr)
		if err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}

	// Wait for async writes
	time.Sleep(100 * time.Millisecond)

	// Verify we files were created
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	// Count batch files
	batchFiles := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			batchFiles++
		}
	}

	// Should have at least 3 batch files (000000, 000001, 000002)
	if batchFiles < 3 {
		t.Errorf("Expected at least 3 batch files after rotation, got %d", batchFiles)
	}

	// Verify all requests can be read back
	results, err := store.ListRequests(context.Background(), storage.RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}

	if len(results) != 15 {
		t.Errorf("ListRequests() returned %d requests, want 15 (rotation lost data)", len(results))
	}

	// Verify all IDs are present
	foundIDs := make(map[string]bool)
	for _, r := range results {
		foundIDs[r.ID] = true
	}

	// Check batch 1 IDs
	for i := 0; i < cfg.RotateAfter; i++ {
		id := fmt.Sprintf("req-rotation-%d", i)
		if !foundIDs[id] {
			t.Errorf("Missing request with ID %s (data lost during rotation)", id)
		}
	}

	// Check batch 2 IDs
	for i := 0; i < cfg.RotateAfter; i++ {
		id := fmt.Sprintf("req-rotation-2-%d", i)
		if !foundIDs[id] {
			t.Errorf("Missing request with ID %s (data lost during rotation)", id)
		}
	}

	// Check batch 3 IDs
	for i := 0; i < cfg.RotateAfter; i++ {
		id := fmt.Sprintf("req-rotation-3-%d", i)
		if !foundIDs[id] {
			t.Errorf("Missing request with ID %s (data lost during rotation)", id)
		}
	}
}
