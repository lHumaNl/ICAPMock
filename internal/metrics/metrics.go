// Copyright 2026 ICAP Mock

// Package metrics provides collection and reporting of server metrics.
package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Histogram buckets for request/response sizes in bytes.
// Covers the range from 100 bytes to 100MB with typical web content sizes.
var sizeBuckets = []float64{
	100, 1000, 10000, 100000, 1e6, 10e6, 100e6,
}

// scenarioResponseDurationBuckets keeps scenario latency compatible with
// classic Prometheus histograms and VictoriaMetrics/vmagent scrapes.
var scenarioResponseDurationBuckets = []float64{
	0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1,
	1.25, 1.5, 1.75, 2,
	2.5, 3, 3.5, 4, 4.5, 5,
	6, 7, 8, 9, 10,
	15, 20, 25, 30, 35, 40, 45, 50, 55, 60,
	70, 80, 90, 100, 110, 120,
}

// Collector collects and exposes Prometheus metrics for the ICAP Mock Server.
// It provides methods to record various metrics related to request processing,
// errors, connections, scenarios, and streaming.
//
// All methods are safe for concurrent use.
type Collector struct {
	// Request metrics
	requestsTotal        *prometheus.CounterVec
	requestsInFlight     *prometheus.GaugeVec
	requestSize          *prometheus.HistogramVec
	previewRequestsTotal *prometheus.CounterVec
	apiRequestsTotal     *prometheus.CounterVec
	apiErrorsTotal       *prometheus.CounterVec
	contentTypeLabels    *contentTypeLabelLimiter

	// Error metrics
	errorsTotal        *prometheus.CounterVec
	requestErrorsTotal *prometheus.CounterVec

	// Connection metrics
	activeConnections          *prometheus.GaugeVec
	idleConnectionsClosedTotal *prometheus.CounterVec
	connectionRejectionsTotal  *prometheus.CounterVec

	// Runtime metrics
	goroutinesCurrent prometheus.Gauge

	// Mock metrics
	scenarioResponseDuration *prometheus.HistogramVec
	scenariosLoaded          *prometheus.GaugeVec
	scenariosLoadedLabels    map[string]struct{}
	scenarioLabels           *scenarioLabelLimiter

	// Streaming metrics
	streamingActive     prometheus.Gauge
	streamingBytesTotal *prometheus.CounterVec

	// Config reload metrics
	configReloadTotal      *prometheus.CounterVec
	configLastReloadStatus prometheus.Gauge

	// Scenario sharding metrics
	scenarioShardingCacheHit   *prometheus.CounterVec
	scenarioShardingCacheMiss  *prometheus.CounterVec
	scenarioShardingFallback   *prometheus.CounterVec
	scenarioShardingShardsUsed prometheus.Gauge

	// File storage metrics (rotation)
	storageRotationTotal  *prometheus.CounterVec
	storageRotationActive prometheus.Gauge

	// Request timeout and cancellation metrics
	requestTimeoutsTotal                *prometheus.CounterVec
	requestCancellationsTotal           *prometheus.CounterVec
	requestContextCancellationsByReason *prometheus.CounterVec

	// Storage backpressure metrics
	storageBackpressureRejected *prometheus.CounterVec
	storageQueueDrained         prometheus.Counter
	storageQueueLength          prometheus.Gauge

	// Disk monitoring metrics
	storageDiskUsageBytes     prometheus.Gauge
	storageDiskAvailableBytes prometheus.Gauge
	storageDiskWarningsTotal  prometheus.Counter
	storageDiskErrorsTotal    prometheus.Counter

	// TLS certificate metrics
	tlsCertificateExpiryDays *prometheus.GaugeVec

	// Adaptive timeout metrics
	adaptiveTimeoutCurrent *prometheus.GaugeVec

	scenariosLoadedMu sync.Mutex
}

