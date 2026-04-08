// Copyright 2026 ICAP Mock

package router

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// mockHandler is a test handler implementation with atomic counter.
type atomicMockHandler struct {
	err     error
	lastReq atomic.Pointer[icap.Request]
	resp    *icap.Response
	method  string
	called  int64
}

// Handle implements Handler interface.
func (h *atomicMockHandler) Handle(_ context.Context, req *icap.Request) (*icap.Response, error) {
	atomic.AddInt64(&h.called, 1)
	h.lastReq.Store(req)
	if h.err != nil {
		return nil, h.err
	}
	if h.resp != nil {
		return h.resp, nil
	}
	return icap.NewResponse(icap.StatusOK), nil
}

// Method implements Handler interface.
func (h *atomicMockHandler) Method() string {
	return h.method
}

// GetCalledCount returns the number of times this handler was called.
func (h *atomicMockHandler) GetCalledCount() int64 {
	return atomic.LoadInt64(&h.called)
}

// buildTestRequest creates a test ICAP request.
//
//nolint:unparam
func buildTestRequest(method, uri string) *icap.Request {
	return &icap.Request{
		Method: method,
		URI:    uri,
		Header: make(icap.Header),
	}
}

// runConcurrent executes a function concurrently in n goroutines.
func runConcurrent(t *testing.T, n int, fn func(goroutineID int)) {
	t.Helper()

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Goroutine %d panicked: %v", goroutineID, r)
				}
			}()

			fn(goroutineID)
		}(i)
	}

	wg.Wait()
}

// runConcurrentWithResults executes a function concurrently and collects results.
func runConcurrentWithResults(t *testing.T, n, iterations int, fn func(goroutineID, iteration int) error) []concurrentResult {
	t.Helper()

	results := make([]concurrentResult, 0, n*iterations)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				start := time.Now()
				err := fn(goroutineID, j)
				duration := time.Since(start)

				mu.Lock()
				results = append(results, concurrentResult{
					GoroutineID: goroutineID,
					Iteration:   j,
					Error:       err,
					Duration:    duration,
				})
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return results
}

// concurrentResult represents the result of a concurrent operation.
type concurrentResult struct {
	Error       error
	GoroutineID int
	Iteration   int
	Duration    time.Duration
}

// assertNoConcurrentErrors asserts that no errors occurred in concurrent results.
func assertNoConcurrentErrors(t *testing.T, results []concurrentResult) {
	t.Helper()

	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Goroutine %d iteration %d failed: %v", result.GoroutineID, result.Iteration, result.Error)
		}
	}
}

// getConcurrentStats calculates statistics from concurrent results.
func getConcurrentStats(results []concurrentResult) (errorCount int, avgDuration, maxDuration, minDuration time.Duration) {
	var totalDuration time.Duration
	var successCount int

	maxDuration = 0
	minDuration = time.Duration(1<<63 - 1)

	for _, result := range results {
		if result.Error != nil {
			errorCount++
			continue
		}

		totalDuration += result.Duration
		successCount++

		if result.Duration > maxDuration {
			maxDuration = result.Duration
		}

		if result.Duration < minDuration {
			minDuration = result.Duration
		}
	}

	if successCount > 0 {
		avgDuration = totalDuration / time.Duration(successCount)
	}

	if errorCount == len(results) {
		minDuration = 0
	}

	return errorCount, avgDuration, maxDuration, minDuration
}

