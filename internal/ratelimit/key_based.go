// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
)

// GlobalKeyBasedLimiter wraps KeyBasedShardedTokenBucketLimiter with a default key
// to implement the Limiter interface for backward compatibility with global rate limiting.
//
// This wrapper allows using the key-based sharded limiter in scenarios where
// only the Limiter interface is required (e.g., existing middleware).
//
// Example:
//
//	limiter := NewGlobalKeyBasedLimiter(100, 150, GlobalKey)
//	if limiter.Allow() {
//	    // Process request
//	}
type GlobalKeyBasedLimiter struct {
	inner *KeyBasedShardedTokenBucketLimiter
	key   Key
}

// NewGlobalKeyBasedLimiter creates a new global key-based limiter.
//
// Parameters:
//   - rate: The number of tokens added per second
//   - burst: The maximum burst capacity
//   - key: The key to use for all rate limiting checks (typically GlobalKey)
//
// Returns:
//   - A new GlobalKeyBasedLimiter instance
//
// Example:
//
//	limiter := NewGlobalKeyBasedLimiter(10000, 15000, GlobalKey)
func NewGlobalKeyBasedLimiter(rate float64, burst int, key Key) *GlobalKeyBasedLimiter {
	return &GlobalKeyBasedLimiter{
		inner: NewKeyBasedShardedTokenBucketLimiter(rate, burst),
		key:   key,
	}
}

// Allow reports whether the next request can proceed immediately.
// Uses the configured key for rate limiting.
//
// Returns:
//   - true: Request is allowed
//   - false: Request is rate-limited
func (l *GlobalKeyBasedLimiter) Allow() bool {
	return l.inner.Allow(l.key)
}

// Wait blocks until a token is available or the context is canceled.
// Uses the configured key for rate limiting.
//
// Parameters:
//   - ctx: Context for timeout/cancellation
//
// Returns:
//   - error if context is canceled, nil otherwise
func (l *GlobalKeyBasedLimiter) Wait(ctx context.Context) error {
	return l.inner.Wait(ctx, l.key)
}

// Reserve returns a Reservation for one token.
// Uses the configured key for rate limiting.
//
// Returns:
//   - A reservation for one token
func (l *GlobalKeyBasedLimiter) Reserve() Reservation {
	return l.inner.Reserve(l.key)
}

// Inner returns the underlying KeyBasedShardedTokenBucketLimiter.
// This allows access to key-specific operations when needed.
//
// Returns:
//   - The underlying key-based sharded limiter
func (l *GlobalKeyBasedLimiter) Inner() *KeyBasedShardedTokenBucketLimiter {
	return l.inner
}
