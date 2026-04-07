// Copyright 2026 ICAP Mock

package router

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestNewRouteCache tests creating a new route cache.
func TestNewRouteCache(t *testing.T) {
	cache := NewRouteCache(100, 5*time.Minute)

	if cache == nil {
		t.Fatal("NewRouteCache() returned nil")
	}

	if cache.maxEntries != 100 {
		t.Errorf("maxEntries = %d, want 100", cache.maxEntries)
	}

	if cache.ttl != 5*time.Minute {
		t.Errorf("ttl = %v, want 5m", cache.ttl)
	}
}

// TestNewRouteCache_Defaults tests creating cache with default parameters.
func TestNewRouteCache_Defaults(t *testing.T) {
	cache := NewRouteCache(0, 0)

	if cache.maxEntries != DefaultMaxEntries {
		t.Errorf("maxEntries = %d, want %d", cache.maxEntries, DefaultMaxEntries)
	}

	if cache.ttl != 0 {
		t.Errorf("ttl = %v, want 0", cache.ttl)
	}
}

// TestRouteCache_Get_Hit tests cache hit returns handler.
func TestRouteCache_Get_Hit(t *testing.T) {
	cache := NewRouteCache(100, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	cache.Put(icap.MethodREQMOD, "/test", h)

	handler, hit := cache.Get(icap.MethodREQMOD, "/test")
	if !hit {
		t.Error("Expected cache hit, got miss")
	}

	if handler == nil {
		t.Fatal("Expected non-nil handler on cache hit")
	}

	if handler != h {
		t.Error("Returned handler does not match cached handler")
	}
}

// TestRouteCache_Get_Miss tests cache miss returns nil.
func TestRouteCache_Get_Miss(t *testing.T) {
	cache := NewRouteCache(100, 0)

	handler, hit := cache.Get(icap.MethodREQMOD, "/nonexistent")
	if hit {
		t.Error("Expected cache miss, got hit")
	}

	if handler != nil {
		t.Error("Expected nil handler on cache miss")
	}

	if cache.Misses() != 1 {
		t.Errorf("misses = %d, want 1", cache.Misses())
	}
}

// TestRouteCache_Get_Expired tests expired entries return miss.
func TestRouteCache_Get_Expired(t *testing.T) {
	cache := NewRouteCache(100, 10*time.Millisecond)
	h := &mockHandler{method: icap.MethodREQMOD}

	cache.Put(icap.MethodREQMOD, "/test", h)

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	handler, hit := cache.Get(icap.MethodREQMOD, "/test")
	if hit {
		t.Error("Expected cache miss for expired entry")
	}

	if handler != nil {
		t.Error("Expected nil handler for expired entry")
	}
}

// TestRouteCache_Put tests storing a handler in the cache.
func TestRouteCache_Put(t *testing.T) {
	cache := NewRouteCache(100, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	cache.Put(icap.MethodREQMOD, "/test", h)

	if cache.Size() != 1 {
		t.Errorf("cache size = %d, want 1", cache.Size())
	}

	handler, hit := cache.Get(icap.MethodREQMOD, "/test")
	if !hit || handler == nil {
		t.Error("Cache did not store handler correctly")
	}
}

// TestRouteCache_Put_NilHandler tests that nil handler is not cached.
func TestRouteCache_Put_NilHandler(t *testing.T) {
	cache := NewRouteCache(100, 0)

	cache.Put(icap.MethodREQMOD, "/test", nil)

	if cache.Size() != 0 {
		t.Errorf("cache size = %d, want 0 (nil handler should not be cached)", cache.Size())
	}
}

// TestRouteCache_Put_Update tests updating an existing cache entry.
func TestRouteCache_Put_Update(t *testing.T) {
	cache := NewRouteCache(100, 0)
	h1 := &mockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(200)}
	h2 := &mockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(204)}

	cache.Put(icap.MethodREQMOD, "/test", h1)
	if cache.Size() != 1 {
		t.Errorf("cache size = %d, want 1", cache.Size())
	}

	// Update with new handler
	cache.Put(icap.MethodREQMOD, "/test", h2)
	if cache.Size() != 1 {
		t.Errorf("cache size = %d, want 1 (update should not increase size)", cache.Size())
	}

	handler, _ := cache.Get(icap.MethodREQMOD, "/test")
	if handler != h2 {
		t.Error("Cache did not update handler correctly")
	}
}

