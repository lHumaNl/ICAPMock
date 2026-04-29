// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

type storedBatchFile struct {
	name     string
	size     int64
	requests []*StoredRequest
}

func TestFileStorage_MaxFileSizeRotatesBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	first := newSizeRotationRequest("req-size-001")
	second := newSizeRotationRequest("req-size-002")
	maxSize := encodedLineSize(t, first) + encodedLineSize(t, second) - 1
	store := newSizeRotationStore(t, dir, maxSize)
	defer store.Close()

	saveAndFlushRequests(t, store, first, second)

	files := readNonEmptyBatchFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("non-empty batch files = %d, want 2", len(files))
	}
	assertFilesWithinMaxSize(t, files, maxSize)
}

func TestFileStorage_MaxFileSizeOversizedRecordIsDropped(t *testing.T) {
	dir := t.TempDir()
	small := newSizeRotationRequest("req-large-001")
	maxSize := encodedLineSize(t, small) + 64
	large := newOversizedRequest("req-large-002", maxSize)
	trailing := newSizeRotationRequest("req-large-003")
	store := newSizeRotationStore(t, dir, maxSize)
	defer store.Close()

	saveAndFlushRequests(t, store, small, large, trailing)

	files := readNonEmptyBatchFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("non-empty batch files = %d, want 2", len(files))
	}
	assertFilesWithinMaxSize(t, readAllBatchFiles(t, dir), maxSize)
	assertRequestNotPersisted(t, store, large.ID)
	assertStoredRequestCount(t, store, 2)
}

func TestFileStorage_MaxFileSizeAsyncFlushSeesRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	records := newAsyncRotationRequests(4)
	maxSize := encodedLineSize(t, records[0]) + encodedLineSize(t, records[1]) - 1
	store := newSizeRotationStore(t, dir, maxSize)
	defer store.Close()

	saveAndFlushRequests(t, store, records...)

	files := readNonEmptyBatchFiles(t, dir)
	if len(files) != len(records) {
		t.Fatalf("non-empty batch files = %d, want %d", len(files), len(records))
	}
	assertStoredRequestCount(t, store, len(records))
}

func newSizeRotationStore(t *testing.T, dir string, maxSize int64) *FileStorage {
	t.Helper()
	store, err := NewFileStorage(config.StorageConfig{
		Enabled:     true,
		RequestsDir: dir,
		MaxFileSize: maxSize,
		RotateAfter: 1000,
	}, nil)
	if err != nil {
		t.Fatalf("NewFileStorage() error = %v", err)
	}
	return store
}

func newSizeRotationRequest(id string) *StoredRequest {
	return &StoredRequest{
		ID:             id,
		Timestamp:      time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		Method:         "REQMOD",
		URI:            "icap://localhost/reqmod",
		ClientIP:       "192.0.2.10",
		ResponseStatus: 204,
	}
}

func newOversizedRequest(id string, maxSize int64) *StoredRequest {
	sr := newSizeRotationRequest(id)
	sr.HTTPRequest = &HTTPMessageRecord{
		Method: "POST",
		URI:    "http://example.com/upload",
		Proto:  "HTTP/1.1",
		Body:   strings.Repeat("x", int(maxSize*2)),
	}
	return sr
}

func newAsyncRotationRequests(count int) []*StoredRequest {
	records := make([]*StoredRequest, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, newSizeRotationRequest(fmt.Sprintf("req-async-%03d", i)))
	}
	return records
}

func encodedLineSize(t *testing.T, sr *StoredRequest) int64 {
	t.Helper()
	data, err := encodeStoredRequestLine(sr)
	if err != nil {
		t.Fatalf("encodeStoredRequestLine() error = %v", err)
	}
	return int64(len(data))
}

func saveAndFlushRequests(t *testing.T, store *FileStorage, records ...*StoredRequest) {
	t.Helper()
	for _, sr := range records {
		if err := store.SaveRequest(context.Background(), sr); err != nil {
			t.Fatalf("SaveRequest() error = %v", err)
		}
	}
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
}

func readNonEmptyBatchFiles(t *testing.T, dir string) []storedBatchFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return parseNonEmptyBatchFiles(t, dir, entries)
}

func readAllBatchFiles(t *testing.T, dir string) []storedBatchFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return parseBatchFiles(t, dir, entries)
}

func parseNonEmptyBatchFiles(t *testing.T, dir string, entries []os.DirEntry) []storedBatchFile {
	t.Helper()
	files := parseBatchFiles(t, dir, entries)
	return nonEmptyBatchFiles(files)
}

func parseBatchFiles(t *testing.T, dir string, entries []os.DirEntry) []storedBatchFile {
	t.Helper()
	files := make([]storedBatchFile, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), extJSONL) {
			continue
		}
		files = append(files, parseBatchFile(t, dir, entry))
	}
	return files
}

func nonEmptyBatchFiles(files []storedBatchFile) []storedBatchFile {
	nonEmpty := make([]storedBatchFile, 0, len(files))
	for _, file := range files {
		if file.size > 0 {
			nonEmpty = append(nonEmpty, file)
		}
	}
	return nonEmpty
}

func parseBatchFile(t *testing.T, dir string, entry os.DirEntry) storedBatchFile {
	t.Helper()
	path := filepath.Join(dir, entry.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
	}
	return storedBatchFile{name: entry.Name(), size: int64(len(data)), requests: parseJSONLines(t, data)}
}

func parseJSONLines(t *testing.T, data []byte) []*StoredRequest {
	t.Helper()
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	requests := make([]*StoredRequest, 0, len(lines))
	for _, line := range lines {
		requests = append(requests, parseStoredRequestLine(t, line))
	}
	return requests
}

func parseStoredRequestLine(t *testing.T, line string) *StoredRequest {
	t.Helper()
	var sr StoredRequest
	if err := json.Unmarshal([]byte(line), &sr); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return &sr
}

func assertFilesWithinMaxSize(t *testing.T, files []storedBatchFile, maxSize int64) {
	t.Helper()
	for _, file := range files {
		if file.size > maxSize {
			t.Fatalf("%s size = %d, want <= %d", file.name, file.size, maxSize)
		}
	}
}

func assertRequestNotPersisted(t *testing.T, store *FileStorage, id string) {
	t.Helper()
	if _, err := store.GetRequest(context.Background(), id); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("GetRequest(%s) error = %v, want %v", id, err, ErrRequestNotFound)
	}
}

func assertStoredRequestCount(t *testing.T, store *FileStorage, want int) {
	t.Helper()
	requests, err := store.ListRequests(context.Background(), RequestFilter{})
	if err != nil {
		t.Fatalf("ListRequests() error = %v", err)
	}
	if len(requests) != want {
		t.Fatalf("ListRequests() returned %d requests, want %d", len(requests), want)
	}
}
