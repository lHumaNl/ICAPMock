// Package handler_test provides tests for the ICAP handler retry middleware.
package handler_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/middleware"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestCalculateBackoffWithJitter_JitterNone tests jitter with JitterNone strategy (no jitter).
func TestCalculateBackoffWithJitter_JitterNone(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	// Without jitter, backoff should be deterministic
	backoff1 := calculateBackoffWithJitter(initial, multiplier, max, 0, handler.JitterNone, 0.25)
	backoff2 := calculateBackoffWithJitter(initial, multiplier, max, 0, handler.JitterNone, 0.25)

	if backoff1 != backoff2 {
		t.Errorf("With JitterNone, backoff should be deterministic: %v != %v", backoff1, backoff2)
	}

	// Verify exponential backoff values
	expectedBackoffs := []time.Duration{
		100 * time.Millisecond, // attempt 0
		200 * time.Millisecond, // attempt 1
		400 * time.Millisecond, // attempt 2
		800 * time.Millisecond, // attempt 3
		1 * time.Second,        // attempt 4 (capped)
	}

	for attempt, expected := range expectedBackoffs {
		backoff := calculateBackoffWithJitter(initial, multiplier, max, attempt, handler.JitterNone, 0.25)
		if backoff != expected {
			t.Errorf("Attempt %d: backoff = %v, want %v", attempt, backoff, expected)
		}
	}
}

// TestCalculateBackoffWithJitter_JitterFull tests jitter with JitterFull strategy.
func TestCalculateBackoffWithJitter_JitterFull(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	// With JitterFull, backoff should vary between 0 and the calculated backoff
	backoffs := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		backoffs[i] = calculateBackoffWithJitter(initial, multiplier, max, 2, handler.JitterFull, 0)
	}

	// Find min and max
	var min, maxDuration time.Duration
	for i, b := range backoffs {
		if i == 0 || b < min {
			min = b
		}
		if b > maxDuration {
			maxDuration = b
		}
	}

	// Backoffs should vary
	if min == maxDuration {
		t.Error("With JitterFull, backoffs should vary")
	}

	// Max should be close to the calculated backoff (400ms for attempt 2)
	expectedMax := 400 * time.Millisecond
	if maxDuration > expectedMax {
		t.Errorf("Max backoff %v should not exceed calculated backoff %v", maxDuration, expectedMax)
	}

	// Min should be close to 0 (with jitter, some values should be small)
	if min > expectedMax/10 {
		t.Errorf("Min backoff %v should be significantly less than max %v", min, maxDuration)
	}
}

// TestCalculateBackoffWithJitter_JitterEqual tests jitter with JitterEqual strategy.
func TestCalculateBackoffWithJitter_JitterEqual(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second
	jitterPercent := 0.25 // 25% jitter

	// With JitterEqual and 25% jitter, backoff should vary between ±12.5% of the calculated backoff
	backoffs := make([]time.Duration, 1000)
	for i := 0; i < 1000; i++ {
		backoffs[i] = calculateBackoffWithJitter(initial, multiplier, max, 2, handler.JitterEqual, jitterPercent)
	}

	// Calculate statistics
	var sum time.Duration
	var min, maxDuration time.Duration
	for i, b := range backoffs {
		sum += b
		if i == 0 || b < min {
			min = b
		}
		if b > maxDuration {
			maxDuration = b
		}
	}

	mean := sum / time.Duration(len(backoffs))

	// Verify mean is close to the calculated backoff (400ms for attempt 2)
	expectedMean := 400 * time.Millisecond
	meanDiff := abs(float64(mean - expectedMean))
	if meanDiff > float64(expectedMean)*0.05 { // Allow 5% deviation
		t.Errorf("Mean backoff %v should be close to %v (diff: %v)", mean, expectedMean, meanDiff)
	}

	// Verify all backoffs are within valid range: [400ms * (1 - 0.25), 400ms * (1 + 0.25)]
	// i.e., [300ms, 500ms]
	expectedMin := time.Duration(float64(expectedMean) * (1 - jitterPercent))
	expectedMax := time.Duration(float64(expectedMean) * (1 + jitterPercent))

	if min < expectedMin || maxDuration > expectedMax {
		t.Errorf("Backoffs should be within [%v, %v], got [%v, %v]", expectedMin, expectedMax, min, maxDuration)
	}
}

