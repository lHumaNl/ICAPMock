// Copyright 2026 ICAP Mock

// Package metrics provides collection and reporting of server metrics.
package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Histogram buckets for request durations in seconds.
// Covers the range from 1ms to 30s with good resolution for typical ICAP latencies.
var durationBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
}

// Histogram buckets for request/response sizes in bytes.
// Covers the range from 100 bytes to 100MB with typical web content sizes.
var sizeBuckets = []float64{
	100, 1000, 10000, 100000, 1e6, 10e6, 100e6,
}

// Histogram buckets for rate limit wait times in seconds.
// Covers the range from 1ms to 10s.
var waitTimeBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Histogram buckets for replay durations in seconds.
// Covers the range from 10ms to 60s.
var replayDurationBuckets = []float64{
	0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
}

// Histogram buckets for config reload durations in seconds.
// Covers the range from 1ms to 10s.
var configReloadDurationBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Collector collects and exposes Prometheus metrics for the ICAP Mock Server.
// It provides methods to record various metrics related to request processing,
// errors, connections, scenarios, chaos, rate limiting, replay, and streaming.
//
// All methods are safe for concurrent use.
type Collector struct {
	// Request metrics
	requestsTotal        *prometheus.CounterVec
	requestDuration      *prometheus.HistogramVec
	requestsInFlight     *prometheus.GaugeVec
	requestSize          *prometheus.HistogramVec
	responseSize         *prometheus.HistogramVec
	previewRequestsTotal *prometheus.CounterVec
	apiRequestsTotal     *prometheus.CounterVec
	apiErrorsTotal       *prometheus.CounterVec

	// Error metrics
	errorsTotal *prometheus.CounterVec

	// Connection metrics
	activeConnections          *prometheus.GaugeVec
	idleConnectionsClosedTotal *prometheus.CounterVec
	connectionRejectionsTotal  *prometheus.CounterVec

	// Runtime metrics
	goroutinesCurrent prometheus.Gauge

	// Mock metrics
	scenariosMatched      *prometheus.CounterVec
	scenarioRequests      *prometheus.CounterVec
	scenarioResponseTime  *prometheus.GaugeVec
	scenariosLoaded       *prometheus.GaugeVec
	scenariosLoadedLabels map[string]struct{}
	scenarioLatencyWindow *scenarioLatencyWindows

	// Chaos metrics
	chaosInjected *prometheus.CounterVec

	// Rate limit metrics
	rateLimitExceeded *prometheus.CounterVec
	rateLimitWaitTime *prometheus.HistogramVec

	// Per-client rate limit metrics
	perClientRateLimitExceeded  *prometheus.CounterVec
	perClientRateLimitWaitTime  *prometheus.HistogramVec
	perClientRateLimitActive    *prometheus.GaugeVec
	perClientRateLimitEvictions *prometheus.CounterVec

	// Replay metrics
	replayRequestsTotal  prometheus.Counter
	replayRequestsFailed prometheus.Counter
	replayDuration       prometheus.Histogram
	replayBehindOriginal prometheus.Gauge

	// Streaming metrics
	streamingActive     prometheus.Gauge
	streamingBytesTotal *prometheus.CounterVec

	// Config reload metrics
	configReloadTotal      *prometheus.CounterVec
	configReloadDuration   prometheus.Histogram
	configLastReloadStatus prometheus.Gauge

	// Scenario sharding metrics
	scenarioShardingCacheHit   *prometheus.CounterVec
	scenarioShardingCacheMiss  *prometheus.CounterVec
	scenarioShardingFallback   *prometheus.CounterVec
	scenarioShardingShardsUsed prometheus.Gauge

	// File storage metrics (rotation)
	storageRotationTotal    *prometheus.CounterVec
	storageRotationDuration prometheus.Histogram
	storageRotationActive   prometheus.Gauge

	// Request timeout and cancellation metrics
	requestTimeoutsTotal                *prometheus.CounterVec
	requestCancellationsTotal           *prometheus.CounterVec
	requestContextCancellationsByReason *prometheus.CounterVec

	// Storage backpressure metrics
	storageBackpressureRejected *prometheus.CounterVec
	storageQueueDrained         prometheus.Counter
	storageQueueLength          prometheus.Gauge

	// Script pool metrics
	scriptPoolRejected *prometheus.CounterVec
	scriptPoolLength   prometheus.Gauge
	scriptPoolWorkers  prometheus.Gauge

	// Circuit breaker metrics
	circuitBreakerState       *prometheus.GaugeVec
	circuitBreakerTransitions *prometheus.CounterVec
	circuitBreakerFailures    *prometheus.CounterVec

	// Disk monitoring metrics
	storageDiskUsageBytes     prometheus.Gauge
	storageDiskAvailableBytes prometheus.Gauge
	storageDiskWarningsTotal  prometheus.Counter
	storageDiskErrorsTotal    prometheus.Counter

	// TLS certificate metrics
	tlsCertificateExpiryDays *prometheus.GaugeVec

	// Adaptive timeout metrics
	adaptiveTimeoutCurrent *prometheus.GaugeVec

	// Preview rate limit metrics
	previewRequestsRejected *prometheus.CounterVec
	previewClientsActive    prometheus.Gauge

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
				Help:      "Total number of ICAP requests by server and method.",
			},
			[]string{"server", "method"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "request_duration_seconds",
				Help:      "Time spent processing ICAP requests in seconds by server and method.",
				Buckets:   durationBuckets,
			},
			[]string{"server", "method"},
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
		responseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "response_size_bytes",
				Help:      "Size of ICAP response bodies in bytes by server and method.",
				Buckets:   sizeBuckets,
			},
			[]string{"server", "method"},
		),
		previewRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "preview_requests_total",
				Help:      "Total number of ICAP preview requests by server, method and preview_used status.",
			},
			[]string{"server", "method", "preview_used"},
		),
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
		scenariosMatched: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "scenarios_matched_total",
				Help:      "Total number of matched mock scenarios by server.",
			},
			[]string{"server", "scenario"},
		),
		scenarioRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "scenario_requests_total",
				Help:      "Total number of matched scenario requests by server, scenario and selected response.",
			},
			[]string{"server", "scenario", "response"},
		),
		scenarioResponseTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "scenario_response_time_seconds",
				Help:      "Rolling scenario response-time statistics in seconds by server, scenario, response, and stat.",
			},
			[]string{"server", "scenario", "response", "stat"},
		),
		scenariosLoaded: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "scenarios_loaded",
				Help:      "Current number of loaded scenarios by server.",
			},
			[]string{"server"},
		),
		scenarioLatencyWindow: newScenarioLatencyWindows(
			scenarioLatencyWindowCapacity,
			maxScenarioLatencySeries,
		),

		// Chaos metrics
		chaosInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "chaos_injected_total",
				Help:      "Total number of chaos injections by type.",
			},
			[]string{"type"},
		),

		// Rate limit metrics
		rateLimitExceeded: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "rate_limit_exceeded_total",
				Help:      "Total number of rate limit exceeded events by server.",
			},
			[]string{"server"},
		),
		rateLimitWaitTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "rate_limit_wait_seconds",
				Help:      "Time spent waiting due to rate limiting in seconds by server.",
				Buckets:   waitTimeBuckets,
			},
			[]string{"server"},
		),

		// Per-client rate limit metrics
		perClientRateLimitExceeded: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "per_client_rate_limit_exceeded_total",
				Help:      "Total number of per-client rate limit exceeded events by server.",
			},
			[]string{"server"},
		),
		perClientRateLimitWaitTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "per_client_rate_limit_wait_seconds",
				Help:      "Time spent waiting due to per-client rate limiting in seconds by server.",
				Buckets:   waitTimeBuckets,
			},
			[]string{"server"},
		),
		perClientRateLimitActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "per_client_rate_limit_active_clients",
				Help:      "Current number of active clients tracked in per-client rate limiter by server.",
			},
			[]string{"server"},
		),
		perClientRateLimitEvictions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "per_client_rate_limit_evictions_total",
				Help:      "Total number of client evictions from the per-client rate limiter cache by server.",
			},
			[]string{"server"},
		),

		// Replay metrics
		replayRequestsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "replay_requests_total",
				Help:      "Total number of replayed requests.",
			},
		),
		replayRequestsFailed: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "replay_requests_failed_total",
				Help:      "Total number of failed replay requests.",
			},
		),
		replayDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "replay_duration_seconds",
				Help:      "Duration of replay operations in seconds.",
				Buckets:   replayDurationBuckets,
			},
		),
		replayBehindOriginal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "replay_behind_original_seconds",
				Help:      "How far behind the replay is compared to the original timeline in seconds.",
			},
		),

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
		configReloadDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "config_reload_duration_seconds",
				Help:      "Duration of configuration reload operations in seconds.",
				Buckets:   configReloadDurationBuckets,
			},
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
		storageRotationDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "icap",
				Name:      "storage_rotation_duration_seconds",
				Help:      "Duration of file rotation operations in seconds.",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
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

		// Script pool metrics
		scriptPoolRejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "script_pool_rejected_total",
				Help:      "Total number of script executions rejected due to queue being full.",
			},
			[]string{"queue_size", "max_queue_size"},
		),
		scriptPoolLength: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "script_pool_queue_length",
				Help:      "Current number of items in the script execution queue.",
			},
		),
		scriptPoolWorkers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "script_pool_workers",
				Help:      "Current number of active script worker goroutines.",
			},
		),

		// Circuit breaker metrics
		circuitBreakerState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "circuit_breaker_state",
				Help:      "Current state of circuit breaker (1=open, 0.5=half-open, 0=closed).",
			},
			[]string{"component"},
		),
		circuitBreakerTransitions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "circuit_breaker_transitions_total",
				Help:      "Total number of circuit breaker state transitions by component and state change.",
			},
			[]string{"component", "from_state", "to_state"},
		),
		circuitBreakerFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "circuit_breaker_failures_total",
				Help:      "Total number of failures recorded by circuit breaker by component.",
			},
			[]string{"component"},
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

		// Preview rate limit metrics
		previewRequestsRejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "preview_requests_rejected_total",
				Help:      "Total number of preview requests rejected due to rate limiting.",
			},
			[]string{}, // No labels to prevent high cardinality
		),
		previewClientsActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "icap",
				Name:      "preview_clients_active",
				Help:      "Current number of active clients tracked by preview rate limiter.",
			},
		),
	}

	// Register all metrics with the provided registry
	reg.MustRegister(
		c.requestsTotal,
		c.requestDuration,
		c.requestsInFlight,
		c.requestSize,
		c.responseSize,
		c.previewRequestsTotal,
		c.apiRequestsTotal,
		c.apiErrorsTotal,
		c.errorsTotal,
		c.activeConnections,
		c.idleConnectionsClosedTotal,
		c.connectionRejectionsTotal,
		c.goroutinesCurrent,
		c.scenariosMatched,
		c.scenarioRequests,
		c.scenarioResponseTime,
		c.scenariosLoaded,
		c.chaosInjected,
		c.rateLimitExceeded,
		c.rateLimitWaitTime,
		c.perClientRateLimitExceeded,
		c.perClientRateLimitWaitTime,
		c.perClientRateLimitActive,
		c.perClientRateLimitEvictions,
		c.replayRequestsTotal,
		c.replayRequestsFailed,
		c.replayDuration,
		c.replayBehindOriginal,
		c.streamingActive,
		c.streamingBytesTotal,
		c.configReloadTotal,
		c.configReloadDuration,
		c.configLastReloadStatus,
		c.scenarioShardingCacheHit,
		c.scenarioShardingCacheMiss,
		c.scenarioShardingFallback,
		c.scenarioShardingShardsUsed,
		c.storageRotationTotal,
		c.storageRotationDuration,
		c.storageRotationActive,
		c.requestTimeoutsTotal,
		c.requestCancellationsTotal,
		c.requestContextCancellationsByReason,
		c.storageBackpressureRejected,
		c.storageQueueDrained,
		c.storageQueueLength,
		c.scriptPoolRejected,
		c.scriptPoolLength,
		c.scriptPoolWorkers,
		c.circuitBreakerState,
		c.circuitBreakerTransitions,
		c.circuitBreakerFailures,
		c.storageDiskUsageBytes,
		c.storageDiskAvailableBytes,
		c.storageDiskWarningsTotal,
		c.storageDiskErrorsTotal,
		c.tlsCertificateExpiryDays,
		c.adaptiveTimeoutCurrent,
		c.previewRequestsRejected,
		c.previewClientsActive,
	)

	return c, nil
}

