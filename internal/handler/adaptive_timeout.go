// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Default configuration values for adaptive timeout.
const (
	// DefaultSampleSize is the number of requests to track per endpoint.
	DefaultSampleSize = 1000
	// DefaultAdjustmentInterval is the time between timeout adjustments.
	DefaultAdjustmentInterval = 10 * time.Second
	// DefaultAdjustmentFrequency is the number of requests between adjustments.
	DefaultAdjustmentFrequency = 100
	// DefaultSafetyMultiplier multiplies P95 latency for timeout calculation.
	DefaultSafetyMultiplier = 2.0
	// DefaultMinTimeout is the minimum allowed timeout.
	DefaultMinTimeout = 10 * time.Millisecond
	// DefaultMaxTimeout is the maximum allowed timeout.
	DefaultMaxTimeout = 60 * time.Second
	// DefaultFallbackTimeout is used when insufficient data is available.
	DefaultFallbackTimeout = 30 * time.Second
	// MinDataPoints is the minimum number of requests needed to calculate P95.
	MinDataPoints = 100
)

// endpointKey uniquely identifies an endpoint (method + path).
type endpointKey struct {
	method string
	path   string
}

// endpointStats tracks request duration history for a single endpoint.
type endpointStats struct {
	lastAdjust time.Time
	durations  []time.Duration
	head       int
	count      int
	totalCount int64
	sampleSize int
	mu         sync.Mutex
}

// AdaptiveTimeoutTracker tracks request durations and calculates adaptive timeouts.
// It maintains P95 latency history per endpoint and periodically adjusts timeouts
// based on observed performance.
//
// Thread-safe for concurrent use. Uses per-endpoint locking for minimal contention.
type AdaptiveTimeoutTracker struct {
	stats              map[endpointKey]*endpointStats
	currentTimeout     map[endpointKey]time.Duration
	metrics            *metrics.Collector
	sampleSize         int
	adjustmentInterval time.Duration
	adjustmentFreq     int64
	safetyMultiplier   float64
	minTimeout         time.Duration
	maxTimeout         time.Duration
	fallbackTimeout    time.Duration
	mu                 sync.RWMutex
}

// AdaptiveTimeoutConfig holds configuration for the adaptive timeout tracker.
type AdaptiveTimeoutConfig struct {
	Metrics             *metrics.Collector
	SampleSize          int
	AdjustmentInterval  time.Duration
	AdjustmentFrequency int64
	SafetyMultiplier    float64
	MinTimeout          time.Duration
	MaxTimeout          time.Duration
	FallbackTimeout     time.Duration
}

// DefaultAdaptiveTimeoutConfig returns the default configuration.
func DefaultAdaptiveTimeoutConfig() AdaptiveTimeoutConfig {
	return AdaptiveTimeoutConfig{
		SampleSize:          DefaultSampleSize,
		AdjustmentInterval:  DefaultAdjustmentInterval,
		AdjustmentFrequency: DefaultAdjustmentFrequency,
		SafetyMultiplier:    DefaultSafetyMultiplier,
		MinTimeout:          DefaultMinTimeout,
		MaxTimeout:          DefaultMaxTimeout,
		FallbackTimeout:     DefaultFallbackTimeout,
	}
}

