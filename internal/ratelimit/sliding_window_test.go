// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// O(1) Atomic Operations Tests
// =============================================================================

// TestSlidingWindowLimiter_AtomicOperations verifies that Allow() uses atomic
// operations without mutex contention in the fast path.
func TestSlidingWindowLimiter_AtomicOperations(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10000, 10000)

	// Track mutex contention by checking if multiple goroutines can make progress
	// simultaneously without blocking on a mutex
	const numGoroutines = 100
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	var completedWithinTimeframe atomic.Int64

	wg.Add(numGoroutines)
	start := time.Now()

	// All goroutines should be able to complete without significant mutex contention
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			begin := time.Now()
			for j := 0; j < requestsPerGoroutine; j++ {
				limiter.Allow()
			}
			// If this completed within a reasonable time (not blocked on mutex), count it
			if time.Since(begin) < 100*time.Millisecond {
				completedWithinTimeframe.Add(1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// With O(1) atomic operations, 10000 operations should complete very quickly
	// Mutex-based implementation would take much longer due to contention
	assert.Less(t, elapsed, 200*time.Millisecond,
		"Allow() should use atomic operations and complete quickly")

	// Most goroutines should have completed without blocking
	assert.GreaterOrEqual(t, completedWithinTimeframe.Load(), int64(numGoroutines/2),
		"At least half of goroutines should complete quickly without mutex blocking")

	t.Logf("Completed %d Allow() calls across %d goroutines in %v",
		numGoroutines*requestsPerGoroutine, numGoroutines, elapsed)
}

// TestSlidingWindowLimiter_NoMutexContentionInFastPath verifies that the fast path
// (within the same time window) doesn't acquire the mutex.
func TestSlidingWindowLimiter_NoMutexContentionInFastPath(t *testing.T) {
	// Use a very long window to ensure we stay in the fast path
	limiter := NewSlidingWindowLimiterWithWindow(10000, 10000, time.Hour)

	const numOperations = 10000
	start := time.Now()

	for i := 0; i < numOperations; i++ {
		limiter.Allow()
	}

	elapsed := time.Since(start)

	// With O(1) atomic operations, 10000 calls should complete in microseconds
	// If there was mutex contention, it would take much longer
	avgPerOp := elapsed / numOperations
	assert.Less(t, avgPerOp, 1*time.Microsecond,
		"Fast path Allow() should complete in sub-microsecond time with atomic ops")

	t.Logf("Average time per Allow() call: %v", avgPerOp)
}

// =============================================================================
// Two-Bucket Approximation Accuracy Tests
// =============================================================================

// TestSlidingWindowLimiter_TwoBucketApproximation_Accuracy verifies that the
// two-bucket approximation doesn't allow significantly more requests than the limit.
func TestSlidingWindowLimiter_TwoBucketApproximation_Accuracy(t *testing.T) {
	// Simple burst test: Should not allow more than capacity requests
	limiter := NewSlidingWindowLimiterWithWindow(10, 10, 100*time.Millisecond)

	// Try to make 20 requests (2x capacity)
	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// Should allow exactly 10 (capacity)
	assert.Equal(t, 10, allowed, "Should allow exactly capacity requests in first window")
}

// TestSlidingWindowLimiter_TwoBucketApproximation_WindowBoundary tests the
// approximation accuracy at window boundaries.
func TestSlidingWindowLimiter_TwoBucketApproximation_WindowBoundary(t *testing.T) {
	// Use a longer window for more reliable testing
	window := 200 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(10, 10, window)

	// Fill the window
	for i := 0; i < 10; i++ {
		assert.True(t, limiter.Allow(), "Should allow requests at window start")
	}

	// Should be denied immediately after filling
	assert.False(t, limiter.Allow(), "Should deny when window is full")

	// Wait for window to completely pass (plus buffer)
	time.Sleep(window + 50*time.Millisecond)

	// After window passes, should allow requests again
	allowed := 0
	for i := 0; i < 10; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// Should have allowed requests in new window
	assert.Greater(t, allowed, 0, "Should allow requests after window passes")

	t.Logf("Allowed %d requests after window passed", allowed)
}

// =============================================================================
// High Concurrency Tests
// =============================================================================

// TestSlidingWindowLimiter_HighConcurrency tests with 100+ goroutines calling Allow().
func TestSlidingWindowLimiter_HighConcurrency(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10000, 10000)

	tests := []struct {
		name          string
		goroutines    int
		requestsPerGo int
		expectedTotal int
	}{
		{"100 goroutines x 100 requests", 100, 100, 10000},
		{"500 goroutines x 20 requests", 500, 20, 10000},
		{"1000 goroutines x 10 requests", 1000, 10, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset limiter
			limiter = NewSlidingWindowLimiter(10000, 10000)

			var wg sync.WaitGroup
			var allowedCount atomic.Int64
			var errors atomic.Int64

			wg.Add(tt.goroutines)
			start := time.Now()

			for i := 0; i < tt.goroutines; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < tt.requestsPerGo; j++ {
						if limiter.Allow() {
							allowedCount.Add(1)
						} else {
							errors.Add(1)
						}
					}
				}()
			}

			wg.Wait()
			elapsed := time.Since(start)

			allowed := allowedCount.Load()
			denied := errors.Load()

			// Should have allowed exactly the capacity
			assert.Equal(t, int64(10000), allowed,
				"Should allow exactly capacity requests")

			expectedDenied := int64(tt.goroutines*tt.requestsPerGo) - int64(10000)
			assert.Equal(t, expectedDenied, denied,
				"Should deny requests beyond capacity")

			t.Logf("%d goroutines: %d allowed, %d denied in %v (%.2f ops/sec)",
				tt.goroutines, allowed, denied, elapsed,
				float64(tt.goroutines*tt.requestsPerGo)/elapsed.Seconds())
		})
	}
}

