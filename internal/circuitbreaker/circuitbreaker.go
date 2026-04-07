// Copyright 2026 ICAP Mock

package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"
)

var (
	// ErrCircuitOpen is returned when the circuit breaker is in OPEN state
	// and rejects a request without attempting execution.
	ErrCircuitOpen = fmt.Errorf("circuit breaker is open")
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed is the normal operating state where all requests pass through.
	StateClosed State = iota

	// StateHalfOpen is the testing state where limited requests are allowed
	// to test if the component has recovered.
	StateHalfOpen

	// StateOpen is the failing state where requests are rejected immediately.
	StateOpen
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	default:
		return "UNKNOWN"
	}
}

// Config holds circuit breaker configuration parameters.
type Config struct {
	// FailureThreshold is the number of failures required to open the circuit.
	// Failures are counted in a rolling time window.
	// Default: 5
	FailureThreshold int

	// SuccessThreshold is the number of consecutive successes required
	// in HALF_OPEN state to transition to CLOSED state.
	// Default: 3
	SuccessThreshold int

	// OpenTimeout is the duration to wait in OPEN state before
	// transitioning to HALF_OPEN to test recovery.
	// Default: 30s
	OpenTimeout time.Duration

	// HalfOpenMaxRequests is the maximum number of requests allowed
	// in HALF_OPEN state before rejecting additional requests.
	// Default: 1
	HalfOpenMaxRequests int

	// RollingWindow is the time window for counting failures.
	// Older failures outside this window are not counted.
	// Default: 60s
	RollingWindow time.Duration

	// WindowBuckets is the number of time buckets in the rolling window.
	// More buckets provide finer granularity but use more memory.
	// Default: 60 (1-second buckets with 60s window)
	WindowBuckets int

	// Enabled determines if the circuit breaker is active.
	// When disabled, all requests pass through without circuit breaker logic.
	// Default: true
	Enabled bool
}

// DefaultConfig returns a default configuration suitable for most use cases.
func DefaultConfig() Config {
	return Config{
		FailureThreshold:    5,
		SuccessThreshold:    3,
		OpenTimeout:         30 * time.Second,
		HalfOpenMaxRequests: 1,
		RollingWindow:       60 * time.Second,
		WindowBuckets:       60,
		Enabled:             true,
	}
}

// bucket holds time-bucketed failure/success counters.
type bucket struct {
	failures  atomic.Int64
	successes atomic.Int64
	requests  atomic.Int64
	timestamp atomic.Int64
}

// CircuitBreaker implements the circuit breaker pattern with sliding window.
type CircuitBreaker struct {
	metrics          MetricsRecorder
	logger           *slog.Logger
	name             string
	buckets          []*bucket
	config           Config
	lastStateChange  atomic.Int64
	state            State
	bucketCount      int
	bucketDuration   time.Duration
	currentBucket    atomic.Int64
	halfOpenRequests atomic.Int64
	lastFailure      atomic.Int64
	stateMu          sync.RWMutex
}

// MetricsRecorder records circuit breaker metrics for monitoring.
// This interface allows integration with Prometheus or other metrics systems.
type MetricsRecorder interface {
	// SetCircuitBreakerState records the current circuit breaker state.
	SetCircuitBreakerState(component string, state string)

	// RecordCircuitBreakerTransition records a state transition.
	RecordCircuitBreakerTransition(component string, fromState string, toState string)

	// RecordCircuitBreakerFailure records a failure event.
	RecordCircuitBreakerFailure(component string)
}

