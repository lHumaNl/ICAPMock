// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

const (
	maxConfigSize  = 1 * 1024 * 1024 // 1MB max config size
	maxFilePathLen = 260             // Windows max path length
)

// ConfigClient provides HTTP client for configuration operations.
type ConfigClient struct {
	baseURL     string
	httpClient  *http.Client
	rateLimiter *RateLimiter
	retryConfig utils.RetryConfig
}

// NewConfigClient creates a new config client with connection pooling.
func NewConfigClient(host string, port int) *ConfigClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		MaxConnsPerHost:     20,
	}

	return &ConfigClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		rateLimiter: NewRateLimiter(5, 200*time.Millisecond),
		retryConfig: utils.DefaultRetryConfig(),
	}
}

// validateConfigInput validates configuration input.
func validateConfigInput(content, filePath string) error {
	// Validate content is not empty
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("configuration content cannot be empty")
	}

	// Validate content size
	if len(content) > maxConfigSize {
		return fmt.Errorf("configuration size %d exceeds maximum allowed size of %d bytes", len(content), maxConfigSize)
	}

	// Validate file path
	if filePath != "" {
		if len(filePath) > maxFilePathLen {
			return fmt.Errorf("file path length %d exceeds maximum allowed length of %d", len(filePath), maxFilePathLen)
		}
		// Check for invalid characters in file path
		if strings.ContainsAny(filePath, "<>:\"|?*") {
			return fmt.Errorf("file path contains invalid characters: %s", filePath)
		}
		// Validate file extension
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return fmt.Errorf("invalid file extension %s, expected .yaml, .yml, or .json", ext)
		}
	}

	return nil
}

// ConfigResponse represents the response from the server config endpoint.
type ConfigResponse struct {
	Message  string `json:"message"`
	Config   string `json:"config"`
	FilePath string `json:"file_path"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success"`
}

// ValidationResponse represents the response from validation endpoint.
type ValidationResponse struct {
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
	Valid   bool   `json:"valid"`
}

// SaveResponse represents the response from save endpoint.
type SaveResponse struct {
	Message  string `json:"message"`
	FilePath string `json:"file_path"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success"`
}

// doRequestWithRetry executes an HTTP request with exponential backoff retry.
func (c *ConfigClient) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.rateLimiter.Acquire(ctx); err != nil {
		return nil, fmt.Errorf("rate limit error: %w", err)
	}

	return utils.DoWithRetryHTTP(ctx, c.retryConfig, c.httpClient, req)
}

// GetConfig retrieves the current server configuration.
func (c *ConfigClient) GetConfig(ctx context.Context) (string, string, error) {
	url := fmt.Sprintf("%s/api/config", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to create config request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch config from server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Validate status code
	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("config endpoint not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return "", "", fmt.Errorf("server error while fetching config (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status code when fetching config: %d", resp.StatusCode)
	}

	// Limit body size
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigSize))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", "", fmt.Errorf("unexpected EOF while reading config body")
		}
		return "", "", fmt.Errorf("failed to read config response body: %w", err)
	}

	var response ConfigResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", fmt.Errorf("failed to parse config response: %w", err)
	}

	if !response.Success {
		return "", "", fmt.Errorf("server error: %s", response.Message)
	}

	return response.Config, response.FilePath, nil
}

// SaveConfig saves the configuration to the server.
func (c *ConfigClient) SaveConfig(ctx context.Context, content, filePath string) (string, error) {
	if err := validateConfigInput(content, filePath); err != nil {
		return "", fmt.Errorf("config validation failed: %w", err)
	}

	url := fmt.Sprintf("%s/api/config", c.baseURL)

	requestBody := map[string]string{
		"config":    content,
		"file_path": filePath,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config request: %w", err)
	}

	bodyCopy := make([]byte, len(bodyBytes))
	copy(bodyCopy, bodyBytes)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytesReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create save config request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytesReader(bodyCopy)), nil
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to save config to server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigSize))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("unexpected EOF while reading save response")
		}
		return "", fmt.Errorf("failed to read save response body: %w", err)
	}

	if resp.StatusCode == http.StatusBadRequest {
		return "", fmt.Errorf("bad request: %s", string(respBody))
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return "", fmt.Errorf("server error while saving config (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code when saving config: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var response SaveResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return "", fmt.Errorf("failed to parse save response: %w", err)
	}

	if !response.Success {
		return "", fmt.Errorf("server error: %s", response.Message)
	}

	return response.FilePath, nil
}

