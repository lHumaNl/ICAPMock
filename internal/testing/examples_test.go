// Copyright 2026 ICAP Mock

package testing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestExampleUnit demonstrates request/response building and assertion.
func TestExampleUnit(t *testing.T) {
	req := BuildICAPRequest(
		"REQMOD",
		"icap://localhost/reqmod",
		map[string]string{"Host": "example.com"},
		nil,
	)

	assert.NotNil(t, req)
	assert.Equal(t, "REQMOD", req.Method)
	assert.Equal(t, "icap://localhost/reqmod", req.URI)
}

// TestExampleUnitTableDriven demonstrates a table-driven test.
func TestExampleUnitTableDriven(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		uri     string
		wantErr bool
	}{
		{"valid request", "REQMOD", "icap://localhost/reqmod", false},
		{"valid RESPMOD", "RESPMOD", "icap://localhost/respmod", false},
		{"valid OPTIONS", "OPTIONS", "icap://localhost/options", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := BuildICAPRequest(tt.method, tt.uri, nil, nil)
			assert.NotNil(t, req)
			assert.Equal(t, tt.method, req.Method)
		})
	}
}

// TestExampleUnitWithHTTP demonstrates building requests with HTTP messages.
func TestExampleUnitWithHTTP(t *testing.T) {
	req := BuildICAPRequestWithHTTP(
		"REQMOD",
		"icap://localhost/reqmod",
		"GET",
		"http://example.com",
		map[string]string{"Host": "example.com"},
		[]byte("request body"),
	)

	assert.NotNil(t, req)
	assert.NotNil(t, req.HTTPRequest)
	assert.Equal(t, "GET", req.HTTPRequest.Method)
}

// TestExampleUnitResponse demonstrates building responses.
func TestExampleUnitResponse(t *testing.T) {
	resp := BuildICAPResponse(
		200,
		map[string]string{"ISTag": "test123"},
		nil,
	)

	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

// TestExampleUnitResponseWithHTTP demonstrates building responses with HTTP messages.
func TestExampleUnitResponseWithHTTP(t *testing.T) {
	resp := BuildICAPResponseWithHTTP(
		200,
		200,
		map[string]string{"Content-Type": "text/html"},
		[]byte("<html>Hello</html>"),
	)

	assert.NotNil(t, resp)
	assert.NotNil(t, resp.HTTPResponse)
	assert.Equal(t, "200", resp.HTTPResponse.Status)
}

// TestExampleUnitWithTimeout demonstrates using timeout helpers.
func TestExampleUnitWithTimeout(t *testing.T) {
	ctx := WithTimeout(t, context.Background(), 5*time.Second)

	select {
	case <-ctx.Done():
		_ = ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
}

// TestExampleIntegration demonstrates an integration test using server harness.
func TestExampleIntegration(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 100,
		MaxBodySize:    1024 * 1024,
		Streaming:      true,
	}

	harness := NewServerHarness(t, cfg)
	require.NoError(t, harness.Start())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = harness.Stop(ctx)
	}()

	resp, err := harness.SendRawRequest("OPTIONS icap://localhost/options ICAP/1.0\r\nHost: localhost\r\n\r\n")
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

// TestExampleIntegrationMemory demonstrates using the memory server harness.
func TestExampleIntegrationMemory(t *testing.T) {
	harness := NewMemoryServerHarness(t)
	harness.SetHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	})

	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
	resp, err := harness.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

// TestExampleIntegrationMetrics demonstrates using the metrics harness.
func TestExampleIntegrationMetrics(t *testing.T) {
	harness := NewMetricsServerHarness(t)
	harness.RecordRequest("REQMOD")
	harness.RecordRequest("REQMOD")
	harness.RecordRequestDuration("REQMOD", 100*time.Millisecond)

	// Note: MetricsServerHarness uses real metrics.Collector
	// For assertions, use mock metrics collector instead
	mock := NewMockMetricsCollector()
	mock.RecordRequest("REQMOD")
	mock.RecordRequest("REQMOD")
	mock.AssertRequestCount(t, 2)
}

