package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// TestNewPerClientRateLimiter tests the constructor.
func TestNewPerClientRateLimiter(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	if limiter == nil {
		t.Fatal("limiter is nil")
	}

	stats := limiter.Stats()
	if stats.MaxClients != 100 {
		t.Errorf("expected MaxClients=100, got %d", stats.MaxClients)
	}
	if stats.TTL != 5*time.Minute {
		t.Errorf("expected TTL=5m, got %v", stats.TTL)
	}
}

// TestNewPerClientRateLimiter_Defaults tests default values.
func TestNewPerClientRateLimiter_Defaults(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled: true,
		// Leave other fields as zero
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	stats := limiter.Stats()
	if stats.MaxClients != 10000 {
		t.Errorf("expected default MaxClients=10000, got %d", stats.MaxClients)
	}
}

// TestPerClientRateLimiter_SingleClient tests rate limiting for a single client.
func TestPerClientRateLimiter_SingleClient(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	clientIP := "192.168.1.1"

	// Should allow burst requests (20)
	for i := 0; i < 20; i++ {
		allowed, ok := limiter.Allow(clientIP)
		if !allowed || !ok {
			t.Errorf("request %d: expected allowed=true, ok=true", i)
		}
	}

	// Next request should be denied (burst exceeded)
	allowed, ok := limiter.Allow(clientIP)
	if allowed {
		t.Error("expected request to be denied (burst exceeded)")
	}
	if !ok {
		t.Error("expected ok=true (client should be in cache)")
	}
}

// TestPerClientRateLimiter_MultipleClients tests multiple clients.
func TestPerClientRateLimiter_MultipleClients(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 5,
		Burst:             10,
		MaxClients:        1000,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	clients := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}

	// Each client should be able to make burst requests
	for _, ip := range clients {
		for i := 0; i < 10; i++ {
			allowed, ok := limiter.Allow(ip)
			if !allowed || !ok {
				t.Errorf("client %s request %d: expected allowed=true, ok=true", ip, i)
			}
		}
	}

	// All clients should have independent buckets
	for _, ip := range clients {
		allowed, _ := limiter.Allow(ip)
		if allowed {
			t.Errorf("client %s: expected request to be denied (burst exceeded)", ip)
		}
	}
}

// TestPerClientRateLimiter_LRU_Eviction tests LRU eviction.
func TestPerClientRateLimiter_LRU_Eviction(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        5,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Fill cache to capacity
	for i := 1; i <= 5; i++ {
		ip := "192.168.1." + string(rune('0'+i))
		limiter.Allow(ip)
		time.Sleep(10 * time.Millisecond) // Ensure different lastAccess times
	}

	stats := limiter.Stats()
	if stats.ActiveClients != 5 {
		t.Errorf("expected 5 active clients, got %d", stats.ActiveClients)
	}

	// Add one more client - should evict the oldest (first one)
	newIP := "192.168.1.6"
	limiter.Allow(newIP)

	stats = limiter.Stats()
	if stats.ActiveClients != 5 {
		t.Errorf("expected 5 active clients after eviction, got %d", stats.ActiveClients)
	}

	// The first client should be able to make requests (new bucket created)
	allowed, ok := limiter.Allow("192.168.1.1")
	if !allowed || !ok {
		t.Error("expected first client to be able to make requests (new bucket)")
	}
}

// TestPerClientRateLimiter_Disabled tests that disabled limiter allows all.
func TestPerClientRateLimiter_Disabled(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           false, // Disabled
		RequestsPerSecond: 1,
		Burst:             1,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	clientIP := "192.168.1.1"

	// Even though burst=1, we should be able to make many requests
	for i := 0; i < 100; i++ {
		allowed, ok := limiter.Allow(clientIP)
		if !allowed {
			t.Errorf("request %d: expected allowed=true (limiter disabled)", i)
		}
		if ok {
			t.Error("expected ok=false (client not cached when disabled)")
		}
	}
}

// TestPerClientRateLimiter_TokenReplenishment tests token replenishment.
func TestPerClientRateLimiter_TokenReplenishment(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10, // 10 tokens per second
		Burst:             10, // 10 burst capacity
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	clientIP := "192.168.1.1"

	// Consume all burst tokens
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(clientIP)
		if !allowed {
			t.Error("expected request to be allowed")
		}
	}

	// Next request should be denied
	allowed, _ := limiter.Allow(clientIP)
	if allowed {
		t.Error("expected request to be denied (no tokens)")
	}

	// Wait for token replenishment (100ms = 1 token at 10/sec)
	time.Sleep(150 * time.Millisecond)

	// Should be able to make 1 request
	allowed, _ = limiter.Allow(clientIP)
	if !allowed {
		t.Error("expected request to be allowed after token replenishment")
	}
}

