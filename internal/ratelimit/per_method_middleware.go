// Package ratelimit provides middleware for per-method rate limiting.
//
// This file implements middleware that:
//   - Extracts ICAP method from requests
//   - Applies per-method rate limiting
//   - Integrates with per-client limiting for combined client+method limiting
//   - Records Prometheus metrics
//
// The middleware supports:
//   - Per-method only rate limiting
//   - Per-client+per-method combined rate limiting
//   - Graceful fallback to global rate limiting
package ratelimit

import (
	"context"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// PerMethodMiddleware wraps a rate limiter with per-method limiting logic.
// It implements middleware that can be used in the ICAP request handling pipeline.
type PerMethodMiddleware struct {
	perMethodLimiter *KeyBasedShardedTokenBucketLimiter
	perClientLimiter *PerClientRateLimiter
	globalLimiter    Limiter
	metrics          *metrics.Collector
	config           *PerMethodRateLimitConfig
}

// PerMethodRateLimitConfig holds configuration for per-method rate limiting.
type PerMethodRateLimitConfig struct {
	Enabled bool
	Rate    float64
	Burst   int
}

// NewPerMethodMiddleware creates a new per-method rate limiting middleware.
//
// Parameters:
//   - perMethodLimiter: Key-based sharded limiter for per-method limiting
//   - perClientLimiter: Per-client limiter (optional, for combined limiting)
//   - globalLimiter: Global fallback rate limiter
//   - metrics: Prometheus metrics collector
//   - config: Per-method rate limit configuration
//
// Example:
//
//	perMethodLimiter := ratelimit.NewKeyBasedShardedTokenBucketLimiter(5000, 7500)
//	middleware := ratelimit.NewPerMethodMiddleware(perMethodLimiter, nil, globalLimiter, metrics, config)
func NewPerMethodMiddleware(
	perMethodLimiter *KeyBasedShardedTokenBucketLimiter,
	perClientLimiter *PerClientRateLimiter,
	globalLimiter Limiter,
	metrics *metrics.Collector,
	config *PerMethodRateLimitConfig,
) *PerMethodMiddleware {
	return &PerMethodMiddleware{
		perMethodLimiter: perMethodLimiter,
		perClientLimiter: perClientLimiter,
		globalLimiter:    globalLimiter,
		metrics:          metrics,
		config:           config,
	}
}

// Allow checks if the request should be allowed based on rate limiting.
//
// The process is:
//  1. Extract ICAP method from request
//  2. Check per-method limiter (if enabled)
//  3. Check per-client+method limiter (if both enabled)
//  4. Fall back to global limiter if needed
//  5. Record metrics
//
// Returns:
//   - allowed: true if request is allowed, false otherwise
//   - err: error if context is canceled during Wait
func (m *PerMethodMiddleware) Allow(ctx context.Context, req *icap.Request) (allowed bool, err error) {
	if req == nil {
		return true, nil // No request, allow
	}

	method := req.Method
	if method == "" {
		return true, nil // No method, allow
	}

	// Check per-method rate limiting
	if m.perMethodLimiter != nil && m.config != nil && m.config.Enabled {
		methodKey := MethodKey(method)
		if !m.perMethodLimiter.Allow(methodKey) {
			m.metrics.RecordRateLimitExceeded("per_method:" + method)
			return false, nil
		}

		// If both per-method and per-client are enabled,
		// check combined client+method rate limiting
		if m.perClientLimiter != nil && m.perClientLimiter.GetConfig().Enabled && req.ClientIP != "unknown" {
			clientMethodKey := ClientMethodKey(req.ClientIP, method)
			allowed, ok := m.perClientLimiter.Allow(req.ClientIP)

			if ok {
				// Client was in cache
				if !allowed {
					m.metrics.RecordPerClientRateLimitExceeded("")
					return false, nil
				}

				// Check combined client+method limit
				if !m.perMethodLimiter.Allow(clientMethodKey) {
					m.metrics.RecordRateLimitExceeded("per_client_method:" + req.ClientIP + ":" + method)
					return false, nil
				}
			}
		}
	}

	// Fall back to global limiter
	if m.globalLimiter != nil {
		if m.globalLimiter.Allow() {
			return true, nil
		}

		m.metrics.RecordRateLimitExceeded("global")
		return false, nil
	}

	// No rate limiting configured
	return true, nil
}

// Wait blocks until the request is allowed or the context is canceled.
func (m *PerMethodMiddleware) Wait(ctx context.Context, req *icap.Request) error {
	if req == nil {
		return nil
	}

	method := req.Method
	if method == "" {
		return nil
	}

	// Check per-method rate limiting
	if m.perMethodLimiter != nil && m.config != nil && m.config.Enabled {
		methodKey := MethodKey(method)

		// Try immediate allow
		if m.perMethodLimiter.Allow(methodKey) {
			return nil
		}

		// Wait for token
		if err := m.perMethodLimiter.Wait(methodKey, ctx); err != nil {
			return err
		}

		// If both per-method and per-client are enabled,
		// check combined limiting
		if m.perClientLimiter != nil && m.perClientLimiter.GetConfig().Enabled && req.ClientIP != "unknown" {
			clientMethodKey := ClientMethodKey(req.ClientIP, method)
			allowed, _ := m.perClientLimiter.Allow(req.ClientIP)

			if !allowed {
				m.metrics.RecordPerClientRateLimitExceeded("")
			}

			// Wait for client+method token
			if err := m.perMethodLimiter.Wait(clientMethodKey, ctx); err != nil {
				return err
			}
		}

		return nil
	}

	// Fall back to global limiter
	if m.globalLimiter != nil {
		if m.globalLimiter.Allow() {
			return nil
		}

		return m.globalLimiter.Wait(ctx)
	}

	return nil
}

// GetStats returns statistics about the per-method rate limiter.
func (m *PerMethodMiddleware) GetStats() ShardedStats {
	if m.perMethodLimiter == nil {
		return ShardedStats{}
	}
	return m.perMethodLimiter.Stats()
}

// GetMethodKey returns the rate limit key for a specific method.
func (m *PerMethodMiddleware) GetMethodKey(method string) Key {
	return MethodKey(method)
}