// NewCollector creates a new Collector and registers all metrics with the provided
// Prometheus registry. The registry must not be nil.
//
// Parameters:
//   - reg: The Prometheus registry to register metrics with. Must not be nil.
//
// Returns:
//   - *Collector: The created collector
//   - error: An error if the registry is nil or if metric registration fails
//
// Example:
//
//	reg := prometheus.NewRegistry()
//	collector, err := NewCollector(reg)
//	if err != nil {
//	    log.Fatal(err)
//	}
func NewCollector(reg prometheus.Registerer) (*Collector, error) {
	if reg == nil {
		return nil, ErrNilRegistry
	}

	c := &Collector{
		// Request metrics
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "requests_total",
				Help:      "Total number of ICAP requests by content type, method, outcome, server, response, and scenario.",
			},
			[]string{"content_type", "method", "outcome", "server", "response", "scenario"},
		),
		requestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "requests_in_flight",
				Help:      "Current number of ICAP requests being processed by server and method.",
			},
			[]string{"server", "method"},
		),
		requestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "request_size_bytes",
				Help:      "Size of ICAP request bodies in bytes by server and method.",
				Buckets:   sizeBuckets,
			},
			[]string{"server", "method"},
		),
		previewRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "preview_requests_total",
				Help:      "Total number of ICAP requests that used preview mode by server and method.",
			},
			[]string{"server", "method"},
		),
		contentTypeLabels: newContentTypeLabelLimiter(),
		apiRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "api_requests_total",
				Help:      "Total number of management API requests by bounded route, method, and status code.",
			},
			[]string{"server", "route", "method", "status_code"},
		),
		apiErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "api_errors_total",
				Help:      "Total number of failed management API requests by bounded route, method, status code, and error type.",
			},
			[]string{"server", "route", "method", "status_code", "error_type"},
		),

		// Error metrics
		errorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "errors_total",
				Help:      "Total number of ICAP errors by server and type.",
			},
			[]string{"server", "type"},
		),
		requestErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "request_errors_total",
				Help:      "Total number of ICAP request errors by bounded server, method, stage, error type, scenario, and response labels.",
			},
			[]string{"server", "method", "stage", "error_type", "scenario", "response"},
		),

		// Connection metrics
		activeConnections: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "active_connections",
				Help:      "Current number of active connections by server.",
			},
			[]string{"server"},
		),
		idleConnectionsClosedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "idle_connections_closed_total",
				Help:      "Total number of connections closed due to idle timeout by server and reason.",
			},
			[]string{"server", "reason"},
		),
		connectionRejectionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "connection_rejections_total",
				Help:      "Total number of rejected ICAP connections by server and reason.",
			},
			[]string{"server", "reason"},
		),

		// Runtime metrics
		goroutinesCurrent: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "goroutines_current",
				Help:      "Current number of goroutines.",
			},
		),

		// Mock metrics
		scenarioResponseDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "scenario_response_duration_seconds",
				Help:      "Scenario response duration in seconds by content type, method, outcome, server, response, and scenario.",
				Buckets:   scenarioResponseDurationBuckets,
			},
			[]string{"content_type", "method", "outcome", "server", "response", "scenario"},
		),
		scenariosLoaded: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "scenarios_loaded",
				Help:      "Current number of loaded scenarios by server.",
			},
			[]string{"server"},
		),
		scenarioLabels: newScenarioLabelLimiter(maxScenarioLatencySeries),

		// Streaming metrics
		streamingActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "streaming_active",
				Help:      "Current number of active streaming sessions.",
			},
		),
		streamingBytesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "streaming_bytes_total",
				Help:      "Total bytes streamed by direction (in/out).",
			},
			[]string{"direction"},
		),

		// Config reload metrics
		configReloadTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "config_reload_total",
				Help:      "Total number of configuration reload attempts by status (success/failure).",
			},
			[]string{"status"},
		),
		configLastReloadStatus: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "config_last_reload_status",
				Help:      "Status of the last configuration reload (1=success, 0=failure).",
			},
		),

		// Scenario sharding metrics
		scenarioShardingCacheHit: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "scenario_sharding_cache_hits_total",
				Help:      "Total number of cache hits in scenario sharding.",
			},
			[]string{},
		),
		scenarioShardingCacheMiss: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "scenario_sharding_cache_misses_total",
				Help:      "Total number of cache misses in scenario sharding.",
			},
			[]string{},
		),
		scenarioShardingFallback: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "scenario_sharding_fallback_total",
				Help:      "Total number of fallback to full scan in scenario sharding.",
			},
			[]string{},
		),
		scenarioShardingShardsUsed: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "scenario_sharding_shards_used",
				Help:      "Number of shards currently used for scenario storage.",
			},
		),

		// File storage metrics (rotation)
		storageRotationTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "storage_rotation_total",
				Help:      "Total number of file rotation attempts by status (success/failure).",
			},
			[]string{"status"},
		),
		storageRotationActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "storage_rotation_active",
				Help:      "Current number of active file rotation operations.",
			},
		),

		// Request timeout and cancellation metrics
		requestTimeoutsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "request_timeouts_total",
				Help:      "Total number of request timeouts by server and method.",
			},
			[]string{"server", "method"},
		),
		requestCancellationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "request_cancellations_total",
				Help:      "Total number of request cancellations by server and method.",
			},
			[]string{"server", "method"},
		),
		requestContextCancellationsByReason: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "request_context_cancellations_total",
				Help:      "Total number of request context cancellations by server, method and reason.",
			},
			[]string{"server", "method", "reason"},
		),

		// Storage backpressure metrics
		storageBackpressureRejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "storage_backpressure_rejected_total",
				Help:      "Total number of requests rejected due to storage queue being full.",
			},
			[]string{"queue_size", "max_queue_size"},
		),
		storageQueueDrained: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "storage_queue_drained_total",
				Help:      "Total number of requests drained from the storage queue during shutdown.",
			},
		),
		storageQueueLength: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "storage_queue_length",
				Help:      "Current number of items in the storage queue.",
			},
		),

		// Disk monitoring metrics
		storageDiskUsageBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "storage_disk_usage_bytes",
				Help:      "Current disk usage in bytes for storage directory.",
			},
		),
		storageDiskAvailableBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "storage_disk_available_bytes",
				Help:      "Current available disk space in bytes for storage directory.",
			},
		),
		storageDiskWarningsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "storage_disk_warnings_total",
				Help:      "Total number of disk space warning events.",
			},
		),
		storageDiskErrorsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "storage_disk_errors_total",
				Help:      "Total number of disk space error events (writes rejected).",
			},
		),

		// TLS certificate metrics
		tlsCertificateExpiryDays: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "tls_certificate_expiry_days",
				Help:      "Days until TLS certificate expires. Set to -1 if certificate cannot be loaded.",
			},
			[]string{"cert_file"},
		),

		// Adaptive timeout metrics
		adaptiveTimeoutCurrent: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "adaptive_timeout_current_ms",
				Help:      "Current adaptive timeout in milliseconds by endpoint and method.",
			},
			[]string{"endpoint", "method"},
		),
	}

	// Register all metrics with the provided registry
	reg.MustRegister(
		c.requestsTotal,
		c.requestsInFlight,
		c.requestSize,
		c.previewRequestsTotal,
		c.apiRequestsTotal,
		c.apiErrorsTotal,
		c.errorsTotal,
		c.requestErrorsTotal,
		c.activeConnections,
		c.idleConnectionsClosedTotal,
		c.connectionRejectionsTotal,
		c.goroutinesCurrent,
		c.scenarioResponseDuration,
		c.scenariosLoaded,
		c.streamingActive,
		c.streamingBytesTotal,
		c.configReloadTotal,
		c.configLastReloadStatus,
		c.scenarioShardingCacheHit,
		c.scenarioShardingCacheMiss,
		c.scenarioShardingFallback,
		c.scenarioShardingShardsUsed,
		c.storageRotationTotal,
		c.storageRotationActive,
		c.requestTimeoutsTotal,
		c.requestCancellationsTotal,
		c.requestContextCancellationsByReason,
		c.storageBackpressureRejected,
		c.storageQueueDrained,
		c.storageQueueLength,
		c.storageDiskUsageBytes,
		c.storageDiskAvailableBytes,
		c.storageDiskWarningsTotal,
		c.storageDiskErrorsTotal,
		c.tlsCertificateExpiryDays,
		c.adaptiveTimeoutCurrent,
	)

	return c, nil
}