// TestRouteCache_LRUEviction tests LRU eviction when cache is full.
func TestRouteCache_LRUEviction(t *testing.T) {
	maxEntries := 5
	cache := NewRouteCache(maxEntries, 0)

	// Fill cache to capacity
	for i := 0; i < maxEntries; i++ {
		path := "/path" + fmt.Sprint(i)
		h := &mockHandler{method: icap.MethodREQMOD}
		cache.Put(icap.MethodREQMOD, path, h)
	}

	if cache.Size() != maxEntries {
		t.Fatalf("cache size = %d, want %d", cache.Size(), maxEntries)
	}

	// Access /path3 (not LRU)
	cache.Get(icap.MethodREQMOD, "/path3")

	// Add one more entry (should evict /path0)
	hNew := &mockHandler{method: icap.MethodREQMOD}
	cache.Put(icap.MethodREQMOD, "/new", hNew)

	if cache.Size() != maxEntries {
		t.Errorf("cache size = %d, want %d (should remain at capacity)", cache.Size(), maxEntries)
	}

	// Verify /path0 was evicted (LRU)
	_, hit := cache.Get(icap.MethodREQMOD, "/path0")
	if hit {
		t.Error("LRU entry /path0 should have been evicted")
	}

	// Verify /path3 is still cached (recently accessed)
	_, hit = cache.Get(icap.MethodREQMOD, "/path3")
	if !hit {
		t.Error("Recently accessed /path3 should still be cached")
	}

	// Verify /new is cached
	_, hit = cache.Get(icap.MethodREQMOD, "/new")
	if !hit {
		t.Error("New entry /new should be cached")
	}

	// Verify one eviction occurred
	if cache.Evictions() != 1 {
		t.Errorf("evictions = %d, want 1", cache.Evictions())
	}
}

// TestRouteCache_Delete tests deleting entries from the cache.
func TestRouteCache_Delete(t *testing.T) {
	cache := NewRouteCache(100, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	cache.Put(icap.MethodREQMOD, "/test", h)
	if cache.Size() != 1 {
		t.Errorf("cache size = %d, want 1", cache.Size())
	}

	cache.Delete(icap.MethodREQMOD, "/test")
	if cache.Size() != 0 {
		t.Errorf("cache size = %d, want 0 after delete", cache.Size())
	}

	_, hit := cache.Get(icap.MethodREQMOD, "/test")
	if hit {
		t.Error("Entry should not exist after delete")
	}
}

// TestRouteCache_Clear tests clearing all entries from the cache.
func TestRouteCache_Clear(t *testing.T) {
	cache := NewRouteCache(100, 0)

	// Add multiple entries
	for i := 0; i < 10; i++ {
		path := "/path" + fmt.Sprint(i)
		h := &mockHandler{method: icap.MethodREQMOD}
		cache.Put(icap.MethodREQMOD, path, h)
	}

	if cache.Size() != 10 {
		t.Fatalf("cache size = %d, want 10", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("cache size = %d, want 0 after clear", cache.Size())
	}

	// Verify no entries remain
	for i := 0; i < 10; i++ {
		path := "/path" + fmt.Sprint(i)
		_, hit := cache.Get(icap.MethodREQMOD, path)
		if hit {
			t.Errorf("Entry %s should not exist after clear", path)
		}
	}
}

// TestRouteCache_Size tests getting the cache size.
func TestRouteCache_Size(t *testing.T) {
	cache := NewRouteCache(100, 0)

	if cache.Size() != 0 {
		t.Errorf("cache size = %d, want 0", cache.Size())
	}

	h := &mockHandler{method: icap.MethodREQMOD}
	cache.Put(icap.MethodREQMOD, "/test", h)

	if cache.Size() != 1 {
		t.Errorf("cache size = %d, want 1", cache.Size())
	}
}

// TestRouteCache_Hits tests tracking cache hits.
func TestRouteCache_Hits(t *testing.T) {
	cache := NewRouteCache(100, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	cache.Put(icap.MethodREQMOD, "/test", h)

	if cache.Hits() != 0 {
		t.Errorf("hits = %d, want 0", cache.Hits())
	}

	// First hit
	cache.Get(icap.MethodREQMOD, "/test")
	if cache.Hits() != 1 {
		t.Errorf("hits = %d, want 1", cache.Hits())
	}

	// Second hit
	cache.Get(icap.MethodREQMOD, "/test")
	if cache.Hits() != 2 {
		t.Errorf("hits = %d, want 2", cache.Hits())
	}
}

// TestRouteCache_Misses tests tracking cache misses.
func TestRouteCache_Misses(t *testing.T) {
	cache := NewRouteCache(100, 0)

	if cache.Misses() != 0 {
		t.Errorf("misses = %d, want 0", cache.Misses())
	}

	// First miss
	cache.Get(icap.MethodREQMOD, "/nonexistent")
	if cache.Misses() != 1 {
		t.Errorf("misses = %d, want 1", cache.Misses())
	}

	// Second miss
	cache.Get(icap.MethodREQMOD, "/another")
	if cache.Misses() != 2 {
		t.Errorf("misses = %d, want 2", cache.Misses())
	}
}

// TestRouteCache_Evictions tests tracking cache evictions.
func TestRouteCache_Evictions(t *testing.T) {
	cache := NewRouteCache(2, 0)

	h := &mockHandler{method: icap.MethodREQMOD}

	// Fill cache
	cache.Put(icap.MethodREQMOD, "/path1", h)
	cache.Put(icap.MethodREQMOD, "/path2", h)

	if cache.Evictions() != 0 {
		t.Errorf("evictions = %d, want 0", cache.Evictions())
	}

	// Add third entry (should evict first)
	cache.Put(icap.MethodREQMOD, "/path3", h)

	if cache.Evictions() != 1 {
		t.Errorf("evictions = %d, want 1", cache.Evictions())
	}
}

// TestRouteCache_ResetMetrics tests resetting cache metrics.
func TestRouteCache_ResetMetrics(t *testing.T) {
	cache := NewRouteCache(2, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Generate some activity
	cache.Put(icap.MethodREQMOD, "/test", h)
	cache.Get(icap.MethodREQMOD, "/test") // hit
	cache.Get(icap.MethodREQMOD, "/miss") // miss
	cache.Put(icap.MethodREQMOD, "/test2", h)
	cache.Put(icap.MethodREQMOD, "/test3", h) // eviction (cache full)

	if cache.Hits() != 1 {
		t.Errorf("hits = %d, want 1 before reset", cache.Hits())
	}
	if cache.Misses() != 1 {
		t.Errorf("misses = %d, want 1 before reset", cache.Misses())
	}
	if cache.Evictions() != 1 {
		t.Errorf("evictions = %d, want 1 before reset", cache.Evictions())
	}

	// Reset metrics
	cache.ResetMetrics()

	if cache.Hits() != 0 {
		t.Errorf("hits = %d, want 0 after reset", cache.Hits())
	}
	if cache.Misses() != 0 {
		t.Errorf("misses = %d, want 0 after reset", cache.Misses())
	}
	if cache.Evictions() != 0 {
		t.Errorf("evictions = %d, want 0 after reset", cache.Evictions())
	}
}

// TestRouteCache_ConcurrentAccess tests thread-safe cache operations.
func TestRouteCache_ConcurrentAccess(t *testing.T) {
	cache := NewRouteCache(100, 0)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/path" + fmt.Sprint(i%10)
			h := &mockHandler{method: icap.MethodREQMOD}
			cache.Put(icap.MethodREQMOD, path, h)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/path" + fmt.Sprint(i%10)
			cache.Get(icap.MethodREQMOD, path)
		}(i)
	}

	// Concurrent deletes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/path" + fmt.Sprint(i%10)
			cache.Delete(icap.MethodREQMOD, path)
		}(i)
	}

	wg.Wait()
	// If we get here without race condition, test passes
}

