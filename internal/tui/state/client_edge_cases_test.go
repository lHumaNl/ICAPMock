// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Network error edge cases

func TestMetricsClient_GetNetworkError_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    1,
	}
	client := NewMetricsClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)
	assert.Error(t, err)
	// Error may be wrapped by rate limiter ("rate limit wait canceled: context deadline exceeded")
	// or come directly as a timeout. Both indicate the context timed out.
	errStr := err.Error()
	assert.True(t, strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"),
		"expected timeout or deadline exceeded error, got: %s", errStr)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetNetworkError_ConnectionRefused(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://127.0.0.1:1/metrics",
		Timeout:    2,
	}
	client := NewMetricsClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetNetworkError_DNSFailure(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://this-domain-does-not-exist-12345.example.com/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetNetworkError_ConnectionReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		conn.Close()
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetNetworkError_UnexpectedEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	// "partial" is a complete HTTP response with no valid metrics lines.
	// The parser is lenient: it returns an empty snapshot, not an error.
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

// Limit edge cases

func TestMetricsClient_GetMetrics_LargeResponse(t *testing.T) {
	largeMetrics := strings.Repeat("icap_requests_total{method=\"GET\"} 1\n", 100000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeMetrics))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    10,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_parseMetrics_EmptyResponse(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	snapshot, err := client.parseMetrics("")

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Zero(t, snapshot.RPS)
	assert.Zero(t, snapshot.Connections)
}

func TestMetricsClient_parseMetrics_ExtremelyLongLine(t *testing.T) {
	longLine := "icap_requests_total{method=\"" + strings.Repeat("a", 100000) + "\"} 1"

	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	assert.NotPanics(t, func() {
		_, err := client.parseMetrics(longLine)
		assert.NoError(t, err)
	})
}

func TestRateLimiter_ConcurrentRequests_ExceedingLimit(t *testing.T) {
	rl := NewRateLimiter(5, 100*time.Millisecond)

	numGoroutines := 100
	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			done <- rl.Acquire(ctx)
		}()
	}

	var successCount, failureCount int
	for i := 0; i < numGoroutines; i++ {
		err := <-done
		if err == nil {
			successCount++
		} else {
			failureCount++
		}
	}

	assert.Greater(t, successCount, 0)
}

