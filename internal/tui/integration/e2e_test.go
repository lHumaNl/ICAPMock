package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_FullWorkflow(t *testing.T) {
	requestCount := make(map[string]int)
	mu := &sync.Mutex{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount[r.URL.Path]++
		mu.Unlock()

		switch r.URL.Path {
		case "/metrics":
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
icap_errors_total{method="REQMOD"} 3
icap_errors_total{method="RESPMOD"} 2
`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(metrics))

		case "/logs":
			logs := []*state.LogEntry{
				{Timestamp: time.Now(), Level: "INFO", Message: "Server started", Fields: map[string]interface{}{}},
				{Timestamp: time.Now(), Level: "INFO", Message: "Processing request", Fields: map[string]interface{}{"id": "123"}},
			}
			logsJSON, _ := json.Marshal(logs)
			w.WriteHeader(http.StatusOK)
			w.Write(logsJSON)

		case "/health":
			status := `{"status":"running","port":1344,"uptime":"10s"}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(status))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	t.Run("Step 1: Initialize clients", func(t *testing.T) {
		metricsClient := state.NewMetricsClient(cfg)
		logsClient := state.NewLogsClient(cfg)
		statusClient := state.NewStatusClient(cfg)

		assert.NotNil(t, metricsClient)
		assert.NotNil(t, logsClient)
		assert.NotNil(t, statusClient)
	})

	t.Run("Step 2: Check server health", func(t *testing.T) {
		statusClient := state.NewStatusClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, err := statusClient.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "running", status.State)
		assert.Equal(t, "1344", status.Port)
		assert.Equal(t, "10s", status.Uptime)
	})

	t.Run("Step 3: Fetch metrics", func(t *testing.T) {
		metricsClient := state.NewMetricsClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		snapshot, err := metricsClient.GetMetrics(ctx)
		require.NoError(t, err)
		assert.NotNil(t, snapshot)
		assert.Greater(t, snapshot.RPS, 0.0)
		assert.Equal(t, 50, snapshot.Connections)
		assert.Equal(t, 5, snapshot.Errors)
	})

	t.Run("Step 4: Fetch logs", func(t *testing.T) {
		logsClient := state.NewLogsClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		logs, err := logsClient.GetLogs(ctx, 10)
		require.NoError(t, err)
		assert.Len(t, logs, 2)
		assert.Equal(t, "INFO", logs[0].Level)
	})

	t.Run("Step 5: Verify request counts", func(t *testing.T) {
		assert.Equal(t, 1, requestCount["/health"])
		assert.Equal(t, 1, requestCount["/metrics"])
		assert.Equal(t, 1, requestCount["/logs"])
	})
}

func TestE2E_ErrorScenarios(t *testing.T) {
	t.Run("Server not responding", func(t *testing.T) {
		cfg := &state.ClientConfig{
			MetricsURL:            "http://localhost:99999",
			LogsURL:               "http://localhost:99999",
			StatusURL:             "http://localhost:99999",
			Timeout:               1,
			MaxConcurrentRequests: 10,
			RequestInterval:       100 * time.Millisecond,
			RetryMax:              3,
		}

		metricsClient := state.NewMetricsClient(cfg)
		logsClient := state.NewLogsClient(cfg)
		statusClient := state.NewStatusClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := metricsClient.GetMetrics(ctx)
		assert.Error(t, err)

		_, err = logsClient.GetLogs(ctx, 10)
		assert.Error(t, err)

		status, err := statusClient.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "stopped", status.State)
	})

	t.Run("Server returns errors", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		cfg := &state.ClientConfig{
			MetricsURL:            ts.URL,
			LogsURL:               ts.URL,
			StatusURL:             ts.URL,
			Timeout:               5,
			MaxConcurrentRequests: 10,
			RequestInterval:       100 * time.Millisecond,
			RetryMax:              3,
		}

		metricsClient := state.NewMetricsClient(cfg)
		logsClient := state.NewLogsClient(cfg)
		statusClient := state.NewStatusClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := metricsClient.GetMetrics(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server error")

		_, err = logsClient.GetLogs(ctx, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server error")

		status, err := statusClient.GetStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "error", status.State)
	})

	t.Run("Invalid responses", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("invalid json"))
		}))
		defer ts.Close()

		cfg := &state.ClientConfig{
			MetricsURL:            ts.URL,
			LogsURL:               ts.URL,
			StatusURL:             ts.URL,
			Timeout:               5,
			MaxConcurrentRequests: 10,
			RequestInterval:       100 * time.Millisecond,
			RetryMax:              3,
		}

		logsClient := state.NewLogsClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := logsClient.GetLogs(ctx, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode logs")
	})
}