// NewAdaptiveTimeoutTracker creates a new adaptive timeout tracker with the given configuration.
//
// Parameters:
//   - cfg: Configuration for the adaptive timeout tracker
//
// Returns:
//   - *AdaptiveTimeoutTracker: The created tracker
//
// Example:
//
//	cfg := handler.DefaultAdaptiveTimeoutConfig()
//	tracker := handler.NewAdaptiveTimeoutTracker(cfg)
func NewAdaptiveTimeoutTracker(cfg AdaptiveTimeoutConfig) *AdaptiveTimeoutTracker {
	if cfg.SampleSize <= 0 {
		cfg.SampleSize = DefaultSampleSize
	}
	if cfg.AdjustmentInterval <= 0 {
		cfg.AdjustmentInterval = DefaultAdjustmentInterval
	}
	if cfg.AdjustmentFrequency <= 0 {
		cfg.AdjustmentFrequency = DefaultAdjustmentFrequency
	}
	if cfg.SafetyMultiplier <= 0 {
		cfg.SafetyMultiplier = DefaultSafetyMultiplier
	}
	if cfg.MinTimeout <= 0 {
		cfg.MinTimeout = DefaultMinTimeout
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = DefaultMaxTimeout
	}
	if cfg.FallbackTimeout <= 0 {
		cfg.FallbackTimeout = DefaultFallbackTimeout
	}

	return &AdaptiveTimeoutTracker{
		stats:              make(map[endpointKey]*endpointStats),
		currentTimeout:     make(map[endpointKey]time.Duration),
		sampleSize:         cfg.SampleSize,
		adjustmentInterval: cfg.AdjustmentInterval,
		adjustmentFreq:     cfg.AdjustmentFrequency,
		safetyMultiplier:   cfg.SafetyMultiplier,
		minTimeout:         cfg.MinTimeout,
		maxTimeout:         cfg.MaxTimeout,
		fallbackTimeout:    cfg.FallbackTimeout,
		metrics:            cfg.Metrics,
	}
}

// RecordDuration records the duration of a request for the given endpoint.
// This should be called after each request completes.
//
// Parameters:
//   - method: The ICAP method (e.g., "REQMOD", "RESPMOD")
//   - path: The request path
//   - duration: The time taken to process the request
//
// This method is safe for concurrent use.
func (t *AdaptiveTimeoutTracker) RecordDuration(method, path string, duration time.Duration) {
	key := endpointKey{method: method, path: path}

	// Get or create endpoint stats (short map-level lock)
	t.mu.RLock()
	stats, exists := t.stats[key]
	t.mu.RUnlock()

	if !exists {
		t.mu.Lock()
		// Double-check after acquiring write lock
		stats, exists = t.stats[key]
		if !exists {
			stats = &endpointStats{
				durations:  make([]time.Duration, t.sampleSize),
				sampleSize: t.sampleSize,
				lastAdjust: time.Now(),
			}
			t.stats[key] = stats
		}
		t.mu.Unlock()
	}

	// Per-endpoint lock for recording duration (no global contention)
	stats.mu.Lock()

	// Ring buffer insert — O(1)
	stats.durations[stats.head] = duration
	stats.head = (stats.head + 1) % stats.sampleSize
	if stats.count < stats.sampleSize {
		stats.count++
	}
	stats.totalCount++

	// Check if we should adjust timeout (every N requests or every interval)
	shouldAdjust := stats.totalCount%t.adjustmentFreq == 0 ||
		time.Since(stats.lastAdjust) >= t.adjustmentInterval

	if shouldAdjust && stats.count >= MinDataPoints {
		stats.lastAdjust = time.Now()
		t.adjustTimeout(key, method, path, stats)
	}

	stats.mu.Unlock()
}

// GetTimeout returns the current adaptive timeout for the given endpoint.
// If insufficient data is available, returns the fallback timeout.
//
// Parameters:
//   - method: The ICAP method (e.g., "REQMOD", "RESPMOD")
//   - path: The request path
//
// Returns:
//   - time.Duration: The calculated timeout for this endpoint
//
// This method is safe for concurrent use.
func (t *AdaptiveTimeoutTracker) GetTimeout(method, path string) time.Duration {
	key := endpointKey{method: method, path: path}

	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return cached timeout if available
	if timeout, exists := t.currentTimeout[key]; exists {
		return timeout
	}

	// Return fallback timeout if no data available
	return t.fallbackTimeout
}

