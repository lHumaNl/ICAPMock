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
		// Temporary network errors are retryable
		if netErr.Temporary() {
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
	if containsAny(errMsg, []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"network is unreachable",
		"temporary failure",
		"timeout",
	}) {
		return true
	}

	return false
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
	// Apply defaults
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = DefaultRetryConfig().MaxRetries
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = DefaultRetryConfig().InitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultRetryConfig().MaxBackoff
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = DefaultRetryConfig().BackoffMultiplier
	}
	if cfg.JitterPercent < 0 || cfg.JitterPercent > 1.0 {
		cfg.JitterPercent = DefaultRetryConfig().JitterPercent
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Initialize retry metrics
	var retryAttemptsTotal *prometheus.CounterVec
	if cfg.MetricsCollector != nil {
		retryAttemptsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "icap",
				Name:      "retry_attempts_total",
				Help:      "Total number of retry attempts by component and status.",
			},
			[]string{"component", "status", "error_type"},
		)
		// Register the metric; ignore AlreadyRegisteredError for idempotency
		if err := prometheus.Register(retryAttemptsTotal); err != nil {
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				retryAttemptsTotal = are.ExistingCollector.(*prometheus.CounterVec) //nolint:errcheck
			}
		}
	}

	return func(next Handler) Handler {
		return WrapHandler(HandlerFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			component := req.Method

			var lastErr error
			for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
				// Execute handler
				resp, err := next.Handle(ctx, req)

				// Success on first attempt
				if attempt == 0 && err == nil {
					return resp, nil
				}

				// No error on retry
				if err == nil {
					// Record successful retry
					if cfg.MetricsCollector != nil && retryAttemptsTotal != nil {
						retryAttemptsTotal.WithLabelValues(component, "success", "").Inc()
					}
					if cfg.Logger != nil {
						cfg.Logger.Debug("request succeeded after retry",
							"component", component,
							"attempt", attempt+1,
							"uri", req.URI,
						)
					}
					return resp, nil
				}

				// Store the error
				lastErr = err

				// Check if error is retryable
				isRetryable := IsRetryable(err)
				if cfg.RetryableErrors != nil {
					// Use custom retryable errors list
					isRetryable = false
					for _, retryableErr := range cfg.RetryableErrors {
						if errors.Is(err, retryableErr) {
							isRetryable = true
							break
						}
					}
				}

				// If not retryable or max retries exceeded, return error
				if !isRetryable || attempt >= cfg.MaxRetries {
					// Record failed retry or non-retryable error
					if cfg.MetricsCollector != nil && retryAttemptsTotal != nil {
						status := "non_retryable"
						if isRetryable {
							status = "exhausted"
						}
						errorType := getErrorType(err)
						retryAttemptsTotal.WithLabelValues(component, status, errorType).Inc()
					}

					if cfg.Logger != nil {
						logLevel := slog.LevelError
						if !isRetryable {
							logLevel = slog.LevelInfo
						}
						cfg.Logger.Log(ctx, logLevel, "request failed",
							"component", component,
							"attempts", attempt+1,
							"retryable", isRetryable,
							"error", err,
							"uri", req.URI,
						)
					}

					return nil, err
				}

				// Calculate backoff duration with jitter
				backoff := calculateBackoffWithJitter(cfg.InitialBackoff, cfg.BackoffMultiplier, cfg.MaxBackoff, attempt, cfg.JitterStrategy, cfg.JitterPercent)

				// Record retry attempt
				if cfg.MetricsCollector != nil && retryAttemptsTotal != nil {
					errorType := getErrorType(err)
					retryAttemptsTotal.WithLabelValues(component, "retry", errorType).Inc()
				}

				if cfg.Logger != nil {
					cfg.Logger.Warn("request failed, retrying",
						"component", component,
						"attempt", attempt+1,
						"max_retries", cfg.MaxRetries+1,
						"backoff", backoff,
						"error", err,
						"uri", req.URI,
					)
				}

				// Wait with exponential backoff
				select {
				case <-time.After(backoff):
					// Continue to next retry
				case <-ctx.Done():
					// Context canceled, abort retry
					if cfg.MetricsCollector != nil && retryAttemptsTotal != nil {
						retryAttemptsTotal.WithLabelValues(component, "canceled", getErrorType(ctx.Err())).Inc()
					}
					if cfg.Logger != nil {
						cfg.Logger.Warn("retry canceled by context",
							"component", component,
							"attempt", attempt+1,
							"error", ctx.Err(),
						)
					}
					return nil, ctx.Err()
				}
			}

			// Should not reach here, but return last error if we do
			return nil, lastErr
		}), next.Method())
	}
}

// calculateBackoffWithJitter calculates the backoff duration with jitter for the given attempt.
// It implements exponential backoff with a maximum cap and applies jitter to prevent
// thundering herd problem when multiple clients retry simultaneously.
func calculateBackoffWithJitter(initial time.Duration, multiplier float64, max time.Duration, attempt int, strategy JitterStrategy, jitterPercent float64) time.Duration {
	// Calculate exponential backoff: initial * (multiplier ^ attempt)
	backoff := time.Duration(float64(initial) * pow(multiplier, attempt))

	// Cap at max backoff
	if backoff > max {
		backoff = max
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

	switch strategy {
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