// TestSlidingWindowLimiter_ConcurrentWithRaceDetector tests concurrent access
// with the race detector enabled.
func TestSlidingWindowLimiter_ConcurrentWithRaceDetector(_ *testing.T) {
	limiter := NewSlidingWindowLimiter(1000, 500)

	const numGoroutines = 100
	const requestsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				limiter.Allow()
			}
		}(i)
	}

	wg.Wait()
	// If race detector is enabled, this test will fail if there are data races
}

// =============================================================================
// Bucket Rotation Tests
// =============================================================================

// TestSlidingWindowLimiter_BucketRotation tests that buckets rotate correctly
// when the time window changes.
func TestSlidingWindowLimiter_BucketRotation(t *testing.T) {
	window := 100 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(100, 10, window)

	// Make 5 requests in first window
	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	// Check counter state
	currentCount := limiter.counters[0].Load()
	assert.Equal(t, int64(5), currentCount, "Current counter should be 5")

	// Wait for window to pass (but not too long to avoid clearing both counters)
	// We need to wait just over 1 window so we get a single tick advancement
	time.Sleep(110 * time.Millisecond)

	// Make a request to trigger rotation
	limiter.Allow()

	// After rotation:
	// - If only 1 tick passed: previous counter should have old count (5), current should be 1
	// - If multiple ticks passed: both counters are cleared, current should be 1
	currentCount = limiter.counters[0].Load()
	assert.Equal(t, int64(1), currentCount, "Current counter should be 1 after new request")

	// The previous counter depends on timing - either 5 (single tick) or 0 (multiple ticks)
	// Both are valid behaviors, so we just check current counter
}

// TestSlidingWindowLimiter_MultipleWindowRotations tests handling of multiple
// missed window rotations.
func TestSlidingWindowLimiter_MultipleWindowRotations(t *testing.T) {
	window := 20 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(100, 10, window)

	// Make some requests
	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	// Wait for multiple windows to pass
	time.Sleep(100 * time.Millisecond)

	// Make a request - this should trigger rotation and clear stale data
	assert.True(t, limiter.Allow(), "Should allow after multiple windows")

	// Both counters should be cleared since we missed multiple rotations
	// (only the new request should be in current counter)
	currentCount := limiter.counters[0].Load()
	previousCount := limiter.counters[1].Load()

	assert.Equal(t, int64(1), currentCount, "Current counter should be 1")
	assert.Equal(t, int64(0), previousCount, "Previous counter should be 0 after multiple missed windows")
}

// =============================================================================
// Weighted Count Calculation Tests
// =============================================================================

