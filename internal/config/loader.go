// Copyright 2026 ICAP Mock

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	cfg := &Config{}
	cfg.SetDefaults()

	// Load from file if specified
	if opts.ConfigPath != "" {
		fileCfg, err := l.LoadFromFile(opts.ConfigPath)
		if err != nil {
			l.recordMetrics(false)
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
		l.mergeConfigs(cfg, fileCfg)
	}

	// Load from environment (highest priority)
	if err := l.LoadFromEnv(cfg); err != nil {
		l.recordMetrics(false)
		return nil, fmt.Errorf("failed to load from environment: %w", err)
	}
	normalizeLoadedConfig(cfg)

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

	l.recordMetrics(true)
	return cfg, nil
}

// recordMetrics records configuration reload metrics if a collector is configured.
func (l *Loader) recordMetrics(success bool) {
	if l.metrics == nil {
		return
	}

	status := "success"
	if !success {
		status = "failure"
	}

	l.metrics.RecordConfigReload(status)
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
	if defaults.MaxBodySize == 0 && !defaults.maxBodySizeSet {
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
		if err := loadYAMLConfig(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := loadJSONConfig(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		// Try YAML first, then JSON
		if err := loadYAMLConfig(data, cfg); err != nil {
			if jsonErr := loadJSONConfig(data, cfg); jsonErr != nil {
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
	if err := l.rejectRemovedEnvVars(); err != nil {
		return err
	}
	l.loadServerEnv(cfg)
	l.loadTLSEnv(cfg)
	l.loadLoggingEnv(cfg)
	l.loadMetricsEnv(cfg)
	l.loadMockEnv(cfg)
	l.loadStorageEnv(cfg)
	l.loadHealthEnv(cfg)
	l.loadManagementEnv(cfg)
	l.loadShardingEnv(cfg)
	l.loadPprofEnv(cfg)
	return nil
}

func normalizeLoadedConfig(cfg *Config) {
	cfg.Mock.Matching.BodyPatternLimitAction = strings.ToLower(strings.TrimSpace(cfg.Mock.Matching.BodyPatternLimitAction))
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

func (l *Loader) envServerMaxBodySize(cfg *Config) {
	if v := os.Getenv(l.envPrefix + "SERVER_MAX_BODY_SIZE"); v != "" {
		if i, err := ParseByteSize(v); err == nil {
			cfg.Server.MaxBodySize = i
			cfg.Server.maxBodySizeSet = true
		} else {
			warnEnvParse(l.envPrefix+"SERVER_MAX_BODY_SIZE", v, err)
		}
	}
}

func (l *Loader) envBodySizeLimit(key string, dst *BodySizeLimit) {
	if v := os.Getenv(l.envPrefix + key); v != "" {
		if limit, err := ParseBodySizeLimit(v); err == nil {
			*dst = limit
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
	l.envServerMaxBodySize(cfg)
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
	l.envStr("MOCK_SCENARIOS_DIR", &cfg.Mock.ScenariosDir)
	l.envDuration("MOCK_DEFAULT_TIMEOUT", &cfg.Mock.DefaultTimeout)
	l.envBodySizeLimit("MOCK_MATCHING_BODY_PATTERN_LIMIT", &cfg.Mock.Matching.BodyPatternLimit)
	l.envStr("MOCK_MATCHING_BODY_PATTERN_LIMIT_ACTION", &cfg.Mock.Matching.BodyPatternLimitAction)
}

func (l *Loader) loadStorageEnv(cfg *Config) {
	l.envBool("STORAGE_ENABLED", &cfg.Storage.Enabled)
	l.envStr("STORAGE_REQUESTS_DIR", &cfg.Storage.RequestsDir)
	l.envInt64ByteSize("STORAGE_MAX_FILE_SIZE", &cfg.Storage.MaxFileSize)
	l.envInt("STORAGE_ROTATE_AFTER", &cfg.Storage.RotateAfter)
}

func (l *Loader) loadHealthEnv(cfg *Config) {
	l.envBool("HEALTH_ENABLED", &cfg.Health.Enabled)
	l.envInt("HEALTH_PORT", &cfg.Health.Port)
	l.envStr("HEALTH_PATH", &cfg.Health.HealthPath)
	l.envStr("HEALTH_HEALTH_PATH", &cfg.Health.HealthPath)
	l.envStr("HEALTH_READY_PATH", &cfg.Health.ReadyPath)
	l.envStr("API_TOKEN", &cfg.Health.APIToken)
}

func (l *Loader) loadManagementEnv(cfg *Config) {
	l.envBool("MANAGEMENT_ENABLED", &cfg.Management.Enabled)
	l.envBool("MANAGEMENT_SCENARIO_RELOAD_ENABLED", &cfg.Management.ScenarioReloadEnabled)
	l.envBool("MANAGEMENT_CONFIG_RELOAD_ENABLED", &cfg.Management.ConfigReloadEnabled)
	l.envStr("MANAGEMENT_TOKEN", &cfg.Management.Token)
	l.envStr("MANAGEMENT_TOKEN_ENV", &cfg.Management.TokenEnv)
}

func (l *Loader) loadShardingEnv(cfg *Config) {
	l.envBool("SHARDING_ENABLED", &cfg.Sharding.Enabled)
	l.envInt("SHARDING_SHARD_COUNT", &cfg.Sharding.ShardCount)
	l.envInt("SHARDING_CACHE_SIZE", &cfg.Sharding.CacheSize)
	l.envBool("SHARDING_ENABLE_CACHE", &cfg.Sharding.EnableCache)
}

func (l *Loader) loadPprofEnv(cfg *Config) {
	l.envBool("PPROF_ENABLED", &cfg.Pprof.Enabled)
}

// mergeConfigs merges source config into destination config.
// Only non-zero values from source are applied.
//
// Boolean fields (Streaming, TLS.Enabled, Metrics.Enabled, Health.Enabled,
// Storage.Enabled, HotReload.Enabled)
// are always overwritten from source, since Go's zero value (false) is
// indistinguishable from "not set". This means a config file that omits
// a boolean field will set it to false, overriding any default of true.
func (l *Loader) mergeConfigs(dst, src *Config) {
	mergeServerConfig(dst, src)
	mergeLoggingConfig(dst, src)
	mergeMetricsConfig(dst, src)
	mergeMockConfig(dst, src)
	mergeStorageConfig(dst, src)
	mergeHealthConfig(dst, src)
	mergeManagementConfig(dst, src)
	mergeShardingConfig(dst, src)
	dst.Pprof.Enabled = src.Pprof.Enabled
	mergeMultiServerConfig(dst, src)
}

func mergeMultiServerConfig(dst, src *Config) {
	if len(src.Servers) > 0 {
		dst.Servers = src.Servers
	}
	mergeStr(&dst.Defaults.Host, src.Defaults.Host)
	mergeDuration(&dst.Defaults.ReadTimeout, src.Defaults.ReadTimeout)
	mergeDuration(&dst.Defaults.WriteTimeout, src.Defaults.WriteTimeout)
	mergeInt(&dst.Defaults.MaxConnections, src.Defaults.MaxConnections)
	if src.Defaults.maxBodySizeSet {
		dst.Defaults.MaxBodySize = src.Defaults.MaxBodySize
		dst.Defaults.maxBodySizeSet = true
	} else {
		mergeInt64(&dst.Defaults.MaxBodySize, src.Defaults.MaxBodySize)
	}
	mergeDuration(&dst.Defaults.IdleTimeout, src.Defaults.IdleTimeout)
	mergeDuration(&dst.Defaults.ShutdownTimeout, src.Defaults.ShutdownTimeout)
	if src.Defaults.streamingSet {
		dst.Defaults.SetStreaming(src.Defaults.Streaming)
	}
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
	if src.Server.maxBodySizeSet {
		dst.Server.MaxBodySize = src.Server.MaxBodySize
		dst.Server.maxBodySizeSet = true
	} else {
		mergeInt64(&dst.Server.MaxBodySize, src.Server.MaxBodySize)
	}
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
	mergeStr(&dst.Mock.ScenariosDir, src.Mock.ScenariosDir)
	mergeDuration(&dst.Mock.DefaultTimeout, src.Mock.DefaultTimeout)
	mergeMockMatchingConfig(&dst.Mock.Matching, src.Mock.Matching)
}

func mergeMockMatchingConfig(dst *MockMatchingConfig, src MockMatchingConfig) {
	if src.BodyPatternLimit.isSet() {
		dst.BodyPatternLimit = src.BodyPatternLimit
	}
	mergeStr(&dst.BodyPatternLimitAction, src.BodyPatternLimitAction)
}

func mergeStorageConfig(dst, src *Config) {
	dst.Storage.Enabled = src.Storage.Enabled
	mergeStr(&dst.Storage.RequestsDir, src.Storage.RequestsDir)
	mergeInt64(&dst.Storage.MaxFileSize, src.Storage.MaxFileSize)
	mergeInt(&dst.Storage.RotateAfter, src.Storage.RotateAfter)
}

func mergeHealthConfig(dst, src *Config) {
	dst.Health.Enabled = src.Health.Enabled
	mergeInt(&dst.Health.Port, src.Health.Port)
	mergeStr(&dst.Health.HealthPath, src.Health.HealthPath)
	mergeStr(&dst.Health.ReadyPath, src.Health.ReadyPath)
	mergeStr(&dst.Health.APIToken, src.Health.APIToken)
}

func mergeManagementConfig(dst, src *Config) {
	if src.Management.enabledSet {
		dst.Management.Enabled = src.Management.Enabled
	}
	if src.Management.scenarioReloadSet {
		dst.Management.ScenarioReloadEnabled = src.Management.ScenarioReloadEnabled
	}
	if src.Management.configReloadSet {
		dst.Management.ConfigReloadEnabled = src.Management.ConfigReloadEnabled
	}
	mergeStr(&dst.Management.Token, src.Management.Token)
	mergeStr(&dst.Management.TokenEnv, src.Management.TokenEnv)
}

func mergeShardingConfig(dst, src *Config) {
	if src.Sharding.enabledSet {
		dst.Sharding.Enabled = src.Sharding.Enabled
	}
	if src.Sharding.cacheSet {
		dst.Sharding.EnableCache = src.Sharding.EnableCache
	}
	mergeInt(&dst.Sharding.ShardCount, src.Sharding.ShardCount)
	mergeInt(&dst.Sharding.CacheSize, src.Sharding.CacheSize)
}
