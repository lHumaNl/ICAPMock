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

// mockProcessor implements processor.Processor for testing.
type mockProcessor struct {
	err     error
	resp    *icap.Response
	reqRecv *icap.Request
	name    string
	called  bool
}

const (
	previewRequestLimitProbeCount = 150
	previewRequestSize            = 100
)

func (m *mockProcessor) Process(_ context.Context, req *icap.Request) (*icap.Response, error) {
	m.called = true
	m.reqRecv = req
	return m.resp, m.err
}

func (m *mockProcessor) Name() string {
	return m.name
}

// TestReqmodHandler tests the REQMOD handler basic functionality.
func TestReqmodHandler(t *testing.T) {
	t.Parallel()

	t.Run("Handle processes request successfully", func(t *testing.T) {
		mockProc := &mockProcessor{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusNoContentNeeded),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)
		req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
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

		if mockProc.reqRecv != req {
			t.Error("Processor did not receive the correct request")
		}

		if resp.StatusCode != icap.StatusNoContentNeeded {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
		}
	})

	t.Run("Method returns REQMOD", func(t *testing.T) {
		h := handler.NewReqmodHandler(nil, nil, nil)
		if h.Method() != icap.MethodREQMOD {
			t.Errorf("Method() = %q, want %q", h.Method(), icap.MethodREQMOD)
		}
	})
}

