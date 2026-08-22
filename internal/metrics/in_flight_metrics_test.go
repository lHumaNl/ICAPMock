// Copyright 2026 ICAP Mock

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCollector_RequestsProcessingInFlight(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.IncRequestsProcessingInFlightForServer("edge", "REQMOD")
	collector.IncRequestsProcessingInFlightForServer("edge", "REQMOD")
	collector.DecRequestsProcessingInFlightForServer("edge", "REQMOD")

	if got := testutil.ToFloat64(collector.requestsProcessingInFlight.WithLabelValues("edge", "REQMOD")); got != 1 {
		t.Fatalf("processing in-flight = %v, want 1", got)
	}
}

func TestHandlerWithRegistry_InFlightHelpDescribesDistinctBoundaries(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	collector.IncRequestsInFlightForServer("edge", "REQMOD")
	collector.IncRequestsProcessingInFlightForServer("edge", "REQMOD")

	rr := httptest.NewRecorder()
	HandlerWithRegistry(reg).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body := rr.Body.String()
	if !strings.Contains(body,
		"# HELP icap_requests_in_flight Current number of ICAP requests awaiting terminal response delivery by server and method.",
	) {
		t.Fatalf("lifecycle in-flight HELP missing:\n%s", body)
	}
	if !strings.Contains(body,
		"# HELP icap_requests_processing_in_flight Current number of ICAP requests executing shared handler and processor response preparation by server and method, excluding response delivery.",
	) {
		t.Fatalf("processing in-flight HELP missing:\n%s", body)
	}
}
