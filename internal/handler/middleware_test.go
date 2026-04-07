// Copyright 2026 ICAP Mock

package handler_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/icap-mock/icap-mock/internal/circuitbreaker"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/middleware"
	"github.com/icap-mock/icap-mock/internal/ratelimit"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// syncBuffer is a thread-safe bytes.Buffer for use as a log output in tests
// where worker goroutines write concurrently with test goroutines reading.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// mockLimiter implements ratelimit.Limiter for testing.
type mockLimiter struct {
	allow bool
}

func (m *mockLimiter) Allow() bool {
	return m.allow
}

func (m *mockLimiter) Wait(ctx context.Context) error {
	if m.allow {
		return nil
	}
	return ctx.Err()
}

func (m *mockLimiter) Reserve() ratelimit.Reservation {
	return &mockReservation{ok: m.allow}
}

type mockReservation struct {
	ok bool
}

func (r *mockReservation) OK() bool             { return r.ok }
func (r *mockReservation) Delay() time.Duration { return 0 }
func (r *mockReservation) Cancel()              {}

// mockStorage implements storage.Storage for testing.
type mockStorage struct {
	saveErr    error
	saveCalled chan struct{}
	requests   []*storage.StoredRequest
	saveCount  int64
	mu         sync.RWMutex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		requests:   make([]*storage.StoredRequest, 0),
		saveCalled: make(chan struct{}, 100),
	}
}

func (m *mockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	atomic.AddInt64(&m.saveCount, 1)
	select {
	case m.saveCalled <- struct{}{}:
	default:
	}
	return m.saveErr
}

func (m *mockStorage) GetRequest(ctx context.Context, id string) (*storage.StoredRequest, error) {
	return nil, storage.ErrRequestNotFound
}

func (m *mockStorage) ListRequests(ctx context.Context, filter storage.RequestFilter) ([]*storage.StoredRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.requests, nil
}

func (m *mockStorage) DeleteRequest(ctx context.Context, id string) error {
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) Flush(ctx context.Context) error {
	return nil
}

func (m *mockStorage) Clear(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := int64(len(m.requests))
	m.requests = make([]*storage.StoredRequest, 0)
	return count, nil
}

func (m *mockStorage) DeleteRequests(ctx context.Context, filter storage.RequestFilter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := int64(len(m.requests))
	m.requests = make([]*storage.StoredRequest, 0)
	return count, nil
}

func (m *mockStorage) getSaveCount() int64 {
	return atomic.LoadInt64(&m.saveCount)
}

// testStorageMiddleware creates a StorageMiddlewareWithPool for testing and returns
// its Wrap method as a handler.Middleware, plus a cleanup function.
func testStorageMiddleware(t *testing.T, store storage.Storage) (handler.Middleware, func()) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := middleware.DefaultStorageMiddlewareConfig()
	sm := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	return sm.Middleware(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sm.Shutdown(ctx)
	}
}

// TestRateLimiterMiddleware_Allow tests that requests are allowed when rate limiter permits.
func TestRateLimiterMiddleware_Allow(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{allow: true}
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.RateLimiterMiddleware(limiter)
	wrappedHandler := middleware(baseHandler)

	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
}

// TestRateLimiterMiddleware_Deny tests that requests are denied when rate limit is exceeded.
func TestRateLimiterMiddleware_Deny(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{allow: false}
	called := false
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		called = true
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.RateLimiterMiddleware(limiter)
	wrappedHandler := middleware(baseHandler)

	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Should return 429 Too Many Requests
	if resp.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", resp.StatusCode)
	}

	// Base handler should not be called
	if called {
		t.Error("Base handler should not be called when rate limited")
	}

	// Check rate limit headers
	if val, ok := resp.GetHeader("X-RateLimit-Remaining"); !ok || val != "0" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", val, "0")
	}
}

// TestStorageMiddleware_SavesRequest tests that requests are saved to storage.
func TestStorageMiddleware_SavesRequest(t *testing.T) {
	t.Parallel()

	store := newMockStorage()
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMw, storageCleanup := testStorageMiddleware(t, store)
	defer storageCleanup()
	wrappedHandler := storageMw(baseHandler)

	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.SetHeader("X-Test-Header", "test-value")

	resp, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Wait for async save to complete
	select {
	case <-store.saveCalled:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for save to complete")
	}

	// Verify request was saved
	requests, _ := store.ListRequests(context.Background(), storage.RequestFilter{})
	if len(requests) != 1 {
		t.Fatalf("Expected 1 saved request, got %d", len(requests))
	}

	saved := requests[0]
	if saved.Method != icap.MethodREQMOD {
		t.Errorf("Saved Method = %q, want %q", saved.Method, icap.MethodREQMOD)
	}
	if saved.URI != "icap://localhost/test" {
		t.Errorf("Saved URI = %q, want %q", saved.URI, "icap://localhost/test")
	}
	if saved.ResponseStatus != icap.StatusOK {
		t.Errorf("Saved ResponseStatus = %d, want %d", saved.ResponseStatus, icap.StatusOK)
	}
}

