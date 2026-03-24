// Package testing provides concurrent test utilities for the ICAP Mock Server.
package testing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// RunConcurrent executes a function concurrently in n goroutines.
// It waits for all goroutines to complete before returning.
// If any goroutine panics, it will be recovered and re-paniced after all goroutines complete.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - fn: Function to execute in each goroutine (receives goroutine ID)
//
// Example:
//
//	RunConcurrent(t, 100, func(goroutineID int) {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    require.NoError(t, err)
//	})
func RunConcurrent(t *testing.T, n int, fn func(goroutineID int)) {
	t.Helper()

	if n <= 0 {
		t.Fatalf("n must be positive, got %d", n)
	}

	if fn == nil {
		t.Fatal("fn cannot be nil")
	}

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Goroutine %d panicked: %v", goroutineID, r)
				}
			}()

			fn(goroutineID)
		}(i)
	}

	wg.Wait()
}

// RunConcurrentWithTimeout executes a function concurrently in n goroutines with a timeout.
// It cancels the context if the timeout is exceeded and returns an error.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - timeout: Timeout duration for the entire operation
//   - fn: Function to execute in each goroutine (receives goroutine ID and context)
//
// Returns:
//   - error if timeout is exceeded or a goroutine panics
//
// Example:
//
//	err := RunConcurrentWithTimeout(t, 100, 5*time.Second, func(goroutineID int, ctx context.Context) {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    require.NoError(t, err)
//	})
//	require.NoError(t, err)
func RunConcurrentWithTimeout(t *testing.T, n int, timeout time.Duration, fn func(goroutineID int, ctx context.Context)) error {
	t.Helper()

	if n <= 0 {
		return fmt.Errorf("n must be positive, got %d", n)
	}

	if fn == nil {
		return fmt.Errorf("fn cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(n)

	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("goroutine %d panicked: %v", goroutineID, r)
				}
			}()

			fn(goroutineID, ctx)
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	}
}

// WaitForAll waits for all provided cancel functions to be called or the context to be canceled.
// This is useful for coordinating multiple goroutines with individual cancellation.
//
// Parameters:
//   - t: Testing instance
//   - ctx: Context for timeout
//   - goroutines: Cancel functions to wait for
//
// Example:
//
//	ctx, cancel1 := context.WithCancel(context.Background())
//	ctx2, cancel2 := context.WithCancel(context.Background())
//	defer cancel1()
//	defer cancel2()
//
//	go func() { /* ... */ cancel1() }()
//	go func() { /* ... */ cancel2() }()
//
//	WaitForAll(t, ctx, cancel1, cancel2)
func WaitForAll(t *testing.T, ctx context.Context, goroutines ...context.CancelFunc) {
	t.Helper()

	if len(goroutines) == 0 {
		return
	}

	done := make(chan struct{}, len(goroutines))

	for _, cancel := range goroutines {
		go func(c context.CancelFunc) {
			<-ctx.Done()
			c()
			done <- struct{}{}
		}(cancel)
	}

	for i := 0; i < len(goroutines); i++ {
		select {
		case <-done:
			continue
		case <-ctx.Done():
			t.Errorf("WaitForAll timed out: %v", ctx.Err())
			return
		}
	}
}

// RunConcurrentStress executes a function concurrently with configurable parameters for stress testing.
// It provides options for controlling the test behavior.
//
// Parameters:
//   - t: Testing instance
//   - cfg: Stress test configuration
//
// Example:
//
//	cfg := StressConfig{
//	    Goroutines: 100,
//	    Iterations: 10,
//	    Delay:      10 * time.Millisecond,
//	}
//	RunConcurrentStress(t, cfg, func(goroutineID, iteration int) {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    require.NoError(t, err)
//	})
type StressConfig struct {
	// Goroutines is the number of concurrent goroutines to run
	Goroutines int

	// Iterations is the number of iterations each goroutine performs
	Iterations int

	// Delay is the delay between iterations (0 for no delay)
	Delay time.Duration

	// Timeout is the total timeout for the stress test
	Timeout time.Duration
}

