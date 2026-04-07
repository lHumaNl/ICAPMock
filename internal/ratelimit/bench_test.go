// Copyright 2026 ICAP Mock

package ratelimit

import (
	"fmt"
	"testing"
)

// BenchmarkTokenBucketAllow benchmarks the Allow() method on a single
// TokenBucketLimiter with a high burst so every call is permitted.
func BenchmarkTokenBucketAllow(b *testing.B) {
	// Large burst so the bucket never empties during the benchmark.
	limiter := NewTokenBucketLimiter(1e9, b.N+1)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

// BenchmarkShardedTokenBucketAllow benchmarks Allow() on the
// KeyBasedShardedTokenBucketLimiter using a single global key.
// This exercises the fast path (shard exists, no lock upgrade needed).
func BenchmarkShardedTokenBucketAllow(b *testing.B) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1e9, b.N+1)
	key := GlobalKey

	// Warm up: create the entry in the shard so the first real call
	// takes the fast (read-lock only) path.
	limiter.Allow(key)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		limiter.Allow(key)
	}
}

// BenchmarkShardedTokenBucketAllow_MultiKey benchmarks Allow() with many
// distinct keys to stress shard distribution and map lookup.
func BenchmarkShardedTokenBucketAllow_MultiKey(b *testing.B) {
	const numKeys = 1000
	limiter := NewKeyBasedShardedTokenBucketLimiter(1e9, b.N+numKeys)

	// Pre-build keys and warm up all shards.
	keys := make([]Key, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = ClientKey(fmt.Sprintf("192.168.%d.%d", i/256, i%256))
		limiter.Allow(keys[i])
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		limiter.Allow(keys[i%numKeys])
	}
}

// BenchmarkSlidingWindowAllow benchmarks the Allow() method on a
// SlidingWindowLimiter with a high capacity so every call is permitted.
func BenchmarkSlidingWindowAllow(b *testing.B) {
	// Large capacity so the window never fills during the benchmark.
	limiter := NewSlidingWindowLimiter(1e9, b.N+1)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}