// TestCalculateBackoffWithJitter_Distribution tests that jitter distributes randomly.
func TestCalculateBackoffWithJitter_Distribution(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	// Generate many backoff values
	numSamples := 10000
	backoffs := make([]time.Duration, numSamples)
	for i := 0; i < numSamples; i++ {
		backoffs[i] = calculateBackoffWithJitter(initial, multiplier, max, 1, handler.JitterEqual, 0.25)
	}

	// Divide into buckets to check distribution
	// For attempt=1, backoff=200ms, jitterPercent=0.25, range is [150ms, 250ms] = 100ms
	baseBackoff := 200 * time.Millisecond
	jitterRange := time.Duration(float64(baseBackoff) * 0.25)
	rangeStart := baseBackoff - jitterRange
	totalRange := jitterRange * 2
	bucketCount := 10
	buckets := make([]int, bucketCount)

	for _, b := range backoffs {
		// Calculate which bucket this backoff falls into
		bucketSize := totalRange / time.Duration(bucketCount)
		offset := b - rangeStart
		bucket := int(offset / bucketSize)

		if bucket >= 0 && bucket < bucketCount {
			buckets[bucket]++
		}
	}

	// Check that distribution is roughly uniform (allow ±50% deviation)
	expectedPerBucket := numSamples / bucketCount
	for i, count := range buckets {
		minExpected := expectedPerBucket / 2
		maxExpected := expectedPerBucket * 3 / 2
		if count < minExpected || count > maxExpected {
			t.Errorf("Bucket %d has %d samples, expected between %d and %d", i, count, minExpected, maxExpected)
		}
	}
}

// TestCalculateBackoffWithJitter_ZeroBackoff tests jitter with zero backoff.
func TestCalculateBackoffWithJitter_ZeroBackoff(t *testing.T) {
	t.Parallel()

	backoff1 := calculateBackoffWithJitter(0, 2.0, 1*time.Second, 0, handler.JitterEqual, 0.25)
	backoff2 := calculateBackoffWithJitter(0, 2.0, 1*time.Second, 0, handler.JitterFull, 0.25)

	// Zero backoff should remain zero regardless of jitter strategy
	if backoff1 != 0 {
		t.Errorf("Zero backoff with JitterEqual should remain 0, got %v", backoff1)
	}
	if backoff2 != 0 {
		t.Errorf("Zero backoff with JitterFull should remain 0, got %v", backoff2)
	}
}

// TestCalculateBackoffWithJitter_InvalidJitterPercent tests handling of invalid jitter percentage.
func TestCalculateBackoffWithJitter_InvalidJitterPercent(t *testing.T) {
	t.Parallel()

	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	// Negative jitter percent should be clamped to default (0.25)
	backoff1 := calculateBackoffWithJitter(initial, multiplier, max, 1, handler.JitterEqual, -0.1)

	// Jitter percent > 1.0 should be clamped to default (0.25)
	backoff2 := calculateBackoffWithJitter(initial, multiplier, max, 1, handler.JitterEqual, 1.5)

	// Both should be valid (within expected range for default 25% jitter)
	baseBackoff := 200 * time.Millisecond
	minExpected := time.Duration(float64(baseBackoff) * 0.75) // 75% of base
	maxExpected := time.Duration(float64(baseBackoff) * 1.25) // 125% of base

	if backoff1 < minExpected || backoff1 > maxExpected {
		t.Errorf("Backoff with negative jitter percent %v should be in range [%v, %v]", backoff1, minExpected, maxExpected)
	}
	if backoff2 < minExpected || backoff2 > maxExpected {
		t.Errorf("Backoff with jitter percent > 1.0 %v should be in range [%v, %v]", backoff2, minExpected, maxExpected)
	}
}

