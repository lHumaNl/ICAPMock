// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"sync"
	"time"

	"log/slog"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// PreviewRateLimiter provides rate limiting for preview mode requests.
// It prevents DoS attacks by limiting the number of preview requests
// per client within a sliding time window.
//
// Thread-safety: All methods are safe for concurrent use.
type PreviewRateLimiter struct {
	ctx     context.Context
	clients map[string]*clientTracker
	metrics *metrics.Collector
	logger  *slog.Logger
	cancel  context.CancelFunc
	config  PreviewRateLimiterConfig
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// PreviewRateLimiterConfig contains configuration for preview rate limiting.
type PreviewRateLimiterConfig struct {
	// Enabled enables preview rate limiting.
	// Default: true
	Enabled bool

	// MaxRequests is the maximum number of preview requests allowed
	// per client within the time window.
	// Default: 100
	MaxRequests int

	// WindowSeconds is the duration of the sliding window in seconds.
	// Default: 60 seconds
	WindowSeconds int

	// MaxClients is the maximum number of clients to track.
	// When this limit is reached, the least recently used client is evicted.
	// Default: 10000
	MaxClients int

	// CleanupInterval is the interval between cleanup runs to remove
	// expired client entries from the map.
	// Default: 5 minutes
	CleanupInterval time.Duration
}

// clientTracker tracks request timestamps for a single client.
type clientTracker struct {
	lastAccess time.Time
	clientID   string
	requests   []time.Time
	remaining  int
}

// NewPreviewRateLimiter creates a new preview rate limiter with the given configuration.
//
// Parameters:
//   - config: Rate limiter configuration
//   - metrics: Metrics collector for recording rate limit events (can be nil)
//   - logger: Logger for structured logging (can be nil)
//
// Returns a new PreviewRateLimiter instance.
//
// Example:
//
//	limiter := handler.NewPreviewRateLimiter(
//	    handler.PreviewRateLimiterConfig{
//	        Enabled:       true,
//	        MaxRequests:   100,
//	        WindowSeconds: 60,
//	        MaxClients:    10000,
//	    },
//	    metricsCollector,
//	    logger,
//	)
func NewPreviewRateLimiter(
	config PreviewRateLimiterConfig,
	metrics *metrics.Collector,
	logger *slog.Logger,
) *PreviewRateLimiter {
	if config.WindowSeconds <= 0 {
		config.WindowSeconds = 60 // Default to 60 seconds
	}
	if config.MaxRequests <= 0 {
		config.MaxRequests = 100 // Default to 100 requests
	}
	if config.MaxClients <= 0 {
		config.MaxClients = 10000 // Default to 10000 clients
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 5 * time.Minute // Default to 5 minutes
	}

	// Create context for goroutine lifecycle management
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel managed elsewhere

	limiter := &PreviewRateLimiter{
		config:  config,
		clients: make(map[string]*clientTracker),
		metrics: metrics,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start cleanup goroutine if enabled
	if config.Enabled {
		limiter.wg.Add(1)
		go limiter.cleanupLoop()
	}

	return limiter
}

// CheckLimit checks if the client has exceeded the preview rate limit.
//
// Parameters:
//   - req: The ICAP request to check
//
// Returns:
//   - bool: true if limit is exceeded, false if allowed
//   - int: remaining requests in the window
//   - time.Duration: time until the oldest request expires
//
// This method is thread-safe.
func (l *PreviewRateLimiter) CheckLimit(req *icap.Request) (exceeded bool, remaining int, resetIn time.Duration) {
	// If rate limiting is disabled, always allow
	if !l.config.Enabled {
		return false, l.config.MaxRequests, 0
	}

	// Only check limits for preview requests
	if !req.IsPreviewMode() {
		return false, l.config.MaxRequests, 0
	}

	// Extract client ID
	clientID := l.extractClientID(req)

	// Hold lock for the entire check+update operation to prevent data races
	l.mu.Lock()
	defer l.mu.Unlock()

	// Get or create client tracker (lock already held)
	tracker := l.getOrCreateTrackerLocked(clientID)

	// Clean up expired requests from the window (lock already held)
	now := time.Now()
	windowStart := now.Add(-time.Duration(l.config.WindowSeconds) * time.Second)
	l.cleanupExpiredRequestsLocked(tracker, windowStart)

	// Check if limit is exceeded
	exceeded = len(tracker.requests) >= l.config.MaxRequests
	remaining = l.config.MaxRequests - len(tracker.requests)

	// Calculate time until oldest request expires
	if len(tracker.requests) > 0 {
		oldestRequest := tracker.requests[0]
		resetIn = oldestRequest.Sub(windowStart)
		if resetIn < 0 {
			resetIn = 0
		}
	} else {
		resetIn = 0
	}

	if !exceeded {
		// Add current request to tracker
		tracker.requests = append(tracker.requests, now)
		tracker.lastAccess = now
		tracker.remaining = remaining - 1
	} else {
		// Record metrics for rejected request
		if l.metrics != nil {
			l.metrics.RecordPreviewRequestRejected(clientID)
		}
		if l.logger != nil {
			l.logger.Warn("preview rate limit exceeded",
				"client_id", clientID,
				"requests", len(tracker.requests),
				"max_requests", l.config.MaxRequests,
				"window_seconds", l.config.WindowSeconds,
			)
		}
	}

	return exceeded, remaining, resetIn
}

// extractClientID extracts a unique client identifier from the request.
// Uses X-Client-ID header if present, otherwise falls back to ClientIP.
func (l *PreviewRateLimiter) extractClientID(req *icap.Request) string {
	// Check for X-Client-ID header first
	if clientID, exists := req.GetHeader("X-Client-ID"); exists && clientID != "" {
		return clientID
	}

	// Fall back to client IP
	if req.ClientIP != "" {
		return req.ClientIP
	}

	// Last resort: use remote address
	if req.RemoteAddr != "" {
		return req.RemoteAddr
	}

	// No identifier found
	return "unknown"
}

// getOrCreateTracker gets an existing client tracker or creates a new one.
// Handles LRU eviction if MaxClients limit is reached.
func (l *PreviewRateLimiter) getOrCreateTracker(clientID string) *clientTracker {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.getOrCreateTrackerLocked(clientID)
}

// getOrCreateTrackerLocked is like getOrCreateTracker but assumes the lock is already held.
func (l *PreviewRateLimiter) getOrCreateTrackerLocked(clientID string) *clientTracker {
	// Check if tracker exists
	if tracker, exists := l.clients[clientID]; exists {
		return tracker
	}

	// Evict oldest client if limit reached
	if len(l.clients) >= l.config.MaxClients {
		l.evictOldestClient()
	}

	// Create new tracker
	tracker := &clientTracker{
		clientID:   clientID,
		requests:   make([]time.Time, 0, l.config.MaxRequests),
		lastAccess: time.Now(),
		remaining:  l.config.MaxRequests,
	}
	l.clients[clientID] = tracker

	// Update metrics
	if l.metrics != nil {
		l.metrics.SetPreviewClientsActive(len(l.clients))
	}

	return tracker
}

// evictOldestClient removes the least recently used client tracker.
func (l *PreviewRateLimiter) evictOldestClient() {
	var oldestClientID string
	var oldestAccess time.Time

	for id, tracker := range l.clients {
		if oldestClientID == "" || tracker.lastAccess.Before(oldestAccess) {
			oldestClientID = id
			oldestAccess = tracker.lastAccess
		}
	}

	if oldestClientID != "" {
		delete(l.clients, oldestClientID)
		if l.logger != nil {
			l.logger.Debug("evicted oldest preview rate limit client",
				"client_id", oldestClientID,
			)
		}
	}
}

// cleanupExpiredRequests removes requests older than the sliding window.
func (l *PreviewRateLimiter) cleanupExpiredRequests(tracker *clientTracker, windowStart time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupExpiredRequestsLocked(tracker, windowStart)
}

// cleanupExpiredRequestsLocked is like cleanupExpiredRequests but assumes the lock is already held.
func (l *PreviewRateLimiter) cleanupExpiredRequestsLocked(tracker *clientTracker, windowStart time.Time) {
	// Find the first request within the window
	index := 0
	for i, reqTime := range tracker.requests {
		if reqTime.After(windowStart) {
			index = i
			break
		}
	}

	// Remove expired requests (all requests before index)
	// If loop completed without finding a request in window (all expired), index=0 but we need to clear all
	if len(tracker.requests) == 0 {
		// Nothing to cleanup
		return
	}

	if index > 0 || (index == 0 && !tracker.requests[0].After(windowStart)) {
		if l.logger != nil {
			l.logger.Debug("cleaning up expired requests",
				"before", len(tracker.requests),
				"after", len(tracker.requests)-index,
				"index", index,
			)
		}
		tracker.requests = tracker.requests[index:]
	}
}

// cleanupLoop periodically cleans up expired client entries.
// Runs in a separate goroutine and exits gracefully on context cancellation.
func (l *PreviewRateLimiter) cleanupLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.ctx.Done():
			// Context canceled, exit gracefully
			if l.logger != nil {
				l.logger.Debug("preview rate limiter cleanup loop shutting down")
			}
			return
		}
	}
}

