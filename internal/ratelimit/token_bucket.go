// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"sync"
	"time"
)

// TokenBucketLimiter implements rate limiting using the token bucket algorithm.
//
// The token bucket algorithm works by maintaining a "bucket" of tokens that is
// filled at a constant rate. Each request consumes one token. If the bucket
// is empty, requests are denied. The bucket has a maximum capacity (burst size),
// allowing for temporary traffic bursts.
//
// This implementation is thread-safe and optimized for high-performance scenarios.
type TokenBucketLimiter struct {
	last   time.Time
	rate   float64
	burst  int
	tokens float64
	mu     sync.Mutex
}

// NewTokenBucketLimiter creates a new token bucket rate limiter.
//
// Parameters:
//   - rate: The number of tokens added per second. This determines the
//     sustained request rate. A rate of 10 means 10 requests per second.
//   - burst: The maximum bucket size. This determines how many requests
//     can be made instantaneously before being rate-limited.
//
// The limiter starts with a full bucket of tokens.
func NewTokenBucketLimiter(rate float64, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		rate:   rate,
		burst:  burst,
		tokens: float64(burst), // Start with a full bucket
		last:   time.Now(),
	}
}

// Allow reports whether the next request can proceed immediately.
// It consumes one token from the bucket if available.
// Returns true if the request is allowed, false otherwise.
func (l *TokenBucketLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.advance(now)

	// Check if we have enough tokens
	if l.tokens >= 1 {
		l.tokens--
		return true
	}

	return false
}

// Wait blocks until a token is available or the context is canceled.
// It returns nil if a token was acquired, or the context error otherwise.
func (l *TokenBucketLimiter) Wait(ctx context.Context) error {
	for {
		// First, try to acquire immediately
		l.mu.Lock()
		now := time.Now()
		l.advance(now)

		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}

		// Calculate wait time
		waitDuration := l.calculateWait(1)
		l.mu.Unlock()

		if waitDuration <= 0 {
			continue
		}

		// Wait for the delay or until context is done
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Continue and try again
		}
	}
}

// Reserve returns a Reservation for one token.
// Use the Reservation to determine how long to wait and to cancel if needed.
func (l *TokenBucketLimiter) Reserve() Reservation {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.advance(now)

	res := &reservation{
		limiter: l,
		tokens:  1,
	}

	// Check if we have enough tokens
	if l.tokens >= 1 {
		l.tokens--
		res.ok = true
		res.delay = 0
		res.timeToAct = now
		return res
	}

	// Not enough tokens - calculate wait time
	if l.rate <= 0 {
		res.ok = false
		res.delay = time.Duration(1<<63 - 1) // Max duration
		return res
	}

	// Reserve a future slot
	needed := 1.0 - l.tokens
	waitDuration := time.Duration(needed / l.rate * float64(time.Second))

	res.ok = true
	res.delay = waitDuration
	res.timeToAct = now.Add(waitDuration)

	// Consume the token (tokens will be negative)
	l.tokens--

	return res
}

// advance updates the token count based on elapsed time.
// Must be called with mutex held.
func (l *TokenBucketLimiter) advance(now time.Time) {
	elapsed := now.Sub(l.last)
	l.last = now

	// Add tokens based on elapsed time
	if l.rate > 0 && elapsed > 0 {
		l.tokens += elapsed.Seconds() * l.rate
	}

	// Cap tokens at burst size
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
}

// calculateWait returns the duration to wait for n tokens.
// Must be called with mutex held.
func (l *TokenBucketLimiter) calculateWait(n int) time.Duration {
	if l.rate <= 0 {
		return time.Duration(1<<63 - 1) // Max duration
	}
	needed := float64(n) - l.tokens
	if needed <= 0 {
		return 0
	}
	return time.Duration(needed / l.rate * float64(time.Second))
}

// reservation represents a reservation of tokens from the limiter.
type reservation struct {
	timeToAct time.Time
	limiter   *TokenBucketLimiter
	delay     time.Duration
	tokens    float64
	ok        bool
}

// OK reports whether the reservation is valid.
func (r *reservation) OK() bool {
	return r.ok
}

// Delay returns the duration to wait before the reservation can be used.
func (r *reservation) Delay() time.Duration {
	return r.delay
}

// Cancel returns the reserved tokens to the limiter.
func (r *reservation) Cancel() {
	if !r.ok {
		return
	}
	r.limiter.mu.Lock()
	defer r.limiter.mu.Unlock()
	r.limiter.tokens += r.tokens
}

// RateLimit returns the current rate (tokens per second).
func (l *TokenBucketLimiter) RateLimit() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// Burst returns the burst size (maximum bucket capacity).
func (l *TokenBucketLimiter) Burst() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.burst
}

// SetRate updates the rate limit. The change takes effect immediately.
func (l *TokenBucketLimiter) SetRate(rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = rate
}

// SetBurst updates the burst size. The change takes effect immediately.
func (l *TokenBucketLimiter) SetBurst(burst int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.burst = burst
	// If tokens exceed new burst, cap them
	if l.tokens > float64(burst) {
		l.tokens = float64(burst)
	}
}
