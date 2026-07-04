// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const requestsTotalMetricName = "icap_requests_total"

func TestRecordIncomingRequestUsesREQMODHTTPContentType(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}

	req := &icap.Request{Method: icap.MethodREQMOD, HTTPRequest: &icap.HTTPMessage{Header: icap.NewHeader()}}
	req.HTTPRequest.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	resp := icap.NewResponse(icap.StatusOK)
	resp.HTTPResponse = &icap.HTTPMessage{Status: "403"}
	srv.recordIncomingRequest(context.Background(), req, resp)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "application/json", "outcome": "blocked",
		"response": "200", "scenario": metricsinternal.NoScenarioMetricLabel,
	}
	if got := counterValue(t, reg, labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestNilResponseIsError(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}

	req := &icap.Request{Method: icap.MethodRESPMOD, HTTPResponse: &icap.HTTPMessage{Header: icap.NewHeader()}}
	req.HTTPResponse.Header.Set("Content-Type", "Text/HTML")
	srv.recordIncomingRequest(context.Background(), req, nil)

	labels := map[string]string{
		"server": "edge", "method": "RESPMOD", "content_type": "text/html", "outcome": "error",
		"response": "error", "scenario": metricsinternal.NoScenarioMetricLabel,
	}
	if got := counterValue(t, reg, labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestOptionsContentTypeNone(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}

	req := &icap.Request{Method: icap.MethodOPTIONS}
	resp := icap.NewResponse(icap.StatusOK)
	srv.recordIncomingRequest(context.Background(), req, resp)

	labels := map[string]string{
		"server": "edge", "method": "OPTIONS", "content_type": "none", "outcome": "allowed",
		"response": "200", "scenario": metricsinternal.NoScenarioMetricLabel,
	}
	if got := counterValue(t, reg, labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestUsesRequestScopedContentTypeLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	req := &icap.Request{Method: icap.MethodREQMOD, HTTPRequest: &icap.HTTPMessage{Header: icap.NewHeader()}}
	req.HTTPRequest.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	ctx := requestinfo.WithContentTypeLabel(context.Background(), metricsinternal.ContentTypeOther)

	srv.recordIncomingRequest(ctx, req, icap.NewResponse(icap.StatusOK))

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "other", "outcome": "allowed",
		"response": "200", "scenario": metricsinternal.NoScenarioMetricLabel,
	}
	if got := counterValue(t, reg, labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestRecordIncomingRequestUsesRequestScopedScenarioMetadata(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	req := &icap.Request{Method: icap.MethodREQMOD, HTTPRequest: &icap.HTTPMessage{Header: icap.NewHeader()}}
	ctx := requestinfo.WithScenarioMetadata(context.Background())
	requestinfo.SetScenarioMetadata(ctx, "scan", "blocked-page")

	srv.recordIncomingRequest(ctx, req, icap.NewResponse(icap.StatusOK))

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "none", "outcome": "allowed",
		"response": "blocked-page", "scenario": "scan",
	}
	if got := counterValue(t, reg, labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func counterValue(t *testing.T, reg prometheus.Gatherer, labels map[string]string) float64 {
	t.Helper()
	for _, metric := range metricFamily(t, reg, requestsTotalMetricName).GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetCounter().GetValue()
		}
	}
	t.Fatalf("metric %s with labels %v not found", requestsTotalMetricName, labels)
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