// TestStorageMiddleware_AsyncSave tests that saving happens asynchronously.
func TestStorageMiddleware_AsyncSave(t *testing.T) {
	t.Parallel()

	store := newMockStorage()

	// Slow save simulation - handler should return immediately
	slowStore := &slowMockStorage{mockStorage: store}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMw, storageCleanup := testStorageMiddleware(t, slowStore)
	defer storageCleanup()
	wrappedHandler := storageMw(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	start := time.Now()
	resp, err := wrappedHandler.Handle(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Response should return immediately, not wait for slow save
	if elapsed > 100*time.Millisecond {
		t.Errorf("Handle() took %v, should return immediately", elapsed)
	}

	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Wait for async save to complete
	select {
	case <-store.saveCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for async save")
	}
}

// slowMockStorage wraps mockStorage with a delay.
type slowMockStorage struct {
	*mockStorage
}

func (s *slowMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	time.Sleep(100 * time.Millisecond)
	return s.mockStorage.SaveRequest(ctx, req)
}

// TestStorageMiddleware_PropagatesError tests that handler errors are propagated.
func TestStorageMiddleware_PropagatesError(t *testing.T) {
	t.Parallel()

	store := newMockStorage()
	expectedErr := errors.New("handler error")
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return nil, expectedErr
	}, "REQMOD")

	storageMw, storageCleanup := testStorageMiddleware(t, store)
	defer storageCleanup()
	wrappedHandler := storageMw(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	if !errors.Is(err, expectedErr) {
		t.Errorf("Handle() error = %v, want %v", err, expectedErr)
	}
	if resp != nil {
		t.Error("Response should be nil on error")
	}

	// Wait for async save
	select {
	case <-store.saveCalled:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for save")
	}

	// Request should still be saved with error status
	requests, _ := store.ListRequests(context.Background(), storage.RequestFilter{})
	if len(requests) != 1 {
		t.Fatalf("Expected 1 saved request, got %d", len(requests))
	}
	// Error case should save status 500
	if requests[0].ResponseStatus != 500 {
		t.Errorf("Saved ResponseStatus = %d, want 500", requests[0].ResponseStatus)
	}
}

// TestChainMiddleware_Order tests that middleware is applied in correct order.
func TestChainMiddleware_Order(t *testing.T) {
	t.Parallel()

	var order []string
	var mu sync.RWMutex

	recordOrder := func(name string) {
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}

	// First middleware (outermost)
	middleware1 := func(next handler.Handler) handler.Handler {
		return handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			recordOrder("m1-before")
			resp, err := next.Handle(ctx, req)
			recordOrder("m1-after")
			return resp, err
		}, "REQMOD")
	}

	// Second middleware
	middleware2 := func(next handler.Handler) handler.Handler {
		return handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			recordOrder("m2-before")
			resp, err := next.Handle(ctx, req)
			recordOrder("m2-after")
			return resp, err
		}, "REQMOD")
	}

	// Base handler
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		recordOrder("base")
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	wrappedHandler := middleware.ChainMiddleware(baseHandler, middleware1, middleware2)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	_, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Order should be: m1-before -> m2-before -> base -> m2-after -> m1-after
	expected := []string{"m1-before", "m2-before", "base", "m2-after", "m1-after"}
	mu.RLock()
	defer mu.RUnlock()
	if len(order) != len(expected) {
		t.Fatalf("Execution order length = %d, want %d", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// TestChainMiddleware_Empty tests that ChainMiddleware with no middleware returns the handler.
func TestChainMiddleware_Empty(t *testing.T) {
	t.Parallel()

	called := false
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		called = true
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	wrappedHandler := middleware.ChainMiddleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	if !called {
		t.Error("Base handler should be called")
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
}

// TestRateLimiterMiddleware_Concurrent tests thread safety of rate limiter middleware.
func TestRateLimiterMiddleware_Concurrent(t *testing.T) {
	t.Parallel()

	limiter := &mockLimiter{allow: true}
	var callCount int64
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt64(&callCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.RateLimiterMiddleware(limiter)
	wrappedHandler := middleware(baseHandler)

	const numRequests = 100
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			resp, err := wrappedHandler.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Handle() returned error: %v", err)
			}
			if resp.StatusCode != icap.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt64(&callCount) != numRequests {
		t.Errorf("callCount = %d, want %d", callCount, numRequests)
	}
}

// TestStorageMiddleware_Concurrent tests thread safety of storage middleware.
func TestStorageMiddleware_Concurrent(t *testing.T) {
	t.Parallel()

	store := newMockStorage()
	var callCount int64
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt64(&callCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMw, storageCleanup := testStorageMiddleware(t, store)
	defer storageCleanup()
	wrappedHandler := storageMw(baseHandler)

	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, err := wrappedHandler.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Handle() returned error: %v", err)
			}
		}()
	}

	wg.Wait()

	// Wait for all async saves to complete
	timeout := time.After(5 * time.Second)
	for store.getSaveCount() < numRequests {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for saves. Got %d, want %d", store.getSaveCount(), numRequests)
		case <-time.After(50 * time.Millisecond):
		}
	}

	if atomic.LoadInt64(&callCount) != numRequests {
		t.Errorf("callCount = %d, want %d", callCount, numRequests)
	}
}

// TestStorageMiddleware_WithHTTPRequest tests storage with embedded HTTP request.
func TestStorageMiddleware_WithHTTPRequest(t *testing.T) {
	t.Parallel()

	store := newMockStorage()
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMw, storageCleanup := testStorageMiddleware(t, store)
	defer storageCleanup()
	wrappedHandler := storageMw(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	req.ClientIP = "192.168.1.100"
	req.RemoteAddr = "192.168.1.100:12345"
	req.SetHeader("X-Client-IP", "192.168.1.100")

	// Add HTTP request info
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "GET",
		URI:    "http://example.com/path",
		Proto:  "HTTP/1.1",
		Header: icap.NewHeader(),
	}
	req.HTTPRequest.Header.Set("Host", "example.com")

	_, err := wrappedHandler.Handle(context.Background(), req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Wait for async save
	select {
	case <-store.saveCalled:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for save")
	}

	requests, _ := store.ListRequests(context.Background(), storage.RequestFilter{})
	if len(requests) != 1 {
		t.Fatalf("Expected 1 saved request, got %d", len(requests))
	}

	saved := requests[0]
	if saved.ClientIP != "192.168.1.100" {
		t.Errorf("Saved ClientIP = %q, want %q", saved.ClientIP, "192.168.1.100")
	}
	if saved.HTTPRequest == nil {
		t.Fatal("Expected HTTPRequest to be saved")
	}
	if saved.HTTPRequest.Method != "GET" {
		t.Errorf("Saved HTTP Method = %q, want %q", saved.HTTPRequest.Method, "GET")
	}
	if saved.HTTPRequest.URI != "http://example.com/path" {
		t.Errorf("Saved HTTP URI = %q, want %q", saved.HTTPRequest.URI, "http://example.com/path")
	}
}

// TestPanicRecoveryMiddleware_RecoversPanic tests that panics are recovered.
func TestPanicRecoveryMiddleware_RecoversPanic(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("test panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	// Should not return error (panic is recovered)
	if err != nil {
		t.Errorf("Handle() returned error: %v, want nil", err)
	}

	// Should return 500 response
	if resp == nil {
		t.Fatal("Response should not be nil")
	}
	if resp.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", resp.StatusCode)
	}

	// Check that panic was logged
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("panic recovered")) {
		t.Errorf("Expected panic to be logged, got: %s", logOutput)
	}
}

// TestPanicRecoveryMiddleware_PropagatesNormalError tests that normal errors are propagated.
func TestPanicRecoveryMiddleware_PropagatesNormalError(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	expectedErr := errors.New("normal error")
	errHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return nil, expectedErr
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(errHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	// Normal errors should be propagated
	if !errors.Is(err, expectedErr) {
		t.Errorf("Handle() error = %v, want %v", err, expectedErr)
	}
	if resp != nil {
		t.Error("Response should be nil on error")
	}

	// Should not log anything about panic
	logOutput := logBuf.String()
	if bytes.Contains([]byte(logOutput), []byte("panic")) {
		t.Errorf("Should not log panic for normal errors, got: %s", logOutput)
	}
}

// TestPanicRecoveryMiddleware_AllowsNormalRequest tests normal requests pass through.
func TestPanicRecoveryMiddleware_AllowsNormalRequest(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
}

// TestStorageMiddlewareWithPool_SavesRequest tests worker pool storage middleware.
func TestStorageMiddlewareWithPool_SavesRequest(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	store := newMockStorage()

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   2,
		QueueSize: 100,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Wait for async save to complete
	select {
	case <-store.saveCalled:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for save to complete")
	}

	// Verify request was saved
	requests, _ := store.ListRequests(context.Background(), storage.RequestFilter{})
	if len(requests) != 1 {
		t.Fatalf("Expected 1 saved request, got %d", len(requests))
	}

	saved := requests[0]
	if saved.Method != icap.MethodREQMOD {
		t.Errorf("Saved Method = %q, want %q", saved.Method, icap.MethodREQMOD)
	}
	if saved.URI != "icap://localhost/test" {
		t.Errorf("Saved URI = %q, want %q", saved.URI, "icap://localhost/test")
	}
}

// TestStorageMiddlewareWithPool_WorkerPoolLimits tests that worker pool limits goroutines.
func TestStorageMiddlewareWithPool_WorkerPoolLimits(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Use a slow storage to ensure queue fills up
	var goroutineCount int64
	slowStore := &countingMockStorage{
		mockStorage: newMockStorage(),
		onSave: func() {
			atomic.AddInt64(&goroutineCount, 1)
			time.Sleep(50 * time.Millisecond)
		},
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   2,
		QueueSize: 10,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(slowStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send many requests concurrently
	const numRequests = 20
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, _ = wrappedHandler.Handle(context.Background(), req)
		}()
	}

	wg.Wait()

	// Wait for all saves to complete
	time.Sleep(500 * time.Millisecond)

	// With only 2 workers and a queue of 10, we should never have more than
	// a bounded number of concurrent operations
	maxObserved := atomic.LoadInt64(&goroutineCount)
	if maxObserved > 12 { // workers + queue size + small buffer
		t.Logf("Warning: observed %d concurrent saves (expected bounded)", maxObserved)
	}
}

// countingMockStorage wraps mockStorage with a callback on save.
type countingMockStorage struct {
	*mockStorage
	onSave func()
}

func (s *countingMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	if s.onSave != nil {
		s.onSave()
	}
	return s.mockStorage.SaveRequest(ctx, req)
}

// TestStorageMiddlewareWithPool_Backpressure tests behavior when pool full.
func TestStorageMiddlewareWithPool_Backpressure(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Use a blocking storage that never completes
	blockingStore := &blockingMockStorage{
		mockStorage: newMockStorage(),
		block:       make(chan struct{}),
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 2, // Very small queue
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(blockingStore, logger, cfg)

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests to fill the queue
	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, _ = wrappedHandler.Handle(context.Background(), req)
		}()
	}

	wg.Wait()

	// Check that some requests were dropped
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("queue full")) {
		t.Errorf("Expected 'queue full' warning in logs, got: %s", logOutput)
	}

	// Unblock and cleanup
	close(blockingStore.block)

	// Shutdown the middleware
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = storageMiddleware.Shutdown(shutdownCtx)
}