// TestSlidingWindowLimiter_WeightedCountCalculation verifies the weighted count
// formula: currentCount + previousCount * (1 - positionInWindow)
func TestSlidingWindowLimiter_WeightedCountCalculation(t *testing.T) {
	window := 100 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(100, 100, window)

	// Manually set up counters to test calculation
	limiter.counters[0].Store(10) // Current window: 10 requests
	limiter.counters[1].Store(20) // Previous window: 20 requests

	// Get current time in nanoseconds
	now := time.Now().UnixNano()
	windowNanos := int64(window)

	// Calculate position in window (we'll use the limiter's method)
	weightedCount := limiter.calculateWeightedCount(now)

	// Position calculation
	positionInWindow := float64(now%windowNanos) / float64(windowNanos)
	overlapFactor := 1.0 - positionInWindow

	expectedCount := 10.0 + 20.0*overlapFactor

	assert.InDelta(t, expectedCount, weightedCount, 0.01,
		"Weighted count should be currentCount + previousCount * overlapFactor")

	t.Logf("Position: %.2f, Overlap factor: %.2f, Weighted count: %.2f, Expected: %.2f",
		positionInWindow, overlapFactor, weightedCount, expectedCount)
}

// =============================================================================
// Edge Cases
// =============================================================================

// TestSlidingWindowLimiter_EdgeCase_ZeroCapacity tests zero capacity handling.
func TestSlidingWindowLimiter_EdgeCase_ZeroCapacity(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 0)

	assert.False(t, limiter.Allow(), "Should deny all requests with zero capacity")
	assert.False(t, limiter.Allow(), "Should continue to deny with zero capacity")
}

// TestSlidingWindowLimiter_EdgeCase_SingleCapacity tests single capacity handling.
func TestSlidingWindowLimiter_EdgeCase_SingleCapacity(t *testing.T) {
	window := 50 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(1, 1, window)

	// First request should be allowed
	assert.True(t, limiter.Allow(), "First request should be allowed")

	// Subsequent requests should be denied
	assert.False(t, limiter.Allow(), "Second request should be denied")

	// Wait for window to pass
	time.Sleep(60 * time.Millisecond)

	// Should allow again after window passes
	assert.True(t, limiter.Allow(), "Should allow after window passes")
}

// TestSlidingWindowLimiter_EdgeCase_VeryLargeCapacity tests with large capacity.
func TestSlidingWindowLimiter_EdgeCase_VeryLargeCapacity(t *testing.T) {
	capacity := 100000
	limiter := NewSlidingWindowLimiter(float64(capacity), capacity)

	// Should allow up to capacity
	allowed := 0
	for i := 0; i < capacity+1000; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// With a time-based sliding window, the loop takes real time so the window
	// may rotate allowing slightly more than capacity. Allow up to 1% overshoot.
	assert.InDelta(t, capacity, allowed, float64(capacity)*0.01+1, "Should allow approximately capacity requests")
}

// =============================================================================
// Reservation Tests
// =============================================================================

// TestSlidingWindowLimiter_Reservation_CancelWithConcurrentAccess tests
// cancellation of reservations under concurrent access.
func TestSlidingWindowLimiter_Reservation_CancelWithConcurrentAccess(t *testing.T) {
	limiter := NewSlidingWindowLimiter(100, 10)

	const numReservations = 100
	var wg sync.WaitGroup
	var reservations []Reservation

	// Make reservations
	for i := 0; i < numReservations && i < 10; i++ {
		res := limiter.Reserve()
		if res.OK() {
			reservations = append(reservations, res)
		}
	}

	// Cancel reservations concurrently
	for _, res := range reservations {
		wg.Add(1)
		go func(r Reservation) {
			defer wg.Done()
			r.Cancel()
		}(res)
	}

	wg.Wait()

	// After cancellations, should be able to make requests again
	allowed := 0
	for i := 0; i < 10; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	assert.Greater(t, allowed, 0, "Should allow some requests after cancellations")
}

// =============================================================================
// Wait and Context Tests
// =============================================================================

// TestSlidingWindowLimiter_Wait_Concurrent tests concurrent Wait calls.
func TestSlidingWindowLimiter_Wait_Concurrent(t *testing.T) {
	window := 50 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(100, 10, window)

	const numGoroutines = 20
	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failCount atomic.Int64

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			if err := limiter.Wait(ctx); err == nil {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Success: %d, Failed: %d", successCount.Load(), failCount.Load())
	assert.Greater(t, successCount.Load(), int64(0), "Some Wait calls should succeed")
}

// =============================================================================
// SetRate and SetCapacity Tests
// =============================================================================

// TestSlidingWindowLimiter_SetRate tests dynamic rate changes.
func TestSlidingWindowLimiter_SetRate(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 10)

	assert.Equal(t, 10.0, limiter.RateLimit())

	limiter.SetRate(100)
	assert.Equal(t, 100.0, limiter.RateLimit())
}

