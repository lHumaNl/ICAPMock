// Copyright 2026 ICAP Mock

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoader_LoadYAML tests loading configuration from YAML file.
func TestLoader_LoadYAML(t *testing.T) {
	yamlContent := `
server:
  host: "127.0.0.1"
  port: 1345
  read_timeout: 60s
  write_timeout: 60s
  max_connections: 500
  max_body_size: 10485760
  streaming: false
  tls:
    enabled: true
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"

logging:
  level: "debug"
  format: "text"
  output: "/var/log/icap.log"
  max_size: 200
  max_backups: 10
  max_age: 60

metrics:
  enabled: true
  host: "0.0.0.0"
  port: 9091
  path: "/custom-metrics"

mock:
  default_mode: "mock"
  scenarios_dir: "./custom-scenarios"
  default_timeout: 10s

chaos:
  enabled: true
  error_rate: 0.15
  timeout_rate: 0.05
  min_latency_ms: 50
  max_latency_ms: 200
  connection_drop_rate: 0.01

storage:
  enabled: true
  requests_dir: "./data/custom-requests"
  max_file_size: 209715200
  rotate_after: 5000

rate_limit:
  enabled: true
  requests_per_second: 5000
  burst: 7500
  algorithm: "sliding_window"

health:
  enabled: true
  port: 8081
  health_path: "/healthz"
  ready_path: "/readyz"

replay:
  enabled: true
  requests_dir: "./data/replay"
  speed: 1.5
`

	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Load config
	loader := NewLoader()
	cfg, err := loader.LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify server config
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %s, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 1345 {
		t.Errorf("Server.Port = %d, want 1345", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 60*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 60s", cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout != 60*time.Second {
		t.Errorf("Server.WriteTimeout = %v, want 60s", cfg.Server.WriteTimeout)
	}
	if cfg.Server.MaxConnections != 500 {
		t.Errorf("Server.MaxConnections = %d, want 500", cfg.Server.MaxConnections)
	}
	if cfg.Server.MaxBodySize != 10485760 {
		t.Errorf("Server.MaxBodySize = %d, want 10485760", cfg.Server.MaxBodySize)
	}
	if cfg.Server.Streaming {
		t.Error("Server.Streaming should be false")
	}
	if !cfg.Server.TLS.Enabled {
		t.Error("Server.TLS.Enabled should be true")
	}
	if cfg.Server.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("Server.TLS.CertFile = %s, want /path/to/cert.pem", cfg.Server.TLS.CertFile)
	}

	// Verify logging config
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %s, want debug", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("Logging.Format = %s, want text", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "/var/log/icap.log" {
		t.Errorf("Logging.Output = %s, want /var/log/icap.log", cfg.Logging.Output)
	}

	// Verify metrics config
	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled should be true")
	}
	if cfg.Metrics.Port != 9091 {
		t.Errorf("Metrics.Port = %d, want 9091", cfg.Metrics.Port)
	}
	if cfg.Metrics.Path != "/custom-metrics" {
		t.Errorf("Metrics.Path = %s, want /custom-metrics", cfg.Metrics.Path)
	}

	// Verify mock config
	if cfg.Mock.DefaultMode != "mock" {
		t.Errorf("Mock.DefaultMode = %s, want mock", cfg.Mock.DefaultMode)
	}
	if cfg.Mock.ScenariosDir != "./custom-scenarios" {
		t.Errorf("Mock.ScenariosDir = %s, want ./custom-scenarios", cfg.Mock.ScenariosDir)
	}
	if cfg.Mock.DefaultTimeout != 10*time.Second {
		t.Errorf("Mock.DefaultTimeout = %v, want 10s", cfg.Mock.DefaultTimeout)
	}

	// Verify chaos config
	if !cfg.Chaos.Enabled {
		t.Error("Chaos.Enabled should be true")
	}
	if cfg.Chaos.ErrorRate != 0.15 {
		t.Errorf("Chaos.ErrorRate = %f, want 0.15", cfg.Chaos.ErrorRate)
	}

	// Verify rate limit config
	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled should be true")
	}
	if cfg.RateLimit.Algorithm != "sliding_window" {
		t.Errorf("RateLimit.Algorithm = %s, want sliding_window", cfg.RateLimit.Algorithm)
	}

	// Verify health config
	if cfg.Health.Port != 8081 {
		t.Errorf("Health.Port = %d, want 8081", cfg.Health.Port)
	}
	if cfg.Health.HealthPath != "/healthz" {
		t.Errorf("Health.HealthPath = %s, want /healthz", cfg.Health.HealthPath)
	}

	// Verify replay config
	if !cfg.Replay.Enabled {
		t.Error("Replay.Enabled should be true")
	}
	if cfg.Replay.Speed != 1.5 {
		t.Errorf("Replay.Speed = %f, want 1.5", cfg.Replay.Speed)
	}

	// Verify storage config
	if !cfg.Storage.Enabled {
		t.Error("Storage.Enabled should be true")
	}
	if cfg.Storage.RequestsDir != "./data/custom-requests" {
		t.Errorf("Storage.RequestsDir = %s, want ./data/custom-requests", cfg.Storage.RequestsDir)
	}
	if cfg.Storage.MaxFileSize != 209715200 {
		t.Errorf("Storage.MaxFileSize = %d, want 209715200", cfg.Storage.MaxFileSize)
	}
	if cfg.Storage.RotateAfter != 5000 {
		t.Errorf("Storage.RotateAfter = %d, want 5000", cfg.Storage.RotateAfter)
	}
}