func TestE2E_MetricsStateLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := `icap_requests_total 1000
icap_request_duration_seconds{quantile="0.5"} 0.05
icap_active_connections 50`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	metricsState := state.NewMetricsState(cfg)
	assert.NotNil(t, metricsState)

	t.Run("Initial state", func(t *testing.T) {
		current := metricsState.GetCurrent()
		assert.NotNil(t, current)
		history := metricsState.GetHistory()
		assert.Len(t, history, 0)
	})

	t.Run("Update state", func(t *testing.T) {
		snapshot := &state.MetricsSnapshot{
			Timestamp:     time.Now(),
			RPS:           10.5,
			LatencyP50:    50.0,
			LatencyP95:    100.0,
			LatencyP99:    150.0,
			Connections:   100,
			Errors:        5,
			BytesSent:     1000000,
			BytesReceived: 500000,
		}
		metricsState.Update(snapshot)

		current := metricsState.GetCurrent()
		assert.Equal(t, 10.5, current.RPS)
		assert.Equal(t, 100, current.Connections)

		history := metricsState.GetHistory()
		assert.Len(t, history, 1)
	})

	t.Run("History limit", func(t *testing.T) {
		for i := 0; i < 150; i++ {
			snapshot := &state.MetricsSnapshot{
				Timestamp:     time.Now(),
				RPS:           float64(i),
				Connections:   i,
				Errors:        0,
				BytesSent:     0,
				BytesReceived: 0,
			}
			metricsState.Update(snapshot)
		}

		history := metricsState.GetHistory()
		assert.LessOrEqual(t, len(history), 100, "History should be limited to maxHistory")
	})

	t.Run("Stop streaming", func(t *testing.T) {
		metricsState.StopStreaming()
	})

	t.Run("Shutdown", func(t *testing.T) {
		metricsState.Shutdown()
	})
}

func TestE2E_LogsStateLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logs := []*state.LogEntry{
			{Timestamp: time.Now(), Level: "INFO", Message: "Test log", Fields: map[string]interface{}{}},
		}
		logsJSON, _ := json.Marshal(logs)
		w.WriteHeader(http.StatusOK)
		w.Write(logsJSON)
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	logsState := state.NewLogsState(cfg)
	assert.NotNil(t, logsState)

	t.Run("Initial state", func(t *testing.T) {
		entries := logsState.GetEntries(nil, 10)
		assert.Len(t, entries, 0)
	})

	t.Run("Add entries", func(t *testing.T) {
		entry1 := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "First entry",
			Fields:    map[string]interface{}{},
		}
		entry2 := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "ERROR",
			Message:   "Second entry",
			Fields:    map[string]interface{}{"error": "test"},
		}

		logsState.AddEntry(entry1)
		logsState.AddEntry(entry2)

		entries := logsState.GetEntries(nil, 10)
		assert.Len(t, entries, 2)
		assert.Equal(t, "INFO", entries[0].Level)
		assert.Equal(t, "ERROR", entries[1].Level)
	})

	t.Run("Filter entries", func(t *testing.T) {
		filter := &state.LogFilter{Level: "INFO"}
		entries := logsState.GetEntries(filter, 10)
		assert.Len(t, entries, 1)
		assert.Equal(t, "INFO", entries[0].Level)
	})

	t.Run("Max lines limit", func(t *testing.T) {
		for i := 0; i < 1500; i++ {
			entry := &state.LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Entry %d", i),
				Fields:    map[string]interface{}{},
			}
			logsState.AddEntry(entry)
		}

		entries := logsState.GetEntries(nil, 10000)
		assert.LessOrEqual(t, len(entries), 1000, "Entries should be limited to maxLines")
	})

	t.Run("Stop streaming", func(t *testing.T) {
		logsState.StopStreaming()
	})

	t.Run("Shutdown", func(t *testing.T) {
		logsState.Shutdown()
	})
}