// RecordRequest increments the counter for ICAP requests by method.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRequest(method string) {
	c.RecordRequestForServer(defaultServerMetricLabel, method, "", OutcomeAllowed, unknownMetricLabel, unknownMetricLabel)
}

// RecordRequestForServer increments the ICAP request counter for a raw Content-Type.
func (c *Collector) RecordRequestForServer(server, method, contentType, outcome, response, scenario string) {
	c.RecordRequestForServerWithContentTypeLabel(
		server, method, c.contentTypeLabel(contentType), outcome, response, scenario,
	)
}

// IncRequestsInFlight increments the gauge tracking requests currently being processed.
// This should be called when a request starts being processed.
//
// This method is safe for concurrent use.
func (c *Collector) IncRequestsInFlight(method string) {
	c.IncRequestsInFlightForServer(defaultServerMetricLabel, method)
}

// IncRequestsInFlightForServer increments in-flight requests by server and method.
func (c *Collector) IncRequestsInFlightForServer(server, method string) {
	c.requestsInFlight.WithLabelValues(normalizedMetricLabel(server), method).Inc()
}

// DecRequestsInFlight decrements the gauge tracking requests currently being processed.
// This should be called when a request finishes processing.
//
// This method is safe for concurrent use.
func (c *Collector) DecRequestsInFlight(method string) {
	c.DecRequestsInFlightForServer(defaultServerMetricLabel, method)
}

