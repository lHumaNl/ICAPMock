// Copyright 2026 ICAP Mock

package metrics

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

// getHistogramCount returns the count of observations from a histogram metric.
func getHistogramCount(reg prometheus.Gatherer, metricName string, labels ...string) uint64 {
	mfs, err := reg.Gather()
	if err != nil {
		return 0
	}
	for _, mf := range mfs {
		if mf.GetName() == metricName {
			for _, m := range mf.GetMetric() {
				if len(labels) == 0 {
					return m.GetHistogram().GetSampleCount()
				}
				match := true
				for i, l := range m.GetLabel() {
					if i < len(labels) && l.GetValue() != labels[i] {
						match = false
						break
					}
				}
				if match && len(m.GetLabel()) > 0 {
					return m.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

func assertHistogramCount(
	t *testing.T,
	reg prometheus.Gatherer,
	name string,
	labels map[string]string,
	want uint64,
) {
	t.Helper()
	for _, metric := range metricFamily(t, reg, name).GetMetric() {
		if metricMatchesLabels(metric, labels) && metric.GetHistogram().GetSampleCount() == want {
			return
		}
	}
	t.Fatalf("histogram %s with labels %v and count %d not found", name, labels, want)
}

func assertScenarioLatencyStat(
	t *testing.T,
	collector *Collector,
	stat string,
	want float64,
) {
	t.Helper()
	got := testutil.ToFloat64(collector.scenarioResponseTime.WithLabelValues("default", "scan", "clean", stat))
	if math.Abs(got-want) > 0.0000001 {
		t.Errorf("scenario latency %s = %v, want %v", stat, got, want)
	}
}

func assertMetricLabels(t *testing.T, reg prometheus.Gatherer, name string, labels []string) {
	t.Helper()
	metric := firstMetric(t, reg, name)
	if len(metric.GetLabel()) != len(labels) {
		t.Fatalf("%s label count = %d, want %d", name, len(metric.GetLabel()), len(labels))
	}
	for i, label := range labels {
		if metric.GetLabel()[i].GetName() != label {
			t.Errorf("%s label[%d] = %s, want %s", name, i, metric.GetLabel()[i].GetName(), label)
		}
	}
}

func assertNoMetric(t *testing.T, reg prometheus.Gatherer, name string) {
	t.Helper()
	for _, mf := range gatherMetricFamilies(t, reg) {
		if mf.GetName() == name {
			t.Fatalf("metric %s exists, want absent", name)
		}
	}
}

func countMetricSeries(t *testing.T, reg prometheus.Gatherer, name string) int {
	t.Helper()
	for _, mf := range gatherMetricFamilies(t, reg) {
		if mf.GetName() == name {
			return len(mf.GetMetric())
		}
	}
	return 0
}

func hasMetricLabels(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) bool {
	t.Helper()
	for _, metric := range metricFamily(t, reg, name).GetMetric() {
		if metricMatchesLabels(metric, labels) {
			return true
		}
	}
	return false
}

func metricFamily(t *testing.T, reg prometheus.Gatherer, name string) *dto.MetricFamily {
	t.Helper()
	for _, mf := range gatherMetricFamilies(t, reg) {
		if mf.GetName() == name {
			return mf
		}
	}
	t.Fatalf("metric %s not found", name)
	return nil
}

func metricValue(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	for _, metric := range metricFamily(t, reg, name).GetMetric() {
		if metricMatchesLabels(metric, labels) {
			return metricSampleValue(t, metric)
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
	return 0
}

func metricSampleValue(t *testing.T, metric *dto.Metric) float64 {
	t.Helper()
	if metric.Counter != nil {
		return metric.Counter.GetValue()
	}
	if metric.Gauge != nil {
		return metric.Gauge.GetValue()
	}
	t.Fatal("metric has neither counter nor gauge value")
	return 0
}

func metricMatchesLabels(metric *dto.Metric, labels map[string]string) bool {
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return len(metric.GetLabel()) == len(labels)
}

func sumCounterMetric(t *testing.T, reg prometheus.Gatherer, name string) float64 {
	t.Helper()
	var total float64
	for _, metric := range metricFamily(t, reg, name).GetMetric() {
		total += metric.GetCounter().GetValue()
	}
	return total
}

func firstMetric(t *testing.T, reg prometheus.Gatherer, name string) *dto.Metric {
	t.Helper()
	for _, mf := range gatherMetricFamilies(t, reg) {
		if mf.GetName() == name && len(mf.GetMetric()) > 0 {
			return mf.GetMetric()[0]
		}
	}
	t.Fatalf("metric %s not found", name)
	return nil
}

func gatherMetricFamilies(t *testing.T, reg prometheus.Gatherer) []*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	return mfs
}

// TestNewCollector tests that a new collector can be created.
func TestNewCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v, want nil", err)
	}
	if collector == nil {
		t.Fatal("NewCollector() returned nil collector")
	}
}

// TestNewCollector_NilRegistry tests that nil registry returns error.
func TestNewCollector_NilRegistry(t *testing.T) {
	_, err := NewCollector(nil)
	if err == nil {
		t.Error("NewCollector(nil) should return error")
	}
}

// TestCollector_RecordRequest tests request counter recording.
func TestCollector_RecordRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Record requests for different methods
	collector.RecordRequest("REQMOD")
	collector.RecordRequest("REQMOD")
	collector.RecordRequest("RESPMOD")

	// Verify counter increased
	countReqmod := testutil.ToFloat64(collector.requestsTotal.WithLabelValues("default", "REQMOD"))
	countRespmod := testutil.ToFloat64(collector.requestsTotal.WithLabelValues("default", "RESPMOD"))

	if countReqmod != 2 {
		t.Errorf("REQMOD count = %v, want 2", countReqmod)
	}
	if countRespmod != 1 {
		t.Errorf("RESPMOD count = %v, want 1", countRespmod)
	}
}

// TestCollector_RecordRequestDuration tests request duration histogram recording.
func TestCollector_RecordRequestDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Record durations
	collector.RecordRequestDuration("REQMOD", 100*time.Millisecond)
	collector.RecordRequestDuration("REQMOD", 200*time.Millisecond)
	collector.RecordRequestDuration("RESPMOD", 150*time.Millisecond)

	// Verify histogram has recordings
	countReqmod := getHistogramCount(reg, "icap_request_duration_seconds", "REQMOD")
	if countReqmod != 2 {
		t.Errorf("REQMOD duration count = %v, want 2", countReqmod)
	}
}

// TestCollector_RequestsInFlight tests in-flight request gauge.
func TestCollector_RequestsInFlight(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Test increment
	collector.IncRequestsInFlight("REQMOD")
	collector.IncRequestsInFlight("REQMOD")
	collector.IncRequestsInFlight("RESPMOD")

	countReqmod := testutil.ToFloat64(collector.requestsInFlight.WithLabelValues("default", "REQMOD"))
	countRespmod := testutil.ToFloat64(collector.requestsInFlight.WithLabelValues("default", "RESPMOD"))

	if countReqmod != 2 {
		t.Errorf("REQMOD in-flight = %v, want 2", countReqmod)
	}
	if countRespmod != 1 {
		t.Errorf("RESPMOD in-flight = %v, want 1", countRespmod)
	}

	// Test decrement
	collector.DecRequestsInFlight("REQMOD")
	countReqmod = testutil.ToFloat64(collector.requestsInFlight.WithLabelValues("default", "REQMOD"))
	if countReqmod != 1 {
		t.Errorf("REQMOD in-flight after decrement = %v, want 1", countReqmod)
	}
}

// TestCollector_RecordRequestSize tests request size histogram recording.
func TestCollector_RecordRequestSize(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordRequestSize("REQMOD", 1024)
	collector.RecordRequestSize("REQMOD", 2048)
	collector.RecordRequestSize("RESPMOD", 512)

	countReqmod := getHistogramCount(reg, "icap_request_size_bytes", "REQMOD")
	if countReqmod != 2 {
		t.Errorf("REQMOD request size count = %v, want 2", countReqmod)
	}
}

// TestCollector_RecordResponseSize tests response size histogram recording.
func TestCollector_RecordResponseSize(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordResponseSize("REQMOD", 4096)
	collector.RecordResponseSize("REQMOD", 8192)

	countReqmod := getHistogramCount(reg, "icap_response_size_bytes", "REQMOD")
	if countReqmod != 2 {
		t.Errorf("REQMOD response size count = %v, want 2", countReqmod)
	}
}

// TestCollector_RecordError tests error counter recording.
func TestCollector_RecordError(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordError("timeout")
	collector.RecordError("timeout")
	collector.RecordError("connection_error")

	countTimeout := testutil.ToFloat64(collector.errorsTotal.WithLabelValues("default", "timeout"))
	countConnErr := testutil.ToFloat64(collector.errorsTotal.WithLabelValues("default", "connection_error"))

	if countTimeout != 2 {
		t.Errorf("timeout error count = %v, want 2", countTimeout)
	}
	if countConnErr != 1 {
		t.Errorf("connection_error count = %v, want 1", countConnErr)
	}
}

// TestCollector_ActiveConnections tests active connections gauge.
func TestCollector_ActiveConnections(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Increment
	collector.IncActiveConnections()
	collector.IncActiveConnections()
	collector.IncActiveConnections()

	count := testutil.ToFloat64(collector.activeConnections)
	if count != 3 {
		t.Errorf("active connections = %v, want 3", count)
	}

	// Decrement
	collector.DecActiveConnections()
	count = testutil.ToFloat64(collector.activeConnections)
	if count != 2 {
		t.Errorf("active connections after decrement = %v, want 2", count)
	}
}

// TestCollector_RecordScenarioMatched tests scenario counter recording.
func TestCollector_RecordScenarioMatched(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioMatched("virus_scan")
	collector.RecordScenarioMatched("virus_scan")
	collector.RecordScenarioMatched("url_filter")

	countVirus := testutil.ToFloat64(collector.scenariosMatched.WithLabelValues("default", "virus_scan"))
	countFilter := testutil.ToFloat64(collector.scenariosMatched.WithLabelValues("default", "url_filter"))

	if countVirus != 2 {
		t.Errorf("virus_scan scenario count = %v, want 2", countVirus)
	}
	if countFilter != 1 {
		t.Errorf("url_filter scenario count = %v, want 1", countFilter)
	}
}

func TestCollector_RecordScenarioRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequest("virus_scan", "blocked", 100*time.Millisecond)
	collector.RecordScenarioRequest("virus_scan", "blocked", 200*time.Millisecond)
	collector.RecordScenarioRequest("virus_scan", "204", 50*time.Millisecond)

	blocked := testutil.ToFloat64(collector.scenarioRequests.WithLabelValues("default", "virus_scan", "blocked"))
	noContent := testutil.ToFloat64(collector.scenarioRequests.WithLabelValues("default", "virus_scan", "204"))
	if blocked != 2 {
		t.Errorf("blocked scenario requests = %v, want 2", blocked)
	}
	if noContent != 1 {
		t.Errorf("204 scenario requests = %v, want 1", noContent)
	}
	assertMetricLabels(t, reg, "icap_scenario_requests_total", []string{"response", "scenario", "server"})
}

func TestCollector_RecordFallbackScenarioRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordFallbackScenarioRequest("server-a", "204", time.Millisecond)
	labels := map[string]string{"server": "server-a", "scenario": "fallback", "response": "204"}
	if got := metricValue(t, reg, "icap_scenario_requests_total", labels); got != 1 {
		t.Errorf("fallback scenario requests = %v, want 1", got)
	}
}