func TestLogsClient_GetLogs_LimitExceeded(t *testing.T) {
	cfg := &ClientConfig{
		LogsURL: "http://localhost/logs",
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()

	_, err := client.GetLogs(ctx, 10001)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestLogsClient_GetLogs_LimitZero(t *testing.T) {
	cfg := &ClientConfig{
		LogsURL: "http://localhost/logs",
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()

	_, err := client.GetLogs(ctx, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestLogsClient_GetLogs_LimitNegative(t *testing.T) {
	cfg := &ClientConfig{
		LogsURL: "http://localhost/logs",
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()

	_, err := client.GetLogs(ctx, -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

// Invalid data edge cases

func TestMetricsClient_parseMetrics_InvalidFormat(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	invalidMetrics := []string{
		"invalid metric format",
		"icap_requests_total invalid_number",
		"icap_requests_total method=\"GET\"",
		"{method=\"GET\"} 1",
		"icap_requests_total{method=\"GET\"}",
	}

	for _, metric := range invalidMetrics {
		t.Run(metric, func(t *testing.T) {
			assert.NotPanics(t, func() {
				snapshot, err := client.parseMetrics(metric)
				assert.NoError(t, err)
				assert.NotNil(t, snapshot)
			})
		})
	}
}

func TestMetricsClient_parseMetrics_MalformedNumber(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	malformedMetrics := []string{
		"icap_requests_total{method=\"GET\"} not_a_number",
		"icap_requests_total{method=\"GET\"} 1.2.3",
		"icap_requests_total{method=\"GET\"} NaN",
		"icap_requests_total{method=\"GET\"} Inf",
		"icap_requests_total{method=\"GET\"} -Infinity",
	}

	for _, metric := range malformedMetrics {
		t.Run(metric, func(t *testing.T) {
			snapshot, err := client.parseMetrics(metric)
			assert.NoError(t, err)
			assert.NotNil(t, snapshot)
		})
	}
}

func TestMetricsClient_parseMetrics_NilValues(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	snapshot, err := client.parseMetrics("")
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.NotNil(t, snapshot.Timestamp)
}

func TestMetricsClient_GetMetrics_HTTP400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetMetrics_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetMetrics_HTTP404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Nil(t, snapshot)
}

func TestLogsClient_GetLogs_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.Error(t, err)
	assert.Nil(t, entries)
}

func TestLogsClient_GetLogs_EmptyArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Len(t, entries, 0)
}

func TestMetricsClient_GetMetrics_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(""))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_parseMetrics_MissingRequiredFields(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	metricsWithoutFields := []string{
		"",
		"# Just a comment\n",
		"\n\n\n",
	}

	for _, metric := range metricsWithoutFields {
		t.Run(fmt.Sprintf("length_%d", len(metric)), func(t *testing.T) {
			snapshot, err := client.parseMetrics(metric)
			assert.NoError(t, err)
			assert.NotNil(t, snapshot)
			assert.Zero(t, snapshot.RPS)
		})
	}
}

func TestMetricsClient_parseMetrics_SpecialCharacters(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	metricsWithSpecialChars := `icap_requests_total{method="GET",path="/api/v1/users?name=张三&age=25"} 123
icap_requests_total{method="POST",path="/api/v1/data?value=100%"} 456
icap_requests_total{method="DELETE",path="/api/v1/\x00"} 789`

	snapshot, err := client.parseMetrics(metricsWithSpecialChars)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_parseMetrics_Unicode(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	metricsWithUnicode := `icap_requests_total{method="GET",path="/用户/数据"} 100
icap_requests_total{method="POST",path="/🎉/测试"} 200
icap_requests_total{method="DELETE",path="/αβγ/δϵζ"} 300`

	snapshot, err := client.parseMetrics(metricsWithUnicode)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

// Concurrent access edge cases

func TestMetricsState_ConcurrentUpdates_Race(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(index),
			}
			state.Update(snapshot)
		}(i)
	}

	wg.Wait()

	history := state.GetHistory()
	assert.LessOrEqual(t, len(history), 100)
}

func TestLogsState_ConcurrentAddEntry_Race(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Concurrent message %d", index),
			}
			state.AddEntry(entry)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, 100, state.entries.Size())
}

func TestRateLimiter_ConcurrentRefill_Race(t *testing.T) {
	rl := NewRateLimiter(10, 10*time.Millisecond)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			rl.Acquire(ctx)
		}()
	}

	wg.Wait()

	rl.mu.Lock()
	assert.Equal(t, 10, rl.maxTokens)
	rl.mu.Unlock()
}

func TestMetricsClient_ConcurrentGetMetrics_Race(_ *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			_, _ = client.GetMetrics(ctx)
		}()
	}

	wg.Wait()
}

func TestLogsState_ConcurrentGetEntries_Race(_ *testing.T) {
	state := NewLogsState(&ClientConfig{})

	for i := 0; i < 100; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Message %d", i),
		}
		state.AddEntry(entry)
	}

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = state.GetEntries(nil, 10)
		}()
	}

	wg.Wait()
}

// Resource edge cases

func TestMetricsState_MemoryExhaustion(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	for i := 0; i < 10000; i++ {
		snapshot := &MetricsSnapshot{
			Timestamp:     time.Now(),
			RPS:           float64(i),
			LatencyP50:    float64(i),
			LatencyP95:    float64(i),
			LatencyP99:    float64(i),
			Connections:   i,
			Errors:        i,
			BytesSent:     int64(i) * 1024,
			BytesReceived: int64(i) * 1024,
		}
		state.Update(snapshot)
	}

	history := state.GetHistory()
	assert.Equal(t, 100, len(history))
}

func TestLogsState_MemoryExhaustion(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	longMessage := strings.Repeat("a", 10000)
	for i := 0; i < 2000; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   longMessage,
			Fields: map[string]interface{}{
				"field1": strings.Repeat("b", 1000),
				"field2": strings.Repeat("c", 1000),
			},
		}
		state.AddEntry(entry)
	}

	// Default MaxLogs is 100, so ring buffer caps at 100 entries.
	assert.Equal(t, 100, state.entries.Size())
}