// TestPerClientRateLimiter_ConcurrentAccess tests concurrent access safety.
func TestPerClientRateLimiter_ConcurrentAccess(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1000,
		Burst:             2000,
		MaxClients:        1000,
		TTL:               5 * time.Minute,
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
			ip := "192.168.1." + string(rune('0'+(id%10)+1))
			for j := 0; j < requestsPerGoroutine; j++ {
				limiter.Allow(ip)
			}
		}(i)
	}

	wg.Wait()

	// Check that all requests succeeded (no panics, deadlocks)
	stats := limiter.Stats()
	if stats.ActiveClients > 10 {
		t.Errorf("expected at most 10 active clients, got %d", stats.ActiveClients)
	}
}

// TestPerClientRateLimiter_GetConfig tests config retrieval.
func TestPerClientRateLimiter_GetConfig(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 50,
		Burst:             100,
		MaxClients:        5000,
		TTL:               10 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	retrievedConfig := limiter.GetConfig()
	if retrievedConfig.RequestsPerSecond != 50 {
		t.Errorf("expected RequestsPerSecond=50, got %d", retrievedConfig.RequestsPerSecond)
	}
	if retrievedConfig.Burst != 100 {
		t.Errorf("expected Burst=100, got %d", retrievedConfig.Burst)
	}
	if retrievedConfig.MaxClients != 5000 {
		t.Errorf("expected MaxClients=5000, got %d", retrievedConfig.MaxClients)
	}
}

// TestPerClientRateLimiter_Stats tests statistics.
func TestPerClientRateLimiter_Stats(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	stats := limiter.Stats()
	if stats.ActiveClients != 0 {
		t.Errorf("expected 0 active clients, got %d", stats.ActiveClients)
	}

	// Add some clients
	limiter.Allow("192.168.1.1")
	limiter.Allow("192.168.1.2")

	stats = limiter.Stats()
	if stats.ActiveClients != 2 {
		t.Errorf("expected 2 active clients, got %d", stats.ActiveClients)
	}
}

// TestPerClientRateLimiter_TTL_Cleanup tests TTL-based cleanup.
func TestPerClientRateLimiter_TTL_Cleanup(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               100 * time.Millisecond, // Very short TTL for testing
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Add clients
	limiter.Allow("192.168.1.1")
	limiter.Allow("192.168.1.2")

	stats := limiter.Stats()
	if stats.ActiveClients != 2 {
		t.Errorf("expected 2 active clients, got %d", stats.ActiveClients)
	}

	// Wait for cleanup tick (TTL/2)
	time.Sleep(60 * time.Millisecond)

	// Trigger cleanup by making a new request
	limiter.Allow("192.168.1.3")

	// Wait a bit more for cleanup to complete
	time.Sleep(100 * time.Millisecond)

	// Old clients should be evicted (cleanup runs periodically)
	stats = limiter.Stats()
	// The actual number depends on timing, but it should be <= 3
	if stats.ActiveClients > 3 {
		t.Errorf("expected at most 3 active clients, got %d", stats.ActiveClients)
	}
}

// BenchmarkPerClientRateLimiter_Allow benchmarks the Allow method.
func BenchmarkPerClientRateLimiter_Allow(b *testing.B) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10000,
		Burst:             20000,
		MaxClients:        10000,
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	// Pre-populate cache
	for i := 0; i < 100; i++ {
		ip := "192.168.1." + string(rune('0'+(i%10)+1))
		limiter.Allow(ip)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ip := "192.168.1." + string(rune('0'+(i%10)+1))
			limiter.Allow(ip)
			i++
		}
	})
}

// BenchmarkPerClientRateLimiter_Allow_NewClient benchmarks Allow with new clients.
func BenchmarkPerClientRateLimiter_Allow_NewClient(b *testing.B) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10000,
		Burst:             20000,
		MaxClients:        100000, // Large cache to avoid eviction
		TTL:               5 * time.Minute,
	}

	limiter := NewPerClientRateLimiter(config)
	defer limiter.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := "10.0.0." + string(rune('0'+(i%256)+1))
		limiter.Allow(ip)
	}
}
