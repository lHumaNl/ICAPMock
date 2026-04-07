// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"

	"sync"
	"sync/atomic"
	"time"
)

const (
	// numShards defines the number of independent shards for the rate limiter.
	// 16 shards provide good distribution while keeping per-shard overhead low.
	// This value can be tuned based on specific workload characteristics.
	numShards = 16
)

// Key is a rate limiter key that identifies a rate limit bucket.
// Keys are used to group requests for rate limiting purposes.
type Key string

// Predefined key constants.
const (
	// GlobalKey is used for global rate limiting (all requests share one bucket).
	GlobalKey Key = "global"
)

// ClientKey generates a rate limit key for a specific client IP address.
// This is used for per-client rate limiting where each IP has its own bucket.
//
// Parameters:
//   - ip: Client IP address
//
// Returns:
//   - Key for per-client rate limiting
//
// Example:
//
//	key := ClientKey("192.168.1.100")
func ClientKey(ip string) Key {
	return Key("client:" + ip)
}

// MethodKey generates a rate limit key for a specific ICAP method.
// This is used for per-method rate limiting where each method has its own bucket.
//
// Parameters:
//   - method: ICAP method (REQMOD, RESPMOD, OPTIONS)
//
// Returns:
//   - Key for per-method rate limiting
//
// Example:
//
//	key := MethodKey("REQMOD")
func MethodKey(method string) Key {
	return Key("method:" + method)
}

// ClientMethodKey generates a rate limit key for a specific client and method combination.
// This is used for per-client+per-method rate limiting where each (client, method)
// pair has its own bucket.
//
// Parameters:
//   - ip: Client IP address
//   - method: ICAP method (REQMOD, RESPMOD, OPTIONS)
//
// Returns:
//   - Key for per-client+per-method rate limiting
//
// Example:
//
//	key := ClientMethodKey("192.168.1.100", "REQMOD")
func ClientMethodKey(ip, method string) Key {
	return Key("client:" + ip + ":method:" + method)
}

// limiterEntry holds a token bucket limiter with its last access time for TTL eviction.
type limiterEntry struct {
	limiter    *TokenBucketLimiter
	lastAccess atomic.Int64 // UnixNano timestamp, atomic to avoid lock for updates
}

// shard holds a shard of the rate limiter with its own mutex.
// Each shard maintains a map of keys to their token bucket limiters.
type shard struct {
	limiters map[Key]*limiterEntry
	mu       sync.RWMutex
}

// KeyBasedShardedTokenBucketLimiter implements rate limiting using key-based
// sharded token buckets to eliminate mutex contention in high-concurrency scenarios.
//
// Instead of a single mutex protecting the entire rate limiter state, this
// implementation uses 16 independent shards, each with its own mutex. Requests
// are distributed across shards using hash-based distribution on the key, reducing
// contention by approximately 16x compared to a single limiter.
//
// This design supports:
//   - Global rate limiting (all requests share one bucket)
//   - Per-client rate limiting (each IP has its own bucket)
//   - Per-method rate limiting (each ICAP method has its own bucket)
//   - Per-client+per-method rate limiting (each (IP, method) pair has its own bucket)
//
// This design is particularly effective for workloads with 10,000+ RPS where
// single-limiter mutex contention becomes a significant bottleneck.
//
// Thread-safe: All operations are protected by per-shard mutexes and atomic fields.
type KeyBasedShardedTokenBucketLimiter struct {
	shards    [numShards]*shard
	rate      atomic.Value
	stopCh    chan struct{}
	burst     atomic.Int64
	evictions atomic.Uint64
	ttl       time.Duration
}

// NewKeyBasedShardedTokenBucketLimiter creates a new key-based sharded token bucket rate limiter.
//
// Each key gets its own token bucket with the specified rate and burst capacity.
// Keys are distributed across 16 shards using hash-based distribution to minimize
// contention.
//
// Parameters:
//   - rate: The number of tokens added per second for each key.
//   - burst: The maximum burst capacity for each key.
//
// Example:
//
//	// Create a 100 RPS per-key limiter with 150 burst
//	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)
//	limiter.Allow(GlobalKey) // Global rate limiting
//	limiter.Allow(ClientKey("192.168.1.100")) // Per-client rate limiting
//	limiter.Allow(MethodKey("REQMOD")) // Per-method rate limiting
func NewKeyBasedShardedTokenBucketLimiter(rate float64, burst int) *KeyBasedShardedTokenBucketLimiter {
	return NewKeyBasedShardedTokenBucketLimiterWithTTL(rate, burst, 0)
}

