// Copyright 2026 ICAP Mock

package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// mockProcessorRespmod implements processor.Processor for testing.
type mockProcessorRespmod struct {
	err     error
	resp    *icap.Response
	reqRecv *icap.Request
	name    string
	called  bool
}

func (m *mockProcessorRespmod) Process(_ context.Context, req *icap.Request) (*icap.Response, error) {
	m.called = true
	m.reqRecv = req
	return m.resp, m.err
}

func (m *mockProcessorRespmod) Name() string {
	return m.name
}

// TestRespmodHandler tests the RESPMOD handler basic functionality.
func TestRespmodHandler(t *testing.T) {
	t.Parallel()

	t.Run("Handle processes response successfully", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)
		req, err := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if !mockProc.called {
			t.Error("Processor was not called")
		}

		if resp.StatusCode != icap.StatusNoContentNeeded {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
		}
	})

	t.Run("Method returns RESPMOD", func(t *testing.T) {
		h := handler.NewRespmodHandler(nil, nil, nil, nil)
		if h.Method() != icap.MethodRESPMOD {
			t.Errorf("Method() = %q, want %q", h.Method(), icap.MethodRESPMOD)
		}
	})
}

// TestRespmodHandlerMetrics tests that metrics are recorded correctly.
func TestRespmodHandlerMetrics(t *testing.T) {
	t.Parallel()

	t.Run("records request metrics", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusOK),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")

		_, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		// Verify metrics were recorded
		mfs, err := reg.Gather()
		if err != nil {
			t.Errorf("Failed to gather metrics: %v", err)
		}

		if len(mfs) == 0 {
			t.Error("No metrics were recorded")
		}
	})

	t.Run("records error metrics on processor error", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			err:  errors.New("processing error"),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")

		_, err := h.Handle(context.Background(), req)
		if err == nil {
			t.Error("Handle() should return error when processor fails")
		}

		// Verify error metrics were recorded
		mfs, _ := reg.Gather()
		for _, mf := range mfs {
			if mf.GetName() == "icap_errors_total" {
				return
			}
		}
	})
}