func TestCollector_RecordAPIMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordAPIRequest("management", "/api/v1/config/load", "POST", http.StatusBadRequest)
	collector.RecordAPIError("management", "/api/v1/config/load", "POST", http.StatusBadRequest, "bad_request")
	reqLabels := map[string]string{"server": "management", "route": "/api/v1/config/load", "method": "POST", "status_code": "400"}
	errLabels := map[string]string{"server": "management", "route": "/api/v1/config/load", "method": "POST", "status_code": "400", "error_type": "bad_request"}
	if got := metricValue(t, reg, "icap_api_requests_total", reqLabels); got != 1 {
		t.Errorf("api requests = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_api_errors_total", errLabels); got != 1 {
		t.Errorf("api errors = %v, want 1", got)
	}
}

func TestCollector_SetScenariosLoaded(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.SetScenariosLoaded("server-a", 7)
	if got := metricValue(t, reg, "icap_scenarios_loaded", map[string]string{"server": "server-a"}); got != 7 {
		t.Errorf("scenarios loaded = %v, want 7", got)
	}
}

func TestCollector_SetScenariosLoadedSnapshotDeletesRemovedServers(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.SetScenariosLoadedSnapshot(map[string]int{"server-a": 3, "server-b": 5})
	collector.SetScenariosLoadedSnapshot(map[string]int{"server-a": 4})

	if got := metricValue(t, reg, "icap_scenarios_loaded", map[string]string{"server": "server-a"}); got != 4 {
		t.Errorf("server-a scenarios loaded = %v, want 4", got)
	}
	if hasMetricLabels(t, reg, "icap_scenarios_loaded", map[string]string{"server": "server-b"}) {
		t.Error("server-b scenarios_loaded series is still present after snapshot removal")
	}
}

