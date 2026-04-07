// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ServerStatus manages server status information.
type ServerStatus struct {
	client        *StatusClient
	ctrl          *ControlClient
	config        *ClientConfig
	status        ServerStatusInfo
	healthHistory []HealthCheckResult
	mu            sync.RWMutex
}

// ControlClient provides HTTP client for server control operations.
type ControlClient struct {
	httpClient  *http.Client
	rateLimiter *RateLimiter
	baseURL     string
}

// NewControlClient creates a new server control client.
func NewControlClient(cfg *ClientConfig) *ControlClient {
	return &ControlClient{
		baseURL: cfg.StatusURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		rateLimiter: NewRateLimiter(5, 200*time.Millisecond),
	}
}

// Start starts the ICAP server.
func (c *ControlClient) Start(ctx context.Context) error {
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/control/start", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/control/stop", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/control/restart", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
	// Acquire rate limit token
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	url := fmt.Sprintf("%s/config", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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

// ServerStatusInfo represents server status information

// HealthCheckResult represents a health check result.
type HealthCheckResult struct {
	Timestamp     time.Time
	ICAPStatus    string
	StorageStatus string
	Error         string
	Scenarios     int
	Healthy       bool
	Ready         bool
}

// HealthResponse represents the response from the /health endpoint.
type HealthResponse struct {
	Time   time.Time `json:"time"`
	Status string    `json:"status"`
}

// ReadyResponse represents the response from the /ready endpoint.
type ReadyResponse struct {
	Checks map[string]interface{} `json:"checks"`
	Status string                 `json:"status"`
}

// NewServerStatus creates a new server status with provided configuration.
func NewServerStatus(cfg *ClientConfig) *ServerStatus {
	return &ServerStatus{
		status: ServerStatusInfo{
			State:  "unknown",
			Port:   "N/A",
			Uptime: "N/A",
		},
		client:        NewStatusClient(cfg),
		ctrl:          NewControlClient(cfg),
		config:        cfg,
		healthHistory: make([]HealthCheckResult, 0, 100),
	}
}

// Check performs a server health check.
func (s *ServerStatus) Check() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, err := s.client.GetStatus(ctx)
		if err != nil {
			status = ServerStatusInfo{
				State:  "error",
				Port:   "N/A",
				Uptime: "N/A",
				Error:  err.Error(),
			}
		}
		return ServerStatusMsg{Status: status}
	}
}

// CheckHealth performs health and readiness checks.
func (s *ServerStatus) CheckHealth() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		healthCheck := HealthCheckResult{
			Timestamp: time.Now(),
		}

		// Check /health endpoint
		healthy, err := s.checkHealthEndpoint(ctx)
		if err != nil {
			healthCheck.Healthy = false
			healthCheck.Error = err.Error()
		} else {
			healthCheck.Healthy = healthy
		}

		// Check /ready endpoint
		readyResult, err := s.checkReadyEndpoint(ctx)
		if err != nil {
			healthCheck.Ready = false
			if healthCheck.Error == "" {
				healthCheck.Error = err.Error()
			}
		} else {
			healthCheck.Ready = readyResult.Status == "ready"
			healthCheck.ICAPStatus = parseCheckStatus(readyResult.Checks["icap_server"])
			healthCheck.StorageStatus = parseCheckStatus(readyResult.Checks["storage"])
			if scenarios, ok := readyResult.Checks["scenarios_loaded"].(float64); ok {
				healthCheck.Scenarios = int(scenarios)
			}
		}

		// Update health history
		s.addHealthHistory(healthCheck)

		return HealthCheckMsg{Result: healthCheck}
	}
}

