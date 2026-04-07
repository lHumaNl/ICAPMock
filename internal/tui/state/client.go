// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	lastRefill   time.Time
	requestQueue chan struct{}
	tokens       int
	maxTokens    int
	refillRate   time.Duration
	mu           sync.Mutex
}

// NewRateLimiter creates a new token bucket rate limiter.
func NewRateLimiter(maxRequests int, interval time.Duration) *RateLimiter {
	if maxRequests <= 0 {
		maxRequests = 10 // sensible default to avoid blocking on unbuffered channel
	}
	return &RateLimiter{
		tokens:       maxRequests,
		maxTokens:    maxRequests,
		refillRate:   interval,
		lastRefill:   time.Now(),
		requestQueue: make(chan struct{}, maxRequests),
	}
}

// Acquire acquires a token from the bucket, blocking if necessary.
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
			return fmt.Errorf("rate limit wait canceled: %w", ctx.Err())
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
			return fmt.Errorf("rate limit wait canceled: %w", ctx.Err())
		}

		rl.mu.Lock()
		rl.refillLocked()
	}

	rl.tokens--
	rl.mu.Unlock()
	return nil
}

// refill refills tokens based on elapsed time.
func (rl *RateLimiter) refill() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refillLocked()
}

// refillLocked refills tokens (must be called with mutex held).
func (rl *RateLimiter) refillLocked() {
	if rl.refillRate <= 0 {
		// No refill interval configured; immediately replenish all tokens.
		rl.tokens = rl.maxTokens
		return
	}
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

type MetricsClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	cfg         *ClientConfig
	baseURL     string
}

func NewMetricsClient(cfg *ClientConfig) *MetricsClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	return &MetricsClient{
		baseURL: cfg.MetricsURL,
		httpClient: &http.Client{
			Timeout:   time.Duration(cfg.Timeout) * time.Second,
			Transport: transport,
		},
		rateLimiter: NewRateLimiter(cfg.MaxConcurrentRequests, cfg.RequestInterval),
		cfg:         cfg,
	}
}

func (c *MetricsClient) GetMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/metrics", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("Metrics endpoint not found")
		return nil, fmt.Errorf("metrics endpoint not found")
	}
	if resp.StatusCode == http.StatusInternalServerError {
		log.Printf("Server error while fetching metrics")
		return nil, fmt.Errorf("server error while fetching metrics")
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Unexpected status code: %d", resp.StatusCode)
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return c.parseMetrics(string(body))
}

func (c *MetricsClient) parseMetrics(body string) (*MetricsSnapshot, error) {
	snapshot := &MetricsSnapshot{
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
	rpsCounters := make(map[string]float64)
	requestDurations := make(map[string]float64)
	var totalRequests float64
	var totalDuration float64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "icap_requests_total{") {
			val, method := parseCounterMetric(line)
			if val > 0 {
				rpsCounters[method] = val
				totalRequests += val
			}
		}

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

		if strings.HasPrefix(line, "icap_request_duration_seconds_sum") {
			if val := parseSimpleMetric(line); val > 0 {
				totalDuration = val
			}
		}

		if strings.HasPrefix(line, "icap_request_duration_seconds_count") {
			if val := parseSimpleMetric(line); val > 0 {
				if totalRequests == 0 {
					totalRequests = val
				}
				if totalDuration > 0 && totalRequests > 0 {
					avgLatency := (totalDuration / totalRequests) * 1000
					if requestDurations["p50"] == 0 {
						requestDurations["p50"] = avgLatency
					}
				}
			}
		}

		if strings.HasPrefix(line, "icap_active_connections") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.Connections = int(val)
			}
		}

		if strings.HasPrefix(line, "icap_errors_total{") {
			val, _ := parseCounterMetric(line)
			if val > 0 {
				snapshot.Errors += int(val)
			}
		}

		if strings.HasPrefix(line, "icap_response_size_bytes_sum") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.BytesSent = int64(val)
			}
		}
		if strings.HasPrefix(line, "icap_request_size_bytes_sum") {
			if val := parseSimpleMetric(line); val > 0 {
				snapshot.BytesReceived = int64(val)
			}
		}
	}

	snapshot.RPS = calculateRPS(rpsCounters)
	snapshot.LatencyP50 = requestDurations["p50"] * 1000
	snapshot.LatencyP95 = requestDurations["p95"] * 1000
	snapshot.LatencyP99 = requestDurations["p99"] * 1000

	return snapshot, nil
}