// NewKeyBasedShardedTokenBucketLimiterWithTTL creates a sharded limiter with TTL-based eviction.
// Limiters idle for longer than ttl are periodically removed to bound memory usage.
// Pass ttl=0 to disable eviction.
func NewKeyBasedShardedTokenBucketLimiterWithTTL(rate float64, burst int, ttl time.Duration) *KeyBasedShardedTokenBucketLimiter {
	s := &KeyBasedShardedTokenBucketLimiter{
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	s.rate.Store(rate)
	s.burst.Store(int64(burst))

	for i := 0; i < numShards; i++ {
		s.shards[i] = &shard{
			limiters: make(map[Key]*limiterEntry),
		}
	}

	if ttl > 0 {
		go s.evictionLoop()
	}

	return s
}

// Allow reports whether a request for the given key should be allowed immediately.
// It creates a new token bucket for the key if it doesn't exist.
//
// This method is thread-safe and can be called from multiple goroutines concurrently.
//
// Parameters:
//   - key: Rate limit key (e.g., GlobalKey, ClientKey(ip), MethodKey(method))
//
// Returns:
//   - true: Request is allowed (proceed)
//   - false: Request is rate-limited (reject)
//
// Example:
//
//	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)
//	if limiter.Allow(ClientKey("192.168.1.100")) {
//	    // Process request
//	} else {
//	    // Rate limited
//	}
func (l *KeyBasedShardedTokenBucketLimiter) Allow(key Key) bool {
	shard := l.getShard(key)

	// Fast path: read lock only to find existing entry
	shard.mu.RLock()
	entry, exists := shard.limiters[key]
	shard.mu.RUnlock()

	if !exists {
		// Slow path: write lock to create new entry
		shard.mu.Lock()
		entry, exists = shard.limiters[key]
		if !exists {
			rate := l.rate.Load().(float64) //nolint:errcheck
			burst := int(l.burst.Load())
			entry = &limiterEntry{
				limiter: NewTokenBucketLimiter(rate, burst),
			}
			entry.lastAccess.Store(time.Now().UnixNano())
			shard.limiters[key] = entry
		}
		shard.mu.Unlock()
	}

	// Update lastAccess atomically — no lock needed
	entry.lastAccess.Store(time.Now().UnixNano())

	return entry.limiter.Allow()
}

// Wait blocks until a request for the given key is allowed or the context is canceled.
// It polls Allow() with exponential backoff to reduce CPU usage during high contention.
//
// Parameters:
//   - key: Rate limit key
//   - ctx: Context for timeout/cancellation (use context.Background() for no timeout)
//
// Returns:
//   - error if context is canceled, nil otherwise
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	err := limiter.Wait(ClientKey("192.168.1.100"), ctx)
//	if err != nil {
//	    // Context canceled or timeout
//	}
func (l *KeyBasedShardedTokenBucketLimiter) Wait(key Key, ctx context.Context) error {
	// Start with 1ms ticker, will exponentially increase up to 100ms
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	backoff := 1 * time.Millisecond
	const maxBackoff = 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if l.Allow(key) {
				return nil
			}
			// Exponential backoff to reduce CPU usage
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			ticker.Reset(backoff)
		}
	}
}

// Reserve returns a Reservation for one token from the key's bucket.
// Use the Reservation to determine how long to wait and to cancel if needed.
//
// Parameters:
//   - key: Rate limit key
//
// Returns:
//   - Reservation for the requested token
//
// Example:
//
//	reservation := limiter.Reserve(ClientKey("192.168.1.100"))
//	if reservation.OK() {
//	    delay := reservation.Delay()
//	    time.Sleep(delay)
//	    // Use the reserved token
//	} else {
//	    reservation.Cancel() // Release the reservation
//	}
func (l *KeyBasedShardedTokenBucketLimiter) Reserve(key Key) Reservation {
	shard := l.getShard(key)

	// Fast path: read lock only to find existing entry
	shard.mu.RLock()
	entry, exists := shard.limiters[key]
	shard.mu.RUnlock()

	if !exists {
		// Slow path: write lock to create new entry
		shard.mu.Lock()
		entry, exists = shard.limiters[key]
		if !exists {
			rate := l.rate.Load().(float64) //nolint:errcheck
			burst := int(l.burst.Load())
			entry = &limiterEntry{
				limiter: NewTokenBucketLimiter(rate, burst),
			}
			entry.lastAccess.Store(time.Now().UnixNano())
			shard.limiters[key] = entry
		}
		shard.mu.Unlock()
	}

	// Update lastAccess atomically — no lock needed
	entry.lastAccess.Store(time.Now().UnixNano())

	return entry.limiter.Reserve()
}

// getShard returns the shard for a given key using hash-based distribution.
// This ensures that the same key always maps to the same shard.
//
// Parameters:
//   - key: Rate limit key
//
// Returns:
//   - Shard that should handle this key
func (l *KeyBasedShardedTokenBucketLimiter) getShard(key Key) *shard {
	// Inline FNV-1a to avoid allocations (fnv.New32a() + []byte(key))
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return l.shards[h&(numShards-1)]
}