// RecordRequest increments the counter for ICAP requests by method.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRequest(method string) {
	c.RecordRequestForServer(defaultServerMetricLabel, method)

}

// RecordRequestForServer increments the ICAP request counter by server and method.
func (c *Collector) RecordRequestForServer(server, method string) {
	c.requestsTotal.WithLabelValues(normalizedMetricLabel(server), method).Inc()
}

// RecordRequestDuration records the duration of processing a request.
// The duration is recorded in seconds for the given ICAP method.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRequestDuration(method string, duration time.Duration) {
	c.RecordRequestDurationForServer(defaultServerMetricLabel, method, duration)
}

// RecordRequestDurationForServer records request processing duration by server and method.
func (c *Collector) RecordRequestDurationForServer(server, method string, duration time.Duration) {
	c.requestDuration.WithLabelValues(normalizedMetricLabel(server), method).Observe(duration.Seconds())
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

// RecordResponseSize records the size of a response body in bytes.
//
// This method is safe for concurrent use.
func (c *Collector) RecordResponseSize(method string, sizeBytes int64) {
	c.RecordResponseSizeForServer(defaultServerMetricLabel, method, sizeBytes)
}

// RecordResponseSizeForServer records response body size by server and method.
func (c *Collector) RecordResponseSizeForServer(server, method string, sizeBytes int64) {
	c.responseSize.WithLabelValues(normalizedMetricLabel(server), method).Observe(float64(sizeBytes))
}

// RecordPreviewRequest increments the counter for preview requests.
// The previewUsed parameter indicates whether preview mode was actually used (true) or not (false).
//
// This method is safe for concurrent use.
func (c *Collector) RecordPreviewRequest(method string, previewUsed bool) {
	c.RecordPreviewRequestForServer(defaultServerMetricLabel, method, previewUsed)
}

// RecordPreviewRequestForServer increments the preview request counter.
func (c *Collector) RecordPreviewRequestForServer(server, method string, previewUsed bool) {
	previewUsedStr := "false"
	if previewUsed {
		previewUsedStr = "true"
	}
	c.previewRequestsTotal.WithLabelValues(normalizedMetricLabel(server), method, previewUsedStr).Inc()
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

// RecordScenarioMatched increments the counter for the given scenario name.
// This tracks how often each mock scenario is matched.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioMatched(scenario string) {
	c.RecordScenarioMatchedForServer(defaultServerMetricLabel, scenario)

}

// RecordScenarioMatchedForServer increments the scenario match counter by server.
func (c *Collector) RecordScenarioMatchedForServer(server, scenario string) {
	server = normalizedMetricLabel(server)
	scenario = normalizedMetricLabel(scenario)
	c.scenariosMatched.WithLabelValues(server, scenario).Inc()
}

// RecordScenarioRequest records a matched scenario request and rolling latency stats.
// The response label should be a response/template name when available, or a status code.
// User-supplied reserved labels are escaped before cardinality admission.
// New (scenario, response) pairs beyond the cardinality cap are aggregated into
// the reserved __overflow__ labels before any scenario Prometheus vector is
// touched. This keeps request counters and latency gauges bounded and consistent.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScenarioRequest(scenario, response string, duration time.Duration) {
	c.RecordScenarioRequestForServer(defaultServerMetricLabel, scenario, response, duration)
}