func TestCollector_RecordScenarioRequestLatencyStats(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	for i := 1; i <= 100; i++ {
		collector.RecordScenarioRequest("scan", "clean", time.Duration(i)*time.Millisecond)
	}

	assertScenarioLatencyStat(t, collector, "min", 0.001)
	assertScenarioLatencyStat(t, collector, "max", 0.100)
	assertScenarioLatencyStat(t, collector, "avg", 0.0505)
	assertScenarioLatencyStat(t, collector, "p50", 0.050)
	assertScenarioLatencyStat(t, collector, "p75", 0.075)
	assertScenarioLatencyStat(t, collector, "p90", 0.090)
	assertScenarioLatencyStat(t, collector, "p92", 0.092)
	assertScenarioLatencyStat(t, collector, "p95", 0.095)
	assertNoMetric(t, reg, "icap_scenario_response_time_seconds_bucket")
}

func TestCollector_RecordScenarioRequestCapsUniqueSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	for i := 0; i < maxScenarioLatencySeries+50; i++ {
		collector.RecordScenarioRequest(
			"scenario-"+strconv.Itoa(i),
			"response-"+strconv.Itoa(i),
			time.Millisecond,
		)
	}

	wantPairs := maxScenarioLatencySeries
	gotRequests := countMetricSeries(t, reg, "icap_scenario_requests_total")
	if gotRequests != wantPairs {
		t.Errorf("scenario request series = %d, want %d", gotRequests, wantPairs)
	}
	gotLatency := countMetricSeries(t, reg, "icap_scenario_response_time_seconds")
	wantLatency := wantPairs * len(scenarioLatencyStatNames)
	if gotLatency != wantLatency {
		t.Errorf("scenario latency series = %d, want %d", gotLatency, wantLatency)
	}
}

