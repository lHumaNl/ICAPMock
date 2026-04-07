// Copyright 2026 ICAP Mock

package testing

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
)

// MockMetricsCollector is a mock implementation of the metrics.Collector interface.
// It records all metric calls and provides methods to inspect them.
//
// Usage:
//
//	mock := NewMockMetricsCollector()
//	mock.RecordRequest("REQMOD")
//	mock.RecordRequestDuration("REQMOD", 100*time.Millisecond)
//
//	calls := mock.GetCalls()
//	require.Len(t, calls, 2)
type MockMetricsCollector struct {
	requests         []mockRequestCall
	requestDurations []mockRequestDurationCall
	errors           []mockErrorCall
	requestCount     atomic.Int64
	errorCount       atomic.Int64
	mu               sync.Mutex
}

type mockRequestCall struct {
	time   time.Time
	method string
}

type mockRequestDurationCall struct {
	time     time.Time
	method   string
	duration time.Duration
}

type mockErrorCall struct {
	time   time.Time
	method string
	err    string
}

// NewMockMetricsCollector creates a new mock metrics collector.
//
// Returns:
//   - A new MockMetricsCollector instance
func NewMockMetricsCollector() *MockMetricsCollector {
	return &MockMetricsCollector{
		requests:         make([]mockRequestCall, 0),
		requestDurations: make([]mockRequestDurationCall, 0),
		errors:           make([]mockErrorCall, 0),
	}
}

// RecordRequest records an ICAP request metric.
//
// Parameters:
//   - method: ICAP method (REQMOD, RESPMOD, OPTIONS)
func (m *MockMetricsCollector) RecordRequest(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, mockRequestCall{
		method: method,
		time:   time.Now(),
	})
	m.requestCount.Add(1)
}

// RecordRequestDuration records a request duration metric.
//
// Parameters:
//   - method: ICAP method
//   - duration: Request duration
func (m *MockMetricsCollector) RecordRequestDuration(method string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestDurations = append(m.requestDurations, mockRequestDurationCall{
		method:   method,
		duration: duration,
		time:     time.Now(),
	})
}

// RecordError records an error metric.
//
// Parameters:
//   - method: ICAP method
//   - err: Error that occurred
func (m *MockMetricsCollector) RecordError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errors = append(m.errors, mockErrorCall{
		method: method,
		err:    err.Error(),
		time:   time.Now(),
	})
	m.errorCount.Add(1)
}

// GetRequestCalls returns all recorded request calls.
//
// Returns:
//   - Slice of request calls
func (m *MockMetricsCollector) GetRequestCalls() []mockRequestCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]mockRequestCall, len(m.requests))
	copy(result, m.requests)
	return result
}

// GetRequestDurationCalls returns all recorded request duration calls.
//
// Returns:
//   - Slice of request duration calls
func (m *MockMetricsCollector) GetRequestDurationCalls() []mockRequestDurationCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]mockRequestDurationCall, len(m.requestDurations))
	copy(result, m.requestDurations)
	return result
}

// GetErrorCalls returns all recorded error calls.
//
// Returns:
//   - Slice of error calls
func (m *MockMetricsCollector) GetErrorCalls() []mockErrorCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]mockErrorCall, len(m.errors))
	copy(result, m.errors)
	return result
}

// GetRequestCount returns the total number of recorded requests.
//
// Returns:
//   - Request count
func (m *MockMetricsCollector) GetRequestCount() int64 {
	return m.requestCount.Load()
}

// GetErrorCount returns the total number of recorded errors.
//
// Returns:
//   - Error count
func (m *MockMetricsCollector) GetErrorCount() int64 {
	return m.errorCount.Load()
}

// Reset clears all recorded metrics.
func (m *MockMetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = make([]mockRequestCall, 0)
	m.requestDurations = make([]mockRequestDurationCall, 0)
	m.errors = make([]mockErrorCall, 0)
	m.requestCount.Store(0)
	m.errorCount.Store(0)
}

// AssertRequestCount asserts that a specific number of requests were recorded.
//
// Parameters:
//   - count: Expected request count
//
// Example:
//
//	mock.AssertRequestCount(t, 10)
func (m *MockMetricsCollector) AssertRequestCount(t *testing.T, count int64) {
	t.Helper()

	got := m.GetRequestCount()
	if got != count {
		t.Errorf("Expected %d requests, got %d", count, got)
	}
}

// AssertErrorCount asserts that a specific number of errors were recorded.
//
// Parameters:
//   - count: Expected error count
//
// Example:
//
//	mock.AssertErrorCount(t, 5)
func (m *MockMetricsCollector) AssertErrorCount(t *testing.T, count int64) {
	t.Helper()

	got := m.GetErrorCount()
	if got != count {
		t.Errorf("Expected %d errors, got %d", count, got)
	}
}

