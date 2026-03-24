package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

const (
	maxBodySize = 1 * 1024 * 1024 // 1MB max body size
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	mu           sync.Mutex
	tokens       int
	maxTokens    int
	refillRate   time.Duration
	lastRefill   time.Time
	requestQueue chan struct{}
}

// NewRateLimiter creates a new token bucket rate limiter
func NewRateLimiter(maxRequests int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:       maxRequests,
		maxTokens:    maxRequests,
		refillRate:   interval,
		lastRefill:   time.Now(),
		requestQueue: make(chan struct{}, maxRequests),
	}
}

// Acquire acquires a token from the bucket, blocking if necessary
func (rl *RateLimiter) Acquire(ctx context.Context) error {
	// Acquire queue slot with proper synchronization
	rl.mu.Lock()

	select {
	case rl.requestQueue <- struct{}{}:
		rl.mu.Unlock()
	default:
		rl.mu.Unlock()
		// Queue is full, wait with context
		select {
		case rl.requestQueue <- struct{}{}:
		case <-ctx.Done():
			return fmt.Errorf("rate limit wait cancelled: %w", ctx.Err())
		}
	}

	// Ensure queue slot is released
	defer func() {
		rl.mu.Lock()
		<-rl.requestQueue
		rl.mu.Unlock()
	}()

	// Acquire a token with refill
	rl.mu.Lock()

	// Refill tokens if needed
	rl.refillLocked()

	for rl.tokens <= 0 {
		rl.mu.Unlock()
		// Wait for refill
		waitTime := rl.refillRate
		select {
		case <-time.After(waitTime):
		case <-ctx.Done():
			return fmt.Errorf("rate limit wait cancelled: %w", ctx.Err())
		}

		rl.mu.Lock()
		rl.refillLocked()
	}

	rl.tokens--
	rl.mu.Unlock()
	return nil
}

// refill refills tokens based on elapsed time
func (rl *RateLimiter) refill() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refillLocked()
}

// refillLocked refills tokens (must be called with mutex held)
func (rl *RateLimiter) refillLocked() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	if elapsed >= rl.refillRate {
		tokensToAdd := int(elapsed / rl.refillRate)
		if newTokens := rl.tokens + tokensToAdd; newTokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		} else {
			rl.tokens = newTokens
		}
		rl.lastRefill = now
	}
}

// doRequestWithRetry executes an HTTP request with exponential backoff retry
func (c *ServerClient) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	var resp *http.Response
	err := c.circuitBreaker.Execute(ctx, func() error {
		var err error
		resp, err = utils.DoWithRetryHTTP(ctx, c.retryConfig, c.httpClient, req)
		return err
	})

	if err != nil {
		if errors.Is(err, utils.CircuitOpenError) {
			return nil, fmt.Errorf("circuit breaker is open: %w", err)
		}
		return nil, err
	}

	return resp, nil
}

// ServerClient provides HTTP client for server integration
type ServerClient struct {
	baseURL        string
	httpClient     *http.Client
	rateLimiter    *RateLimiter
	retryConfig    utils.RetryConfig
	circuitBreaker *utils.CircuitBreaker
}

// NewServerClient creates a new server client with connection pooling
func NewServerClient(host string, port int) *ServerClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		MaxConnsPerHost:     20,
	}

	return &ServerClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		rateLimiter:    NewRateLimiter(10, 100*time.Millisecond),
		retryConfig:    utils.DefaultRetryConfig(),
		circuitBreaker: utils.DefaultMetricsCircuitBreaker(),
	}
}

// GetMetrics fetches metrics from the server
func (c *ServerClient) GetMetrics(ctx context.Context) (*state.MetricsSnapshot, error) {
	url := fmt.Sprintf("%s/metrics", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics from server: %w", err)
	}
	defer resp.Body.Close()

	// Validate status code
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("metrics endpoint not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("server error while fetching metrics (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code when fetching metrics: %d", resp.StatusCode)
	}

	// Limit body size
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("unexpected EOF while reading metrics body")
		}
		return nil, fmt.Errorf("failed to read metrics response body: %w", err)
	}

	return c.parseMetrics(string(body))
}

