package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
	"github.com/prometheus/client_golang/prometheus"
)

// TestNewPerClientMiddleware tests the middleware constructor.
func TestNewPerClientMiddleware(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             20,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(1000, 1500)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	if middleware == nil {
		t.Fatal("middleware is nil")
	}
	if middleware.perClientLimiter == nil {
		t.Error("perClientLimiter is nil")
	}
	if middleware.globalLimiter == nil {
		t.Error("globalLimiter is nil")
	}
	if middleware.metrics == nil {
		t.Error("metrics is nil")
	}
}

// TestPerClientMiddleware_Allow tests the Allow method.
func TestPerClientMiddleware_Allow(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(1000, 1500)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	req := &icap.Request{
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}

	ctx := context.Background()

	// Should allow burst requests (10)
	for i := 0; i < 10; i++ {
		allowed, err := middleware.Allow(ctx, req)
		if err != nil {
			t.Errorf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: expected allowed=true", i)
		}
	}

	// Next request should be denied
	allowed, err := middleware.Allow(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected request to be denied (burst exceeded)")
	}
}

// TestPerClientMiddleware_Allow_ExtractIP tests IP extraction from various sources.
func TestPerClientMiddleware_Allow_ExtractIP(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(1000, 1500)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	tests := []struct {
		name     string
		request  *icap.Request
		expected string
	}{
		{
			name: "ClientIP field",
			request: &icap.Request{
				ClientIP:   "10.0.0.1",
				RemoteAddr: "10.0.0.1:12345",
			},
			expected: "10.0.0.1",
		},
		{
			name: "X-Client-IP header",
			request: func() *icap.Request {
				req := &icap.Request{
					RemoteAddr: "10.0.0.1:12345",
				}
				req.SetHeader("X-Client-IP", "10.0.0.2")
				return req
			}(),
			expected: "10.0.0.2",
		},
		{
			name: "X-Forwarded-For header",
			request: func() *icap.Request {
				req := &icap.Request{
					RemoteAddr: "10.0.0.1:12345",
				}
				req.SetHeader("X-Forwarded-For", "10.0.0.3, 10.0.0.4")
				return req
			}(),
			expected: "10.0.0.3",
		},
		{
			name: "RemoteAddr only",
			request: &icap.Request{
				RemoteAddr: "10.0.0.5:54321",
			},
			expected: "10.0.0.5",
		},
		{
			name:     "nil request",
			request:  nil,
			expected: "unknown",
		},
		{
			name:     "no IP information",
			request:  &icap.Request{},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := middleware.extractClientIP(tt.request)
			if ip != tt.expected {
				t.Errorf("expected IP=%s, got %s", tt.expected, ip)
			}
		})
	}
}

// TestPerClientMiddleware_Allow_Disabled tests that disabled per-client limiter falls back to global.
func TestPerClientMiddleware_Allow_Disabled(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled: false, // Disabled
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(10, 10) // Very low limit
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	req := &icap.Request{
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}

	ctx := context.Background()

	// Should allow burst requests (10 from global limiter)
	for i := 0; i < 10; i++ {
		allowed, err := middleware.Allow(ctx, req)
		if err != nil {
			t.Errorf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: expected allowed=true", i)
		}
	}

	// Next request should be denied (global limiter)
	allowed, err := middleware.Allow(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected request to be denied (global limiter burst exceeded)")
	}
}

// TestPerClientMiddleware_Allow_NoLimiter tests with no limiters configured.
func TestPerClientMiddleware_Allow_NoLimiter(t *testing.T) {
	middleware := NewPerClientMiddleware(nil, nil, nil)

	req := &icap.Request{
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}

	ctx := context.Background()

	// Should allow all requests when no limiters configured
	for i := 0; i < 1000; i++ {
		allowed, err := middleware.Allow(ctx, req)
		if err != nil {
			t.Errorf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: expected allowed=true", i)
		}
	}
}

// TestPerClientMiddleware_Wait tests the Wait method.
func TestPerClientMiddleware_Wait(t *testing.T) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
		MaxClients:        100,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(1000, 1500)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	req := &icap.Request{
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}

	ctx := context.Background()

	// Should allow burst requests (10)
	for i := 0; i < 10; i++ {
		err := middleware.Wait(ctx, req)
		if err != nil {
			t.Errorf("request %d: unexpected error: %v", i, err)
		}
	}

	// Next wait should succeed quickly (will use reservation)
	// Since per-client doesn't support reservations well, this tests the fallback
	start := time.Now()
	err := middleware.Wait(ctx, req)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should complete quickly (< 1s)
	if duration > time.Second {
		t.Errorf("expected quick completion, took %v", duration)
	}
}

// BenchmarkPerClientMiddleware_Allow benchmarks the Allow method.
func BenchmarkPerClientMiddleware_Allow(b *testing.B) {
	config := PerClientRateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10000,
		Burst:             20000,
		MaxClients:        10000,
		TTL:               5 * time.Minute,
	}

	perClientLimiter := NewPerClientRateLimiter(config)
	defer perClientLimiter.Stop()

	globalLimiter := NewTokenBucketLimiter(10000, 20000)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)

	middleware := NewPerClientMiddleware(perClientLimiter, globalLimiter, metricsCollector)

	req := &icap.Request{
		ClientIP:   "192.168.1.1",
		RemoteAddr: "192.168.1.1:12345",
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		middleware.Allow(ctx, req)
	}
}