// RunConcurrentStress executes a stress test with the given configuration.
func RunConcurrentStress(t *testing.T, cfg StressConfig, fn func(goroutineID, iteration int)) {
	t.Helper()

	if cfg.Goroutines <= 0 {
		t.Fatalf("Goroutines must be positive, got %d", cfg.Goroutines)
	}

	if cfg.Iterations <= 0 {
		t.Fatalf("Iterations must be positive, got %d", cfg.Iterations)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(cfg.Goroutines)

	errors := make(chan error, cfg.Goroutines*cfg.Iterations)

	for i := 0; i < cfg.Goroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < cfg.Iterations; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					func() {
						defer func() {
							if r := recover(); r != nil {
								errors <- fmt.Errorf("goroutine %d iteration %d panicked: %v", goroutineID, j, r)
							}
						}()

						fn(goroutineID, j)

						if cfg.Delay > 0 {
							time.Sleep(cfg.Delay)
						}
					}()
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		select {
		case err := <-errors:
			t.Errorf("Stress test failed: %v", err)
		default:
		}
	case <-ctx.Done():
		t.Errorf("Stress test timed out: %v", ctx.Err())
	case err := <-errors:
		cancel()
		wg.Wait()
		t.Errorf("Stress test failed: %v", err)
	}
}

// ConcurrentResult represents the result of a concurrent operation.
type ConcurrentResult struct {
	// GoroutineID is the ID of the goroutine that produced this result
	GoroutineID int

	// Iteration is the iteration number (for multi-iteration tests)
	Iteration int

	// Error is any error that occurred during the operation
	Error error

	// Duration is the time taken for the operation
	Duration time.Duration
}

// RunConcurrentWithResults executes a function concurrently and collects results.
// This is useful for benchmarking or collecting statistics from concurrent operations.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - iterations: Number of iterations per goroutine
//   - fn: Function to execute (receives goroutine ID and iteration, returns error)
//
// Returns:
//   - slice of results from all goroutines
//
// Example:
//
//	results := RunConcurrentWithResults(t, 100, 10, func(goroutineID, iteration int) error {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    return err
//	})
//
//	for _, result := range results {
//	    if result.Error != nil {
//	        t.Errorf("Goroutine %d iteration %d failed: %v", result.GoroutineID, result.Iteration, result.Error)
//	    }
//	}
func RunConcurrentWithResults(t *testing.T, n, iterations int, fn func(goroutineID, iteration int) error) []ConcurrentResult {
	t.Helper()

	if n <= 0 {
		t.Fatalf("n must be positive, got %d", n)
	}

	if iterations <= 0 {
		t.Fatalf("iterations must be positive, got %d", iterations)
	}

	if fn == nil {
		t.Fatal("fn cannot be nil")
	}

	results := make([]ConcurrentResult, 0, n*iterations)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				start := time.Now()
				err := fn(goroutineID, j)
				duration := time.Since(start)

				mu.Lock()
				results = append(results, ConcurrentResult{
					GoroutineID: goroutineID,
					Iteration:   j,
					Error:       err,
					Duration:    duration,
				})
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return results
}

// AssertNoConcurrentErrors asserts that no errors occurred in concurrent results.
//
// Parameters:
//   - t: Testing instance
//   - results: Results from RunConcurrentWithResults
//
// Example:
//
//	results := RunConcurrentWithResults(t, 100, 10, fn)
//	AssertNoConcurrentErrors(t, results)
func AssertNoConcurrentErrors(t *testing.T, results []ConcurrentResult) {
	t.Helper()

	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Goroutine %d iteration %d failed: %v", result.GoroutineID, result.Iteration, result.Error)
		}
	}
}