// TestExampleIntegrationStorage demonstrates using the storage harness.
func TestExampleIntegrationStorage(t *testing.T) {
	harness := NewStorageServerHarness(t)
	defer harness.Cleanup()

	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
	err := harness.StoreRequest(req)
	require.NoError(t, err)

	// Storage writes asynchronously via worker goroutines.
	// Wait briefly for the write to complete before listing.
	time.Sleep(200 * time.Millisecond)

	saved := harness.Storage()
	ctx := context.Background()
	requests, err := saved.ListRequests(ctx, storage.RequestFilter{})
	require.NoError(t, err)
	assert.Len(t, requests, 1)
}

// TestExampleConcurrent demonstrates concurrent testing.
func TestExampleConcurrent(t *testing.T) {
	RunConcurrent(t, 100, func(_ int) {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
	})
}

// TestExampleConcurrentWithTimeout demonstrates concurrent testing with timeout.
func TestExampleConcurrentWithTimeout(t *testing.T) {
	err := RunConcurrentWithTimeout(t, 1000, 5*time.Second, func(_ int, _ context.Context) {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
	})

	assert.NoError(t, err)
}

// TestExampleStress demonstrates stress testing.
func TestExampleStress(t *testing.T) {
	cfg := StressConfig{
		Goroutines: 10,
		Iterations: 100,
		Delay:      10 * time.Millisecond,
		Timeout:    30 * time.Second,
	}

	RunConcurrentStress(t, cfg, func(_, _ int) {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
	})
}

// TestExampleConcurrentWithResults demonstrates collecting results from concurrent operations.
func TestExampleConcurrentWithResults(t *testing.T) {
	results := RunConcurrentWithResults(t, 100, 10, func(_, _ int) error {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
		return nil
	})

	AssertNoConcurrentErrors(t, results)
	errCount, avgDur, maxDur, minDur := GetConcurrentStats(results)

	_ = errCount
	_ = avgDur
	_ = maxDur
	_ = minDur
}

// TestExampleBurst demonstrates burst testing.
func TestExampleBurst(t *testing.T) {
	RunConcurrentBurst(t, 1000, func() {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
	})
}

// TestExampleConcurrentWithBackoff demonstrates concurrent testing with backoff.
func TestExampleConcurrentWithBackoff(t *testing.T) {
	RunConcurrentWithBackoff(t, 100, 3, 100*time.Millisecond, func() error {
		req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
		_ = req
		return nil
	})
}

// TestExampleConcurrentForDuration demonstrates concurrent testing for a duration.
func TestExampleConcurrentForDuration(t *testing.T) {
	RunConcurrentForDuration(t, 50, 5*time.Second, func(stopCh <-chan struct{}) {
		for {
			select {
			case <-stopCh:
				return
			default:
				req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
				_ = req
			}
		}
	})
}

// TestExampleMockMetrics demonstrates using the mock metrics collector.
func TestExampleMockMetrics(t *testing.T) {
	mock := NewMockMetricsCollector()
	mock.RecordRequest("REQMOD")
	mock.RecordRequest("REQMOD")
	mock.RecordRequest("RESPMOD")
	mock.RecordRequestDuration("REQMOD", 100*time.Millisecond)

	mock.AssertRequestCount(t, 3)
	assert.True(t, mock.AssertMethodCalled(t, "REQMOD"))
	assert.Equal(t, int64(3), mock.GetRequestCount())
}

// TestExampleMockStorage demonstrates using the mock storage.
func TestExampleMockStorage(t *testing.T) {
	mock := NewMockStorage()
	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
	sr := storage.FromICAPRequest(req, 204, 0)

	ctx := context.Background()
	err := mock.SaveRequest(ctx, sr)
	require.NoError(t, err)

	saved := mock.GetSavedRequests()
	assert.Len(t, saved, 1)
	assert.Equal(t, int64(1), mock.GetSaveCount())
}

// TestExampleMockFileSystem demonstrates using the mock file system.
func TestExampleMockFileSystem(t *testing.T) {
	mock := NewMockFileSystem(10 * 1024 * 1024 * 1024)

	err := mock.WriteFile("/test.txt", []byte("hello"), 0o644)
	require.NoError(t, err)

	data, err := mock.ReadFile("/test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)

	used := mock.GetUsedSpace()
	free := mock.GetFreeSpace()
	total := mock.GetTotalSpace()

	assert.Equal(t, int64(5), used)
	assert.Equal(t, total-used, free)
}

// TestExampleServerRestart demonstrates server restart functionality.
func TestExampleServerRestart(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		Streaming:      true,
	}

	harness := NewServerHarness(t, cfg)
	require.NoError(t, harness.Start())

	addr1 := harness.Addr()
	assert.NotEmpty(t, addr1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, harness.Restart(ctx))

	addr2 := harness.Addr()
	assert.NotEmpty(t, addr2)
}

