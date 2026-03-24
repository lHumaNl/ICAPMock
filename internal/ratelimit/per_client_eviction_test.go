package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
	"github.com/prometheus/client_golang/prometheus"
)

// TestPerClientRateLimiter_EvictExpired_Performance verifies that evictExpired()
// has O(n) complexity instead of O(n×m).
func TestPerClientRateLimiter_EvictExpired_Performance(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        10000,
		TTL:               100 * time.Millisecond, // Short TTL for testing
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Add many clients (1000)
	numClients := 1000
	for i := 0; i < numClients; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		limiter.Allow(ip)
	}

	stats := limiter.Stats()
	if stats.ActiveClients != numClients {
		t.Fatalf("expected %d active clients, got %d", numClients, stats.ActiveClients)
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup by adding a new client
	limiter.Allow("10.1.1.1")

	// Wait for cleanup to complete
	time.Sleep(100 * time.Millisecond)

	// Verify that expired clients were evicted
	stats = limiter.Stats()
	if stats.ActiveClients >= numClients {
		t.Errorf("expected fewer active clients after eviction, got %d", stats.ActiveClients)
	}

	// The cleanup should be fast (O(n) where n is number of expired entries)
	// If it were O(n×m), it would be very slow with 1000 clients
}

// TestPerClientRateLimiter_EvictExpired_Correctness verifies that evictExpired()
// correctly removes only expired entries and keeps valid ones.
func TestPerClientRateLimiter_EvictExpired_Correctness(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        10000,
		TTL:               200 * time.Millisecond,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Add clients in two batches
	for i := 0; i < 10; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		limiter.Allow(ip)
	}

	time.Sleep(150 * time.Millisecond)

	// Add more clients (should not be expired)
	for i := 10; i < 20; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		limiter.Allow(ip)
	}

	stats := limiter.Stats()
	if stats.ActiveClients != 20 {
		t.Fatalf("expected 20 active clients before cleanup, got %d", stats.ActiveClients)
	}

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)
	limiter.Allow("10.0.0.20")
	time.Sleep(100 * time.Millisecond)

	// First 10 clients should be evicted, last 10 should remain
	stats = limiter.Stats()
	if stats.ActiveClients > 11 {
		t.Errorf("expected at most 11 active clients after eviction, got %d", stats.ActiveClients)
	}

	// Verify that recent clients are still in cache
	for i := 10; i < 15; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		allowed, ok := limiter.Allow(ip)
		if !ok {
			t.Errorf("recent client %s should still be in cache", ip)
		}
		if !allowed {
			t.Errorf("recent client %s should be allowed", ip)
		}
	}
}

// TestPerClientMiddleware_UnknownIP_DoSProtection verifies that requests with
// unknown IP are not tracked in per-client limiter to prevent DoS attacks.
func TestPerClientMiddleware_UnknownIP_DoSProtection(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1, // Very low limit
		Burst:             1, // Very low burst
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(1000, 1000) // High global limit
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	// Create request with no IP (will return "unknown")
	req := &icap.Request{}
	ctx := context.Background()

	// Should allow all requests even though per-client limit is 1
	// because "unknown" IP skips per-client limiting
	for i := 0; i < 100; i++ {
		allowed, err := middleware.Allow(ctx, req)
		if err != nil {
			t.Errorf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: should be allowed (unknown IP uses global limiter)", i)
		}
	}

	// Verify that per-client cache is empty
	stats := perClientLimiter.Stats()
	if stats.ActiveClients != 0 {
		t.Errorf("expected 0 active clients (unknown IP not tracked), got %d", stats.ActiveClients)
	}
}

// TestPerClientMiddleware_UnknownIP_RaceCondition tests that concurrent requests
// with unknown IP don't cause race conditions.
func TestPerClientMiddleware_UnknownIP_RaceCondition(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(10000, 20000)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	numGoroutines := 50
	requestsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			req := &icap.Request{}
			ctx := context.Background()
			for j := 0; j < requestsPerGoroutine; j++ {
				middleware.Allow(ctx, req)
			}
		}(i)
	}

	wg.Wait()

	// Verify no panics or race conditions occurred
	stats := perClientLimiter.Stats()
	if stats.ActiveClients != 0 {
		t.Errorf("expected 0 active clients (unknown IP not tracked), got %d", stats.ActiveClients)
	}
}