// RecordScenarioRequestForServer records a matched scenario request by server.
func (c *Collector) RecordScenarioRequestForServer(server, scenario, response string, duration time.Duration) {
	server = normalizedMetricLabel(server)
	scenario = normalizedMetricLabel(scenario)
	response = normalizedMetricLabel(response)
	observation := c.scenarioLatencyWindow.observe(server, scenario, response, duration.Seconds())
	c.scenariosMatched.WithLabelValues(observation.server, observation.scenario).Inc()
	c.scenarioRequests.WithLabelValues(observation.server, observation.scenario, observation.response).Inc()
	for _, stat := range observation.stats {
		c.scenarioResponseTime.WithLabelValues(
			observation.server,
			observation.scenario,
			observation.response,
			stat.name,
		).Set(stat.value)
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
	c.RecordScenarioRequestForServer(server, fallbackScenarioMetricLabel, response, duration)
}

// RecordChaosInjected increments the counter for chaos injections.
// Common chaos types include "latency", "error", "timeout", "connection_drop".
//
// This method is safe for concurrent use.
func (c *Collector) RecordChaosInjected(chaosType string) {
	c.chaosInjected.WithLabelValues(chaosType).Inc()
}

// RecordRateLimitExceeded increments the counter for rate limit exceeded events
// for the given client identifier.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRateLimitExceeded(_ string) {
	c.RecordRateLimitExceededForServer(defaultServerMetricLabel)
}

