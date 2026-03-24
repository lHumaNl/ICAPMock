// Package router provides ICAP request routing functionality with caching.
package router

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Default cache configuration values.
const (
	DefaultMaxEntries = 1000            // Maximum number of entries in the cache
	DefaultTTL        = 5 * time.Minute // Default TTL for cache entries
)

// CacheEntry represents a cached route lookup result.
type CacheEntry struct {
	// Handler is the cached handler reference.
	Handler handler.Handler

	// Timestamp is when this entry was created/updated.
	Timestamp time.Time

	// AccessCount tracks how many times this entry has been accessed.
	AccessCount int64
}

// LRUNode represents a node in the doubly-linked list for LRU tracking.
type LRUNode struct {
	key   string
	prev  *LRUNode
	next  *LRUNode
	entry *CacheEntry
}

// RouteCache implements an LRU cache for route lookups.
// It reduces latency for frequently accessed routes by caching
// the handler reference for (method + path) combinations.
//
// Cache keys are formatted as "{method}:{path}" (e.g., "REQMOD:/api/scan").
//
// The cache is thread-safe and supports concurrent Get/Put/Delete operations.
// LRU eviction is O(1) using a doubly-linked list.
type RouteCache struct {
	// cache stores the cached entries using a mutex-protected map.
	cache map[string]*LRUNode

	// head points to the most recently used (MRU) node.
	head *LRUNode

	// tail points to the least recently used (LRU) node.
	tail *LRUNode

	// mu protects all cache operations.
	mu sync.RWMutex

	// maxEntries is the maximum number of entries the cache can hold.
	maxEntries int

	// ttl is the time-to-live for cache entries. Zero means no expiration.
	ttl time.Duration

	// metrics records cache operations (hits, misses, evictions).
	metrics *CacheMetrics
}

// CacheMetrics tracks cache performance metrics using atomic counters
// so they can be updated without holding an exclusive lock.
type CacheMetrics struct {
	hits      atomic.Int64
	misses    atomic.Int64
	evictions int64 // only updated under write lock
}

// NewRouteCache creates a new RouteCache with the specified configuration.
//
// Parameters:
//   - maxEntries: Maximum number of entries in the cache (0 uses DefaultMaxEntries).
//   - ttl: Time-to-live for cache entries (0 means no expiration).
//
// Returns:
//   - *RouteCache: The created cache.
//
// Example:
//
//	cache := NewRouteCache(1000, 5*time.Minute)
func NewRouteCache(maxEntries int, ttl time.Duration) *RouteCache {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}

	return &RouteCache{
		cache:      make(map[string]*LRUNode),
		maxEntries: maxEntries,
		ttl:        ttl,
		metrics:    &CacheMetrics{},
	}
}

// Get retrieves a cached handler for the given method and path.
//
// Returns the cached handler and true if found and not expired.
// Returns nil and false if not found, expired, or on cache miss.
//
// Parameters:
//   - method: The ICAP method (REQMOD, RESPMOD, OPTIONS).
//   - path: The route path.
//
// Returns:
//   - handler.Handler: The cached handler, or nil if not found.
//   - bool: True if cache hit, false if miss.
//
// Example:
//
//	handler, hit := cache.Get("REQMOD", "/api/scan")
//	if hit {
//	    return handler.Handle(ctx, req)
//	}
func (c *RouteCache) Get(method, path string) (handler.Handler, bool) {
	key := c.buildKey(method, path)

	// Fast path: read-only lookup under RLock
	c.mu.RLock()
	node, exists := c.cache[key]
	if !exists {
		c.mu.RUnlock()
		c.metrics.misses.Add(1)
		return nil, false
	}

	// Check TTL expiration (read-only check)
	if c.ttl > 0 && time.Since(node.entry.Timestamp) > c.ttl {
		c.mu.RUnlock()
		// Expired: acquire write lock to evict
		c.mu.Lock()
		// Re-check under write lock (another goroutine may have evicted it)
		if _, stillExists := c.cache[key]; stillExists {
			c.evictKeyLocked(key)
		}
		c.mu.Unlock()
		c.metrics.misses.Add(1)
		return nil, false
	}

	// Cache hit: grab the handler reference while still under RLock
	h := node.entry.Handler
	c.mu.RUnlock()

	// Probabilistic LRU promotion: only promote ~1/10 hits to reduce write lock contention
	count := atomic.AddInt64(&node.entry.AccessCount, 1)
	if count%10 == 0 {
		c.mu.Lock()
		if _, stillExists := c.cache[key]; stillExists {
			c.moveToFront(node)
		}
		c.mu.Unlock()
	}

	c.metrics.hits.Add(1)
	return h, true
}

// Put stores a handler in the cache for the given method and path.
//
// If the cache is full, the least recently used entry is evicted.
// If an entry for the key already exists, it is updated.
//
// Parameters:
//   - method: The ICAP method (REQMOD, RESPMOD, OPTIONS).
//   - path: The route path.
//   - h: The handler to cache.
//
// Example:
//
//	cache.Put("REQMOD", "/api/scan", handler)
func (c *RouteCache) Put(method, path string, h handler.Handler) {
	if h == nil {
		return
	}

	key := c.buildKey(method, path)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update it
	if node, exists := c.cache[key]; exists {
		node.entry.Handler = h
		node.entry.Timestamp = now
		node.entry.AccessCount++
		c.moveToFront(node)
		return
	}

	// Evict LRU entry if cache is full
	if len(c.cache) >= c.maxEntries {
		c.evictLRU()
	}

	// Add new entry
	node := &LRUNode{
		key: key,
		entry: &CacheEntry{
			Handler:     h,
			Timestamp:   now,
			AccessCount: 1,
		},
	}

	c.cache[key] = node
	c.addToFront(node)
}

