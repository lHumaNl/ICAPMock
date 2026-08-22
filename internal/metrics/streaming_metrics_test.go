// Copyright 2026 ICAP Mock

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCollector_StreamingActiveIsLabeledByServer(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.IncStreamingActiveForServer("edge-a")
	collector.IncStreamingActiveForServer("edge-a")
	collector.IncStreamingActiveForServer("edge-b")
	collector.DecStreamingActiveForServer("edge-a")
	collector.IncStreamingActive()

	if got := metricValue(t, reg, "icap_streaming_active", map[string]string{"server": "edge-a"}); got != 1 {
		t.Errorf("edge-a streaming active = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_streaming_active", map[string]string{"server": "edge-b"}); got != 1 {
		t.Errorf("edge-b streaming active = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_streaming_active", map[string]string{"server": defaultServerMetricLabel}); got != 1 {
		t.Errorf("default streaming active = %v, want 1", got)
	}

	assertMetricLabels(t, reg, "icap_streaming_active", []string{"server"})

	collector.DecStreamingActiveForServer("edge-a")
	collector.DecStreamingActiveForServer("edge-a")
	if got := metricValue(t, reg, "icap_streaming_active", map[string]string{"server": "edge-a"}); got != 0 {
		t.Errorf("edge-a streaming active after unmatched decrement = %v, want 0", got)
	}
}

func TestCollector_StreamingActiveForServerConcurrentLifecycle(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	const workers = 16
	const iterations = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				collector.IncStreamingActiveForServer("edge")
				collector.DecStreamingActiveForServer("edge")
			}
		}()
	}
	wg.Wait()

	if got := metricValue(t, reg, "icap_streaming_active", map[string]string{"server": "edge"}); got != 0 {
		t.Errorf("concurrent streaming active = %v, want 0", got)
	}
}

func TestHandlerWithRegistry_StreamingMetricExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.IncStreamingActiveForServer("edge")
	rr := httptest.NewRecorder()
	HandlerWithRegistry(reg).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))

	body := rr.Body.String()
	if !strings.Contains(body, "# HELP icap_streaming_active Current number of active streaming sessions by server.") {
		t.Fatalf("streaming metric HELP is missing or incorrect:\n%s", body)
	}
	if !strings.Contains(body, "icap_streaming_active{server=\"edge\"} 1") {
		t.Fatalf("streaming metric server label is missing:\n%s", body)
	}
}