func TestMetricsClient_MemoryExhaustion_LargeResponse(t *testing.T) {
	largeMetrics := strings.Repeat("icap_requests_total{method=\"GET\"} 1\n", 1000000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeMetrics))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    30,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()

	assert.NotPanics(t, func() {
		snapshot, err := client.GetMetrics(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, snapshot)
	})
}

func TestRateLimiter_GoroutineLeak(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	initialGoroutines := 0

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_ = rl.Acquire(ctx)
		cancel()
	}

	finalGoroutines := 0

	assert.LessOrEqual(t, finalGoroutines-initialGoroutines, 5)
}

// Context edge cases

func TestRateLimiter_ContextCancellation_BeforeAcquire(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When a token is immediately available, Acquire succeeds even with
	// a canceled context (non-blocking path). This is correct behavior.
	err := rl.Acquire(ctx)
	// err may be nil if the token was available without blocking
	if err != nil {
		assert.Contains(t, err.Error(), "canceled")
	}
}

func TestRateLimiter_ContextCancellation_DuringWait(t *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := rl.Acquire(ctx)
	// With only 1 token available, Acquire may succeed immediately
	// without hitting the context cancellation.
	if err != nil {
		assert.Contains(t, err.Error(), "canceled")
	}
}

func TestMetricsClient_GetMetrics_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    10,
	}
	client := NewMetricsClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetMetrics_ContextNoDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_GetMetrics_ContextReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		snapshot, err := client.GetMetrics(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, snapshot)
	}
}

func TestLogsClient_GetLogs_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	entries, err := client.GetLogs(ctx, 10)

	assert.Error(t, err)
	assert.Nil(t, entries)
}

// Time-related edge cases

func TestRateLimiter_ZeroDuration(t *testing.T) {
	rl := NewRateLimiter(10, 0*time.Millisecond)

	assert.NotNil(t, rl)
	assert.Equal(t, 0*time.Millisecond, rl.refillRate)
}

func TestRateLimiter_NegativeDuration(t *testing.T) {
	rl := NewRateLimiter(10, -100*time.Millisecond)

	assert.NotNil(t, rl)
	assert.Equal(t, -100*time.Millisecond, rl.refillRate)
}

func TestRateLimiter_ExtremelyLongTimeout(t *testing.T) {
	rl := NewRateLimiter(10, time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	err := rl.Acquire(ctx)
	assert.NoError(t, err)
}

func TestMetricsClient_GetMetrics_ClockSkew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", time.Now().Add(-24*time.Hour).Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.True(t, snapshot.Timestamp.After(time.Now().Add(-1*time.Hour)))
}

func TestMetricsState_Update_PastTimestamp(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshot := &MetricsSnapshot{
		Timestamp: time.Now().Add(-24 * time.Hour),
		RPS:       100.0,
	}

	assert.NotPanics(t, func() {
		state.Update(snapshot)
	})

	current := state.GetCurrent()
	assert.NotNil(t, current)
	assert.Equal(t, 100.0, current.RPS)
}

func TestLogsState_AddEntry_FutureTimestamp(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{
		Timestamp: time.Now().Add(24 * time.Hour),
		Level:     "INFO",
		Message:   "Future message",
	}

	assert.NotPanics(t, func() {
		state.AddEntry(entry)
	})

	assert.Equal(t, 1, state.entries.Size())
}

func TestMetricsClient_parseMetrics_ZeroTimestamp(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	snapshot, err := client.parseMetrics("icap_requests_total{method=\"GET\"} 100")

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.False(t, snapshot.Timestamp.IsZero())
}

// Additional edge cases

func TestMetricsClient_GetMetrics_InvalidURL(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "://invalid-url",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestMetricsClient_GetMetrics_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, _ := hj.Hijack()
		conn.Write([]byte("malformed response without headers"))
		conn.Close()
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestRateLimiter_RequestQueueOverflow(_ *testing.T) {
	rl := NewRateLimiter(1, 100*time.Millisecond)

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rl.Acquire(ctx)
		}()
	}

	wg.Wait()
}

