// Copyright 2026 ICAP Mock

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
	metrics   MetricsCollector
	envPrefix string
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
// 3. Environment variables override file values.
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
	if len(cfg.Servers) > 0 {
		applyServerDefaults(&cfg.Defaults)
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

// applyServerDefaults fills in sane defaults for multi-server mode.
func applyServerDefaults(defaults *DefaultsConfig) {
	if defaults.Host == "" {
		defaults.Host = defaultHost
	}
	if defaults.ReadTimeout == 0 {
		defaults.ReadTimeout = 30 * time.Second
	}
	if defaults.WriteTimeout == 0 {
		defaults.WriteTimeout = 30 * time.Second
	}
	if defaults.MaxConnections == 0 {
		defaults.MaxConnections = 15000
	}
	if defaults.MaxBodySize == 0 {
		defaults.MaxBodySize = 10 * 1024 * 1024
	}
	if defaults.IdleTimeout == 0 {
		defaults.IdleTimeout = 60 * time.Second
	}
	if defaults.ShutdownTimeout == 0 {
		defaults.ShutdownTimeout = 30 * time.Second
	}
}

// LoadFromFile loads configuration from a YAML or JSON file.
// The file format is determined by the file extension.
func (l *Loader) LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is validated
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
				return nil, fmt.Errorf("failed to parse config file as YAML or JSON: yaml=%w, json=%w", err, jsonErr)
			}
		}
	}

	return cfg, nil
}

// LoadFromEnv loads configuration from environment variables.
// Environment variables follow the pattern: ICAP_<SECTION>_<KEY>
// For example: ICAP_SERVER_PORT, ICAP_LOGGING_LEVEL.
func (l *Loader) LoadFromEnv(cfg *Config) error {
	l.loadServerEnv(cfg)
	l.loadTLSEnv(cfg)
	l.loadLoggingEnv(cfg)
	l.loadMetricsEnv(cfg)
	l.loadMockEnv(cfg)
	l.loadChaosEnv(cfg)
	l.loadStorageEnv(cfg)
	l.loadRateLimitEnv(cfg)
	l.loadHealthEnv(cfg)
	l.loadReplayEnv(cfg)
	l.loadPprofEnv(cfg)
	return nil
}

// envStr reads an environment variable and sets dst if non-empty.
func (l *Loader) envStr(key string, dst *string) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		*dst = v
	}
}

// envInt reads an environment variable, parses as int, sets dst if valid.
func (l *Loader) envInt(key string, dst *int) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			*dst = i
		} else {
			warnEnvParse(l.envPrefix+key, v, err)
		}
	}
}

// envInt64ByteSize reads an environment variable, parses as byte size, sets dst if valid.
func (l *Loader) envInt64ByteSize(key string, dst *int64) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if i, err := ParseByteSize(v); err == nil {
			*dst = i
		} else {
			warnEnvParse(l.envPrefix+key, v, err)
		}
	}
}

// envBool reads an environment variable, parses as bool, sets dst if valid.
func (l *Loader) envBool(key string, dst *bool) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		} else {
			warnEnvParse(l.envPrefix+key, v, err)
		}
	}
}

// envFloat64 reads an environment variable, parses as float64, sets dst if valid.
func (l *Loader) envFloat64(key string, dst *float64) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*dst = f
		} else {
			warnEnvParse(l.envPrefix+key, v, err)
		}
	}
}

// envDuration reads an environment variable, parses as duration, sets dst if valid.
func (l *Loader) envDuration(key string, dst *time.Duration) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		} else {
			warnEnvParse(l.envPrefix+key, v, err)
		}
	}
}