// parseMetrics parses Prometheus metrics format from response body
func (c *ServerClient) parseMetrics(body string) (*state.MetricsSnapshot, error) {
	snapshot := &state.MetricsSnapshot{
		Timestamp:     time.Now(),
		RPS:           0,
		LatencyP50:    0,
		LatencyP95:    0,
		LatencyP99:    0,
		Connections:   0,
		Errors:        0,
		BytesSent:     0,
		BytesReceived: 0,
	}

	lines := strings.Split(body, "\n")
	var totalRequests float64
	requestDurations := make(map[string]float64)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse icap_requests_total
		if strings.HasPrefix(line, "icap_requests_total") {
			if val := parseSimpleMetric(line); val > 0 {
				totalRequests += val
			}
		}

		// Parse icap_request_duration_seconds (histogram)
		if strings.HasPrefix(line, "icap_request_duration_seconds{") {
			if val, quantile := parseHistogramMetric(line); val > 0 {
				switch quantile {
				case "0.5":
					requestDurations["p50"] = val
				case "0.95":
					requestDurations["p95"] = val
				case "0.99":
					requestDurations["p99"] = val
				}
			}
		}

		// Parse icap_active_connections
		if strings.HasPrefix(line, "icap_active_connections") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.Connections = int(val)
			}
		}

		// Parse icap_errors_total
		if strings.HasPrefix(line, "icap_errors_total") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.Errors += int(val)
			}
		}

		// Parse request/response sizes
		if strings.HasPrefix(line, "icap_response_size_bytes") && !strings.Contains(line, "{") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.BytesSent = int64(val)
			}
		}
		if strings.HasPrefix(line, "icap_request_size_bytes") && !strings.Contains(line, "{") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.BytesReceived = int64(val)
			}
		}
	}

	// Calculate RPS (simplified - should track time between scrapes in production)
	if totalRequests > 0 {
		snapshot.RPS = totalRequests / 60.0
	}

	// Set latencies (convert from seconds to milliseconds)
	snapshot.LatencyP50 = requestDurations["p50"] * 1000
	snapshot.LatencyP95 = requestDurations["p95"] * 1000
	snapshot.LatencyP99 = requestDurations["p99"] * 1000

	return snapshot, nil
}

// parseSimpleMetric parses a metric without labels
func parseSimpleMetric(line string) float64 {
	// Format: metric_name 123.45 or metric_name{labels} 123.45
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0
	}

	value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0
	}

	return value
}

// parseHistogramMetric parses a histogram metric with quantile
func parseHistogramMetric(line string) (float64, string) {
	// Format: metric_name{method="REQMOD",quantile="0.5"} 0.05
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, ""
	}

	value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, ""
	}

	// Extract quantile from labels
	labelPart := parts[0]
	if strings.Contains(labelPart, `quantile="`) {
		start := strings.Index(labelPart, `quantile="`) + 10
		end := strings.Index(labelPart[start:], `"`)
		if end > 0 {
			quantile := labelPart[start : start+end]
			return value, quantile
		}
	}

	return value, ""
}

// GetLogs fetches recent logs from the server
func (c *ServerClient) GetLogs(ctx context.Context, limit int) ([]*state.LogEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("invalid limit: %d, must be positive", limit)
	}
	if limit > 1000 {
		return nil, fmt.Errorf("invalid limit: %d, maximum is 1000", limit)
	}

	url := fmt.Sprintf("%s/logs?limit=%d", c.baseURL, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create logs request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch logs from server: %w", err)
	}
	defer resp.Body.Close()

	// Validate status code
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("logs endpoint not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("server error while fetching logs (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code when fetching logs: %d", resp.StatusCode)
	}

	// Parse logs from JSON response
	var logs []*state.LogEntry
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, fmt.Errorf("failed to decode logs response: %w", err)
	}

	return logs, nil
}

// CheckHealth checks the server health status
func (c *ServerClient) CheckHealth(ctx context.Context) (*state.ServerStatusInfo, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return &state.ServerStatusInfo{
			State:  "stopped",
			Port:   "1344",
			Uptime: "0s",
		}, nil
	}
	defer resp.Body.Close()

	// Parse health response
	var healthResp struct {
		Status string `json:"status"`
		Port   int    `json:"port"`
		Uptime string `json:"uptime"`
	}

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return &state.ServerStatusInfo{
				State:  "running",
				Port:   "1344",
				Uptime: "unknown",
			}, nil
		}

		if err := json.Unmarshal(body, &healthResp); err != nil {
			// If JSON parsing fails, assume server is running based on status code
			return &state.ServerStatusInfo{
				State:  "running",
				Port:   "1344",
				Uptime: "unknown",
			}, nil
		}

		return &state.ServerStatusInfo{
			State:  healthResp.Status,
			Port:   fmt.Sprintf("%d", healthResp.Port),
			Uptime: healthResp.Uptime,
		}, nil
	}

	return &state.ServerStatusInfo{
		State:  "stopped",
		Port:   "1344",
		Uptime: "0s",
	}, nil
}
