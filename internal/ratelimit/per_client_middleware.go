// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const clientIPUnknown = "unknown"

// PerClientMiddleware wraps a rate limiter with per-client limiting logic.
// It implements middleware that can be used in the ICAP request handling pipeline.
type PerClientMiddleware struct {
	perClientLimiter *PerClientRateLimiter
	globalLimiter    Limiter // Fallback global limiter
	metrics          *metrics.Collector
}

// NewPerClientMiddleware creates a new per-client rate limiting middleware.
//
// Parameters:
//   - perClientLimiter: Per-client rate limiter (can be nil if disabled)
//   - globalLimiter: Global fallback rate limiter
//   - metrics: Prometheus metrics collector
//
// Example:
//
//	perClientLimiter := ratelimit.NewPerClientRateLimiter(config)
//	globalLimiter := ratelimit.NewShardedTokenBucketLimiter(10000, 15000)
//	middleware := ratelimit.NewPerClientMiddleware(perClientLimiter, globalLimiter, metrics)
func NewPerClientMiddleware(
	perClientLimiter *PerClientRateLimiter,
	globalLimiter Limiter,
	mc *metrics.Collector,
) *PerClientMiddleware {
	return &PerClientMiddleware{
		perClientLimiter: perClientLimiter,
		globalLimiter:    globalLimiter,
		metrics:          mc,
	}
}

// Allow checks if the request should be allowed based on rate limiting.
//
// The process is:
//  1. Extract client IP from request
//  2. If IP is "unknown", skip per-client limiting (DoS protection)
//  3. Check per-client limiter (if enabled)
//  4. Fall back to global limiter if per-client check fails
//  5. Record metrics
//
// Returns:
//   - allowed: true if request is allowed, false otherwise
//   - err: error if context is canceled during Wait
func (m *PerClientMiddleware) Allow(_ context.Context, req *icap.Request) (allowed bool, err error) {
	clientIP := m.extractClientIP(req)

	// Skip per-client limiting for unknown IPs to prevent DoS attacks
	// Attackers could use spoofed/missing IPs to bypass rate limiting
	// by sharing a common "unknown" bucket
	if m.perClientLimiter != nil && m.perClientLimiter.GetConfig().Enabled && clientIP != clientIPUnknown {
		// Try per-client limiting first
		allowed, ok := m.perClientLimiter.Allow(clientIP)

		if ok {
			// Client was in cache - update metrics
			m.metrics.SetPerClientRateLimitActive(m.perClientLimiter.Stats().ActiveClients)
		}

		if allowed {
			return true, nil
		}

		// Per-client limiter denied request
		m.metrics.RecordPerClientRateLimitExceeded("")
		return false, nil
	}

	// Fall back to global limiter (also used for unknown IPs)
	if m.globalLimiter != nil {
		if m.globalLimiter.Allow() {
			return true, nil
		}

		m.metrics.RecordRateLimitExceeded("global")
		return false, nil
	}

	// No rate limiting configured - allow all
	return true, nil
}

// Wait blocks until the request is allowed or the context is canceled.
//
// This method uses the per-client limiter's Wait if available,
// otherwise falls back to the global limiter's Wait.
func (m *PerClientMiddleware) Wait(ctx context.Context, req *icap.Request) error {
	clientIP := m.extractClientIP(req)

	// Skip per-client limiting for unknown IPs (DoS protection)
	if m.perClientLimiter != nil && m.perClientLimiter.GetConfig().Enabled && clientIP != clientIPUnknown {
		// Try per-client limiting first
		allowed, ok := m.perClientLimiter.Allow(clientIP)

		if ok {
			// Client was in cache
			m.metrics.SetPerClientRateLimitActive(m.perClientLimiter.Stats().ActiveClients)
		}

		if allowed {
			return nil
		}

		// Per-client limiter denied - fall back to global limiter for waiting
		// Note: We don't try to get a reservation from per-client limiter
		// to avoid race conditions (double Allow() call)
	}

	// Fall back to global limiter (also used for unknown IPs and waiting)
	if m.globalLimiter != nil {
		if m.globalLimiter.Allow() {
			return nil
		}

		reservation := m.globalLimiter.Reserve()
		if !reservation.OK() {
			m.metrics.RecordRateLimitExceeded("global")
			return nil // Cannot wait
		}

		delay := reservation.Delay()
		m.metrics.RecordRateLimitWaitTime("global", delay)

		if delay > 0 {
			select {
			case <-ctx.Done():
				reservation.Cancel()
				return ctx.Err()
			case <-time.After(delay):
				// Proceed
			}
		}

		return nil
	}

	// No rate limiting configured
	return nil
}

// extractClientIP extracts the client IP address from an ICAP request.
//
// It checks the following sources in order:
//  1. X-Client-IP header (if set)
//  2. X-Forwarded-For header (takes first IP if multiple)
//  3. RemoteAddr field from request
//  4. "unknown" if no IP can be determined
func (m *PerClientMiddleware) extractClientIP(req *icap.Request) string {
	if req == nil {
		return clientIPUnknown
	}

	// Check ClientIP field (may be set by connection handler)
	if req.ClientIP != "" && req.ClientIP != clientIPUnknown {
		return req.ClientIP
	}

	// Check X-Client-IP header
	if clientIP, exists := req.GetHeader("X-Client-IP"); exists {
		if ip := net.ParseIP(clientIP); ip != nil {
			return ip.String()
		}
	}

	// Check X-Forwarded-For header
	if xff, exists := req.GetHeader("X-Forwarded-For"); exists {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		// We take the first one (original client)
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			if ip := net.ParseIP(strings.TrimSpace(ips[0])); ip != nil {
				return ip.String()
			}
		}
	}

	// Check RemoteAddr
	if req.RemoteAddr != "" {
		// RemoteAddr is typically "IP:port", we just want the IP
		if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if ip := net.ParseIP(host); ip != nil {
				return ip.String()
			}
		}
	}

	return clientIPUnknown
}

