// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/middleware"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/ratelimit"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestRegisterHandlers_RateLimiterIntegration tests that rate limiter middleware
// is properly integrated when enabled.
func TestRegisterHandlers_RateLimiterIntegration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.RateLimit.Enabled = true
	cfg.RateLimit.RequestsPerSecond = 100
	cfg.RateLimit.Burst = 150

	log, err := logger.New(cfg.Logging)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = log.Close()
	})

	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	limiter := ratelimit.NewTokenBucketLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	rtr := router.NewRouter()
	proc := processor.NewEchoProcessor()

	if err := registerHandlers(rtr, proc, collector, limiter, nil, cfg, log, nil, testServerEntry(cfg)); err != nil {
		t.Fatalf("registerHandlers() failed: %v", err)
	}
}

// TestRegisterHandlers_StorageMiddlewareIntegration tests that storage middleware
// is properly integrated when enabled.
func TestRegisterHandlers_StorageMiddlewareIntegration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.RateLimit.Enabled = false
	cfg.Storage.Enabled = true
	cfg.Storage.Workers = 2
	cfg.Storage.QueueSize = 10

	log, err := logger.New(cfg.Logging)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = log.Close()
	})

	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	tmpDir := t.TempDir()
	cfg.Storage.RequestsDir = tmpDir

	store, err := storage.NewFileStorage(cfg.Storage, nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	storageCfg := middleware.StorageMiddlewareConfig{
		Workers:   cfg.Storage.Workers,
		QueueSize: cfg.Storage.QueueSize,
	}
	storageMiddleware, err := middleware.NewStorageMiddlewareWithPool(store, log.Logger, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage middleware: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = storageMiddleware.Shutdown(ctx)
		time.Sleep(150 * time.Millisecond)
		_ = store.Close()
	})

	rtr := router.NewRouter()
	proc := processor.NewEchoProcessor()

	if err := registerHandlers(rtr, proc, collector, nil, storageMiddleware, cfg, log, nil, testServerEntry(cfg)); err != nil {
		t.Fatalf("registerHandlers() failed: %v", err)
	}
}

// TestRegisterHandlers_DisabledMiddleware tests that disabled middleware
// is not applied to handlers.
func TestRegisterHandlers_DisabledMiddleware(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.RateLimit.Enabled = false
	cfg.Storage.Enabled = false

	log, err := logger.New(cfg.Logging)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = log.Close()
	})

	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	rtr := router.NewRouter()
	proc := processor.NewEchoProcessor()

	if err := registerHandlers(rtr, proc, collector, nil, nil, cfg, log, nil, testServerEntry(cfg)); err != nil {
		t.Fatalf("registerHandlers() failed: %v", err)
	}
}

// TestRegisterHandlers_AllMiddleware tests that all middleware works together.
func TestRegisterHandlers_AllMiddleware(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.RateLimit.Enabled = true
	cfg.Storage.Enabled = true
	cfg.Storage.Workers = 2
	cfg.Storage.QueueSize = 10

	log, err := logger.New(cfg.Logging)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() {
		_ = log.Close()
	})

	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	tmpDir := t.TempDir()
	cfg.Storage.RequestsDir = tmpDir

	store, err := storage.NewFileStorage(cfg.Storage, nil)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	storageCfg := middleware.StorageMiddlewareConfig{
		Workers:   cfg.Storage.Workers,
		QueueSize: cfg.Storage.QueueSize,
	}
	storageMiddleware, err := middleware.NewStorageMiddlewareWithPool(store, log.Logger, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage middleware: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = storageMiddleware.Shutdown(ctx)
		time.Sleep(150 * time.Millisecond)
		_ = store.Close()
	})

	limiter := ratelimit.NewTokenBucketLimiter(1000, 100)
	rtr := router.NewRouter()
	proc := processor.NewEchoProcessor()

	if err := registerHandlers(rtr, proc, collector, limiter, storageMiddleware, cfg, log, nil, testServerEntry(cfg)); err != nil {
		t.Fatalf("registerHandlers() failed: %v", err)
	}
}

// TestMiddlewareChain_RateLimitPreventsRequests tests that rate limiter
// prevents excessive requests.
func TestMiddlewareChain_RateLimitPreventsRequests(t *testing.T) {
	t.Parallel()

	// Create a handler that returns success
	baseHandler := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	// Create a very strict rate limiter (only 1 request allowed)
	limiter := ratelimit.NewTokenBucketLimiter(1, 1)

	// Apply rate limiter middleware
	rateLimitMW := middleware.RateLimiterMiddleware(limiter)
	wrappedHandler := rateLimitMW(baseHandler)

	// First request should succeed
	req1, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp1, err1 := wrappedHandler.Handle(context.Background(), req1)

	if err1 != nil {
		t.Errorf("First request failed with error: %v", err1)
	}
	if resp1.StatusCode != icap.StatusOK {
		t.Errorf("First request StatusCode = %d, want %d", resp1.StatusCode, icap.StatusOK)
	}

	// Second request should be rate limited
	req2, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp2, err2 := wrappedHandler.Handle(context.Background(), req2)

	if err2 != nil {
		t.Errorf("Second request failed with error: %v", err2)
	}
	if resp2.StatusCode != 429 {
		t.Errorf("Second request StatusCode = %d, want 429", resp2.StatusCode)
	}
}