func TestCollector_RecordScenarioRequestOverflowIsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	fillScenarioMetricCapacity(collector)
	collector.RecordScenarioRequest("overflow-a", "blocked", time.Millisecond)
	collector.RecordScenarioRequest("overflow-b", "allowed", 2*time.Millisecond)
	collector.RecordScenarioRequest("overflow-c", "other", 3*time.Millisecond)

	labels := map[string]string{"server": overflowMetricLabel, "scenario": overflowMetricLabel, "response": overflowMetricLabel}
	if got := metricValue(t, reg, "icap_scenario_requests_total", labels); got != 3 {
		t.Errorf("overflow scenario requests = %v, want 3", got)
	}
	if hasMetricLabels(t, reg, "icap_scenario_requests_total", overflowSourceLabels()) {
		t.Fatal("overflow source labels created a request series")
	}
	if got := countMetricSeries(t, reg, "icap_scenario_requests_total"); got != maxScenarioLatencySeries {
		t.Errorf("scenario request series = %d, want %d", got, maxScenarioLatencySeries)
	}
}

func TestCollector_RecordScenarioRequestEscapesReservedUserLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequest(overflowMetricLabel, overflowMetricLabel, time.Millisecond)

	escapedLabels := escapedOverflowLabels()
	if got := metricValue(t, reg, "icap_scenario_requests_total", escapedLabels); got != 1 {
		t.Errorf("escaped reserved scenario requests = %v, want 1", got)
	}
	if hasMetricLabels(t, reg, "icap_scenario_requests_total", overflowLabels()) {
		t.Fatal("reserved user labels created the overflow aggregate series")
	}
	matchedLabels := map[string]string{"server": "default", "scenario": escapedOverflowMetricLabel()}
	if got := metricValue(t, reg, "icap_scenarios_matched_total", matchedLabels); got != 1 {
		t.Errorf("escaped reserved scenarios matched = %v, want 1", got)
	}
}

func TestCollector_RecordScenarioRequestSeparatesReservedUserLabelsFromOverflow(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequest(overflowMetricLabel, overflowMetricLabel, time.Millisecond)
	fillScenarioMetricCapacityAfterReserved(collector)
	collector.RecordScenarioRequest("overflow-a", "blocked", 2*time.Millisecond)

	if got := metricValue(t, reg, "icap_scenario_requests_total", escapedOverflowLabels()); got != 1 {
		t.Errorf("escaped reserved scenario requests = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_scenario_requests_total", overflowLabels()); got != 1 {
		t.Errorf("overflow aggregate scenario requests = %v, want 1", got)
	}
	if got := countMetricSeries(t, reg, "icap_scenario_requests_total"); got != maxScenarioLatencySeries {
		t.Errorf("scenario request series = %d, want %d", got, maxScenarioLatencySeries)
	}
}

func TestCollector_RecordScenarioRequestConcurrent(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	const workers = 16
	const iterations = 128
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go recordScenarioRequestsWorker(&wg, start, collector, worker, iterations)
	}
	close(start)
	wg.Wait()

	if got := countMetricSeries(t, reg, "icap_scenario_requests_total"); got > maxScenarioLatencySeries {
		t.Errorf("scenario request series = %d, want <= %d", got, maxScenarioLatencySeries)
	}
	if got := sumCounterMetric(t, reg, "icap_scenario_requests_total"); got != float64(workers*iterations) {
		t.Errorf("scenario request count = %v, want %d", got, workers*iterations)
	}
}

func fillScenarioMetricCapacity(collector *Collector) {
	for i := 0; i < maxScenarioLatencySeries-1; i++ {
		collector.RecordScenarioRequest("scenario-"+strconv.Itoa(i), "response", time.Millisecond)
	}
}

func fillScenarioMetricCapacityAfterReserved(collector *Collector) {
	for i := 0; i < maxScenarioLatencySeries-2; i++ {
		collector.RecordScenarioRequest("scenario-"+strconv.Itoa(i), "response", time.Millisecond)
	}
}

func overflowLabels() map[string]string {
	return map[string]string{"server": overflowMetricLabel, "scenario": overflowMetricLabel, "response": overflowMetricLabel}
}