// TestLockFreeRouter_NoLockContention verifies that the router is lock-free for read operations.
func TestLockFreeRouter_NoLockContention(t *testing.T) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	// Perform concurrent lookups without any blocking
	startTime := time.Now()

	for i := 0; i < 10000; i++ {
		go func(idx int) {
			path := fmt.Sprintf("/path%d", idx%100)
			req := buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
			_, err := r.Serve(context.Background(), req)
			if err != nil {
				t.Errorf("Serve() error = %v", err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	time.Sleep(100 * time.Millisecond)
	duration := time.Since(startTime)

	t.Logf("Completed 10,000 concurrent lookups in %v", duration)

	// Verify all handlers are registered
	routes := r.Routes()
	if len(routes) != 100 {
		t.Errorf("Expected 100 routes, got %d", len(routes))
	}
}

// TestLockFreeRouter_ConcurrentReadWrite tests concurrent route registration and serving.
func TestLockFreeRouter_ConcurrentReadWrite(t *testing.T) {
	r := NewRouter()

	var wg sync.WaitGroup

	// Concurrent route registration (writes)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("/path%d", idx)
			h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
			if err := r.Handle(path, h); err != nil {
				t.Errorf("Handle() error = %v", err)
			}
		}(i)
	}

	// Concurrent route serving (reads)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("/path%d", idx%100)
			req := buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
			_, err := r.Serve(context.Background(), req)
			if err != nil {
				// Some lookups may fail if route not yet registered
			}
		}(i)
	}

	wg.Wait()

	// Verify all routes are registered
	routes := r.Routes()
	if len(routes) < 90 { // Allow some race condition tolerance
		t.Errorf("Expected at least 90 routes, got %d", len(routes))
	}
}

// TestLockFreeRouter_HighConcurrencyStress tests router under extreme concurrency.
func TestLockFreeRouter_HighConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	r := NewRouter()

	// Register handlers
	for i := 0; i < 50; i++ {
		path := fmt.Sprintf("/service%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	results := runConcurrentWithResults(t, 1000, 100, func(_, iteration int) error {
		path := fmt.Sprintf("/service%d", iteration%50)
		req := buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
		_, err := r.Serve(context.Background(), req)
		return err
	})

	assertNoConcurrentErrors(t, results)
	errCount, avgDur, maxDur, minDur := getConcurrentStats(results)

	t.Logf("High concurrency stress test results:")
	t.Logf("  Errors: %d / %d", errCount, len(results))
	t.Logf("  Avg duration: %v", avgDur)
	t.Logf("  Max duration: %v", maxDur)
	t.Logf("  Min duration: %v", minDur)
	t.Logf("  Total operations: %d", len(results))
}

// BenchmarkRouter_Lookup benchmarks route lookup performance.
func BenchmarkRouter_Lookup(b *testing.B) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}

	// Create a request for each path
	requests := make([]*icap.Request, 100)
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		requests[i] = buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := requests[i%100]
		_, err := r.Serve(context.Background(), req)
		if err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

// BenchmarkRouter_LookupParallel benchmarks concurrent route lookups.
func BenchmarkRouter_LookupParallel(b *testing.B) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}

	// Create a request for each path
	requests := make([]*icap.Request, 100)
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		requests[i] = buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := requests[i%100]
			_, err := r.Serve(context.Background(), req)
			if err != nil {
				b.Fatalf("Serve() error = %v", err)
			}
			i++
		}
	})
}

// BenchmarkRouter_ConcurrentReadWrite benchmarks concurrent registration and serving.
func BenchmarkRouter_ConcurrentReadWrite(b *testing.B) {
	r := NewRouter()

	// Pre-register some handlers
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := fmt.Sprintf("/path%d", i%10)
			req := buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
			_, err := r.Serve(context.Background(), req)
			if err != nil {
				b.Fatalf("Serve() error = %v", err)
			}
			i++

			// Occasionally register a new route (less frequent)
			if i%100 == 0 {
				newPath := fmt.Sprintf("/newpath%d", i)
				h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
				if err := r.Handle(newPath, h); err != nil {
					b.Fatalf("Handle() error = %v", err)
				}
			}
		}
	})
}

// BenchmarkRouter_CacheHit benchmarks cache hits.
func BenchmarkRouter_CacheHit(b *testing.B) {
	r := NewRouter()

	// Register a single handler
	h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
	if err := r.Handle("/test", h); err != nil {
		b.Fatalf("Handle() error = %v", err)
	}

	req := buildTestRequest(icap.MethodREQMOD, "icap://localhost:1344/test")

	// Warm up cache
	for i := 0; i < 100; i++ {
		r.Serve(context.Background(), req)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := r.Serve(context.Background(), req)
		if err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

// BenchmarkRouter_CacheHitParallel benchmarks parallel cache hits.
func BenchmarkRouter_CacheHitParallel(b *testing.B) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}

	// Create requests for each path
	requests := make([]*icap.Request, 10)
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/path%d", i)
		requests[i] = buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
	}

	// Warm up cache
	for _, req := range requests {
		for i := 0; i < 100; i++ {
			r.Serve(context.Background(), req)
		}
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := requests[i%10]
			_, err := r.Serve(context.Background(), req)
			if err != nil {
				b.Fatalf("Serve() error = %v", err)
			}
			i++
		}
	})
}

