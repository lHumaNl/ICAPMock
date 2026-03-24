package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	ErrMaxAttemptsExceeded = errors.New("max retry attempts exceeded")
	ErrContextCanceled     = errors.New("context canceled")
	ErrNonRetryableError   = errors.New("non-retryable error")
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	Multiplier      float64
	Jitter          float64
	RetryableErrors []error
}

// DefaultRetryConfig returns a conservative default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.1,
		RetryableErrors: []error{},
	}
}

// DoWithRetry executes a function with exponential backoff retry
func DoWithRetry(ctx context.Context, config RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(config, attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("%w: %v", ErrContextCanceled, ctx.Err())
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if !shouldRetry(err, config) {
			return err
		}
	}

	return fmt.Errorf("%w: %v", ErrMaxAttemptsExceeded, lastErr)
}

// DoWithRetryHTTP executes an HTTP request with retry logic
func DoWithRetryHTTP(ctx context.Context, config RetryConfig, httpClient *http.Client, req *http.Request) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(config, attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v", ErrContextCanceled, ctx.Err())
			}
		}

		retryReq := req.Clone(ctx)
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to get request body for retry: %w", err)
			}
			retryReq.Body = body
		} else if req.Body != nil {
			return nil, fmt.Errorf("cannot retry request with body without GetBody function")
		}

		resp, err := httpClient.Do(retryReq)
		if err != nil {
			if !shouldRetryError(err, config) {
				return nil, err
			}
			lastErr = err
			continue
		}

		if shouldRetryResponse(resp) {
			lastResp = resp
			ioCopyAndClose(resp.Body)
			lastErr = fmt.Errorf("HTTP error: status %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	if lastResp != nil {
		return lastResp, fmt.Errorf("%w: %v", ErrMaxAttemptsExceeded, lastErr)
	}

	return nil, fmt.Errorf("%w: %v", ErrMaxAttemptsExceeded, lastErr)
}

// calculateBackoff calculates the delay for a given retry attempt with exponential backoff and jitter
func calculateBackoff(config RetryConfig, attempt int) time.Duration {
	baseDelay := config.InitialDelay

	exponentialFactor := 1.0
	for i := 1; i < attempt; i++ {
		exponentialFactor *= config.Multiplier
	}

	delay := time.Duration(float64(baseDelay) * exponentialFactor)

	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}

	if config.Jitter > 0 {
		jitterRange := float64(delay) * config.Jitter
		jitter := (rand.Float64()*2 - 1) * jitterRange
		delay = time.Duration(float64(delay) + jitter)
		if delay < 0 {
			delay = 0
		}
	}

	return delay
}

// shouldRetry determines if an error should be retried
func shouldRetry(err error, config RetryConfig) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrNonRetryableError) {
		return false
	}

	for _, retryableErr := range config.RetryableErrors {
		if errors.Is(err, retryableErr) {
			return true
		}
	}

	return shouldRetryError(err, config)
}

// shouldRetryError determines if an error is retryable based on error type
func shouldRetryError(err error, config RetryConfig) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}

		if strings.Contains(netErr.Error(), "connection refused") {
			return true
		}

		if strings.Contains(netErr.Error(), "connection reset") {
			return true
		}

		if strings.Contains(netErr.Error(), "EOF") {
			return true
		}

		if netErr.Temporary() {
			return true
		}
	}

	if strings.Contains(err.Error(), "dial tcp") {
		return true
	}

	if strings.Contains(err.Error(), "no such host") {
		return false
	}

	if strings.Contains(err.Error(), "TLS handshake") {
		return false
	}

	return true
}

// shouldRetryResponse determines if an HTTP response status code indicates a retryable error
func shouldRetryResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}

	statusCode := resp.StatusCode

	if statusCode >= 500 && statusCode <= 599 {
		return statusCode != http.StatusServiceUnavailable
	}

	return false
}

// ioCopyAndClose copies and closes the response body
func ioCopyAndClose(r io.ReadCloser) {
	if r == nil {
		return
	}
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
