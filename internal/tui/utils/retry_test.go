// Copyright 2026 ICAP Mock

package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts to be 3, got %d", config.MaxAttempts)
	}

	if config.InitialDelay != 100*time.Millisecond {
		t.Errorf("Expected InitialDelay to be 100ms, got %v", config.InitialDelay)
	}

	if config.MaxDelay != 5*time.Second {
		t.Errorf("Expected MaxDelay to be 5s, got %v", config.MaxDelay)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier to be 2.0, got %f", config.Multiplier)
	}

	if config.Jitter != 0.1 {
		t.Errorf("Expected Jitter to be 0.1, got %f", config.Jitter)
	}
}

func TestDoWithRetry_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	callCount := 0

	err := DoWithRetry(ctx, config, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestDoWithRetry_RetryOnError(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	callCount := 0

	err := DoWithRetry(ctx, config, func() error {
		callCount++
		if callCount < 2 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}
}

func TestDoWithRetry_MaxAttemptsExceeded(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   1.5,
		Jitter:       0,
	}
	callCount := 0

	err := DoWithRetry(ctx, config, func() error {
		callCount++
		return errors.New("persistent error")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if !errors.Is(err, ErrMaxAttemptsExceeded) {
		t.Errorf("Expected ErrMaxAttemptsExceeded, got %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}
}

func TestDoWithRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   1.5,
		Jitter:       0,
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	callCount := 0
	err := DoWithRetry(ctx, config, func() error {
		callCount++
		time.Sleep(10 * time.Millisecond)
		return errors.New("error")
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if !errors.Is(err, ErrContextCanceled) {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}

	if callCount == 0 {
		t.Error("Expected at least 1 call")
	}
}

func TestDoWithRetry_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()
	callCount := 0

	err := DoWithRetry(ctx, config, func() error {
		callCount++
		return ErrNonRetryableError
	})

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if callCount != 1 {
		t.Errorf("Expected 1 call for non-retryable error, got %d", callCount)
	}
}

func TestCalculateBackoff(t *testing.T) {
	config := RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		Jitter:       0,
	}

	tests := []struct {
		attempt     int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{1, 100 * time.Millisecond, 100 * time.Millisecond},
		{2, 200 * time.Millisecond, 200 * time.Millisecond},
		{3, 400 * time.Millisecond, 400 * time.Millisecond},
		{4, 800 * time.Millisecond, 800 * time.Millisecond},
		{5, 1600 * time.Millisecond, 1600 * time.Millisecond},
		{6, 3200 * time.Millisecond, 3200 * time.Millisecond},
		{7, 5000 * time.Millisecond, 5000 * time.Millisecond},
		{8, 5000 * time.Millisecond, 5000 * time.Millisecond},
	}

	for _, tt := range tests {
		delay := calculateBackoff(config, tt.attempt)
		if delay < tt.expectedMin || delay > tt.expectedMax {
			t.Errorf("Attempt %d: expected delay between %v and %v, got %v", tt.attempt, tt.expectedMin, tt.expectedMax, delay)
		}
	}
}

func TestCalculateBackoff_WithJitter(t *testing.T) {
	config := RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}

	attempts := 100
	variance := make(map[int]bool)

	for i := 0; i < attempts; i++ {
		delay := calculateBackoff(config, 2)
		variance[int(delay/time.Millisecond)] = true
	}

	if len(variance) < 2 {
		t.Error("Expected jitter to create variance in delays")
	}
}

func TestDoWithRetryHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := DefaultRetryConfig()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	resp.Body.Close()
}

func TestDoWithRetryHTTP_RetryOn5xx(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   1.5,
		Jitter:       0,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Expected 3 calls, got %d", atomic.LoadInt32(&callCount))
	}

	resp.Body.Close()
}

func TestDoWithRetryHTTP_NoRetryOn4xx(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := DefaultRetryConfig()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for 4xx error, got %d", atomic.LoadInt32(&callCount))
	}

	resp.Body.Close()
}

func TestDoWithRetryHTTP_NoRetryOn503(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := DefaultRetryConfig()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for 503 error, got %d", atomic.LoadInt32(&callCount))
	}

	resp.Body.Close()
}

func TestDoWithRetryHTTP_RetryOnConnectionError(t *testing.T) {
	var callCount int32
	var shouldFail int32 = 1

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) < 2 && atomic.LoadInt32(&shouldFail) > 0 {
			atomic.StoreInt32(&shouldFail, 0)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("Server does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("Failed to hijack: %v", err)
			}
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   1.5,
		Jitter:       0,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	resp.Body.Close()
}

func TestDoWithRetryHTTP_WithBody(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}
		if count < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   1.5,
		Jitter:       0,
	}

	body := strings.NewReader("test body")
	bodyCopy, _ := io.ReadAll(body)
	body = strings.NewReader(string(bodyCopy))

	req, err := http.NewRequestWithContext(ctx, "POST", server.URL, body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(bodyCopy))), nil
	}

	resp, err := DoWithRetryHTTP(ctx, config, client, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(respBody) != "test body" {
		t.Errorf("Expected body 'test body', got '%s'", string(respBody))
	}

	resp.Body.Close()
}

func TestShouldRetryError(t *testing.T) {
	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{"nil error", nil, false},
		{"timeout error", &timeoutError{}, true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"EOF error", errors.New("EOF"), true},
		{"DNS error", errors.New("no such host"), false},
		{"TLS error", errors.New("TLS handshake timeout"), false},
		{"generic error", errors.New("generic error"), true},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
	}

	config := DefaultRetryConfig()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldRetryError(tt.err, config)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for error: %v", tt.expected, result, tt.err)
			}
		})
	}
}

func TestShouldRetryResponse(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		{200, false},
		{201, false},
		{300, false},
		{400, false},
		{401, false},
		{404, false},
		{500, true},
		{502, true},
		{503, false},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.statusCode), func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.statusCode}
			result := shouldRetryResponse(resp)
			if result != tt.expected {
				t.Errorf("Expected %v for status %d, got %v", tt.expected, tt.statusCode, result)
			}
		})
	}
}

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func (e *timeoutError) Unwrap() error { return nil }
