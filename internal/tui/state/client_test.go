// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 100*time.Millisecond)

	assert.NotNil(t, rl)
	assert.Equal(t, 10, rl.tokens)
	assert.Equal(t, 10, rl.maxTokens)
	assert.Equal(t, 100*time.Millisecond, rl.refillRate)
	assert.NotNil(t, rl.requestQueue)
	assert.Equal(t, 10, cap(rl.requestQueue))
}

func TestRateLimiter_Acquire_Success(t *testing.T) {
	rl := NewRateLimiter(5, 100*time.Millisecond)
	ctx := context.Background()

	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	assert.Equal(t, 4, rl.tokens)
	rl.mu.Unlock()
}

func TestRateLimiter_TokenExhaustion(t *testing.T) {
	rl := NewRateLimiter(3, 100*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens
	for i := 0; i < 3; i++ {
		err := rl.Acquire(ctx)
		assert.NoError(t, err, "should acquire token %d", i)
	}

	rl.mu.Lock()
	assert.Equal(t, 0, rl.tokens)
	rl.mu.Unlock()

	// Next acquisition should wait for refill
	start := time.Now()
	err := rl.Acquire(ctx)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "should wait for refill")
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	rl := NewRateLimiter(5, 50*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens
	for i := 0; i < 5; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	rl.mu.Lock()
	assert.Equal(t, 0, rl.tokens)
	rl.mu.Unlock()

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Trigger refill by calling Acquire
	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	assert.Greater(t, rl.tokens, 0, "tokens should be refilled")
	rl.mu.Unlock()

	// Should be able to acquire again without waiting
	err = rl.Acquire(ctx)
	assert.NoError(t, err)
}

func TestRateLimiter_ConcurrentAcquire(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Millisecond)
	ctx := context.Background()

	numGoroutines := 20
	numAcquisitionsPerGoroutine := 5

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numAcquisitionsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numAcquisitionsPerGoroutine; j++ {
				err := rl.Acquire(ctx)
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check that no errors occurred
	for err := range errors {
		assert.NoError(t, err)
	}

	rl.mu.Lock()
	assert.Equal(t, 10, rl.maxTokens)
	rl.mu.Unlock()
}

func TestRateLimiter_ContextCancellation(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	// Acquire the only token
	ctx := context.Background()
	err := rl.Acquire(ctx)
	require.NoError(t, err)

	// Try to acquire with canceled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = rl.Acquire(cancelledCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

func TestRateLimiter_ContextCancellationDuringWait(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	// Acquire the only token
	ctx := context.Background()
	err := rl.Acquire(ctx)
	require.NoError(t, err)

	// Create a context that will be canceled
	cancelCtx, cancel := context.WithCancel(context.Background())

	// Start a goroutine that tries to acquire and will block
	errChan := make(chan error, 1)
	go func() {
		errChan <- rl.Acquire(cancelCtx)
	}()

	// Wait a bit to ensure the goroutine is waiting
	time.Sleep(10 * time.Millisecond)

	// Cancel the context
	cancel()

	// Should receive error about cancellation
	select {
	case err := <-errChan:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "canceled")
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for canceled context error")
	}
}

func TestRateLimiter_BlockingWhenLimitExceeded(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens quickly
	for i := 0; i < 2; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	// Next acquire should block until refill
	start := time.Now()
	err := rl.Acquire(ctx)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestRateLimiter_EdgeCase_MaxTokensZero(t *testing.T) {
	rl := NewRateLimiter(0, 100*time.Millisecond)

	// Should still create a valid limiter
	assert.NotNil(t, rl)
	assert.Equal(t, 10, rl.maxTokens)
	assert.Equal(t, 10, cap(rl.requestQueue))
}

func TestRateLimiter_EdgeCase_NegativeInterval(t *testing.T) {
	rl := NewRateLimiter(5, -100*time.Millisecond)
	ctx := context.Background()

	// Should create a valid limiter with negative interval
	assert.NotNil(t, rl)
	assert.Equal(t, -100*time.Millisecond, rl.refillRate)

	// Should still allow acquisition
	err := rl.Acquire(ctx)
	assert.NoError(t, err)
}

func TestRateLimiter_RefillAccuracy(t *testing.T) {
	rl := NewRateLimiter(5, 50*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens
	for i := 0; i < 5; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	rl.mu.Lock()
	assert.Equal(t, 0, rl.tokens)
	rl.mu.Unlock()

	// Wait for exactly one refill period
	time.Sleep(50 * time.Millisecond)

	// Trigger refill by calling Acquire
	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	tokensAfterOnePeriod := rl.tokens
	rl.mu.Unlock()

	// Should have at least 0 tokens after one period (1 refilled - 1 consumed)
	assert.GreaterOrEqual(t, tokensAfterOnePeriod, 0)
	assert.LessOrEqual(t, tokensAfterOnePeriod, 5)
}

func TestRateLimiter_RefillMultipleTokens(t *testing.T) {
	rl := NewRateLimiter(10, 20*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens
	for i := 0; i < 10; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	rl.mu.Lock()
	assert.Equal(t, 0, rl.tokens)
	rl.mu.Unlock()

	// Wait for enough time to refill multiple tokens
	time.Sleep(60 * time.Millisecond)

	// Trigger refill by calling Acquire
	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	tokensAfterRefill := rl.tokens
	rl.mu.Unlock()

	// Should have at least 2 tokens after 3 periods (3 refilled - 1 consumed)
	assert.GreaterOrEqual(t, tokensAfterRefill, 2)
	assert.LessOrEqual(t, tokensAfterRefill, 10)
}

func TestRateLimiter_RequestQueueBlocking(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)
	ctx := context.Background()

	// Acquire all tokens
	for i := 0; i < 2; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	// Try to acquire with a timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := rl.Acquire(timeoutCtx)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestRateLimiter_RapidAcquisitionAndRelease(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Millisecond)
	ctx := context.Background()

	// Rapidly acquire and release tokens
	for i := 0; i < 100; i++ {
		err := rl.Acquire(ctx)
		assert.NoError(t, err, "iteration %d", i)
	}

	rl.mu.Lock()
	assert.Equal(t, 10, rl.maxTokens)
	rl.mu.Unlock()
}

func TestRateLimiter_StressTest(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Millisecond)
	ctx := context.Background()

	numGoroutines := 50
	numOperations := 100

	var wg sync.WaitGroup
	successCount := make(chan int, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			successes := 0
			for j := 0; j < numOperations; j++ {
				if rl.Acquire(ctx) == nil {
					successes++
				}
			}
			successCount <- successes
		}()
	}

	wg.Wait()
	close(successCount)

	totalSuccesses := 0
	for count := range successCount {
		totalSuccesses += count
	}

	// Should have completed all operations successfully
	assert.Equal(t, numGoroutines*numOperations, totalSuccesses)
}

func TestRateLimiter_RefillDoesNotExceedMax(t *testing.T) {
	rl := NewRateLimiter(5, 20*time.Millisecond)
	ctx := context.Background()

	// Acquire some tokens
	for i := 0; i < 3; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	// Wait for multiple refill periods
	time.Sleep(100 * time.Millisecond)

	// Trigger refill by calling Acquire
	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	tokensAfterRefill := rl.tokens
	rl.mu.Unlock()

	// Should not exceed max tokens (max is 5, started with 2, refilled to 5, consumed 1)
	assert.Equal(t, 4, tokensAfterRefill)
	assert.Equal(t, 5, rl.maxTokens)
}

func TestRateLimiter_ContextCancellationInQueue(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	// Fill the queue
	ctx := context.Background()
	err := rl.Acquire(ctx)
	require.NoError(t, err)

	// Try to acquire with a context that gets canceled quickly
	cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)

	start := time.Now()
	err = rl.Acquire(cancelCtx)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)
	cancel()
}

func TestRateLimiter_LastRefillUpdated(t *testing.T) {
	rl := NewRateLimiter(5, 50*time.Millisecond)
	ctx := context.Background()

	rl.mu.Lock()
	initialLastRefill := rl.lastRefill
	rl.mu.Unlock()

	// Acquire all tokens
	for i := 0; i < 5; i++ {
		err := rl.Acquire(ctx)
		require.NoError(t, err)
	}

	// Wait for refill
	time.Sleep(60 * time.Millisecond)

	// Trigger refill by calling Acquire
	err := rl.Acquire(ctx)
	assert.NoError(t, err)

	rl.mu.Lock()
	updatedLastRefill := rl.lastRefill
	rl.mu.Unlock()

	// Last refill should be updated
	assert.True(t, updatedLastRefill.After(initialLastRefill))
}