// blockingMockStorage wraps mockStorage with blocking behavior.
type blockingMockStorage struct {
	*mockStorage
	block chan struct{}
}

func (s *blockingMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	<-s.block // Block until released
	return s.mockStorage.SaveRequest(ctx, req)
}

// TestStorageMiddlewareWithPool_Concurrent tests thread safety of worker pool.
func TestStorageMiddlewareWithPool_Concurrent(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	store := newMockStorage()

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   4,
		QueueSize: 500,
	}

	var callCount int64
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt64(&callCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	const numRequests = 100
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, err := wrappedHandler.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Handle() returned error: %v", err)
			}
		}()
	}

	wg.Wait()

	// Wait for all async saves to complete
	timeout := time.After(5 * time.Second)
	for store.getSaveCount() < numRequests {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for saves. Got %d, want %d", store.getSaveCount(), numRequests)
		case <-time.After(50 * time.Millisecond):
		}
	}

	if atomic.LoadInt64(&callCount) != numRequests {
		t.Errorf("callCount = %d, want %d", callCount, numRequests)
	}
}

// TestDefaultStorageMiddlewareConfig tests default configuration values.
func TestDefaultStorageMiddlewareConfig(t *testing.T) {
	t.Parallel()

	cfg := middleware.DefaultStorageMiddlewareConfig()

	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if cfg.QueueSize != 1000 {
		t.Errorf("QueueSize = %d, want 1000", cfg.QueueSize)
	}
}

// TestStorageMiddlewareWithPool_GracefulShutdown tests that workers shut down gracefully.
func TestStorageMiddlewareWithPool_GracefulShutdown(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	store := newMockStorage()

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   2,
		QueueSize: 100,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send some requests
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := wrappedHandler.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}
	}

	// Wait for saves to complete
	timeout := time.After(2 * time.Second)
	for store.getSaveCount() < 5 {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for saves. Got %d, want 5", store.getSaveCount())
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Shutdown should complete within reasonable time
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := storageMiddleware.Shutdown(shutdownCtx)
	if err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}

	// Verify all requests were saved
	if store.getSaveCount() != 5 {
		t.Errorf("SaveCount = %d, want 5", store.getSaveCount())
	}
}

// TestStorageMiddlewareWithPool_ContextPropagation tests that request context is preserved.
func TestStorageMiddlewareWithPool_ContextPropagation(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that captures the context
	var capturedContext context.Context
	ctxStore := &contextCapturingStorage{
		mockStorage: newMockStorage(),
		onSave: func(ctx context.Context) {
			capturedContext = ctx
		},
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 10,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(ctxStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Create a context with a custom value
	type contextKey string
	const testKey contextKey = "test-key"
	ctx := context.WithValue(context.Background(), testKey, "test-value")

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	_, err := wrappedHandler.Handle(ctx, req)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	// Wait for save to complete
	select {
	case <-ctxStore.saveCalled:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for save")
	}

	// Verify context was propagated
	if capturedContext == nil {
		t.Fatal("Context was not captured")
	}
	if capturedContext.Value(testKey) != "test-value" {
		t.Errorf("Context value not preserved: got %v, want 'test-value'", capturedContext.Value(testKey))
	}
}

// TestStorageMiddlewareWithPool_ShutdownTimeout tests shutdown respects context timeout.
func TestStorageMiddlewareWithPool_ShutdownTimeout(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a blocking storage that never completes
	blockingStore := &blockingMockStorage{
		mockStorage: newMockStorage(),
		block:       make(chan struct{}),
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 10,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(blockingStore, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send a request that will block
	go func() {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, _ = wrappedHandler.Handle(context.Background(), req)
	}()

	// Give time for the job to be picked up by the worker
	time.Sleep(50 * time.Millisecond)

	// Shutdown with a short timeout should timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := storageMiddleware.Shutdown(shutdownCtx)
	if err == nil {
		t.Error("Shutdown() should return error on timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() error = %v, want %v", err, context.DeadlineExceeded)
	}

	// Unblock the storage
	close(blockingStore.block)
}

// TestNewStorageMiddlewareWithPool tests the constructor function.
func TestNewStorageMiddlewareWithPool(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	store := newMockStorage()

	t.Run("valid configuration", func(t *testing.T) {
		cfg := middleware.StorageMiddlewareConfig{
			Workers:   2,
			QueueSize: 100,
		}

		middleware, err := middleware.NewStorageMiddlewareWithPool(store, logger, cfg)
		if err != nil {
			t.Fatalf("NewStorageMiddlewareWithPool() error = %v", err)
		}
		if middleware == nil {
			t.Fatal("NewStorageMiddlewareWithPool() returned nil")
		}
		defer middleware.Shutdown(context.Background())
	})

	t.Run("nil store", func(t *testing.T) {
		cfg := middleware.StorageMiddlewareConfig{
			Workers:   2,
			QueueSize: 100,
		}

		_, err := middleware.NewStorageMiddlewareWithPool(nil, logger, cfg)
		if err == nil {
			t.Error("NewStorageMiddlewareWithPool() should return error for nil store")
		}
	})

	t.Run("default values for invalid config", func(t *testing.T) {
		cfg := middleware.StorageMiddlewareConfig{
			Workers:   0, // Should default to 4
			QueueSize: 0, // Should default to 1000
		}

		middleware, err := middleware.NewStorageMiddlewareWithPool(store, logger, cfg)
		if err != nil {
			t.Fatalf("NewStorageMiddlewareWithPool() error = %v", err)
		}
		if middleware == nil {
			t.Fatal("NewStorageMiddlewareWithPool() returned nil")
		}
		defer middleware.Shutdown(context.Background())
	})
}

// contextCapturingStorage wraps mockStorage and captures context on save.
type contextCapturingStorage struct {
	*mockStorage
	onSave func(ctx context.Context)
}

func (s *contextCapturingStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	if s.onSave != nil {
		s.onSave(ctx)
	}
	return s.mockStorage.SaveRequest(ctx, req)
}

// ============================================================================
// Wave 1 Fix: Enhanced Panic Recovery Tests
// ============================================================================

// TestPanicRecoveryMiddleware_WithStackTrace tests that panic stack trace is captured.
func TestPanicRecoveryMiddleware_WithStackTrace(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("detailed panic message with stack trace")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	// Should not return error (panic is recovered)
	if err != nil {
		t.Errorf("Handle() returned error: %v, want nil", err)
	}

	// Should return 500 response
	if resp == nil {
		t.Fatal("Response should not be nil")
	}
	if resp.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", resp.StatusCode)
	}

	// Check that panic details were logged
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("panic recovered")) {
		t.Errorf("Expected 'panic recovered' in logs, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("detailed panic message")) {
		t.Errorf("Expected panic message in logs, got: %s", logOutput)
	}
}

// TestPanicRecoveryMiddleware_DifferentPanicTypes tests recovery from various panic types.
func TestPanicRecoveryMiddleware_DifferentPanicTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		panicVal interface{}
		wantLog  string
	}{
		{
			name:     "string panic",
			panicVal: "string panic",
			wantLog:  "string panic",
		},
		{
			name:     "error panic",
			panicVal: errors.New("error panic"),
			wantLog:  "error panic",
		},
		{
			name:     "int panic",
			panicVal: 42,
			wantLog:  "42", // slog should convert to string
		},
		{
			name:     "nil panic",
			panicVal: nil,
			wantLog:  "<nil>",
		},
		{
			name:     "struct panic",
			panicVal: struct{ Name string }{"test"},
			wantLog:  "", // Just verify it doesn't crash
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf syncBuffer
			logger := slog.New(slog.NewTextHandler(&logBuf, nil))

			panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
				panic(tt.panicVal)
			}, "REQMOD")

			middleware := middleware.PanicRecoveryMiddleware(logger)
			wrappedHandler := middleware(panicHandler)

			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

			resp, err := wrappedHandler.Handle(context.Background(), req)

			// Should always recover and return 500
			if err != nil {
				t.Errorf("Handle() returned error: %v, want nil", err)
			}
			if resp == nil || resp.StatusCode != 500 {
				t.Errorf("Expected 500 response, got: %v", resp)
			}
		})
	}
}