func escapedOverflowLabels() map[string]string {
	escaped := escapedOverflowMetricLabel()
	return map[string]string{"server": "default", "scenario": escaped, "response": escaped}
}

func escapedOverflowMetricLabel() string {
	return userMetricLabelEscapePrefix + overflowMetricLabel
}

func overflowSourceLabels() map[string]string {
	return map[string]string{"server": "default", "scenario": "overflow-a", "response": "blocked"}
}

func recordScenarioRequestsWorker(
	wg *sync.WaitGroup,
	start <-chan struct{},
	collector *Collector,
	worker int,
	iterations int,
) {
	defer wg.Done()
	<-start
	for i := 0; i < iterations; i++ {
		collector.RecordScenarioRequest(
			"scenario-"+strconv.Itoa(i),
			"response-"+strconv.Itoa(worker),
			time.Duration(i+1)*time.Microsecond,
		)
	}
}

// TestCollector_RecordChaosInjected tests chaos injection counter recording.
func TestCollector_RecordChaosInjected(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordChaosInjected("latency")
	collector.RecordChaosInjected("latency")
	collector.RecordChaosInjected("error")

	countLatency := testutil.ToFloat64(collector.chaosInjected.WithLabelValues("latency"))
	countError := testutil.ToFloat64(collector.chaosInjected.WithLabelValues("error"))

	if countLatency != 2 {
		t.Errorf("latency chaos count = %v, want 2", countLatency)
	}
	if countError != 1 {
		t.Errorf("error chaos count = %v, want 1", countError)
	}
}

// TestCollector_RecordRateLimitExceeded tests rate limit counter recording.
func TestCollector_RecordRateLimitExceeded(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordRateLimitExceeded("client_a")
	collector.RecordRateLimitExceeded("client_a")
	collector.RecordRateLimitExceeded("client_b")

	count := testutil.ToFloat64(collector.rateLimitExceeded.WithLabelValues("default"))
	if count != 3 {
		t.Errorf("rate limit count = %v, want 3", count)
	}
}

// TestCollector_RecordRateLimitWaitTime tests rate limit wait time histogram recording.
func TestCollector_RecordRateLimitWaitTime(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordRateLimitWaitTime("client_a", 50*time.Millisecond)
	collector.RecordRateLimitWaitTime("client_a", 100*time.Millisecond)

	countA := getHistogramCount(reg, "icap_rate_limit_wait_seconds", "default")
	if countA != 2 {
		t.Errorf("client_a wait time count = %v, want 2", countA)
	}
}

func TestCollector_RecordRateLimitForServer(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordRateLimitExceededForServer("edge-a")
	collector.RecordRateLimitWaitTimeForServer("edge-a", 50*time.Millisecond)
	collector.RecordPerClientRateLimitExceededForServer("edge-a")
	collector.RecordPerClientRateLimitWaitTimeForServer("edge-a", 50*time.Millisecond)
	collector.SetPerClientRateLimitActiveForServer("edge-a", 2)
	collector.IncPerClientRateLimitEvictionsForServer("edge-a")

	if got := metricValue(t, reg, "icap_rate_limit_exceeded_total", map[string]string{"server": "edge-a"}); got != 1 {
		t.Errorf("rate limit exceeded = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_per_client_rate_limit_exceeded_total", map[string]string{"server": "edge-a"}); got != 1 {
		t.Errorf("per-client exceeded = %v, want 1", got)
	}
	if got := metricValue(t, reg, "icap_per_client_rate_limit_active_clients", map[string]string{"server": "edge-a"}); got != 2 {
		t.Errorf("per-client active clients = %v, want 2", got)
	}
	if got := metricValue(t, reg, "icap_per_client_rate_limit_evictions_total", map[string]string{"server": "edge-a"}); got != 1 {
		t.Errorf("per-client evictions = %v, want 1", got)
	}
	assertHistogramCount(t, reg, "icap_rate_limit_wait_seconds", map[string]string{"server": "edge-a"}, 1)
	assertHistogramCount(t, reg, "icap_per_client_rate_limit_wait_seconds", map[string]string{"server": "edge-a"}, 1)
}

// TestCollector_RecordReplayRequest tests replay request counter recording.
func TestCollector_RecordReplayRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordReplayRequest()
	collector.RecordReplayRequest()
	collector.RecordReplayRequest()

	count := testutil.ToFloat64(collector.replayRequestsTotal)
	if count != 3 {
		t.Errorf("replay requests count = %v, want 3", count)
	}
}

// TestCollector_RecordReplayFailure tests replay failure counter recording.
func TestCollector_RecordReplayFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordReplayFailure()
	collector.RecordReplayFailure()

	count := testutil.ToFloat64(collector.replayRequestsFailed)
	if count != 2 {
		t.Errorf("replay failures count = %v, want 2", count)
	}
}

