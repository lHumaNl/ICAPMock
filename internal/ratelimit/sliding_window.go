// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// SlidingWindowLimiter implements rate limiting using a two-bucket sliding window approximation.
//
// Instead of storing individual request timestamps (O(n) memory and time), this implementation
// uses two atomic counters to approximate the sliding window behavior in O(1) time.
//
// Algorithm:
//   - We maintain two counters: one for the current time window, one for the previous window
//   - When calculating if a request is allowed, we compute a weighted sum:
//     weightedCount = currentCount + previousCount * overlapFactor
//   - The overlapFactor is the fraction of the previous window that overlaps with our
//     effective sliding window (1 - position within current window)
//   - When a new time window starts, we rotate: previous = current, current = 0
//
// This approximation provides rate limiting behavior very close to a true sliding window
// while maintaining O(1) time complexity for Allow() calls.
//
// Thread-safety: All operations are safe for concurrent use. Allow() uses atomic operations
// with minimal contention. Wait() and Reserve() use a mutex for accurate delay calculations.
type SlidingWindowLimiter struct {
	// Configuration (immutable after creation)
	rate     float64 // requests per second (informational)
	capacity int64   // maximum requests in window
	window   int64   // window size in nanoseconds

	// Two-bucket counter implementation
	// counters[0] = current window, counters[1] = previous window
	counters [2]atomic.Int64

	// lastTick stores the current window index (nanoseconds / window)
	// Used to detect when we need to rotate buckets
	lastTick atomic.Int64

	// Mutex for bucket rotation and Wait/Reserve operations
	mu sync.Mutex

	// pendingReservations tracks reservations that haven't been used yet
	// This allows Cancel() to "return" a slot
	pendingReservations atomic.Int64
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter with a 1-second window.
//
// Parameters:
//   - rate: The maximum number of requests per second (informational, capacity is the actual limit).
//   - capacity: The maximum number of requests allowed in the window.
//
// The window size is set to 1 second by default.
func NewSlidingWindowLimiter(rate float64, capacity int) *SlidingWindowLimiter {
	return NewSlidingWindowLimiterWithWindow(rate, capacity, time.Second)
}

// NewSlidingWindowLimiterWithWindow creates a new sliding window rate limiter
// with a custom window size.
//
// Parameters:
//   - rate: The maximum number of requests per second.
//   - capacity: The maximum number of requests allowed in the window.
//   - window: The time window duration.
func NewSlidingWindowLimiterWithWindow(rate float64, capacity int, window time.Duration) *SlidingWindowLimiter {
	l := &SlidingWindowLimiter{
		rate:     rate,
		capacity: int64(capacity),
		window:   int64(window),
	}
	// Initialize lastTick to a valid starting value
	l.lastTick.Store(time.Now().UnixNano() / l.window)
	return l
}

// Allow reports whether the next request can proceed immediately.
// This is the hot path and uses atomic operations for O(1) performance.
//
// Returns true if the request is allowed, false otherwise.
func (l *SlidingWindowLimiter) Allow() bool {
	now := time.Now().UnixNano()
	currentTick := now / l.window

	// Fast path: check if we need to rotate buckets
	// This is atomic and lock-free in the common case
	lastTick := l.lastTick.Load()

	if currentTick != lastTick {
		// Slow path: rotate buckets (requires lock)
		l.rotateBuckets(currentTick)
	}

	// Calculate weighted count using two-bucket approximation
	weightedCount := l.calculateWeightedCount(now)

	// Account for pending reservations
	effectiveCount := weightedCount + float64(l.pendingReservations.Load())

	// Check capacity
	if effectiveCount >= float64(l.capacity) {
		return false
	}

	// Increment current counter atomically
	// Note: There's a small race window here where multiple goroutines could
	// pass the check above before any increments happen. This is acceptable
	// as it allows slightly more requests than capacity in high-concurrency scenarios,
	// but it's much simpler than compare-and-swap loops.
	l.counters[0].Add(1)

	return true
}

// rotateBuckets rotates the counter buckets when a new time window starts.
// This is called when currentTick != lastTick.
func (l *SlidingWindowLimiter) rotateBuckets(currentTick int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring lock (prevent multiple rotations)
	if l.lastTick.Load() == currentTick {
		return
	}

	// Check how many ticks we've missed (handle long pauses)
	lastTick := l.lastTick.Load()
	ticksMissed := currentTick - lastTick

	if ticksMissed == 1 {
		// Normal case: single tick advancement
		// Rotate: previous = current, current = 0
		currentCount := l.counters[0].Load()
		l.counters[1].Store(currentCount)
		l.counters[0].Store(0)
	} else {
		// Multiple ticks missed (e.g., system was idle or paused)
		// Previous window data is stale, clear both
		l.counters[1].Store(0)
		l.counters[0].Store(0)
	}

	l.lastTick.Store(currentTick)
}

// calculateWeightedCount computes the approximate request count using the two-bucket algorithm.
// The formula is: currentCount + previousCount * overlapFactor
// where overlapFactor = 1 - (position within current window).
func (l *SlidingWindowLimiter) calculateWeightedCount(now int64) float64 {
	currentCount := float64(l.counters[0].Load())
	previousCount := float64(l.counters[1].Load())

	// Calculate position within current window (0 to 1)
	// This determines how much of the previous window "overlaps" with our effective window
	positionInWindow := float64(now%l.window) / float64(l.window)

	// Weighted count: current + previous * (1 - position)
	// When we're at the start of a window (position=0), we give full weight to previous
	// When we're at the end of a window (position=1), previous has no weight
	overlapFactor := 1.0 - positionInWindow

	return currentCount + previousCount*overlapFactor
}

// Wait blocks until capacity is available or the context is canceled.
// It returns nil if the request was allowed, or the context error otherwise.
func (l *SlidingWindowLimiter) Wait(ctx context.Context) error {
	for {
		// Try to acquire immediately
		if l.Allow() {
			return nil
		}

		// Calculate how long to wait
		waitTime := l.calculateWaitTime()
		if waitTime <= 0 {
			continue
		}

		// Wait for the calculated time or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Try again
		}
	}
}

