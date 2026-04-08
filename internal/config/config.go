// Copyright 2026 ICAP Mock

// Package config handles loading and validation of server configuration.
package config

import (
	"encoding/json"
	"fmt"
	"time"
)

const defaultHost = "0.0.0.0"

// CircuitBreakerGlobalConfig contains global circuit breaker configuration.
// Circuit breakers provide automatic failure isolation for external dependencies.
type CircuitBreakerGlobalConfig struct {
	Components map[string]CircuitBreakerComponentConfig `yaml:"components" json:"components"`
	Defaults   CircuitBreakerComponentConfig            `yaml:"defaults" json:"defaults"`
	Enabled    bool                                     `yaml:"enabled" json:"enabled"`
}

// CircuitBreakerComponentConfig contains circuit breaker configuration for a single component.
type CircuitBreakerComponentConfig struct {
	// FailureThreshold is the number of failures to open the circuit.
	// Default: 5
	FailureThreshold int `yaml:"failure_threshold" json:"failure_threshold"`

	// SuccessThreshold is the number of successes to close from HALF_OPEN.
	// Default: 3
	SuccessThreshold int `yaml:"success_threshold" json:"success_threshold"`

	// OpenTimeout is the duration to wait before trying HALF_OPEN.
	// Default: 30s
	OpenTimeout time.Duration `yaml:"open_timeout" json:"open_timeout"`

	// HalfOpenMaxRequests limits requests in HALF_OPEN state.
	// Default: 1
	HalfOpenMaxRequests int `yaml:"half_open_max_requests" json:"half_open_max_requests"`

	// RollingWindow is the time window for failure counting.
	// Default: 60s
	RollingWindow time.Duration `yaml:"rolling_window" json:"rolling_window"`

	// WindowBuckets is the number of buckets in the rolling window.
	// Default: 60
	WindowBuckets int `yaml:"window_buckets" json:"window_buckets"`
}

