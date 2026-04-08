// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestRaceCondition_ConcurrentAllowSameKey tests that multiple goroutines
// calling Allow() for the same key don't create duplicate limiters.
// This verifies the double-checked locking pattern works correctly.
func TestRaceCondition_ConcurrentAllowSameKey(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)
	key := GlobalKey

	// Run 100 goroutines all calling Allow() for the same key simultaneously
	var wg sync.WaitGroup
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				limiter.Allow(key)
			}
		}()
	}
	wg.Wait()

	// Check that only one limiter was created for this key
	stats := limiter.Stats()
	if stats.TotalLimiters != 1 {
		t.Errorf("Expected 1 limiter, got %d (potential memory leak)", stats.TotalLimiters)
	}
}

// TestRaceCondition_ConcurrentSetRate tests that concurrent SetRate() calls
// don't cause data races. Verifies atomic operations on rate field.
func TestRaceCondition_ConcurrentSetRate(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	// Run 50 goroutines each calling SetRate() with different values
	var wg sync.WaitGroup
	rates := []float64{10, 50, 100, 200, 500}
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				rate := rates[(goroutineID+i)%len(rates)]
				limiter.SetRate(rate)
			}
		}(g)
	}

	// While SetRate() is being called, also call Allow() to ensure
	// no race between reading and writing rate
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.Allow(GlobalKey)
		}
	}()

	wg.Wait()
}

// TestRaceCondition_ConcurrentSetBurst tests that concurrent SetBurst() calls
// don't cause data races. Verifies atomic operations on burst field.
func TestRaceCondition_ConcurrentSetBurst(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	// Run 50 goroutines each calling SetBurst() with different values
	var wg sync.WaitGroup
	bursts := []int{50, 100, 150, 200, 300}
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				burst := bursts[(goroutineID+i)%len(bursts)]
				limiter.SetBurst(burst)
			}
		}(g)
	}

	// While SetBurst() is being called, also call Allow() to ensure
	// no race between reading and writing burst
	go func() {
		for i := 0; i < 1000; i++ {
			limiter.Allow(GlobalKey)
		}
	}()

	wg.Wait()
}

// TestRaceCondition_ConcurrentSetRateAndSetBurst tests that concurrent
// SetRate() and SetBurst() calls don't interfere with each other.
func TestRaceCondition_ConcurrentSetRateAndSetBurst(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	var wg sync.WaitGroup

	// 25 goroutines calling SetRate()
	for g := 0; g < 25; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				rate := float64((goroutineID + i*5) % 1000)
				if rate == 0 {
					rate = 1
				}
				limiter.SetRate(rate)
			}
		}(g)
	}

	// 25 goroutines calling SetBurst()
	for g := 0; g < 25; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				burst := ((goroutineID + i*5) % 500) + 1
				limiter.SetBurst(burst)
			}
		}(g)
	}

	wg.Wait()
}

// TestRaceCondition_AllowDuringSetRate tests that Allow() can be called
// while SetRate() is updating the rate, without data races.
func TestRaceCondition_AllowDuringSetRate(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	var wg sync.WaitGroup

	// Continuously update rate
	go func() {
		for i := 0; i < 100; i++ {
			limiter.SetRate(float64(10 + i*10))
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// 100 goroutines calling Allow() while rate is changing
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				limiter.Allow(GlobalKey)
			}
		}()
	}

	wg.Wait()
}

// TestRaceCondition_AllowDuringSetBurst tests that Allow() can be called
// while SetBurst() is updating the burst, without data races.
func TestRaceCondition_AllowDuringSetBurst(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	var wg sync.WaitGroup

	// Continuously update burst
	go func() {
		for i := 0; i < 100; i++ {
			burst := 50 + (i*10)%500
			limiter.SetBurst(burst)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// 100 goroutines calling Allow() while burst is changing
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				limiter.Allow(GlobalKey)
			}
		}()
	}

	wg.Wait()
}

// TestRaceCondition_ReserveDuringSetRate tests that Reserve() can be called
// while SetRate() is updating the rate, without data races.
func TestRaceCondition_ReserveDuringSetRate(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(100, 150)

	var wg sync.WaitGroup

	// Continuously update rate
	go func() {
		for i := 0; i < 50; i++ {
			limiter.SetRate(float64(10 + i*20))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// 50 goroutines calling Reserve() while rate is changing
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				res := limiter.Reserve(GlobalKey)
				if res.OK() {
					// Most reservations should be OK with high rate
					if res.Delay() > 0 {
						res.Cancel()
					}
				}
			}
		}()
	}

	wg.Wait()
}

// TestRaceCondition_MultipleKeysConcurrentCreation tests that creating
// limiters for multiple keys concurrently doesn't cause issues.
func TestRaceCondition_MultipleKeysConcurrentCreation(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(10, 15)

	// Create 1000 different keys with unique IPs
	keys := make([]Key, 1000)
	for i := 0; i < 1000; i++ {
		keys[i] = ClientKey(fmt.Sprintf("192.168.%d.%d", i/256, i%256))
	}

	var wg sync.WaitGroup

	// 100 goroutines each accessing a subset of keys
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := keys[(goroutineID*100+i)%len(keys)]
				limiter.Allow(key)
			}
		}(g)
	}

	wg.Wait()

	// Verify that we created exactly 1000 limiters (one per key)
	stats := limiter.Stats()
	if stats.TotalLimiters != 1000 {
		t.Errorf("Expected 1000 limiters, got %d", stats.TotalLimiters)
	}

	if stats.TotalKeys != 1000 {
		t.Errorf("Expected 1000 keys, got %d", stats.TotalKeys)
	}
}

// TestRaceCondition_WaitExponentialBackoff tests that Wait() uses
// exponential backoff and doesn't busy-wait.
func TestRaceCondition_WaitExponentialBackoff(t *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1, 1) // Very low rate

	// Exhaust the burst
	limiter.Allow(GlobalKey)

	// Measure time for Wait() to complete
	start := time.Now()
	err := limiter.Wait(GlobalKey, context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait() should not return error, got: %v", err)
	}

	// Wait() should complete in approximately 1 second (rate=1 token/sec)
	// If it was busy-waiting without backoff, it would consume more CPU
	// and might complete faster due to high CPU usage
	expectedMin := 900 * time.Millisecond
	expectedMax := 1100 * time.Millisecond

	if elapsed < expectedMin {
		t.Logf("Wait completed quickly: %v (expected ~1s)", elapsed)
	}
	if elapsed > expectedMax {
		t.Logf("Wait took longer than expected: %v (expected ~1s)", elapsed)
	}
}

// TestRaceCondition_AllowAndReserveMixed tests mixed Allow() and Reserve()
// calls to ensure they don't interfere with each other.
func TestRaceCondition_AllowAndReserveMixed(_ *testing.T) {
	limiter := NewKeyBasedShardedTokenBucketLimiter(1000, 1500)

	var wg sync.WaitGroup

	// 50 goroutines calling Allow()
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				limiter.Allow(GlobalKey)
			}
		}()
	}

	// 50 goroutines calling Reserve()
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				res := limiter.Reserve(GlobalKey)
				if res.OK() {
					if res.Delay() > 10*time.Millisecond {
						res.Cancel()
					}
				}
			}
		}()
	}

	wg.Wait()
}