// Stats holds current circuit breaker statistics.
type Stats struct {
	LastFailure      time.Time
	LastStateChange  time.Time
	State            State
	Failures         int64
	Successes        int64
	Requests         int64
	HalfOpenRequests int64
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
//
// Parameters:
//   - name: Component name for logging and metrics (e.g., "storage")
//   - config: Circuit breaker configuration
//   - logger: Structured logger (nil uses no logging)
//   - metrics: Metrics recorder (nil uses no metrics)
//
// Returns a new circuit breaker instance ready for use.
//
// Example:
//
//	cb := NewCircuitBreaker("storage", config, logger, metrics)
func NewCircuitBreaker(name string, config Config, logger *slog.Logger, metrics MetricsRecorder) *CircuitBreaker {
	// Validate and sanitize configuration
	if config.WindowBuckets <= 0 {
		config.WindowBuckets = 60
	}
	if config.RollingWindow <= 0 {
		config.RollingWindow = 60 * time.Second
	}

	// Calculate bucket duration
	bucketDuration := config.RollingWindow / time.Duration(config.WindowBuckets)

	// Initialize time buckets with timestamps
	buckets := make([]*bucket, config.WindowBuckets)
	now := time.Now().UnixNano()
	for i := range buckets {
		buckets[i] = &bucket{}
		buckets[i].timestamp.Store(now + int64(i)*int64(bucketDuration))
	}

	// Create circuit breaker instance
	cb := &CircuitBreaker{
		name:           name,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		state:          StateClosed,
		buckets:        buckets,
		bucketCount:    config.WindowBuckets,
		bucketDuration: bucketDuration,
	}

	// Set current bucket index
	currentBucketIndex := cb.getCurrentBucketIndex()
	cb.currentBucket.Store(int64(currentBucketIndex))
	cb.lastStateChange.Store(now)

	// Log initialization
	if logger != nil {
		logger.Info("circuit breaker initialized",
			"component", name,
			"failure_threshold", config.FailureThreshold,
			"success_threshold", config.SuccessThreshold,
			"open_timeout", config.OpenTimeout,
			"rolling_window", config.RollingWindow,
			"enabled", config.Enabled,
		)
	}

	// Record initial state
	if metrics != nil {
		metrics.SetCircuitBreakerState(name, StateClosed.String())
	}

	return cb
}

// Call executes the given function with circuit breaker protection.
//
// If the circuit is OPEN, returns ErrCircuitOpen without executing the function.
// If the circuit is HALF_OPEN, allows limited requests to test recovery.
// If the circuit is CLOSED, executes the function normally and tracks results.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - fn: Function to execute with circuit breaker protection
//
// Returns the function's error or ErrCircuitOpen if rejected.
//
// Example:
//
//	err := cb.Call(ctx, func() error {
//	    return writeToDisk(data)
//	})
func (cb *CircuitBreaker) Call(ctx context.Context, fn func() error) error {
	// If disabled, bypass circuit breaker logic
	if !cb.config.Enabled {
		return fn()
	}

	// Check current state
	state := cb.State()

	// OPEN state: Check if we should allow a request
	if state == StateOpen {
		if cb.shouldAllowRequest() {
			return cb.executeRequest(ctx, fn)
		}
		return ErrCircuitOpen
	}

	// HALF_OPEN state: Check request limit
	if state == StateHalfOpen {
		if cb.halfOpenRequests.Load() >= int64(cb.config.HalfOpenMaxRequests) {
			return ErrCircuitOpen
		}
		return cb.executeRequest(ctx, fn)
	}

	// CLOSED state: Execute normally
	return cb.executeRequest(ctx, fn)
}

// executeRequest executes the protected function and records the result.
func (cb *CircuitBreaker) executeRequest(ctx context.Context, fn func() error) error {
	// Update current bucket
	bucketIdx := cb.updateCurrentBucket()

	// Execute the protected function
	err := fn()

	// Record result
	if err != nil {
		cb.recordFailure(bucketIdx)
	} else {
		cb.recordSuccess(bucketIdx)
	}

	return err
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() State {
	cb.stateMu.RLock()
	defer cb.stateMu.RUnlock()
	return cb.state
}

// RecordResult manually records a success or failure.
// Use this when not using the Call() method.
//
// Parameters:
//   - success: true to record a success, false to record a failure
//
// Example:
//
//	cb.RecordResult(err == nil)
func (cb *CircuitBreaker) RecordResult(success bool) {
	bucketIdx := cb.updateCurrentBucket()

	if success {
		cb.recordSuccess(bucketIdx)
	} else {
		cb.recordFailure(bucketIdx)
	}
}

// Reset resets the circuit breaker to CLOSED state.
// This clears all counters and allows requests to pass through immediately.
//
// Example:
//
//	cb.Reset()
func (cb *CircuitBreaker) Reset() {
	cb.stateMu.Lock()
	defer cb.stateMu.Unlock()

	oldState := cb.state
	if oldState == StateClosed {
		return // Already closed, nothing to do
	}

	// Reset to CLOSED state
	cb.setState(StateClosed)

	// Clear all counters
	for _, b := range cb.buckets {
		b.failures.Store(0)
		b.successes.Store(0)
		b.requests.Store(0)
	}

	// Reset half-open request count
	cb.halfOpenRequests.Store(0)
	cb.lastFailure.Store(0)

	// Log reset
	if cb.logger != nil {
		cb.logger.Info("circuit breaker reset",
			"component", cb.name,
			"from_state", oldState,
		)
	}

	// Record transition
	if cb.metrics != nil {
		cb.metrics.RecordCircuitBreakerTransition(cb.name, oldState.String(), StateClosed.String())
		cb.metrics.SetCircuitBreakerState(cb.name, StateClosed.String())
	}
}

// Stats returns current circuit breaker statistics.
//
// Example:
//
//	stats := cb.Stats()
//	fmt.Printf("State: %s, Failures: %d\n", stats.State, stats.Failures)
func (cb *CircuitBreaker) Stats() Stats {
	state := cb.State()

	// Sum counters from all buckets
	totalFailures := atomic.Int64{}
	totalSuccesses := atomic.Int64{}
	totalRequests := atomic.Int64{}

	for _, b := range cb.buckets {
		totalFailures.Add(b.failures.Load())
		totalSuccesses.Add(b.successes.Load())
		totalRequests.Add(b.requests.Load())
	}

	// Get timestamps
	lastFailureTime := time.Unix(0, cb.lastFailure.Load())
	lastStateChangeTime := time.Unix(0, cb.lastStateChange.Load())

	return Stats{
		State:            state,
		Failures:         totalFailures.Load(),
		Successes:        totalSuccesses.Load(),
		Requests:         totalRequests.Load(),
		HalfOpenRequests: cb.halfOpenRequests.Load(),
		LastFailure:      lastFailureTime,
		LastStateChange:  lastStateChangeTime,
	}
}

// getCurrentBucketIndex calculates the current bucket index based on time.
func (cb *CircuitBreaker) getCurrentBucketIndex() int {
	now := time.Now().UnixNano()
	idx := (now / int64(cb.bucketDuration)) % int64(cb.bucketCount)
	return int(idx)
}

// updateCurrentBucket ensures the current bucket is properly initialized.
func (cb *CircuitBreaker) updateCurrentBucket() int {
	currentIdx := int(cb.currentBucket.Load())
	newIdx := cb.getCurrentBucketIndex()

	// If bucket changed, try to atomically update the index first
	if newIdx != currentIdx {
		// Use CAS to atomically update the current bucket index
		if cb.currentBucket.CompareAndSwap(int64(currentIdx), int64(newIdx)) {
			// We won the race - reset the new bucket
			newBucket := cb.buckets[newIdx]
			newBucket.failures.Store(0)
			newBucket.successes.Store(0)
			newBucket.requests.Store(0)
			newBucket.timestamp.Store(time.Now().UnixNano())
			return newIdx
		}
		// CAS failed - another goroutine already updated it
		// Fall through to return the updated index
	}

	// At this point, currentBucket is already correct (either we won CAS or no change needed)
	// Re-read to get the latest value
	currentIdx = int(cb.currentBucket.Load())

	// Check if current bucket needs reset (stale timestamp)
	bucket := cb.buckets[currentIdx]
	now := time.Now().UnixNano()
	if bucket.timestamp.Load() == 0 || now-bucket.timestamp.Load() > int64(cb.bucketDuration) {
		bucket.timestamp.Store(now)
	}

	return currentIdx
}

// getFailureCount returns the total failures in the rolling window.
func (cb *CircuitBreaker) getFailureCount() int {
	var totalFailures int64
	now := time.Now().UnixNano()
	windowStart := now - int64(cb.config.RollingWindow)

	// Sum failures from buckets within the rolling window
	for _, b := range cb.buckets {
		if b.timestamp.Load() >= windowStart {
			totalFailures += b.failures.Load()
		}
	}

	return int(totalFailures)
}

// recordSuccess records a successful operation.
func (cb *CircuitBreaker) recordSuccess(bucketIdx int) {
	bucket := cb.buckets[bucketIdx]
	bucket.successes.Add(1)
	bucket.requests.Add(1)

	state := cb.State()

	// HALF_OPEN state: Check if we should close the circuit
	switch state {
	case StateHalfOpen:
		cb.halfOpenRequests.Add(1)

		if cb.halfOpenRequests.Load() >= int64(cb.config.SuccessThreshold) {
			cb.transitionTo(StateClosed)
		}
	case StateClosed:
		// Reset half-open request counter on success in CLOSED state
		cb.halfOpenRequests.Store(0)
	}
}

// recordFailure records a failed operation.
func (cb *CircuitBreaker) recordFailure(bucketIdx int) {
	bucket := cb.buckets[bucketIdx]
	bucket.failures.Add(1)
	bucket.requests.Add(1)
	now := time.Now()
	cb.lastFailure.Store(now.UnixNano())

	// Record failure metric
	if cb.metrics != nil {
		cb.metrics.RecordCircuitBreakerFailure(cb.name)
	}

	// Log failure at debug level
	if cb.logger != nil {
		cb.logger.Debug("circuit breaker failure recorded",
			"component", cb.name,
			"state", cb.State(),
			"failure_count", bucket.failures.Load(),
		)
	}

	state := cb.State()

	// CLOSED state: Check if we should open the circuit
	switch state {
	case StateClosed:
		failureCount := cb.getFailureCount()
		if failureCount >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
			if cb.logger != nil {
				cb.logger.Warn("circuit breaker opened due to failure threshold",
					"component", cb.name,
					"failure_count", failureCount,
					"threshold", cb.config.FailureThreshold,
				)
			}
		}
	case StateHalfOpen:
		// HALF_OPEN state: Any failure reopens the circuit
		cb.transitionTo(StateOpen)
		if cb.logger != nil {
			cb.logger.Warn("circuit breaker reopened (failure in half-open)",
				"component", cb.name,
			)
		}
	}
}

// shouldAllowRequest checks if a request should be allowed when in OPEN state.
func (cb *CircuitBreaker) shouldAllowRequest() bool {
	now := time.Now()
	lastFailureTime := time.Unix(0, cb.lastFailure.Load())
	timeSinceFailure := now.Sub(lastFailureTime)

	// Check if enough time has passed to test recovery
	if timeSinceFailure >= cb.config.OpenTimeout {
		cb.stateMu.Lock()
		if cb.state == StateOpen {
			// Transition to HALF_OPEN for recovery testing
			cb.transitionToLocked(StateHalfOpen)
			if cb.logger != nil {
				cb.logger.Info("circuit breaker transition to half-open (timeout)",
					"component", cb.name,
					"time_in_open", timeSinceFailure,
				)
			}
		}
		cb.stateMu.Unlock()
		return true
	}

	// Log rejection at debug level
	if cb.logger != nil {
		cb.logger.Debug("circuit breaker open, request rejected",
			"component", cb.name,
			"time_since_failure", timeSinceFailure,
			"open_timeout", cb.config.OpenTimeout,
		)
	}

	return false
}

// setState updates the state and last change timestamp.
func (cb *CircuitBreaker) setState(newState State) {
	cb.state = newState
	cb.lastStateChange.Store(time.Now().UnixNano())
}

// transitionTo performs a state transition with proper locking.
func (cb *CircuitBreaker) transitionTo(newState State) {
	cb.stateMu.Lock()
	defer cb.stateMu.Unlock()
	cb.transitionToLocked(newState)
}

// transitionToLocked performs a state transition (assumes lock is held).
func (cb *CircuitBreaker) transitionToLocked(newState State) {
	// Check for no-op transition
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.setState(newState)

	// Reset counters on certain transitions
	switch newState {
	case StateHalfOpen:
		cb.halfOpenRequests.Store(0)
	case StateClosed:
		cb.halfOpenRequests.Store(0)
		// Clear all buckets when closing circuit
		for _, b := range cb.buckets {
			b.failures.Store(0)
			b.successes.Store(0)
			b.requests.Store(0)
		}
	}

	// Log transition
	if cb.logger != nil {
		cb.logger.Info("circuit breaker state transition",
			"component", cb.name,
			"from_state", oldState,
			"to_state", newState,
		)
	}

	// Record metrics
	if cb.metrics != nil {
		cb.metrics.RecordCircuitBreakerTransition(cb.name, oldState.String(), newState.String())
		cb.metrics.SetCircuitBreakerState(cb.name, newState.String())
	}
}
