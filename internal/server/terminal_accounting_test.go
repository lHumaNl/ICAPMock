// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestTerminalAccountingRecordsIdenticalOutcomeExactlyOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	req := &icap.Request{Method: icap.MethodREQMOD}
	ctx := requestinfo.WithScenarioMetadata(context.Background())
	ctx = requestinfo.WithContentTypeLabel(ctx, metricsinternal.ContentTypeNone)
	requestinfo.SetScenarioMetadata(ctx, "scan", "clean")
	requestinfo.SetScenarioOutcome(ctx, metricsinternal.OutcomeBlocked)
	accounting := newTerminalAccounting(ctx, srv, req, time.Now().Add(-20*time.Millisecond))
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 1 {
		t.Fatalf("lifecycle in-flight before terminal accounting = %v, want 1", got)
	}

	accounting.streamingStarted()
	accounting.streamingStarted()
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 1 {
		t.Fatalf("active streams before terminal accounting = %v, want 1", got)
	}
	resp := icap.NewResponse(icap.StatusNoContentNeeded)
	accounting.finalize(resp, "")
	accounting.finalize(resp, metricsinternal.OutcomeError)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "none",
		"outcome": "blocked", "response": "clean", "scenario": "scan",
	}
	if got := gatheredCounterValue(t, reg, requestsTotalMetricName, labels); got != 1 {
		t.Errorf("terminal request count = %v, want 1", got)
	}
	if got := gatheredHistogramCount(t, reg, labels); got != 1 {
		t.Errorf("terminal latency count = %v, want 1", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
		t.Errorf("active streams after terminal accounting = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Errorf("lifecycle in-flight after terminal accounting = %v, want 0", got)
	}
}

func TestPendingTerminalAccountingRecordsRequestWithoutLatency(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	req := &icap.Request{Method: icap.MethodREQMOD}
	ctx := requestinfo.WithScenarioMetadata(context.Background())
	ctx = requestinfo.WithContentTypeLabel(ctx, metricsinternal.ContentTypeNone)
	requestinfo.SetScenarioMetadata(ctx, "scan", "preview-error")
	accounting := newPendingTerminalAccounting(ctx, srv, req)

	accounting.finalize(nil, metricsinternal.OutcomeError)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "none",
		"outcome": "error", "response": "preview-error", "scenario": "scan",
	}
	if got := gatheredCounterValue(t, reg, requestsTotalMetricName, labels); got != 1 {
		t.Fatalf("terminal request count = %v, want 1", got)
	}
	if got := gatheredHistogramCount(t, reg, labels); got != 0 {
		t.Fatalf("terminal latency count = %v, want 0", got)
	}
}

func TestTerminalAccountingKeepsCapturedCollectorAndLabels(t *testing.T) {
	regA := prometheus.NewRegistry()
	collectorA, err := metricsinternal.NewCollector(regA)
	if err != nil {
		t.Fatalf("NewCollector(A) error = %v", err)
	}
	regB := prometheus.NewRegistry()
	collectorB, err := metricsinternal.NewCollector(regB)
	if err != nil {
		t.Fatalf("NewCollector(B) error = %v", err)
	}
	srv := &ICAPServer{metricsServerName: "default"}
	srv.SetMetrics(collectorA)
	srv.SetMetricsServerName("edge-a")
	req := &icap.Request{Method: icap.MethodRESPMOD}
	accountingA := newPendingTerminalAccounting(context.Background(), srv, req)
	accountingA.streamingStarted()
	if got := gatheredNamedGaugeValue(t, regA, "icap_requests_in_flight", map[string]string{
		"server": "edge-a", "method": "RESPMOD",
	}); got != 1 {
		t.Fatalf("collector A lifecycle in-flight = %v, want 1", got)
	}
	if got := gatheredGaugeValue(t, regA, map[string]string{"server": "edge-a"}); got != 1 {
		t.Fatalf("collector A streaming active = %v, want 1", got)
	}

	srv.SetMetrics(collectorB)
	srv.SetMetricsServerName("edge-b")
	accountingA.finalize(nil, metricsinternal.OutcomeError)
	if got := gatheredNamedGaugeValue(t, regA, "icap_requests_in_flight", map[string]string{
		"server": "edge-a", "method": "RESPMOD",
	}); got != 0 {
		t.Fatalf("collector A lifecycle after finalization = %v, want 0", got)
	}
	if got := gatheredGaugeValue(t, regA, map[string]string{"server": "edge-a"}); got != 0 {
		t.Fatalf("collector A streaming after finalization = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, regB, "icap_requests_in_flight", map[string]string{
		"server": "edge-b", "method": "RESPMOD",
	}); got != 0 {
		t.Fatalf("collector B lifecycle before next request = %v, want 0", got)
	}

	accountingB := newPendingTerminalAccounting(context.Background(), srv, req)
	if got := gatheredNamedGaugeValue(t, regB, "icap_requests_in_flight", map[string]string{
		"server": "edge-b", "method": "RESPMOD",
	}); got != 1 {
		t.Fatalf("collector B lifecycle in-flight = %v, want 1", got)
	}
	accountingB.finalize(nil, metricsinternal.OutcomeError)
}

