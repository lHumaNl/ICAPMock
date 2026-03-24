package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestNewLimiter_TokenBucket(t *testing.T) {
	limiter, err := NewLimiter(AlgorithmTokenBucket, 100, 10)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify it's a working limiter
	if !limiter.Allow() {
		t.Error("expected first request to be allowed")
	}
}

func TestNewLimiter_SlidingWindow(t *testing.T) {
	limiter, err := NewLimiter(AlgorithmSlidingWindow, 100, 10)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify it's a working limiter
	if !limiter.Allow() {
		t.Error("expected first request to be allowed")
	}
}

func TestNewLimiter_UnsupportedAlgorithm(t *testing.T) {
	_, err := NewLimiter("invalid_algorithm", 100, 10)
	if err != ErrUnsupportedAlgorithm {
		t.Errorf("expected ErrUnsupportedAlgorithm, got: %v", err)
	}
}

func TestNewLimiterWithConfig_TokenBucket(t *testing.T) {
	cfg := Config{
		Rate:  50,
		Burst: 100,
	}

	limiter, err := NewLimiterWithConfig(AlgorithmTokenBucket, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify it works
	for i := 0; i < 100; i++ {
		if !limiter.Allow() {
			t.Errorf("expected request %d to be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("expected request to be denied after burst exhausted")
	}
}

func TestNewLimiterWithConfig_SlidingWindow(t *testing.T) {
	cfg := Config{
		Rate:   10,
		Burst:  5,
		Window: 500 * time.Millisecond,
	}

	limiter, err := NewLimiterWithConfig(AlgorithmSlidingWindow, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify it works
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Errorf("expected request %d to be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("expected request to be denied after capacity exhausted")
	}
}

func TestNewLimiterWithConfig_SlidingWindow_DefaultWindow(t *testing.T) {
	cfg := Config{
		Rate:  10,
		Burst: 5,
		// Window not specified, should default to 1 second
	}

	limiter, err := NewLimiterWithConfig(AlgorithmSlidingWindow, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Just verify it works
	if !limiter.Allow() {
		t.Error("expected request to be allowed")
	}
}

func TestNewLimiterWithConfig_UnsupportedAlgorithm(t *testing.T) {
	cfg := Config{
		Rate:  100,
		Burst: 10,
	}

	_, err := NewLimiterWithConfig("invalid_algorithm", cfg)
	if err != ErrUnsupportedAlgorithm {
		t.Errorf("expected ErrUnsupportedAlgorithm, got: %v", err)
	}
}

func TestLimiterInterface_TokenBucket(t *testing.T) {
	var limiter Limiter = NewTokenBucketLimiter(10, 5)

	// Test Allow
	if !limiter.Allow() {
		t.Error("expected Allow to return true")
	}

	// Test Reserve
	res := limiter.Reserve()
	if !res.OK() {
		t.Error("expected Reserve to return valid reservation")
	}
	if res.Delay() > 0 {
		t.Errorf("expected no delay, got %v", res.Delay())
	}

	// Test Wait
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("expected Wait to succeed, got: %v", err)
	}
}

func TestLimiterInterface_SlidingWindow(t *testing.T) {
	var limiter Limiter = NewSlidingWindowLimiter(10, 5)

	// Test Allow
	if !limiter.Allow() {
		t.Error("expected Allow to return true")
	}

	// Test Reserve
	res := limiter.Reserve()
	if !res.OK() {
		t.Error("expected Reserve to return valid reservation")
	}
	if res.Delay() > 0 {
		t.Errorf("expected no delay, got %v", res.Delay())
	}

	// Test Wait
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("expected Wait to succeed, got: %v", err)
	}
}

func TestReservationInterface(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 5)

	var res Reservation = limiter.Reserve()

	// Test OK
	if !res.OK() {
		t.Error("expected OK to return true")
	}

	// Test Delay
	if res.Delay() < 0 {
		t.Error("expected non-negative delay")
	}

	// Test Cancel (should not panic)
	res.Cancel()
}

func BenchmarkNewLimiter_TokenBucket(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewLimiter(AlgorithmTokenBucket, 10000, 15000)
	}
}

func BenchmarkNewLimiter_SlidingWindow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewLimiter(AlgorithmSlidingWindow, 10000, 15000)
	}
}

func BenchmarkTokenBucketLimiter_Allow(b *testing.B) {
	limiter := NewTokenBucketLimiter(10000, 15000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkTokenBucketLimiter_Allow_Parallel(b *testing.B) {
	limiter := NewTokenBucketLimiter(10000, 15000)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkTokenBucketLimiter_Reserve(b *testing.B) {
	limiter := NewTokenBucketLimiter(10000, 15000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Reserve()
	}
}

func BenchmarkSlidingWindowLimiter_Allow(b *testing.B) {
	limiter := NewSlidingWindowLimiter(10000, 15000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkSlidingWindowLimiter_Allow_Parallel(b *testing.B) {
	limiter := NewSlidingWindowLimiter(10000, 15000)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkSlidingWindowLimiter_Reserve(b *testing.B) {
	limiter := NewSlidingWindowLimiter(10000, 15000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Reserve()
	}
}