// DecRequestsInFlightForServer decrements in-flight requests by server and method.
func (c *Collector) DecRequestsInFlightForServer(server, method string) {
	c.requestsInFlight.WithLabelValues(normalizedMetricLabel(server), method).Dec()
}

// RecordRequestSize records the size of a request body in bytes.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRequestSize(method string, sizeBytes int64) {
	c.RecordRequestSizeForServer(defaultServerMetricLabel, method, sizeBytes)
}

// RecordRequestSizeForServer records request body size by server and method.
func (c *Collector) RecordRequestSizeForServer(server, method string, sizeBytes int64) {
	c.requestSize.WithLabelValues(normalizedMetricLabel(server), method).Observe(float64(sizeBytes))
}

// RecordPreviewRequest increments the counter for preview requests.
//
// This method is safe for concurrent use.
func (c *Collector) RecordPreviewRequest(method string) {
	c.RecordPreviewRequestForServer(defaultServerMetricLabel, method)
}

// RecordPreviewRequestForServer increments the preview request counter when preview was used.
func (c *Collector) RecordPreviewRequestForServer(server, method string) {
	c.previewRequestsTotal.WithLabelValues(normalizedMetricLabel(server), method).Inc()
}

// RecordError increments the error counter for the given error type.
// Common error types include "timeout", "connection_error", "invalid_request", etc.
//
// This method is safe for concurrent use.
func (c *Collector) RecordError(errorType string) {
	c.RecordErrorForServer(defaultServerMetricLabel, errorType)
}

// RecordErrorForServer increments the error counter by server and error type.
func (c *Collector) RecordErrorForServer(server, errorType string) {
	c.errorsTotal.WithLabelValues(normalizedMetricLabel(server), errorType).Inc()
}

// IncActiveConnections increments the gauge tracking active connections.
// This should be called when a new connection is established.
//
// This method is safe for concurrent use.
func (c *Collector) IncActiveConnections() {
	c.IncActiveConnectionsForServer(defaultServerMetricLabel)
}

// IncActiveConnectionsForServer increments active connections by server.
func (c *Collector) IncActiveConnectionsForServer(server string) {
	c.activeConnections.WithLabelValues(normalizedMetricLabel(server)).Inc()
}

// DecActiveConnections decrements the gauge tracking active connections.
// This should be called when a connection is closed.
//
// This method is safe for concurrent use.
func (c *Collector) DecActiveConnections() {
	c.DecActiveConnectionsForServer(defaultServerMetricLabel)
}

// DecActiveConnectionsForServer decrements active connections by server.
func (c *Collector) DecActiveConnectionsForServer(server string) {
	c.activeConnections.WithLabelValues(normalizedMetricLabel(server)).Dec()
}

// RecordIdleConnectionClosed increments the counter for connections closed due to idle timeout.
// The reason should indicate why the connection was closed (e.g., "idle", "timeout").
//
// This method is safe for concurrent use.
func (c *Collector) RecordIdleConnectionClosed(reason string) {
	c.RecordIdleConnectionClosedForServer(defaultServerMetricLabel, reason)
}

// RecordIdleConnectionClosedForServer increments idle close counts by server and reason.
func (c *Collector) RecordIdleConnectionClosedForServer(server, reason string) {
	c.idleConnectionsClosedTotal.WithLabelValues(normalizedMetricLabel(server), reason).Inc()

}

// RecordConnectionRejected increments rejected connection counts for the server.
func (c *Collector) RecordConnectionRejected(server, reason string) {
	c.connectionRejectionsTotal.WithLabelValues(normalizedMetricLabel(server), reason).Inc()
}