// TestRouteCache_LookupHandler tests cache-aware handler lookup.
func TestRouteCache_LookupHandler(t *testing.T) {
	cache := NewRouteCache(100, 0)
	routes := make(map[string]handler.Handler)

	h1 := &mockHandler{method: icap.MethodREQMOD}
	h2 := &mockHandler{method: icap.MethodRESPMOD}

	routes["/reqmod"] = h1
	routes["/respmod"] = h2

	// Test first lookup (cache miss)
	req1, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	handler, hit := cache.LookupHandler(routes, req1)
	if hit {
		t.Error("Expected cache miss on first lookup")
	}
	if handler != h1 {
		t.Error("Handler mismatch on first lookup")
	}

	// Test second lookup (cache hit)
	req2, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	handler, hit = cache.LookupHandler(routes, req2)
	if !hit {
		t.Error("Expected cache hit on second lookup")
	}
	if handler != h1 {
		t.Error("Handler mismatch on cache hit")
	}

	// Test unknown path (cache miss, no handler)
	req3, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost:1344/unknown")
	handler, hit = cache.LookupHandler(routes, req3)
	if hit {
		t.Error("Expected cache miss for unknown path")
	}
	if handler != nil {
		t.Error("Expected nil handler for unknown path")
	}
}

// TestRouter_WithCache tests router with cache enabled.
func TestRouter_WithCache(t *testing.T) {
	r := NewRouter()
	h := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", h)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// First request (cache miss)
	req1, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	resp1, err := r.Serve(context.Background(), req1)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if resp1.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp1.StatusCode, icap.StatusOK)
	}

	// Second request (cache hit)
	req2, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	resp2, err := r.Serve(context.Background(), req2)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if resp2.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp2.StatusCode, icap.StatusOK)
	}

	// Verify cache has activity
	hits, misses, _, _ := r.CacheStats()
	if misses != 1 {
		t.Errorf("cache misses = %d, want 1 (first lookup)", misses)
	}
	if hits != 1 {
		t.Errorf("cache hits = %d, want 1 (second lookup)", hits)
	}
}