// TestCollector_RecordReplayDuration tests replay duration histogram recording.
func TestCollector_RecordReplayDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordReplayDuration(150 * time.Millisecond)
	collector.RecordReplayDuration(250 * time.Millisecond)

	count := getHistogramCount(reg, "icap_replay_duration_seconds")
	if count != 2 {
		t.Errorf("replay duration count = %v, want 2", count)
	}
}

// TestCollector_SetReplayBehindOriginal tests replay behind gauge setting.
func TestCollector_SetReplayBehindOriginal(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.SetReplayBehindOriginal(5.2)
	count := testutil.ToFloat64(collector.replayBehindOriginal)
	if count != 5.2 {
		t.Errorf("replay behind original = %v, want 5.2", count)
	}

	collector.SetReplayBehindOriginal(0)
	count = testutil.ToFloat64(collector.replayBehindOriginal)
	if count != 0 {
		t.Errorf("replay behind original = %v, want 0", count)
	}
}

// TestCollector_StreamingActive tests streaming active gauge.
func TestCollector_StreamingActive(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.IncStreamingActive()
	collector.IncStreamingActive()

	count := testutil.ToFloat64(collector.streamingActive)
	if count != 2 {
		t.Errorf("streaming active = %v, want 2", count)
	}

	collector.DecStreamingActive()
	count = testutil.ToFloat64(collector.streamingActive)
	if count != 1 {
		t.Errorf("streaming active after decrement = %v, want 1", count)
	}
}

// TestCollector_RecordStreamingBytes tests streaming bytes counter recording.
func TestCollector_RecordStreamingBytes(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordStreamingBytes("in", 1024)
	collector.RecordStreamingBytes("in", 2048)
	collector.RecordStreamingBytes("out", 512)

	countIn := testutil.ToFloat64(collector.streamingBytesTotal.WithLabelValues("in"))
	countOut := testutil.ToFloat64(collector.streamingBytesTotal.WithLabelValues("out"))

	if countIn != 3072 {
		t.Errorf("streaming bytes in = %v, want 3072", countIn)
	}
	if countOut != 512 {
		t.Errorf("streaming bytes out = %v, want 512", countOut)
	}
}

// TestCollector_RecordConfigReload tests config reload counter recording.
func TestCollector_RecordConfigReload(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordConfigReload("success")
	collector.RecordConfigReload("success")
	collector.RecordConfigReload("failure")

	countSuccess := testutil.ToFloat64(collector.configReloadTotal.WithLabelValues("success"))
	countFailure := testutil.ToFloat64(collector.configReloadTotal.WithLabelValues("failure"))

	if countSuccess != 2 {
		t.Errorf("config reload success count = %v, want 2", countSuccess)
	}
	if countFailure != 1 {
		t.Errorf("config reload failure count = %v, want 1", countFailure)
	}
}

// TestCollector_RecordConfigReloadDuration tests config reload duration histogram recording.
func TestCollector_RecordConfigReloadDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordConfigReloadDuration(50 * time.Millisecond)
	collector.RecordConfigReloadDuration(150 * time.Millisecond)

	count := getHistogramCount(reg, "icap_config_reload_duration_seconds")
	if count != 2 {
		t.Errorf("config reload duration count = %v, want 2", count)
	}
}

// TestCollector_SetConfigLastReloadStatus tests config last reload status gauge.
func TestCollector_SetConfigLastReloadStatus(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Test success status
	collector.SetConfigLastReloadStatus(true)
	count := testutil.ToFloat64(collector.configLastReloadStatus)
	if count != 1 {
		t.Errorf("config last reload status = %v, want 1", count)
	}

	// Test failure status
	collector.SetConfigLastReloadStatus(false)
	count = testutil.ToFloat64(collector.configLastReloadStatus)
	if count != 0 {
		t.Errorf("config last reload status = %v, want 0", count)
	}
}