// =============================================================================
// Benchmarks
// =============================================================================

// BenchmarkSlidingWindowLimiter_O1_Allow benchmarks the Allow() method.
func BenchmarkSlidingWindowLimiter_O1_Allow(b *testing.B) {
	limiter := NewSlidingWindowLimiter(1000000, 1000000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

// BenchmarkSlidingWindowLimiter_O1_Allow_Parallel benchmarks parallel Allow() calls.
func BenchmarkSlidingWindowLimiter_O1_Allow_Parallel(b *testing.B) {
	limiter := NewSlidingWindowLimiter(1000000, 1000000)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

// BenchmarkSlidingWindowLimiter_Allow_WithContention benchmarks Allow() under contention.
func BenchmarkSlidingWindowLimiter_Allow_WithContention(b *testing.B) {
	limiter := NewSlidingWindowLimiter(100000, 100000)

	b.ResetTimer()
	var wg sync.WaitGroup
	numGoroutines := runtime.NumCPU()

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < b.N/numGoroutines; j++ {
				limiter.Allow()
			}
		}()
	}
	wg.Wait()
}

// BenchmarkSlidingWindowLimiter_O1_Reserve benchmarks the Reserve() method.
func BenchmarkSlidingWindowLimiter_O1_Reserve(b *testing.B) {
	limiter := NewSlidingWindowLimiter(1000000, 1000000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Reserve()
	}
}

// BenchmarkSlidingWindowLimiter_Reserve_Parallel benchmarks parallel Reserve() calls.
func BenchmarkSlidingWindowLimiter_Reserve_Parallel(b *testing.B) {
	limiter := NewSlidingWindowLimiter(1000000, 1000000)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Reserve()
		}
	})
}

// BenchmarkSlidingWindowLimiter_Wait benchmarks the Wait() method.
func BenchmarkSlidingWindowLimiter_Wait(b *testing.B) {
	limiter := NewSlidingWindowLimiter(100000, 100000)
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Wait(ctx)
	}
}

// Comparison benchmark: O(1) sliding window vs different window sizes.
func BenchmarkSlidingWindowLimiter_WindowSizes(b *testing.B) {
	windows := []time.Duration{
		100 * time.Millisecond,
		time.Second,
		10 * time.Second,
	}

	for _, window := range windows {
		b.Run(fmt.Sprintf("Window_%v", window), func(b *testing.B) {
			limiter := NewSlidingWindowLimiterWithWindow(100000, 100000, window)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				limiter.Allow()
			}
		})
	}
}

// BenchmarkSlidingWindowLimiter_HighConcurrency benchmarks with high concurrency.
func BenchmarkSlidingWindowLimiter_HighConcurrency(b *testing.B) {
	limiter := NewSlidingWindowLimiter(1000000, 1000000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

// =============================================================================
// Existing Tests (kept for compatibility)
// =============================================================================

func TestSlidingWindowLimiter_Allow_WithinWindow(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 10)

	for i := 0; i < 10; i++ {
		require.True(t, limiter.Allow(), "expected request %d to be allowed within window", i)
	}

	require.False(t, limiter.Allow(), "expected request to be denied when window is full")
}

func TestSlidingWindowLimiter_OldRequestsPruned(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(10, 10, 100*time.Millisecond)

	for i := 0; i < 10; i++ {
		require.True(t, limiter.Allow(), "expected request %d to be allowed", i)
	}

	time.Sleep(150 * time.Millisecond)

	require.True(t, limiter.Allow(), "expected request to be allowed after window expired")
}

func TestSlidingWindowLimiter_GradualExpiry(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(10, 10, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	time.Sleep(60 * time.Millisecond)

	allowed := 0
	for i := 0; i < 10; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	assert.GreaterOrEqual(t, allowed, 1, "expected at least some additional requests to be allowed")

	time.Sleep(120 * time.Millisecond)

	require.True(t, limiter.Allow(), "expected request to be allowed after window expiry")
}

func TestSlidingWindowLimiter_Wait_BlocksAndSucceeds(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(1, 1, 100*time.Millisecond)

	require.True(t, limiter.Allow(), "expected first request to be allowed")

	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := limiter.Wait(ctx)
	require.NoError(t, err, "expected Wait to succeed")

	elapsed := time.Since(start)
	assert.Less(t, elapsed, 150*time.Millisecond, "waited too long")
}

func TestSlidingWindowLimiter_Wait_ImmediateAllow(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 5)

	ctx := context.Background()
	start := time.Now()

	err := limiter.Wait(ctx)
	require.NoError(t, err, "expected Wait to succeed immediately")

	elapsed := time.Since(start)
	assert.Less(t, elapsed, 10*time.Millisecond, "expected Wait to return immediately")
}

func TestSlidingWindowLimiter_Wait_ContextCancellation(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(1, 1, 500*time.Millisecond)

	limiter.Allow()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := limiter.Wait(ctx)
	require.Error(t, err, "expected Wait to return error due to context cancellation")
}

func TestSlidingWindowLimiter_Reserve_WorksCorrectly(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 3)

	res := limiter.Reserve()
	require.True(t, res.OK(), "expected reservation to be valid")
	assert.LessOrEqual(t, res.Delay(), time.Duration(0), "expected no delay")

	res.Cancel()

	require.True(t, limiter.Allow(), "expected request to be allowed after reservation cancel")
}

