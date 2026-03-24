package state

import (
	"fmt"
	"time"
)

// ClientConfig holds configuration for TUI clients
type ClientConfig struct {
	ServerHost            string // ICAP server host (default: localhost)
	ServerPort            int    // ICAP server port (default: 1344)
	MetricsURL            string
	LogsURL               string
	StatusURL             string
	Timeout               int           // timeout in seconds
	MaxConcurrentRequests int           // maximum concurrent requests
	RequestInterval       time.Duration // minimum interval between requests
	RetryMax              int           // maximum retry attempts
	MaxHistory            int           // maximum metrics history entries (default: 100)
	MaxLogs               int           // maximum log entries (default: 100)
}

// DefaultClientConfig returns default client configuration
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ServerHost:            "localhost",
		ServerPort:            1344,
		MetricsURL:            "http://localhost:9090",
		LogsURL:               "http://localhost:8080",
		StatusURL:             "http://localhost:8080",
		Timeout:               5,
		MaxConcurrentRequests: 10,
		RequestInterval:       100 * time.Millisecond,
		RetryMax:              3,
		MaxHistory:            100,
		MaxLogs:               100,
	}
}

// Validate validates the client configuration
func (c *ClientConfig) Validate() error {
	if c.MetricsURL == "" {
		return fmt.Errorf("metrics URL cannot be empty")
	}
	if c.LogsURL == "" {
		return fmt.Errorf("logs URL cannot be empty")
	}
	if c.StatusURL == "" {
		return fmt.Errorf("status URL cannot be empty")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %d", c.Timeout)
	}
	if c.MaxConcurrentRequests <= 0 {
		return fmt.Errorf("max concurrent requests must be positive, got %d", c.MaxConcurrentRequests)
	}
	if c.RequestInterval < 0 {
		return fmt.Errorf("request interval cannot be negative, got %v", c.RequestInterval)
	}
	if c.RetryMax < 0 {
		return fmt.Errorf("retry max cannot be negative, got %d", c.RetryMax)
	}
	return nil
}