// TestHandler tests that Handler returns a valid HTTP handler.
func TestHandler(t *testing.T) {
	handler := Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should return 200 OK
	if rec.Code != http.StatusOK {
		t.Errorf("Handler() status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Should contain prometheus metrics output
	body := rec.Body.String()
	if !strings.Contains(body, "# HELP") && !strings.Contains(body, "# TYPE") {
		t.Error("Handler() response doesn't contain Prometheus metrics format")
	}
}

// TestHandlerWithRegistry tests that HandlerWithRegistry returns a valid HTTP handler.
func TestHandlerWithRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	handler := HandlerWithRegistry(reg)
	if handler == nil {
		t.Fatal("HandlerWithRegistry() returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HandlerWithRegistry() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHandlerWithRegistry_NilRegistry tests that nil registry returns a handler with default registry.
func TestHandlerWithRegistry_NilRegistry(t *testing.T) {
	handler := HandlerWithRegistry(nil)
	if handler == nil {
		t.Fatal("HandlerWithRegistry(nil) returned nil")
	}
}

// TestCollector_MetricNames tests that all expected metric names are registered.
func TestCollector_MetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Initialize all metrics with at least one data point
	// This is required because Prometheus doesn't expose labeled metrics until they have data
	collector.RecordRequest("REQMOD")
	collector.RecordRequestDuration("REQMOD", time.Millisecond)
	collector.IncRequestsInFlight("REQMOD")
	collector.DecRequestsInFlight("REQMOD")
	collector.RecordRequestSize("REQMOD", 100)
	collector.RecordResponseSize("REQMOD", 100)
	collector.RecordError("test")
	collector.IncActiveConnections()
	collector.SetGoroutines(1)
	collector.RecordScenarioMatched("test")
	collector.RecordScenarioRequest("test", "204", time.Millisecond)
	collector.RecordChaosInjected("test")
	collector.RecordRateLimitExceeded("test")
	collector.RecordRateLimitWaitTime("test", time.Millisecond)
	collector.RecordReplayRequest()
	collector.RecordReplayFailure()
	collector.RecordReplayDuration(time.Millisecond)
	collector.SetReplayBehindOriginal(0)
	collector.IncStreamingActive()
	collector.RecordStreamingBytes("in", 1)
	collector.RecordConfigReload("success")
	collector.RecordConfigReloadDuration(time.Millisecond)
	collector.SetConfigLastReloadStatus(true)

	// Gather metrics
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	expectedMetrics := []string{
		"icap_requests_total",
		"icap_request_duration_seconds",
		"icap_requests_in_flight",
		"icap_request_size_bytes",
		"icap_response_size_bytes",
		"icap_errors_total",
		"icap_active_connections",
		"icap_goroutines_current",
		"icap_scenarios_matched_total",
		"icap_scenario_requests_total",
		"icap_scenario_response_time_seconds",
		"icap_chaos_injected_total",
		"icap_rate_limit_exceeded_total",
		"icap_rate_limit_wait_seconds",
		"icap_replay_requests_total",
		"icap_replay_requests_failed_total",
		"icap_replay_duration_seconds",
		"icap_replay_behind_original_seconds",
		"icap_streaming_active",
		"icap_streaming_bytes_total",
		"icap_config_reload_total",
		"icap_config_reload_duration_seconds",
		"icap_config_last_reload_status",
	}

	foundMetrics := make(map[string]bool)
	for _, mf := range mfs {
		foundMetrics[mf.GetName()] = true
	}

	for _, expected := range expectedMetrics {
		if !foundMetrics[expected] {
			t.Errorf("Expected metric %s not found", expected)
		}
	}
}

// TestCollector_ConcurrentAccess tests that the collector is safe for concurrent use.
func TestCollector_ConcurrentAccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	done := make(chan bool)

	// Concurrent request recording
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				collector.RecordRequest("REQMOD")
				collector.IncRequestsInFlight("REQMOD")
				collector.DecRequestsInFlight("REQMOD")
				collector.IncActiveConnections()
				collector.DecActiveConnections()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without race condition, test passes
	count := testutil.ToFloat64(collector.requestsTotal.WithLabelValues("default", "REQMOD"))
	if count != 1000 {
		t.Errorf("concurrent request count = %v, want 1000", count)
	}
}

// TestCollector_SetCircuitBreakerState tests circuit breaker state gauge.
func TestCollector_SetCircuitBreakerState(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Test closed state
	collector.SetCircuitBreakerState("storage", "closed")
	value := testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("storage"))
	if value != 0 {
		t.Errorf("closed state = %v, want 0", value)
	}

	// Test half-open state
	collector.SetCircuitBreakerState("storage", "half-open")
	value = testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("storage"))
	if value != 0.5 {
		t.Errorf("half-open state = %v, want 0.5", value)
	}

	// Test open state
	collector.SetCircuitBreakerState("storage", "open")
	value = testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("storage"))
	if value != 1 {
		t.Errorf("open state = %v, want 1", value)
	}
}

// TestCollector_SetCircuitBreakerState_MultipleComponents tests that state gauge works for multiple components.
func TestCollector_SetCircuitBreakerState_MultipleComponents(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Set different states for different components
	collector.SetCircuitBreakerState("storage", "closed")
	collector.SetCircuitBreakerState("processor", "open")
	collector.SetCircuitBreakerState("cache", "half-open")

	// Verify each component has its own state
	storageState := testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("storage"))
	processorState := testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("processor"))
	cacheState := testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("cache"))

	if storageState != 0 {
		t.Errorf("storage state = %v, want 0", storageState)
	}
	if processorState != 1 {
		t.Errorf("processor state = %v, want 1", processorState)
	}
	if cacheState != 0.5 {
		t.Errorf("cache state = %v, want 0.5", cacheState)
	}
}