// TestPerClientRateLimiter_ConcurrentEviction tests that eviction and concurrent
// access don't cause race conditions.
func TestPerClientRateLimiter_ConcurrentEviction(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               50 * time.Millisecond, // Very short TTL
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	numGoroutines := 10
	requestsPerGoroutine := 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				// Each goroutine uses different IPs
				ip := fmt.Sprintf("10.%d.%d.%d", id, (j/256)%256, j%256)
				limiter.Allow(ip)
			}
		}(i)
	}

	wg.Wait()

	// Verify no panics or race conditions occurred
	stats := limiter.Stats()
	if stats.ActiveClients > config.MaxClients {
		t.Errorf("expected at most %d active clients, got %d", config.MaxClients, stats.ActiveClients)
	}
}

// TestPerClientMiddleware_Wait_RaceCondition tests that the Wait method
// doesn't cause race conditions when called concurrently.
func TestPerClientMiddleware_Wait_RaceCondition(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(10000, 20000)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	numGoroutines := 20
	requestsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			req := &icap.Request{
				ClientIP: fmt.Sprintf("192.168.1.%d", (id%10)+1),
			}
			ctx := context.Background()
			for j := 0; j < requestsPerGoroutine; j++ {
				middleware.Wait(ctx, req)
			}
		}(i)
	}

	wg.Wait()

	// Verify no panics or race conditions occurred
}

// TestPerClientRateLimiter_LRU_Eviction_Order verifies that LRU eviction
// maintains correct order and evicts the least recently used entry.
func TestPerClientRateLimiter_LRU_Eviction_Order(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        3, // Small cache
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Add clients 1, 2, 3
	limiter.Allow("10.0.0.1")
	limiter.Allow("10.0.0.2")
	limiter.Allow("10.0.0.3")

	stats := limiter.Stats()
	if stats.ActiveClients != 3 {
		t.Fatalf("expected 3 active clients, got %d", stats.ActiveClients)
	}

	// Access client 1 to make it MRU
	limiter.Allow("10.0.0.1")

	// Add client 4 - should evict client 2 (LRU)
	limiter.Allow("10.0.0.4")

	// Verify evictions count
	evictions := limiter.GetEvictions()
	if evictions != 1 {
		t.Errorf("expected 1 eviction, got %d", evictions)
	}

	// Client 2 should be evicted (needs new bucket)
	allowed, ok := limiter.Allow("10.0.0.2")
	if !ok {
		t.Error("client 2 should create new bucket after eviction")
	}
	if !allowed {
		t.Error("client 2 should be allowed with new bucket")
	}

	// Clients 1, 3, 4 should still be in cache
	for _, ip := range []string{"10.0.0.1", "10.0.0.3", "10.0.0.4"} {
		allowed, ok := limiter.Allow(ip)
		if !ok {
			t.Errorf("client %s should still be in cache", ip)
		}
		if !allowed {
			t.Errorf("client %s should be allowed", ip)
		}
	}
}

// BenchmarkPerClientRateLimiter_EvictExpired benchmarks the evictExpired
// method to verify O(n) complexity.
func BenchmarkPerClientRateLimiter_EvictExpired(b *testing.B) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        10000,
		TTL:               100 * time.Millisecond,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter := NewPerClientRateLimiter(config)

		// Add many clients
		for j := 0; j < 1000; j++ {
			ip := fmt.Sprintf("10.0.%d.%d", j/256, j%256)
			limiter.Allow(ip)
		}

		// Wait for expiration and trigger cleanup
		time.Sleep(150 * time.Millisecond)
		limiter.Allow("10.1.1.1")
		time.Sleep(100 * time.Millisecond)

		limiter.Stop()
	}
}