func TestTerminalAccountingStartedWithoutMetricsDoesNotDecrementLaterCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metricsServerName: "edge"}
	accounting := newPendingTerminalAccounting(
		context.Background(), srv, &icap.Request{Method: icap.MethodOPTIONS},
	)
	srv.SetMetrics(collector)
	accounting.finalize(nil, metricsinternal.OutcomeError)

	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Fatalf("late collector lifecycle in-flight = %v, want 0", got)
	}
}

func TestTerminalAccountingCollectorRemovalDoesNotLeakOriginalGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metricsServerName: "edge"}
	srv.SetMetrics(collector)
	accounting := newPendingTerminalAccounting(
		context.Background(), srv, &icap.Request{Method: icap.MethodREQMOD},
	)
	srv.SetMetrics(nil)
	accounting.finalize(nil, metricsinternal.OutcomeError)

	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Fatalf("removed collector lifecycle in-flight = %v, want 0", got)
	}
}

func TestTerminalAccountingConcurrentMetricsReplacementBalancesCapturedTuples(t *testing.T) {
	regA := prometheus.NewRegistry()
	collectorA, err := metricsinternal.NewCollector(regA)
	if err != nil {
		t.Fatalf("NewCollector(A) error = %v", err)
	}
	regB := prometheus.NewRegistry()
	collectorB, err := metricsinternal.NewCollector(regB)
	if err != nil {
		t.Fatalf("NewCollector(B) error = %v", err)
	}
	srv := &ICAPServer{metricsServerName: "edge-a"}
	srv.SetMetrics(collectorA)
	req := &icap.Request{Method: icap.MethodREQMOD}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				srv.SetMetrics(collectorA)
				srv.SetMetricsServerName("edge-a")
			} else {
				srv.SetMetrics(collectorB)
				srv.SetMetricsServerName("edge-b")
			}
		}
	}()
	for range 2 {
		go func() {
			defer wg.Done()
			for range 200 {
				accounting := newPendingTerminalAccounting(context.Background(), srv, req)
				accounting.finalize(nil, metricsinternal.OutcomeError)
			}
		}()
	}
	wg.Wait()

	assertAllLifecycleGaugeSeriesZero(t, regA)
	assertAllLifecycleGaugeSeriesZero(t, regB)
}

func TestTerminalAccountingConcurrentFinalizeDecrementsSameRequestExactlyOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	accounting := newPendingTerminalAccounting(
		context.Background(), srv, &icap.Request{Method: icap.MethodREQMOD},
	)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			accounting.finalize(icap.NewResponse(icap.StatusNoContentNeeded), "")
		}()
	}
	wg.Wait()

	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Fatalf("lifecycle in-flight after concurrent finalization = %v, want 0", got)
	}
}