// TestLoader_LoadJSON tests loading configuration from JSON file.
func TestLoader_LoadJSON(t *testing.T) {
	jsonContent := `{
		"server": {
			"host": "192.168.1.1",
			"port": 1346,
			"read_timeout": "45s",
			"write_timeout": "45s",
			"max_connections": 2000,
			"max_body_size": 0,
			"streaming": true,
			"tls": {
				"enabled": false,
				"cert_file": "",
				"key_file": ""
			}
		},
		"logging": {
			"level": "warn",
			"format": "json",
			"output": "stdout"
		},
		"metrics": {
			"enabled": false,
			"host": "0.0.0.0",
			"port": 9092,
			"path": "/metrics"
		},
		"mock": {
			"default_mode": "echo",
			"scenarios_dir": "./scenarios",
			"default_timeout": "3s"
		},
		"chaos": {
			"enabled": false,
			"error_rate": 0,
			"timeout_rate": 0,
			"min_latency_ms": 0,
			"max_latency_ms": 0,
			"connection_drop_rate": 0
		},
		"storage": {
			"enabled": false,
			"requests_dir": "",
			"max_file_size": 0,
			"rotate_after": 0
		},
		"rate_limit": {
			"enabled": false,
			"requests_per_second": 0,
			"burst": 0,
			"algorithm": "token_bucket"
		},
		"health": {
			"enabled": false,
			"port": 8082,
			"health_path": "/health",
			"ready_path": "/ready"
		},
		"replay": {
			"enabled": false,
			"requests_dir": "",
			"speed": 1
		}
	}`

	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(tmpFile, []byte(jsonContent), 0o644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Load config
	loader := NewLoader()
	cfg, err := loader.LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify server config
	if cfg.Server.Host != "192.168.1.1" {
		t.Errorf("Server.Host = %s, want 192.168.1.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 1346 {
		t.Errorf("Server.Port = %d, want 1346", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 45*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 45s", cfg.Server.ReadTimeout)
	}
	if cfg.Server.MaxConnections != 2000 {
		t.Errorf("Server.MaxConnections = %d, want 2000", cfg.Server.MaxConnections)
	}

	// Verify logging config
	if cfg.Logging.Level != "warn" {
		t.Errorf("Logging.Level = %s, want warn", cfg.Logging.Level)
	}

	// Verify metrics disabled
	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled should be false")
	}
}

// TestLoader_LoadFromFile_InvalidFile tests error handling for invalid files.
func TestLoader_LoadFromFile_InvalidFile(t *testing.T) {
	loader := NewLoader()

	// Non-existent file
	_, err := loader.LoadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("LoadFromFile() should return error for non-existent file")
	}
}

// TestLoader_LoadFromFile_InvalidYAML tests error handling for invalid YAML.
func TestLoader_LoadFromFile_InvalidYAML(t *testing.T) {
	invalidYAML := `
server:
  host: "localhost"
  port: [invalid]
`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(tmpFile, []byte(invalidYAML), 0o644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	loader := NewLoader()
	_, err := loader.LoadFromFile(tmpFile)
	if err == nil {
		t.Error("LoadFromFile() should return error for invalid YAML")
	}
}

// TestLoader_LoadFromEnv tests loading configuration from environment variables.
func TestLoader_LoadFromEnv(t *testing.T) {
	// Set environment variables
	envVars := map[string]string{
		"ICAP_SERVER_HOST":            "10.0.0.1",
		"ICAP_SERVER_PORT":            "9999",
		"ICAP_SERVER_MAX_CONNECTIONS": "5000",
		"ICAP_LOGGING_LEVEL":          "error",
		"ICAP_METRICS_ENABLED":        "false",
		"ICAP_HEALTH_PORT":            "9999",
		"ICAP_RATE_LIMIT_ENABLED":     "true",
		"ICAP_RATE_LIMIT_RPS":         "2000",
	}

	// Set env vars
	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k) //nolint:gocritic // deferInLoop: intentional cleanup
	}

	loader := NewLoader()
	cfg := &Config{}
	cfg.SetDefaults()

	err := loader.LoadFromEnv(cfg)
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	// Verify environment overrides
	if cfg.Server.Host != "10.0.0.1" {
		t.Errorf("Server.Host = %s, want 10.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want 9999", cfg.Server.Port)
	}
	if cfg.Server.MaxConnections != 5000 {
		t.Errorf("Server.MaxConnections = %d, want 5000", cfg.Server.MaxConnections)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("Logging.Level = %s, want error", cfg.Logging.Level)
	}
	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled should be false from env")
	}
	if cfg.Health.Port != 9999 {
		t.Errorf("Health.Port = %d, want 9999", cfg.Health.Port)
	}
	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled should be true from env")
	}
	if cfg.RateLimit.RequestsPerSecond != 2000 {
		t.Errorf("RateLimit.RequestsPerSecond = %f, want 2000", cfg.RateLimit.RequestsPerSecond)
	}
}