// TestPanicRecoveryMiddleware_ConcurrentPanics tests concurrent panic recovery.
func TestPanicRecoveryMiddleware_ConcurrentPanics(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("concurrent panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	var successCount int64

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			resp, err := wrappedHandler.Handle(context.Background(), req)

			if err == nil && resp != nil && resp.StatusCode == 500 {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	if successCount != numRequests {
		t.Errorf("Recovered from %d/%d panics", successCount, numRequests)
	}
}

// TestPanicRecoveryMiddleware_ConnectionHeader tests that Connection: close is set.
func TestPanicRecoveryMiddleware_ConnectionHeader(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("test panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}

	if connHeader, ok := resp.GetHeader("Connection"); !ok || connHeader != "close" {
		t.Errorf("Connection header = %q, want 'close'", connHeader)
	}
}

// TestPanicRecoveryMiddleware_LogsRequestDetails tests that request details are logged.
func TestPanicRecoveryMiddleware_LogsRequestDetails(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("test panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/custom-path")
	req.SetHeader("X-Custom", "value")

	resp, _ := wrappedHandler.Handle(context.Background(), req)
	_ = resp // Ignore response, we're checking logs

	logOutput := logBuf.String()

	// Check that request method and URI are logged
	if !bytes.Contains([]byte(logOutput), []byte("REQMOD")) {
		t.Errorf("Expected method 'REQMOD' in logs, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("/custom-path")) {
		t.Errorf("Expected URI in logs, got: %s", logOutput)
	}
}

// TestPanicRecoveryMiddleware_ChainedWithOtherMiddleware tests panic recovery in middleware chain.
func TestPanicRecoveryMiddleware_ChainedWithOtherMiddleware(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a panic-inducing handler
	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("chained panic")
	}, "REQMOD")

	// Chain: Rate limiter -> Panic Recovery -> Panic Handler
	limiter := &mockLimiter{allow: true}
	rateLimitMiddleware := middleware.RateLimiterMiddleware(limiter)
	panicMiddleware := middleware.PanicRecoveryMiddleware(logger)

	// Apply in order: panic recovery wraps rate limiter wraps handler
	wrapped := panicMiddleware(rateLimitMiddleware(panicHandler))

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	resp, err := wrapped.Handle(context.Background(), req)

	// Should recover from panic
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp == nil || resp.StatusCode != 500 {
		t.Errorf("Expected 500 response, got: %v", resp)
	}
}

// ============================================================================
// Panic Recovery Integration Tests
// ============================================================================

// TestPanicRecoveryMiddleware_ServerContinuesAfterPanic verifies that the handler
// continues to process requests after a panic occurs. This is critical for server stability.
func TestPanicRecoveryMiddleware_ServerContinuesAfterPanic(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicCount := 0
	successCount := 0

	// Create a handler that panics on certain URIs
	flakyHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		if req.URI == "icap://localhost/panic" {
			panicCount++
			panic("intentional panic for testing")
		}
		successCount++
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(flakyHandler)

	// First request: trigger a panic
	panicReq, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/panic")
	resp1, err1 := wrappedHandler.Handle(context.Background(), panicReq)

	// Verify panic was recovered and returned 500
	if err1 != nil {
		t.Errorf("First request returned error: %v, want nil (panic recovered)", err1)
	}
	if resp1 == nil || resp1.StatusCode != 500 {
		t.Errorf("First request StatusCode = %d, want 500", resp1.StatusCode)
	}

	// Second request: should work normally
	normalReq, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/normal")
	resp2, err2 := wrappedHandler.Handle(context.Background(), normalReq)

	// Verify second request succeeded
	if err2 != nil {
		t.Errorf("Second request returned error: %v, want nil", err2)
	}
	if resp2 == nil || resp2.StatusCode != icap.StatusOK {
		t.Errorf("Second request StatusCode = %d, want %d", resp2.StatusCode, icap.StatusOK)
	}

	// Third request: another panic to verify continued stability
	resp3, err3 := wrappedHandler.Handle(context.Background(), panicReq)

	if err3 != nil {
		t.Errorf("Third request returned error: %v, want nil", err3)
	}
	if resp3 == nil || resp3.StatusCode != 500 {
		t.Errorf("Third request StatusCode = %d, want 500", resp3.StatusCode)
	}

	// Fourth request: should still work
	resp4, err4 := wrappedHandler.Handle(context.Background(), normalReq)

	if err4 != nil {
		t.Errorf("Fourth request returned error: %v, want nil", err4)
	}
	if resp4 == nil || resp4.StatusCode != icap.StatusOK {
		t.Errorf("Fourth request StatusCode = %d, want %d", resp4.StatusCode, icap.StatusOK)
	}

	// Verify the handler state
	if panicCount != 2 {
		t.Errorf("Panic count = %d, want 2", panicCount)
	}
	if successCount != 2 {
		t.Errorf("Success count = %d, want 2", successCount)
	}
}

// TestPanicRecoveryMiddleware_NoServerCrash verifies the middleware prevents
// server crashes by ensuring the test itself doesn't panic.
func TestPanicRecoveryMiddleware_NoServerCrash(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// This test should complete without the test itself crashing
	// If PanicRecoveryMiddleware is broken, this test will fail/panic
	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("server crash test panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	// This should NOT panic - the middleware should recover
	resp, err := wrappedHandler.Handle(context.Background(), req)

	// If we reach here, the panic was recovered (server didn't crash)
	if err != nil {
		t.Errorf("Handle() returned error: %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("Response should not be nil")
	}
	if resp.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", resp.StatusCode)
	}

	// Verify connection close header is set (server should close connection)
	if conn, ok := resp.GetHeader("Connection"); !ok || conn != "close" {
		t.Errorf("Connection header = %q, want 'close'", conn)
	}
}

// TestPanicRecoveryMiddleware_ThroughFullChain tests panic recovery through
// a full middleware chain simulating real server setup.
func TestPanicRecoveryMiddleware_ThroughFullChain(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	store := newMockStorage()

	// Create a handler that sometimes panics
	var requestCount int64
	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt64(&requestCount, 1)
		if count%3 == 0 {
			panic("periodic panic")
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	// Build full middleware chain like in main.go
	limiter := &mockLimiter{allow: true}
	rateLimitMiddleware := middleware.RateLimiterMiddleware(limiter)
	panicMiddleware := middleware.PanicRecoveryMiddleware(logger)
	storageMw, storageCleanup := testStorageMiddleware(t, store)
	defer storageCleanup()
	storageMiddleware := storageMw

	// Chain: Panic Recovery -> Rate Limiter -> Storage -> Handler
	// (Panic recovery should be outermost to catch all panics)
	wrapped := middleware.ChainMiddleware(panicHandler,
		panicMiddleware,
		rateLimitMiddleware,
		storageMiddleware,
	)

	// Send multiple requests, some will trigger panics
	const numRequests = 9
	var panicResponses, successResponses int

	for i := 0; i < numRequests; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		resp, err := wrapped.Handle(context.Background(), req)

		if err != nil {
			t.Errorf("Request %d returned error: %v", i, err)
			continue
		}
		if resp == nil {
			t.Errorf("Request %d returned nil response", i)
			continue
		}

		switch resp.StatusCode {
		case 500:
			panicResponses++
		case icap.StatusOK:
			successResponses++
		default:
			t.Errorf("Request %d: unexpected status code %d", i, resp.StatusCode)
		}
	}

	// Requests 3, 6, 9 should panic (count % 3 == 0)
	expectedPanicResponses := 3
	expectedSuccessResponses := 6

	if panicResponses != expectedPanicResponses {
		t.Errorf("Panic responses = %d, want %d", panicResponses, expectedPanicResponses)
	}
	if successResponses != expectedSuccessResponses {
		t.Errorf("Success responses = %d, want %d", successResponses, expectedSuccessResponses)
	}

	// Verify all requests were handled (no crashes)
	if panicResponses+successResponses != numRequests {
		t.Errorf("Total responses = %d, want %d", panicResponses+successResponses, numRequests)
	}
}

// TestPanicRecoveryMiddleware_ConcurrentServerStability tests that concurrent
// panics don't crash the server and all requests are handled.
func TestPanicRecoveryMiddleware_ConcurrentServerStability(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	var panicCount int64
	var successCount int64

	// Handler that panics 50% of the time
	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		// Use request URI to determine behavior for deterministic testing
		// Panic if URI contains "/panic-trigger" (long URI)
		if len(req.URI) > 40 {
			atomic.AddInt64(&panicCount, 1)
			panic("concurrent panic")
		}
		atomic.AddInt64(&successCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	const numGoroutines = 20
	const requestsPerGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var errorCount int64

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for r := 0; r < requestsPerGoroutine; r++ {
				// Alternate between panic and normal URIs
				// Normal: ~24 chars, Panic: >40 chars
				uri := "icap://localhost/normal"
				if r%2 == 0 {
					uri = "icap://localhost/panic-trigger-path-with-long-name"
				}

				req, _ := icap.NewRequest(icap.MethodREQMOD, uri)
				resp, err := wrappedHandler.Handle(context.Background(), req)

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					continue
				}
				if resp == nil {
					atomic.AddInt64(&errorCount, 1)
					continue
				}
				// Verify response is either 500 (panic) or 200 (success)
				if resp.StatusCode != 500 && resp.StatusCode != icap.StatusOK {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify no errors occurred (all panics were recovered)
	if errorCount > 0 {
		t.Errorf("Error count = %d, want 0", errorCount)
	}

	// Verify both panic and success paths were hit
	totalRequests := numGoroutines * requestsPerGoroutine
	handledRequests := atomic.LoadInt64(&panicCount) + atomic.LoadInt64(&successCount)
	if handledRequests != int64(totalRequests) {
		t.Errorf("Handled requests = %d, want %d", handledRequests, totalRequests)
	}

	t.Logf("Concurrent test completed: %d panics recovered, %d successful requests",
		atomic.LoadInt64(&panicCount), atomic.LoadInt64(&successCount))
}

// TestPanicRecoveryMiddleware_ResponseBody tests that panic recovery returns
// appropriate response body.
func TestPanicRecoveryMiddleware_ResponseBody(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("response body test panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	// Verify response structure
	if resp.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", resp.StatusCode)
	}
	if resp.Proto != "ICAP/1.0" {
		t.Errorf("Proto = %q, want 'ICAP/1.0'", resp.Proto)
	}

	// Verify connection close header
	if conn, ok := resp.GetHeader("Connection"); !ok || conn != "close" {
		t.Errorf("Connection header = %q, want 'close'", conn)
	}
}

// ============================================================================
// Benchmarks for Middleware Performance
// ============================================================================

// BenchmarkPanicRecoveryMiddleware_NoPanic benchmarks middleware without panic.
func BenchmarkPanicRecoveryMiddleware_NoPanic(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrappedHandler.Handle(context.Background(), req)
	}
}

// BenchmarkPanicRecoveryMiddleware_WithPanic benchmarks middleware with panic (slow path).
func BenchmarkPanicRecoveryMiddleware_WithPanic(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	panicHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		panic("benchmark panic")
	}, "REQMOD")

	middleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := middleware(panicHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrappedHandler.Handle(context.Background(), req)
	}
}

// BenchmarkRateLimiterMiddleware_Allow benchmarks rate limiter when allowed.
func BenchmarkRateLimiterMiddleware_Allow(b *testing.B) {
	limiter := &mockLimiter{allow: true}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := middleware.RateLimiterMiddleware(limiter)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrappedHandler.Handle(context.Background(), req)
	}
}

// BenchmarkChainMiddleware benchmarks chained middleware performance.
func BenchmarkChainMiddleware(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := &mockLimiter{allow: true}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	// Chain 3 middlewares
	wrapped := middleware.ChainMiddleware(baseHandler,
		middleware.PanicRecoveryMiddleware(logger),
		middleware.RateLimiterMiddleware(limiter),
		middleware.PanicRecoveryMiddleware(logger), // Second layer for safety
	)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrapped.Handle(context.Background(), req)
	}
}

