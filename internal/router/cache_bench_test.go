// Package router provides benchmarks for the route cache.
package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// benchHandler is a minimal handler.Handler implementation used in benchmarks.
type benchHandler struct {
	method string
}

func (h *benchHandler) Handle(_ context.Context, _ *icap.Request) (*icap.Response, error) {
	return icap.NewResponse(icap.StatusOK), nil
}

func (h *benchHandler) Method() string { return h.method }

// newBenchHandler creates a benchHandler that satisfies handler.Handler.
func newBenchHandler(method string) handler.Handler {
	return handler.WrapHandler(
		func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(icap.StatusOK), nil
		},
		method,
	)
}

// BenchmarkRouteCacheGet_Hit measures Get() latency when the entry is in the cache.
func BenchmarkRouteCacheGet_Hit(b *testing.B) {
	cache := NewRouteCache(1000, 5*time.Minute)
	h := newBenchHandler(icap.MethodREQMOD)

	// Pre-populate with a known entry.
	cache.Put(icap.MethodREQMOD, "/api/v1/scan", h)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		got, hit := cache.Get(icap.MethodREQMOD, "/api/v1/scan")
		if !hit || got == nil {
			b.Fatal("expected cache hit")
		}
	}
}

// BenchmarkRouteCacheGet_Miss measures Get() latency when the entry is absent.
func BenchmarkRouteCacheGet_Miss(b *testing.B) {
	cache := NewRouteCache(1000, 5*time.Minute)

	// Fill the cache with entries that don't match the lookup key.
	for i := 0; i < 500; i++ {
		cache.Put(icap.MethodREQMOD, fmt.Sprintf("/api/v1/route-%d", i), newBenchHandler(icap.MethodREQMOD))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, hit := cache.Get(icap.MethodREQMOD, "/api/v1/not-in-cache")
		if hit {
			b.Fatal("expected cache miss")
		}
	}
}

// BenchmarkRouteCacheGet_Concurrent measures Get() throughput under parallel access
// where all goroutines hit the same pre-warmed cache entry.
func BenchmarkRouteCacheGet_Concurrent(b *testing.B) {
	cache := NewRouteCache(1000, 5*time.Minute)
	h := newBenchHandler(icap.MethodREQMOD)
	cache.Put(icap.MethodREQMOD, "/api/v1/scan", h)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			got, hit := cache.Get(icap.MethodREQMOD, "/api/v1/scan")
			if !hit || got == nil {
				b.Error("expected cache hit in parallel benchmark")
			}
		}
	})
}