// TestLoader_Merge tests merging multiple configuration sources.
func TestLoader_Merge(t *testing.T) {
	// Create a base config file
	yamlContent := `
server:
  host: "192.168.1.100"
  port: 1344
logging:
  level: "info"
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Set env to override some values
	os.Setenv("ICAP_SERVER_PORT", "8888")
	os.Setenv("ICAP_LOGGING_LEVEL", "debug")
	defer func() {
		os.Unsetenv("ICAP_SERVER_PORT")
		os.Unsetenv("ICAP_LOGGING_LEVEL")
	}()

	loader := NewLoader()
	cfg, err := loader.Load(LoadOptions{
		ConfigPath: tmpFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// File value should be preserved for host
	if cfg.Server.Host != "192.168.1.100" {
		t.Errorf("Server.Host = %s, want 192.168.1.100 (from file)", cfg.Server.Host)
	}

	// Env should override file for port
	if cfg.Server.Port != 8888 {
		t.Errorf("Server.Port = %d, want 8888 (from env)", cfg.Server.Port)
	}

	// Env should override file for logging level
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %s, want debug (from env)", cfg.Logging.Level)
	}

	// Defaults should be applied for unset values
	if cfg.Server.MaxConnections != 15000 {
		t.Errorf("Server.MaxConnections = %d, want 15000 (default)", cfg.Server.MaxConnections)
	}
}

// TestLoader_DefaultsOnly tests loading with defaults only.
func TestLoader_DefaultsOnly(t *testing.T) {
	loader := NewLoader()
	cfg, err := loader.Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// All values should be defaults
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %s, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 1344 {
		t.Errorf("Server.Port = %d, want 1344", cfg.Server.Port)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %s, want info", cfg.Logging.Level)
	}
}

// TestLoadOptions tests LoadOptions configuration.
func TestLoadOptions(t *testing.T) {
	opts := LoadOptions{
		ConfigPath: "/path/to/config.yaml",
	}

	if opts.ConfigPath != "/path/to/config.yaml" {
		t.Errorf("ConfigPath = %s, want /path/to/config.yaml", opts.ConfigPath)
	}
}

// mockMetricsCollector is a mock implementation of MetricsCollector for testing.
type mockMetricsCollector struct {
	reloadCounts     map[string]int
	lastReloadStatus *bool
	reloadDurations  []time.Duration
}

func newMockMetricsCollector() *mockMetricsCollector {
	return &mockMetricsCollector{
		reloadCounts: make(map[string]int),
	}
}

func (m *mockMetricsCollector) RecordConfigReload(status string) {
	m.reloadCounts[status]++
}

func (m *mockMetricsCollector) RecordConfigReloadDuration(duration time.Duration) {
	m.reloadDurations = append(m.reloadDurations, duration)
}

func (m *mockMetricsCollector) SetConfigLastReloadStatus(success bool) {
	m.lastReloadStatus = &success
}

// TestLoader_WithMetrics tests that WithMetrics sets the metrics collector.
func TestLoader_WithMetrics(t *testing.T) {
	mock := newMockMetricsCollector()
	loader := NewLoader().WithMetrics(mock)

	if loader.metrics == nil {
		t.Error("WithMetrics() should set metrics collector")
	}
}

// TestLoader_LoadWithMetrics_Success tests that metrics are recorded on successful load.
func TestLoader_LoadWithMetrics_Success(t *testing.T) {
	mock := newMockMetricsCollector()
	loader := NewLoader().WithMetrics(mock)

	_, err := loader.Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify success metrics were recorded
	if mock.reloadCounts["success"] != 1 {
		t.Errorf("success count = %d, want 1", mock.reloadCounts["success"])
	}
	if len(mock.reloadDurations) != 1 {
		t.Errorf("duration count = %d, want 1", len(mock.reloadDurations))
	}
	if mock.lastReloadStatus == nil || !*mock.lastReloadStatus {
		t.Error("last reload status should be true (success)")
	}
}

// TestLoader_LoadWithMetrics_Failure tests that metrics are recorded on failed load.
func TestLoader_LoadWithMetrics_Failure(t *testing.T) {
	mock := newMockMetricsCollector()
	loader := NewLoader().WithMetrics(mock)

	// Try to load from non-existent file
	_, err := loader.Load(LoadOptions{ConfigPath: "/nonexistent/config.yaml"})
	if err == nil {
		t.Fatal("Load() should return error for non-existent file")
	}

	// Verify failure metrics were recorded
	if mock.reloadCounts["failure"] != 1 {
		t.Errorf("failure count = %d, want 1", mock.reloadCounts["failure"])
	}
	if len(mock.reloadDurations) != 1 {
		t.Errorf("duration count = %d, want 1", len(mock.reloadDurations))
	}
	if mock.lastReloadStatus == nil || *mock.lastReloadStatus {
		t.Error("last reload status should be false (failure)")
	}
}

// TestLoader_LoadWithoutMetrics tests that loader works without metrics collector.
func TestLoader_LoadWithoutMetrics(t *testing.T) {
	loader := NewLoader()

	// Should not panic when metrics is nil
	_, err := loader.Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoader_LoadMergesShardingConfig(t *testing.T) {
	content := `
sharding:
  enabled: false
  shard_count: 4
  cache_size: 2
  enable_cache: false
`
	cfgPath := filepath.Join(t.TempDir(), "sharding.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertShardingConfig(t, cfg.Sharding)
}

func assertShardingConfig(t *testing.T, got ShardingConfig) {
	t.Helper()
	if got.Enabled || got.EnableCache || got.ShardCount != 4 || got.CacheSize != 2 {
		t.Fatalf("sharding config = %+v, want disabled cache disabled count 4 size 2", got)
	}
}

// TestLoader_LoadProductionConfig tests loading a production-like configuration.
func TestLoader_LoadProductionConfig(t *testing.T) {
	content := `
server:
  host: "0.0.0.0"
  port: 1344
  max_connections: 15000
  tls:
    enabled: true
    cert_file: "/etc/ssl/cert.pem"
    key_file: "/etc/ssl/key.pem"
storage:
  queue_size: 10000
  circuit_breaker:
    enabled: true
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "production.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(LoadOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Load() production config error = %v", err)
	}

	if cfg.Storage.QueueSize != 10000 {
		t.Errorf("Storage.QueueSize = %d, want 10000", cfg.Storage.QueueSize)
	}
	if !cfg.Server.TLS.Enabled {
		t.Error("Server.TLS.Enabled should be true")
	}
	if cfg.Server.MaxConnections != 15000 {
		t.Errorf("Server.MaxConnections = %d, want 15000", cfg.Server.MaxConnections)
	}
	if !cfg.Storage.CircuitBreaker.Enabled {
		t.Error("Storage.CircuitBreaker.Enabled should be true")
	}
}

// TestLoader_LoadDevelopmentConfig tests loading a development-like configuration.
func TestLoader_LoadDevelopmentConfig(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 1344
storage:
  queue_size: 100
logging:
  level: "debug"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "development.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	loader := NewLoader()
	cfg, err := loader.Load(LoadOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Load() development config error = %v", err)
	}

	if cfg.Storage.QueueSize <= 0 {
		t.Errorf("Storage.QueueSize = %d, should be positive", cfg.Storage.QueueSize)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %s, want debug", cfg.Logging.Level)
	}
}
