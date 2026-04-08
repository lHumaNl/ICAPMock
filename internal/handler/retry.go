// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"net"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// JitterStrategy defines the jitter strategy for retry backoff.
type JitterStrategy int

const (
	// JitterNone disables jitter (deterministic backoff).
	JitterNone JitterStrategy = iota
	// JitterFull adds full random jitter between 0 and backoff duration.
	JitterFull
	// JitterEqual adds equal jitter: backoff ± (backoff * percent / 2).
	JitterEqual
)

// IsRetryable checks if an error is transient and should trigger a retry.
// Transient errors include network timeouts, connection errors, temporary errors,
// and certain system call errors that may resolve on retry.
//
// Non-retryable errors include validation errors, not found errors, and
// other permanent errors that will not resolve on retry.
//
// Examples of retryable errors:
//   - net timeout errors (net.Error with Timeout() == true)
//   - connection reset/refused errors
//   - syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ETIMEDOUT
//   - errors marked as temporary (errors.Is(err, errTemporary))
//
// Examples of non-retryable errors:
//   - validation errors
//   - not found errors
//   - authentication/authorization errors
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for network timeouts
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	// Check for specific system call errors
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}

	// Check for context timeout (but not explicit cancellation)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for generic temporary error marker
	if errors.Is(err, errTemporary) {
		return true
	}

	// Check for connection-related string errors (fallback)
	errMsg := err.Error()
	return containsAny(errMsg, []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"network is unreachable",
		"temporary failure",
		"timeout",
	})
}

// errTemporary is a sentinel error for marking errors as temporary.
var errTemporary = errors.New("temporary error")

// containsAny checks if the message contains any of the substrings.
func containsAny(msg string, substrings []string) bool {
	for _, s := range substrings {
		if contains(msg, s) {
			return true
		}
	}
	return false
}

// contains is a simple string contains helper.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

// findSubstring checks if substr exists in s.
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// RetryConfig holds configuration for the retry middleware.
type RetryConfig struct {
	MetricsCollector  *metrics.Collector
	Logger            *slog.Logger
	RetryableErrors   []error
	MaxRetries        int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	JitterStrategy    JitterStrategy
	JitterPercent     float64
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		BackoffMultiplier: 2.0,
		JitterStrategy:    JitterEqual,
		JitterPercent:     0.25,
		Logger:            slog.Default(),
	}
}

// RetryMiddleware wraps a handler with retry logic for transient errors.
// It implements exponential backoff between retries and tracks retry attempts
// via Prometheus metrics.
//
// The middleware checks if an error is retryable using IsRetryable() function.
// If retryable, it waits with exponential backoff and retries the operation.
// After MaxRetries attempts, it returns the last error.
//
// Non-retryable errors are returned immediately without retry.
//
// Context cancellation is respected - if the context is canceled during
// backoff, the retry loop stops immediately.
//
// Example:
//
//	cfg := handler.RetryConfig{
//	    MaxRetries: 3,
//	    InitialBackoff: 100 * time.Millisecond,
//	    MetricsCollector: collector,
//	    Logger: logger,
//	}
//	middleware := handler.RetryMiddleware(cfg)
//	handler := middleware(baseHandler)
func RetryMiddleware(cfg RetryConfig) Middleware {
	applyRetryDefaults(&cfg)

	retryAttemptsTotal := initRetryMetrics(cfg)

	return func(next Handler) Handler {
		return WrapHandler(Func(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			rs := &retryState{cfg: &cfg, metrics: retryAttemptsTotal, component: req.Method}
			return rs.execute(ctx, req, next)
		}), next.Method())
	}
}

// applyRetryDefaults fills zero-value fields in cfg with defaults.
func applyRetryDefaults(cfg *RetryConfig) {
	defaults := DefaultRetryConfig()
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaults.InitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaults.MaxBackoff
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = defaults.BackoffMultiplier
	}
	if cfg.JitterPercent < 0 || cfg.JitterPercent > 1.0 {
		cfg.JitterPercent = defaults.JitterPercent
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
}

// initRetryMetrics initializes Prometheus metrics for retry tracking.
func initRetryMetrics(cfg RetryConfig) *prometheus.CounterVec {
	if cfg.MetricsCollector == nil {
		return nil
	}
	retryAttemptsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "icap",
			Name:      "retry_attempts_total",
			Help:      "Total number of retry attempts by component and status.",
		},
		[]string{"component", "status", "error_type"},
	)
	if err := prometheus.Register(retryAttemptsTotal); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			retryAttemptsTotal = are.ExistingCollector.(*prometheus.CounterVec) //nolint:errcheck
		}
	}
	return retryAttemptsTotal
}

// retryState holds per-request retry state.
type retryState struct {
	cfg       *RetryConfig
	metrics   *prometheus.CounterVec
	component string
}

func (rs *retryState) execute(ctx context.Context, req *icap.Request, next Handler) (*icap.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= rs.cfg.MaxRetries; attempt++ {
		resp, err := next.Handle(ctx, req)

		if err == nil {
			if attempt > 0 {
				rs.recordMetric("success", "")
				if rs.cfg.Logger != nil {
					rs.cfg.Logger.Debug("request succeeded after retry",
						"component", rs.component, "attempt", attempt+1, "uri", req.URI)
				}
			}
			return resp, nil
		}

		lastErr = err
		retryable := rs.isRetryable(err)

		if !retryable || attempt >= rs.cfg.MaxRetries {
			rs.recordFinalFailure(ctx, req, err, retryable, attempt)
			return nil, err
		}

		if waitErr := rs.waitBackoff(ctx, req, err, attempt); waitErr != nil {
			return nil, waitErr
		}
	}
	return nil, lastErr
}

