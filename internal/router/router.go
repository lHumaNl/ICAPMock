// Copyright 2026 ICAP Mock

// Package router provides request routing with caching for the ICAP server.
package router

import (
	"context"
	"fmt"

	"strings"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Route represents a registered route with its path and handler.
type Route struct {
	Handler handler.Handler
	Path    string
}

// Router implements ICAP request routing.
// It routes requests to handlers based on the URI path extracted from
// the ICAP request URI.
//
// The router is safe for concurrent use. Routes can be added and requests
// can be served concurrently.
//
// It uses sync.Map for lock-free route storage, eliminating lock contention
// at high throughput (10k+ RPS). Read operations (lookup) are lock-free,
// while write operations (registration) use minimal internal synchronization.
//
// It includes an LRU cache for route lookups to reduce latency for
// frequently accessed routes.
type Router struct {
	cache  *RouteCache
	routes sync.Map
}

// NewRouter creates a new Router with an empty route table.
// It initializes a route cache with default configuration (1000 max entries, 5 minute TTL).
func NewRouter() *Router {
	return &Router{
		cache: NewRouteCache(DefaultMaxEntries, DefaultTTL),
	}
}

// NewRouterWithCache creates a new Router with custom cache configuration.
//
// Parameters:
//   - maxEntries: Maximum number of entries in the route cache.
//   - ttl: Time-to-live for cached route entries (0 means no expiration).
//
// Example:
//
//	r := NewRouterWithCache(500, 10*time.Minute)
func NewRouterWithCache(maxEntries int, ttl time.Duration) *Router {
	return &Router{
		cache: NewRouteCache(maxEntries, ttl),
	}
}

// Handle registers a handler for the given path.
// If a handler already exists for the path, it is replaced.
//
// The cache is invalidated for this path to ensure the new handler is used.
//
// Parameters:
//   - path: The URI path to match (e.g., "/reqmod", "/respmod"). Must not be empty.
//   - h: The handler to register. Must not be nil.
//
// Returns an error if path is empty or handler is nil.
func (r *Router) Handle(path string, h handler.Handler) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if h == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// sync.Map.Store is safe for concurrent use
	r.routes.Store(path, h)

	// Invalidate cache for this path to use the new handler
	r.cache.Clear()
	return nil
}

// HandleFunc registers a handler function for the given path.
// This is a convenience method for registering simple handlers without
// creating a full Handler implementation.
//
// The cache is invalidated for this path to ensure the new handler is used.
//
// Parameters:
//   - path: The URI path to match (e.g., "/reqmod", "/respmod"). Must not be empty.
//   - fn: The handler function to register. Must not be nil.
//
// Returns an error if path is empty or function is nil.
func (r *Router) HandleFunc(path string, fn func(ctx context.Context, req *icap.Request) (*icap.Response, error)) error {
	if fn == nil {
		return fmt.Errorf("handler function cannot be nil")
	}
	return r.Handle(path, handler.WrapHandler(handler.HandlerFunc(fn), ""))
}

// Serve route the request to the appropriate handler.
// It extracts the path from the request URI and looks up the matching handler.
//
// Route lookups are cached using an LRU cache to reduce latency for frequently
// accessed routes. The cache key is formed from the method and path.
//
// Route lookups use sync.Map for lock-free operation at high concurrency.
// Read operations (lookup) are lock-free, providing minimal latency at 10k+ RPS.
//
// If no handler is found for the path, it returns a 404 ICAP Service not found response.
//
// Parameters:
//   - ctx: Context for cancellation and timeout handling.
//   - req: The ICAP request to route.
//
// Returns the response from the handler, or a 404 response if no handler found.
func (r *Router) Serve(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	// Use cache-aware handler lookup with sync.Map
	h, _ := r.cache.LookupHandlerFunc(func(path string) (handler.Handler, bool) {
		val, ok := r.routes.Load(path)
		if !ok {
			return nil, false
		}
		return val.(handler.Handler), true //nolint:errcheck
	}, req)

	if h == nil {
		// No handler found, return 404
		return icap.NewResponseError(icap.StatusNotFound, "ICAP service not found"), nil
	}

	// Delegate to handler
	return h.Handle(ctx, req)
}

// Routes returns a copy of all registered routes.
// The returned slice is a snapshot and will not reflect subsequent
// route registrations.
func (r *Router) Routes() []Route {
	var routes []Route

	r.routes.Range(func(key, value interface{}) bool {
		path := key.(string)               //nolint:errcheck
		h := value.(handler.Handler) //nolint:errcheck
		routes = append(routes, Route{
			Path:    path,
			Handler: h,
		})
		return true
	})

	return routes
}

// CacheStats returns statistics about the route cache.
// This includes cache hits, misses, evictions, and current size.
//
// Returns:
//   - hits: Total number of cache hits.
//   - misses: Total number of cache misses.
//   - evictions: Total number of cache evictions.
//   - size: Current number of entries in the cache.
func (r *Router) CacheStats() (hits, misses, evictions, size int64) {
	return r.cache.Hits(), r.cache.Misses(), r.cache.Evictions(), int64(r.cache.Size())
}

// CacheClear clears the route cache.
// This is useful when routes are reloaded and cached entries need to be invalidated.
func (r *Router) CacheClear() {
	r.cache.Clear()
}

// CacheResetMetrics resets the cache metrics (hits, misses, evictions).
// This is useful for monitoring and testing purposes.
func (r *Router) CacheResetMetrics() {
	r.cache.ResetMetrics()
}

// extractPath extracts the path component from an ICAP URI.
// ICAP URI format: icap://server:port/path?query
//
// Examples:
//   - icap://localhost:1344/reqmod -> /reqmod
//   - icap://localhost:1344/reqmod?preview=1 -> /reqmod
//   - icap://localhost:1344/api/v1/reqmod -> /api/v1/reqmod
//   - icap://localhost:1344/ -> /
//   - invalid-uri -> /invalid-uri (fallback)
func extractPath(uri string) string {
	// Handle empty URI
	if uri == "" {
		return "/"
	}

	// Fast path for ICAP URIs: icap://host:port/path?query
	// Find the scheme separator
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd != -1 {
		// Find the path after host:port (third slash)
		rest := uri[schemeEnd+3:]
		slashIdx := strings.IndexByte(rest, '/')
		if slashIdx == -1 {
			return "/"
		}
		path := rest[slashIdx:]
		// Strip query string
		if qidx := strings.IndexByte(path, '?'); qidx != -1 {
			path = path[:qidx]
		}
		if path == "" {
			return "/"
		}
		return path
	}

	// No scheme — treat as path directly
	path := uri
	if qidx := strings.IndexByte(path, '?'); qidx != -1 {
		path = path[:qidx]
	}
	if path == "" || path[0] != '/' {
		return "/" + path
	}
	return path
}