// TestExampleGracefulShutdown demonstrates graceful shutdown functionality.
func TestExampleGracefulShutdown(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		Streaming:      true,
	}

	harness := NewServerHarness(t, cfg)
	require.NoError(t, harness.Start())

	// Start some requests
	RunConcurrent(t, 10, func(_ int) {
		req := BuildICAPRequest("OPTIONS", "icap://localhost/options", nil, nil)
		_, _ = harness.SendRequest(req)
	})

	// Server should handle shutdown gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, harness.Stop(ctx))
}

// TestExampleHighLoad demonstrates high load testing.
func TestExampleHighLoad(t *testing.T) {
	serverCfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 1000,
		Streaming:      true,
	}

	harness := NewServerHarness(t, serverCfg)
	require.NoError(t, harness.Start())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = harness.Stop(ctx)
	}()

	stressCfg := StressConfig{
		Goroutines: 50,
		Iterations: 20,
		Delay:      10 * time.Millisecond,
		Timeout:    60 * time.Second,
	}

	var errCount int64
	RunConcurrentStress(t, stressCfg, func(_, _ int) {
		req := BuildICAPRequest("OPTIONS", "icap://localhost/options", nil, nil)
		_, err := harness.SendRequest(req)
		if err != nil {
			atomic.AddInt64(&errCount, 1)
		}
	})
	total := int64(stressCfg.Goroutines * stressCfg.Iterations)
	t.Logf("High load test: %d/%d errors", errCount, total)
	// Allow up to 10% error rate under high load
	assert.Less(t, errCount, total/10, "error rate too high")
}

// TestExampleRateLimit demonstrates rate limiting testing.
func TestExampleRateLimit(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		Streaming:      true,
	}

	harness := NewServerHarness(t, cfg)
	require.NoError(t, harness.Start())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = harness.Stop(ctx)
	}()

	results := RunConcurrentWithResults(t, 500, 10, func(_, _ int) error {
		req := BuildICAPRequest("OPTIONS", "icap://localhost/options", nil, nil)
		resp, err := harness.SendRequest(req)
		_ = resp
		return err
	})

	errCount, _, _, _ := GetConcurrentStats(results)
	t.Logf("Rate limit test: %d errors out of %d requests", errCount, len(results))
}

// TestExampleCircuitBreaker demonstrates circuit breaker testing.
func TestExampleCircuitBreaker(t *testing.T) {
	mockMetrics := NewMockMetricsCollector()
	testErr := errors.New("test error")

	results := RunConcurrentWithResults(t, 100, 10, func(_, iteration int) error {
		if iteration%10 == 0 {
			mockMetrics.RecordError("REQMOD", testErr)
			return testErr
		}
		mockMetrics.RecordRequest("REQMOD")
		return nil
	})

	errCount, _, _, _ := GetConcurrentStats(results)
	mockMetrics.AssertErrorCount(t, int64(errCount))
}

// TestExampleAssertion demonstrates assertion helpers.
func TestExampleAssertion(t *testing.T) {
	want := BuildICAPResponse(200, map[string]string{"ISTag": "test123"}, nil)
	got := BuildICAPResponse(200, map[string]string{"ISTag": "test123"}, nil)

	AssertICAPResponse(t, got, want)

	wantReq := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
	gotReq := BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)

	AssertICAPRequest(t, gotReq, wantReq)
}

// TestExampleTimeout demonstrates timeout testing.
func TestExampleTimeout(t *testing.T) {
	ctx := WithTimeout(t, context.Background(), 100*time.Millisecond)

	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		t.Log("Operation completed before timeout")
	case <-ctx.Done():
		t.Log("Context timed out")
	}
}

// TestExampleFreePort demonstrates getting a free port.
func TestExampleFreePort(t *testing.T) {
	port := GetFreePort(t)
	assert.Greater(t, port, 0)
	assert.Less(t, port, 65536)
}