// Delete removes an entry from the cache for the given method and path.
//
// Parameters:
//   - method: The ICAP method (REQMOD, RESPMOD, OPTIONS).
//   - path: The route path.
//
// Example:
//
//	cache.Delete("REQMOD", "/api/scan")
func (c *RouteCache) Delete(method, path string) {
	key := c.buildKey(method, path)
	c.mu.Lock()
	c.evictKey(key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache.
// This is useful when routes are reloaded.
//
// Example:
//
//	cache.Clear()
func (c *RouteCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*LRUNode)
	c.head = nil
	c.tail = nil
}

// Size returns the current number of entries in the cache.
//
// Returns:
//   - int: The number of cached entries.
func (c *RouteCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Hits returns the total number of cache hits.
//
// Returns:
//   - int64: The total cache hits.
func (c *RouteCache) Hits() int64 {
	return c.metrics.hits.Load()
}

// Misses returns the total number of cache misses.
//
// Returns:
//   - int64: The total cache misses.
func (c *RouteCache) Misses() int64 {
	return c.metrics.misses.Load()
}

// Evictions returns the total number of cache evictions.
//
// Returns:
//   - int64: The total cache evictions.
func (c *RouteCache) Evictions() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics.evictions // protected by write lock, read under RLock is fine
}

// ResetMetrics resets all cache metrics (hits, misses, evictions).
//
// Example:
//
//	cache.ResetMetrics()
func (c *RouteCache) ResetMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.hits.Store(0)
	c.metrics.misses.Store(0)
	c.metrics.evictions = 0
}

// evictLRU removes the least recently used entry from the cache.
// Caller must hold c.mu.
func (c *RouteCache) evictLRU() {
	if c.tail == nil {
		return
	}

	// Remove tail (LRU) - O(1)
	key := c.tail.key
	c.removeNode(c.tail)
	delete(c.cache, key)
	c.metrics.evictions++
}

// evictKey removes a specific key from the cache.
// Caller must hold c.mu.
func (c *RouteCache) evictKey(key string) {
	c.evictKeyLocked(key)
}

// evictKeyLocked removes a specific key from the cache without acquiring lock.
// Caller must hold c.mu.
func (c *RouteCache) evictKeyLocked(key string) {
	node, exists := c.cache[key]
	if !exists {
		return
	}

	c.removeNode(node)
	delete(c.cache, key)
	c.metrics.evictions++
}

// moveToFront moves a node to the front of the list (most recently used).
// This is an O(1) operation.
// Caller must hold c.mu.
func (c *RouteCache) moveToFront(node *LRUNode) {
	if node == c.head {
		return // Already at front
	}

	c.removeNode(node)
	c.addToFront(node)
}

// addToFront adds a node to the front of the list (most recently used).
// This is an O(1) operation.
// Caller must hold c.mu.
func (c *RouteCache) addToFront(node *LRUNode) {
	node.prev = nil
	node.next = c.head

	if c.head != nil {
		c.head.prev = node
	}

	c.head = node

	if c.tail == nil {
		c.tail = node
	}
}

// removeNode removes a node from the doubly-linked list.
// This is an O(1) operation.
// Caller must hold c.mu.
func (c *RouteCache) removeNode(node *LRUNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}

	// Clear pointers to help garbage collector
	node.prev = nil
	node.next = nil
}

// buildKey creates a cache key from method and path.
func (c *RouteCache) buildKey(method, path string) string {
	return method + ":" + path
}

// recordHit increments the cache hit counter.
func (c *RouteCache) recordHit() {
	c.metrics.hits.Add(1)
}

// recordMiss increments the cache miss counter.
func (c *RouteCache) recordMiss() {
	c.metrics.misses.Add(1)
}

// LookupHandler performs a cache-aware handler lookup.
// It checks the cache first, and on miss, performs the route lookup
// and caches the result.
//
// Parameters:
//   - routes: The route table to search on cache miss.
//   - req: The ICAP request.
//
// Returns:
//   - handler.Handler: The found handler, or nil if not found.
//   - bool: True if cache hit, false if cache miss.
func (c *RouteCache) LookupHandler(routes map[string]handler.Handler, req *icap.Request) (handler.Handler, bool) {
	// Extract path from URI
	path := extractPath(req.URI)

	// Check cache first
	handler, hit := c.Get(req.Method, path)
	if hit {
		return handler, true
	}

	// Cache miss: perform route lookup
	handler, exists := routes[path]
	if !exists {
		return nil, false
	}

	// Cache the result for future lookups
	c.Put(req.Method, path, handler)

	return handler, false
}

// LookupHandlerFunc performs a cache-aware handler lookup using a function.
// This is useful for lock-free route storage like sync.Map.
//
// Parameters:
//   - lookupFn: Function to call on cache miss to look up the handler.
//   - req: The ICAP request.
//
// Returns:
//   - handler.Handler: The found handler, or nil if not found.
//   - bool: True if cache hit, false if cache miss.
func (c *RouteCache) LookupHandlerFunc(lookupFn func(path string) (handler.Handler, bool), req *icap.Request) (handler.Handler, bool) {
	// Extract path from URI
	path := extractPath(req.URI)

	// Check cache first
	handler, hit := c.Get(req.Method, path)
	if hit {
		return handler, true
	}

	// Cache miss: call lookup function
	handler, exists := lookupFn(path)
	if !exists {
		return nil, false
	}

	// Cache the result for future lookups
	c.Put(req.Method, path, handler)

	return handler, false
}