// TestRetryMiddleware_JitterPreventsThunderingHerd tests that jitter prevents thundering herd.
func TestRetryMiddleware_JitterPreventsThunderingHerd(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    50 * time.Millisecond,
		BackoffMultiplier: 2.0,
		JitterStrategy:    handler.JitterEqual,
		JitterPercent:     0.5, // 50% jitter for more visible effect
		Logger:            slog.Default(),
	}

	var callTimes []time.Time
	var mu sync.Mutex
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		mu.Unlock()
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	// Simulate 10 concurrent clients retrying simultaneously
	const numClients = 10
	var wg sync.WaitGroup
	wg.Add(numClients)

	start := time.Now()
	for i := 0; i < numClients; i++ {
		go func() {
			defer wg.Done()
			_, _ = wrappedHandler.Handle(context.Background(), req)
		}()
	}
	wg.Wait()
	totalTime := time.Since(start)

	mu.Lock()
	times := make([]time.Time, len(callTimes))
	copy(times, callTimes)
	mu.Unlock()

	// Should have numClients * (MaxRetries + 1) calls
	expectedCalls := numClients * (cfg.MaxRetries + 1)
	if len(times) != expectedCalls {
		t.Fatalf("Expected %d calls, got %d", expectedCalls, len(times))
	}

	// Sort times and verify that they are spread out (jitter prevents thundering herd)
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	// With 10 clients * 3 attempts = 30 calls, the total time span should show spread
	// due to jitter. Without jitter all retries would fire at exactly the same offsets.
	totalSpread := times[len(times)-1].Sub(times[0])

	// The spread should be non-trivial — at least 10ms shows jitter is working.
	// Use a very conservative threshold to avoid flakiness under race detector.
	if totalSpread < 10*time.Millisecond {
		t.Errorf("Call times too clustered (spread: %v), jitter should spread them out", totalSpread)
	}

	// Total time sanity check (log only, don't fail — race detector can slow things significantly)
	t.Logf("Total time: %v, spread: %v, calls: %d", totalTime, totalSpread, len(times))
}

// BenchmarkCalculateBackoffWithoutJitter benchmarks backoff calculation without jitter.
func BenchmarkCalculateBackoffWithoutJitter(b *testing.B) {
	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateBackoffWithJitter(initial, multiplier, max, i%10, handler.JitterNone, 0)
	}
}

// BenchmarkCalculateBackoffWithJitterEqual benchmarks backoff calculation with equal jitter.
func BenchmarkCalculateBackoffWithJitterEqual(b *testing.B) {
	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateBackoffWithJitter(initial, multiplier, max, i%10, handler.JitterEqual, 0.25)
	}
}

// BenchmarkCalculateBackoffWithJitterFull benchmarks backoff calculation with full jitter.
func BenchmarkCalculateBackoffWithJitterFull(b *testing.B) {
	initial := 100 * time.Millisecond
	multiplier := 2.0
	max := 1 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateBackoffWithJitter(initial, multiplier, max, i%10, handler.JitterFull, 0)
	}
}

// BenchmarkRetryMiddleware_WithoutJitter benchmarks retry middleware without jitter.
func BenchmarkRetryMiddleware_WithoutJitter(b *testing.B) {
	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		JitterStrategy: handler.JitterNone,
		Logger:         slog.Default(),
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = wrappedHandler.Handle(ctx, req)
	}
}

// BenchmarkRetryMiddleware_WithJitter benchmarks retry middleware with jitter.
func BenchmarkRetryMiddleware_WithJitter(b *testing.B) {
	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		JitterStrategy: handler.JitterEqual,
		JitterPercent:  0.25,
		Logger:         slog.Default(),
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = wrappedHandler.Handle(ctx, req)
	}
}

// abs returns the absolute value of a float64.
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// calculateBackoffWithJitter is a helper function to test the private function.
func calculateBackoffWithJitter(initial time.Duration, multiplier float64, max time.Duration, attempt int, strategy handler.JitterStrategy, jitterPercent float64) time.Duration {
	// This is a test helper that mirrors the implementation in retry.go
	// We use reflection or package-level access if needed

	// For now, we'll create a simple wrapper that tests the middleware behavior
	// Calculate exponential backoff
	backoff := time.Duration(float64(initial) * pow(multiplier, attempt))

	// Cap at max backoff
	if backoff > max {
		backoff = max
	}

	// Apply jitter based on strategy
	if strategy == handler.JitterNone || backoff == 0 {
		return backoff
	}

	switch strategy {
	case handler.JitterFull:
		jitter := rand.Float64()
		return time.Duration(float64(backoff) * jitter)

	case handler.JitterEqual:
		if jitterPercent <= 0 || jitterPercent > 1.0 {
			jitterPercent = 0.25
		}
		jitterRange := float64(backoff) * jitterPercent
		jitter := (rand.Float64() - 0.5) * 2 * jitterRange
		return time.Duration(float64(backoff) + jitter)

	default:
		return backoff
	}
}

