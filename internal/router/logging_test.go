// Copyright 2026 ICAP Mock

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestRouterLogsRequestArrivalAtDebug(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err := r.Handle("/avscan", &mockHandler{method: icap.MethodREQMOD}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	_, err := r.Serve(context.Background(), loggedRouterRequest())
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	entry := decodeRouterLogEntry(t, buf.Bytes())
	assertRouterLogField(t, entry, "level", "DEBUG")
	assertRouterLogField(t, entry, "msg", "ICAP request received")
	assertRouterLogField(t, entry, "method", icap.MethodREQMOD)
	assertRouterLogField(t, entry, "uri", "icap://localhost/avscan")
	assertRouterLogField(t, entry, "client_ip", "192.0.2.10")
	assertRouterLogFieldMissing(t, entry, "content_type")
}

func TestRouterLogsRequestScopedContentTypeLabel(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err := r.Handle("/avscan", &mockHandler{method: icap.MethodREQMOD}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	ctx := requestinfo.WithContentTypeLabel(context.Background(), metrics.ContentTypeOther)

	_, err := r.Serve(ctx, loggedRouterRequest())
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	entry := decodeRouterLogEntry(t, buf.Bytes())
	assertRouterLogFieldMissing(t, entry, "content_type")
}

func TestRouterLogsAndCountsRouteNotFoundAtError(t *testing.T) {
	var buf bytes.Buffer
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	r := NewRouter()
	r.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r.SetMetricsForServer(collector, "edge")

	resp, err := r.Serve(context.Background(), loggedRouterRequest())
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if resp.StatusCode != icap.StatusNotFound {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNotFound)
	}

	entry := decodeRouterLogEntry(t, buf.Bytes())
	assertRouterLogField(t, entry, "level", "ERROR")
	assertRouterLogField(t, entry, "stage", metrics.RequestErrorStageRouting)
	assertRouterLogField(t, entry, "error_type", metrics.RequestErrorTypeRouteNotFound)
	assertRouterLogField(t, entry, "content_type", "application/json")
	assertRouteNotFoundMetric(t, reg)
}

func loggedRouterRequest() *icap.Request {
	return &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/avscan",
		ClientIP: "192.0.2.10",
		Header:   icap.NewHeader(),
		HTTPRequest: &icap.HTTPMessage{
			Header: icap.Header{"Content-Type": {"Application/JSON; charset=utf-8"}},
		},
	}
}

func decodeRouterLogEntry(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return entry
}

func assertRouterLogField(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	if got, ok := entry[key].(string); !ok || got != want {
		t.Fatalf("%s = %v, want %q", key, entry[key], want)
	}
}

func assertRouterLogFieldMissing(t *testing.T, entry map[string]any, key string) {
	t.Helper()
	if got, ok := entry[key]; ok {
		t.Fatalf("%s = %v, want field to be omitted", key, got)
	}
}

func assertRouteNotFoundMetric(t *testing.T, reg prometheus.Gatherer) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "icap_request_errors_total" && len(mf.GetMetric()) == 1 {
			return
		}
	}
	t.Fatal("icap_request_errors_total route-not-found metric not found")
}
