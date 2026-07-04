// Copyright 2026 ICAP Mock

package metrics

import (
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

func TestNewCollector_RemovedFeatureMetricsAreAbsent(t *testing.T) {
	reg := prometheus.NewRegistry()
	_, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	removedMetrics := []string{
		"icap_rate_limit_exceeded_total",
		"icap_rate_limit_wait_seconds",
		"icap_per_client_rate_limit_exceeded_total",
		"icap_per_client_rate_limit_wait_seconds",
		"icap_per_client_rate_limit_active_clients",
		"icap_per_client_rate_limit_evictions_total",
		"icap_replay_requests_total",
		"icap_replay_requests_failed_total",
		"icap_replay_duration_seconds",
		"icap_replay_behind_original_seconds",
		"icap_chaos_injected_total",
		"icap_script_pool_rejected_total",
		"icap_script_pool_queue_length",
		"icap_script_pool_workers",
		"icap_circuit_breaker_state",
		"icap_circuit_breaker_transitions_total",
		"icap_circuit_breaker_failures_total",
		"icap_preview_requests_rejected_total",
	}
	for _, name := range removedMetrics {
		t.Run(name, func(t *testing.T) {
			assertNoMetric(t, reg, name)
		})
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

	assertHistogramCount(t, reg, "icap_response_size_bytes", map[string]string{
		"server": "default",
		"method": "REQMOD",
		"body":   "icap",
	}, 2)
}

func TestCollector_RecordResponseBodySizeForServer(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordResponseBodySizeForServer("edge", "RESPMOD", ResponseBodyHTTP, 512)

	assertHistogramCount(t, reg, "icap_response_size_bytes", map[string]string{
		"server": "edge",
		"method": "RESPMOD",
		"body":   "http",
	}, 1)
}

func TestCollector_RecordIncomingRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordIncomingRequest("edge", "REQMOD", "/scan/file.exe", "exe", "error", "403", true)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "endpoint": "/scan/file.exe",
		"extension": "exe", "result": "error", "icap_status": "403", "blocked": "true",
	}
	if got := metricValue(t, reg, "icap_incoming_requests_total", labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestCollector_RecordIncomingRequestBoundsExtensionLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordIncomingRequest("edge", "REQMOD", "default", ExtractExtension("/scan/file.randomext"), "error", "403", true)

	labels := map[string]string{
		"server": "edge", "method": "REQMOD", "endpoint": "default",
		"extension": "other", "result": "error", "icap_status": "403", "blocked": "true",
	}
	if got := metricValue(t, reg, "icap_incoming_requests_total", labels); got != 1 {
		t.Errorf("incoming requests = %v, want 1", got)
	}
}

func TestEndpointLabelHelpers(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		uri       string
		endpoint  string
		extension string
	}{
		{"default", EndpointLabelModeDefault, "icap://host/scan/file.exe?q=1", "default", "exe"},
		{"path", EndpointLabelModePath, "icap://host/scan/../clean/file.TXT?q=1#frag", "/clean/file.TXT", "txt"},
		{"relative", EndpointLabelModePath, "/scan/archive", "/scan/archive", "none"},
		{"empty path", EndpointLabelModePath, "icap://host", "/", "none"},
		{"unknown extension", EndpointLabelModePath, "icap://host/scan/file.randomext", "/scan/file.randomext", "other"},
		{"malformed extension", EndpointLabelModePath, "icap://host/scan/file.bad-ext", "/scan/file.bad-ext", "other"},
		{"too long extension", EndpointLabelModePath, "icap://host/scan/file.verylongext", "/scan/file.verylongext", "other"},
		{"known mixed case", EndpointLabelModePath, "icap://host/scan/file.JpEg", "/scan/file.JpEg", "jpeg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeEndpointLabel(tt.mode, tt.uri); got != tt.endpoint {
				t.Errorf("NormalizeEndpointLabel() = %q, want %q", got, tt.endpoint)
			}
			if got := ExtractExtension(tt.uri); got != tt.extension {
				t.Errorf("ExtractExtension() = %q, want %q", got, tt.extension)
			}
		})
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

	blocked := testutil.ToFloat64(collector.scenarioRequests.WithLabelValues("default", "virus_scan", "blocked", "false"))
	noContent := testutil.ToFloat64(collector.scenarioRequests.WithLabelValues("default", "virus_scan", "204", "false"))
	if blocked != 2 {
		t.Errorf("blocked scenario requests = %v, want 2", blocked)
	}
	if noContent != 1 {
		t.Errorf("204 scenario requests = %v, want 1", noContent)
	}
	assertMetricLabels(t, reg, "icap_scenario_requests_total", []string{"block", "response", "scenario", "server"})
}