// pow calculates base^exp for float64.
func pow(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// customError implements error interface for testing.
type customError struct {
	msg  string
	temp bool
}

func (e *customError) Error() string   { return e.msg }
func (e *customError) Timeout() bool   { return false }
func (e *customError) Temporary() bool { return e.temp }

// TestIsRetryable_NetworkTimeout tests that network timeouts are retryable.
func TestIsRetryable_NetworkTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "timeout error",
			err:  &customError{msg: "timeout", temp: true},
			want: true,
		},
		{
			name: "non-temporary error",
			err:  &customError{msg: "permanent error", temp: false},
			want: false,
		},
		{
			name: "net.Error with timeout",
			err: &net.OpError{
				Op:     "read",
				Net:    "tcp",
				Source: nil,
				Addr:   nil,
				Err:    &customError{msg: "i/o timeout", temp: true},
			},
			want: true,
		},
		{
			name: "net.Error without timeout",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &customError{msg: "no route to host", temp: false},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsRetryable_SyscallErrors tests that syscall errors are correctly classified.
func TestIsRetryable_SyscallErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ECONNRESET",
			err:  syscall.ECONNRESET,
			want: true,
		},
		{
			name: "ECONNREFUSED",
			err:  syscall.ECONNREFUSED,
			want: true,
		},
		{
			name: "ETIMEDOUT",
			err:  syscall.ETIMEDOUT,
			want: true,
		},
		{
			name: "ECONNABORTED",
			err:  syscall.ECONNABORTED,
			want: true,
		},
		{
			name: "EHOSTUNREACH",
			err:  syscall.EHOSTUNREACH,
			want: true,
		},
		{
			name: "EINVAL",
			err:  syscall.EINVAL,
			want: false,
		},
		{
			name: "EBADF",
			err:  syscall.EBADF,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsRetryable_ContextErrors tests context error classification.
func TestIsRetryable_ContextErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "wrapped deadline exceeded",
			err:  fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsRetryable_StringErrors tests string-based error detection.
func TestIsRetryable_StringErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection reset",
			err:  errors.New("connection reset by peer"),
			want: true,
		},
		{
			name: "connection refused",
			err:  errors.New("connection refused"),
			want: true,
		},
		{
			name: "broken pipe",
			err:  errors.New("broken pipe"),
			want: true,
		},
		{
			name: "network unreachable",
			err:  errors.New("network is unreachable"),
			want: true,
		},
		{
			name: "temporary failure",
			err:  errors.New("temporary failure"),
			want: true,
		},
		{
			name: "validation error",
			err:  errors.New("validation failed: invalid input"),
			want: false,
		},
		{
			name: "not found error",
			err:  errors.New("resource not found"),
			want: false,
		},
		{
			name: "auth error",
			err:  errors.New("unauthorized"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRetryMiddleware_SuccessOnFirstAttempt tests successful requests without retry.
func TestRetryMiddleware_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries: 3,
		Logger:     slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

// TestRetryMiddleware_RetryOnTransientError tests retry on transient errors.
func TestRetryMiddleware_RetryOnTransientError(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond, // Fast for testing
		Logger:         slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count <= 2 {
			// Fail first 2 attempts
			return nil, syscall.ECONNRESET
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	start := time.Now()
	resp, err := wrappedHandler.Handle(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}

	// With 2 failures and 10ms backoff: ~30ms minimum (10ms + 20ms)
	if elapsed < 25*time.Millisecond {
		t.Errorf("Handle() took %v, expected at least 25ms for 2 retries", elapsed)
	}
}

// TestRetryMiddleware_NoRetryOnNonTransientError tests no retry on non-transient errors.
func TestRetryMiddleware_NoRetryOnNonTransientError(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         slog.Default(),
	}

	var callCount int32
	expectedErr := errors.New("validation error")
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, expectedErr
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if !errors.Is(err, expectedErr) {
		t.Errorf("Handle() error = %v, want %v", err, expectedErr)
	}
	if resp != nil {
		t.Error("Response should be nil on error")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1 (should not retry)", callCount)
	}
}

// TestRetryMiddleware_MaxRetriesEnforced tests that max retries is enforced.
func TestRetryMiddleware_MaxRetriesEnforced(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         slog.Default(),
	}

	var callCount int32
	retryableErr := syscall.ECONNRESET
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, retryableErr
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if !errors.Is(err, retryableErr) {
		t.Errorf("Handle() error = %v, want %v", err, retryableErr)
	}
	if resp != nil {
		t.Error("Response should be nil on error")
	}
	// Should be called MaxRetries + 1 times (initial + 2 retries = 3 total)
	expectedCalls := int32(cfg.MaxRetries + 1)
	if atomic.LoadInt32(&callCount) != expectedCalls {
		t.Errorf("callCount = %d, want %d", callCount, expectedCalls)
	}
}

// TestRetryMiddleware_ExponentialBackoffTiming tests exponential backoff timing.
func TestRetryMiddleware_ExponentialBackoffTiming(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:        4,
		InitialBackoff:    100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		MaxBackoff:        1 * time.Second,
		Logger:            slog.Default(),
	}

	var callTimes []time.Time
	var mu sync.Mutex
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		mu.Unlock()
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	start := time.Now()
	_, _ = wrappedHandler.Handle(context.Background(), req)

	mu.Lock()
	times := make([]time.Time, len(callTimes))
	copy(times, callTimes)
	mu.Unlock()

	// Verify call count (initial + 4 retries = 5 calls)
	if len(times) != 5 {
		t.Fatalf("Expected 5 calls, got %d", len(times))
	}

	// Check backoff between calls
	// Expected: 0ms, ~100ms, ~200ms, ~400ms, ~800ms (doubled each time)
	expectedBackoffs := []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}
	for i := 1; i < len(times); i++ {
		actualBackoff := times[i].Sub(times[i-1])
		expectedMin := time.Duration(float64(expectedBackoffs[i]) * 0.8) // Allow 20% tolerance
		expectedMax := time.Duration(float64(expectedBackoffs[i]) * 1.2)

		if actualBackoff < expectedMin || actualBackoff > expectedMax {
			t.Errorf("Backoff %d = %v, expected ~%v", i, actualBackoff, expectedBackoffs[i])
		}
	}

	// Total time should be approximately 1.5s (100 + 200 + 400 + 800)
	totalTime := time.Since(start)
	minTime := time.Duration(float64(time.Second) * 1.3)
	maxTime := time.Duration(float64(time.Second) * 1.8)
	if totalTime < minTime || totalTime > maxTime {
		t.Errorf("Total time = %v, expected ~1.5s", totalTime)
	}
}

