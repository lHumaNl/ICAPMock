// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestKeyBasedShardedTokenBucketLimiter_Basic tests basic functionality.
func TestKeyBasedShardedTokenBucketLimiter_Basic(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	// Should allow requests initially
	for i := 0; i < 150; i++ {
		if !limiter.Allow(GlobalKey) {
			t.Fatalf("Request %d should be allowed", i)
		}
	}

	// Next request should be denied (burst exhausted)
	if limiter.Allow(GlobalKey) {
		t.Fatal("Request should be denied after burst exhaustion")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ClientKey tests per-client rate limiting.
func TestKeyBasedShardedTokenBucketLimiter_ClientKey(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 15)

	client1 := ClientKey("192.168.1.100")
	client2 := ClientKey("192.168.1.101")

	// Both clients should be able to make requests independently
	for i := 0; i < 15; i++ {
		if !limiter.Allow(client1) {
			t.Fatalf("Client1 request %d should be allowed", i)
		}
		if !limiter.Allow(client2) {
			t.Fatalf("Client2 request %d should be allowed", i)
		}
	}

	// Both clients should be rate-limited independently
	if limiter.Allow(client1) {
		t.Fatal("Client1 should be rate-limited")
	}
	if limiter.Allow(client2) {
		t.Fatal("Client2 should be rate-limited")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_MethodKey tests per-method rate limiting.
func TestKeyBasedShardedTokenBucketLimiter_MethodKey(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(50, 75)

	reqmodKey := MethodKey("REQMOD")
	respmodKey := MethodKey("RESPMOD")
	optionsKey := MethodKey("OPTIONS")

	// All methods should be able to make requests independently
	for i := 0; i < 75; i++ {
		if !limiter.Allow(reqmodKey) {
			t.Fatalf("REQMOD request %d should be allowed", i)
		}
		if !limiter.Allow(respmodKey) {
			t.Fatalf("RESPMOD request %d should be allowed", i)
		}
		if !limiter.Allow(optionsKey) {
			t.Fatalf("OPTIONS request %d should be allowed", i)
		}
	}

	// All methods should be rate-limited independently
	if limiter.Allow(reqmodKey) {
		t.Fatal("REQMOD should be rate-limited")
	}
	if limiter.Allow(respmodKey) {
		t.Fatal("RESPMOD should be rate-limited")
	}
	if limiter.Allow(optionsKey) {
		t.Fatal("OPTIONS should be rate-limited")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ClientMethodKey tests per-client+per-method rate limiting.
func TestKeyBasedShardedTokenBucketLimiter_ClientMethodKey(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(20, 30)

	client1Reqmod := ClientMethodKey("192.168.1.100", "REQMOD")
	client1Respmod := ClientMethodKey("192.168.1.100", "RESPMOD")
	client2Reqmod := ClientMethodKey("192.168.1.101", "REQMOD")

	// All (client, method) pairs should be independent
	for i := 0; i < 30; i++ {
		if !limiter.Allow(client1Reqmod) {
			t.Fatalf("Client1 REQMOD request %d should be allowed", i)
		}
		if !limiter.Allow(client1Respmod) {
			t.Fatalf("Client1 RESPMOD request %d should be allowed", i)
		}
		if !limiter.Allow(client2Reqmod) {
			t.Fatalf("Client2 REQMOD request %d should be allowed", i)
		}
	}

	// All (client, method) pairs should be rate-limited independently
	if limiter.Allow(client1Reqmod) {
		t.Fatal("Client1 REQMOD should be rate-limited")
	}
	if limiter.Allow(client1Respmod) {
		t.Fatal("Client1 RESPMOD should be rate-limited")
	}
	if limiter.Allow(client2Reqmod) {
		t.Fatal("Client2 REQMOD should be rate-limited")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ConcurrentAccess tests concurrent access.
func TestKeyBasedShardedTokenBucketLimiter_ConcurrentAccess(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1000, 1500)
	key := GlobalKey

	// Run 100 goroutines making 10 requests each (1,000 total requests)
	// This should be within the burst limit
	var wg sync.WaitGroup
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if !limiter.Allow(key) {
					t.Errorf("Goroutine %d request %d should be allowed", goroutineID, i)
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestKeyBasedShardedTokenBucketLimiter_ConcurrentMixedKeys tests concurrent access with mixed keys.
func TestKeyBasedShardedTokenBucketLimiter_ConcurrentMixedKeys(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	// Create 100 different client keys
	keys := make([]Key, 100)
	for i := 0; i < 100; i++ {
		digit1 := string(rune('0' + (i % 10)))
		digit2 := string(rune('0' + (i / 10)))
		keys[i] = ClientKey(digit1 + "." + digit2)
	}

	// Run 100 goroutines each accessing different keys
	var wg sync.WaitGroup
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			key := keys[goroutineID%len(keys)]
			for i := 0; i < 10; i++ {
				if !limiter.Allow(key) {
					t.Errorf("Goroutine %d request %d for key %s should be allowed",
						goroutineID, i, key)
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestKeyBasedShardedTokenBucketLimiter_Wait tests the Wait method.
func TestKeyBasedShardedTokenBucketLimiter_Wait(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 10)

	// Exhaust burst
	for i := 0; i < 10; i++ {
		limiter.Allow(GlobalKey)
	}

	// Next Wait should block briefly
	start := time.Now()
	err := limiter.Wait(context.Background(), GlobalKey)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait should not return error, got: %v", err)
	}

	// Should wait approximately 100ms for one token at 10 RPS
	if elapsed < 50*time.Millisecond {
		t.Errorf("Wait completed very quickly: %v (expected ~100ms)", elapsed)
	}
}

// TestKeyBasedShardedTokenBucketLimiter_WaitWithCancel tests Wait with context cancellation.
func TestKeyBasedShardedTokenBucketLimiter_WaitWithCancel(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1, 1)

	// Exhaust burst
	limiter.Allow(GlobalKey)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Wait should be canceled by context timeout
	err := limiter.Wait(ctx, GlobalKey)
	if err == nil {
		t.Fatal("Wait should return error due to context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait should return DeadlineExceeded, got: %v", err)
	}
}

// TestKeyBasedShardedTokenBucketLimiter_Reserve tests the Reserve method.
func TestKeyBasedShardedTokenBucketLimiter_Reserve(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 10)

	// Exhaust burst
	for i := 0; i < 10; i++ {
		res := limiter.Reserve(GlobalKey)
		if !res.OK() {
			t.Fatalf("Reservation %d should be OK", i)
		}
	}

	// Next reservation should have a delay
	res := limiter.Reserve(GlobalKey)
	if !res.OK() {
		t.Fatal("Reservation should be OK")
	}

	delay := res.Delay()
	if delay < 80*time.Millisecond {
		t.Errorf("Delay is very small: %v (expected ~100ms)", delay)
	}
	if delay > 150*time.Millisecond {
		t.Errorf("Delay is larger than expected: %v (expected ~100ms)", delay)
	}

	// Cancel reservation
	res.Cancel()

	// Wait a bit for token refill
	time.Sleep(110 * time.Millisecond)

	// Should be able to allow again
	if !limiter.Allow(GlobalKey) {
		t.Fatal("Should allow after token refill")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_SetRate tests dynamic rate adjustment.
func TestKeyBasedShardedTokenBucketLimiter_SetRate(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 15)

	// Exhaust burst at 10 RPS
	for i := 0; i < 15; i++ {
		limiter.Allow(GlobalKey)
	}

	// Should be rate-limited
	if limiter.Allow(GlobalKey) {
		t.Fatal("Should be rate-limited at 10 RPS")
	}

	// Increase rate to 100 RPS
	limiter.SetRate(100)

	// Wait a bit for refill at new rate
	time.Sleep(50 * time.Millisecond)

	// Should be able to allow again
	if !limiter.Allow(GlobalKey) {
		t.Fatal("Should allow after rate increase")
	}
}

// TestKeyBasedShardedTokenBucketLimiter_Stats tests statistics collection.
func TestKeyBasedShardedTokenBucketLimiter_Stats(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	// Make requests for 10 different keys
	keys := make([]Key, 10)
	for i := 0; i < 10; i++ {
		keys[i] = ClientKey(string(rune('A' + i)))
		for j := 0; j < 5; j++ {
			limiter.Allow(keys[i])
		}
	}

	// Check stats
	stats := limiter.Stats()

	if stats.TotalKeys != 10 {
		t.Errorf("Expected 10 keys, got %d", stats.TotalKeys)
	}

	if stats.TotalLimiters != 10 {
		t.Errorf("Expected 10 limiters, got %d", stats.TotalLimiters)
	}

	// Check distribution across shards
	// With 10 keys distributed across 16 shards, some shards will have 0, some will have 1
	totalInShards := 0
	for _, count := range stats.KeysPerShard {
		totalInShards += count
	}
	if totalInShards != 10 {
		t.Errorf("Expected 10 total in shards, got %d", totalInShards)
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ManyKeys tests behavior with many keys.
func TestKeyBasedShardedTokenBucketLimiter_ManyKeys(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 15)

	// Create 10,000 different keys
	for i := 0; i < 10000; i++ {
		digit1 := string(rune('0' + (i % 10)))
		digit2 := string(rune('0' + (i / 10)))
		key := ClientKey(digit1 + "." + digit2)
		limiter.Allow(key)
	}

	// Check stats
	stats := limiter.Stats()

	if stats.TotalKeys != 10000 {
		t.Errorf("Expected 10,000 keys, got %d", stats.TotalKeys)
	}

	// Check that keys are distributed across shards
	emptyShards := 0
	for _, count := range stats.KeysPerShard {
		if count == 0 {
			emptyShards++
		}
	}

	// With 10,000 keys across 16 shards, we expect some distribution
	// Not all shards should be empty
	if emptyShards > 8 {
		t.Errorf("Too many empty shards: %d/16", emptyShards)
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ZeroRate tests behavior with zero rate.
func TestKeyBasedShardedTokenBucketLimiter_ZeroRate(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(0, 10)

	// First 10 requests should be allowed (from initial burst)
	for i := 0; i < 10; i++ {
		if !limiter.Allow(GlobalKey) {
			t.Fatalf("Request %d should be allowed from initial burst", i)
		}
	}

	// After burst is exhausted, all requests should be denied
	for i := 0; i < 5; i++ {
		if limiter.Allow(GlobalKey) {
			t.Fatalf("Request %d should be denied after burst exhaustion", i)
		}
	}
}

// TestKeyBasedShardedTokenBucketLimiter_ZeroBurst tests behavior with zero burst.
func TestKeyBasedShardedTokenBucketLimiter_ZeroBurst(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 0)

	// All requests should be denied
	for i := 0; i < 5; i++ {
		if limiter.Allow(GlobalKey) {
			t.Fatalf("Request %d should be denied with zero burst", i)
		}
	}
}

// TestKeyGeneration tests key generation functions.
func TestKeyGeneration(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() Key
		expected string
	}{
		{
			name:     "GlobalKey",
			fn:       func() Key { return GlobalKey },
			expected: "global",
		},
		{
			name:     "ClientKey",
			fn:       func() Key { return ClientKey("192.168.1.100") },
			expected: "client:192.168.1.100",
		},
		{
			name:     "MethodKey",
			fn:       func() Key { return MethodKey("REQMOD") },
			expected: "method:REQMOD",
		},
		{
			name:     "ClientMethodKey",
			fn:       func() Key { return ClientMethodKey("192.168.1.100", "REQMOD") },
			expected: "client:192.168.1.100:method:REQMOD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.fn()
			if string(key) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, key)
			}
		})
	}
}

// TestGlobalKeyBasedLimiter tests the global key wrapper.
func TestGlobalKeyBasedLimiter(t *testing.T) {
	limiter := NewGlobalKeyBasedLimiter(100, 150, GlobalKey)

	// Should allow requests initially
	for i := 0; i < 150; i++ {
		if !limiter.Allow() {
			t.Fatalf("Request %d should be allowed", i)
		}
	}

	// Next request should be denied
	if limiter.Allow() {
		t.Fatal("Request should be denied after burst exhaustion")
	}
}

// TestGlobalKeyBasedLimiter_Wait tests Wait method.
func TestGlobalKeyBasedLimiter_Wait(t *testing.T) {
	limiter := NewGlobalKeyBasedLimiter(10, 10, GlobalKey)

	// Exhaust burst
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	// Next Wait should block but eventually return
	err := limiter.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait should not return error, got: %v", err)
	}

	// Wait consumed a token, so we should have exhausted the burst again
	if limiter.Allow() {
		t.Fatal("Should not allow immediately after Wait (token consumed)")
	}

	// But after a short wait, we should be able to allow again
	time.Sleep(110 * time.Millisecond)
	if !limiter.Allow() {
		t.Fatal("Should allow after waiting for token refill")
	}
}

// TestGlobalKeyBasedLimiter_Reserve tests Reserve method.
func TestGlobalKeyBasedLimiter_Reserve(t *testing.T) {
	limiter := NewGlobalKeyBasedLimiter(10, 10, GlobalKey)

	// Exhaust burst
	for i := 0; i < 10; i++ {
		res := limiter.Reserve()
		if !res.OK() {
			t.Fatalf("Reservation %d should be OK", i)
		}
	}

	// Next reservation should have delay
	res := limiter.Reserve()
	if !res.OK() {
		t.Fatal("Reservation should be OK")
	}

	// Cancel reservation
	res.Cancel()
}

// TestKeyBasedShardedTokenBucketLimiter_TTLEviction tests that idle limiters are evicted.
func TestKeyBasedShardedTokenBucketLimiter_TTLEviction(t *testing.T) {
	t.Parallel()
	limiter := NewKeyBasedShardedTokenBucketLimiterWithTTL(1000, 100, 100*time.Millisecond)
	defer limiter.Stop()

	// Create some keys
	limiter.Allow(ClientKey("1.1.1.1"))
	limiter.Allow(ClientKey("2.2.2.2"))
	limiter.Allow(ClientKey("3.3.3.3"))

	stats := limiter.Stats()
	if stats.TotalKeys != 3 {
		t.Fatalf("Expected 3 keys, got %d", stats.TotalKeys)
	}

	// Wait for eviction
	time.Sleep(250 * time.Millisecond)

	stats = limiter.Stats()
	if stats.TotalKeys != 0 {
		t.Errorf("Expected 0 keys after TTL eviction, got %d", stats.TotalKeys)
	}

	if limiter.GetEvictions() != 3 {
		t.Errorf("Expected 3 evictions, got %d", limiter.GetEvictions())
	}
}

// TestKeyBasedShardedTokenBucketLimiter_TTLKeepsActive tests that active keys are not evicted.
func TestKeyBasedShardedTokenBucketLimiter_TTLKeepsActive(t *testing.T) {
	t.Parallel()
	limiter := NewKeyBasedShardedTokenBucketLimiterWithTTL(1000, 100, 200*time.Millisecond)
	defer limiter.Stop()

	activeKey := ClientKey("active")
	idleKey := ClientKey("idle")

	limiter.Allow(activeKey)
	limiter.Allow(idleKey)

	// Keep active key alive
	time.Sleep(100 * time.Millisecond)
	limiter.Allow(activeKey)

	// Wait for idle key to expire
	time.Sleep(150 * time.Millisecond)

	stats := limiter.Stats()
	if stats.TotalKeys != 1 {
		t.Errorf("Expected 1 key (active), got %d", stats.TotalKeys)
	}
}

// BenchmarkKeyBasedShardedTokenBucketLimiter_Allow benchmarks Allow method.
func BenchmarkKeyBasedShardedTokenBucketLimiter_Allow(b *testing.B) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1000000, 1000000)
	key := GlobalKey

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(key)
	}
}

// BenchmarkKeyBasedShardedTokenBucketLimiter_ManyKeys benchmarks with multiple keys.
func BenchmarkKeyBasedShardedTokenBucketLimiter_ManyKeys(b *testing.B) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1000000, 1000000)

	// Create 100 different keys
	keys := make([]Key, 100)
	for i := 0; i < 100; i++ {
		digit1 := string(rune('0' + (i % 10)))
		digit2 := string(rune('0' + (i / 10)))
		keys[i] = ClientKey(digit1 + "." + digit2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(keys[i%len(keys)])
	}
}

// BenchmarkKeyBasedShardedTokenBucketLimiter_Concurrent benchmarks concurrent access.
func BenchmarkKeyBasedShardedTokenBucketLimiter_Concurrent(b *testing.B) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100000, 100000)
	key := GlobalKey

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow(key)
		}
	})
}