// SetGoroutines sets the gauge tracking the current number of goroutines.
// This is typically called periodically by a goroutine monitoring routine.
//
// This method is safe for concurrent use.
func (c *Collector) SetGoroutines(count int) {
	c.goroutinesCurrent.Set(float64(count))
}

// RecordScenarioRequest records a matched scenario request and duration histogram sample.
// The response label should be a response/template name when available, or a status code.
// User-supplied reserved labels are escaped before cardinality admission.
// New (scenario, response) pairs beyond the cardinality cap are aggregated into
// the reserved __overflow__ labels before any scenario Prometheus vector is touched.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioRequest(scenario, response string, duration time.Duration) {
	c.RecordScenarioRequestForServer(defaultServerMetricLabel, scenario, response, duration)
}

// RecordScenarioRequestForServer records a matched scenario request by server.
func (c *Collector) RecordScenarioRequestForServer(server, scenario, response string, duration time.Duration) {
	c.RecordScenarioRequestForServerWithLabels(
		server,
		unknownMetricLabel,
		ContentTypeNone,
		OutcomeAllowed,
		scenario,
		response,
		duration,
	)
}

// RecordScenarioRequestForServerWithBlock records a matched scenario request by server and block outcome.
func (c *Collector) RecordScenarioRequestForServerWithBlock(
	server, scenario, response string,
	block bool,
	duration time.Duration,
) {
	outcome := OutcomeAllowed
	if block {
		outcome = OutcomeBlocked
	}
	c.RecordScenarioRequestForServerWithLabels(
		server,
		unknownMetricLabel,
		ContentTypeNone,
		outcome,
		scenario,
		response,
		duration,
	)
}

// RecordScenarioRequestForServerWithLabels records merged request and scenario duration metrics.
func (c *Collector) RecordScenarioRequestForServerWithLabels(
	server, method, contentTypeLabel, outcome, scenario, response string,
	duration time.Duration,
) {
	c.RecordRequestForServerWithContentTypeLabel(server, method, contentTypeLabel, outcome, response, scenario)
	c.RecordScenarioResponseDurationForServer(server, method, contentTypeLabel, outcome, scenario, response, duration)
}

// RecordScenarioResponseDurationForServer records matched scenario latency without incrementing requests.
func (c *Collector) RecordScenarioResponseDurationForServer(
	server, method, contentTypeLabel, outcome, scenario, response string,
	duration time.Duration,
) {
	labels := c.admitScenarioLabels(server, method, contentTypeLabel, outcome, scenario, response)
	if duration < 0 {
		duration = 0
	}
	c.scenarioResponseDuration.WithLabelValues(
		labels.contentType,
		labels.method,
		labels.outcome,
		labels.server,
		labels.response,
		labels.scenario,
	).
		Observe(duration.Seconds())
}

func normalizeOutcomeLabel(outcome string) string {
	switch outcome {
	case OutcomeAllowed, OutcomeBlocked, OutcomeError:
		return outcome
	default:
		return OutcomeError
	}
}

// SetScenariosLoaded sets the current loaded scenario count for a server.
func (c *Collector) SetScenariosLoaded(server string, count int) {
	label := normalizedMetricLabel(server)
	c.scenariosLoaded.WithLabelValues(label).Set(float64(count))
	c.trackScenariosLoadedLabel(label)
}

// SetScenariosLoadedSnapshot replaces the reported scenario-loaded server set.
func (c *Collector) SetScenariosLoadedSnapshot(counts map[string]int) {
	c.scenariosLoadedMu.Lock()
	defer c.scenariosLoadedMu.Unlock()
	current := make(map[string]struct{}, len(counts))
	for server, count := range counts {
		label := normalizedMetricLabel(server)
		c.scenariosLoaded.WithLabelValues(label).Set(float64(count))
		current[label] = struct{}{}
	}
	for label := range c.scenariosLoadedLabels {
		if _, ok := current[label]; !ok {
			c.scenariosLoaded.DeleteLabelValues(label)
		}
	}
	c.scenariosLoadedLabels = current
}