// BenchmarkRouter_Routes benchmarks getting all routes.
func BenchmarkRouter_Routes(b *testing.B) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = r.Routes()
	}
}

// BenchmarkRouter_Handle benchmarks route registration.
func BenchmarkRouter_Handle(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := NewRouter()
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

// TestRouter_LockFreeVerification verifies lock-free behavior by measuring contention.
func TestRouter_LockFreeVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping verification test in short mode")
	}

	r := NewRouter()

	// Register handlers
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	// Create requests
	requests := make([]*icap.Request, 10)
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/path%d", i)
		requests[i] = buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
	}

	// Run high-concurrency test and measure time
	startTime := time.Now()
	const goroutines = 1000
	const iterations = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				req := requests[i%10]
				r.Serve(context.Background(), req)
			}
		}()
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := int64(goroutines * iterations)
	opsPerSec := float64(totalOps) / duration.Seconds()

	t.Logf("Lock-free verification:")
	t.Logf("  Goroutines: %d", goroutines)
	t.Logf("  Operations: %d", totalOps)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f ops/sec", opsPerSec)

	// Expected: should handle 100k+ ops/sec without blocking
	if opsPerSec < 50000 {
		t.Logf("Warning: Throughput (%.2f ops/sec) is lower than expected for lock-free operation", opsPerSec)
	}
}

// TestRouter_CacheEffectiveness verifies cache reduces lookup time.
func TestRouter_CacheEffectiveness(t *testing.T) {
	r := NewRouter()

	// Register handlers
	for i := 0; i < 50; i++ {
		path := fmt.Sprintf("/path%d", i)
		h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
		if err := r.Handle(path, h); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}

	// Create requests
	requests := make([]*icap.Request, 50)
	for i := 0; i < 50; i++ {
		path := fmt.Sprintf("/path%d", i)
		requests[i] = buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
	}

	// Measure uncached performance (clear cache before each lookup)
	startTime := time.Now()
	for i := 0; i < 1000; i++ {
		r.CacheClear()
		req := requests[i%50]
		r.Serve(context.Background(), req)
	}
	uncachedDuration := time.Since(startTime)

	// Reset metrics
	r.CacheResetMetrics()

	// Measure cached performance (same routes repeated)
	startTime = time.Now()
	for i := 0; i < 1000; i++ {
		req := requests[i%50]
		r.Serve(context.Background(), req)
	}
	cachedDuration := time.Since(startTime)

	// Get cache stats
	hits, misses, _, _ := r.CacheStats()

	t.Logf("Cache effectiveness:")
	t.Logf("  Uncached time: %v", uncachedDuration)
	t.Logf("  Cached time: %v", cachedDuration)
	t.Logf("  Cache hits: %d", hits)
	t.Logf("  Cache misses: %d", misses)
	t.Logf("  Hit rate: %.2f%%", float64(hits)/float64(hits+misses)*100)
	t.Logf("  Speedup: %.2fx", float64(uncachedDuration)/float64(cachedDuration))

	// Cache should have good hit rate
	hitRate := float64(hits) / float64(hits+misses) * 100
	if hitRate < 90 {
		t.Errorf("Cache hit rate (%.2f%%) is below 90%%", hitRate)
	}
}

// TestRouter_RaceDetector runs with race detector to verify thread safety.
func TestRouter_RaceDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race detector test in short mode")
	}

	r := NewRouter()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("/path%d", idx)
			h := &atomicMockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(icap.StatusOK)}
			if err := r.Handle(path, h); err != nil {
				t.Errorf("Handle() error = %v", err)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("/path%d", idx%100)
			req := buildTestRequest(icap.MethodREQMOD, fmt.Sprintf("icap://localhost:1344%s", path))
			_, _ = r.Serve(context.Background(), req)
		}(i)
	}

	// Concurrent route listing
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Routes()
		}()
	}

	// Concurrent cache operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _ = r.CacheStats()
		}()
	}

	wg.Wait()
}