// AssertMethodCalled asserts that a specific method was called.
//
// Parameters:
//   - method: ICAP method to check
//
// Returns:
//   - true if the method was called, false otherwise
//
// Example:
//
//	if !mock.AssertMethodCalled(t, "REQMOD") {
//	    t.Error("REQMOD was not called")
//	}
func (m *MockMetricsCollector) AssertMethodCalled(t *testing.T, method string) bool {
	t.Helper()

	for _, call := range m.GetRequestCalls() {
		if call.method == method {
			return true
		}
	}
	return false
}

// MockStorage is a mock implementation of the storage.Storage interface.
// It provides in-memory storage and records all operations.
//
// Usage:
//
//	mock := NewMockStorage()
//	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//
//	sr := storage.FromICAPRequest(req, 204, 0)
//	err := mock.SaveRequest(ctx, sr)
//	require.NoError(t, err)
//
//	saved := mock.GetSavedRequests()
//	require.Len(t, saved, 1)
type MockStorage struct {
	requests      map[string]*storage.StoredRequest
	savedRequests []*storage.StoredRequest
	saveCount     atomic.Int64
	mu            sync.Mutex
	closed        bool
}

// NewMockStorage creates a new mock storage.
//
// Returns:
//   - A new MockStorage instance
func NewMockStorage() *MockStorage {
	return &MockStorage{
		requests:      make(map[string]*storage.StoredRequest),
		savedRequests: make([]*storage.StoredRequest, 0),
	}
}

// SaveRequest saves a request to the mock storage.
//
// Parameters:
//   - ctx: Context for the operation
//   - req: Request to save
//
// Returns:
//   - error if storage is closed
func (m *MockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return storage.ErrStorageClosed
	}

	m.requests[req.ID] = req
	m.savedRequests = append(m.savedRequests, req)
	m.saveCount.Add(1)

	return nil
}

// GetRequest retrieves a request by ID.
//
// Parameters:
//   - ctx: Context for the operation
//   - id: Request ID
//
// Returns:
//   - The stored request
//   - error if not found or storage is closed
func (m *MockStorage) GetRequest(ctx context.Context, id string) (*storage.StoredRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, storage.ErrStorageClosed
	}

	req, ok := m.requests[id]
	if !ok {
		return nil, storage.ErrRequestNotFound
	}

	return req, nil
}

// ListRequests retrieves all stored requests.
//
// Parameters:
//   - ctx: Context for the operation
//   - filter: Filter for requests (ignored in mock)
//
// Returns:
//   - All stored requests
//   - error if storage is closed
func (m *MockStorage) ListRequests(ctx context.Context, filter storage.RequestFilter) ([]*storage.StoredRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, storage.ErrStorageClosed
	}

	result := make([]*storage.StoredRequest, len(m.savedRequests))
	copy(result, m.savedRequests)
	return result, nil
}

// DeleteRequest deletes a request by ID.
//
// Parameters:
//   - ctx: Context for the operation
//   - id: Request ID
//
// Returns:
//   - error if not found or storage is closed
func (m *MockStorage) DeleteRequest(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return storage.ErrStorageClosed
	}

	if _, ok := m.requests[id]; !ok {
		return storage.ErrRequestNotFound
	}

	delete(m.requests, id)

	for i, req := range m.savedRequests {
		if req.ID == id {
			m.savedRequests = append(m.savedRequests[:i], m.savedRequests[i+1:]...)
			break
		}
	}

	return nil
}

// DeleteRequests deletes multiple requests by filter.
//
// Parameters:
//   - ctx: Context for the operation
//   - filter: Filter for requests (ignored in mock, deletes all)
//
// Returns:
//   - Number of requests deleted
//   - error if storage is closed
func (m *MockStorage) DeleteRequests(ctx context.Context, filter storage.RequestFilter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, storage.ErrStorageClosed
	}

	count := int64(len(m.savedRequests))
	m.requests = make(map[string]*storage.StoredRequest)
	m.savedRequests = make([]*storage.StoredRequest, 0)

	return count, nil
}

// Flush is a no-op in the mock storage.
//
// Parameters:
//   - ctx: Context for the operation
//
// Returns:
//   - Always nil
func (m *MockStorage) Flush(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return storage.ErrStorageClosed
	}

	return nil
}

// Clear removes all stored requests.
//
// Parameters:
//   - ctx: Context for the operation
//
// Returns:
//   - Number of requests cleared
//   - error if storage is closed
func (m *MockStorage) Clear(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, storage.ErrStorageClosed
	}

	count := int64(len(m.savedRequests))
	m.requests = make(map[string]*storage.StoredRequest)
	m.savedRequests = make([]*storage.StoredRequest, 0)

	return count, nil
}