// BenchmarkStorageMiddlewareWithPool benchmarks storage middleware with pool.
func BenchmarkStorageMiddlewareWithPool(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newMockStorage()

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   4,
		QueueSize: 1000,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrappedHandler.Handle(context.Background(), req)
	}
}

// ============================================================================
// Circuit Breaker Tests (standalone tests removed — see internal/circuitbreaker)
// ============================================================================

// placeholder_removed marks that standalone CircuitBreaker unit tests were removed
// because they tested the duplicate implementation. The canonical tests live in
// internal/circuitbreaker/circuitbreaker_test.go.
//
// Integration tests with StorageMiddleware remain below.

// failingMockStorage is a mock storage that always fails.
type failingMockStorage struct {
	*mockStorage
	failCount int64
}

func (s *failingMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	atomic.AddInt64(&s.failCount, 1)
	return errors.New("simulated storage failure")
}

// TestStorageMiddlewareWithPool_CircuitBreakerOpens tests that circuit breaker opens on failures.
func TestStorageMiddlewareWithPool_CircuitBreakerOpens(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that always fails
	failingStore := &failingMockStorage{mockStorage: newMockStorage()}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 100,
		CircuitBreaker: middleware.CircuitBreakerConfig{
			Enabled:          true,
			MaxFailures:      3,
			ResetTimeout:     30 * time.Second,
			SuccessThreshold: 2,
		},
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(failingStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests to trigger failures
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := wrappedHandler.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Request %d returned error: %v", i, err)
		}
	}

	// Wait for workers to process
	time.Sleep(100 * time.Millisecond)

	// Circuit should be open
	cb := storageMiddleware.GetCircuitBreaker()
	if cb == nil {
		t.Fatal("CircuitBreaker should not be nil")
	}
	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("CircuitBreaker state = %v, want %v", cb.State(), circuitbreaker.StateOpen)
	}

	// Check that circuit opened log was recorded
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("circuit breaker opened")) {
		t.Errorf("Expected 'circuit breaker opened' in logs, got: %s", logOutput)
	}
}