func (rs *retryState) isRetryable(err error) bool {
	if rs.cfg.RetryableErrors == nil {
		return IsRetryable(err)
	}
	for _, retryableErr := range rs.cfg.RetryableErrors {
		if errors.Is(err, retryableErr) {
			return true
		}
	}
	return false
}

func (rs *retryState) recordMetric(status, errorType string) {
	if rs.cfg.MetricsCollector != nil && rs.metrics != nil {
		rs.metrics.WithLabelValues(rs.component, status, errorType).Inc()
	}
}

func (rs *retryState) recordFinalFailure(ctx context.Context, req *icap.Request, err error, retryable bool, attempt int) {
	status := "non_retryable"
	if retryable {
		status = "exhausted"
	}
	rs.recordMetric(status, getErrorType(err))

	if rs.cfg.Logger != nil {
		logLevel := slog.LevelError
		if !retryable {
			logLevel = slog.LevelInfo
		}
		rs.cfg.Logger.Log(ctx, logLevel, "request failed",
			"component", rs.component, "attempts", attempt+1,
			"retryable", retryable, "error", err, "uri", req.URI)
	}
}

func (rs *retryState) waitBackoff(ctx context.Context, req *icap.Request, err error, attempt int) error {
	backoff := calculateBackoffWithJitter(rs.cfg.InitialBackoff, rs.cfg.BackoffMultiplier, rs.cfg.MaxBackoff, attempt, rs.cfg.JitterStrategy, rs.cfg.JitterPercent)

	rs.recordMetric("retry", getErrorType(err))

	if rs.cfg.Logger != nil {
		rs.cfg.Logger.Warn("request failed, retrying",
			"component", rs.component, "attempt", attempt+1,
			"max_retries", rs.cfg.MaxRetries+1, "backoff", backoff,
			"error", err, "uri", req.URI)
	}

	select {
	case <-time.After(backoff):
		return nil
	case <-ctx.Done():
		rs.recordMetric("canceled", getErrorType(ctx.Err()))
		if rs.cfg.Logger != nil {
			rs.cfg.Logger.Warn("retry canceled by context",
				"component", rs.component, "attempt", attempt+1, "error", ctx.Err())
		}
		return ctx.Err()
	}
}

// calculateBackoffWithJitter calculates the backoff duration with jitter for the given attempt.
// It implements exponential backoff with a maximum cap and applies jitter to prevent
// thundering herd problem when multiple clients retry simultaneously.
func calculateBackoffWithJitter(initial time.Duration, multiplier float64, maxBackoff time.Duration, attempt int, strategy JitterStrategy, jitterPercent float64) time.Duration {
	// Calculate exponential backoff: initial * (multiplier ^ attempt)
	backoff := time.Duration(float64(initial) * pow(multiplier, attempt))

	// Cap at max backoff
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Apply jitter based on strategy
	backoff = applyJitter(backoff, strategy, jitterPercent)

	return backoff
}

// applyJitter applies jitter to the backoff duration based on the specified strategy.
func applyJitter(backoff time.Duration, strategy JitterStrategy, jitterPercent float64) time.Duration {
	if strategy == JitterNone || backoff == 0 {
		return backoff
	}

	switch strategy { //nolint:exhaustive // JitterNone handled above
	case JitterFull:
		// Full jitter: random value between 0 and backoff
		// This is the most aggressive jitter strategy
		jitter := rand.Float64() //nolint:gosec // crypto not needed here
		return time.Duration(float64(backoff) * jitter)

	case JitterEqual:
		// Equal jitter: backoff ± (backoff * percent / 2)
		// This provides centered jitter around the original backoff value
		if jitterPercent <= 0 || jitterPercent > 1.0 {
			jitterPercent = 0.25 // Default to 25%
		}
		jitterRange := float64(backoff) * jitterPercent
		jitter := (rand.Float64() - 0.5) * 2 * jitterRange //nolint:gosec // crypto not needed here
		return time.Duration(float64(backoff) + jitter)

	default:
		return backoff
	}
}

// pow calculates base^exp for float64.
func pow(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// getErrorType returns a string representation of the error type for metrics.
func getErrorType(err error) string {
	if err == nil {
		return "none"
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, syscall.ECONNRESET):
		return "conn_reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "conn_refused"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "timed_out"
	case errors.Is(err, net.ErrClosed):
		return "network_error"
	default:
		// Use error type name as fallback
		return "unknown"
	}
}

// GetRetryMetrics returns the retry attempts counter metric.
// This can be used to register the metric with a Prometheus registry.
func GetRetryMetrics(retryConfig RetryConfig) *prometheus.CounterVec {
	if retryConfig.MetricsCollector == nil {
		return nil
	}

	// Recreate the metric for registration
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "icap",
			Name:      "retry_attempts_total",
			Help:      "Total number of retry attempts by component and status.",
		},
		[]string{"component", "status", "error_type"},
	)
}