// Close closes the mock storage.
//
// Returns:
//   - Always nil
func (m *MockStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// GetSavedRequests returns all saved requests.
//
// Returns:
//   - Slice of saved requests
func (m *MockStorage) GetSavedRequests() []*storage.StoredRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*storage.StoredRequest, len(m.savedRequests))
	copy(result, m.savedRequests)
	return result
}

// GetSaveCount returns the total number of save operations.
//
// Returns:
//   - Save count
func (m *MockStorage) GetSaveCount() int64 {
	return m.saveCount.Load()
}

// IsClosed returns whether the storage is closed.
//
// Returns:
//   - true if closed
func (m *MockStorage) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.closed
}

// MockFileSystem is a mock file system for testing disk space monitoring.
// It simulates file operations and disk space usage.
//
// Usage:
//
//	fs := NewMockFileSystem(10 * 1024 * 1024 * 1024) // 10GB
//	fs.WriteFile("/test.txt", []byte("hello"), 0644)
//
//	used := fs.GetUsedSpace()
//	require.Equal(t, int64(5), used)
type MockFileSystem struct {
	files        map[string][]byte
	writeErrors  map[string]error
	readErrors   map[string]error
	deleteErrors map[string]error
	totalSpace   int64
	usedSpace    int64
	mu           sync.Mutex
}

// NewMockFileSystem creates a new mock file system.
//
// Parameters:
//   - totalSpace: Total disk space in bytes
//
// Returns:
//   - A new MockFileSystem instance
func NewMockFileSystem(totalSpace int64) *MockFileSystem {
	return &MockFileSystem{
		totalSpace:   totalSpace,
		usedSpace:    0,
		files:        make(map[string][]byte),
		writeErrors:  make(map[string]error),
		readErrors:   make(map[string]error),
		deleteErrors: make(map[string]error),
	}
}

// WriteFile writes a file to the mock file system.
//
// Parameters:
//   - path: File path
//   - data: File data
//   - perm: File permissions (ignored)
//
// Returns:
//   - error if disk is full or write error is set
func (m *MockFileSystem) WriteFile(path string, data []byte, perm int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.writeErrors[path]; ok {
		return err
	}

	if m.usedSpace+int64(len(data)) > m.totalSpace {
		return fmt.Errorf("no space left on device")
	}

	m.files[path] = data
	m.usedSpace += int64(len(data))

	return nil
}

// ReadFile reads a file from the mock file system.
//
// Parameters:
//   - path: File path
//
// Returns:
//   - File data
//   - error if file not found or read error is set
func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.readErrors[path]; ok {
		return nil, err
	}

	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// DeleteFile deletes a file from the mock file system.
//
// Parameters:
//   - path: File path
//
// Returns:
//   - error if file not found or delete error is set
func (m *MockFileSystem) DeleteFile(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.deleteErrors[path]; ok {
		return err
	}

	size, ok := m.files[path]
	if !ok {
		return fmt.Errorf("file not found: %s", path)
	}

	m.usedSpace -= int64(len(size))
	delete(m.files, path)

	return nil
}

// GetTotalSpace returns the total disk space.
//
// Returns:
//   - Total space in bytes
func (m *MockFileSystem) GetTotalSpace() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.totalSpace
}

// GetUsedSpace returns the used disk space.
//
// Returns:
//   - Used space in bytes
func (m *MockFileSystem) GetUsedSpace() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.usedSpace
}

// GetFreeSpace returns the free disk space.
//
// Returns:
//   - Free space in bytes
func (m *MockFileSystem) GetFreeSpace() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.totalSpace - m.usedSpace
}

// SetWriteError sets an error to be returned when writing to a specific file.
//
// Parameters:
//   - path: File path
//   - err: Error to return
func (m *MockFileSystem) SetWriteError(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeErrors[path] = err
}

// SetReadError sets an error to be returned when reading from a specific file.
//
// Parameters:
//   - path: File path
//   - err: Error to return
func (m *MockFileSystem) SetReadError(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readErrors[path] = err
}

// SetDeleteError sets an error to be returned when deleting a specific file.
//
// Parameters:
//   - path: File path
//   - err: Error to return
func (m *MockFileSystem) SetDeleteError(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteErrors[path] = err
}

// GetFileCount returns the number of files in the mock file system.
//
// Returns:
//   - File count
func (m *MockFileSystem) GetFileCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.files)
}

// Clear clears all files from the mock file system.
func (m *MockFileSystem) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files = make(map[string][]byte)
	m.usedSpace = 0
	m.writeErrors = make(map[string]error)
	m.readErrors = make(map[string]error)
	m.deleteErrors = make(map[string]error)
}