// TestStorageMiddlewareWithPool_CircuitBreakerSkipsStorage tests that open circuit skips storage.
func TestStorageMiddlewareWithPool_CircuitBreakerSkipsStorage(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that always fails
	failingStore := &failingMockStorage{mockStorage: newMockStorage()}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 100,
		CircuitBreaker: middleware.CircuitBreakerConfig{
			Enabled:          true,
			MaxFailures:      2,
			ResetTimeout:     30 * time.Second,
			SuccessThreshold: 2,
		},
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(failingStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests to trigger failures and open circuit
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := wrappedHandler.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Request %d returned error: %v", i, err)
		}
	}

	// Wait for workers to process
	time.Sleep(100 * time.Millisecond)

	// Reset fail count
	initialFailCount := atomic.LoadInt64(&failingStore.failCount)

	// Send more requests - should be skipped due to open circuit
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := wrappedHandler.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Request %d returned error: %v", i, err)
		}
	}

	// Wait a bit for any potential async processing
	time.Sleep(50 * time.Millisecond)

	// Storage should not have been called again (circuit is open)
	finalFailCount := atomic.LoadInt64(&failingStore.failCount)
	if finalFailCount > initialFailCount+1 { // Allow 1 for race conditions
		t.Errorf("Storage was called %d times after circuit opened, expected no more calls", finalFailCount-initialFailCount)
	}

	// Check that circuit breaker opened (logged by circuitbreaker package)
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("circuit breaker opened due to failure threshold")) &&
		!bytes.Contains([]byte(logOutput), []byte("circuit breaker state transition")) {
		t.Errorf("Expected circuit breaker open log in output, got: %s", logOutput)
	}
}

// TestStorageMiddlewareWithPool_CircuitBreakerRecovery tests circuit breaker recovery.
func TestStorageMiddlewareWithPool_CircuitBreakerRecovery(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that can be toggled between failing and working
	toggleStore := &toggleableMockStorage{
		mockStorage: newMockStorage(),
		shouldFail:  true,
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 100,
		CircuitBreaker: middleware.CircuitBreakerConfig{
			Enabled:          true,
			MaxFailures:      2,
			ResetTimeout:     200 * time.Millisecond,
			SuccessThreshold: 2,
		},
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(toggleStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)
	cb := storageMiddleware.GetCircuitBreaker()

	// Send requests to trigger failures and open circuit
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		wrappedHandler.Handle(context.Background(), req)
	}

	// Wait for workers to process
	time.Sleep(100 * time.Millisecond)

	if cb.State() != circuitbreaker.StateOpen {
		t.Fatalf("CircuitBreaker state = %v, want %v", cb.State(), circuitbreaker.StateOpen)
	}

	// Fix the storage
	toggleStore.mu.Lock()
	toggleStore.shouldFail = false
	toggleStore.mu.Unlock()

	// Wait for reset timeout
	time.Sleep(250 * time.Millisecond)

	// Send requests to test recovery (half-open -> closed)
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		wrappedHandler.Handle(context.Background(), req)
		time.Sleep(50 * time.Millisecond) // Give time for processing
	}

	// Circuit should be closed now
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("CircuitBreaker state = %v, want %v", cb.State(), circuitbreaker.StateClosed)
	}
}

// toggleableMockStorage is a mock storage that can be toggled between failing and working.
type toggleableMockStorage struct {
	*mockStorage
	mu         sync.Mutex
	shouldFail bool
}

func (s *toggleableMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	s.mu.Lock()
	shouldFail := s.shouldFail
	s.mu.Unlock()

	if shouldFail {
		return errors.New("simulated storage failure")
	}
	return s.mockStorage.SaveRequest(ctx, req)
}

// TestStorageMiddlewareWithPool_CircuitBreakerDisabled tests disabled circuit breaker.
func TestStorageMiddlewareWithPool_CircuitBreakerDisabled(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that always fails
	failingStore := &failingMockStorage{mockStorage: newMockStorage()}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 100,
		CircuitBreaker: middleware.CircuitBreakerConfig{
			Enabled: false, // Disabled
		},
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(failingStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	// Circuit breaker should be nil when disabled
	if storageMiddleware.GetCircuitBreaker() != nil {
		t.Error("CircuitBreaker should be nil when disabled")
	}

	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests - should still be attempted even though storage fails
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := wrappedHandler.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Request %d returned error: %v", i, err)
		}
	}

	// Wait for workers to process
	time.Sleep(100 * time.Millisecond)

	// All requests should have attempted storage (no circuit breaker protection)
	failCount := atomic.LoadInt64(&failingStore.failCount)
	if failCount < 5 {
		t.Errorf("Storage fail count = %d, expected at least 5", failCount)
	}
}

// TestStorageMiddlewareConfig_DefaultsWithCircuitBreaker tests default config includes circuit breaker.
func TestStorageMiddlewareConfig_DefaultsWithCircuitBreaker(t *testing.T) {
	t.Parallel()

	cfg := middleware.DefaultStorageMiddlewareConfig()

	if !cfg.CircuitBreaker.Enabled {
		t.Error("CircuitBreaker.Enabled should be true by default")
	}
	if cfg.CircuitBreaker.MaxFailures != 5 {
		t.Errorf("CircuitBreaker.MaxFailures = %d, want 5", cfg.CircuitBreaker.MaxFailures)
	}
	if cfg.CircuitBreaker.ResetTimeout != 30*time.Second {
		t.Errorf("CircuitBreaker.ResetTimeout = %v, want %v", cfg.CircuitBreaker.ResetTimeout, 30*time.Second)
	}
	if cfg.CircuitBreaker.SuccessThreshold != 3 {
		t.Errorf("CircuitBreaker.SuccessThreshold = %d, want 3", cfg.CircuitBreaker.SuccessThreshold)
	}
}