// RecordRateLimitExceededForServer increments bounded rate-limit events by server.
func (c *Collector) RecordRateLimitExceededForServer(server string) {
	c.rateLimitExceeded.WithLabelValues(normalizedMetricLabel(server)).Inc()
}

// RecordRateLimitWaitTime records the time a request waited due to rate limiting.
//
// This method is safe for concurrent use.
func (c *Collector) RecordRateLimitWaitTime(_ string, waitTime time.Duration) {
	c.RecordRateLimitWaitTimeForServer(defaultServerMetricLabel, waitTime)
}

// RecordRateLimitWaitTimeForServer records rate-limit wait time by server.
func (c *Collector) RecordRateLimitWaitTimeForServer(server string, waitTime time.Duration) {
	c.rateLimitWaitTime.WithLabelValues(normalizedMetricLabel(server)).Observe(waitTime.Seconds())
}

// RecordPerClientRateLimitExceeded increments the counter for per-client
// rate limit exceeded events.
//
// Note: This metric does not include client IP label to prevent high cardinality
// in Prometheus when many unique clients are being rate limited.
//
// This method is safe for concurrent use.
func (c *Collector) RecordPerClientRateLimitExceeded(_ string) {
	c.RecordPerClientRateLimitExceededForServer(defaultServerMetricLabel)
}

