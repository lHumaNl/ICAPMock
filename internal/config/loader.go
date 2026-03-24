// Package config provides configuration loading from files and environment
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func warnEnvParse(key, value string, err error) {
	fmt.Fprintf(os.Stderr, "warning: invalid value for %s=%q: %v\n", key, value, err)
}

// MetricsCollector defines the interface for recording config reload metrics.
// This allows the config package to record metrics without importing the metrics package.
type MetricsCollector interface {
	// RecordConfigReload increments the counter for configuration reload attempts
	// with the given status ("success" or "failure").
	RecordConfigReload(status string)

	// RecordConfigReloadDuration records the duration of a configuration reload.
	RecordConfigReloadDuration(duration time.Duration)

	// SetConfigLastReloadStatus sets the gauge indicating the status of the last
	// configuration reload (1 for success, 0 for failure).
	SetConfigLastReloadStatus(success bool)
}

// Loader handles loading configuration from various sources.
type Loader struct {
	envPrefix string
	metrics   MetricsCollector
}

// NewLoader creates a new configuration loader.
// The envPrefix is used for environment variable names (e.g., "ICAP_" for ICAP_SERVER_PORT).
func NewLoader() *Loader {
	return &Loader{
		envPrefix: "ICAP_",
	}
}

// WithMetrics sets the metrics collector for the loader.
// This is optional but recommended for production deployments.
func (l *Loader) WithMetrics(metrics MetricsCollector) *Loader {
	l.metrics = metrics
	return l
}

// LoadOptions contains options for loading configuration.
type LoadOptions struct {
	// ConfigPath is the path to the configuration file (YAML or JSON).
	// If empty, only defaults and environment variables are used.
	ConfigPath string
}

// Load loads configuration from multiple sources with proper precedence:
// 1. Defaults are applied first
// 2. Configuration file values override defaults
// 3. Environment variables override file values
func (l *Loader) Load(opts LoadOptions) (*Config, error) {
	startTime := time.Now()
	cfg := &Config{}
	cfg.SetDefaults()

	// Load from file if specified
	if opts.ConfigPath != "" {
		fileCfg, err := l.LoadFromFile(opts.ConfigPath)
		if err != nil {
			l.recordMetrics(false, time.Since(startTime))
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
		l.mergeConfigs(cfg, fileCfg)
	}

	// Load from environment (highest priority)
	if err := l.LoadFromEnv(cfg); err != nil {
		l.recordMetrics(false, time.Since(startTime))
		return nil, fmt.Errorf("failed to load from environment: %w", err)
	}

	// Validate servers map
	for name, entry := range cfg.Servers {
		if entry.Port == 0 {
			return nil, fmt.Errorf("server %q: port is required", name)
		}
		if entry.ScenariosDir == "" {
			return nil, fmt.Errorf("server %q: scenarios_dir is required", name)
		}
	}

	// Apply defaults to servers: if defaults has values but server entry doesn't,
	// the merge happens at runtime via ServerEntryConfig.ToServerConfig()
	// Here we just ensure defaults have sane values
	if cfg.Defaults.Host == "" && len(cfg.Servers) > 0 {
		cfg.Defaults.Host = "0.0.0.0"
	}
	if cfg.Defaults.ReadTimeout == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.ReadTimeout = 30 * time.Second
	}
	if cfg.Defaults.WriteTimeout == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.WriteTimeout = 30 * time.Second
	}
	if cfg.Defaults.MaxConnections == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.MaxConnections = 15000
	}
	if cfg.Defaults.MaxBodySize == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.MaxBodySize = 10 * 1024 * 1024
	}
	if cfg.Defaults.IdleTimeout == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.IdleTimeout = 60 * time.Second
	}
	if cfg.Defaults.ShutdownTimeout == 0 && len(cfg.Servers) > 0 {
		cfg.Defaults.ShutdownTimeout = 30 * time.Second
	}

	l.recordMetrics(true, time.Since(startTime))
	return cfg, nil
}