// checkHealthEndpoint checks the /health endpoint.
func (s *ServerStatus) checkHealthEndpoint(ctx context.Context) (bool, error) {
	url := fmt.Sprintf("%s/health", s.client.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to fetch health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	var healthResp HealthResponse
	if err := json.Unmarshal(body, &healthResp); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	return healthResp.Status == "healthy", nil
}

// checkReadyEndpoint checks the /ready endpoint.
func (s *ServerStatus) checkReadyEndpoint(ctx context.Context) (*ReadyResponse, error) {
	url := fmt.Sprintf("%s/ready", s.client.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch readiness: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var readyResp ReadyResponse
	if err := json.Unmarshal(body, &readyResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &readyResp, nil
}

// parseCheckStatus parses a check status from the ready response.
func parseCheckStatus(check interface{}) string {
	if check == nil {
		return "unknown"
	}
	if str, ok := check.(string); ok {
		return str
	}
	return "unknown"
}

// addHealthHistory adds a health check result to history.
func (s *ServerStatus) addHealthHistory(result HealthCheckResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthHistory = append(s.healthHistory, result)

	// Keep only last 100 entries
	if len(s.healthHistory) > 100 {
		s.healthHistory = s.healthHistory[1:]
	}
}

// GetHealthHistory returns the health check history.
func (s *ServerStatus) GetHealthHistory() []HealthCheckResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]HealthCheckResult{}, s.healthHistory...)
}

// GetLastHealthCheck returns the most recent health check.
func (s *ServerStatus) GetLastHealthCheck() *HealthCheckResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.healthHistory) == 0 {
		return nil
	}
	return &s.healthHistory[len(s.healthHistory)-1]
}

// Start starts the server.
func (s *ServerStatus) Start() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.ctrl.Start(ctx); err != nil {
			return ErrorMessage{Err: err}
		}

		// Wait a bit for server to start
		time.Sleep(1 * time.Second)

		// Check status
		status, err := s.client.GetStatus(ctx)
		if err != nil {
			return ServerControlMsg{
				Action: "start",
				Error:  err.Error(),
			}
		}

		return ServerControlMsg{
			Action:  "start",
			Success: true,
			Status:  status,
		}
	}
}

// Stop stops the server.
func (s *ServerStatus) Stop() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.ctrl.Stop(ctx); err != nil {
			return ErrorMessage{Err: err}
		}

		// Wait a bit for server to stop
		time.Sleep(1 * time.Second)

		// Check status
		status, err := s.client.GetStatus(ctx)
		if err != nil {
			return ServerControlMsg{
				Action: "stop",
				Error:  err.Error(),
			}
		}

		return ServerControlMsg{
			Action:  "stop",
			Success: true,
			Status:  status,
		}
	}
}

// Restart restarts the server.
func (s *ServerStatus) Restart() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := s.ctrl.Restart(ctx); err != nil {
			return ErrorMessage{Err: err}
		}

		// Wait a bit for server to restart
		time.Sleep(2 * time.Second)

		// Check status
		status, err := s.client.GetStatus(ctx)
		if err != nil {
			return ServerControlMsg{
				Action: "restart",
				Error:  err.Error(),
			}
		}

		return ServerControlMsg{
			Action:  "restart",
			Success: true,
			Status:  status,
		}
	}
}

// GetConfig retrieves the server configuration.
func (s *ServerStatus) GetConfig() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		config, err := s.ctrl.GetConfig(ctx)
		if err != nil {
			return ErrorMessage{Err: err}
		}

		return ServerConfigMsg{Config: config}
	}
}

// Update updates the server status.
func (s *ServerStatus) Update(status ServerStatusInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// Current returns the current server status.
func (s *ServerStatus) Current() ServerStatusInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// Shutdown releases all resources.
func (s *ServerStatus) Shutdown() {
	// No resources to clean up
	// Server is optionally stopped by the user, not automatically on shutdown
}

// ServerStatusMsg is sent when server status changes.
type ServerStatusMsg struct {
	Status ServerStatusInfo
}

// ServerControlMsg is sent when server control operation completes.
type ServerControlMsg struct {
	Status  ServerStatusInfo
	Action  string
	Error   string
	Success bool
}

// ServerConfigMsg is sent when server configuration is retrieved.
type ServerConfigMsg struct {
	Config map[string]interface{}
}

// ErrorMessage represents an error message.
type ErrorMessage struct {
	Err error
}

// HealthCheckMsg is sent when a health check is completed.
type HealthCheckMsg struct {
	Result HealthCheckResult
}