// TestRouter_CacheClear tests clearing the router cache.
func TestRouter_CacheClear(t *testing.T) {
	r := NewRouter()
	h := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", h)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Make a request to populate cache
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	r.Serve(context.Background(), req)

	// Verify cache has entries
	_, _, _, size := r.CacheStats()
	if size != 1 {
		t.Errorf("cache size = %d, want 1", size)
	}

	// Clear cache
	r.CacheClear()

	// Verify cache is empty
	_, _, _, size = r.CacheStats()
	if size != 0 {
		t.Errorf("cache size = %d, want 0 after clear", size)
	}
}

// TestRouter_CacheResetMetrics tests resetting router cache metrics.
func TestRouter_CacheResetMetrics(t *testing.T) {
	r := NewRouter()
	h := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", h)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Make requests to generate cache activity
	for i := 0; i < 5; i++ {
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
		r.Serve(context.Background(), req)
	}

	// Verify metrics are non-zero
	hits, misses, _, _ := r.CacheStats()
	if hits == 0 || misses == 0 {
		t.Error("Expected non-zero cache metrics")
	}

	// Reset metrics
	r.CacheResetMetrics()

	// Verify metrics are zero
	hits, misses, _, _ = r.CacheStats()
	if hits != 0 {
		t.Errorf("hits = %d, want 0 after reset", hits)
	}
	if misses != 0 {
		t.Errorf("misses = %d, want 0 after reset", misses)
	}
}

// TestRouter_CacheInvalidatedOnRouteUpdate tests that cache is invalidated when routes are updated.
func TestRouter_CacheInvalidatedOnRouteUpdate(t *testing.T) {
	r := NewRouter()
	h1 := &mockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(200)}

	err := r.Handle("/reqmod", h1)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Make a request to populate cache
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	r.Serve(context.Background(), req)

	// Verify cache has entries
	_, _, _, size := r.CacheStats()
	if size != 1 {
		t.Errorf("cache size = %d, want 1", size)
	}

	// Update route with new handler
	h2 := &mockHandler{method: icap.MethodREQMOD, resp: icap.NewResponse(204)}
	err = r.Handle("/reqmod", h2)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Verify cache was cleared (size should be 0)
	_, _, _, size = r.CacheStats()
	if size != 0 {
		t.Errorf("cache size = %d, want 0 after route update", size)
	}

	// Make another request and verify it uses new handler
	req, _ = icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	resp, err := r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if resp.StatusCode != 204 {
		t.Errorf("StatusCode = %d, want 204 (new handler)", resp.StatusCode)
	}
}

// BenchmarkRouter_Serve_WithoutCache benchmarks router serving without cache.
func BenchmarkRouter_Serve_WithoutCache(b *testing.B) {
	r := NewRouter()
	h := &mockHandler{method: icap.MethodREQMOD}

	r.Handle("/reqmod", h)
	r.CacheClear() // Ensure cache is empty

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Serve(context.Background(), req)
	}
}

// BenchmarkRouter_Serve_WithCache benchmarks router serving with cache.
func BenchmarkRouter_Serve_WithCache(b *testing.B) {
	r := NewRouter()
	h := &mockHandler{method: icap.MethodREQMOD}

	r.Handle("/reqmod", h)

	// Warm up cache
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	r.Serve(context.Background(), req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Serve(context.Background(), req)
	}
}

// BenchmarkRouteCache_Get benchmarks cache get operations.
func BenchmarkRouteCache_Get(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Warm up cache
	cache.Put(icap.MethodREQMOD, "/test", h)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(icap.MethodREQMOD, "/test")
	}
}

// BenchmarkRouteCache_Put benchmarks cache put operations.
func BenchmarkRouteCache_Put(b *testing.B) {
	cache := NewRouteCache(1000, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := &mockHandler{method: icap.MethodREQMOD}
		cache.Put(icap.MethodREQMOD, "/test", h)
	}
}

// BenchmarkRouteCache_LRUEviction benchmarks cache eviction performance.
func BenchmarkRouteCache_LRUEviction(b *testing.B) {
	cache := NewRouteCache(100, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := &mockHandler{method: icap.MethodREQMOD}
		path := "/path" + string(rune('0'+i%100))
		cache.Put(icap.MethodREQMOD, path, h)
	}
}

