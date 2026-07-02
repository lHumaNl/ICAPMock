// Copyright 2026 ICAP Mock

package server

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestRecordIncomingRequestUsesConfiguredEndpointLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	srv.SetMetricsEndpointLabelMode(metricsinternal.EndpointLabelModePath)

	req := &icap.Request{Method: icap.MethodREQMOD, URI: "icap://localhost:1344/scan/file.exe?token=secret"}
	resp := icap.NewResponse(icap.StatusOK)
	resp.HTTPResponse = &icap.HTTPMessage{Status: "403"}
	srv.recordIncomingRequest(req, resp)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "endpoint": "/scan/file.exe",
		"extension": "exe", "result": "success", "icap_status": "200", "blocked": "true",
	}
	if got := counterValue(t, reg, "icap_incoming_requests_total", labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestDefaultEndpointAndNilResponse(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}

	req := &icap.Request{Method: icap.MethodRESPMOD, URI: "/upload/archive"}
	srv.recordIncomingRequest(req, nil)

	labels := map[string]string{
		"server": "edge", "method": "RESPMOD", "endpoint": "default",
		"extension": "none", "result": "error", "icap_status": "none", "blocked": "true",
	}
	if got := counterValue(t, reg, "icap_incoming_requests_total", labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestBoundsUnknownExtension(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}

	req := &icap.Request{Method: icap.MethodREQMOD, URI: "icap://localhost:1344/scan/file.untrustedextension"}
	resp := icap.NewResponse(403)
	srv.recordIncomingRequest(req, resp)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "endpoint": "default",
		"extension": "other", "result": "error", "icap_status": "403", "blocked": "true",
	}
	if got := counterValue(t, reg, "icap_incoming_requests_total", labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func counterValue(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	for _, metric := range metricFamily(t, reg, name).GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetCounter().GetValue()
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
	return 0
}

func metricFamily(t *testing.T, reg prometheus.Gatherer, name string) *dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	t.Fatalf("metric %s not found", name)
	return nil
}

func labelsMatch(metric *dto.Metric, labels map[string]string) bool {
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return len(metric.GetLabel()) == len(labels)
}