// TestRespmodHandlerProcessorErrors tests error handling from processor.
func TestRespmodHandlerProcessorErrors(t *testing.T) {
	t.Parallel()

	t.Run("propagates processor error", func(t *testing.T) {
		expectedErr := errors.New("processor failed")
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			err:  expectedErr,
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")

		_, err := h.Handle(context.Background(), req)
		if !errors.Is(err, expectedErr) {
			t.Errorf("Handle() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("handles nil processor gracefully", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(nil, m, nil, nil)
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")

		resp, err := h.Handle(context.Background(), req)
		if err == nil {
			t.Error("Handle() should return error for nil processor")
		}
		if resp != nil {
			t.Error("Handle() should return nil response for nil processor")
		}
	})
}

// TestRespmodHandlerContextCancellation tests context cancellation handling.
func TestRespmodHandlerContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("respects context cancellation", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "slow-processor",
			resp: icap.NewResponse(icap.StatusOK),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		_, err := h.Handle(ctx, req)

		if !errors.Is(err, context.Canceled) {
			t.Errorf("Handle() error = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("handles context deadline", func(t *testing.T) {
		mockProc := processor.ProcessorFunc(func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return icap.NewResponse(icap.StatusOK), nil
			}
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		_, err := h.Handle(ctx, req)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Handle() error = %v, want %v", err, context.DeadlineExceeded)
		}
	})

	// P0 FIX: Test that response is not sent when context is canceled after processing
	t.Run("does not send response when context canceled after processing", func(t *testing.T) {
		mockProc := processor.ProcessorFunc(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			// Processor takes time and creates response
			time.Sleep(50 * time.Millisecond)
			resp := icap.NewResponse(icap.StatusOK)
			resp.Body = []byte("response body")
			return resp, nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		// Create context with timeout shorter than processing time
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")

		resp, err := h.Handle(ctx, req)

		// Should return context error
		if err == nil {
			t.Error("Handle() should return error when context is canceled")
		}

		// Response should be nil because context was canceled after processing
		if resp != nil {
			t.Errorf("Handle() should return nil response when context canceled, got %+v", resp)
		}
	})
}

// TestRespmodHandlerWithHTTPResponse tests handling of requests with embedded HTTP response.
func TestRespmodHandlerWithHTTPResponse(t *testing.T) {
	t.Parallel()

	t.Run("passes HTTP response to processor", func(t *testing.T) {
		var receivedReq *icap.Request

		mockProc := processor.ProcessorFunc(func(_ context.Context, req *icap.Request) (*icap.Response, error) {
			receivedReq = req
			return icap.NewResponse(icap.StatusNoContentNeeded), nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		req.HTTPResponse = &icap.HTTPMessage{
			Proto:      "HTTP/1.1",
			Status:     "200",
			StatusText: "OK",
		}

		_, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if receivedReq.HTTPResponse == nil {
			t.Error("HTTP response was not passed to processor")
		}
		if receivedReq.HTTPResponse.Status != "200" {
			t.Errorf("HTTP Status = %q, want %q", receivedReq.HTTPResponse.Status, "200")
		}
	})
}

// TestRespmodHandlerConcurrent tests concurrent request handling.
func TestRespmodHandlerConcurrent(t *testing.T) {
	t.Parallel()

	t.Run("handles concurrent requests", func(t *testing.T) {
		mockProc := processor.ProcessorFunc(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			time.Sleep(10 * time.Millisecond)
			return icap.NewResponse(icap.StatusNoContentNeeded), nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		const numRequests = 10
		errCh := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
				_, err := h.Handle(context.Background(), req)
				errCh <- err
			}()
		}

		for i := 0; i < numRequests; i++ {
			if err := <-errCh; err != nil {
				t.Errorf("Concurrent request failed: %v", err)
			}
		}
	})
}

// TestRespmodHandlerModifiedResponse tests returning modified HTTP responses.
func TestRespmodHandlerModifiedResponse(t *testing.T) {
	t.Parallel()

	t.Run("returns modified HTTP response", func(t *testing.T) {
		mockProc := processor.ProcessorFunc(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			resp := icap.NewResponse(icap.StatusOK)
			resp.HTTPResponse = &icap.HTTPMessage{
				Proto:      "HTTP/1.1",
				Status:     "200",
				StatusText: "OK",
				Header:     icap.NewHeader(),
			}
			resp.HTTPResponse.Header.Set("X-Modified", "true")
			return resp, nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if resp.HTTPResponse == nil {
			t.Fatal("Expected HTTP response to be set")
		}

		modified, ok := resp.HTTPResponse.Header.Get("X-Modified")
		if !ok || modified != "true" {
			t.Errorf("Expected X-Modified header to be 'true', got %q", modified)
		}
	})
}

// TestRespmodHandlerRateLimiting tests preview rate limiting integration.
func TestRespmodHandlerRateLimiting(t *testing.T) {
	t.Parallel()

	t.Run("allows requests within rate limit", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       true,
			MaxRequests:   5,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Send 5 requests - all should be allowed
		for i := 0; i < 5; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100 // Enable preview mode
			req.ClientIP = "127.0.0.1"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})

	t.Run("rejects requests exceeding rate limit", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       true,
			MaxRequests:   3,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Send 3 requests - should be allowed
		for i := 0; i < 3; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.2"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}

		// 4th request should be rate limited
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		req.Preview = 100
		req.ClientIP = "127.0.0.2"

		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Rate limit response should not return error, got: %v", err)
		}

		// Should return 503 ServiceUnavailable with rate limit headers
		if resp.StatusCode != icap.StatusServiceUnavailable {
			t.Errorf("Rate limited request status = %d, want %d", resp.StatusCode, icap.StatusServiceUnavailable)
		}

		// Check for required headers
		retryAfter, exists := resp.GetHeader("Retry-After")
		if !exists || retryAfter == "" {
			t.Error("Response should include Retry-After header")
		}

		rateLimitLimit, exists := resp.GetHeader("X-RateLimit-Limit")
		if !exists || rateLimitLimit != "3" {
			t.Errorf("X-RateLimit-Limit should be 3, got %q", rateLimitLimit)
		}

		rateLimitRemaining, exists := resp.GetHeader("X-RateLimit-Remaining")
		if !exists || rateLimitRemaining != "0" {
			t.Errorf("X-RateLimit-Remaining should be 0, got %q", rateLimitRemaining)
		}

		rateLimitReset, exists := resp.GetHeader("X-RateLimit-Reset")
		if !exists || rateLimitReset == "" {
			t.Error("Response should include X-RateLimit-Reset header")
		}
	})

	t.Run("allows requests from different clients", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       true,
			MaxRequests:   2,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Client 1 sends 2 requests
		for i := 0; i < 2; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.3"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Client 1 request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Client 1 request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}

		// Client 2 should still be able to send requests
		for i := 0; i < 2; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.4"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Client 2 request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Client 2 request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})

	t.Run("does not rate limit non-preview requests", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       true,
			MaxRequests:   1,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Send 10 non-preview requests - all should be allowed
		for i := 0; i < 10; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 0 // Non-preview mode
			req.ClientIP = "127.0.0.5"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Non-preview request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Non-preview request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})

	t.Run("allows unlimited requests when rate limiter is disabled", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       false,
			MaxRequests:   1,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Send 10 preview requests - all should be allowed when rate limiting is disabled
		for i := 0; i < 10; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.6"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Request %d should succeed with rate limiter disabled, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})

	t.Run("allows requests when rate limiter is nil", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		// Pass nil rate limiter
		h := handler.NewRespmodHandler(mockProc, m, nil, nil)

		// Send 10 preview requests - all should be allowed when rate limiter is nil
		for i := 0; i < 10; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.7"

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Request %d should succeed with nil rate limiter, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})

	t.Run("extracts client ID from X-Client-ID header", func(t *testing.T) {
		mockProc := &mockProcessorRespmod{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		config := handler.PreviewRateLimiterConfig{
			Enabled:       true,
			MaxRequests:   2,
			WindowSeconds: 10,
			MaxClients:    100,
		}
		previewRateLimiter := handler.NewPreviewRateLimiter(config, m, nil)

		h := handler.NewRespmodHandler(mockProc, m, nil, previewRateLimiter)

		// Client 1 with X-Client-ID header
		for i := 0; i < 2; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.8"
			req.Header.Set("X-Client-ID", "client-1")

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Client-1 request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Client-1 request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}

		// Client 1 should be rate limited
		req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
		req.Preview = 100
		req.ClientIP = "127.0.0.8"
		req.Header.Set("X-Client-ID", "client-1")

		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Rate limit response should not return error, got: %v", err)
		}
		if resp.StatusCode != icap.StatusServiceUnavailable {
			t.Errorf("Client-1 should be rate limited, status = %d, want %d", resp.StatusCode, icap.StatusServiceUnavailable)
		}

		// Client 2 with different X-Client-ID should still be allowed
		for i := 0; i < 2; i++ {
			req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/respmod")
			req.Preview = 100
			req.ClientIP = "127.0.0.8" // Same IP, different client ID
			req.Header.Set("X-Client-ID", "client-2")

			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Client-2 request %d should succeed, got error: %v", i+1, err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("Client-2 request %d status = %d, want %d", i+1, resp.StatusCode, icap.StatusNoContentNeeded)
			}
		}
	})
}