// UnmarshalJSON implements custom JSON unmarshaling for CircuitBreakerComponentConfig.
// It handles time.Duration fields which can be strings like "30s".
func (c *CircuitBreakerComponentConfig) UnmarshalJSON(data []byte) error {
	type Alias CircuitBreakerComponentConfig

	temp := struct {
		*Alias
		OpenTimeout   string `json:"open_timeout"`
		RollingWindow string `json:"rolling_window"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Parse duration strings
	if temp.OpenTimeout != "" {
		d, err := time.ParseDuration(temp.OpenTimeout)
		if err != nil {
			return fmt.Errorf("invalid open_timeout: %w", err)
		}
		c.OpenTimeout = d
	}
	if temp.RollingWindow != "" {
		d, err := time.ParseDuration(temp.RollingWindow)
		if err != nil {
			return fmt.Errorf("invalid rolling_window: %w", err)
		}
		c.RollingWindow = d
	}

	return nil
}

// DefaultsConfig contains shared default settings inherited by all servers.
// Individual servers can override any of these fields.
type DefaultsConfig struct {
	Host            string        `yaml:"host,omitempty" json:"host,omitempty"`
	ReadTimeout     time.Duration `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty"`
	WriteTimeout    time.Duration `yaml:"write_timeout,omitempty" json:"write_timeout,omitempty"`
	MaxConnections  int           `yaml:"max_connections,omitempty" json:"max_connections,omitempty"`
	MaxBodySize     int64         `yaml:"max_body_size,omitempty" json:"max_body_size,omitempty"`
	IdleTimeout     time.Duration `yaml:"idle_timeout,omitempty" json:"idle_timeout,omitempty"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
}

// InlineWeightedResponse mirrors storage.WeightedResponseV2 for inline scenario definitions.
// Defined here to avoid a circular import between config and storage packages.
type InlineWeightedResponse struct {
	Set        map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Body       string            `yaml:"body,omitempty" json:"body,omitempty"`
	Delay      string            `yaml:"delay,omitempty" json:"delay,omitempty"`
	Weight     int               `yaml:"weight,omitempty" json:"weight,omitempty"`
	Status     int               `yaml:"status,omitempty" json:"status,omitempty"`
	HTTPStatus int               `yaml:"http_status,omitempty" json:"http_status,omitempty"`
}

// InlineScenarioEntry mirrors storage.ScenarioEntryV2 for inline scenario definitions.
// Defined here to avoid a circular import between config and storage packages.
type InlineScenarioEntry struct {
	When       map[string]string        `yaml:"when,omitempty" json:"when,omitempty"`
	Set        map[string]string        `yaml:"set,omitempty" json:"set,omitempty"`
	Method     string                   `yaml:"method,omitempty" json:"method,omitempty"`
	Endpoint   string                   `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Body       string                   `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile   string                   `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	Delay      string                   `yaml:"delay,omitempty" json:"delay,omitempty"`
	Responses  []InlineWeightedResponse `yaml:"responses,omitempty" json:"responses,omitempty"`
	Status     int                      `yaml:"status,omitempty" json:"status,omitempty"`
	HTTPStatus int                      `yaml:"http_status,omitempty" json:"http_status,omitempty"`
	Priority   int                      `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// ServerEntryConfig defines an ICAP server instance with its own port and scenarios.
// Fields that are zero/empty fall back to DefaultsConfig values.
type ServerEntryConfig struct {
	Scenarios       map[string]InlineScenarioEntry `yaml:"scenarios,omitempty" json:"scenarios,omitempty"`
	ScenariosDir    string                         `yaml:"scenarios_dir" json:"scenarios_dir"`
	ServiceID       string                         `yaml:"service_id,omitempty" json:"service_id,omitempty"`
	Host            string                         `yaml:"host,omitempty" json:"host,omitempty"`
	Port            int                            `yaml:"port" json:"port"`
	ReadTimeout     time.Duration                  `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty"`
	WriteTimeout    time.Duration                  `yaml:"write_timeout,omitempty" json:"write_timeout,omitempty"`
	MaxConnections  int                            `yaml:"max_connections,omitempty" json:"max_connections,omitempty"`
	MaxBodySize     int64                          `yaml:"max_body_size,omitempty" json:"max_body_size,omitempty"`
	IdleTimeout     time.Duration                  `yaml:"idle_timeout,omitempty" json:"idle_timeout,omitempty"`
	ShutdownTimeout time.Duration                  `yaml:"shutdown_timeout,omitempty" json:"shutdown_timeout,omitempty"`
}

// ToServerConfig merges this entry with defaults to produce a ServerConfig
// that can be passed to server.NewServer.
func (e *ServerEntryConfig) ToServerConfig(defaults DefaultsConfig) ServerConfig {
	cfg := ServerConfig{
		Host:            defaults.Host,
		Port:            e.Port,
		ReadTimeout:     defaults.ReadTimeout,
		WriteTimeout:    defaults.WriteTimeout,
		MaxConnections:  defaults.MaxConnections,
		MaxBodySize:     defaults.MaxBodySize,
		IdleTimeout:     defaults.IdleTimeout,
		ShutdownTimeout: defaults.ShutdownTimeout,
		Streaming:       true, // default
	}
	// Apply per-server overrides
	if e.Host != "" {
		cfg.Host = e.Host
	}
	if e.ReadTimeout != 0 {
		cfg.ReadTimeout = e.ReadTimeout
	}
	if e.WriteTimeout != 0 {
		cfg.WriteTimeout = e.WriteTimeout
	}
	if e.MaxConnections != 0 {
		cfg.MaxConnections = e.MaxConnections
	}
	if e.MaxBodySize != 0 {
		cfg.MaxBodySize = e.MaxBodySize
	}
	if e.IdleTimeout != 0 {
		cfg.IdleTimeout = e.IdleTimeout
	}
	if e.ShutdownTimeout != 0 {
		cfg.ShutdownTimeout = e.ShutdownTimeout
	}
	return cfg
}

// Config is the root configuration structure for the ICAP Mock Server.
// It contains all sub-configurations for different components.
type Config struct {
	Servers            map[string]ServerEntryConfig `yaml:"servers,omitempty" json:"servers,omitempty"`
	Health             HealthConfig                 `yaml:"health" json:"health"`
	Plugin             PluginConfig                 `yaml:"plugin" json:"plugin"`
	Metrics            MetricsConfig                `yaml:"metrics" json:"metrics"`
	Replay             ReplayConfig                 `yaml:"replay" json:"replay"`
	Mock               MockConfig                   `yaml:"mock" json:"mock"`
	Logging            LoggingConfig                `yaml:"logging" json:"logging"`
	RateLimit          RateLimitConfig              `yaml:"rate_limit" json:"rate_limit"`
	CircuitBreaker     CircuitBreakerGlobalConfig   `yaml:"circuit_breaker" json:"circuit_breaker"`
	Defaults           DefaultsConfig               `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Server             ServerConfig                 `yaml:"server" json:"server"`
	Storage            StorageConfig                `yaml:"storage" json:"storage"`
	Chaos              ChaosConfig                  `yaml:"chaos" json:"chaos"`
	PerClientRateLimit PerClientRateLimitConfig     `yaml:"per_client_rate_limit" json:"per_client_rate_limit"`
	Preview            PreviewConfig                `yaml:"preview" json:"preview"`
	Sharding           ShardingConfig               `yaml:"sharding" json:"sharding"`
	PerMethodRateLimit PerMethodRateLimitConfig     `yaml:"per_method_rate_limit" json:"per_method_rate_limit"`
	Pprof              PprofConfig                  `yaml:"pprof" json:"pprof"`
}

// SetDefaults sets default values for all configuration fields.
// This should be called before loading configuration from files or environment.
func (c *Config) SetDefaults() {
	// Server defaults
	c.Server.Host = defaultHost //nolint:goconst
	c.Server.Port = 1344
	c.Server.ReadTimeout = 30 * time.Second
	c.Server.WriteTimeout = 30 * time.Second
	// MaxConnections: 15000 - high concurrency for production workloads
	// Previously 1000 was too low for high-traffic scenarios
	c.Server.MaxConnections = 15000
	// MaxBodySize: 10MB - protects against memory exhaustion attacks
	// 0 (unlimited) is dangerous in production as malicious clients
	// could send extremely large payloads causing OOM
	c.Server.MaxBodySize = 10485760 // 10MB
	c.Server.Streaming = true
	c.Server.IdleTimeout = 60 * time.Second     // 60 seconds default
	c.Server.ShutdownTimeout = 30 * time.Second // 30 seconds default
	c.Server.TLS.Enabled = false
	c.Server.TLS.CertCheckInterval = 24 * time.Hour // 24 hours default
	c.Server.TLS.ExpiryWarningDays = 30             // 30 days default

	// Logging defaults
	c.Logging.Level = "info"
	c.Logging.Format = "json"
	c.Logging.Output = "stdout"
	c.Logging.MaxSize = 100
	c.Logging.MaxBackups = 5
	c.Logging.MaxAge = 30

	// Metrics defaults
	c.Metrics.Enabled = true
	c.Metrics.Host = defaultHost
	c.Metrics.Port = 9090
	c.Metrics.Path = "/metrics"

	// Mock defaults
	c.Mock.DefaultMode = "mock"
	c.Mock.DefaultTimeout = 5 * time.Second
	c.Mock.ServiceID = "icap-mock"

	// Hot reload defaults (disabled by default)
	c.Mock.HotReload.Enabled = false
	c.Mock.HotReload.Debounce = time.Second
	c.Mock.HotReload.WatchDirectory = true

	// Chaos defaults (disabled by default)
	c.Chaos.Enabled = false
	c.Chaos.ErrorRate = 0.1
	c.Chaos.TimeoutRate = 0.05
	c.Chaos.MinLatencyMs = 10
	c.Chaos.MaxLatencyMs = 500
	c.Chaos.LatencyRate = 0.1
	c.Chaos.ConnectionDropRate = 0.02

	// Storage defaults (enabled by default)
	c.Storage.Enabled = true
	c.Storage.RequestsDir = "./data/requests"
	c.Storage.MaxFileSize = 104857600 // 100MB
	c.Storage.RotateAfter = 10000
	c.Storage.Workers = 16
	c.Storage.QueueSize = 10000

	// Disk Monitor defaults (enabled by default for production safety)
	c.Storage.DiskMonitor.Enabled = true
	c.Storage.DiskMonitor.CheckInterval = 30 * time.Second
	c.Storage.DiskMonitor.WarningThreshold = 0.80         // 80%
	c.Storage.DiskMonitor.ErrorThreshold = 0.95           // 95%
	c.Storage.DiskMonitor.Path = ""                       // Empty means use requests_dir
	c.Storage.DiskMonitor.UseSyscalls = true              // Use platform-specific syscalls (fast)
	c.Storage.DiskMonitor.CacheInterval = 5 * time.Second // Cache results for 5 seconds

	// Circuit Breaker defaults (enabled by default for resilience)
	c.Storage.CircuitBreaker.Enabled = true
	c.Storage.CircuitBreaker.MaxFailures = 5
	c.Storage.CircuitBreaker.ResetTimeout = 30 * time.Second
	c.Storage.CircuitBreaker.SuccessThreshold = 3

	// RateLimit defaults (enabled by default for production safety)
	c.RateLimit.Enabled = true
	c.RateLimit.RequestsPerSecond = 10000
	c.RateLimit.Burst = 15000
	c.RateLimit.Algorithm = "sharded_token_bucket"

	// PerClientRateLimit defaults (disabled by default to avoid breaking changes)
	c.PerClientRateLimit.Enabled = false
	c.PerClientRateLimit.RequestsPerSecond = 100
	c.PerClientRateLimit.Burst = 200
	c.PerClientRateLimit.MaxClients = 10000
	c.PerClientRateLimit.TTL = 5 * time.Minute

	// PerMethodRateLimit defaults (disabled by default)
	c.PerMethodRateLimit.Enabled = false
	c.PerMethodRateLimit.RequestsPerSecond = 5000
	c.PerMethodRateLimit.Burst = 7500

	// Health defaults
	c.Health.Enabled = true
	c.Health.Port = 8080
	c.Health.HealthPath = "/health"
	c.Health.ReadyPath = "/ready"

	// Replay defaults (disabled by default)
	c.Replay.Enabled = false
	c.Replay.Speed = 1.0

	// Pprof defaults (disabled by default for security)
	// Production profiling should be explicitly enabled
	c.Pprof.Enabled = false

	// Plugin defaults (disabled by default)
	c.Plugin.Enabled = false
	c.Plugin.Dir = "./plugins"
	c.Plugin.Names = nil

	// Sharding defaults (enabled by default for performance)
	c.Sharding.Enabled = true
	c.Sharding.ShardCount = 16
	c.Sharding.CacheSize = 1000
	c.Sharding.EnableCache = true

	// Preview rate limiting defaults (enabled by default for security)
	c.Preview.Enabled = true
	c.Preview.MaxRequests = 100
	c.Preview.WindowSeconds = 60
	c.Preview.MaxClients = 10000

	// Circuit Breaker defaults (disabled by default for backward compatibility)
	c.CircuitBreaker.Enabled = false
	c.CircuitBreaker.Defaults = CircuitBreakerComponentConfig{
		FailureThreshold:    5,
		SuccessThreshold:    3,
		OpenTimeout:         30 * time.Second,
		HalfOpenMaxRequests: 1,
		RollingWindow:       60 * time.Second,
		WindowBuckets:       60,
	}

	// Per-component circuit breaker defaults
	c.CircuitBreaker.Components = map[string]CircuitBreakerComponentConfig{
		"storage": {
			FailureThreshold:    10,
			SuccessThreshold:    3,
			OpenTimeout:         60 * time.Second,
			HalfOpenMaxRequests: 2,
		},
		"scenario_loader": {
			FailureThreshold:    5,
			SuccessThreshold:    3,
			OpenTimeout:         30 * time.Second,
			HalfOpenMaxRequests: 1,
		},
	}
}

// ServerConfig contains ICAP server configuration.
type ServerConfig struct {
	Host            string        `yaml:"host" json:"host"`
	TLS             TLSConfig     `yaml:"tls" json:"tls"`
	Port            int           `yaml:"port" json:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	MaxConnections  int           `yaml:"max_connections" json:"max_connections"`
	MaxBodySize     int64         `yaml:"max_body_size" json:"max_body_size"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
	Streaming       bool          `yaml:"streaming" json:"streaming"`
}

// UnmarshalJSON implements custom JSON unmarshaling for ServerConfig.
// It handles time.Duration fields which can be strings like "45s".
func (c *ServerConfig) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias ServerConfig

	// Create a temporary struct with duration fields as strings
	temp := struct {
		*Alias
		ReadTimeout     string          `json:"read_timeout"`
		WriteTimeout    string          `json:"write_timeout"`
		IdleTimeout     string          `json:"idle_timeout"`
		ShutdownTimeout string          `json:"shutdown_timeout"`
		MaxBodySize     json.RawMessage `json:"max_body_size"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Parse duration strings
	if temp.ReadTimeout != "" {
		d, err := time.ParseDuration(temp.ReadTimeout)
		if err != nil {
			return fmt.Errorf("invalid read_timeout: %w", err)
		}
		c.ReadTimeout = d
	}
	if temp.WriteTimeout != "" {
		d, err := time.ParseDuration(temp.WriteTimeout)
		if err != nil {
			return fmt.Errorf("invalid write_timeout: %w", err)
		}
		c.WriteTimeout = d
	}
	if temp.IdleTimeout != "" {
		d, err := time.ParseDuration(temp.IdleTimeout)
		if err != nil {
			return fmt.Errorf("invalid idle_timeout: %w", err)
		}
		c.IdleTimeout = d
	}
	if temp.ShutdownTimeout != "" {
		d, err := time.ParseDuration(temp.ShutdownTimeout)
		if err != nil {
			return fmt.Errorf("invalid shutdown_timeout: %w", err)
		}
		c.ShutdownTimeout = d
	}

	// Parse max_body_size (supports human-readable strings like "10MB")
	if len(temp.MaxBodySize) > 0 {
		// Try as number first
		var num int64
		if err := json.Unmarshal(temp.MaxBodySize, &num); err == nil {
			c.MaxBodySize = num
		} else {
			// Try as string
			var s string
			if err := json.Unmarshal(temp.MaxBodySize, &s); err == nil {
				if size, parseErr := ParseByteSize(s); parseErr == nil {
					c.MaxBodySize = size
				} else {
					return fmt.Errorf("invalid max_body_size: %w", parseErr)
				}
			}
		}
	}

	return nil
}

// TLSConfig contains TLS configuration for the ICAP server.
type TLSConfig struct {
	CertFile          string        `yaml:"cert_file" json:"cert_file"`
	KeyFile           string        `yaml:"key_file" json:"key_file"`
	ClientCAFile      string        `yaml:"client_ca_file" json:"client_ca_file"`
	ClientAuth        string        `yaml:"client_auth" json:"client_auth"`
	CertCheckInterval time.Duration `yaml:"cert_check_interval" json:"cert_check_interval"`
	ExpiryWarningDays int           `yaml:"expiry_warning_days" json:"expiry_warning_days"`
	Enabled           bool          `yaml:"enabled" json:"enabled"`
}

// UnmarshalJSON implements custom JSON unmarshaling for TLSConfig.
// It handles time.Duration fields which can be strings like "24h".
func (c *TLSConfig) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias TLSConfig

	// Create a temporary struct with duration fields as strings
	temp := struct {
		*Alias
		CertCheckInterval string `json:"cert_check_interval"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Parse duration string
	if temp.CertCheckInterval != "" {
		d, err := time.ParseDuration(temp.CertCheckInterval)
		if err != nil {
			return fmt.Errorf("invalid cert_check_interval: %w", err)
		}
		c.CertCheckInterval = d
	}

	return nil
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	// Level is the logging level.
	// Valid values: "debug", "info", "warn", "error"
	// Default: "info"
	Level string `yaml:"level" json:"level"`

	// Format is the log output format.
	// Valid values: "json", "text"
	// Default: "json"
	Format string `yaml:"format" json:"format"`

	// Output is the log output destination.
	// Valid values: "stdout", "stderr", or a file path
	// Default: "stdout"
	Output string `yaml:"output" json:"output"`

	// MaxSize is the maximum size in megabytes of the log file
	// before it gets rotated.
	// Default: 100
	MaxSize int `yaml:"max_size" json:"max_size"`

	// MaxBackups is the maximum number of old log files to retain.
	// Default: 5
	MaxBackups int `yaml:"max_backups" json:"max_backups"`

	// MaxAge is the maximum number of days to retain old log files.
	// Default: 30
	MaxAge int `yaml:"max_age" json:"max_age"`
}

// MetricsConfig contains Prometheus metrics configuration.
type MetricsConfig struct {
	Host    string `yaml:"host" json:"host"`
	Path    string `yaml:"path" json:"path"`
	Port    int    `yaml:"port" json:"port"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// MockConfig contains mock processor configuration.
type MockConfig struct {
	DefaultMode    string          `yaml:"default_mode" json:"default_mode"`
	ScenariosDir   string          `yaml:"scenarios_dir" json:"scenarios_dir"`
	ServiceID      string          `yaml:"service_id" json:"service_id"`
	HotReload      HotReloadConfig `yaml:"hot_reload" json:"hot_reload"`
	DefaultTimeout time.Duration   `yaml:"default_timeout" json:"default_timeout"`
}

// HotReloadConfig contains configuration for scenario hot-reloading.
type HotReloadConfig struct {
	Debounce       time.Duration `yaml:"debounce" json:"debounce"`
	Enabled        bool          `yaml:"enabled" json:"enabled"`
	WatchDirectory bool          `yaml:"watch_directory" json:"watch_directory"`
}

// UnmarshalJSON implements custom JSON unmarshaling for HotReloadConfig.
// It handles time.Duration fields which can be strings like "1s".
func (c *HotReloadConfig) UnmarshalJSON(data []byte) error {
	type Alias HotReloadConfig

	temp := struct {
		*Alias
		Debounce string `json:"debounce"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.Debounce != "" {
		d, err := time.ParseDuration(temp.Debounce)
		if err != nil {
			return fmt.Errorf("invalid debounce: %w", err)
		}
		c.Debounce = d
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for MockConfig.
// It handles time.Duration fields which can be strings like "3s".
func (c *MockConfig) UnmarshalJSON(data []byte) error {
	type Alias MockConfig

	temp := struct {
		*Alias
		DefaultTimeout string `json:"default_timeout"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.DefaultTimeout != "" {
		d, err := time.ParseDuration(temp.DefaultTimeout)
		if err != nil {
			return fmt.Errorf("invalid default_timeout: %w", err)
		}
		c.DefaultTimeout = d
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for PerClientRateLimitConfig.
// It handles time.Duration fields which can be strings like "5m".
func (c *PerClientRateLimitConfig) UnmarshalJSON(data []byte) error {
	type Alias PerClientRateLimitConfig

	temp := struct {
		*Alias
		TTL string `json:"ttl"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.TTL != "" {
		d, err := time.ParseDuration(temp.TTL)
		if err != nil {
			return fmt.Errorf("invalid ttl: %w", err)
		}
		c.TTL = d
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for CircuitBreakerConfig.
// It handles time.Duration fields which can be strings like "30s".
func (c *CircuitBreakerConfig) UnmarshalJSON(data []byte) error {
	type Alias CircuitBreakerConfig

	temp := struct {
		*Alias
		ResetTimeout string `json:"reset_timeout"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.ResetTimeout != "" {
		d, err := time.ParseDuration(temp.ResetTimeout)
		if err != nil {
			return fmt.Errorf("invalid reset_timeout: %w", err)
		}
		c.ResetTimeout = d
	}

	return nil
}

// ChaosConfig contains chaos engineering configuration.
// Chaos features are disabled by default.
type ChaosConfig struct {
	// Enabled enables chaos engineering features.
	// Default: false
	Enabled bool `yaml:"enabled" json:"enabled"`

	// ErrorRate is the probability of injecting an error (0.0 to 1.0).
	// Default: 0.1 (10%)
	ErrorRate float64 `yaml:"error_rate" json:"error_rate"`

	// TimeoutRate is the probability of injecting a timeout (0.0 to 1.0).
	// Default: 0.05 (5%)
	TimeoutRate float64 `yaml:"timeout_rate" json:"timeout_rate"`

	// MinLatencyMs is the minimum latency to inject in milliseconds.
	// Default: 10
	MinLatencyMs int `yaml:"min_latency_ms" json:"min_latency_ms"`

	// MaxLatencyMs is the maximum latency to inject in milliseconds.
	// Default: 500
	MaxLatencyMs int `yaml:"max_latency_ms" json:"max_latency_ms"`

	// LatencyRate is the probability of injecting latency (0.0 to 1.0).
	// Default: 0.1 (10%)
	LatencyRate float64 `yaml:"latency_rate" json:"latency_rate"`

	// ConnectionDropRate is the probability of dropping connections (0.0 to 1.0).
	// Default: 0.02 (2%)
	ConnectionDropRate float64 `yaml:"connection_drop_rate" json:"connection_drop_rate"`
}

// StorageConfig contains request storage configuration.
type StorageConfig struct {
	RequestsDir    string               `yaml:"requests_dir" json:"requests_dir"`
	DiskMonitor    DiskMonitorConfig    `yaml:"disk_monitor" json:"disk_monitor"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
	MaxFileSize    int64                `yaml:"max_file_size" json:"max_file_size"`
	RotateAfter    int                  `yaml:"rotate_after" json:"rotate_after"`
	Workers        int                  `yaml:"workers" json:"workers"`
	QueueSize      int                  `yaml:"queue_size" json:"queue_size"`
	Enabled        bool                 `yaml:"enabled" json:"enabled"`
}

// CircuitBreakerConfig contains circuit breaker configuration for storage operations.
// The circuit breaker has three states: Closed (normal), Open (failing fast),
// and Half-Open (testing recovery).
type CircuitBreakerConfig struct {
	// Enabled enables the circuit breaker for storage operations.
	// When disabled, storage failures are logged but don't affect request flow.
	// Default: true
	Enabled bool `yaml:"enabled" json:"enabled"`

	// MaxFailures is the number of consecutive failures before opening the circuit.
	// Once this threshold is reached, the circuit opens and storage is skipped.
	// Default: 5
	MaxFailures int `yaml:"max_failures" json:"max_failures"`

	// ResetTimeout is the duration to wait before transitioning from Open to Half-Open.
	// In Half-Open state, a single request is allowed through to test recovery.
	// Default: 30s
	ResetTimeout time.Duration `yaml:"reset_timeout" json:"reset_timeout"`

	// SuccessThreshold is the number of consecutive successes in Half-Open state
	// required to close the circuit and resume normal operation.
	// Default: 3
	SuccessThreshold int `yaml:"success_threshold" json:"success_threshold"`
}

// DiskMonitorConfig contains disk space monitoring configuration for storage operations.
// The disk monitor prevents crashes when disk is full by checking available space
// before writes and rejecting requests at error threshold.
type DiskMonitorConfig struct {
	Path             string        `yaml:"path" json:"path"`
	CheckInterval    time.Duration `yaml:"check_interval" json:"check_interval"`
	WarningThreshold float64       `yaml:"warning_threshold" json:"warning_threshold"`
	ErrorThreshold   float64       `yaml:"error_threshold" json:"error_threshold"`
	CacheInterval    time.Duration `yaml:"cache_interval" json:"cache_interval"`
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	UseSyscalls      bool          `yaml:"use_syscalls" json:"use_syscalls"`
}

// UnmarshalJSON implements custom JSON unmarshaling for DiskMonitorConfig.
// It handles time.Duration fields which can be strings like "30s".
func (c *DiskMonitorConfig) UnmarshalJSON(data []byte) error {
	type Alias DiskMonitorConfig

	temp := struct {
		*Alias
		CheckInterval string `json:"check_interval"`
		CacheInterval string `json:"cache_interval"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.CheckInterval != "" {
		d, err := time.ParseDuration(temp.CheckInterval)
		if err != nil {
			return fmt.Errorf("invalid check_interval: %w", err)
		}
		c.CheckInterval = d
	}

	if temp.CacheInterval != "" {
		d, err := time.ParseDuration(temp.CacheInterval)
		if err != nil {
			return fmt.Errorf("invalid cache_interval: %w", err)
		}
		c.CacheInterval = d
	}

	return nil
}

// RateLimitConfig contains rate limiting configuration.
type RateLimitConfig struct {
	Algorithm         string  `yaml:"algorithm" json:"algorithm"`
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int     `yaml:"burst" json:"burst"`
	Enabled           bool    `yaml:"enabled" json:"enabled"`
}

// PerClientRateLimitConfig contains per-client rate limiting configuration.
// Per-client rate limiting protects against DoS attacks by limiting requests
// from individual IP addresses independently.
type PerClientRateLimitConfig struct {
	// Enabled enables per-client rate limiting.
	// When enabled, each client IP has its own rate limit bucket.
	// Default: false
	Enabled bool `yaml:"enabled" json:"enabled"`

	// RequestsPerSecond is the maximum requests per second per client.
	// Each client IP has an independent bucket with this rate.
	// Default: 100
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`

	// Burst is the maximum burst capacity per client.
	// Allows temporary traffic bursts from each client.
	// Default: 200 (2x requests_per_second)
	Burst int `yaml:"burst" json:"burst"`

	// MaxClients is the maximum number of clients tracked in the cache.
	// When this limit is reached, the least recently used client is evicted.
	// This protects against memory exhaustion from tracking too many IPs.
	// Default: 10000
	MaxClients int `yaml:"max_clients" json:"max_clients"`

	// TTL is the time-to-live for inactive client entries.
	// Clients not accessed within this period are candidates for eviction.
	// Default: 5m (5 minutes)
	TTL time.Duration `yaml:"ttl" json:"ttl"`
}

// PerMethodRateLimitConfig contains per-method rate limiting configuration.
// Per-method rate limiting allows different rate limits for REQMOD, RESPMOD, and OPTIONS.
type PerMethodRateLimitConfig struct {
	// Enabled enables per-method rate limiting.
	// When enabled, each ICAP method (REQMOD, RESPMOD, OPTIONS) has its own rate limit bucket.
	// Default: false
	Enabled bool `yaml:"enabled" json:"enabled"`

	// RequestsPerSecond is the maximum requests per second per method.
	// Each method has an independent bucket with this rate.
	// Default: 5000
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`

	// Burst is the maximum burst capacity per method.
	// Allows temporary traffic bursts for each method.
	// Default: 7500 (1.5x requests_per_second)
	Burst int `yaml:"burst" json:"burst"`
}

// HealthConfig contains health check endpoint configuration.
type HealthConfig struct {
	HealthPath string `yaml:"health_path" json:"health_path"`
	ReadyPath  string `yaml:"ready_path" json:"ready_path"`
	APIToken   string `yaml:"api_token" json:"api_token"`
	Port       int    `yaml:"port" json:"port"`
	Enabled    bool   `yaml:"enabled" json:"enabled"`
}

// ReplayConfig contains request replay configuration.
type ReplayConfig struct {
	RequestsDir string  `yaml:"requests_dir" json:"requests_dir"`
	Speed       float64 `yaml:"speed" json:"speed"`
	Enabled     bool    `yaml:"enabled" json:"enabled"`
}

// PprofConfig contains pprof profiling endpoint configuration.
// Pprof endpoints are disabled by default for security reasons.
// Enable only when needed for production profiling and diagnostics.
type PprofConfig struct {
	// Enabled enables pprof profiling endpoints.
	// When enabled, pprof endpoints are exposed on the metrics server.
	// WARNING: These endpoints can expose sensitive runtime information.
	// Only enable in trusted environments or with proper access controls.
	// Default: false
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// PluginConfig contains plugin system configuration.
type PluginConfig struct {
	Dir     string   `yaml:"dir" json:"dir"`
	Names   []string `yaml:"names" json:"names"`
	Enabled bool     `yaml:"enabled" json:"enabled"`
}

// ShardingConfig contains scenario sharding configuration for O(1) matching.
// Sharding distributes scenarios across multiple shards based on path hash,
// dramatically improving matching performance for large scenario sets.
type ShardingConfig struct {
	ShardCount  int  `yaml:"shard_count" json:"shard_count"`
	CacheSize   int  `yaml:"cache_size" json:"cache_size"`
	Enabled     bool `yaml:"enabled" json:"enabled"`
	EnableCache bool `yaml:"enable_cache" json:"enable_cache"`
}

// PreviewConfig contains preview mode rate limiting configuration.
// This prevents DoS attacks by limiting the number of preview requests
// per client within a time window.
type PreviewConfig struct {
	// Enabled enables preview rate limiting.
	// When true, preview requests are rate-limited per client.
	// Default: true
	Enabled bool `yaml:"enabled" json:"enabled"`

	// MaxRequests is the maximum number of preview requests allowed
	// per client within the time window.
	// Default: 100
	MaxRequests int `yaml:"max_requests" json:"max_requests"`

	// WindowSeconds is the duration of the sliding window in seconds.
	// Default: 60 seconds
	WindowSeconds int `yaml:"window_seconds" json:"window_seconds"`

	// MaxClients is the maximum number of clients to track.
	// When this limit is reached, the least recently used client is evicted.
	// Default: 10000
	MaxClients int `yaml:"max_clients" json:"max_clients"`
}
