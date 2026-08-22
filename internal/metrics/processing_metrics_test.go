// Copyright 2026 ICAP Mock

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollector_ScenarioProcessingDurationUsesSummaryWithoutQuantiles(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	collector.RecordScenarioProcessingDurationForServer(
		"edge", "REQMOD", "application/json", OutcomeAllowed, "scan", "clean", 100*time.Millisecond,
	)
	collector.RecordScenarioProcessingDurationForServer(
		"edge", "REQMOD", "application/json", OutcomeAllowed, "scan", "clean", 300*time.Millisecond,
	)

	family := metricFamily(t, reg, "icap_scenario_processing_duration_seconds")
	if family.GetType() != dto.MetricType_SUMMARY {
		t.Fatalf("metric type = %v, want SUMMARY", family.GetType())
	}
	labels := requestMetricLabels("edge", "REQMOD", "application/json", "allowed", "clean", "scan")
	summary := summaryForLabels(t, family, labels)
	if summary.GetSampleCount() != 2 {
		t.Fatalf("sample count = %d, want 2", summary.GetSampleCount())
	}
	if got := summary.GetSampleSum(); got < 0.399 || got > 0.401 {
		t.Fatalf("sample sum = %v, want 0.4", got)
	}
	if len(summary.GetQuantile()) != 0 {
		t.Fatalf("quantiles = %v, want none", summary.GetQuantile())
	}
}

func TestHandlerWithRegistry_ProcessingDurationExposesOnlySumAndCount(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	collector.RecordScenarioProcessingDurationForServer(
		"edge", "REQMOD", "none", OutcomeAllowed, "scan", "clean", time.Second,
	)
	rr := httptest.NewRecorder()
	HandlerWithRegistry(reg).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body := rr.Body.String()

	if !strings.Contains(body, "icap_scenario_processing_duration_seconds_sum{") ||
		!strings.Contains(body, "icap_scenario_processing_duration_seconds_count{") {
		t.Fatalf("processing summary sum/count missing:\n%s", body)
	}
	if strings.Contains(body, "icap_scenario_processing_duration_seconds_bucket{") || strings.Contains(body, "quantile=") {
		t.Fatalf("processing summary unexpectedly exposes buckets or quantiles:\n%s", body)
	}
}

func TestCollector_ScenarioProcessingDurationCapsUniqueSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	for i := 0; i < maxScenarioLatencySeries+50; i++ {
		collector.RecordScenarioProcessingDurationForServer(
			"default",
			"REQMOD",
			ContentTypeNone,
			OutcomeAllowed,
			"scenario-"+strconv.Itoa(i),
			"response-"+strconv.Itoa(i),
			time.Millisecond,
		)
	}

	if got := countMetricSeries(t, reg, "icap_scenario_processing_duration_seconds"); got != maxScenarioLatencySeries {
		t.Fatalf("scenario processing series = %d, want %d", got, maxScenarioLatencySeries)
	}
}

func summaryForLabels(t *testing.T, family *dto.MetricFamily, labels map[string]string) *dto.Summary {
	t.Helper()
	for _, metric := range family.GetMetric() {
		if metricMatchesLabels(metric, labels) {
			if metric.GetSummary() == nil {
				t.Fatalf("metric %s with labels %v is not a summary", family.GetName(), labels)
			}
			return metric.GetSummary()
		}
	}
	t.Fatalf("summary %s with labels %v not found", family.GetName(), labels)
	return nil
}