func (c *Collector) trackScenariosLoadedLabel(label string) {
	c.scenariosLoadedMu.Lock()
	defer c.scenariosLoadedMu.Unlock()
	if c.scenariosLoadedLabels == nil {
		c.scenariosLoadedLabels = make(map[string]struct{})
	}
	c.scenariosLoadedLabels[label] = struct{}{}
}

// RecordFallbackScenarioRequest records use of the default fallback response.
func (c *Collector) RecordFallbackScenarioRequest(server, response string, duration time.Duration) {
	c.RecordScenarioRequestForServer(server, FallbackScenarioMetricLabel, response, duration)
}

// IncStreamingActive increments the gauge tracking active streaming sessions.
// This should be called when a new streaming session starts.
//
// This method is safe for concurrent use.
func (c *Collector) IncStreamingActive() {
	c.streamingActive.Inc()
}

// DecStreamingActive decrements the gauge tracking active streaming sessions.
// This should be called when a streaming session ends.
//
// This method is safe for concurrent use.
func (c *Collector) DecStreamingActive() {
	c.streamingActive.Dec()
}

// RecordStreamingBytes adds to the counter for streamed bytes.
// Direction should be "in" for incoming bytes or "out" for outgoing bytes.
//
// This method is safe for concurrent use.
func (c *Collector) RecordStreamingBytes(direction string, bytes int64) {
	c.streamingBytesTotal.WithLabelValues(direction).Add(float64(bytes))
}

// RecordConfigReload increments the counter for configuration reload attempts
// with the given status. Status should be "success" or "failure".
//
// This method is safe for concurrent use.
func (c *Collector) RecordConfigReload(status string) {
	c.configReloadTotal.WithLabelValues(status).Inc()
}

// SetConfigLastReloadStatus sets the gauge indicating the status of the last
// configuration reload. Use 1 for success and 0 for failure.
//
// This method is safe for concurrent use.
func (c *Collector) SetConfigLastReloadStatus(success bool) {
	value := float64(0)
	if success {
		value = 1
	}
	c.configLastReloadStatus.Set(value)
}

// RecordScenarioShardingCacheHit increments the counter for cache hits in scenario sharding.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioShardingCacheHit() {
	c.scenarioShardingCacheHit.WithLabelValues().Inc()
}

// RecordScenarioShardingCacheMiss increments the counter for cache misses in scenario sharding.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioShardingCacheMiss() {
	c.scenarioShardingCacheMiss.WithLabelValues().Inc()
}

// RecordScenarioShardingFallback increments the counter for fallback to full scan.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioShardingFallback() {
	c.scenarioShardingFallback.WithLabelValues().Inc()
}

// SetScenarioShardingShardsUsed sets the gauge for number of shards in use.
//
// This method is safe for concurrent use.
func (c *Collector) SetScenarioShardingShardsUsed(count int) {
	c.scenarioShardingShardsUsed.Set(float64(count))
}

// RecordStorageRotation increments the counter for file rotation operations
// with the given status. Status should be "success" or "failure".
//
// This method is safe for concurrent use.
func (c *Collector) RecordStorageRotation(status string) {
	c.storageRotationTotal.WithLabelValues(status).Inc()
}

// IncStorageRotationActive increments the gauge tracking active file rotation operations.
// This should be called when a rotation operation starts.
//
// This method is safe for concurrent use.
func (c *Collector) IncStorageRotationActive() {
	c.storageRotationActive.Inc()
}

// DecStorageRotationActive decrements the gauge tracking active file rotation operations.
// This should be called when a rotation operation completes.
//
// This method is safe for concurrent use.
func (c *Collector) DecStorageRotationActive() {
	c.storageRotationActive.Dec()
}

// RecordRequestTimeout increments the counter for request timeouts.
// This method is safe for concurrent use.
func (c *Collector) RecordRequestTimeout(method string) {
	c.RecordRequestTimeoutForServer(defaultServerMetricLabel, method)
}

// RecordRequestTimeoutForServer increments request timeouts by server and method.
func (c *Collector) RecordRequestTimeoutForServer(server, method string) {
	c.requestTimeoutsTotal.WithLabelValues(normalizedMetricLabel(server), method).Inc()
}

// RecordRequestCancellation increments the counter for request cancellations.
// This method is safe for concurrent use.
func (c *Collector) RecordRequestCancellation(method string) {
	c.RecordRequestCancellationForServer(defaultServerMetricLabel, method)
}

