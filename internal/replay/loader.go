// Package replay provides request replay functionality for the ICAP Mock Server.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
)

// LoadRequestFiles loads recorded requests from a directory.
// It reads all JSON files from the directory and parses them into StoredRequest objects.
//
// Parameters:
//   - dir: Directory containing request files
//   - filter: Filter to apply to loaded requests (empty filter matches all)
//
// Returns the filtered requests or an error if the directory cannot be read.
func LoadRequestFiles(dir string, filter storage.RequestFilter) ([]*storage.StoredRequest, error) {
	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("accessing directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	// Read directory contents
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var requests []*storage.StoredRequest

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Only process JSON files
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}

		// Load the file
		path := filepath.Join(dir, name)
		req, err := LoadRequestFile(path)
		if err != nil {
			// Log warning but continue with other files
			continue
		}

		// Apply filter
		if matchesFilter(req, filter) {
			requests = append(requests, req)
		}
	}

	// Sort by timestamp (oldest first for replay)
	sortRequestsByTime(requests)

	// Apply limit and offset from filter
	if filter.Offset > 0 && filter.Offset < len(requests) {
		requests = requests[filter.Offset:]
	} else if filter.Offset >= len(requests) {
		requests = nil
	}

	if filter.Limit > 0 && filter.Limit < len(requests) {
		requests = requests[:filter.Limit]
	}

	return requests, nil
}

// LoadRequestFile loads a single request file and parses it.
func LoadRequestFile(path string) (*storage.StoredRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	var req storage.StoredRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parsing file %s: %w", path, err)
	}

	return &req, nil
}

// ParseFilterFromFlags creates a RequestFilter from CLI flag values.
//
// Parameters:
//   - from: Start time in RFC3339 or YYYY-MM-DD format (empty for no start)
//   - to: End time in RFC3339 or YYYY-MM-DD format (empty for no end)
//   - method: ICAP method filter (empty for all methods)
//
// Returns the filter or an error if the date formats are invalid.
func ParseFilterFromFlags(from, to, method string) (storage.RequestFilter, error) {
	filter := storage.RequestFilter{}

	// Parse from date
	if from != "" {
		t, err := parseDateFlag(from)
		if err != nil {
			return filter, fmt.Errorf("parsing --from: %w", err)
		}
		filter.Start = t
	}

	// Parse to date
	if to != "" {
		t, err := parseDateFlag(to)
		if err != nil {
			return filter, fmt.Errorf("parsing --to: %w", err)
		}
		// Set to end of day
		filter.End = t.Add(24*time.Hour - time.Second)
	}

	// Set method filter
	if method != "" {
		filter.Method = strings.ToUpper(method)
	}

	return filter, nil
}

// parseDateFlag parses a date string in various formats.
// Supported formats:
//   - RFC3339: 2006-01-02T15:04:05Z07:00
//   - RFC3339Nano: 2006-01-02T15:04:05.999999999Z07:00
//   - Date only: 2006-01-02
func parseDateFlag(s string) (time.Time, error) {
	// Try RFC3339Nano first
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try date only (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid date format, use RFC3339 or YYYY-MM-DD")
}

// matchesFilter checks if a request matches the given filter criteria.
func matchesFilter(req *storage.StoredRequest, filter storage.RequestFilter) bool {
	// Check time range
	if !filter.Start.IsZero() && req.Timestamp.Before(filter.Start) {
		return false
	}
	if !filter.End.IsZero() && req.Timestamp.After(filter.End) {
		return false
	}

	// Check method
	if filter.Method != "" && req.Method != filter.Method {
		return false
	}

	// Check client IP
	if filter.ClientIP != "" && req.ClientIP != filter.ClientIP {
		return false
	}

	return true
}

// sortRequestsByTime sorts requests by timestamp in ascending order.
func sortRequestsByTime(requests []*storage.StoredRequest) {
	// Simple insertion sort for small arrays
	for i := 1; i < len(requests); i++ {
		for j := i; j > 0 && requests[j].Timestamp.Before(requests[j-1].Timestamp); j-- {
			requests[j], requests[j-1] = requests[j-1], requests[j]
		}
	}
}

// FileStorageAdapter adapts a directory to the Storage interface for replay.
// This allows using LoadRequestFiles through the standard Storage interface.
type FileStorageAdapter struct {
	dir    string
	filter storage.RequestFilter
}

// NewFileStorageAdapter creates a new FileStorageAdapter.
func NewFileStorageAdapter(dir string) *FileStorageAdapter {
	return &FileStorageAdapter{dir: dir}
}

// SaveRequest is not supported for FileStorageAdapter (read-only).
func (a *FileStorageAdapter) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	return fmt.Errorf("FileStorageAdapter is read-only")
}

// GetRequest retrieves a request by ID.
func (a *FileStorageAdapter) GetRequest(ctx context.Context, id string) (*storage.StoredRequest, error) {
	requests, err := LoadRequestFiles(a.dir, storage.RequestFilter{})
	if err != nil {
		return nil, err
	}

	for _, req := range requests {
		if req.ID == id {
			return req, nil
		}
	}

	return nil, fmt.Errorf("request not found: %s", id)
}

// ListRequests lists all requests matching the filter.
func (a *FileStorageAdapter) ListRequests(ctx context.Context, filter storage.RequestFilter) ([]*storage.StoredRequest, error) {
	return LoadRequestFiles(a.dir, filter)
}

// DeleteRequest is not supported for FileStorageAdapter (read-only).
func (a *FileStorageAdapter) DeleteRequest(ctx context.Context, id string) error {
	return fmt.Errorf("FileStorageAdapter is read-only")
}

// DeleteRequests is not supported for FileStorageAdapter (read-only).
func (a *FileStorageAdapter) DeleteRequests(ctx context.Context, filter storage.RequestFilter) (int64, error) {
	return 0, fmt.Errorf("FileStorageAdapter is read-only")
}

// Flush is a no-op for FileStorageAdapter (read-only).
func (a *FileStorageAdapter) Flush(ctx context.Context) error {
	return nil
}

// Clear is not supported for FileStorageAdapter (read-only).
func (a *FileStorageAdapter) Clear(ctx context.Context) (int64, error) {
	return 0, fmt.Errorf("FileStorageAdapter is read-only")
}

// Close is a no-op for FileStorageAdapter.
func (a *FileStorageAdapter) Close() error {
	return nil
}

// SetFilter sets the default filter for the adapter.
func (a *FileStorageAdapter) SetFilter(filter storage.RequestFilter) {
	a.filter = filter
}
