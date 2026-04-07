// Copyright 2026 ICAP Mock

package middleware

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/circuitbreaker"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/ratelimit"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// CircuitBreakerConfig holds configuration for the circuit breaker used by StorageMiddleware.
type CircuitBreakerConfig struct {
	Metrics          *metrics.Collector
	Component        string
	MaxFailures      int
	ResetTimeout     time.Duration
	SuccessThreshold int
	Enabled          bool
}

// DefaultCircuitBreakerConfig returns the default circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Enabled:          true,
		MaxFailures:      5,
		ResetTimeout:     30 * time.Second,
		SuccessThreshold: 3,
		Component:        "storage",
	}
}

// toCircuitBreakerConfig converts to the circuitbreaker package Config.
func (cfg CircuitBreakerConfig) toCircuitBreakerConfig() circuitbreaker.Config {
	c := circuitbreaker.DefaultConfig()
	if cfg.MaxFailures > 0 {
		c.FailureThreshold = cfg.MaxFailures
	}
	if cfg.ResetTimeout > 0 {
		c.OpenTimeout = cfg.ResetTimeout
	}
	if cfg.SuccessThreshold > 0 {
		c.SuccessThreshold = cfg.SuccessThreshold
		c.HalfOpenMaxRequests = cfg.SuccessThreshold
	}
	c.Enabled = cfg.Enabled
	return c
}

// RateLimiterMiddleware returns middleware that checks rate limit before processing.
// If rate limit is exceeded, returns ICAP 429 (Too Many Requests).
func RateLimiterMiddleware(limiter ratelimit.Limiter) handler.Middleware {
	return func(next handler.Handler) handler.Handler {
		return handler.WrapHandler(handler.HandlerFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			if !limiter.Allow() {
				resp := icap.NewResponse(429)
				resp.SetHeader("X-RateLimit-Remaining", "0")
				resp.SetHeader("Connection", "close")
				return resp, nil
			}
			return next.Handle(ctx, req)
		}), next.Method())
	}
}

// PanicRecoveryMiddleware returns middleware that recovers from panics in handlers.
// If a panic occurs, it logs the error and returns a 500 Internal Server Error response.
func PanicRecoveryMiddleware(logger *slog.Logger) handler.Middleware {
	return func(next handler.Handler) handler.Handler {
		return handler.WrapHandler(handler.HandlerFunc(func(ctx context.Context, req *icap.Request) (resp *icap.Response, err error) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("panic recovered in handler",
						"request_id", util.RequestIDFromContext(ctx),
						"error", r,
						"method", req.Method,
						"uri", req.URI,
					)
					resp = icap.NewResponse(500)
					resp.SetHeader("Connection", "close")
					err = nil
				}
			}()
			return next.Handle(ctx, req)
		}), next.Method())
	}
}

// storageJob represents a job for the storage worker pool.
type storageJob struct {
	ctx context.Context
	req *storage.StoredRequest
}

// StorageMiddlewareConfig holds configuration for the storage middleware.
type StorageMiddlewareConfig struct {
	CircuitBreaker CircuitBreakerConfig
	Metrics        *metrics.Collector
	Workers        int
	QueueSize      int
}

// DefaultStorageMiddlewareConfig returns the default configuration.
func DefaultStorageMiddlewareConfig() StorageMiddlewareConfig {
	return StorageMiddlewareConfig{
		Workers:        4,
		QueueSize:      1000,
		CircuitBreaker: DefaultCircuitBreakerConfig(),
	}
}

// StorageMiddleware manages a pool of workers for async storage operations.
type StorageMiddleware struct {
	ctx            context.Context
	store          storage.Storage
	jobs           chan *storageJob
	cancel         context.CancelFunc
	logger         *slog.Logger
	circuitBreaker *circuitbreaker.CircuitBreaker
	metrics        *metrics.Collector
	wg             sync.WaitGroup
	maxQueueSize   int
	rejectedCount  int64
	stopped        atomic.Bool
}

// StorageMiddlewareWithPool returns middleware that saves requests to storage
// using a bounded worker pool.
func StorageMiddlewareWithPool(store storage.Storage, logger *slog.Logger, cfg StorageMiddlewareConfig) *StorageMiddleware {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel managed elsewhere
	jobs := make(chan *storageJob, cfg.QueueSize)

	var cb *circuitbreaker.CircuitBreaker
	if cfg.CircuitBreaker.Enabled {
		component := cfg.CircuitBreaker.Component
		if component == "" {
			component = "storage"
		}
		cbCfg := cfg.CircuitBreaker.toCircuitBreakerConfig()
		var recorder circuitbreaker.MetricsRecorder
		if cfg.Metrics != nil {
			recorder = cfg.Metrics
		}
		cb = circuitbreaker.NewCircuitBreaker(component, cbCfg, logger, recorder)
	}

	m := &StorageMiddleware{
		jobs:           jobs,
		ctx:            ctx,
		cancel:         cancel,
		store:          store,
		logger:         logger,
		circuitBreaker: cb,
		metrics:        cfg.Metrics,
		maxQueueSize:   cfg.QueueSize,
	}

	for i := 0; i < cfg.Workers; i++ {
		m.wg.Add(1)
		go m.storageWorker()
	}

	return m
}