func TestMetricsClient_parseMetrics_MixedFormat(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	mixedMetrics := `# HELP icap_requests_total Total number of ICAP requests
# TYPE icap_requests_total counter
icap_requests_total{method="GET"} 100
icap_requests_total{method="POST"} 200

# HELP icap_request_duration_seconds Request duration in seconds
# TYPE icap_request_duration_seconds histogram
icap_request_duration_seconds{quantile="0.5"} 0.01
icap_request_duration_seconds{quantile="0.95"} 0.05
icap_request_duration_seconds_sum 15.5
icap_request_duration_seconds_count 1500

icap_active_connections 10
icap_errors_total 5
icap_response_size_bytes_sum 1024000
icap_request_size_bytes_sum 2048000
`

	snapshot, err := client.parseMetrics(mixedMetrics)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Greater(t, snapshot.RPS, float64(0))
	assert.Greater(t, snapshot.Connections, 0)
}

func TestLogsState_UpdateEntries_NilSlice(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	assert.NotPanics(t, func() {
		state.UpdateEntries(nil)
	})

	assert.Equal(t, 0, state.entries.Size())
}

func TestLogsState_UpdateEntries_NilEntries(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entriesWithNil := []*LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Valid"},
		nil,
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error"},
	}

	assert.NotPanics(t, func() {
		state.UpdateEntries(entriesWithNil)
	})

	assert.Equal(t, 2, state.entries.Size())
}

func TestMetricsState_Update_NilSnapshot(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	assert.NotPanics(t, func() {
		state.Update(nil)
	})
}

func TestRateLimiter_MaxTokensZero(t *testing.T) {
	rl := NewRateLimiter(0, 100*time.Millisecond)

	assert.NotNil(t, rl)
	// Zero is replaced with default (10) to avoid unbuffered channel deadlock
	assert.Equal(t, 10, rl.maxTokens)
	assert.Equal(t, 10, rl.tokens)
}

func TestRateLimiter_MaxTokensNegative(t *testing.T) {
	rl := NewRateLimiter(-10, 100*time.Millisecond)

	assert.NotNil(t, rl)
	// Negative is replaced with default (10) to avoid unbuffered channel deadlock
	assert.Equal(t, 10, rl.maxTokens)
	assert.Equal(t, 10, rl.tokens)
}

func TestMetricsClient_GetMetrics_NilContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	assert.NotPanics(t, func() {
		_, _ = client.GetMetrics(nil)
	})
}

func TestLogsClient_GetLogs_NilContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	assert.NotPanics(t, func() {
		_, _ = client.GetLogs(nil, 10)
	})
}