// TestMiddlewareChain_StorageSavesRequests tests that storage middleware
// saves requests properly.
func TestMiddlewareChain_StorageSavesRequests(t *testing.T) {
	t.Parallel()

	// Create mock storage
	store := &mockStorage{requests: make([]*storage.StoredRequest, 0)}

	// Create a handler that returns success
	baseHandler := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	// Create storage middleware with worker pool (replaces deprecated LegacyStorageMiddleware)
	storageCfg := middleware.StorageMiddlewareConfig{
		Workers:        1,
		QueueSize:      10,
		CircuitBreaker: middleware.CircuitBreakerConfig{Enabled: false},
	}
	storageMW, err := middleware.NewStorageMiddlewareWithPool(store, nil, storageCfg)
	if err != nil {
		t.Fatalf("NewStorageMiddlewareWithPool() failed: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = storageMW.Shutdown(ctx)
	})

	wrappedHandler := storageMW.Wrap(baseHandler)

	// Make a request
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Wait for async save to complete (worker pool processes in background)
	time.Sleep(100 * time.Millisecond)

	store.mu.Lock()
	savedCount := len(store.requests)
	store.mu.Unlock()

	if savedCount != 1 {
		t.Errorf("Expected 1 saved request, got %d", savedCount)
	}
}

func TestRegisterHandlers_StorageUsesPerServerMaxBodySize(t *testing.T) {
	const serverMaxBodySize int64 = 8
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Server.MaxBodySize = 1024
	log, err := logger.New(cfg.Logging)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	store := &mockStorage{requests: make([]*storage.StoredRequest, 0)}
	storageMW := testStorageMiddleware(t, store, log)
	rtr := router.NewRouter()
	proc := processor.NewEchoProcessor()

	entry := testServerEntry(cfg)
	entry.serverCfg.MaxBodySize = serverMaxBodySize
	err = registerHandlers(rtr, proc, nil, nil, storageMW, cfg, log, nil, entry)
	if err != nil {
		t.Fatalf("registerHandlers() failed: %v", err)
	}
	_, err = rtr.Serve(context.Background(), requestWithHTTPBody("0123456789"))
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	saved := waitForStoredRequest(t, store)

	if saved.HTTPRequest.BodyLimit != serverMaxBodySize {
		t.Fatalf("BodyLimit = %d, want %d", saved.HTTPRequest.BodyLimit, serverMaxBodySize)
	}
	if !saved.HTTPRequest.BodyTruncated {
		t.Fatal("BodyTruncated = false, want true")
	}
}

func testStorageMiddleware(t *testing.T, store storage.Storage, log *logger.Logger) *middleware.StorageMiddleware {
	t.Helper()
	cfg := middleware.StorageMiddlewareConfig{
		Workers:        1,
		QueueSize:      10,
		CircuitBreaker: middleware.CircuitBreakerConfig{Enabled: false},
	}
	storageMW, err := middleware.NewStorageMiddlewareWithPool(store, log.Logger, cfg)
	if err != nil {
		t.Fatalf("NewStorageMiddlewareWithPool() failed: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = storageMW.Shutdown(ctx)
	})
	return storageMW
}

func testServerEntry(cfg *config.Config) serverEntry {
	return serverEntry{name: "default", serviceID: cfg.Mock.ServiceID, serverCfg: cfg.Server}
}

func requestWithHTTPBody(body string) *icap.Request {
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
	req.HTTPRequest = &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPRequest.BodyReader = bytes.NewReader([]byte(body))
	return req
}

func waitForStoredRequest(t *testing.T, store *mockStorage) *storage.StoredRequest {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if req := lastStoredRequest(store); req != nil {
			return req
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for stored request")
	return nil
}

func lastStoredRequest(store *mockStorage) *storage.StoredRequest {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.requests) == 0 {
		return nil
	}
	return store.requests[len(store.requests)-1]
}

// mockStorage is a simple in-memory storage for testing.
type mockStorage struct {
	requests []*storage.StoredRequest
	mu       sync.RWMutex
}

func (m *mockStorage) SaveRequest(_ context.Context, req *storage.StoredRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return nil
}

func (m *mockStorage) GetRequest(_ context.Context, _ string) (*storage.StoredRequest, error) {
	return nil, storage.ErrRequestNotFound
}

func (m *mockStorage) ListRequests(_ context.Context, _ storage.RequestFilter) ([]*storage.StoredRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.requests, nil
}

func (m *mockStorage) DeleteRequest(_ context.Context, _ string) error {
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) Flush(_ context.Context) error {
	return nil
}

func (m *mockStorage) Clear(_ context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := int64(len(m.requests))
	m.requests = make([]*storage.StoredRequest, 0)
	return count, nil
}

func (m *mockStorage) DeleteRequests(_ context.Context, _ storage.RequestFilter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := int64(len(m.requests))
	m.requests = make([]*storage.StoredRequest, 0)
	return count, nil
}