// Wrap returns a handler.Middleware function that wraps handlers with storage functionality.
func (m *StorageMiddleware) Wrap(next handler.Handler) handler.Handler {
	return handler.WrapHandler(handler.HandlerFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		start := time.Now()
		resp, err := next.Handle(ctx, req)

		status := 500
		if resp != nil {
			status = resp.StatusCode
		}
		processingTime := time.Since(start)
		sr := storage.FromICAPRequest(req, status, processingTime)

		if m.stopped.Load() {
			m.logger.Warn("storage middleware stopped, dropping request",
				"request_id", util.RequestIDFromContext(ctx),
				"method", req.Method,
				"uri", req.URI,
			)
		} else {
			select {
			case m.jobs <- &storageJob{ctx: ctx, req: sr}:
				if m.metrics != nil {
					m.metrics.SetStorageQueueLength(len(m.jobs))
				}
			default:
				rejected := atomic.AddInt64(&m.rejectedCount, 1)
				currentQueueSize := len(m.jobs)

				m.logger.Warn("storage queue full, dropping request",
					"request_id", util.RequestIDFromContext(ctx),
					"method", req.Method,
					"uri", req.URI,
					"rejected_count", rejected,
					"queue_size", currentQueueSize,
					"max_queue_size", m.maxQueueSize,
				)

				if m.metrics != nil {
					m.metrics.RecordStorageBackpressureRejected(currentQueueSize, m.maxQueueSize)
				}
			}
		}

		return resp, err
	}), next.Method())
}

// Shutdown gracefully stops all workers and waits for them to complete.
func (m *StorageMiddleware) Shutdown(ctx context.Context) error {
	// Mark as stopped so no new sends to the jobs channel occur.
	m.stopped.Store(true)
	// Cancel worker context and close the jobs channel so workers drain and exit.
	m.cancel()
	close(m.jobs)

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *StorageMiddleware) storageWorker() {
	defer m.wg.Done()
	for job := range m.jobs {
		draining := m.ctx.Err() != nil
		m.processStorageJob(job, draining)
	}
}

func (m *StorageMiddleware) processStorageJob(job *storageJob, draining bool) {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("panic in storage worker", "error", r)
			if m.circuitBreaker != nil {
				m.circuitBreaker.RecordResult(false)
			}
		}
	}()

	var err error
	if m.circuitBreaker != nil {
		err = m.circuitBreaker.Call(job.ctx, func() error {
			return m.store.SaveRequest(job.ctx, job.req)
		})
		if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
			return
		}
	} else {
		err = m.store.SaveRequest(job.ctx, job.req)
	}

	if err != nil {
		m.logger.WarnContext(job.ctx, "failed to save request",
			"error", err,
			"request_id", util.RequestIDFromContext(job.ctx),
			"stored_request_id", job.req.ID,
		)
	}

	if m.metrics != nil {
		if draining {
			m.metrics.RecordStorageQueueDrained(1)
		}
		m.metrics.SetStorageQueueLength(len(m.jobs))
	}
}

// Middleware returns a handler.Middleware function from this StorageMiddleware.
func (m *StorageMiddleware) Middleware() handler.Middleware {
	return m.Wrap
}

// GetCircuitBreaker returns the circuit breaker instance for monitoring.
// Returns nil if the circuit breaker is not enabled.
func (m *StorageMiddleware) GetCircuitBreaker() *circuitbreaker.CircuitBreaker {
	return m.circuitBreaker
}

// SetMetrics sets the Prometheus metrics collector.
func (m *StorageMiddleware) SetMetrics(collector *metrics.Collector) {
	m.metrics = collector
}

// NewStorageMiddlewareWithPool creates a new StorageMiddleware with a worker pool.
func NewStorageMiddlewareWithPool(store storage.Storage, logger *slog.Logger, cfg StorageMiddlewareConfig) (*StorageMiddleware, error) {
	if store == nil {
		return nil, errors.New("store cannot be nil")
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultStorageMiddlewareConfig().Workers
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultStorageMiddlewareConfig().QueueSize
	}

	return StorageMiddlewareWithPool(store, logger, cfg), nil
}

// LegacyStorageMiddleware returns middleware that saves requests to storage.
//

// ChainMiddleware applies multiple middleware to a handler in order.
func ChainMiddleware(h handler.Handler, middlewares ...handler.Middleware) handler.Handler {
	return handler.Use(h, middlewares...)
}