func (l *Loader) loadServerEnv(cfg *Config) {
	l.envStr("SERVER_HOST", &cfg.Server.Host)
	l.envInt("SERVER_PORT", &cfg.Server.Port)
	l.envDuration("SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout)
	l.envDuration("SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout)
	l.envInt("SERVER_MAX_CONNECTIONS", &cfg.Server.MaxConnections)
	l.envInt64ByteSize("SERVER_MAX_BODY_SIZE", &cfg.Server.MaxBodySize)
	l.envBool("SERVER_STREAMING", &cfg.Server.Streaming)
}

func (l *Loader) loadTLSEnv(cfg *Config) {
	l.envBool("SERVER_TLS_ENABLED", &cfg.Server.TLS.Enabled)
	l.envStr("SERVER_TLS_CERT_FILE", &cfg.Server.TLS.CertFile)
	l.envStr("SERVER_TLS_KEY_FILE", &cfg.Server.TLS.KeyFile)
	l.envStr("SERVER_TLS_CLIENT_CA_FILE", &cfg.Server.TLS.ClientCAFile)
	l.envStr("SERVER_TLS_CLIENT_AUTH", &cfg.Server.TLS.ClientAuth)
}

func (l *Loader) loadLoggingEnv(cfg *Config) {
	l.envStr("LOGGING_LEVEL", &cfg.Logging.Level)
	l.envStr("LOGGING_FORMAT", &cfg.Logging.Format)
	l.envStr("LOGGING_OUTPUT", &cfg.Logging.Output)
	l.envInt("LOGGING_MAX_SIZE", &cfg.Logging.MaxSize)
	l.envInt("LOGGING_MAX_BACKUPS", &cfg.Logging.MaxBackups)
	l.envInt("LOGGING_MAX_AGE", &cfg.Logging.MaxAge)
}

func (l *Loader) loadMetricsEnv(cfg *Config) {
	l.envBool("METRICS_ENABLED", &cfg.Metrics.Enabled)
	l.envStr("METRICS_HOST", &cfg.Metrics.Host)
	l.envInt("METRICS_PORT", &cfg.Metrics.Port)
	l.envStr("METRICS_PATH", &cfg.Metrics.Path)
}

func (l *Loader) loadMockEnv(cfg *Config) {
	l.envStr("MOCK_DEFAULT_MODE", &cfg.Mock.DefaultMode)
	l.envStr("MOCK_SCENARIOS_DIR", &cfg.Mock.ScenariosDir)
	l.envDuration("MOCK_DEFAULT_TIMEOUT", &cfg.Mock.DefaultTimeout)
}

func (l *Loader) loadChaosEnv(cfg *Config) {
	l.envBool("CHAOS_ENABLED", &cfg.Chaos.Enabled)
	l.envFloat64("CHAOS_ERROR_RATE", &cfg.Chaos.ErrorRate)
	l.envFloat64("CHAOS_TIMEOUT_RATE", &cfg.Chaos.TimeoutRate)
	l.envInt("CHAOS_MIN_LATENCY_MS", &cfg.Chaos.MinLatencyMs)
	l.envInt("CHAOS_MAX_LATENCY_MS", &cfg.Chaos.MaxLatencyMs)
	l.envFloat64("CHAOS_CONNECTION_DROP_RATE", &cfg.Chaos.ConnectionDropRate)
}

func (l *Loader) loadStorageEnv(cfg *Config) {
	l.envBool("STORAGE_ENABLED", &cfg.Storage.Enabled)
	l.envStr("STORAGE_REQUESTS_DIR", &cfg.Storage.RequestsDir)
	l.envInt64ByteSize("STORAGE_MAX_FILE_SIZE", &cfg.Storage.MaxFileSize)
	l.envInt("STORAGE_ROTATE_AFTER", &cfg.Storage.RotateAfter)
}

func (l *Loader) loadRateLimitEnv(cfg *Config) {
	l.envBool("RATE_LIMIT_ENABLED", &cfg.RateLimit.Enabled)
	l.envFloat64("RATE_LIMIT_RPS", &cfg.RateLimit.RequestsPerSecond)
	l.envFloat64("RATE_LIMIT_REQUESTS_PER_SECOND", &cfg.RateLimit.RequestsPerSecond)
	l.envInt("RATE_LIMIT_BURST", &cfg.RateLimit.Burst)
	l.envStr("RATE_LIMIT_ALGORITHM", &cfg.RateLimit.Algorithm)
}

func (l *Loader) loadHealthEnv(cfg *Config) {
	l.envBool("HEALTH_ENABLED", &cfg.Health.Enabled)
	l.envInt("HEALTH_PORT", &cfg.Health.Port)
	l.envStr("HEALTH_PATH", &cfg.Health.HealthPath)
	l.envStr("HEALTH_HEALTH_PATH", &cfg.Health.HealthPath)
	l.envStr("HEALTH_READY_PATH", &cfg.Health.ReadyPath)
	l.envStr("API_TOKEN", &cfg.Health.APIToken)
}

func (l *Loader) loadReplayEnv(cfg *Config) {
	l.envBool("REPLAY_ENABLED", &cfg.Replay.Enabled)
	l.envStr("REPLAY_REQUESTS_DIR", &cfg.Replay.RequestsDir)
	l.envFloat64("REPLAY_SPEED", &cfg.Replay.Speed)
}

func (l *Loader) loadPprofEnv(cfg *Config) {
	l.envBool("PPROF_ENABLED", &cfg.Pprof.Enabled)
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
	mergeServerConfig(dst, src)
	mergeLoggingConfig(dst, src)
	mergeMetricsConfig(dst, src)
	mergeMockConfig(dst, src)
	mergeChaosConfig(dst, src)
	mergeStorageConfig(dst, src)
	mergeRateLimitConfig(dst, src)
	mergeHealthConfig(dst, src)
	mergeReplayConfig(dst, src)
	mergePluginConfig(dst, src)
	dst.Pprof.Enabled = src.Pprof.Enabled
}

// mergeStr sets dst to src if src is non-empty.
func mergeStr(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}

// mergeInt sets dst to src if src is non-zero.
func mergeInt(dst *int, src int) {
	if src != 0 {
		*dst = src
	}
}

// mergeInt64 sets dst to src if src is non-zero.
func mergeInt64(dst *int64, src int64) {
	if src != 0 {
		*dst = src
	}
}

// mergeFloat64 sets dst to src if src is non-zero.
func mergeFloat64(dst *float64, src float64) {
	if src != 0 {
		*dst = src
	}
}

// mergeDuration sets dst to src if src is non-zero.
func mergeDuration(dst *time.Duration, src time.Duration) {
	if src != 0 {
		*dst = src
	}
}

func mergeServerConfig(dst, src *Config) {
	mergeStr(&dst.Server.Host, src.Server.Host)
	mergeInt(&dst.Server.Port, src.Server.Port)
	mergeDuration(&dst.Server.ReadTimeout, src.Server.ReadTimeout)
	mergeDuration(&dst.Server.WriteTimeout, src.Server.WriteTimeout)
	mergeInt(&dst.Server.MaxConnections, src.Server.MaxConnections)
	mergeInt64(&dst.Server.MaxBodySize, src.Server.MaxBodySize)
	dst.Server.Streaming = src.Server.Streaming

	// TLS
	mergeStr(&dst.Server.TLS.CertFile, src.Server.TLS.CertFile)
	mergeStr(&dst.Server.TLS.KeyFile, src.Server.TLS.KeyFile)
	dst.Server.TLS.Enabled = src.Server.TLS.Enabled
	mergeStr(&dst.Server.TLS.ClientCAFile, src.Server.TLS.ClientCAFile)
	mergeStr(&dst.Server.TLS.ClientAuth, src.Server.TLS.ClientAuth)
}

func mergeLoggingConfig(dst, src *Config) {
	mergeStr(&dst.Logging.Level, src.Logging.Level)
	mergeStr(&dst.Logging.Format, src.Logging.Format)
	mergeStr(&dst.Logging.Output, src.Logging.Output)
	mergeInt(&dst.Logging.MaxSize, src.Logging.MaxSize)
	mergeInt(&dst.Logging.MaxBackups, src.Logging.MaxBackups)
	mergeInt(&dst.Logging.MaxAge, src.Logging.MaxAge)
}

func mergeMetricsConfig(dst, src *Config) {
	dst.Metrics.Enabled = src.Metrics.Enabled
	mergeStr(&dst.Metrics.Host, src.Metrics.Host)
	mergeInt(&dst.Metrics.Port, src.Metrics.Port)
	mergeStr(&dst.Metrics.Path, src.Metrics.Path)
}

func mergeMockConfig(dst, src *Config) {
	mergeStr(&dst.Mock.DefaultMode, src.Mock.DefaultMode)
	mergeStr(&dst.Mock.ScenariosDir, src.Mock.ScenariosDir)
	mergeDuration(&dst.Mock.DefaultTimeout, src.Mock.DefaultTimeout)
}

func mergeChaosConfig(dst, src *Config) {
	dst.Chaos.Enabled = src.Chaos.Enabled
	mergeFloat64(&dst.Chaos.ErrorRate, src.Chaos.ErrorRate)
	mergeFloat64(&dst.Chaos.TimeoutRate, src.Chaos.TimeoutRate)
	mergeInt(&dst.Chaos.MinLatencyMs, src.Chaos.MinLatencyMs)
	mergeInt(&dst.Chaos.MaxLatencyMs, src.Chaos.MaxLatencyMs)
	mergeFloat64(&dst.Chaos.LatencyRate, src.Chaos.LatencyRate)
	mergeFloat64(&dst.Chaos.ConnectionDropRate, src.Chaos.ConnectionDropRate)
}

func mergeStorageConfig(dst, src *Config) {
	dst.Storage.Enabled = src.Storage.Enabled
	mergeStr(&dst.Storage.RequestsDir, src.Storage.RequestsDir)
	mergeInt64(&dst.Storage.MaxFileSize, src.Storage.MaxFileSize)
	mergeInt(&dst.Storage.RotateAfter, src.Storage.RotateAfter)
}

func mergeRateLimitConfig(dst, src *Config) {
	dst.RateLimit.Enabled = src.RateLimit.Enabled
	mergeFloat64(&dst.RateLimit.RequestsPerSecond, src.RateLimit.RequestsPerSecond)
	mergeInt(&dst.RateLimit.Burst, src.RateLimit.Burst)
	mergeStr(&dst.RateLimit.Algorithm, src.RateLimit.Algorithm)
}

func mergeHealthConfig(dst, src *Config) {
	dst.Health.Enabled = src.Health.Enabled
	mergeInt(&dst.Health.Port, src.Health.Port)
	mergeStr(&dst.Health.HealthPath, src.Health.HealthPath)
	mergeStr(&dst.Health.ReadyPath, src.Health.ReadyPath)
}

func mergeReplayConfig(dst, src *Config) {
	dst.Replay.Enabled = src.Replay.Enabled
	mergeStr(&dst.Replay.RequestsDir, src.Replay.RequestsDir)
	mergeFloat64(&dst.Replay.Speed, src.Replay.Speed)
}

func mergePluginConfig(dst, src *Config) {
	dst.Plugin.Enabled = src.Plugin.Enabled
	mergeStr(&dst.Plugin.Dir, src.Plugin.Dir)
	if len(src.Plugin.Names) > 0 {
		dst.Plugin.Names = src.Plugin.Names
	}
}