// adjustTimeout calculates and updates the timeout based on P95 latency.
// This method must be called with the lock held.
func (t *AdaptiveTimeoutTracker) adjustTimeout(key endpointKey, method, path string, stats *endpointStats) {
	// Calculate P95 latency
	p95 := t.calculateP95(stats)

	// Calculate timeout as P95 * safety_multiplier
	timeout := time.Duration(float64(p95) * t.safetyMultiplier)

	// Clamp to min/max bounds
	if timeout < t.minTimeout {
		timeout = t.minTimeout
	}
	if timeout > t.maxTimeout {
		timeout = t.maxTimeout
	}

	// Update current timeout (need map write lock)
	t.mu.Lock()
	t.currentTimeout[key] = timeout
	t.mu.Unlock()

	// Update metrics if collector is available
	if t.metrics != nil {
		t.metrics.SetAdaptiveTimeout(method, path, float64(timeout.Milliseconds()))
	}
}

// calculateP95 calculates the 95th percentile from a slice of durations.
// Uses quickselect algorithm for O(n) average time complexity.
func (t *AdaptiveTimeoutTracker) calculateP95(stats *endpointStats) time.Duration {
	if stats.count == 0 {
		return t.fallbackTimeout
	}

	// Copy active elements from ring buffer
	durationsCopy := make([]time.Duration, stats.count)
	if stats.count < stats.sampleSize {
		copy(durationsCopy, stats.durations[:stats.count])
	} else {
		// Ring buffer is full — copy from head to end, then start to head
		n := copy(durationsCopy, stats.durations[stats.head:])
		copy(durationsCopy[n:], stats.durations[:stats.head])
	}

	// Calculate P95 index (95th percentile)
	index := int(float64(len(durationsCopy)) * 0.95)
	if index >= len(durationsCopy) {
		index = len(durationsCopy) - 1
	}

	// Use quickselect to find the k-th smallest element (O(n) average)
	return t.quickselect(durationsCopy, 0, len(durationsCopy)-1, index)
}

// quickselect finds the k-th smallest element in the slice using the
// quickselect algorithm. This provides O(n) average time complexity,
// compared to O(n log n) for sorting the entire slice.
//
// Parameters:
//   - arr: Slice of durations to search
//   - left: Left boundary of current partition
//   - right: Right boundary of current partition
//   - k: Index of the element to find (0-based)
//
// Returns:
//   - time.Duration: The k-th smallest duration
func (t *AdaptiveTimeoutTracker) quickselect(arr []time.Duration, left, right, k int) time.Duration {
	// Base case: only one element in the partition
	if left == right {
		return arr[left]
	}

	// Partition the array and get the pivot index
	pivotIndex := t.partition(arr, left, right)

	// Check if pivot is the k-th element
	switch {
	case k == pivotIndex:
		return arr[k]
	case k < pivotIndex:
		// k-th element is in the left partition
		return t.quickselect(arr, left, pivotIndex-1, k)
	default:
		// k-th element is in the right partition
		return t.quickselect(arr, pivotIndex+1, right, k)
	}
}

// partition partitions the array around a pivot and returns the final pivot index.
// Uses the rightmost element as pivot for simplicity.
func (t *AdaptiveTimeoutTracker) partition(arr []time.Duration, left, right int) int {
	// Choose rightmost element as pivot
	pivot := arr[right]
	i := left

	// Move all elements smaller than pivot to the left
	for j := left; j < right; j++ {
		if arr[j] <= pivot {
			arr[i], arr[j] = arr[j], arr[i]
			i++
		}
	}

	// Place pivot in its correct position
	arr[i], arr[right] = arr[right], arr[i]
	return i
}

// AdaptiveTimeoutMiddleware wraps a handler with adaptive timeout logic.
// It sets a context deadline based on the tracker's calculated timeout
// and records request duration for timeout adjustment.
//
// Thread-safe for concurrent use.
type AdaptiveTimeoutMiddleware struct {
	tracker  *AdaptiveTimeoutTracker
	next     Handler
	basePath string
}