func parseCounterMetric(line string) (float64, string) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, ""
	}

	value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, ""
	}

	labelPart := parts[0]
	if strings.Contains(labelPart, `method="`) {
		start := strings.Index(labelPart, `method="`) + 8
		end := strings.Index(labelPart[start:], `"`)
		if end > 0 {
			method := labelPart[start : start+end]
			return value, method
		}
	}

	return value, ""
}

func parseHistogramMetric(line string) (float64, string) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, ""
	}

	value, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, ""
	}

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

func parseSimpleMetric(line string) float64 {
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

func calculateRPS(counters map[string]float64) float64 {
	var total float64
	for _, val := range counters {
		total += val
	}

	if total > 0 {
		return total / 60.0
	}

	return 0
}

type LogsClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	cfg         *ClientConfig
	baseURL     string
}

func NewLogsClient(cfg *ClientConfig) *LogsClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	return &LogsClient{
		baseURL: cfg.LogsURL,
		httpClient: &http.Client{
			Timeout:   time.Duration(cfg.Timeout) * time.Second,
			Transport: transport,
		},
		rateLimiter: NewRateLimiter(cfg.MaxConcurrentRequests, cfg.RequestInterval),
		cfg:         cfg,
	}
}

func (c *LogsClient) GetLogs(ctx context.Context, limit int) ([]*LogEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	if limit > 10000 {
		return nil, fmt.Errorf("limit too large, maximum is 10000, got %d", limit)
	}

	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/logs?limit=%d", c.baseURL, limit)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch logs: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("Logs endpoint not found")
		return nil, fmt.Errorf("logs endpoint not found")
	}
	if resp.StatusCode == http.StatusInternalServerError {
		log.Printf("Server error while fetching logs")
		return nil, fmt.Errorf("server error while fetching logs")
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Unexpected status code: %d", resp.StatusCode)
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var logs []*LogEntry
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, fmt.Errorf("failed to decode logs: %w", err)
	}

	return logs, nil
}

type StatusClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	cfg         *ClientConfig
	baseURL     string
}

func NewStatusClient(cfg *ClientConfig) *StatusClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	return &StatusClient{
		baseURL: cfg.StatusURL,
		httpClient: &http.Client{
			Timeout:   time.Duration(cfg.Timeout) * time.Second,
			Transport: transport,
		},
		rateLimiter: NewRateLimiter(cfg.MaxConcurrentRequests, cfg.RequestInterval),
		cfg:         cfg,
	}
}

// ServerStatusInfo represents server status information.
type ServerStatusInfo struct {
	State  string
	Port   string
	Uptime string
	Error  string
}

func (c *StatusClient) GetStatus(ctx context.Context) (ServerStatusInfo, error) {
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return ServerStatusInfo{}, fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return ServerStatusInfo{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to fetch status: %v", err)
		return ServerStatusInfo{State: "stopped", Port: "1344", Uptime: "0s"}, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusInternalServerError {
		log.Printf("Server error while fetching status")
		return ServerStatusInfo{State: "error", Port: "1344", Uptime: "0s"}, nil
	}
	if resp.StatusCode == http.StatusOK {
		var healthResp struct {
			Status string `json:"status"`
			Uptime string `json:"uptime"`
			Port   int    `json:"port"`
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return ServerStatusInfo{
				State:  "running",
				Port:   "1344",
				Uptime: "unknown",
			}, nil
		}

		if err := json.Unmarshal(body, &healthResp); err == nil {
			return ServerStatusInfo{
				State:  healthResp.Status,
				Port:   fmt.Sprintf("%d", healthResp.Port),
				Uptime: healthResp.Uptime,
			}, nil
		}

		return ServerStatusInfo{
			State:  "running",
			Port:   "1344",
			Uptime: "unknown",
		}, nil
	}

	return ServerStatusInfo{State: "stopped", Port: "1344", Uptime: "0s"}, nil
}

func (c *StatusClient) GetBaseURL() string {
	return c.baseURL
}
