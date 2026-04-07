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

// APIScenario represents a scenario for API operations.
type APIScenario struct {
	Config   map[string]interface{} `json:"config"`
	Name     string                 `json:"name"`
	Method   string                 `json:"method,omitempty"`
	Path     string                 `json:"path,omitempty"`
	Priority int                    `json:"priority"`
}

// ScenarioListResponse represents the response for listing scenarios.
type ScenarioListResponse struct {
	Scenarios []APIScenario `json:"scenarios"`
}

// ScenarioClient provides HTTP client for scenario operations.
type ScenarioClient struct {
	baseURL     string
	httpClient  *http.Client
	retryConfig utils.RetryConfig
}

// NewScenarioClient creates a new scenario client.
func NewScenarioClient(host string, port int) *ScenarioClient {
	return &ScenarioClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retryConfig: utils.DefaultRetryConfig(),
	}
}

// doRequestWithRetry executes an HTTP request with retry.
func (c *ScenarioClient) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	return utils.DoWithRetryHTTP(ctx, c.retryConfig, c.httpClient, req)
}

// ListScenarios fetches all scenarios from the server.
func (c *ScenarioClient) ListScenarios(ctx context.Context) ([]APIScenario, error) {
	url := fmt.Sprintf("%s/scenarios", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch scenarios: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return []APIScenario{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response ScenarioListResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Scenarios, nil
}

// CreateScenario creates a new scenario.
func (c *ScenarioClient) CreateScenario(ctx context.Context, scenario APIScenario) error {
	body, err := json.Marshal(scenario)
	if err != nil {
		return fmt.Errorf("failed to marshal scenario: %w", err)
	}

	url := fmt.Sprintf("%s/scenarios", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create scenario: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	return fmt.Errorf("failed to create scenario (status %d): %s", resp.StatusCode, string(respBody))
}

// UpdateScenario updates an existing scenario.
func (c *ScenarioClient) UpdateScenario(ctx context.Context, oldName string, scenario APIScenario) error {
	body, err := json.Marshal(scenario)
	if err != nil {
		return fmt.Errorf("failed to marshal scenario: %w", err)
	}

	url := fmt.Sprintf("%s/scenarios/%s", c.baseURL, oldName)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update scenario: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("scenario not found: %s", oldName)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("failed to update scenario (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteScenario deletes a scenario.
func (c *ScenarioClient) DeleteScenario(ctx context.Context, name string) error {
	url := fmt.Sprintf("%s/scenarios/%s", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete scenario: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("scenario not found: %s", name)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("failed to delete scenario (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ReloadScenarios reloads scenarios from disk.
func (c *ScenarioClient) ReloadScenarios(ctx context.Context) error {
	url := fmt.Sprintf("%s/scenarios/reload", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to reload scenarios: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("failed to reload scenarios (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