// BenchmarkRouteCache_O1_GetHit benchmarks cache get with O(1) LRU update.
// This demonstrates the improvement of doubly-linked list over slice-based approach.
func BenchmarkRouteCache_O1_GetHit(b *testing.B) {
	cache := NewRouteCache(10000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Alternate between different keys to exercise LRU updates
		path := "/path" + fmt.Sprint(i%10000)
		cache.Get(icap.MethodREQMOD, path)
	}
}

// BenchmarkRouteCache_O1_SequentialAccess benchmarks sequential access pattern.
// In a doubly-linked list, sequential access with LRU updates is O(1) per operation.
func BenchmarkRouteCache_O1_SequentialAccess(b *testing.B) {
	cache := NewRouteCache(10000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Access keys in order, triggering LRU updates
		path := "/path" + fmt.Sprint(i%10000)
		cache.Get(icap.MethodREQMOD, path)
	}
}

// BenchmarkRouteCache_O1_RandomAccess benchmarks random access pattern.
// With doubly-linked list, both map lookup and LRU update are O(1).
func BenchmarkRouteCache_O1_RandomAccess(b *testing.B) {
	cache := NewRouteCache(10000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, keys[i], h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Random key access
		idx := (i * 7) % 10000 // pseudo-random
		cache.Get(icap.MethodREQMOD, keys[idx])
	}
}

// BenchmarkRouteCache_O1_EvictionHeavy benchmarks heavy eviction workload.
// Tests O(1) eviction performance under constant churn.
func BenchmarkRouteCache_O1_EvictionHeavy(b *testing.B) {
	cache := NewRouteCache(1000, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := &mockHandler{method: icap.MethodREQMOD}
		// Constant churn - always adding new entries
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}
}

// BenchmarkRouteCache_O1_MixedWorkload benchmarks realistic mixed workload.
// Combines reads, writes, and evictions to demonstrate overall performance.
func BenchmarkRouteCache_O1_MixedWorkload(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache with 500 entries
	for i := 0; i < 500; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%10 == 0 {
			// 10% writes
			path := "/path" + fmt.Sprint(i+500)
			cache.Put(icap.MethodREQMOD, path, h)
		} else {
			// 90% reads
			path := "/path" + fmt.Sprint(i%500)
			cache.Get(icap.MethodREQMOD, path)
		}
	}
}

// BenchmarkRouteCache_MoveToFront benchmarks the O(1) moveToFront operation.
// This is the key operation that benefits from doubly-linked list.
func BenchmarkRouteCache_MoveToFront(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Repeatedly access same key to test moveToFront
		path := "/path999"
		cache.Get(icap.MethodREQMOD, path)
	}
}

// BenchmarkRouteCache_Update benchmarks cache entry updates.
// Updates should be O(1) with doubly-linked list.
func BenchmarkRouteCache_Update(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h1 := &mockHandler{method: icap.MethodREQMOD}
	h2 := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := "/path" + fmt.Sprint(i%1000)
		cache.Put(icap.MethodREQMOD, path, h2)
	}
}

// BenchmarkRouteCache_Delete benchmarks delete operations.
// Delete should be O(1) with doubly-linked list.
func BenchmarkRouteCache_Delete(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := "/path" + fmt.Sprint(i%1000)
		cache.Delete(icap.MethodREQMOD, path)
	}
}

// BenchmarkRouteCache_ConcurrentReadWrite benchmarks concurrent operations.
// Tests thread-safety and lock contention with O(1) operations.
func BenchmarkRouteCache_ConcurrentReadWrite(b *testing.B) {
	cache := NewRouteCache(1000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := "/path" + fmt.Sprint(i%1000)
			if i%2 == 0 {
				cache.Put(icap.MethodREQMOD, path, h)
			} else {
				cache.Get(icap.MethodREQMOD, path)
			}
			i++
		}
	})
}

// BenchmarkRouteCache_LargeScale benchmarks large-scale operations.
// Tests performance with many cache entries.
func BenchmarkRouteCache_LargeScale(b *testing.B) {
	cache := NewRouteCache(100000, 0)
	h := &mockHandler{method: icap.MethodREQMOD}

	// Pre-populate with 10000 entries
	for i := 0; i < 10000; i++ {
		path := "/path" + fmt.Sprint(i)
		cache.Put(icap.MethodREQMOD, path, h)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := "/path" + fmt.Sprint(i%10000)
		cache.Get(icap.MethodREQMOD, path)
	}
}