func TestTerminalAccountingIsolatesREQMODAndRESPMODLifecycleGauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv := &ICAPServer{metrics: collector, metricsServerName: "edge"}
	reqmod := newPendingTerminalAccounting(
		context.Background(), srv, &icap.Request{Method: icap.MethodREQMOD},
	)
	respmod := newPendingTerminalAccounting(
		context.Background(), srv, &icap.Request{Method: icap.MethodRESPMOD},
	)

	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 1 {
		t.Fatalf("REQMOD lifecycle in-flight = %v, want 1", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "RESPMOD",
	}); got != 1 {
		t.Fatalf("RESPMOD lifecycle in-flight = %v, want 1", got)
	}
	reqmod.finalize(icap.NewResponse(icap.StatusNoContentNeeded), "")
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Fatalf("REQMOD lifecycle after finalization = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "RESPMOD",
	}); got != 1 {
		t.Fatalf("RESPMOD lifecycle after REQMOD finalization = %v, want 1", got)
	}
	respmod.finalize(icap.NewResponse(icap.StatusNoContentNeeded), "")
}

func TestEffectiveRequestOutcomeDefaultsPartialStreamsToBlocked(t *testing.T) {
	for _, mode := range []string{icap.StreamFinishFIN, icap.StreamFinishTerm} {
		t.Run(mode, func(t *testing.T) {
			resp := streamingTestResponse(t, mode, 0)
			if got := effectiveRequestOutcome(context.Background(), resp); got != metricsinternal.OutcomeBlocked {
				t.Fatalf("effectiveRequestOutcome() = %q, want blocked", got)
			}
		})
	}
}

func TestEffectiveRequestOutcomeAllowsExplicitPartialStreamOverride(t *testing.T) {
	ctx := requestinfo.WithScenarioMetadata(context.Background())
	requestinfo.SetScenarioOutcome(ctx, metricsinternal.OutcomeAllowed)
	resp := streamingTestResponse(t, icap.StreamFinishFIN, 0)

	if got := effectiveRequestOutcome(ctx, resp); got != metricsinternal.OutcomeAllowed {
		t.Fatalf("effectiveRequestOutcome() = %q, want allowed", got)
	}
}

func streamingTestResponse(t *testing.T, mode string, duration time.Duration) *icap.Response {
	t.Helper()
	plan, err := icap.PlanBodyStream(icap.BodyStreamPlanOptions{
		FinishMode: mode, SourceSize: 1, SelectedBytes: 1, SelectedBytesSet: true, Duration: duration,
	})
	if err != nil {
		t.Fatalf("PlanBodyStream() error = %v", err)
	}
	resp := icap.NewResponse(icap.StatusOK)
	resp.SetHTTPResponse(&icap.HTTPMessage{
		Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader(),
		BodyStream: &icap.BodyStream{Payload: icap.NewBytesStreamPayload([]byte("x")), Plan: plan},
	})
	if mode == icap.StreamFinishFIN {
		resp.MarkCloseAfterWrite()
	}
	return resp
}

func gatheredGaugeValue(t *testing.T, reg prometheus.Gatherer, labels map[string]string) float64 {
	return gatheredNamedGaugeValue(t, reg, "icap_streaming_active", labels)
}

func gatheredNamedGaugeValue(
	t *testing.T,
	reg prometheus.Gatherer,
	name string,
	labels map[string]string,
) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			for _, metric := range family.GetMetric() {
				if labelsMatch(metric, labels) {
					return metric.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

func assertAllLifecycleGaugeSeriesZero(t *testing.T, reg prometheus.Gatherer) {
	t.Helper()
	const name = "icap_requests_in_flight"
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if got := metric.GetGauge().GetValue(); got != 0 {
				t.Fatalf("metric %s has unbalanced value %v", name, got)
			}
		}
	}
}

func gatheredHistogramCount(t *testing.T, reg prometheus.Gatherer, labels map[string]string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() == "icap_scenario_response_duration_seconds" {
			for _, metric := range family.GetMetric() {
				if labelsMatch(metric, labels) {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}
