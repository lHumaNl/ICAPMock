// Copyright 2026 ICAP Mock

package ratelimit

import (
	"sync"
	"sync/atomic"
	"time"
)

// PerClientRateLimitConfig holds the configuration for per-client rate limiting.
type PerClientRateLimitConfig struct {
	// Enabled enables per-client rate limiting.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// RequestsPerSecond is the maximum requests per second per client.
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`

	// Burst is the maximum burst capacity per client.
	Burst int `yaml:"burst" json:"burst"`

	// MaxClients is the maximum number of clients tracked in the cache.
	// When this limit is reached, the least recently used client is evicted.
	// This protects against memory exhaustion from tracking too many IPs.
	MaxClients int `yaml:"max_clients" json:"max_clients"`

	// TTL is the time-to-live for inactive client entries.
	// Clients not accessed within this period are candidates for eviction.
	TTL time.Duration `yaml:"ttl" json:"ttl"`
}

// clientState holds the rate limiter state for a single client.
type clientState struct {
	lastAccess time.Time
	limiter    *TokenBucketLimiter
	prev       *clientState
	next       *clientState
	ip         string
}

// PerClientRateLimiter implements per-client rate limiting with LRU cache eviction.
//
// The limiter maintains a cache of client token buckets, limited by MaxClients.
// When the cache is full, the least recently used client is evicted to make room
// for new clients. This provides a balance between:
//   - Memory usage (bounded by MaxClients)
//   - Protection against abuse (tracked clients are rate-limited)
//   - Graceful degradation (evicted clients fall back to global limiter)
//
// Thread-safe: All operations are protected by mutex for concurrent access.
type PerClientRateLimiter struct {
	cache         map[string]*clientState
	head          *clientState
	tail          *clientState
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	config        PerClientRateLimitConfig
	evictions     atomic.Uint64
	mu            sync.RWMutex
}

// NewPerClientRateLimiter creates a new per-client rate limiter.
//
// The limiter starts a background goroutine to clean up expired entries
// based on the TTL. Remember to call Stop() to release resources.
//
// Parameters:
//   - config: Configuration for the per-client rate limiter.
//
// Example:
//
//	config := ratelimit.PerClientRateLimitConfig{
//	    Enabled:           true,
//	    RequestsPerSecond: 10,
//	    Burst:             20,
//	    MaxClients:        10000,
//	    TTL:               5 * time.Minute,
//	}
//	limiter := NewPerClientRateLimiter(config)
//	defer limiter.Stop()
func NewPerClientRateLimiter(config PerClientRateLimitConfig) *PerClientRateLimiter {
	// Set reasonable defaults if not provided
	if config.MaxClients <= 0 {
		config.MaxClients = 10000
	}
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = 100
	}
	if config.Burst <= 0 {
		config.Burst = config.RequestsPerSecond * 2
	}
	if config.TTL <= 0 {
		config.TTL = 5 * time.Minute
	}

	l := &PerClientRateLimiter{
		config:        config,
		cache:         make(map[string]*clientState),
		cleanupTicker: time.NewTicker(config.TTL / 2),
		stopCleanup:   make(chan struct{}),
	}

	// Start cleanup goroutine
	go l.cleanupExpiredEntries()

	return l
}

// Allow checks if the request from the given client IP should be allowed.
//
// Returns:
//   - true: Request is allowed (proceed)
//   - false: Request is rate-limited (reject with 429)
//   - ok: false if the client was not in cache (caller should use fallback)
//
// This method is O(1) for existing clients and O(log n) for new clients
// (due to potential eviction when cache is full).
func (l *PerClientRateLimiter) Allow(clientIP string) (allowed, ok bool) {
	if !l.config.Enabled {
		return true, false // Disabled, allow all, no cache hit
	}

	l.mu.RLock()
	state, exists := l.cache[clientIP]
	l.mu.RUnlock()

	if exists {
		// Existing client: O(1) operation
		l.mu.Lock()
		allowed = state.limiter.Allow()
		l.moveToHead(state)
		l.mu.Unlock()
		return allowed, true
	}

	// New client: need to create state (may involve eviction)
	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if state, exists := l.cache[clientIP]; exists {
		allowed = state.limiter.Allow()
		l.moveToHead(state)
		return allowed, true
	}

	// Check if we need to evict
	if len(l.cache) >= l.config.MaxClients {
		// Evict LRU client
		if l.tail != nil {
			delete(l.cache, l.tail.key())
			l.removeFromList(l.tail)
			l.evictions.Add(1) // Track eviction
		}
	}

	// Create new client state
	state = &clientState{
		ip:         clientIP,
		limiter:    NewTokenBucketLimiter(float64(l.config.RequestsPerSecond), l.config.Burst),
		lastAccess: time.Now(),
	}

	// Add to cache and list head
	l.cache[clientIP] = state
	l.addToHead(state)

	// Check if allowed
	allowed = state.limiter.Allow()
	return allowed, true
}

// moveToHead moves a client state to the head of the LRU list.
// Must be called with mutex held.
func (l *PerClientRateLimiter) moveToHead(state *clientState) {
	state.lastAccess = time.Now()

	// If already at head, nothing to do
	if l.head == state {
		return
	}

	// Remove from current position
	l.removeFromList(state)

	// Add to head
	l.addToHead(state)
}

// addToHead adds a client state to the head of the LRU list.
// Must be called with mutex held.
func (l *PerClientRateLimiter) addToHead(state *clientState) {
	state.prev = nil
	state.next = l.head

	if l.head != nil {
		l.head.prev = state
	}
	l.head = state

	if l.tail == nil {
		l.tail = state
	}
}

// removeFromList removes a client state from the LRU list.
// Must be called with mutex held.
func (l *PerClientRateLimiter) removeFromList(state *clientState) {
	if state.prev != nil {
		state.prev.next = state.next
	} else {
		// Removing head
		l.head = state.next
	}

	if state.next != nil {
		state.next.prev = state.prev
	} else {
		// Removing tail
		l.tail = state.prev
	}

	state.prev = nil
	state.next = nil
}

// key returns the cache key for a client state.
func (s *clientState) key() string {
	return s.ip
}

// cleanupExpiredEntries removes entries that haven't been accessed recently.
// Runs periodically in the background.
func (l *PerClientRateLimiter) cleanupExpiredEntries() {
	for {
		select {
		case <-l.cleanupTicker.C:
			l.evictExpired()
		case <-l.stopCleanup:
			return
		}
	}
}

// evictExpired removes all entries older than TTL.
// Must be called with mutex held.
//
// Complexity: O(n) where n is the number of expired entries.
// This is optimal as we only traverse the LRU list from tail until
// we reach a non-expired entry.
func (l *PerClientRateLimiter) evictExpired() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.config.TTL)

	// Traverse from tail (LRU) and remove expired entries
	// Since the list is sorted by lastAccess (LRU at tail),
	// we can stop as soon as we reach a non-expired entry
	for l.tail != nil && l.tail.lastAccess.Before(cutoff) {
		// Remove from list - get the expired entry
		expired := l.tail
		l.removeFromList(expired)

		// Remove from cache using the IP stored in clientState
		// This is O(1) map lookup instead of O(n) iteration
		delete(l.cache, expired.ip)
	}
}

// Stop stops the background cleanup goroutine.
// Should be called when the limiter is no longer needed.
func (l *PerClientRateLimiter) Stop() {
	close(l.stopCleanup)
	l.cleanupTicker.Stop()
}

// GetConfig returns the current configuration.
func (l *PerClientRateLimiter) GetConfig() PerClientRateLimitConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}

// Stats returns statistics about the limiter state.
type Stats struct {
	ActiveClients int           // Number of active clients in cache
	MaxClients    int           // Maximum clients allowed
	TTL           time.Duration // Time-to-live for inactive clients
	CacheHitRate  float64       // Cache hit rate (0-1)
	Evictions     uint64        // Number of client evictions
}

// Stats returns current statistics about the limiter.
func (l *PerClientRateLimiter) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return Stats{
		ActiveClients: len(l.cache),
		MaxClients:    l.config.MaxClients,
		TTL:           l.config.TTL,
		CacheHitRate:  0.0, // Would need to track hits/misses
		Evictions:     l.evictions.Load(),
	}
}

// GetEvictions returns the total number of client evictions.
func (l *PerClientRateLimiter) GetEvictions() uint64 {
	return l.evictions.Load()
}
