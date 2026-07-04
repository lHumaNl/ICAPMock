// Copyright 2026 ICAP Mock

// Package config handles loading and validation of server configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultHost       = "0.0.0.0"
	defaultServerName = "default"
)

// DefaultBodyPatternLimitBytes preserves the legacy 10 MiB body_pattern read cap.
const DefaultBodyPatternLimitBytes int64 = 10 * 1024 * 1024

const (
	BodyPatternLimitActionNoMatch = "no_match"
	BodyPatternLimitActionError   = "error"
)

// durationField maps a raw JSON string to a target time.Duration pointer for parsing.
type durationField struct {
	raw    string
	target *time.Duration
	name   string
}

// parseDurationFields parses multiple duration string fields into their targets.
// Empty strings are skipped; invalid durations return a descriptive error.
func parseDurationFields(fields ...durationField) error {
	for _, f := range fields {
		if f.raw != "" {
			d, err := time.ParseDuration(f.raw)
			if err != nil {
				return fmt.Errorf("invalid %s: %w", f.name, err)
			}
			*f.target = d
		}
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
	Streaming       bool          `yaml:"streaming,omitempty" json:"streaming,omitempty"`
	streamingSet    bool
	maxBodySizeSet  bool
}

// UnmarshalJSON implements custom JSON unmarshaling for DefaultsConfig.
func (d *DefaultsConfig) UnmarshalJSON(data []byte) error {
	type Alias DefaultsConfig
	temp := struct {
		*Alias
		MaxBodySize json.RawMessage `json:"max_body_size"`
	}{Alias: (*Alias)(d)}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	if jsonObjectHasKey(data, "streaming") {
		d.SetStreaming(d.Streaming)
	}
	return applyJSONMaxBodySize(temp.MaxBodySize, &d.MaxBodySize, &d.maxBodySizeSet)
}

// SetMaxBodySize records an explicit defaults max body size override.
func (d *DefaultsConfig) SetMaxBodySize(value int64) {
	d.MaxBodySize = value
	d.maxBodySizeSet = true
}

// SetStreaming records an explicit defaults streaming override.
func (d *DefaultsConfig) SetStreaming(value bool) {
	d.Streaming = value
	d.streamingSet = true
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
	Method     MethodList               `yaml:"method,omitempty" json:"method,omitempty"`
	Endpoint   EndpointList             `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Body       string                   `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile   string                   `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	Delay      string                   `yaml:"delay,omitempty" json:"delay,omitempty"`
	Responses  []InlineWeightedResponse `yaml:"responses,omitempty" json:"responses,omitempty"`
	Status     int                      `yaml:"status,omitempty" json:"status,omitempty"`
	HTTPStatus int                      `yaml:"http_status,omitempty" json:"http_status,omitempty"`
	Priority   int                      `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// ServerEntryConfig defines an ICAP server instance with its own port and scenarios.
// Fields that are unset fall back to DefaultsConfig values.
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
	Streaming       bool                           `yaml:"streaming,omitempty" json:"streaming,omitempty"`
	streamingSet    bool
	maxBodySizeSet  bool
}

// UnmarshalJSON implements custom JSON unmarshaling for ServerEntryConfig.
func (e *ServerEntryConfig) UnmarshalJSON(data []byte) error {
	type Alias ServerEntryConfig
	temp := struct {
		*Alias
		MaxBodySize json.RawMessage `json:"max_body_size"`
	}{Alias: (*Alias)(e)}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	if jsonObjectHasKey(data, "streaming") {
		e.SetStreaming(e.Streaming)
	}
	return applyJSONMaxBodySize(temp.MaxBodySize, &e.MaxBodySize, &e.maxBodySizeSet)
}

// SetMaxBodySize records an explicit server entry max body size override.
func (e *ServerEntryConfig) SetMaxBodySize(value int64) {
	e.MaxBodySize = value
	e.maxBodySizeSet = true
}

// SetStreaming records an explicit server entry streaming override.
func (e *ServerEntryConfig) SetStreaming(value bool) {
	e.Streaming = value
	e.streamingSet = true
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
	if defaults.streamingSet {
		cfg.Streaming = defaults.Streaming
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
	if e.MaxBodySize != 0 || e.maxBodySizeSet {
		cfg.MaxBodySize = e.MaxBodySize
	}
	if e.IdleTimeout != 0 {
		cfg.IdleTimeout = e.IdleTimeout
	}
	if e.ShutdownTimeout != 0 {
		cfg.ShutdownTimeout = e.ShutdownTimeout
	}
	if e.streamingSet {
		cfg.Streaming = e.Streaming
	}
	return cfg
}

// Config is the root configuration structure for the ICAP Mock Server.
// It contains all sub-configurations for different components.
type Config struct {
	SourcePath string                       `yaml:"-" json:"-"`
	Servers    map[string]ServerEntryConfig `yaml:"servers,omitempty" json:"servers,omitempty"`
	Health     HealthConfig                 `yaml:"health" json:"health"`
	Management ManagementConfig             `yaml:"management" json:"management"`
	Metrics    MetricsConfig                `yaml:"metrics" json:"metrics"`
	Mock       MockConfig                   `yaml:"mock" json:"mock"`
	Logging    LoggingConfig                `yaml:"logging" json:"logging"`
	Defaults   DefaultsConfig               `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Server     ServerConfig                 `yaml:"server" json:"server"`
	Storage    StorageConfig                `yaml:"storage" json:"storage"`
	Sharding   ShardingConfig               `yaml:"sharding" json:"sharding"`
	Pprof      PprofConfig                  `yaml:"pprof" json:"pprof"`
}

// SetDefaults sets default values for all configuration fields.
// This should be called before loading configuration from files or environment.
func (c *Config) SetDefaults() {
	// Server defaults
	c.Server.Host = defaultHost //nolint:goconst
	c.Server.Name = defaultServerName
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
	c.Metrics.EndpointLabelMode = metricsEndpointLabelModeDefault

	// Mock defaults
	c.Mock.DefaultTimeout = 5 * time.Second
	c.Mock.ServiceID = "icap-mock"
	c.Mock.Matching.BodyPatternLimit = NewBodySizeLimit(DefaultBodyPatternLimitBytes)
	c.Mock.Matching.BodyPatternLimitAction = BodyPatternLimitActionNoMatch

	// Hot reload defaults (disabled by default)
	c.Mock.HotReload.Enabled = false
	c.Mock.HotReload.Debounce = time.Second
	c.Mock.HotReload.WatchDirectory = true

	// Storage defaults (disabled by default)
	c.Storage.Enabled = false
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

	// Health defaults
	c.Health.Enabled = true
	c.Health.Port = 8080
	c.Health.HealthPath = "/health"
	c.Health.ReadyPath = "/ready"

	// Management API defaults (disabled until explicitly enabled)
	c.Management.Enabled = false
	c.Management.ScenarioReloadEnabled = false
	c.Management.ConfigReloadEnabled = false

	// Pprof defaults (disabled by default for security)
	// Production profiling should be explicitly enabled
	c.Pprof.Enabled = false

	// Sharding defaults (disabled by default)
	c.Sharding.Enabled = false
	c.Sharding.ShardCount = 16
	c.Sharding.CacheSize = 1000
	c.Sharding.EnableCache = true

}

// ServerConfig contains ICAP server configuration.
//
//nolint:govet // config field grouping favors stable readability over fieldalignment.
type ServerConfig struct {
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	MaxBodySize     int64         `yaml:"max_body_size" json:"max_body_size"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
	Name            string        `yaml:"name" json:"name"`
	Port            int           `yaml:"port" json:"port"`
	MaxConnections  int           `yaml:"max_connections" json:"max_connections"`
	Streaming       bool          `yaml:"streaming" json:"streaming"`
	maxBodySizeSet  bool
	TLS             TLSConfig `yaml:"tls" json:"tls"`
	Host            string    `yaml:"host" json:"host"`
}

// EffectiveMaxBodySize returns the configured body size limit.
// An explicit max_body_size: 0 means unlimited; a zero value that was never
// configured falls back to defaultLimit for manually constructed configs.
func (c ServerConfig) EffectiveMaxBodySize(defaultLimit int64) int64 {
	if c.MaxBodySize == 0 && !c.maxBodySizeSet {
		return defaultLimit
	}
	return c.MaxBodySize
}

func applyJSONMaxBodySize(raw json.RawMessage, value *int64, present *bool) error {
	if len(raw) == 0 {
		return nil
	}
	size, err := parseJSONMaxBodySize(raw)
	if err != nil {
		return err
	}
	*present = true
	*value = size
	return nil
}

func jsonObjectHasKey(data []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

func parseJSONMaxBodySize(raw json.RawMessage) (int64, error) {
	var num int64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("invalid max_body_size: %w", err)
	}
	size, err := ParseByteSize(text)
	if err != nil {
		return 0, fmt.Errorf("invalid max_body_size: %w", err)
	}
	return size, nil
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

	return applyJSONMaxBodySize(temp.MaxBodySize, &c.MaxBodySize, &c.maxBodySizeSet)
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
	EndpointLabelMode string `yaml:"endpoint_label_mode" json:"endpoint_label_mode"`
	Host              string `yaml:"host" json:"host"`
	Path              string `yaml:"path" json:"path"`
	Port              int    `yaml:"port" json:"port"`
	Enabled           bool   `yaml:"enabled" json:"enabled"`
}

// MockConfig contains mock processor configuration.
type MockConfig struct {
	ScenariosDir   string             `yaml:"scenarios_dir" json:"scenarios_dir"`
	ServiceID      string             `yaml:"service_id" json:"service_id"`
	Matching       MockMatchingConfig `yaml:"matching" json:"matching"`
	HotReload      HotReloadConfig    `yaml:"hot_reload" json:"hot_reload"`
	DefaultTimeout time.Duration      `yaml:"default_timeout" json:"default_timeout"`
}

// MockMatchingConfig contains scenario matching safety limits.
type MockMatchingConfig struct {
	BodyPatternLimitAction string        `yaml:"body_pattern_limit_action" json:"body_pattern_limit_action"`
	BodyPatternLimit       BodySizeLimit `yaml:"body_pattern_limit" json:"body_pattern_limit"`
}

// BodySizeLimit stores a finite byte limit or an explicit unlimited value.
type BodySizeLimit struct {
	Bytes     int64
	Unlimited bool
	set       bool
}

// NewBodySizeLimit returns a finite byte-size limit.
func NewBodySizeLimit(bytes int64) BodySizeLimit {
	return BodySizeLimit{Bytes: bytes, set: true}
}

// NewUnlimitedBodySizeLimit returns an explicitly unlimited byte-size limit.
func NewUnlimitedBodySizeLimit() BodySizeLimit {
	return BodySizeLimit{Unlimited: true, set: true}
}

func (l BodySizeLimit) isSet() bool {
	return l.set || l.Unlimited || l.Bytes != 0
}

// EffectiveBodyPatternLimit resolves the matching limit with server.max_body_size.
func EffectiveBodyPatternLimit(limit BodySizeLimit, serverMaxBodySize int64) BodySizeLimit {
	if limit.Unlimited {
		return effectiveUnlimitedBodyPatternLimit(serverMaxBodySize)
	}
	return effectiveFiniteBodyPatternLimit(limit, serverMaxBodySize)
}

func effectiveUnlimitedBodyPatternLimit(serverMaxBodySize int64) BodySizeLimit {
	if serverMaxBodySize > 0 {
		return NewBodySizeLimit(serverMaxBodySize)
	}
	return NewUnlimitedBodySizeLimit()
}

func effectiveFiniteBodyPatternLimit(limit BodySizeLimit, serverMaxBodySize int64) BodySizeLimit {
	if serverMaxBodySize > 0 && serverMaxBodySize < limit.Bytes {
		return NewBodySizeLimit(serverMaxBodySize)
	}
	return limit
}

// UnmarshalYAML supports sizes like "10mb" and the literal "unlimited".
func (l *BodySizeLimit) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := parseBodySizeLimitNode(value)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// UnmarshalJSON supports sizes like "10mb" and the literal "unlimited".
func (l *BodySizeLimit) UnmarshalJSON(data []byte) error {
	parsed, err := parseBodySizeLimitJSON(data)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

func parseBodySizeLimitNode(value *yaml.Node) (BodySizeLimit, error) {
	if value.Tag == "!!str" {
		return ParseBodySizeLimit(value.Value)
	}
	var bytes int64
	if err := value.Decode(&bytes); err != nil {
		return BodySizeLimit{}, fmt.Errorf("invalid body_pattern_limit: %w", err)
	}
	return parseBodySizeLimitBytes(bytes)
}

func parseBodySizeLimitJSON(data []byte) (BodySizeLimit, error) {
	var bytes int64
	if err := json.Unmarshal(data, &bytes); err == nil {
		return parseBodySizeLimitBytes(bytes)
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return BodySizeLimit{}, fmt.Errorf("invalid body_pattern_limit: %w", err)
	}
	return ParseBodySizeLimit(raw)
}

// ParseBodySizeLimit parses a positive byte size or the literal "unlimited".
func ParseBodySizeLimit(value string) (BodySizeLimit, error) {
	if strings.EqualFold(strings.TrimSpace(value), "unlimited") {
		return NewUnlimitedBodySizeLimit(), nil
	}
	bytes, err := ParseByteSize(value)
	if err != nil {
		return BodySizeLimit{}, err
	}
	return parseBodySizeLimitBytes(bytes)
}

func parseBodySizeLimitBytes(bytes int64) (BodySizeLimit, error) {
	if bytes <= 0 {
		return BodySizeLimit{}, fmt.Errorf("body size must be positive or unlimited: %d", bytes)
	}
	return NewBodySizeLimit(bytes), nil
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

// StorageConfig contains request storage configuration.
type StorageConfig struct {
	RequestsDir string            `yaml:"requests_dir" json:"requests_dir"`
	DiskMonitor DiskMonitorConfig `yaml:"disk_monitor" json:"disk_monitor"`
	MaxFileSize int64             `yaml:"max_file_size" json:"max_file_size"`
	RotateAfter int               `yaml:"rotate_after" json:"rotate_after"`
	Workers     int               `yaml:"workers" json:"workers"`
	QueueSize   int               `yaml:"queue_size" json:"queue_size"`
	Enabled     bool              `yaml:"enabled" json:"enabled"`
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

	return parseDurationFields(
		durationField{raw: temp.CheckInterval, target: &c.CheckInterval, name: "check_interval"},
		durationField{raw: temp.CacheInterval, target: &c.CacheInterval, name: "cache_interval"},
	)
}

// HealthConfig contains health check endpoint configuration.
type HealthConfig struct {
	HealthPath string `yaml:"health_path" json:"health_path"`
	ReadyPath  string `yaml:"ready_path" json:"ready_path"`
	APIToken   string `yaml:"api_token" json:"api_token"`
	Port       int    `yaml:"port" json:"port"`
	Enabled    bool   `yaml:"enabled" json:"enabled"`
}

// ManagementConfig contains management API controls and authentication.
type ManagementConfig struct {
	Token                 string `yaml:"token" json:"token"`
	TokenEnv              string `yaml:"token_env" json:"token_env"`
	Enabled               bool   `yaml:"enabled" json:"enabled"`
	ScenarioReloadEnabled bool   `yaml:"scenario_reload_enabled" json:"scenario_reload_enabled"`
	ConfigReloadEnabled   bool   `yaml:"config_reload_enabled" json:"config_reload_enabled"`
	enabledSet            bool
	scenarioReloadSet     bool
	configReloadSet       bool
}

type managementConfigWire struct {
	Enabled               *bool  `yaml:"enabled" json:"enabled"`
	ScenarioReloadEnabled *bool  `yaml:"scenario_reload_enabled" json:"scenario_reload_enabled"`
	ConfigReloadEnabled   *bool  `yaml:"config_reload_enabled" json:"config_reload_enabled"`
	Token                 string `yaml:"token" json:"token"`
	TokenEnv              string `yaml:"token_env" json:"token_env"`
}

// UnmarshalYAML tracks whether boolean fields were present in a YAML file.
func (c *ManagementConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw managementConfigWire
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.applyWire(raw)
	return nil
}

// UnmarshalJSON tracks whether boolean fields were present in a JSON file.
func (c *ManagementConfig) UnmarshalJSON(data []byte) error {
	var raw managementConfigWire
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.applyWire(raw)
	return nil
}

func (c *ManagementConfig) applyWire(raw managementConfigWire) {
	c.Token = raw.Token
	c.TokenEnv = raw.TokenEnv
	c.setManagementBools(raw)
}

func (c *ManagementConfig) setManagementBools(raw managementConfigWire) {
	setBool(&c.Enabled, &c.enabledSet, raw.Enabled)
	setBool(&c.ScenarioReloadEnabled, &c.scenarioReloadSet, raw.ScenarioReloadEnabled)
	setBool(&c.ConfigReloadEnabled, &c.configReloadSet, raw.ConfigReloadEnabled)
}

func setBool(dst, present, src *bool) {
	if src == nil {
		return
	}
	*dst = *src
	*present = true
}

// ResolvedToken returns the configured bearer token or the token_env value.
func (c ManagementConfig) ResolvedToken() string {
	if c.Token != "" {
		return c.Token
	}
	if c.TokenEnv == "" {
		return ""
	}
	return os.Getenv(c.TokenEnv)
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

// ShardingConfig contains scenario sharding configuration for O(1) matching.
// Sharding distributes scenarios across multiple shards based on path hash,
// dramatically improving matching performance for large scenario sets.
type ShardingConfig struct {
	ShardCount  int  `yaml:"shard_count" json:"shard_count"`
	CacheSize   int  `yaml:"cache_size" json:"cache_size"`
	Enabled     bool `yaml:"enabled" json:"enabled"`
	EnableCache bool `yaml:"enable_cache" json:"enable_cache"`
	enabledSet  bool
	cacheSet    bool
}

// UnmarshalJSON tracks explicitly provided sharding boolean fields.
func (c *ShardingConfig) UnmarshalJSON(data []byte) error {
	type Alias ShardingConfig
	var temp Alias
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	*c = ShardingConfig(temp)

	fields := struct {
		Enabled     *bool `json:"enabled"`
		EnableCache *bool `json:"enable_cache"`
	}{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.Enabled != nil {
		c.Enabled = *fields.Enabled
	}
	if fields.EnableCache != nil {
		c.EnableCache = *fields.EnableCache
	}
	c.enabledSet = fields.Enabled != nil
	c.cacheSet = fields.EnableCache != nil
	return nil
}
