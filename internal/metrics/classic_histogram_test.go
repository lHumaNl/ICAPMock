// Copyright 2026 ICAP Mock

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollector_ScenarioResponseDurationUsesClassicBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequestForServerWithBlock("edge", "scan", "clean", false, 100*time.Millisecond)

	labels := requestMetricLabels("edge", "unknown", "none", "allowed", "clean", "scan")
	histogram := histogramForLabels(t, reg, "icap_scenario_response_duration_seconds", labels)
	assertScenarioBucketLayout(t, histogram.GetBucket())
}

func assertScenarioBucketLayout(t *testing.T, buckets []*dto.Bucket) {
	t.Helper()
	if len(buckets) != len(scenarioResponseDurationBuckets) {
		t.Fatalf("bucket count = %d, want %d", len(buckets), len(scenarioResponseDurationBuckets))
	}
	for i, want := range scenarioResponseDurationBuckets {
		if got := buckets[i].GetUpperBound(); got != want {
			t.Fatalf("bucket[%d] upper bound = %v, want %v", i, got, want)
		}
	}
	for _, want := range []float64{0.1, 1, 2, 5, 10, 60, 120} {
		if !hasBucketUpperBound(buckets, want) {
			t.Fatalf("missing bucket upper bound %v", want)
		}
	}
}

func hasBucketUpperBound(buckets []*dto.Bucket, want float64) bool {
	for _, bucket := range buckets {
		if bucket.GetUpperBound() == want {
			return true
		}
	}
	return false
}

func TestHandlerWithRegistryTextShowsClassicBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequestForServerWithBlock("edge", "scan", "clean", false, 100*time.Millisecond)

	rr := httptest.NewRecorder()
	HandlerWithRegistry(reg).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))

	bucketLines, hasInfBucket := scenarioBucketTextStats(rr.Body.String())
	if !hasInfBucket {
		t.Fatalf("text exposition did not include +Inf bucket:\n%s", rr.Body.String())
	}
	if finiteBucketCount := bucketLines - 1; finiteBucketCount != len(scenarioResponseDurationBuckets) {
		t.Fatalf("text exposition finite bucket count = %d, want %d", finiteBucketCount, len(scenarioResponseDurationBuckets))
	}
}

func scenarioBucketTextStats(body string) (int, bool) {
	bucketLines := 0
	hasInfBucket := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "icap_scenario_response_duration_seconds_bucket{") {
			continue
		}
		bucketLines++
		hasInfBucket = hasInfBucket || strings.Contains(line, `le="+Inf"`)
	}
	return bucketLines, hasInfBucket
}

func histogramForLabels(
	t *testing.T,
	reg prometheus.Gatherer,
	name string,
	labels map[string]string,
) *dto.Histogram {
	t.Helper()
	return histogramForLabelsInFamily(t, metricFamily(t, reg, name), labels)
}

func histogramForLabelsInFamily(t *testing.T, family *dto.MetricFamily, labels map[string]string) *dto.Histogram {
	t.Helper()
	for _, metric := range family.GetMetric() {
		if metricMatchesLabels(metric, labels) {
			histogram := metric.GetHistogram()
			if histogram == nil {
				t.Fatalf("metric %s with labels %v is not a histogram", family.GetName(), labels)
			}
			return histogram
		}
	}
	t.Fatalf("histogram %s with labels %v not found", family.GetName(), labels)
	return nil
}
