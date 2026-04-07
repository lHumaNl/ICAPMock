// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"errors"
	"time"
)

// Common errors returned by the rate limiter.
var (
	// ErrContextCanceled is returned when the context is canceled while waiting.
	ErrContextCanceled = errors.New("rate limiter: context canceled")

	// ErrUnsupportedAlgorithm is returned when an unknown algorithm is specified.
	ErrUnsupportedAlgorithm = errors.New("rate limiter: unsupported algorithm")
)

// Limiter defines the interface for rate limiting implementations.
// All implementations must be safe for concurrent use by multiple goroutines.
type Limiter interface {
	// Allow reports whether the next request can proceed at this moment.
	// It returns true if the request is allowed, false otherwise.
	// Allow does not block; it makes a decision based on the current state.
	Allow() bool

	// Wait blocks until the rate limiter allows the request to proceed
	// or until the context is canceled. It returns nil if the request
	// was allowed, or the context error if the context was canceled.
	Wait(ctx context.Context) error

	// Reserve returns a Reservation that indicates how long the caller
	// must wait before proceeding. The caller can cancel the reservation
	// to return tokens to the limiter.
	Reserve() Reservation
}

// Reservation represents a reservation to proceed with a request.
// It provides information about the wait time and allows cancellation.
type Reservation interface {
	// OK reports whether the reservation is valid. A reservation is invalid
	// if the rate limiter cannot satisfy the request within the maximum
	// wait time (if one was specified).
	OK() bool

	// Delay returns the duration that the caller must wait before proceeding.
	// Returns 0 if no waiting is required.
	Delay() time.Duration

	// Cancel returns the reserved tokens to the rate limiter.
	// This should be called if the reservation will not be used.
	Cancel()
}

// Algorithm constants define the available rate limiting algorithms.
const (
	// AlgorithmTokenBucket uses the token bucket algorithm.
	// Tokens are added at a fixed rate, and requests consume tokens.
	// Allows for burst traffic up to the bucket size.
	AlgorithmTokenBucket = "token_bucket"

	// AlgorithmSlidingWindow uses the sliding window algorithm.
	// Tracks requests within a time window for smoother rate limiting.
	AlgorithmSlidingWindow = "sliding_window"

	// AlgorithmShardedTokenBucket uses a sharded token bucket algorithm.
	// Distributes requests across 16 independent shards to eliminate
	// mutex contention in high-concurrency scenarios (10,000+ RPS).
	// Best for high-throughput workloads where single-limiter contention
	// becomes a bottleneck.
	AlgorithmShardedTokenBucket = "sharded_token_bucket" //nolint:gosec // not credentials
)

// Config holds the configuration for creating a rate limiter.
type Config struct {
	// Rate is the number of requests allowed per second.
	Rate float64

	// Burst is the maximum number of requests that can be made
	// instantaneously (for token bucket) or the window capacity
	// (for sliding window).
	Burst int

	// Window is the time window for the sliding window algorithm.
	// Defaults to 1 second if not specified.
	Window time.Duration
}

// NewLimiter creates a new rate limiter based on the specified algorithm.
// The algorithm must be one of AlgorithmTokenBucket, AlgorithmSlidingWindow,
// or AlgorithmShardedTokenBucket.
//
// Parameters:
//   - algorithm: The rate limiting algorithm to use ("token_bucket", "sliding_window",
//     or "sharded_token_bucket")
//   - rate: The number of requests allowed per second
//   - burst: The maximum burst capacity (for token bucket) or window size (for sliding window)
//
// Returns an error if the algorithm is not supported.
func NewLimiter(algorithm string, rate float64, burst int) (Limiter, error) {
	switch algorithm {
	case AlgorithmTokenBucket:
		return NewTokenBucketLimiter(rate, burst), nil
	case AlgorithmSlidingWindow:
		return NewSlidingWindowLimiter(rate, burst), nil
	case AlgorithmShardedTokenBucket:
		// Use key-based sharded limiter with global key for backward compatibility
		return NewGlobalKeyBasedLimiter(rate, burst, GlobalKey), nil
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

// NewLimiterWithConfig creates a new rate limiter with the specified configuration.
// This provides more control over the limiter parameters.
func NewLimiterWithConfig(algorithm string, cfg Config) (Limiter, error) {
	switch algorithm {
	case AlgorithmTokenBucket:
		return NewTokenBucketLimiter(cfg.Rate, cfg.Burst), nil
	case AlgorithmSlidingWindow:
		window := cfg.Window
		if window <= 0 {
			window = time.Second
		}
		return NewSlidingWindowLimiterWithWindow(cfg.Rate, cfg.Burst, window), nil
	case AlgorithmShardedTokenBucket:
		// Use key-based sharded limiter with global key for backward compatibility
		return NewGlobalKeyBasedLimiter(cfg.Rate, cfg.Burst, GlobalKey), nil
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}
