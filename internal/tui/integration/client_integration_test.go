package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsClient_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/metrics", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		metrics := `# HELP icap_requests_total Total number of ICAP requests
# TYPE icap_requests_total counter
icap_requests_total{method="REQMOD"} 1000
icap_requests_total{method="RESPMOD"} 500
# HELP icap_request_duration_seconds Request duration in seconds
# TYPE icap_request_duration_seconds histogram
icap_request_duration_seconds{quantile="0.5"} 0.05
icap_request_duration_seconds{quantile="0.95"} 0.1
icap_request_duration_seconds{quantile="0.99"} 0.15
icap_request_duration_seconds_sum 150
icap_request_duration_seconds_count 15000
# HELP icap_active_connections Number of active connections
# TYPE icap_active_connections gauge
icap_active_connections 50
# HELP icap_errors_total Total number of errors
# TYPE icap_errors_total counter
icap_errors_total 5
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Greater(t, snapshot.RPS, 0.0)
	assert.Equal(t, 50, snapshot.Connections)
}

func TestMetricsClient_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, err.Error(), "not found")
}

func TestMetricsClient_InternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, err.Error(), "server error")
}

func TestMetricsClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               1,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_ConnectionError(t *testing.T) {
	cfg := &state.ClientConfig{
		MetricsURL:            "http://localhost:99999",
		LogsURL:               "http://localhost:99999",
		StatusURL:             "http://localhost:99999",
		Timeout:               1,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_RateLimit(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		metrics := `icap_requests_total 100
icap_request_duration_seconds{quantile="0.5"} 0.05
icap_active_connections 10`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 3,
		RequestInterval:       200 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx := context.Background()

	startTime := time.Now()
	for i := 0; i < 5; i++ {
		_, err := client.GetMetrics(ctx)
		assert.NoError(t, err)
	}
	elapsed := time.Since(startTime)

	assert.Less(t, elapsed, 2*time.Second, "Rate limiting should allow reasonable throughput")
}

func TestMetricsClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewMetricsClient(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, strings.ToLower(err.Error()), "canceled")
}

func TestLogsClient_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/logs", r.URL.Path)
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		assert.Equal(t, http.MethodGet, r.Method)

		logs := `[{"timestamp":"2024-01-01T00:00:00Z","level":"INFO","message":"Test log","fields":{}}]`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(logs))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewLogsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logs, err := client.GetLogs(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "INFO", logs[0].Level)
	assert.Equal(t, "Test log", logs[0].Message)
}

func TestLogsClient_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewLogsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logs, err := client.GetLogs(ctx, 10)
	assert.Error(t, err)
	assert.Nil(t, logs)
	assert.Contains(t, err.Error(), "not found")
}

func TestLogsClient_InvalidLimit(t *testing.T) {
	cfg := &state.ClientConfig{
		MetricsURL:            "http://localhost:8080",
		LogsURL:               "http://localhost:8080",
		StatusURL:             "http://localhost:8080",
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewLogsClient(cfg)
	ctx := context.Background()

	t.Run("Negative limit", func(t *testing.T) {
		logs, err := client.GetLogs(ctx, -1)
		assert.Error(t, err)
		assert.Nil(t, logs)
		assert.Contains(t, err.Error(), "must be positive")
	})

	t.Run("Zero limit", func(t *testing.T) {
		logs, err := client.GetLogs(ctx, 0)
		assert.Error(t, err)
		assert.Nil(t, logs)
		assert.Contains(t, err.Error(), "must be positive")
	})

	t.Run("Too large limit", func(t *testing.T) {
		logs, err := client.GetLogs(ctx, 10001)
		assert.Error(t, err)
		assert.Nil(t, logs)
		assert.Contains(t, err.Error(), "too large")
	})
}

func TestLogsClient_InternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewLogsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logs, err := client.GetLogs(ctx, 10)
	assert.Error(t, err)
	assert.Nil(t, logs)
	assert.Contains(t, err.Error(), "server error")
}

func TestLogsClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               1,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewLogsClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	logs, err := client.GetLogs(ctx, 10)
	assert.Error(t, err)
	assert.Nil(t, logs)
}

func TestStatusClient_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		status := `{"status":"running","port":1344,"uptime":"10s"}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(status))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewStatusClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "running", status.State)
	assert.Equal(t, "1344", status.Port)
	assert.Equal(t, "10s", status.Uptime)
}

func TestStatusClient_ConnectionError(t *testing.T) {
	cfg := &state.ClientConfig{
		MetricsURL:            "http://localhost:99999",
		LogsURL:               "http://localhost:99999",
		StatusURL:             "http://localhost:99999",
		Timeout:               1,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewStatusClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "stopped", status.State)
}

func TestStatusClient_InternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewStatusClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "error", status.State)
}

func TestStatusClient_RateLimit(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		status := `{"status":"running","port":1344,"uptime":"1s"}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(status))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 3,
		RequestInterval:       200 * time.Millisecond,
		RetryMax:              3,
	}

	client := state.NewStatusClient(cfg)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := client.GetStatus(ctx)
		assert.NoError(t, err)
	}

	assert.Equal(t, 5, requestCount, "All requests should be allowed")
}

func TestConnectionPooling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"running","port":1344,"uptime":"1s"}`))
	}))
	defer server.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            server.URL,
		LogsURL:               server.URL,
		StatusURL:             server.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       10 * time.Millisecond,
		RetryMax:              0,
	}

	client := state.NewStatusClient(cfg)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		_, err := client.GetStatus(ctx)
		assert.NoError(t, err)
	}

	assert.Equal(t, server.URL, client.GetBaseURL())
}