// TestCollector_RecordCircuitBreakerTransition tests circuit breaker transition counter.
func TestCollector_RecordCircuitBreakerTransition(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Record transitions
	collector.RecordCircuitBreakerTransition("storage", "closed", "open")
	collector.RecordCircuitBreakerTransition("storage", "closed", "open")
	collector.RecordCircuitBreakerTransition("storage", "open", "half-open")
	collector.RecordCircuitBreakerTransition("storage", "half-open", "closed")
	collector.RecordCircuitBreakerTransition("processor", "closed", "open")

	// Verify transitions are recorded correctly
	closedToOpen := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "closed", "open"))
	openToHalfOpen := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "open", "half-open"))
	halfOpenToClosed := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "half-open", "closed"))
	processorTransitions := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("processor", "closed", "open"))

	if closedToOpen != 2 {
		t.Errorf("closed→open transitions = %v, want 2", closedToOpen)
	}
	if openToHalfOpen != 1 {
		t.Errorf("open→half-open transitions = %v, want 1", openToHalfOpen)
	}
	if halfOpenToClosed != 1 {
		t.Errorf("half-open→closed transitions = %v, want 1", halfOpenToClosed)
	}
	if processorTransitions != 1 {
		t.Errorf("processor closed→open transitions = %v, want 1", processorTransitions)
	}
}

// TestCollector_RecordCircuitBreakerFailure tests circuit breaker failure counter.
func TestCollector_RecordCircuitBreakerFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Record failures
	collector.RecordCircuitBreakerFailure("storage")
	collector.RecordCircuitBreakerFailure("storage")
	collector.RecordCircuitBreakerFailure("storage")
	collector.RecordCircuitBreakerFailure("processor")

	// Verify failures are recorded correctly
	storageFailures := testutil.ToFloat64(collector.circuitBreakerFailures.WithLabelValues("storage"))
	processorFailures := testutil.ToFloat64(collector.circuitBreakerFailures.WithLabelValues("processor"))

	if storageFailures != 3 {
		t.Errorf("storage failures = %v, want 3", storageFailures)
	}
	if processorFailures != 1 {
		t.Errorf("processor failures = %v, want 1", processorFailures)
	}
}

// TestCollector_CircuitBreakerMetrics_ConcurrentAccess tests that circuit breaker metrics are safe for concurrent use.
func TestCollector_CircuitBreakerMetrics_ConcurrentAccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	done := make(chan bool)

	// Concurrent state changes, transitions, and failures
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				collector.SetCircuitBreakerState("storage", "closed")
				collector.SetCircuitBreakerState("storage", "open")
				collector.RecordCircuitBreakerTransition("storage", "closed", "open")
				collector.RecordCircuitBreakerTransition("storage", "open", "half-open")
				collector.RecordCircuitBreakerFailure("storage")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without race condition, test passes
	failures := testutil.ToFloat64(collector.circuitBreakerFailures.WithLabelValues("storage"))
	if failures != 1000 {
		t.Errorf("concurrent failures = %v, want 1000", failures)
	}

	transitions := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "closed", "open"))
	if transitions != 1000 {
		t.Errorf("concurrent transitions = %v, want 1000", transitions)
	}
}

// TestCollector_CircuitBreakerMetrics_InitialState tests that initial state is set to closed (0).
func TestCollector_CircuitBreakerMetrics_InitialState(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Set initial state
	collector.SetCircuitBreakerState("storage", "closed")

	value := testutil.ToFloat64(collector.circuitBreakerState.WithLabelValues("storage"))
	if value != 0 {
		t.Errorf("initial state = %v, want 0 (closed)", value)
	}
}

// TestCollector_CircuitBreakerMetrics_AllTransitions tests all possible state transitions.
func TestCollector_CircuitBreakerMetrics_AllTransitions(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Test all possible transitions
	collector.RecordCircuitBreakerTransition("storage", "closed", "open")
	collector.RecordCircuitBreakerTransition("storage", "open", "half-open")
	collector.RecordCircuitBreakerTransition("storage", "half-open", "closed")
	collector.RecordCircuitBreakerTransition("storage", "half-open", "open")

	closedToOpen := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "closed", "open"))
	openToHalfOpen := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "open", "half-open"))
	halfOpenToClosed := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "half-open", "closed"))
	halfOpenToOpen := testutil.ToFloat64(collector.circuitBreakerTransitions.WithLabelValues("storage", "half-open", "open"))

	if closedToOpen != 1 {
		t.Errorf("closed→open = %v, want 1", closedToOpen)
	}
	if openToHalfOpen != 1 {
		t.Errorf("open→half-open = %v, want 1", openToHalfOpen)
	}
	if halfOpenToClosed != 1 {
		t.Errorf("half-open→closed = %v, want 1", halfOpenToClosed)
	}
	if halfOpenToOpen != 1 {
		t.Errorf("half-open→open = %v, want 1", halfOpenToOpen)
	}
}