// SetRate updates the rate limit for all keys.
// The change takes effect immediately for all existing and new limiters.
// This operation is atomic with respect to concurrent Allow/Reserve calls.
//
// Parameters:
//   - rate: New rate (tokens per second)
//
// Example:
//
//	limiter.SetRate(200) // Double the rate limit
func (l *KeyBasedShardedTokenBucketLimiter) SetRate(rate float64) {
	l.rate.Store(rate)
	for i := 0; i < numShards; i++ {
		shard := l.shards[i]
		shard.mu.Lock()
		for _, entry := range shard.limiters {
			entry.limiter.SetRate(rate)
		}
		shard.mu.Unlock()
	}
}

// SetBurst updates the burst size for all keys.
// The change takes effect immediately for all existing and new limiters.
// This operation is atomic with respect to concurrent Allow/Reserve calls.
//
// Parameters:
//   - burst: New burst size
//
// Example:
//
//	limiter.SetBurst(300) // Double the burst capacity
func (l *KeyBasedShardedTokenBucketLimiter) SetBurst(burst int) {
	l.burst.Store(int64(burst))
	for i := 0; i < numShards; i++ {
		shard := l.shards[i]
		shard.mu.Lock()
		for _, entry := range shard.limiters {
			entry.limiter.SetBurst(burst)
		}
		shard.mu.Unlock()
	}
}

// ShardedStats provides statistics about the sharded rate limiter.
type ShardedStats struct {
	// TotalKeys is the total number of unique keys across all shards.
	TotalKeys int

	// KeysPerShard is the number of keys in each shard.
	// Useful for detecting hot spots or uneven distribution.
	KeysPerShard [numShards]int

	// TotalLimiters is the total number of token bucket limiters.
	TotalLimiters int
}

// Stats returns statistics about the current state of the limiter.
// This is useful for monitoring and debugging rate limiting behavior.
//
// Returns:
//   - ShardedStats with current statistics
//
// Example:
//
//	stats := limiter.Stats()
//	fmt.Printf("Total keys: %d, Max per shard: %d",
//	    stats.TotalKeys, max(stats.KeysPerShard[:]))
func (l *KeyBasedShardedTokenBucketLimiter) Stats() ShardedStats {
	stats := ShardedStats{}

	for i := 0; i < numShards; i++ {
		sh := l.shards[i]
		sh.mu.RLock()
		stats.KeysPerShard[i] = len(sh.limiters)
		stats.TotalKeys += len(sh.limiters)
		stats.TotalLimiters += len(sh.limiters)
		sh.mu.RUnlock()
	}

	return stats
}

// GetEvictions returns the total number of key evictions.
// Currently returns 0 as LRU eviction is not implemented.
// Future versions may implement LRU to bound memory usage.
//
// Returns:
//   - Total number of evictions
func (l *KeyBasedShardedTokenBucketLimiter) GetEvictions() uint64 {
	return l.evictions.Load()
}

// NewShardedTokenBucketLimiter creates a new global key-based sharded token bucket rate limiter.
// This is a compatibility function for backward compatibility with existing code.
// It creates a KeyBasedShardedTokenBucketLimiter wrapped in a GlobalKeyBasedLimiter.
//
// Deprecated: Use NewGlobalKeyBasedLimiter or NewKeyBasedShardedTokenBucketLimiter instead.
//
// Parameters:
//   - rate: The number of tokens added per second
//   - burst: The maximum burst capacity
//
// Returns:
//   - A new Limiter implementing global rate limiting with sharded buckets
//
// Example:
//
//	limiter := NewShardedTokenBucketLimiter(10000, 15000)
//	if limiter.Allow() {
//	    // Process request
//	}
func NewShardedTokenBucketLimiter(rate float64, burst int) Limiter {
	return NewGlobalKeyBasedLimiter(rate, burst, GlobalKey)
}

// evictionLoop periodically removes idle limiters that haven't been accessed within TTL.
func (l *KeyBasedShardedTokenBucketLimiter) evictionLoop() {
	ticker := time.NewTicker(l.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.evictExpired()
		}
	}
}

// evictExpired removes limiters that have been idle longer than TTL.
func (l *KeyBasedShardedTokenBucketLimiter) evictExpired() {
	cutoff := time.Now().Add(-l.ttl).UnixNano()
	for i := 0; i < numShards; i++ {
		sh := l.shards[i]
		sh.mu.Lock()
		for key, entry := range sh.limiters {
			if entry.lastAccess.Load() < cutoff {
				delete(sh.limiters, key)
				l.evictions.Add(1)
			}
		}
		sh.mu.Unlock()
	}
}

// Stop stops the background eviction goroutine.
// Safe to call multiple times or if eviction is not enabled.
func (l *KeyBasedShardedTokenBucketLimiter) Stop() {
	select {
	case <-l.stopCh:
		// Already stopped
	default:
		close(l.stopCh)
	}
}

// ShardedTokenBucketLimiter is an alias for KeyBasedShardedTokenBucketLimiter.
// This is provided for backward compatibility with existing code.
//
// Deprecated: Use KeyBasedShardedTokenBucketLimiter instead.
type ShardedTokenBucketLimiter = KeyBasedShardedTokenBucketLimiter