// RecordPerClientRateLimitExceededForServer increments per-client events by server.
func (c *Collector) RecordPerClientRateLimitExceededForServer(server string) {
	c.perClientRateLimitExceeded.WithLabelValues(normalizedMetricLabel(server)).Inc()
}

// RecordPerClientRateLimitWaitTime records the time a request waited due
// to per-client rate limiting.
//
// Note: This metric does not include client IP label to prevent high cardinality
// in Prometheus when many unique clients are being rate limited.
//
// This method is safe for concurrent use.
func (c *Collector) RecordPerClientRateLimitWaitTime(waitTime time.Duration) {
	c.RecordPerClientRateLimitWaitTimeForServer(defaultServerMetricLabel, waitTime)
}

// RecordPerClientRateLimitWaitTimeForServer records per-client wait time by server.
func (c *Collector) RecordPerClientRateLimitWaitTimeForServer(server string, waitTime time.Duration) {
	c.perClientRateLimitWaitTime.WithLabelValues(normalizedMetricLabel(server)).Observe(waitTime.Seconds())
}

// SetPerClientRateLimitActive sets the gauge tracking the current number
// of active clients in the per-client rate limiter.
//
// This method is safe for concurrent use.
func (c *Collector) SetPerClientRateLimitActive(count int) {
	c.SetPerClientRateLimitActiveForServer(defaultServerMetricLabel, count)
}

// SetPerClientRateLimitActiveForServer sets active per-client limiters by server.
func (c *Collector) SetPerClientRateLimitActiveForServer(server string, count int) {
	c.perClientRateLimitActive.WithLabelValues(normalizedMetricLabel(server)).Set(float64(count))
}

// IncPerClientRateLimitEvictions increments the counter for client evictions
// from the per-client rate limiter cache.
//
// This method is safe for concurrent use.
func (c *Collector) IncPerClientRateLimitEvictions() {
	c.IncPerClientRateLimitEvictionsForServer(defaultServerMetricLabel)
}