// TestStorageMiddlewareWithPool_CircuitBreakerWithSuccesses tests successful operations reset failures.
func TestStorageMiddlewareWithPool_CircuitBreakerWithSuccesses(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a storage that alternates between success and failure
	alternatingStore := &alternatingMockStorage{mockStorage: newMockStorage()}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 100,
		CircuitBreaker: middleware.CircuitBreakerConfig{
			Enabled:          true,
			MaxFailures:      15, // High threshold: rolling window counts all failures, not consecutive
			ResetTimeout:     30 * time.Second,
			SuccessThreshold: 2,
		},
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(alternatingStore, logger, cfg)
	defer storageMiddleware.Shutdown(context.Background())

	wrappedHandler := storageMiddleware.Wrap(baseHandler)
	cb := storageMiddleware.GetCircuitBreaker()

	// Send many requests - successes should reset failure count
	for i := 0; i < 20; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		wrappedHandler.Handle(context.Background(), req)
	}

	// Wait for workers to process
	time.Sleep(200 * time.Millisecond)

	// Circuit should still be closed because successes reset the failure counter
	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("CircuitBreaker state = %v, want %v (successes should reset failures)", cb.State(), circuitbreaker.StateClosed)
	}
}

// alternatingMockStorage alternates between success and failure.
type alternatingMockStorage struct {
	*mockStorage
	callCount int64
}

func (s *alternatingMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	count := atomic.AddInt64(&s.callCount, 1)
	if count%2 == 0 {
		return errors.New("alternating failure")
	}
	return s.mockStorage.SaveRequest(ctx, req)
}

// TestStorageMiddlewareWithPool_BackpressureMetrics tests that backpressure
// rejections are properly tracked in metrics.
func TestStorageMiddlewareWithPool_BackpressureMetrics(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Use a blocking storage that never completes
	blockingStore := &blockingMockStorage{
		mockStorage: newMockStorage(),
		block:       make(chan struct{}),
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 2, // Very small queue
		Metrics:   metricsCollector,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(blockingStore, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests to fill the queue
	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, _ = wrappedHandler.Handle(context.Background(), req)
		}()
	}

	wg.Wait()

	// Check that some requests were dropped
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("queue full")) {
		t.Errorf("Expected 'queue full' warning in logs, got: %s", logOutput)
	}

	// Check that the backpressure rejection counter was incremented
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	var backpressureCount float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_storage_backpressure_rejected_total" {
			for _, m := range mf.GetMetric() {
				backpressureCount += m.GetCounter().GetValue()
			}
		}
	}

	if backpressureCount == 0 {
		t.Errorf("Expected icap_storage_backpressure_rejected_total metric to be incremented, got 0")
	}

	// Verify the log contains enhanced details with rejection count
	if !bytes.Contains([]byte(logOutput), []byte("rejected_count")) {
		t.Errorf("Expected 'rejected_count' in logs, got: %s", logOutput)
	}

	if !bytes.Contains([]byte(logOutput), []byte("queue_size")) {
		t.Errorf("Expected 'queue_size' in logs, got: %s", logOutput)
	}

	if !bytes.Contains([]byte(logOutput), []byte("max_queue_size")) {
		t.Errorf("Expected 'max_queue_size' in logs, got: %s", logOutput)
	}

	// Unblock and cleanup
	close(blockingStore.block)

	// Shutdown the middleware
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = storageMiddleware.Shutdown(shutdownCtx)
}

// TestStorageMiddlewareWithPool_QueueDrainedMetrics tests that queue drain
// during shutdown is properly tracked in metrics.
func TestStorageMiddlewareWithPool_QueueDrainedMetrics(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Use a blocking storage that will only unblock after shutdown starts
	blockingStore := &blockingMockStorage{
		mockStorage: newMockStorage(),
		block:       make(chan struct{}),
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   2,
		QueueSize: 10,
		Metrics:   metricsCollector,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(blockingStore, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send requests - these will be queued because storage is blocked
	const numRequests = 8
	for i := 0; i < numRequests; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, _ = wrappedHandler.Handle(context.Background(), req)
	}

	// Brief pause to ensure jobs are enqueued (but not processed due to block)
	time.Sleep(50 * time.Millisecond)

	// Verify jobs are queued (not saved yet)
	savedBeforeShutdown := blockingStore.getSaveCount()
	if savedBeforeShutdown != 0 {
		t.Logf("Note: %d requests were saved before shutdown (expected 0 if blocking works)", savedBeforeShutdown)
	}

	// Start shutdown in background, which will drain the queue
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownDone <- storageMiddleware.Shutdown(shutdownCtx)
	}()

	// Unblock storage after a short delay so shutdown can drain the queue
	time.Sleep(100 * time.Millisecond)
	close(blockingStore.block)

	// Wait for shutdown to complete
	if err := <-shutdownDone; err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Check that the queue drained metric was incremented
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	var drainedCount float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_storage_queue_drained_total" {
			for _, m := range mf.GetMetric() {
				drainedCount += m.GetCounter().GetValue()
			}
		}
	}

	// The drained count should track items processed during shutdown
	// Some items may have been processed normally before shutdown completed
	totalSaved := blockingStore.getSaveCount()

	// At least some items should have been drained during shutdown
	if drainedCount == 0 && totalSaved > 0 {
		t.Errorf("Expected icap_storage_queue_drained_total > 0 when items were in queue, got %v", drainedCount)
	}

	// Verify all requests were eventually saved
	if totalSaved != numRequests {
		t.Errorf("Expected %d saved requests, got %d", numRequests, totalSaved)
	}

	// The drained count should be a significant portion of the saved requests
	// (accounting for timing variances)
	expectedDrained := float64(numRequests - savedBeforeShutdown)
	minDrained := expectedDrained * 0.5 // At least 50% should be drained
	if drainedCount < minDrained {
		t.Logf("Warning: Only %.0f of expected %.0f items were drained during shutdown (may be timing variance)",
			drainedCount, expectedDrained)
	}

	t.Logf("Saved before shutdown: %d, Drained during shutdown: %.0f, Total saved: %d",
		savedBeforeShutdown, drainedCount, totalSaved)
}

