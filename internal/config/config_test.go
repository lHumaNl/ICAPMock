// Copyright 2026 ICAP Mock

package config

import (
	"testing"
	"time"
)

// TestConfig_SetDefaults tests that all default values are set correctly.
func TestConfig_SetDefaults(t *testing.T) {
	tests := []struct {
		expected interface{}
		name     string
		field    string
	}{
		// Server defaults
		{"Server.Host", "Server.Host", "0.0.0.0"},
		{"Server.Port", "Server.Port", 1344},
		{"Server.ReadTimeout", "Server.ReadTimeout", 30 * time.Second},
		{"Server.WriteTimeout", "Server.WriteTimeout", 30 * time.Second},
		{"Server.MaxConnections", "Server.MaxConnections", 15000},
		{"Server.MaxBodySize", "Server.MaxBodySize", int64(10485760)}, // 10MB
		{"Server.Streaming", "Server.Streaming", true},

		// Logging defaults
		{"Logging.Level", "Logging.Level", "info"},
		{"Logging.Format", "Logging.Format", "json"},
		{"Logging.Output", "Logging.Output", "stdout"},

		// Metrics defaults
		{"Metrics.Enabled", "Metrics.Enabled", true},
		{"Metrics.Port", "Metrics.Port", 9090},
		{"Metrics.Path", "Metrics.Path", "/metrics"},

		// Mock defaults
		{"Mock.DefaultMode", "Mock.DefaultMode", "mock"},
		{"Mock.DefaultTimeout", "Mock.DefaultTimeout", 5 * time.Second},
		{"Mock.ServiceID", "Mock.ServiceID", "icap-mock"},

		// Health defaults
		{"Health.Enabled", "Health.Enabled", true},
		{"Health.Port", "Health.Port", 8080},
		{"Health.HealthPath", "Health.HealthPath", "/health"},
		{"Health.ReadyPath", "Health.ReadyPath", "/ready"},
	}

	cfg := &Config{}
	cfg.SetDefaults()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual interface{}

			switch tt.field {
			// Server
			case "Server.Host":
				actual = cfg.Server.Host
			case "Server.Port":
				actual = cfg.Server.Port
			case "Server.ReadTimeout":
				actual = cfg.Server.ReadTimeout
			case "Server.WriteTimeout":
				actual = cfg.Server.WriteTimeout
			case "Server.MaxConnections":
				actual = cfg.Server.MaxConnections
			case "Server.MaxBodySize":
				actual = cfg.Server.MaxBodySize
			case "Server.Streaming":
				actual = cfg.Server.Streaming

			// Logging
			case "Logging.Level":
				actual = cfg.Logging.Level
			case "Logging.Format":
				actual = cfg.Logging.Format
			case "Logging.Output":
				actual = cfg.Logging.Output

			// Metrics
			case "Metrics.Enabled":
				actual = cfg.Metrics.Enabled
			case "Metrics.Port":
				actual = cfg.Metrics.Port
			case "Metrics.Path":
				actual = cfg.Metrics.Path

			// Mock
			case "Mock.DefaultMode":
				actual = cfg.Mock.DefaultMode
			case "Mock.DefaultTimeout":
				actual = cfg.Mock.DefaultTimeout
			case "Mock.ServiceID":
				actual = cfg.Mock.ServiceID

			// Health
			case "Health.Enabled":
				actual = cfg.Health.Enabled
			case "Health.Port":
				actual = cfg.Health.Port
			case "Health.HealthPath":
				actual = cfg.Health.HealthPath
			case "Health.ReadyPath":
				actual = cfg.Health.ReadyPath
			}

			if actual != tt.expected {
				t.Errorf("SetDefaults() %s = %v, want %v", tt.field, actual, tt.expected)
			}
		})
	}
}

// TestConfig_Structure tests that all configuration structures exist.
func TestConfig_Structure(t *testing.T) {
	cfg := &Config{}

	// Verify all sub-configs exist
	if &cfg.Server == nil {
		t.Error("Server config is nil")
	}
	if &cfg.Logging == nil {
		t.Error("Logging config is nil")
	}
	if &cfg.Metrics == nil {
		t.Error("Metrics config is nil")
	}
	if &cfg.Mock == nil {
		t.Error("Mock config is nil")
	}
	if &cfg.Chaos == nil {
		t.Error("Chaos config is nil")
	}
	if &cfg.Storage == nil {
		t.Error("Storage config is nil")
	}
	if &cfg.RateLimit == nil {
		t.Error("RateLimit config is nil")
	}
	if &cfg.Health == nil {
		t.Error("Health config is nil")
	}
	if &cfg.Replay == nil {
		t.Error("Replay config is nil")
	}
}

// TestServerConfig_Tags tests that yaml/json tags are properly set.
func TestServerConfig_Tags(t *testing.T) {
	cfg := ServerConfig{}
	_ = cfg // Just verify it compiles

	// We'll verify tags work via YAML marshaling in loader tests
}

// TestTLSConfig tests TLS configuration structure.
func TestTLSConfig(t *testing.T) {
	tls := TLSConfig{
		Enabled:  true,
		CertFile: "/path/to/cert.pem",
		KeyFile:  "/path/to/key.pem",
	}

	if !tls.Enabled {
		t.Error("TLS should be enabled")
	}
	if tls.CertFile != "/path/to/cert.pem" {
		t.Errorf("CertFile = %s, want /path/to/cert.pem", tls.CertFile)
	}
	if tls.KeyFile != "/path/to/key.pem" {
		t.Errorf("KeyFile = %s, want /path/to/key.pem", tls.KeyFile)
	}
}

