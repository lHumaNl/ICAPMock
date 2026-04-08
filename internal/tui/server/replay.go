// Copyright 2026 ICAP Mock

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

// ReplayRequest represents a recorded request for replay.
type ReplayRequest struct {
	Timestamp  time.Time         `json:"timestamp"`
	Headers    map[string]string `json:"headers"`
	ID         string            `json:"id"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Body       string            `json:"body"`
	Response   string            `json:"response"`
	StatusCode int               `json:"status_code"`
	Duration   time.Duration     `json:"duration"`
}

// ReplayConfig represents replay configuration.
type ReplayConfig struct {
	TargetURL string  `json:"target_url"`
	Speed     float64 `json:"speed"`
	Async     bool    `json:"async"`
}

// ReplayResult represents the result of a single request replay.
type ReplayResult struct {
	ID         string        `json:"id"`
	Error      string        `json:"error,omitempty"`
	StatusCode int           `json:"status_code"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
}

// ReplaySummary represents the summary of a replay session.
type ReplaySummary struct {
	StartTime      time.Time      `json:"start_time"`
	EndTime        time.Time      `json:"end_time"`
	RequestResults []ReplayResult `json:"request_results"`
	TotalRequests  int            `json:"total_requests"`
	SuccessCount   int            `json:"success_count"`
	FailureCount   int            `json:"failure_count"`
	TotalDuration  time.Duration  `json:"total_duration"`
	AverageLatency time.Duration  `json:"average_latency"`
}

// ReplayClient provides HTTP client for replay operations.
type ReplayClient struct {
	baseURL     string
	httpClient  *http.Client
	rateLimiter *RateLimiter
	retryConfig utils.RetryConfig
}

// NewReplayClient creates a new replay client with connection pooling.
func NewReplayClient(host string, port int) *ReplayClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		MaxConnsPerHost:     20,
	}

	return &ReplayClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		rateLimiter: NewRateLimiter(5, 200*time.Millisecond),
		retryConfig: utils.DefaultRetryConfig(),
	}
}

// doRequestWithRetry executes an HTTP request with exponential backoff retry.
func (c *ReplayClient) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	return utils.DoWithRetryHTTP(ctx, c.retryConfig, c.httpClient, req)
}

// ListRequests fetches all recorded requests from the server.
func (c *ReplayClient) ListRequests(ctx context.Context) ([]ReplayRequest, error) {
	url := fmt.Sprintf("%s/replay/requests", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch requests: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("replay endpoint not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("server error while fetching requests (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Limit response body size to 10MB
	limitedReader := io.LimitReader(resp.Body, 10<<20) // 10MB
	var requests []ReplayRequest
	if err := json.NewDecoder(limitedReader).Decode(&requests); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return requests, nil
}

// StartReplay starts replaying selected requests.
func (c *ReplayClient) StartReplay(ctx context.Context, requestIDs []string, config ReplayConfig) (string, error) {
	// Validate requestIDs
	if len(requestIDs) == 0 {
		return "", fmt.Errorf("request IDs cannot be empty")
	}
	if len(requestIDs) > 1000 {
		return "", fmt.Errorf("too many requests: maximum 1000, got %d", len(requestIDs))
	}

	requestBody := struct {
		RequestIDs []string     `json:"request_ids"`
		Config     ReplayConfig `json:"config"`
	}{
		RequestIDs: requestIDs,
		Config:     config,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)

	url := fmt.Sprintf("%s/replay/start", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to start replay: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Read response body once
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("replay endpoint not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return "", fmt.Errorf("server error while starting replay (status %d): %s", resp.StatusCode, string(respBody))
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ReplayID string `json:"replay_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.ReplayID, nil
}

// StopReplay stops an ongoing replay.
func (c *ReplayClient) StopReplay(ctx context.Context, replayID string) error {
	url := fmt.Sprintf("%s/replay/%s/stop", c.baseURL, replayID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stop replay: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Read response body once
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("replay not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return fmt.Errorf("server error while stopping replay (status %d): %s", resp.StatusCode, string(respBody))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetReplayStatus retrieves the current status of a replay.
func (c *ReplayClient) GetReplayStatus(ctx context.Context, replayID string) (*ReplaySummary, error) {
	url := fmt.Sprintf("%s/replay/%s/status", c.baseURL, replayID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get replay status: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("replay not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("server error while getting status (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var summary ReplaySummary
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&summary); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &summary, nil
}

// ExportReport exports a replay report.
func (c *ReplayClient) ExportReport(ctx context.Context, replayID, format string) ([]byte, error) {
	url := fmt.Sprintf("%s/replay/%s/export?format=%s", c.baseURL, replayID, format)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to export report: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("report not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("server error while exporting report (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	report, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50MB for reports
	if err != nil {
		return nil, fmt.Errorf("failed to read report: %w", err)
	}

	return report, nil
}