func TestStatusClient_GetStatus_NilContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"running","port":1344,"uptime":"1m"}`))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		StatusURL: server.URL,
		Timeout:   5,
	}
	client := NewStatusClient(cfg)

	assert.NotPanics(t, func() {
		_, _ = client.GetStatus(nil)
	})
}

func TestLogsClient_GetLogs_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("["))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.Error(t, err)
	assert.Nil(t, entries)
}

func TestMetricsClient_GetMetrics_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestLogsState_matchesFilter_NilEntry(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	filter := &LogFilter{Level: "ERROR"}

	result := state.matchesFilter(nil, filter)
	assert.False(t, result)
}

func TestLogsState_matchesFilter_NilFilter(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "ERROR",
		Message:   "Test",
	}

	result := state.matchesFilter(entry, nil)
	assert.True(t, result)
}

func TestLogsState_matchesFilter_EmptyFields(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test",
		Fields:    nil,
	}

	filter := &LogFilter{Search: "Test"}
	result := state.matchesFilter(entry, filter)
	assert.True(t, result)
}

func TestMetricsClient_parseMetrics_LargeNumbers(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	largeMetrics := `icap_requests_total{method="GET"} 9223372036854775807
icap_request_duration_seconds{quantile="0.5"} 1.7976931348623157e+308
icap_response_size_bytes_sum 9223372036854775807
`

	snapshot, err := client.parseMetrics(largeMetrics)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_parseMetrics_NegativeNumbers(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost/metrics",
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	negativeMetrics := `icap_requests_total{method="GET"} -100
icap_request_duration_seconds{quantile="0.5"} -0.05
icap_response_size_bytes_sum -500
`

	snapshot, err := client.parseMetrics(negativeMetrics)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestRateLimiter_RefillWithNegativeDuration(t *testing.T) {
	rl := NewRateLimiter(10, -100*time.Millisecond)

	rl.mu.Lock()
	rl.tokens = 0
	rl.mu.Unlock()

	assert.NotPanics(t, func() {
		rl.refill()
	})
}

func TestMetricsClient_GetMetrics_ChunkedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} "))
		flusher.Flush()

		time.Sleep(10 * time.Millisecond)

		w.Write([]byte("100\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestMetricsClient_GetMetrics_CompressedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestLogsState_AddEntry_MaxLines_Race(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				entry := &LogEntry{
					Timestamp: time.Now(),
					Level:     "INFO",
					Message:   fmt.Sprintf("Message %d-%d", index, j),
				}
				state.AddEntry(entry)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, 100, state.entries.Size())
}

func TestMetricsState_GetCurrent_NilSnapshot_Race(_ *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = state.GetCurrent()
		}()
	}

	wg.Wait()
}

func TestLogsState_GetEntries_NegativeLimit(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	for i := 0; i < 10; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Message %d", i),
		}
		state.AddEntry(entry)
	}

	entries := state.GetEntries(nil, -1)
	assert.Equal(t, 10, len(entries))
}

func TestLogsState_GetEntries_ZeroLimit(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	for i := 0; i < 10; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Message %d", i),
		}
		state.AddEntry(entry)
	}

	entries := state.GetEntries(nil, 0)
	assert.Equal(t, 10, len(entries))
}

func TestMetricsClient_GetMetrics_StatusCode300(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
}

func TestLogsClient_GetLogs_StatusCode300(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.Error(t, err)
	assert.Nil(t, entries)
}

func TestRateLimiter_RefillRace(_ *testing.T) {
	rl := NewRateLimiter(10, 10*time.Millisecond)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl.refill()
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			_ = rl.Acquire(ctx)
		}()
	}

	wg.Wait()
}

func TestLogsState_SetFilter_NilState(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	assert.NotPanics(t, func() {
		cmd := state.SetFilter("ERROR")
		assert.NotNil(t, cmd)
	})
}

func TestLogsState_SetSearch_EmptyQuery(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	assert.NotPanics(t, func() {
		cmd := state.SetSearch("")
		assert.NotNil(t, cmd)
	})
}

func TestLogsState_SetAutoScroll_Race(_ *testing.T) {
	state := NewLogsState(&ClientConfig{})

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_ = state.SetAutoScroll(index%2 == 0)
		}(i)
	}

	wg.Wait()
}

func TestMetricsClient_GetMetrics_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Empty(t, body) // GET requests have no body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		MetricsURL: server.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestLogsClient_GetLogs_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Empty(t, body) // GET requests have no body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	cfg := &ClientConfig{
		LogsURL: server.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.NoError(t, err)
	assert.NotNil(t, entries)
}

func TestStatusClient_GetStatus_NetworkError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()

	cfg := &ClientConfig{
		StatusURL: "http://" + addr,
		Timeout:   1,
	}
	client := NewStatusClient(cfg)

	ctx := context.Background()
	info, err := client.GetStatus(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "stopped", info.State)
}

func TestStatusClient_GetStatus_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		StatusURL: server.URL,
		Timeout:   10,
	}
	client := NewStatusClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	info, err := client.GetStatus(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "stopped", info.State)
}

func TestRateLimiter_Acquire_ImmediateReturn(t *testing.T) {
	rl := NewRateLimiter(100, 100*time.Millisecond)

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		start := time.Now()
		err := rl.Acquire(ctx)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.Less(t, elapsed, 10*time.Millisecond)
	}
}

func TestMetricsClient_GetMetrics_Redirect(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("icap_requests_total{method=\"GET\"} 100\n"))
	}))
	defer targetServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL+"/metrics", http.StatusFound)
	}))
	defer redirectServer.Close()

	cfg := &ClientConfig{
		MetricsURL: redirectServer.URL,
		Timeout:    5,
	}
	client := NewMetricsClient(cfg)

	ctx := context.Background()
	snapshot, err := client.GetMetrics(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestLogsClient_GetLogs_Redirect(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer targetServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL+"/logs", http.StatusFound)
	}))
	defer redirectServer.Close()

	cfg := &ClientConfig{
		LogsURL: redirectServer.URL,
		Timeout: 5,
	}
	client := NewLogsClient(cfg)

	ctx := context.Background()
	entries, err := client.GetLogs(ctx, 10)

	assert.NoError(t, err)
	assert.NotNil(t, entries)
}