// calculateWaitTime estimates how long to wait until capacity might be available.
func (l *SlidingWindowLimiter) calculateWaitTime() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UnixNano()
	currentTick := now / l.window

	// Ensure buckets are up to date
	if l.lastTick.Load() != currentTick {
		l.rotateBucketsInternal(currentTick)
	}

	currentCount := l.counters[0].Load()
	pendingCount := l.pendingReservations.Load()
	effectiveCount := currentCount + pendingCount

	// If current bucket has capacity, no wait needed
	if effectiveCount < l.capacity {
		return 0
	}

	// Calculate time remaining in current window
	windowPosition := now % l.window
	timeRemainingInWindow := l.window - windowPosition

	// We need to wait at least until the next window starts
	// (at which point the current bucket becomes previous and gets partial weight)
	return time.Duration(timeRemainingInWindow + int64(time.Microsecond))
}

// rotateBucketsInternal is the unlocked version of rotateBuckets.
// Must be called with l.mu held.
func (l *SlidingWindowLimiter) rotateBucketsInternal(currentTick int64) {
	if l.lastTick.Load() == currentTick {
		return
	}

	lastTick := l.lastTick.Load()
	ticksMissed := currentTick - lastTick

	if ticksMissed == 1 {
		currentCount := l.counters[0].Load()
		l.counters[1].Store(currentCount)
		l.counters[0].Store(0)
	} else {
		l.counters[1].Store(0)
		l.counters[0].Store(0)
	}

	l.lastTick.Store(currentTick)
}

// Reserve returns a Reservation for one token.
// For sliding window, this reserves a slot if capacity is available,
// or indicates the wait time until capacity becomes available.
func (l *SlidingWindowLimiter) Reserve() Reservation {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UnixNano()
	currentTick := now / l.window

	// Ensure buckets are up to date
	if l.lastTick.Load() != currentTick {
		l.rotateBucketsInternal(currentTick)
	}

	res := &slidingReservation{
		limiter: l,
	}

	// Calculate current effective count
	weightedCount := l.calculateWeightedCountLocked(now)
	effectiveCount := weightedCount + float64(l.pendingReservations.Load())

	if effectiveCount < float64(l.capacity) {
		// Capacity available - reserve immediately
		l.counters[0].Add(1)
		l.pendingReservations.Add(1)
		res.ok = true
		res.delay = 0
		res.reserved = true
		return res
	}

	// No capacity - calculate wait time
	windowPosition := now % l.window
	timeRemainingInWindow := l.window - windowPosition
	res.ok = true
	res.delay = time.Duration(timeRemainingInWindow + int64(time.Microsecond))
	res.reserved = false

	return res
}

// calculateWeightedCountLocked computes the weighted count.
// Must be called with l.mu held.
func (l *SlidingWindowLimiter) calculateWeightedCountLocked(now int64) float64 {
	currentCount := float64(l.counters[0].Load())
	previousCount := float64(l.counters[1].Load())
	positionInWindow := float64(now%l.window) / float64(l.window)
	overlapFactor := 1.0 - positionInWindow
	return currentCount + previousCount*overlapFactor
}

// slidingReservation represents a reservation in the sliding window limiter.
type slidingReservation struct {
	limiter  *SlidingWindowLimiter
	delay    time.Duration
	canceled atomic.Bool
	ok       bool
	reserved bool
}

// OK reports whether the reservation is valid.
func (r *slidingReservation) OK() bool {
	return r.ok
}

// Delay returns the duration to wait before the reservation can be used.
func (r *slidingReservation) Delay() time.Duration {
	return r.delay
}

// Cancel removes the reservation from the limiter.
// If the reservation was immediately reserved, it decrements the pending counter.
func (r *slidingReservation) Cancel() {
	if !r.reserved {
		return
	}

	// Only cancel once
	if r.canceled.Swap(true) {
		return
	}

	// Decrement pending reservations and the actual counter
	r.limiter.pendingReservations.Add(-1)
	r.limiter.counters[0].Add(-1)
}

// RateLimit returns the current rate limit (requests per second).
func (l *SlidingWindowLimiter) RateLimit() float64 {
	return l.rate
}

// Capacity returns the maximum number of requests in the window.
func (l *SlidingWindowLimiter) Capacity() int {
	return int(l.capacity)
}

// Window returns the window size.
func (l *SlidingWindowLimiter) Window() time.Duration {
	return time.Duration(l.window)
}

// RequestCount returns the approximate current number of requests in the window.
// This is an approximation based on the weighted count of the two-bucket algorithm.
func (l *SlidingWindowLimiter) RequestCount() int {
	now := time.Now().UnixNano()
	weightedCount := l.calculateWeightedCount(now)
	return int(weightedCount + 0.5) // Round to nearest integer
}

// SetRate updates the rate limit. The change takes effect immediately.
func (l *SlidingWindowLimiter) SetRate(rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = rate
}

// SetCapacity updates the capacity. The change takes effect immediately.
func (l *SlidingWindowLimiter) SetCapacity(capacity int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.capacity = int64(capacity)
}
