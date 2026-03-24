package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenBucketLimiter_Allow_WhenTokensAvailable(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 5)

	// Should allow burst number of requests immediately
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Errorf("expected request %d to be allowed, but it was denied", i)
		}
	}

	// Should deny the next request (bucket is empty)
	if limiter.Allow() {
		t.Error("expected request to be denied when bucket is empty")
	}
}

func TestTokenBucketLimiter_Allow_WhenBurstExceeded(t *testing.T) {
	limiter := NewTokenBucketLimiter(1, 2)

	// Use up the burst
	limiter.Allow()
	limiter.Allow()

	// Should be denied
	if limiter.Allow() {
		t.Error("expected request to be denied when burst exceeded")
	}
}

func TestTokenBucketLimiter_TokenReplenishment(t *testing.T) {
	// 100 tokens per second = 1 token per 10ms
	limiter := NewTokenBucketLimiter(100, 2)

	// Use up the burst
	if !limiter.Allow() {
		t.Error("expected first request to be allowed")
	}
	if !limiter.Allow() {
		t.Error("expected second request to be allowed")
	}

	// Wait for 15ms, should have ~1.5 tokens (enough for 1 request)
	time.Sleep(15 * time.Millisecond)

	// Should allow one more request
	if !limiter.Allow() {
		t.Error("expected request to be allowed after token replenishment")
	}

	// But not a second one
	if limiter.Allow() {
		t.Error("expected request to be denied (not enough tokens replenished)")
	}
}

func TestTokenBucketLimiter_Wait_BlocksAndSucceeds(t *testing.T) {
	// 10 tokens per second
	limiter := NewTokenBucketLimiter(10, 1)

	// Use the burst
	if !limiter.Allow() {
		t.Error("expected first request to be allowed")
	}

	start := time.Now()

	// Wait should block and eventually succeed
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := limiter.Wait(ctx)
	if err != nil {
		t.Errorf("expected Wait to succeed, got error: %v", err)
	}

	elapsed := time.Since(start)
	// Should have waited at least ~100ms (1/10 of a second for 1 token at rate 10)
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected to wait at least 80ms, but only waited %v", elapsed)
	}
}

func TestTokenBucketLimiter_Wait_ImmediateAllow(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 5)

	ctx := context.Background()
	start := time.Now()

	err := limiter.Wait(ctx)
	if err != nil {
		t.Errorf("expected Wait to succeed immediately, got error: %v", err)
	}

	elapsed := time.Since(start)
	// Should be nearly instant
	if elapsed > 10*time.Millisecond {
		t.Errorf("expected Wait to return immediately, but took %v", elapsed)
	}
}

func TestTokenBucketLimiter_Wait_ContextCancellation(t *testing.T) {
	limiter := NewTokenBucketLimiter(1, 0) // Very low rate, no burst

	// Use any available tokens
	for limiter.Allow() {
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := limiter.Wait(ctx)
	if err == nil {
		t.Error("expected Wait to return error due to context cancellation")
	}
}

func TestTokenBucketLimiter_Reserve_WorksCorrectly(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 3)

	// Reserve should work when tokens are available
	res := limiter.Reserve()
	if !res.OK() {
		t.Error("expected reservation to be valid")
	}
	if res.Delay() > 0 {
		t.Errorf("expected no delay for immediate reservation, got %v", res.Delay())
	}

	// Cancel the reservation to return the token
	res.Cancel()

	// Should still have tokens available
	if !limiter.Allow() {
		t.Error("expected token to be available after reservation cancel")
	}
}

func TestTokenBucketLimiter_Reserve_WithDelay(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 1)

	// Use the burst
	limiter.Allow()

	// Reserve should indicate a delay
	res := limiter.Reserve()
	if !res.OK() {
		t.Error("expected reservation to be valid")
	}

	delay := res.Delay()
	if delay <= 0 {
		t.Errorf("expected positive delay when bucket is empty, got %v", delay)
	}
}

func TestTokenBucketLimiter_Concurrency(t *testing.T) {
	limiter := NewTokenBucketLimiter(1000, 100)

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

	// Should have allowed at least burst (100) but no more than total (1000)
	if allowedCount < 100 {
		t.Errorf("expected at least 100 allowed requests, got %d", allowedCount)
	}
	if allowedCount > 1000 {
		t.Errorf("expected at most 1000 allowed requests, got %d", allowedCount)
	}
}

func TestTokenBucketLimiter_Wait_Concurrency(t *testing.T) {
	limiter := NewTokenBucketLimiter(100, 50)

	const goroutines = 20
	var wg sync.WaitGroup
	var successCount int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			if err := limiter.Wait(ctx); err == nil {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// All should succeed within the timeout
	if successCount != goroutines {
		t.Errorf("expected %d successful waits, got %d", goroutines, successCount)
	}
}

func TestTokenBucketLimiter_ZeroRate(t *testing.T) {
	limiter := NewTokenBucketLimiter(0, 5)

	// Should still allow burst
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Errorf("expected request %d to be allowed with zero rate", i)
		}
	}

	// After burst, should always deny (no replenishment)
	if limiter.Allow() {
		t.Error("expected request to be denied with zero rate after burst")
	}
}

func TestTokenBucketLimiter_ZeroBurst(t *testing.T) {
	// With zero burst, only replenishment allows requests
	limiter := NewTokenBucketLimiter(100, 0)

	// Initially might allow if tokens have accumulated, but test immediate behavior
	// The implementation may vary, so let's just test that it doesn't panic
	limiter.Allow()
}

func TestTokenBucketLimiter_FullReplenishment(t *testing.T) {
	// Test that tokens don't exceed burst size
	limiter := NewTokenBucketLimiter(1000, 3)

	// Wait enough time for bucket to be completely full (more than 3 tokens worth)
	time.Sleep(10 * time.Millisecond)

	// Should only allow burst amount
	allowed := 0
	for i := 0; i < 10; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	if allowed > 3 {
		t.Errorf("expected at most 3 requests to be allowed (burst size), got %d", allowed)
	}
}

func TestTokenBucketLimiter_RateLimit(t *testing.T) {
	limiter := NewTokenBucketLimiter(5, 10)
	if limiter.RateLimit() != 5 {
		t.Errorf("expected rate limit 5, got %f", limiter.RateLimit())
	}
}

func TestTokenBucketLimiter_Burst(t *testing.T) {
	limiter := NewTokenBucketLimiter(5, 10)
	if limiter.Burst() != 10 {
		t.Errorf("expected burst 10, got %d", limiter.Burst())
	}
}

func TestTokenBucketLimiter_SetRate(t *testing.T) {
	limiter := NewTokenBucketLimiter(5, 10)
	limiter.SetRate(20)
	if limiter.RateLimit() != 20 {
		t.Errorf("expected rate limit 20 after SetRate, got %f", limiter.RateLimit())
	}
}

func TestTokenBucketLimiter_SetBurst(t *testing.T) {
	limiter := NewTokenBucketLimiter(5, 10)
	limiter.SetBurst(15)
	if limiter.Burst() != 15 {
		t.Errorf("expected burst 15 after SetBurst, got %d", limiter.Burst())
	}
}