// GetConcurrentStats calculates statistics from concurrent results.
//
// Parameters:
//   - results: Results from RunConcurrentWithResults
//
// Returns:
//   - errorCount: Number of errors
//   - avgDuration: Average duration of successful operations
//   - maxDuration: Maximum duration of all operations
//   - minDuration: Minimum duration of successful operations
//
// Example:
//
//	results := RunConcurrentWithResults(t, 100, 10, fn)
//	errCount, avgDur, maxDur, minDur := GetConcurrentStats(results)
//	t.Logf("Errors: %d, Avg: %v, Max: %v, Min: %v", errCount, avgDur, maxDur, minDur)
func GetConcurrentStats(results []ConcurrentResult) (errorCount int, avgDuration, maxDuration, minDuration time.Duration) {
	var totalDuration time.Duration
	var successCount int

	maxDuration = 0
	minDuration = time.Duration(1<<63 - 1)

	for _, result := range results {
		if result.Error != nil {
			errorCount++
			continue
		}

		totalDuration += result.Duration
		successCount++

		if result.Duration > maxDuration {
			maxDuration = result.Duration
		}

		if result.Duration < minDuration {
			minDuration = result.Duration
		}
	}

	if successCount > 0 {
		avgDuration = totalDuration / time.Duration(successCount)
	}

	if errorCount == len(results) {
		minDuration = 0
	}

	return errorCount, avgDuration, maxDuration, minDuration
}

// RunConcurrentBurst executes a burst of concurrent operations without delay.
// This is useful for testing how the system handles sudden load spikes.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - fn: Function to execute in each goroutine
//
// Example:
//
//	RunConcurrentBurst(t, 1000, func() {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    require.NoError(t, err)
//	})
func RunConcurrentBurst(t *testing.T, n int, fn func()) {
	t.Helper()

	if n <= 0 {
		t.Fatalf("n must be positive, got %d", n)
	}

	if fn == nil {
		t.Fatal("fn cannot be nil")
	}

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Burst goroutine panicked: %v", r)
				}
			}()

			fn()
		}()
	}

	wg.Wait()
}

// RunConcurrentWithBackoff executes a function concurrently with exponential backoff on errors.
// This is useful for testing retry logic and resilience.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - maxRetries: Maximum number of retries per goroutine
//   - initialDelay: Initial delay before first retry
//   - fn: Function to execute (returns error)
//
// Example:
//
//	RunConcurrentWithBackoff(t, 100, 3, 100*time.Millisecond, func() error {
//	    req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	    resp, err := harness.SendRequest(req)
//	    return err
//	})
func RunConcurrentWithBackoff(t *testing.T, n, maxRetries int, initialDelay time.Duration, fn func() error) {
	t.Helper()

	if n <= 0 {
		t.Fatalf("n must be positive, got %d", n)
	}

	if maxRetries < 0 {
		t.Fatalf("maxRetries must be non-negative, got %d", maxRetries)
	}

	if fn == nil {
		t.Fatal("fn cannot be nil")
	}

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			var err error
			delay := initialDelay

			for attempt := 0; attempt <= maxRetries; attempt++ {
				err = fn()
				if err == nil {
					return
				}

				if attempt < maxRetries {
					time.Sleep(delay)
					delay *= 2
				}
			}

			t.Errorf("Goroutine %d failed after %d retries: %v", goroutineID, maxRetries, err)
		}(i)
	}

	wg.Wait()
}

// RunConcurrentForDuration executes a function concurrently for a specified duration.
// This is useful for testing system stability over time.
//
// Parameters:
//   - t: Testing instance
//   - n: Number of goroutines to run
//   - duration: Duration to run the test
//   - fn: Function to execute in each goroutine
//
// Example:
//
//	RunConcurrentForDuration(t, 50, 10*time.Second, func(stopCh <-chan struct{}) {
//	    for {
//	        select {
//	        case <-stopCh:
//	            return
//	        default:
//	            req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	            harness.SendRequest(req)
//	        }
//	    }
//	})
func RunConcurrentForDuration(t *testing.T, n int, duration time.Duration, fn func(stopCh <-chan struct{})) {
	t.Helper()

	if n <= 0 {
		t.Fatalf("n must be positive, got %d", n)
	}

	if fn == nil {
		t.Fatal("fn cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Goroutine %d panicked: %v", goroutineID, r)
				}
			}()

			fn(ctx.Done())
		}(i)
	}

	wg.Wait()
}