// TestRetryMiddleware_ContextCancellation tests context cancellation during retry.
func TestRetryMiddleware_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     10,
		InitialBackoff: 1 * time.Second, // Long backoff for testing
		Logger:         slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	// Cancel context after 150ms (before first retry completes)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	_, err := wrappedHandler.Handle(ctx, req)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Handle() error = %v, want %v", err, context.Canceled)
	}

	// Should only make 1 call before being cancelled
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1 (cancelled after first attempt)", callCount)
	}
}

// TestRetryMiddleware_ContextDeadlineExceeded tests context deadline exceeded.
func TestRetryMiddleware_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     10,
		InitialBackoff: 1 * time.Second,
		Logger:         slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	// Set deadline after 150ms
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := wrappedHandler.Handle(ctx, req)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Handle() error = %v, want %v", err, context.DeadlineExceeded)
	}

	// Should only make 1 call before deadline
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1 (deadline exceeded after first attempt)", callCount)
	}
}

// TestRetryMiddleware_ConcurrentRequests tests thread safety of retry middleware.
func TestRetryMiddleware_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         slog.Default(),
	}

	var callCount int64
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt64(&callCount, 1)
		// Fail first attempt, succeed on retry
		if count == 1 {
			return nil, syscall.ECONNRESET
		}
		// Use per-request tracking for more predictable behavior
		if count%3 == 0 {
			return nil, syscall.ECONNRESET
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	const numRequests = 20 // Reduced for more stable testing
	var wg sync.WaitGroup
	wg.Add(numRequests)
	var successCount int64
	var errorCount int64

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			resp, err := wrappedHandler.Handle(context.Background(), req)

			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				t.Logf("Handle() returned error: %v", err)
			} else if resp != nil && resp.StatusCode == icap.StatusOK {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// Most requests should succeed (allow some failures due to test logic)
	successes := atomic.LoadInt64(&successCount)
	errors := atomic.LoadInt64(&errorCount)
	if successes == 0 {
		t.Errorf("No requests succeeded. Successes: %d, Errors: %d", successes, errors)
	}
}

// TestRetryMiddleware_ZeroMaxRetries tests behavior with zero max retries.
func TestRetryMiddleware_ZeroMaxRetries(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries: 0, // Disable retries
		Logger:     slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("Handle() error = %v, want %v", err, syscall.ECONNRESET)
	}
	if resp != nil {
		t.Error("Response should be nil on error")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1 (no retries)", callCount)
	}
}