// TestReqmodHandlerMetrics tests that metrics are recorded correctly.
func TestReqmodHandlerMetrics(t *testing.T) {
	t.Parallel()

	t.Run("records request metrics", func(t *testing.T) {
		mockProc := &mockProcessor{
			name: "test-processor",
			resp: icap.NewResponse(icap.StatusOK),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")

		_, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		// Verify metrics were recorded by checking the registry
		mfs, err := reg.Gather()
		if err != nil {
			t.Errorf("Failed to gather metrics: %v", err)
		}

		// Check that we have some metrics recorded
		if len(mfs) == 0 {
			t.Error("No metrics were recorded")
		}
	})

	t.Run("records error metrics on processor error", func(t *testing.T) {
		mockProc := &mockProcessor{
			name: "test-processor",
			err:  errors.New("processing error"),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")

		_, err := h.Handle(context.Background(), req)
		if err == nil {
			t.Error("Handle() should return error when processor fails")
		}

		// Verify error metrics were recorded
		mfs, _ := reg.Gather()
		for _, mf := range mfs {
			if mf.GetName() == "icap_errors_total" {
				// Found error metrics
				return
			}
		}
	})
}

func TestReqmodHandler_ProcessingInFlightUsesCapturedCollector(t *testing.T) {
	regA := prometheus.NewRegistry()
	collectorA, err := metrics.NewCollector(regA)
	if err != nil {
		t.Fatalf("NewCollector(A) error = %v", err)
	}
	regB := prometheus.NewRegistry()
	collectorB, err := metrics.NewCollector(regB)
	if err != nil {
		t.Fatalf("NewCollector(B) error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	proc := processor.Func(func(context.Context, *icap.Request) (*icap.Response, error) {
		close(started)
		<-release
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	h := handler.NewReqmodHandlerForServer("edge", proc, collectorA, nil)
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/scan")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, handleErr := h.Handle(context.Background(), req)
		done <- handleErr
	}()
	<-started

	if got := gatheredHandlerGauge(t, regA, "icap_requests_processing_in_flight"); got != 1 {
		t.Fatalf("collector A processing in-flight while blocked = %v, want 1", got)
	}
	if got := gatheredHandlerGauge(t, regA, "icap_requests_in_flight"); got != 0 {
		t.Fatalf("handler-owned lifecycle in-flight = %v, want 0", got)
	}
	h.SetMetrics(collectorB)
	close(release)
	if handleErr := <-done; handleErr != nil {
		t.Fatalf("Handle() error = %v", handleErr)
	}
	if got := gatheredHandlerGauge(t, regA, "icap_requests_processing_in_flight"); got != 0 {
		t.Fatalf("collector A processing in-flight after return = %v, want 0", got)
	}
	if got := gatheredHandlerGauge(t, regB, "icap_requests_processing_in_flight"); got != 0 {
		t.Fatalf("collector B processing in-flight = %v, want 0", got)
	}
}

func TestReqmodHandler_ProcessingStartedWithoutMetricsDoesNotDecrementLaterCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	proc := processor.Func(func(context.Context, *icap.Request) (*icap.Response, error) {
		close(started)
		<-release
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	h := handler.NewReqmodHandlerForServer("edge", proc, nil, nil)
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/scan")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, handleErr := h.Handle(context.Background(), req)
		done <- handleErr
	}()
	<-started
	h.SetMetrics(collector)
	close(release)
	if handleErr := <-done; handleErr != nil {
		t.Fatalf("Handle() error = %v", handleErr)
	}
	if got := gatheredHandlerGauge(t, reg, "icap_requests_processing_in_flight"); got != 0 {
		t.Fatalf("late collector processing in-flight = %v, want 0", got)
	}
}

func TestReqmodHandler_AllInvocationMetricsUseCapturedCollector(t *testing.T) {
	regA := prometheus.NewRegistry()
	collectorA, err := metrics.NewCollector(regA)
	if err != nil {
		t.Fatalf("NewCollector(A) error = %v", err)
	}
	regB := prometheus.NewRegistry()
	collectorB, err := metrics.NewCollector(regB)
	if err != nil {
		t.Fatalf("NewCollector(B) error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	expectedErr := errors.New("processor failed")
	proc := processor.Func(func(context.Context, *icap.Request) (*icap.Response, error) {
		close(started)
		<-release
		return nil, expectedErr
	})
	h := handler.NewReqmodHandlerForServer("edge", proc, collectorA, nil)
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/scan")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, handleErr := h.Handle(context.Background(), req)
		done <- handleErr
	}()
	<-started
	h.SetMetrics(collectorB)
	close(release)
	if handleErr := <-done; !errors.Is(handleErr, expectedErr) {
		t.Fatalf("Handle() error = %v, want %v", handleErr, expectedErr)
	}

	if got := gatheredHandlerCounter(t, regA, "icap_errors_total", map[string]string{
		"server": "edge", "type": "processing_error",
	}); got != 1 {
		t.Fatalf("collector A processing errors = %v, want 1", got)
	}
	if got := gatheredHandlerCounter(t, regB, "icap_errors_total", map[string]string{
		"server": "edge", "type": "processing_error",
	}); got != 0 {
		t.Fatalf("collector B processing errors = %v, want 0", got)
	}
}

func gatheredHandlerGauge(
	t *testing.T,
	reg prometheus.Gatherer,
	name string,
) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["server"] == "edge" && labels["method"] == "REQMOD" {
				return metric.GetGauge().GetValue()
			}
		}
	}
	return 0
}

func gatheredHandlerCounter(
	t *testing.T,
	reg prometheus.Gatherer,
	name string,
	wantLabels map[string]string,
) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			matched := true
			for label, want := range wantLabels {
				if labels[label] != want {
					matched = false
					break
				}
			}
			if matched {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// TestReqmodHandlerProcessorErrors tests error handling from processor.
func TestReqmodHandlerProcessorErrors(t *testing.T) {
	t.Parallel()

	t.Run("propagates processor error", func(t *testing.T) {
		expectedErr := errors.New("processor failed")
		mockProc := &mockProcessor{
			name: "test-processor",
			err:  expectedErr,
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")

		_, err := h.Handle(context.Background(), req)
		if !errors.Is(err, expectedErr) {
			t.Errorf("Handle() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("handles nil processor gracefully", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(nil, m, nil)
		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")

		resp, err := h.Handle(context.Background(), req)
		// Should return error for nil processor
		if err == nil {
			t.Error("Handle() should return error for nil processor")
		}
		if resp != nil {
			t.Error("Handle() should return nil response for nil processor")
		}
	})
}

// TestReqmodHandlerContextCancellation tests context cancellation handling.
func TestReqmodHandlerContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("respects context cancellation", func(t *testing.T) {
		mockProc := &mockProcessor{
			name: "slow-processor",
			resp: icap.NewResponse(icap.StatusOK),
		}

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		_, err := h.Handle(ctx, req)

		// Should return context canceled error
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Handle() error = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("handles context deadline", func(t *testing.T) {
		mockProc := processor.Func(func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
			// Simulate slow processing
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return icap.NewResponse(icap.StatusOK), nil
			}
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		_, err := h.Handle(ctx, req)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Handle() error = %v, want %v", err, context.DeadlineExceeded)
		}
	})

	// P0 FIX: Test that response is not sent when context is canceled after processing
	t.Run("does not send response when context canceled after processing", func(t *testing.T) {
		mockProc := processor.Func(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			// Processor takes time and creates response
			time.Sleep(50 * time.Millisecond)
			resp := icap.NewResponse(icap.StatusOK)
			resp.Body = []byte("response body")
			return resp, nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)

		// Create context with timeout shorter than processing time
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")

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

// TestReqmodHandlerWithHTTPRequest tests handling of requests with embedded HTTP.
func TestReqmodHandlerWithHTTPRequest(t *testing.T) {
	t.Parallel()

	t.Run("passes HTTP request to processor", func(t *testing.T) {
		var receivedReq *icap.Request

		mockProc := processor.Func(func(_ context.Context, req *icap.Request) (*icap.Response, error) {
			receivedReq = req
			return icap.NewResponse(icap.StatusNoContentNeeded), nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		req.HTTPRequest = &icap.HTTPMessage{
			Method: "GET",
			URI:    "http://example.com/test",
			Proto:  "HTTP/1.1",
		}

		_, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if receivedReq.HTTPRequest == nil {
			t.Error("HTTP request was not passed to processor")
		}
		if receivedReq.HTTPRequest.Method != "GET" {
			t.Errorf("HTTP Method = %q, want %q", receivedReq.HTTPRequest.Method, "GET")
		}
	})
}

// TestReqmodHandlerConcurrent tests concurrent request handling.
func TestReqmodHandlerConcurrent(t *testing.T) {
	t.Parallel()

	t.Run("handles concurrent requests", func(t *testing.T) {
		mockProc := processor.Func(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			time.Sleep(10 * time.Millisecond) // Simulate work
			return icap.NewResponse(icap.StatusNoContentNeeded), nil
		})

		reg := prometheus.NewRegistry()
		m, _ := metrics.NewCollector(reg)

		h := handler.NewReqmodHandler(mockProc, m, nil)

		const numRequests = 10
		errCh := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
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

func TestReqmodHandlerPreviewRequestsAreNotLimited(t *testing.T) {
	t.Parallel()

	proc := &mockProcessor{
		name: "test-processor",
		resp: icap.NewResponse(icap.StatusNoContentNeeded),
	}
	reg := prometheus.NewRegistry()
	m, _ := metrics.NewCollector(reg)
	h := handler.NewReqmodHandler(proc, m, nil)

	for range previewRequestLimitProbeCount {
		assertPreviewRequestNotLimited(t, h)
	}
}

func assertPreviewRequestNotLimited(t *testing.T, h *handler.ReqmodHandler) {
	t.Helper()
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
	req.Preview = previewRequestSize
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
	}
}