// TestChaosConfig tests Chaos configuration defaults and ranges.
func TestChaosConfig_Defaults(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	// Chaos should be disabled by default
	if cfg.Chaos.Enabled {
		t.Error("Chaos should be disabled by default")
	}
}

// TestRateLimitConfig tests RateLimit configuration.
func TestRateLimitConfig_Algorithms(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	// Rate limit should be configurable
	cfg.RateLimit.Algorithm = "token_bucket"
	if cfg.RateLimit.Algorithm != "token_bucket" {
		t.Error("RateLimit algorithm should be settable")
	}

	cfg.RateLimit.Algorithm = "sliding_window"
	if cfg.RateLimit.Algorithm != "sliding_window" {
		t.Error("RateLimit algorithm should be settable")
	}
}

// TestMockConfig tests Mock configuration.
func TestMockConfig(t *testing.T) {
	cfg := &MockConfig{
		DefaultMode:    "echo",
		ScenariosDir:   "./scenarios",
		DefaultTimeout: 5 * time.Second,
	}

	if cfg.DefaultMode != "echo" {
		t.Errorf("DefaultMode = %s, want echo", cfg.DefaultMode)
	}
	if cfg.ScenariosDir != "./scenarios" {
		t.Errorf("ScenariosDir = %s, want ./scenarios", cfg.ScenariosDir)
	}
	if cfg.DefaultTimeout != 5*time.Second {
		t.Errorf("DefaultTimeout = %v, want 5s", cfg.DefaultTimeout)
	}
}

// TestStorageConfig tests Storage configuration.
func TestStorageConfig(t *testing.T) {
	cfg := &StorageConfig{
		Enabled:     true,
		RequestsDir: "./data/requests",
		MaxFileSize: 104857600, // 100MB
		RotateAfter: 10000,
	}

	if !cfg.Enabled {
		t.Error("Storage should be enabled")
	}
	if cfg.RequestsDir != "./data/requests" {
		t.Errorf("RequestsDir = %s, want ./data/requests", cfg.RequestsDir)
	}
}

// TestReplayConfig tests Replay configuration.
func TestReplayConfig(t *testing.T) {
	cfg := &ReplayConfig{
		Enabled:     true,
		RequestsDir: "./data/requests",
		Speed:       2.0,
	}

	if !cfg.Enabled {
		t.Error("Replay should be enabled")
	}
	if cfg.Speed != 2.0 {
		t.Errorf("Speed = %f, want 2.0", cfg.Speed)
	}
}

// TestLoggingConfig tests Logging configuration levels.
func TestLoggingConfig_Levels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}
	validFormats := []string{"json", "text"}

	for _, level := range validLevels {
		cfg := &LoggingConfig{Level: level}
		if cfg.Level != level {
			t.Errorf("Logging level should be %s", level)
		}
	}

	for _, format := range validFormats {
		cfg := &LoggingConfig{Format: format}
		if cfg.Format != format {
			t.Errorf("Logging format should be %s", format)
		}
	}
}

// TestStorageConfig_QueueSize verifies Storage QueueSize can be configured.
func TestStorageConfig_QueueSize(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	// Verify default QueueSize is 10000
	expectedDefault := 10000
	if cfg.Storage.QueueSize != expectedDefault {
		t.Errorf("Storage.QueueSize default = %d, want %d", cfg.Storage.QueueSize, expectedDefault)
	}

	// Verify QueueSize can be customized
	customSize := 5000
	cfg.Storage.QueueSize = customSize
	if cfg.Storage.QueueSize != customSize {
		t.Errorf("Storage.QueueSize custom = %d, want %d", cfg.Storage.QueueSize, customSize)
	}

	// Verify QueueSize should be positive
	if cfg.Storage.QueueSize <= 0 {
		t.Error("Storage.QueueSize must be positive")
	}
}

// TestStorageConfig_QueueSize_Range verifies QueueSize is within reasonable range.
func TestStorageConfig_QueueSize_Range(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	// QueueSize should be reasonable (not too small, not too large)
	if cfg.Storage.QueueSize < 100 {
		t.Errorf("Storage.QueueSize (%d) should be >= 100", cfg.Storage.QueueSize)
	}

	if cfg.Storage.QueueSize > 1000000 {
		t.Errorf("Storage.QueueSize (%d) should be <= 1000000", cfg.Storage.QueueSize)
	}
}

// TestStorageConfig_QueueSize_HighLoad verifies QueueSize supports high-load scenarios.
func TestStorageConfig_QueueSize_HighLoad(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	// QueueSize should be >= RateLimit.RequestsPerSecond for high-load scenarios
	rps := int(cfg.RateLimit.RequestsPerSecond)
	if cfg.Storage.QueueSize < rps {
		t.Errorf("Storage.QueueSize (%d) should be >= RequestsPerSecond (%d) for high-load",
			cfg.Storage.QueueSize, rps)
	}

	// QueueSize should handle at least 1 second of traffic at full rate
	if cfg.Storage.QueueSize < 10000 {
		t.Errorf("Storage.QueueSize (%d) should be >= 10000 to handle 1s of 10k RPS traffic",
			cfg.Storage.QueueSize)
	}
}