// RecordRequestCancellationForServer increments request cancellations by server and method.
func (c *Collector) RecordRequestCancellationForServer(server, method string) {
	c.requestCancellationsTotal.WithLabelValues(normalizedMetricLabel(server), method).Inc()
}

// RecordRequestContextCancellation increments the counter for request context cancellations by reason.
// Reason should be "deadline_exceeded" or "canceled".
//
// This method is safe for concurrent use.
func (c *Collector) RecordRequestContextCancellation(method, reason string) {
	c.RecordRequestContextCancellationForServer(defaultServerMetricLabel, method, reason)
}

// RecordRequestContextCancellationForServer increments context cancellations by server.
func (c *Collector) RecordRequestContextCancellationForServer(server, method, reason string) {
	c.requestContextCancellationsByReason.WithLabelValues(normalizedMetricLabel(server), method, reason).Inc()
}

// RecordStorageBackpressureRejected increments the counter for requests rejected
// due to the storage queue being full.
//
// This method is safe for concurrent use.
func (c *Collector) RecordStorageBackpressureRejected(queueSize, maxQueueSize int) {
	c.storageBackpressureRejected.WithLabelValues(
		string(rune(queueSize)),    //nolint:gosec // safe range
		string(rune(maxQueueSize)), //nolint:gosec // safe range
	).Inc()
}

// RecordStorageQueueDrained increments the counter for items drained from
// the storage queue during shutdown.
//
// This method is safe for concurrent use.
func (c *Collector) RecordStorageQueueDrained(count int) {
	c.storageQueueDrained.Add(float64(count))
}

// SetStorageQueueLength sets the gauge for the current number of items in the storage queue.
//
// This method is safe for concurrent use.
func (c *Collector) SetStorageQueueLength(length int) {
	c.storageQueueLength.Set(float64(length))
}

// SetTLSCertificateExpiryDays sets the gauge for TLS certificate expiry.
// The certFile parameter is the path to the certificate file.
// Set to -1 if the certificate cannot be loaded or is invalid.
//
// This method is safe for concurrent use.
func (c *Collector) SetTLSCertificateExpiryDays(certFile string, days float64) {
	c.tlsCertificateExpiryDays.WithLabelValues(certFile).Set(days)
}

// SetAdaptiveTimeout sets the gauge for the current adaptive timeout.
// The timeout is in milliseconds.
//
// This method is safe for concurrent use.
func (c *Collector) SetAdaptiveTimeout(endpoint, method string, timeoutMs float64) {
	c.adaptiveTimeoutCurrent.WithLabelValues(endpoint, method).Set(timeoutMs)
}

// RecordAPIRequest increments the bounded management API request counter.
func (c *Collector) RecordAPIRequest(server, route, method string, statusCode int) {
	c.apiRequestsTotal.WithLabelValues(
		normalizedMetricLabel(server), route, method, fmt.Sprintf("%d", statusCode),
	).Inc()
}

// RecordAPIError increments the bounded management API error counter.
func (c *Collector) RecordAPIError(server, route, method string, statusCode int, errorType string) {
	c.apiErrorsTotal.WithLabelValues(
		normalizedMetricLabel(server), route, method, fmt.Sprintf("%d", statusCode), errorType,
	).Inc()
}

// SetStorageDiskUsage sets the gauge for the current disk usage in bytes.
//
// This method is safe for concurrent use.
func (c *Collector) SetStorageDiskUsage(usageBytes int64) {
	c.storageDiskUsageBytes.Set(float64(usageBytes))
}

// SetStorageDiskAvailable sets the gauge for the current available disk space in bytes.
//
// This method is safe for concurrent use.
func (c *Collector) SetStorageDiskAvailable(availableBytes int64) {
	c.storageDiskAvailableBytes.Set(float64(availableBytes))
}

// IncStorageDiskWarnings increments the counter for disk space warning events.
// A warning is logged when disk usage reaches the warning threshold.
//
// This method is safe for concurrent use.
func (c *Collector) IncStorageDiskWarnings() {
	c.storageDiskWarningsTotal.Inc()
}

// IncStorageDiskErrors increments the counter for disk space error events.
// An error is logged when a write is rejected due to insufficient disk space.
//
// This method is safe for concurrent use.
func (c *Collector) IncStorageDiskErrors() {
	c.storageDiskErrorsTotal.Inc()
}