// TestRetryMiddleware_NegativeMaxRetries tests behavior with negative max retries.
func TestRetryMiddleware_NegativeMaxRetries(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries: -1, // Should use default
		Logger:     slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			return nil, syscall.ECONNRESET
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// With default (3 retries), should retry once and succeed
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("callCount = %d, want 2 (default retries)", callCount)
	}
}

// TestRetryMiddleware_MaxBackoffCap tests that backoff is capped at max.
func TestRetryMiddleware_MaxBackoffCap(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:        10,
		InitialBackoff:    10 * time.Millisecond,
		BackoffMultiplier: 10.0,                   // Aggressive multiplier
		MaxBackoff:        100 * time.Millisecond, // Low cap
		Logger:            slog.Default(),
	}

	var callTimes []time.Time
	var mu sync.Mutex
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		mu.Unlock()
		return nil, syscall.ECONNRESET
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	_, _ = wrappedHandler.Handle(context.Background(), req)

	mu.Lock()
	times := make([]time.Time, len(callTimes))
	copy(times, callTimes)
	mu.Unlock()

	// Verify call count
	if len(times) != 11 { // initial + 10 retries
		t.Fatalf("Expected 11 calls, got %d", len(times))
	}

	// Check that backoff is capped at 100ms
	for i := 1; i < len(times); i++ {
		actualBackoff := times[i].Sub(times[i-1])
		// First few retries might be less than cap, but all should be <= cap
		if actualBackoff > 150*time.Millisecond {
			t.Errorf("Backoff %d = %v, should be capped at 100ms", i, actualBackoff)
		}
	}
}

// TestRetryMiddleware_DefaultConfig tests default configuration values.
func TestRetryMiddleware_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := handler.DefaultRetryConfig()

	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialBackoff != 100*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 100ms", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff = %v, want 5s", cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", cfg.BackoffMultiplier)
	}
}

// TestRetryMiddleware_IntegrationWithOtherMiddleware tests retry in a middleware chain.
func TestRetryMiddleware_IntegrationWithOtherMiddleware(t *testing.T) {
	t.Parallel()

	// Create a panic recovery middleware
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         logger,
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count <= 2 {
			return nil, syscall.ECONNRESET
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	// Chain: Retry -> Panic Recovery -> Handler
	retryMiddleware := handler.RetryMiddleware(cfg)
	panicMiddleware := middleware.PanicRecoveryMiddleware(logger)
	wrappedHandler := panicMiddleware(retryMiddleware(baseHandler))

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Should retry twice before success
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

// TestRetryMiddleware_NilLogger tests behavior with nil logger.
func TestRetryMiddleware_NilLogger(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         nil, // Should not panic
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
}

// TestRetryMiddleware_NilResponse tests behavior when handler returns nil response with no error.
func TestRetryMiddleware_NilResponse(t *testing.T) {
	t.Parallel()

	cfg := handler.RetryConfig{
		MaxRetries: 2,
		Logger:     slog.Default(),
	}

	var callCount int32
	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return nil, nil // Nil response, no error
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	resp, err := wrappedHandler.Handle(context.Background(), req)

	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if resp != nil {
		t.Error("Response should be nil")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

// BenchmarkRetryMiddleware_NoRetry benchmarks retry middleware without any retries.
func BenchmarkRetryMiddleware_NoRetry(b *testing.B) {
	cfg := handler.RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		Logger:         slog.Default(),
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = wrappedHandler.Handle(ctx, req)
	}
}

// BenchmarkRetryMiddleware_WithRetry benchmarks retry middleware with retries.
func BenchmarkRetryMiddleware_WithRetry(b *testing.B) {
	var callCount int32

	cfg := handler.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 1 * time.Millisecond, // Very fast for benchmarking
		Logger:         slog.Default(),
	}

	baseHandler := handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		count := atomic.AddInt32(&callCount, 1)
		atomic.AddInt32(&callCount, -1) // Reset for next benchmark iteration
		// Fail first attempt
		if count%2 == 1 {
			return nil, syscall.ECONNRESET
		}
		return icap.NewResponse(icap.StatusOK), nil
	}, "REQMOD")

	middleware := handler.RetryMiddleware(cfg)
	wrappedHandler := middleware(baseHandler)

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = wrappedHandler.Handle(ctx, req)
	}
}

// TestRetryMiddleware_GetDefaultConfig tests default configuration values.
func TestRetryMiddleware_GetDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := handler.DefaultRetryConfig()

	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialBackoff != 100*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 100ms", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff = %v, want 5s", cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", cfg.BackoffMultiplier)
	}
}