func TestCollector_RecordScenarioRequestWithBlock(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordScenarioRequestForServerWithBlock("edge", "scan", "blocked", true, time.Millisecond)

	labels := map[string]string{"server": "edge", "scenario": "scan", "response": "blocked", "block": "true"}
	if got := metricValue(t, reg, "icap_scenario_requests_total", labels); got != 1 {
		t.Errorf("blocked scenario requests = %v, want 1", got)
	}
	assertHistogramCount(t, reg, "icap_scenario_response_duration_seconds", labels, 1)
}

func TestCollector_RecordFallbackScenarioRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	collector.RecordFallbackScenarioRequest("server-a", "204", time.Millisecond)
	labels := map[string]string{"server": "server-a", "scenario": "fallback", "response": "204", "block": "false"}
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

func TestCollector_RecordScenarioRequestLatencyHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	for i := 1; i <= 100; i++ {
		collector.RecordScenarioRequest("scan", "clean", time.Duration(i)*time.Millisecond)
	}

	assertHistogramCount(t, reg, "icap_scenario_response_duration_seconds", map[string]string{
		"server":   "default",
		"scenario": "scan",
		"response": "clean",
		"block":    "false",
	}, 100)
	assertNoMetric(t, reg, "icap_scenario_response_time_seconds")
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
	gotLatency := countMetricSeries(t, reg, "icap_scenario_response_duration_seconds")
	wantLatency := wantPairs
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

	labels := map[string]string{"server": overflowMetricLabel, "scenario": overflowMetricLabel, "response": overflowMetricLabel, "block": overflowMetricLabel}
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
	return map[string]string{"server": overflowMetricLabel, "scenario": overflowMetricLabel, "response": overflowMetricLabel, "block": overflowMetricLabel}
}

func escapedOverflowLabels() map[string]string {
	escaped := escapedOverflowMetricLabel()
	return map[string]string{"server": "default", "scenario": escaped, "response": escaped, "block": "false"}
}

func escapedOverflowMetricLabel() string {
	return userMetricLabelEscapePrefix + overflowMetricLabel
}

func overflowSourceLabels() map[string]string {
	return map[string]string{"server": "default", "scenario": "overflow-a", "response": "blocked", "block": "false"}
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
	collector.RecordIncomingRequest("default", "REQMOD", "default", "none", "success", "204", false)
	collector.IncRequestsInFlight("REQMOD")
	collector.DecRequestsInFlight("REQMOD")
	collector.RecordRequestSize("REQMOD", 100)
	collector.RecordResponseSize("REQMOD", 100)
	collector.RecordError("test")
	collector.IncActiveConnections()
	collector.SetGoroutines(1)
	collector.RecordScenarioMatched("test")
	collector.RecordScenarioRequest("test", "204", time.Millisecond)
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
		"icap_incoming_requests_total",
		"icap_requests_in_flight",
		"icap_request_size_bytes",
		"icap_response_size_bytes",
		"icap_errors_total",
		"icap_active_connections",
		"icap_goroutines_current",
		"icap_scenarios_matched_total",
		"icap_scenario_requests_total",
		"icap_scenario_response_duration_seconds",
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