func TestE2E_ConcurrentOperations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)

		switch r.URL.Path {
		case "/metrics":
			metrics := `icap_requests_total 1000
icap_active_connections 50`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(metrics))

		case "/logs":
			logs := []*state.LogEntry{
				{Timestamp: time.Now(), Level: "INFO", Message: "Test log", Fields: map[string]interface{}{}},
			}
			logsJSON, _ := json.Marshal(logs)
			w.WriteHeader(http.StatusOK)
			w.Write(logsJSON)

		case "/health":
			status := `{"status":"running","port":1344,"uptime":"10s"}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(status))
		}
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       50 * time.Millisecond,
		RetryMax:              3,
	}

	metricsClient := state.NewMetricsClient(cfg)
	logsClient := state.NewLogsClient(cfg)
	statusClient := state.NewStatusClient(cfg)

	done := make(chan bool, 30)
	errors := make(chan error, 30)

	for i := 0; i < 10; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := metricsClient.GetMetrics(ctx)
			if err != nil {
				errors <- err
			}
			done <- true
		}()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := logsClient.GetLogs(ctx, 10)
			if err != nil {
				errors <- err
			}
			done <- true
		}()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := statusClient.GetStatus(ctx)
			if err != nil {
				errors <- err
			}
			done <- true
		}()
	}

	for i := 0; i < 30; i++ {
		<-done
	}

	close(errors)
	errCount := 0
	for err := range errors {
		t.Logf("Concurrent operation error: %v", err)
		errCount++
	}

	assert.LessOrEqual(t, errCount, 5, "Should have minimal errors under concurrent load")
}

func TestE2E_RateLimiting(t *testing.T) {
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		metrics := `icap_requests_total 1000
icap_active_connections 50`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 3,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	metricsClient := state.NewMetricsClient(cfg)
	ctx := context.Background()

	startTime := time.Now()
	for i := 0; i < 10; i++ {
		_, err := metricsClient.GetMetrics(ctx)
		assert.NoError(t, err)
	}
	elapsed := time.Since(startTime)

	assert.Greater(t, requestCount, 0)
	assert.Less(t, elapsed, 3*time.Second, "Should complete requests within reasonable time")
}

func TestE2E_ClientConfiguration(t *testing.T) {
	t.Run("Default configuration", func(t *testing.T) {
		cfg := state.DefaultClientConfig()
		assert.NotNil(t, cfg)
		assert.NotEmpty(t, cfg.MetricsURL)
		assert.NotEmpty(t, cfg.LogsURL)
		assert.NotEmpty(t, cfg.StatusURL)
		assert.Greater(t, cfg.Timeout, 0)
		assert.Greater(t, cfg.MaxConcurrentRequests, 0)
		assert.Greater(t, int(cfg.RequestInterval), 0)
		assert.GreaterOrEqual(t, cfg.RetryMax, 0)
	})

	t.Run("Validate valid configuration", func(t *testing.T) {
		cfg := &state.ClientConfig{
			MetricsURL:            "http://localhost:8080",
			LogsURL:               "http://localhost:8080",
			StatusURL:             "http://localhost:8080",
			Timeout:               5,
			MaxConcurrentRequests: 10,
			RequestInterval:       100 * time.Millisecond,
			RetryMax:              3,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Validate invalid configuration", func(t *testing.T) {
		testCases := []struct {
			name    string
			cfg     *state.ClientConfig
			wantErr bool
		}{
			{"Empty metrics URL", &state.ClientConfig{MetricsURL: "", LogsURL: "http://localhost", StatusURL: "http://localhost", Timeout: 5}, true},
			{"Invalid timeout", &state.ClientConfig{MetricsURL: "http://localhost", LogsURL: "http://localhost", StatusURL: "http://localhost", Timeout: -1}, true},
			{"Invalid max concurrent", &state.ClientConfig{MetricsURL: "http://localhost", LogsURL: "http://localhost", StatusURL: "http://localhost", Timeout: 5, MaxConcurrentRequests: -1}, true},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.cfg.Validate()
				if tc.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}

func TestE2E_GracefulShutdown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		metrics := `icap_requests_total 1000
icap_active_connections 50`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer ts.Close()

	cfg := &state.ClientConfig{
		MetricsURL:            ts.URL,
		LogsURL:               ts.URL,
		StatusURL:             ts.URL,
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
	}

	metricsState := state.NewMetricsState(cfg)
	logsState := state.NewLogsState(cfg)

	t.Run("Make requests before shutdown", func(t *testing.T) {
		snapshot := &state.MetricsSnapshot{
			Timestamp:     time.Now(),
			RPS:           10.5,
			Connections:   100,
			Errors:        5,
			BytesSent:     1000000,
			BytesReceived: 500000,
		}
		metricsState.Update(snapshot)

		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test entry",
			Fields:    map[string]interface{}{},
		}
		logsState.AddEntry(entry)
	})

	t.Run("Shutdown gracefully", func(t *testing.T) {
		metricsState.Shutdown()
		logsState.Shutdown()

		assert.NotNil(t, metricsState.GetCurrent())
		assert.NotNil(t, logsState.GetEntries(nil, 10))
	})
}