// recordMetrics records configuration reload metrics if a collector is configured.
func (l *Loader) recordMetrics(success bool, duration time.Duration) {
	if l.metrics == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	l.metrics.RecordConfigReload(status)
	l.metrics.RecordConfigReloadDuration(duration)
	l.metrics.SetConfigLastReloadStatus(success)
}

// LoadFromFile loads configuration from a YAML or JSON file.
// The file format is determined by the file extension.
func (l *Loader) LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := &Config{}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		// Try YAML first, then JSON
		if err := yaml.Unmarshal(data, cfg); err != nil {
			if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
				return nil, fmt.Errorf("failed to parse config file as YAML or JSON: yaml=%v, json=%v", err, jsonErr)
			}
		}
	}

	return cfg, nil
}

// LoadFromEnv loads configuration from environment variables.
// Environment variables follow the pattern: ICAP_<SECTION>_<KEY>
// For example: ICAP_SERVER_PORT, ICAP_LOGGING_LEVEL
func (l *Loader) LoadFromEnv(cfg *Config) error {
	// Server configuration
	if v := os.Getenv(l.envPrefix + "SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv(l.envPrefix + "SERVER_PORT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = i
		} else {
			warnEnvParse(l.envPrefix+"SERVER_PORT", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ReadTimeout = d
		} else {
			warnEnvParse(l.envPrefix+"SERVER_READ_TIMEOUT", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.WriteTimeout = d
		} else {
			warnEnvParse(l.envPrefix+"SERVER_WRITE_TIMEOUT", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_MAX_CONNECTIONS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Server.MaxConnections = i
		} else {
			warnEnvParse(l.envPrefix+"SERVER_MAX_CONNECTIONS", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_MAX_BODY_SIZE"); v != "" {
		if i, err := ParseByteSize(v); err == nil {
			cfg.Server.MaxBodySize = i
		} else {
			warnEnvParse(l.envPrefix+"SERVER_MAX_BODY_SIZE", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_STREAMING"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Server.Streaming = b
		} else {
			warnEnvParse(l.envPrefix+"SERVER_STREAMING", v, err)
		}
	}

	// TLS configuration
	if v := os.Getenv(l.envPrefix + "SERVER_TLS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Server.TLS.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"SERVER_TLS_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "SERVER_TLS_CERT_FILE"); v != "" {
		cfg.Server.TLS.CertFile = v
	}
	if v := os.Getenv(l.envPrefix + "SERVER_TLS_KEY_FILE"); v != "" {
		cfg.Server.TLS.KeyFile = v
	}
	if v := os.Getenv(l.envPrefix + "SERVER_TLS_CLIENT_CA_FILE"); v != "" {
		cfg.Server.TLS.ClientCAFile = v
	}
	if v := os.Getenv(l.envPrefix + "SERVER_TLS_CLIENT_AUTH"); v != "" {
		cfg.Server.TLS.ClientAuth = v
	}

	// Logging configuration
	if v := os.Getenv(l.envPrefix + "LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv(l.envPrefix + "LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv(l.envPrefix + "LOGGING_OUTPUT"); v != "" {
		cfg.Logging.Output = v
	}
	if v := os.Getenv(l.envPrefix + "LOGGING_MAX_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Logging.MaxSize = i
		} else {
			warnEnvParse(l.envPrefix+"LOGGING_MAX_SIZE", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "LOGGING_MAX_BACKUPS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Logging.MaxBackups = i
		} else {
			warnEnvParse(l.envPrefix+"LOGGING_MAX_BACKUPS", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "LOGGING_MAX_AGE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Logging.MaxAge = i
		} else {
			warnEnvParse(l.envPrefix+"LOGGING_MAX_AGE", v, err)
		}
	}

	// Metrics configuration
	if v := os.Getenv(l.envPrefix + "METRICS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Metrics.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"METRICS_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "METRICS_HOST"); v != "" {
		cfg.Metrics.Host = v
	}
	if v := os.Getenv(l.envPrefix + "METRICS_PORT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Metrics.Port = i
		} else {
			warnEnvParse(l.envPrefix+"METRICS_PORT", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "METRICS_PATH"); v != "" {
		cfg.Metrics.Path = v
	}

	// Mock configuration
	if v := os.Getenv(l.envPrefix + "MOCK_DEFAULT_MODE"); v != "" {
		cfg.Mock.DefaultMode = v
	}
	if v := os.Getenv(l.envPrefix + "MOCK_SCENARIOS_DIR"); v != "" {
		cfg.Mock.ScenariosDir = v
	}
	if v := os.Getenv(l.envPrefix + "MOCK_DEFAULT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Mock.DefaultTimeout = d
		} else {
			warnEnvParse(l.envPrefix+"MOCK_DEFAULT_TIMEOUT", v, err)
		}
	}

	// Chaos configuration
	if v := os.Getenv(l.envPrefix + "CHAOS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Chaos.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "CHAOS_ERROR_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Chaos.ErrorRate = f
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_ERROR_RATE", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "CHAOS_TIMEOUT_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Chaos.TimeoutRate = f
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_TIMEOUT_RATE", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "CHAOS_MIN_LATENCY_MS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Chaos.MinLatencyMs = i
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_MIN_LATENCY_MS", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "CHAOS_MAX_LATENCY_MS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Chaos.MaxLatencyMs = i
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_MAX_LATENCY_MS", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "CHAOS_CONNECTION_DROP_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Chaos.ConnectionDropRate = f
		} else {
			warnEnvParse(l.envPrefix+"CHAOS_CONNECTION_DROP_RATE", v, err)
		}
	}

	// Storage configuration
	if v := os.Getenv(l.envPrefix + "STORAGE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Storage.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"STORAGE_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "STORAGE_REQUESTS_DIR"); v != "" {
		cfg.Storage.RequestsDir = v
	}
	if v := os.Getenv(l.envPrefix + "STORAGE_MAX_FILE_SIZE"); v != "" {
		if i, err := ParseByteSize(v); err == nil {
			cfg.Storage.MaxFileSize = i
		} else {
			warnEnvParse(l.envPrefix+"STORAGE_MAX_FILE_SIZE", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "STORAGE_ROTATE_AFTER"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Storage.RotateAfter = i
		} else {
			warnEnvParse(l.envPrefix+"STORAGE_ROTATE_AFTER", v, err)
		}
	}

	// Rate limit configuration
	if v := os.Getenv(l.envPrefix + "RATE_LIMIT_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.RateLimit.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"RATE_LIMIT_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RateLimit.RequestsPerSecond = f
		} else {
			warnEnvParse(l.envPrefix+"RATE_LIMIT_RPS", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "RATE_LIMIT_REQUESTS_PER_SECOND"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RateLimit.RequestsPerSecond = f
		} else {
			warnEnvParse(l.envPrefix+"RATE_LIMIT_REQUESTS_PER_SECOND", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "RATE_LIMIT_BURST"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.Burst = i
		} else {
			warnEnvParse(l.envPrefix+"RATE_LIMIT_BURST", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "RATE_LIMIT_ALGORITHM"); v != "" {
		cfg.RateLimit.Algorithm = v
	}

	// Health configuration
	if v := os.Getenv(l.envPrefix + "HEALTH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Health.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"HEALTH_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "HEALTH_PORT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Health.Port = i
		} else {
			warnEnvParse(l.envPrefix+"HEALTH_PORT", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "HEALTH_PATH"); v != "" {
		cfg.Health.HealthPath = v
	}
	if v := os.Getenv(l.envPrefix + "HEALTH_HEALTH_PATH"); v != "" {
		cfg.Health.HealthPath = v
	}
	if v := os.Getenv(l.envPrefix + "HEALTH_READY_PATH"); v != "" {
		cfg.Health.ReadyPath = v
	}
	if v := os.Getenv(l.envPrefix + "API_TOKEN"); v != "" {
		cfg.Health.APIToken = v
	}

	// Replay configuration
	if v := os.Getenv(l.envPrefix + "REPLAY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Replay.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"REPLAY_ENABLED", v, err)
		}
	}
	if v := os.Getenv(l.envPrefix + "REPLAY_REQUESTS_DIR"); v != "" {
		cfg.Replay.RequestsDir = v
	}
	if v := os.Getenv(l.envPrefix + "REPLAY_SPEED"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Replay.Speed = f
		} else {
			warnEnvParse(l.envPrefix+"REPLAY_SPEED", v, err)
		}
	}

	// Pprof configuration
	if v := os.Getenv(l.envPrefix + "PPROF_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Pprof.Enabled = b
		} else {
			warnEnvParse(l.envPrefix+"PPROF_ENABLED", v, err)
		}
	}

	// Listener overrides are not supported via environment variables
	// (use config file for multi-listener setup)

	return nil
}

// mergeConfigs merges source config into destination config.
// Only non-zero values from source are applied.
//
// Boolean fields (Streaming, TLS.Enabled, Metrics.Enabled, Health.Enabled,
// Storage.Enabled, RateLimit.Enabled, Chaos.Enabled, HotReload.Enabled)
// are always overwritten from source, since Go's zero value (false) is
// indistinguishable from "not set". This means a config file that omits
// a boolean field will set it to false, overriding any default of true.
func (l *Loader) mergeConfigs(dst, src *Config) {
	// Server
	if src.Server.Host != "" {
		dst.Server.Host = src.Server.Host
	}
	if src.Server.Port != 0 {
		dst.Server.Port = src.Server.Port
	}
	if src.Server.ReadTimeout != 0 {
		dst.Server.ReadTimeout = src.Server.ReadTimeout
	}
	if src.Server.WriteTimeout != 0 {
		dst.Server.WriteTimeout = src.Server.WriteTimeout
	}
	if src.Server.MaxConnections != 0 {
		dst.Server.MaxConnections = src.Server.MaxConnections
	}
	if src.Server.MaxBodySize != 0 {
		dst.Server.MaxBodySize = src.Server.MaxBodySize
	}
	// Streaming is a bool, need to check if it was explicitly set
	// For simplicity, we always merge if source was loaded from file
	dst.Server.Streaming = src.Server.Streaming

	// TLS
	if src.Server.TLS.CertFile != "" {
		dst.Server.TLS.CertFile = src.Server.TLS.CertFile
	}
	if src.Server.TLS.KeyFile != "" {
		dst.Server.TLS.KeyFile = src.Server.TLS.KeyFile
	}
	dst.Server.TLS.Enabled = src.Server.TLS.Enabled
	if src.Server.TLS.ClientCAFile != "" {
		dst.Server.TLS.ClientCAFile = src.Server.TLS.ClientCAFile
	}
	if src.Server.TLS.ClientAuth != "" {
		dst.Server.TLS.ClientAuth = src.Server.TLS.ClientAuth
	}

	// Logging
	if src.Logging.Level != "" {
		dst.Logging.Level = src.Logging.Level
	}
	if src.Logging.Format != "" {
		dst.Logging.Format = src.Logging.Format
	}
	if src.Logging.Output != "" {
		dst.Logging.Output = src.Logging.Output
	}
	if src.Logging.MaxSize != 0 {
		dst.Logging.MaxSize = src.Logging.MaxSize
	}
	if src.Logging.MaxBackups != 0 {
		dst.Logging.MaxBackups = src.Logging.MaxBackups
	}
	if src.Logging.MaxAge != 0 {
		dst.Logging.MaxAge = src.Logging.MaxAge
	}

	// Metrics
	dst.Metrics.Enabled = src.Metrics.Enabled
	if src.Metrics.Host != "" {
		dst.Metrics.Host = src.Metrics.Host
	}
	if src.Metrics.Port != 0 {
		dst.Metrics.Port = src.Metrics.Port
	}
	if src.Metrics.Path != "" {
		dst.Metrics.Path = src.Metrics.Path
	}

	// Mock
	if src.Mock.DefaultMode != "" {
		dst.Mock.DefaultMode = src.Mock.DefaultMode
	}
	if src.Mock.ScenariosDir != "" {
		dst.Mock.ScenariosDir = src.Mock.ScenariosDir
	}
	if src.Mock.DefaultTimeout != 0 {
		dst.Mock.DefaultTimeout = src.Mock.DefaultTimeout
	}

	// Chaos
	dst.Chaos.Enabled = src.Chaos.Enabled
	if src.Chaos.ErrorRate != 0 {
		dst.Chaos.ErrorRate = src.Chaos.ErrorRate
	}
	if src.Chaos.TimeoutRate != 0 {
		dst.Chaos.TimeoutRate = src.Chaos.TimeoutRate
	}
	if src.Chaos.MinLatencyMs != 0 {
		dst.Chaos.MinLatencyMs = src.Chaos.MinLatencyMs
	}
	if src.Chaos.MaxLatencyMs != 0 {
		dst.Chaos.MaxLatencyMs = src.Chaos.MaxLatencyMs
	}
	if src.Chaos.LatencyRate != 0 {
		dst.Chaos.LatencyRate = src.Chaos.LatencyRate
	}
	if src.Chaos.ConnectionDropRate != 0 {
		dst.Chaos.ConnectionDropRate = src.Chaos.ConnectionDropRate
	}

	// Storage
	dst.Storage.Enabled = src.Storage.Enabled
	if src.Storage.RequestsDir != "" {
		dst.Storage.RequestsDir = src.Storage.RequestsDir
	}
	if src.Storage.MaxFileSize != 0 {
		dst.Storage.MaxFileSize = src.Storage.MaxFileSize
	}
	if src.Storage.RotateAfter != 0 {
		dst.Storage.RotateAfter = src.Storage.RotateAfter
	}

	// RateLimit
	dst.RateLimit.Enabled = src.RateLimit.Enabled
	if src.RateLimit.RequestsPerSecond != 0 {
		dst.RateLimit.RequestsPerSecond = src.RateLimit.RequestsPerSecond
	}
	if src.RateLimit.Burst != 0 {
		dst.RateLimit.Burst = src.RateLimit.Burst
	}
	if src.RateLimit.Algorithm != "" {
		dst.RateLimit.Algorithm = src.RateLimit.Algorithm
	}

	// Health
	dst.Health.Enabled = src.Health.Enabled
	if src.Health.Port != 0 {
		dst.Health.Port = src.Health.Port
	}
	if src.Health.HealthPath != "" {
		dst.Health.HealthPath = src.Health.HealthPath
	}
	if src.Health.ReadyPath != "" {
		dst.Health.ReadyPath = src.Health.ReadyPath
	}

	// Replay
	dst.Replay.Enabled = src.Replay.Enabled
	if src.Replay.RequestsDir != "" {
		dst.Replay.RequestsDir = src.Replay.RequestsDir
	}
	if src.Replay.Speed != 0 {
		dst.Replay.Speed = src.Replay.Speed
	}

	// Plugin
	dst.Plugin.Enabled = src.Plugin.Enabled
	if src.Plugin.Dir != "" {
		dst.Plugin.Dir = src.Plugin.Dir
	}
	if len(src.Plugin.Names) > 0 {
		dst.Plugin.Names = src.Plugin.Names
	}

	// Pprof
	dst.Pprof.Enabled = src.Pprof.Enabled
}
