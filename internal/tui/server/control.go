// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

// ControlClient provides HTTP client for server control operations.
type ControlClient struct {
	baseURL     string
	httpClient  *http.Client
	rateLimiter *RateLimiter
	retryConfig utils.RetryConfig
}

// NewControlClient creates a new server control client.
func NewControlClient(host string, port int) *ControlClient {
	return &ControlClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		rateLimiter: NewRateLimiter(5, 200*time.Millisecond),
		retryConfig: utils.DefaultRetryConfig(),
	}
}

// doRequest executes an HTTP request with exponential backoff retry.
func (c *ControlClient) doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	return utils.DoWithRetryHTTP(ctx, c.retryConfig, c.httpClient, req)
}

// Start starts the ICAP server.
func (c *ControlClient) Start(ctx context.Context) error {
	url := fmt.Sprintf("%s/control/start", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Stop stops the ICAP server.
func (c *ControlClient) Stop(ctx context.Context) error {
	url := fmt.Sprintf("%s/control/stop", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Restart restarts the ICAP server.
func (c *ControlClient) Restart(ctx context.Context) error {
	url := fmt.Sprintf("%s/control/restart", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to restart server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetConfig retrieves the current server configuration.
func (c *ControlClient) GetConfig(ctx context.Context) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/config", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %w", err)
	}

	return config, nil
}