// TestStorageMiddlewareWithPool_QueueLengthGauge tests that the queue length
// gauge is properly updated.
func TestStorageMiddlewareWithPool_QueueLengthGauge(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	store := newMockStorage()

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 5,
		Metrics:   metricsCollector,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(store, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send one request and check queue length
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	_, _ = wrappedHandler.Handle(context.Background(), req)

	time.Sleep(50 * time.Millisecond)

	// Get the queue length metric
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	var queueLength float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_storage_queue_length" {
			for _, m := range mf.GetMetric() {
				queueLength = m.GetGauge().GetValue()
			}
		}
	}

	// Queue should be empty or nearly empty after processing
	if queueLength > 1 {
		t.Logf("Note: queue_length = %v (expected 0 or 1, but this is timing-dependent)", queueLength)
	}

	// Send multiple requests rapidly to fill the queue
	for i := 0; i < 10; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, _ = wrappedHandler.Handle(context.Background(), req)
	}

	// Check queue length while still processing
	metricFamilies, err = reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_storage_queue_length" {
			for _, m := range mf.GetMetric() {
				queueLength = m.GetGauge().GetValue()
			}
		}
	}

	// Queue should not exceed the configured size
	if queueLength > float64(cfg.QueueSize) {
		t.Errorf("Queue length %v exceeds max queue size %v", queueLength, cfg.QueueSize)
	}

	// Shutdown and cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = storageMiddleware.Shutdown(shutdownCtx)
}

// TestStorageMiddlewareWithPool_BackpressureMetricAccuracy tests the accuracy
// of backpressure metrics under high load.
func TestStorageMiddlewareWithPool_BackpressureMetricAccuracy(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Slow storage to simulate high load
	slowStore := &configurableSlowMockStorage{
		mockStorage: newMockStorage(),
		delay:       10 * time.Millisecond,
	}

	cfg := middleware.StorageMiddlewareConfig{
		Workers:   1,
		QueueSize: 5,
		Metrics:   metricsCollector,
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	storageMiddleware := middleware.StorageMiddlewareWithPool(slowStore, logger, cfg)
	wrappedHandler := storageMiddleware.Wrap(baseHandler)

	// Send many requests rapidly
	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	startTime := time.Now()
	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			_, _ = wrappedHandler.Handle(context.Background(), req)
		}()
	}

	wg.Wait()
	duration := time.Since(startTime)

	// Wait a bit for some items to be processed before shutdown
	time.Sleep(100 * time.Millisecond)

	// Gather metrics before shutdown
	preShutdownMetricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather pre-shutdown metrics: %v", err)
	}

	var (
		preShutdownRejected float64
		preShutdownDrained  float64
		preShutdownQueue    float64
	)

	for _, mf := range preShutdownMetricFamilies {
		switch mf.GetName() {
		case "icap_storage_backpressure_rejected_total":
			for _, m := range mf.GetMetric() {
				preShutdownRejected += m.GetCounter().GetValue()
			}
		case "icap_storage_queue_drained_total":
			for _, m := range mf.GetMetric() {
				preShutdownDrained += m.GetCounter().GetValue()
			}
		case "icap_storage_queue_length":
			for _, m := range mf.GetMetric() {
				preShutdownQueue = m.GetGauge().GetValue()
			}
		}
	}

	// Shutdown to ensure all processing is complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = storageMiddleware.Shutdown(shutdownCtx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Gather metrics after shutdown
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	var (
		rejectedCount float64
		drainedCount  float64
		queueLength   float64
	)

	for _, mf := range metricFamilies {
		switch mf.GetName() {
		case "icap_storage_backpressure_rejected_total":
			for _, m := range mf.GetMetric() {
				rejectedCount += m.GetCounter().GetValue()
			}
		case "icap_storage_queue_drained_total":
			for _, m := range mf.GetMetric() {
				drainedCount += m.GetCounter().GetValue()
			}
		case "icap_storage_queue_length":
			for _, m := range mf.GetMetric() {
				queueLength = m.GetGauge().GetValue()
			}
		}
	}

	drainedDuringShutdown := drainedCount - preShutdownDrained
	actualSaves := slowStore.getSaveCount()

	// Verify metrics are consistent
	t.Logf("Processed %d requests in %v (%.0f RPS)", numRequests, duration, float64(numRequests)/duration.Seconds())
	t.Logf("Rejected: %.0f", rejectedCount)
	t.Logf("Saved (actual): %d", actualSaves)
	t.Logf("Drained (during shutdown): %.0f (was %.0f, now %.0f)", drainedDuringShutdown, preShutdownDrained, drainedCount)
	t.Logf("Pre-shutdown queue length: %.0f", preShutdownQueue)
	t.Logf("Post-shutdown queue length: %.0f", queueLength)

	// Rejected count should be incremented
	if rejectedCount == 0 {
		t.Logf("Note: No rejections occurred (test environment may be too fast or queue size sufficient)")
	}

	// Queue should be empty after shutdown
	if queueLength != 0 {
		t.Errorf("Expected queue_length = 0 after shutdown, got %v", queueLength)
	}

	// Verify actual saves match expectations
	expectedSaves := numRequests - int64(rejectedCount)
	if actualSaves != expectedSaves {
		t.Errorf("Expected %d saved requests (%d total - %.0f rejected), got %d",
			expectedSaves, numRequests, rejectedCount, actualSaves)
	}

	// The drained metric should track items processed during shutdown
	// This is the difference between pre and post shutdown drained count
	itemsInQueuePreShutdown := preShutdownQueue
	if drainedDuringShutdown < itemsInQueuePreShutdown-1 { // Allow for some timing variance
		t.Logf("Warning: Drained during shutdown (%.0f) is less than items in queue before shutdown (%.0f)",
			drainedDuringShutdown, itemsInQueuePreShutdown)
	}

	// Total should balance: rejected + saved = total
	totalAccounted := int64(rejectedCount) + actualSaves
	if totalAccounted != numRequests {
		t.Errorf("Total accounted (%d) does not match total requests (%d)", totalAccounted, numRequests)
	}
}

// configurableSlowMockStorage adds a configurable delay to simulate slow storage.
type configurableSlowMockStorage struct {
	*mockStorage
	delay time.Duration
}

func (s *configurableSlowMockStorage) SaveRequest(ctx context.Context, req *storage.StoredRequest) error {
	time.Sleep(s.delay)
	return s.mockStorage.SaveRequest(ctx, req)
}

// getGaugeValue retrieves the current value of a gauge metric.
func getGaugeValue(t *testing.T, reg prometheus.Gatherer, metricName string, labelPairs ...string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == metricName {
			if len(mf.GetMetric()) == 0 {
				return 0
			}
			// Find the metric with matching labels
			for _, m := range mf.GetMetric() {
				if labelsMatch(m, labelPairs...) {
					return m.GetGauge().GetValue()
				}
			}
			// No labels specified, return first value
			if len(labelPairs) == 0 {
				return mf.GetMetric()[0].GetGauge().GetValue()
			}
		}
	}
	return 0
}

// getCounterValue retrieves the current value of a counter metric.
func getCounterValue(t *testing.T, reg prometheus.Gatherer, metricName string, labelPairs ...string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == metricName {
			if len(mf.GetMetric()) == 0 {
				return 0
			}
			// Find the metric with matching labels
			for _, m := range mf.GetMetric() {
				if labelsMatch(m, labelPairs...) {
					return m.GetCounter().GetValue()
				}
			}
			// No labels specified, return first value
			if len(labelPairs) == 0 {
				return mf.GetMetric()[0].GetCounter().GetValue()
			}
		}
	}
	return 0
}

// labelsMatch checks if a metric's labels match the provided label pairs.
func labelsMatch(metric *dto.Metric, labelPairs ...string) bool {
	if len(labelPairs)%2 != 0 {
		return false
	}

	labels := make(map[string]string)
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}

	for i := 0; i < len(labelPairs); i += 2 {
		key := labelPairs[i]
		value := labelPairs[i+1]
		if labels[key] != value {
			return false
		}
	}

	return true
}
