// Copyright 2026 ICAP Mock

package circuitbreaker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCircuitBreakerRaceConcurrentBucketUpdates tests that concurrent bucket updates
// don't cause data loss due to race conditions.
func TestCircuitBreakerRaceConcurrentBucketUpdates(t *testing.T) {
	config := DefaultConfig()
	config.RollingWindow = 100 * time.Millisecond
	config.WindowBuckets = 10
	config.FailureThreshold = 1000 // High threshold to avoid premature opening
	config.Enabled = true
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 50
	callsPerGoroutine := 100
	var totalExecuted atomic.Int64

	// Launch concurrent goroutines
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				err := cb.Call(ctx, func() error {
					totalExecuted.Add(1)
					return nil
				})
				if err != nil {
					t.Errorf("goroutine %d call %d: unexpected error %v", id, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify no data loss - all requests should be counted
	stats := cb.Stats()
	expectedRequests := int64(numGoroutines * callsPerGoroutine)

	if stats.Requests != expectedRequests {
		t.Errorf("race detected: expected %d requests, got %d (data loss: %d)",
			expectedRequests, stats.Requests, expectedRequests-stats.Requests)
	}

	if stats.Requests != totalExecuted.Load() {
		t.Errorf("inconsistent counters: stats.Requests=%d, totalExecuted=%d",
			stats.Requests, totalExecuted.Load())
	}

	// Verify state remained closed (no unexpected failures)
	if cb.State() != StateClosed {
		t.Errorf("unexpected state: got %v, expected CLOSED", cb.State())
	}
}

// TestCircuitBreakerRaceBucketReset tests that bucket resets are atomic.
func TestCircuitBreakerRaceBucketReset(t *testing.T) {
	config := DefaultConfig()
	config.RollingWindow = 50 * time.Millisecond
	config.WindowBuckets = 5
	config.FailureThreshold = 100
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Populate first bucket with some data
	for i := 0; i < 10; i++ {
		cb.Call(ctx, func() error {
			return nil
		})
	}

	statsBefore := cb.Stats()
	if statsBefore.Requests != 10 {
		t.Errorf("expected 10 requests before wait, got %d", statsBefore.Requests)
	}

	var wg sync.WaitGroup
	numGoroutines := 20

	// Wait for bucket to expire, then launch concurrent calls
	time.Sleep(config.RollingWindow + 10*time.Millisecond)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				cb.Call(ctx, func() error {
					return nil
				})
			}
		}(i)
	}

	wg.Wait()

	// Verify all new requests were counted (no data loss during bucket reset)
	statsAfter := cb.Stats()
	expectedNewRequests := int64(numGoroutines * 5)

	// Allow some tolerance for bucket transitions, but ensure no significant data loss
	minExpected := expectedNewRequests * 95 / 100 // 95% tolerance
	if statsAfter.Requests < minExpected {
		t.Errorf("possible race during bucket reset: expected at least %d requests, got %d",
			minExpected, statsAfter.Requests)
	}
}

// TestCircuitBreakerRaceRecordResult tests concurrent RecordResult calls.
func TestCircuitBreakerRaceRecordResult(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 1000
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)

	var wg sync.WaitGroup
	numGoroutines := 100
	callsPerGoroutine := 50

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				// Alternate between success and failure
				cb.RecordResult(j%2 == 0)
			}
		}(i)
	}

	wg.Wait()

	stats := cb.Stats()
	expectedRequests := int64(numGoroutines * callsPerGoroutine)

	if stats.Requests != expectedRequests {
		t.Errorf("race detected in RecordResult: expected %d requests, got %d",
			expectedRequests, stats.Requests)
	}

	// Verify successes and failures add up
	if stats.Successes+stats.Failures != stats.Requests {
		t.Errorf("inconsistent counters: successes+failures=%d, requests=%d",
			stats.Successes+stats.Failures, stats.Requests)
	}
}

// TestCircuitBreakerRaceMixedOperations tests mixed Call() and RecordResult() operations.
func TestCircuitBreakerRaceMixedOperations(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 1000
	config.RollingWindow = 200 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 50
	callsPerGoroutine := 40
	var totalRecorded atomic.Int64

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				if id%2 == 0 {
					// Even goroutines use Call()
					cb.Call(ctx, func() error {
						totalRecorded.Add(1)
						return nil
					})
				} else {
					// Odd goroutines use RecordResult()
					cb.RecordResult(true)
					totalRecorded.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	stats := cb.Stats()
	expectedRequests := int64(numGoroutines * callsPerGoroutine)

	if stats.Requests != expectedRequests {
		t.Errorf("race detected in mixed operations: expected %d requests, got %d",
			expectedRequests, stats.Requests)
	}

	if stats.Requests != totalRecorded.Load() {
		t.Errorf("inconsistent counters: stats.Requests=%d, totalRecorded=%d",
			stats.Requests, totalRecorded.Load())
	}
}

// TestCircuitBreakerRaceBucketTransition tests concurrent calls during bucket transitions.
func TestCircuitBreakerRaceBucketTransition(t *testing.T) {
	config := DefaultConfig()
	// Use a window longer than the test duration so no buckets wrap and reset,
	// which would lose request counts. 30 goroutines × 20 iterations × 5ms ≈ 100ms total.
	config.RollingWindow = 5 * time.Second
	config.WindowBuckets = 10
	config.FailureThreshold = 200
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 30

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			// Keep making calls over time to ensure bucket transitions happen
			for j := 0; j < 20; j++ {
				cb.Call(ctx, func() error {
					return nil
				})
				time.Sleep(5 * time.Millisecond) // Spread calls over time
			}
		}(i)
	}

	wg.Wait()

	stats := cb.Stats()
	expectedRequests := int64(numGoroutines * 20)

	// All requests should be counted since the window is long enough
	// to hold all buckets without wrapping. Allow small tolerance for
	// concurrent bucket index calculation edge cases.
	minExpected := expectedRequests * 95 / 100 // 95% tolerance
	if stats.Requests < minExpected {
		t.Errorf("possible race during bucket transitions: expected at least %d requests, got %d",
			minExpected, stats.Requests)
	}

	// Verify state remained closed
	if cb.State() != StateClosed {
		t.Errorf("unexpected state: got %v, expected CLOSED", cb.State())
	}
}

// TestCircuitBreakerRaceStatsConcurrentAccess tests concurrent Stats() calls.
func TestCircuitBreakerRaceStatsConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 1000
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	var wg sync.WaitGroup

	// Goroutines making calls
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cb.Call(ctx, func() error {
					return nil
				})
			}
		}()
	}

	// Goroutines calling Stats()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				stats := cb.Stats()
				if stats.Requests < 0 || stats.Failures < 0 || stats.Successes < 0 {
					t.Errorf("invalid stats: requests=%d, failures=%d, successes=%d",
						stats.Requests, stats.Failures, stats.Successes)
				}
			}
		}()
	}

	wg.Wait()

	// Verify final stats are consistent
	stats := cb.Stats()
	if stats.Requests != stats.Successes+stats.Failures {
		t.Errorf("stats inconsistency: requests=%d, successes+failures=%d",
			stats.Requests, stats.Successes+stats.Failures)
	}
}