// cleanup removes all expired client entries from the tracker map.
func (l *PreviewRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Duration(l.config.WindowSeconds) * time.Second)

	var expiredClients []string

	for id, tracker := range l.clients {
		// Clean up expired requests for this tracker
		index := 0
		for i, reqTime := range tracker.requests {
			if reqTime.After(windowStart) {
				index = i
				break
			}
		}
		tracker.requests = tracker.requests[index:]

		// Mark client for deletion if no requests and not recently accessed
		if len(tracker.requests) == 0 && now.Sub(tracker.lastAccess) > l.config.CleanupInterval {
			expiredClients = append(expiredClients, id)
		}
	}

	// Remove expired clients
	for _, id := range expiredClients {
		delete(l.clients, id)
		if l.logger != nil {
			l.logger.Debug("removed expired preview rate limit client",
				"client_id", id,
			)
		}
	}

	// Update metrics
	if l.metrics != nil && len(l.clients) > 0 {
		l.metrics.SetPreviewClientsActive(len(l.clients))
	}
}

// GetClientCount returns the current number of tracked clients.
// This method is thread-safe.
func (l *PreviewRateLimiter) GetClientCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.clients)
}

// GetClientInfo returns information about a specific client tracker.
// This method is thread-safe.
//
// Returns nil if the client is not being tracked.
func (l *PreviewRateLimiter) GetClientInfo(clientID string) *clientTracker {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.clients[clientID]
}

// Shutdown gracefully stops the cleanup goroutine.
// This method should be called when the rate limiter is no longer needed
// to prevent goroutine leaks. It is safe to call multiple times.
//
// The method will wait for the cleanup goroutine to exit with a timeout
// to ensure clean shutdown.
func (l *PreviewRateLimiter) Shutdown() {
	// Cancel the context to signal cleanup goroutine to exit
	if l.cancel != nil {
		l.cancel()
	}

	// Wait for cleanup goroutine to exit with timeout
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutine exited successfully
		if l.logger != nil {
			l.logger.Debug("preview rate limiter shutdown completed")
		}
	case <-time.After(5 * time.Second):
		// Timeout waiting for goroutine to exit
		if l.logger != nil {
			l.logger.Warn("preview rate limiter shutdown timeout after 5 seconds")
		}
	}
}