// ValidateConfig validates the configuration without saving.
func (c *ConfigClient) ValidateConfig(ctx context.Context, content string) (bool, string, error) {
	if err := validateConfigInput(content, ""); err != nil {
		return false, err.Error(), fmt.Errorf("config validation failed: %w", err)
	}

	url := fmt.Sprintf("%s/api/config/validate", c.baseURL)

	requestBody := map[string]string{
		"config": content,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return false, "", fmt.Errorf("failed to marshal validate config request: %w", err)
	}

	bodyCopy := make([]byte, len(bodyBytes))
	copy(bodyCopy, bodyBytes)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytesReader(bodyBytes))
	if err != nil {
		return false, "", fmt.Errorf("failed to create validate config request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytesReader(bodyCopy)), nil
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return false, "", fmt.Errorf("failed to validate config on server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigSize))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, "unexpected EOF while reading validation response", nil
		}
		return false, "", fmt.Errorf("failed to read validation response body: %w", err)
	}

	if resp.StatusCode == http.StatusBadRequest {
		return false, fmt.Sprintf("bad request: %s", string(respBody)), nil
	}
	if resp.StatusCode == http.StatusInternalServerError {
		return false, "server error while validating config", nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody)), nil
	}

	var response ValidationResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return false, "", fmt.Errorf("failed to parse validation response: %w", err)
	}

	if !response.Success {
		return false, response.Message, nil
	}

	return response.Valid, response.Message, nil
}

// LoadConfigFile loads configuration from a local file (client-side).
func (c *ConfigClient) LoadConfigFile(ctx context.Context, filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}

	if len(filePath) > maxFilePathLen {
		return "", fmt.Errorf("file path length %d exceeds maximum allowed length of %d", len(filePath), maxFilePathLen)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return "", fmt.Errorf("invalid file extension %s, expected .yaml, .yml, or .json", ext)
	}

	content, err := os.ReadFile(filePath) //nolint:gosec // path is validated
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", filePath)
		}
		if os.IsPermission(err) {
			return "", fmt.Errorf("permission denied: cannot read file %s", filePath)
		}
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if len(content) > maxConfigSize {
		return "", fmt.Errorf("configuration file size %d exceeds maximum allowed size of %d bytes", len(content), maxConfigSize)
	}

	if strings.TrimSpace(string(content)) == "" {
		return "", fmt.Errorf("configuration file is empty: %s", filePath)
	}

	return string(content), nil
}

// ValidateConfigYAML validates YAML configuration locally (client-side).
func (c *ConfigClient) ValidateConfigYAML(content string) (bool, string, error) {
	// Validate input
	if strings.TrimSpace(content) == "" {
		return false, "YAML content cannot be empty", nil
	}

	if len(content) > maxConfigSize {
		return false, fmt.Sprintf("YAML size %d exceeds maximum allowed size of %d bytes", len(content), maxConfigSize), nil
	}

	var cfg config.Config
	err := yaml.Unmarshal([]byte(content), &cfg)
	if err != nil {
		return false, fmt.Sprintf("YAML parsing error: %v", err), nil
	}

	// Additional validation: check for required fields
	if cfg.Server.Host == "" {
		return false, "required field 'server.host' is missing in configuration", nil
	}

	// Additional validation can be added here
	return true, "YAML configuration is valid", nil
}

// ValidateConfigJSON validates JSON configuration locally (client-side).
func (c *ConfigClient) ValidateConfigJSON(content string) (bool, string, error) {
	// Validate input
	if strings.TrimSpace(content) == "" {
		return false, "JSON content cannot be empty", nil
	}

	if len(content) > maxConfigSize {
		return false, fmt.Sprintf("JSON size %d exceeds maximum allowed size of %d bytes", len(content), maxConfigSize), nil
	}

	var cfg config.Config
	err := json.Unmarshal([]byte(content), &cfg)
	if err != nil {
		return false, fmt.Sprintf("JSON parsing error: %v", err), nil
	}

	// Additional validation: check for required fields
	if cfg.Server.Host == "" {
		return false, "required field 'server.host' is missing in configuration", nil
	}

	// Additional validation can be added here
	return true, "JSON configuration is valid", nil
}

// bytesReader creates an io.Reader from byte slice.
func bytesReader(b []byte) io.Reader {
	return &byteReader{data: b}
}

// byteReader implements io.Reader for byte slice.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