// IncPerClientRateLimitEvictionsForServer increments client evictions by server.
func (c *Collector) IncPerClientRateLimitEvictionsForServer(server string) {
	c.perClientRateLimitEvictions.WithLabelValues(normalizedMetricLabel(server)).Inc()
}

// RecordReplayRequest increments the counter for replayed requests.
//
// This method is safe for concurrent use.
func (c *Collector) RecordReplayRequest() {
	c.replayRequestsTotal.Inc()
}

// RecordReplayFailure increments the counter for failed replay requests.
//
// This method is safe for concurrent use.
func (c *Collector) RecordReplayFailure() {
	c.replayRequestsFailed.Inc()
}

// RecordReplayDuration records the duration of a replay operation.
//
// This method is safe for concurrent use.
func (c *Collector) RecordReplayDuration(duration time.Duration) {
	c.replayDuration.Observe(duration.Seconds())
}

// SetReplayBehindOriginal sets the gauge indicating how far behind
// the replay is compared to the original timeline.
//
// This method is safe for concurrent use.
func (c *Collector) SetReplayBehindOriginal(seconds float64) {
	c.replayBehindOriginal.Set(seconds)
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

// RecordConfigReloadDuration records the duration of a configuration reload operation.
//
// This method is safe for concurrent use.
func (c *Collector) RecordConfigReloadDuration(duration time.Duration) {
	c.configReloadDuration.Observe(duration.Seconds())
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

// RecordStorageRotationDuration records the duration of a file rotation operation.
//
// This method is safe for concurrent use.
func (c *Collector) RecordStorageRotationDuration(duration time.Duration) {
	c.storageRotationDuration.Observe(duration.Seconds())
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

// RecordScriptPoolRejected increments the counter for script executions rejected
// due to the script pool queue being full.
//
// This method is safe for concurrent use.
func (c *Collector) RecordScriptPoolRejected(queueSize, maxQueueSize float64) {
	c.scriptPoolRejected.WithLabelValues(
		fmt.Sprintf("%.0f", queueSize),
		fmt.Sprintf("%.0f", maxQueueSize),
	).Inc()
}

// SetScriptPoolQueueLength sets the gauge for the current number of items in the script pool queue.
//
// This method is safe for concurrent use.
func (c *Collector) SetScriptPoolQueueLength(length float64) {
	c.scriptPoolLength.Set(length)
}

// SetScriptPoolWorkers sets the gauge for the current number of active script worker goroutines.
//
// This method is safe for concurrent use.
func (c *Collector) SetScriptPoolWorkers(workers float64) {
	c.scriptPoolWorkers.Set(workers)
}

// SetCircuitBreakerState sets the gauge for the current circuit breaker state.
// State values: "closed" = 0, "half-open" = 0.5, "open" = 1.
//
// This method is safe for concurrent use.
func (c *Collector) SetCircuitBreakerState(component, state string) {
	value := 0.0
	switch state {
	case "half-open":
		value = 0.5
	case "open":
		value = 1.0
	}
	c.circuitBreakerState.WithLabelValues(component).Set(value)
}

// RecordCircuitBreakerTransition increments the counter for circuit breaker state transitions.
//
// This method is safe for concurrent use.
func (c *Collector) RecordCircuitBreakerTransition(component, fromState, toState string) {
	c.circuitBreakerTransitions.WithLabelValues(component, fromState, toState).Inc()
}

// RecordCircuitBreakerFailure increments the counter for circuit breaker failures.
//
// This method is safe for concurrent use.
func (c *Collector) RecordCircuitBreakerFailure(component string) {
	c.circuitBreakerFailures.WithLabelValues(component).Inc()
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

// RecordPreviewRequestRejected increments the counter for preview requests
// rejected due to rate limiting.
//
// This method is safe for concurrent use.
func (c *Collector) RecordPreviewRequestRejected(_ string) {
	c.previewRequestsRejected.WithLabelValues().Inc()
}

// SetPreviewClientsActive sets the gauge for the current number
// of active clients tracked by the preview rate limiter.
//
// This method is safe for concurrent use.
func (c *Collector) SetPreviewClientsActive(count int) {
	c.previewClientsActive.Set(float64(count))
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