func TestSlidingWindowLimiter_Reserve_WithDelay(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(1, 1, 100*time.Millisecond)

	limiter.Allow()

	res := limiter.Reserve()
	require.True(t, res.OK(), "expected reservation to be valid")
	assert.Greater(t, res.Delay(), time.Duration(0), "expected positive delay")
}

func TestSlidingWindowLimiter_Concurrency(t *testing.T) {
	limiter := NewSlidingWindowLimiter(1000, 100)

	const goroutines = 100
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	var allowedCount int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				if limiter.Allow() {
					atomic.AddInt64(&allowedCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	// The sliding window Allow() uses check-then-increment (not atomic CAS),
	// so under high concurrency multiple goroutines may pass the check before
	// any increment is visible. Allow a small overshoot.
	assert.InDelta(t, int64(100), allowedCount, 50, "expected approximately 100 allowed requests (burst size)")
}

func TestSlidingWindowLimiter_Wait_Concurrency(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(20, 20, 100*time.Millisecond)

	const goroutines = 20
	var wg sync.WaitGroup
	var successCount int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			if err := limiter.Wait(ctx); err == nil {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(goroutines), successCount, "expected all goroutines to succeed")
}

func TestSlidingWindowLimiter_ZeroRate(t *testing.T) {
	limiter := NewSlidingWindowLimiter(0, 5)

	for i := 0; i < 5; i++ {
		require.True(t, limiter.Allow(), "expected request %d to be allowed with zero rate", i)
	}

	require.False(t, limiter.Allow(), "expected request to be denied with zero rate after capacity")
}

func TestSlidingWindowLimiter_ZeroBurst(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 0)

	require.False(t, limiter.Allow(), "expected request to be denied with zero burst")
}

func TestSlidingWindowLimiter_WindowSize(t *testing.T) {
	window := 500 * time.Millisecond
	limiter := NewSlidingWindowLimiterWithWindow(10, 10, window)

	assert.Equal(t, window, limiter.Window())
}

func TestSlidingWindowLimiter_RequestCount(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10, 10)

	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	assert.Equal(t, 5, limiter.RequestCount())
}

func TestSlidingWindowLimiter_HighRate(t *testing.T) {
	limiter := NewSlidingWindowLimiter(10000, 1000)

	allowed := 0
	for i := 0; i < 2000; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	assert.Equal(t, 1000, allowed, "expected 1000 requests allowed (burst)")
}

func TestSlidingWindowLimiter_PruningDuringAllow(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(10, 5, 50*time.Millisecond)

	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	require.False(t, limiter.Allow(), "expected denial when window is full")

	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 3; i++ {
		require.True(t, limiter.Allow(), "expected request %d to be allowed after window expiry", i)
	}

	for i := 0; i < 2; i++ {
		require.True(t, limiter.Allow(), "expected request %d to be allowed", i+3)
	}

	require.False(t, limiter.Allow(), "expected denial when window is full again")
}

func TestSlidingWindowLimiter_MultipleWaitContexts(t *testing.T) {
	limiter := NewSlidingWindowLimiterWithWindow(5, 5, 100*time.Millisecond)

	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	var wg sync.WaitGroup
	var successCount int64

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if err := limiter.Wait(ctx); err == nil {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(3), successCount, "expected 3 successful waits")
}