// AdaptiveTimeoutMiddlewareConfig holds configuration for the adaptive timeout middleware.
type AdaptiveTimeoutMiddlewareConfig struct {
	// Tracker is the adaptive timeout tracker to use.
	Tracker *AdaptiveTimeoutTracker
	// BasePath is the base path to use for endpoint identification.
	// If empty, the full request URI will be used.
	BasePath string
}

// NewAdaptiveTimeoutMiddleware creates a new adaptive timeout middleware.
//
// Parameters:
//   - cfg: Configuration for the middleware
//
// Returns:
//   - Middleware: A middleware function that wraps handlers
//
// Example:
//
//	cfg := handler.DefaultAdaptiveTimeoutConfig()
//	tracker := handler.NewAdaptiveTimeoutTracker(cfg)
//	middlewareCfg := handler.AdaptiveTimeoutMiddlewareConfig{
//	    Tracker: tracker,
//	    BasePath: "/icap",
//	}
//	middleware := handler.NewAdaptiveTimeoutMiddleware(middlewareCfg)
//	handler := middleware(baseHandler)
func NewAdaptiveTimeoutMiddleware(cfg AdaptiveTimeoutMiddlewareConfig) Middleware {
	return func(next Handler) Handler {
		return &AdaptiveTimeoutMiddleware{
			tracker:  cfg.Tracker,
			next:     next,
			basePath: cfg.BasePath,
		}
	}
}

// Handle implements the Handler interface.
// It sets a context deadline based on the adaptive timeout and records
// the request duration after completion.
func (m *AdaptiveTimeoutMiddleware) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	start := time.Now()

	// Determine the path for endpoint identification
	path := req.URI
	if m.basePath != "" {
		path = m.basePath
	}

	// Get the adaptive timeout for this endpoint
	timeout := m.tracker.GetTimeout(m.next.Method(), path)

	// Set context deadline with the adaptive timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the handler
	resp, err := m.next.Handle(ctx, req)

	// Record the request duration for timeout adjustment
	duration := time.Since(start)
	m.tracker.RecordDuration(m.next.Method(), path, duration)

	// Check if context deadline was exceeded
	if ctx.Err() == context.DeadlineExceeded {
		// Record timeout in metrics
		if m.tracker.metrics != nil {
			m.tracker.metrics.RecordRequestTimeout(m.next.Method())
		}
	}

	return resp, err
}

// Method returns the ICAP method this handler processes.
func (m *AdaptiveTimeoutMiddleware) Method() string {
	return m.next.Method()
}

// Wrap returns a Middleware function that wraps handlers with adaptive timeout functionality.
// This method allows the AdaptiveTimeoutMiddleware to be used with the standard middleware chain.
func (m *AdaptiveTimeoutMiddleware) Wrap(next Handler) Handler {
	return &AdaptiveTimeoutMiddleware{
		tracker:  m.tracker,
		next:     next,
		basePath: m.basePath,
	}
}

// GetTracker returns the adaptive timeout tracker for monitoring purposes.
// This is a helper method that can be used with type assertion.
func (m *AdaptiveTimeoutMiddleware) GetTracker() *AdaptiveTimeoutTracker {
	return m.tracker
}

// GetAdaptiveTimeoutTracker returns the adaptive timeout tracker from a handler if it is an AdaptiveTimeoutMiddleware.
// Returns nil if the handler is not an AdaptiveTimeoutMiddleware.
//
// Example:
//
//	tracker := GetAdaptiveTimeoutTracker(wrappedHandler)
//	if tracker != nil {
//	    timeout := tracker.GetTimeout("REQMOD", "/test")
//	}
func GetAdaptiveTimeoutTracker(h Handler) *AdaptiveTimeoutTracker {
	if am, ok := h.(*AdaptiveTimeoutMiddleware); ok {
		return am.tracker
	}
	return nil
}
