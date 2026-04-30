// Copyright 2026 ICAP Mock

package ratelimit

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestPerMethodMiddleware_ServerLabelMetrics(t *testing.T) {
	perMethodLimiter := NewKeyBasedShardedTokenBucketLimiter(1, 1)
	reg := prometheus.NewRegistry()
	metricsCollector, _ := metrics.NewCollector(reg)
	config := &PerMethodRateLimitConfig{Enabled: true, Rate: 1, Burst: 1}
	middleware := NewPerMethodMiddlewareForServer(perMethodLimiter, nil, nil, metricsCollector, config, "edge-a")
	req := &icap.Request{Method: icap.MethodREQMOD}

	allowed, _ := middleware.Allow(context.Background(), req)
	if !allowed {
		t.Fatal("first request should be allowed")
	}
	allowed, _ = middleware.Allow(context.Background(), req)
	if allowed {
		t.Fatal("second request should be denied")
	}
	labels := map[string]string{"server": "edge-a"}
	if got := metricValue(t, reg, "icap_rate_limit_exceeded_total", labels); got != 1 {
		t.Errorf("rate limit exceeded = %v, want 1", got)
	}
}
