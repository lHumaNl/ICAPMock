// Copyright 2026 ICAP Mock

package config_test

import (
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestConfigDefaults_MaxConnections_15000 verifies MaxConnections default is exactly 15000.
// WAVE-001: Production-ready value for high-traffic workloads.
// Previously was 1000 which caused connection exhaustion under load.
func TestConfigDefaults_MaxConnections_15000(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Exact value verification
	expected := 15000
	if cfg.Server.MaxConnections != expected {
		t.Errorf("Server.MaxConnections = %d, want exactly %d", cfg.Server.MaxConnections, expected)
	}

	// Verify it's greater than old default (regression test)
	if cfg.Server.MaxConnections <= 1000 {
		t.Errorf("Server.MaxConnections should be > 1000 (old default), got %d", cfg.Server.MaxConnections)
	}
}

// TestConfigDefaults_MaxBodySize_10MB verifies MaxBodySize default is exactly 10MB (10485760 bytes).
// WAVE-001: Security fix - prevents memory exhaustion attacks.
// Previously was 0 (unlimited) which allowed OOM attacks.
func TestConfigDefaults_MaxBodySize_10MB(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Exact value verification
	expectedBytes := int64(10485760) // 10 * 1024 * 1024
	if cfg.Server.MaxBodySize != expectedBytes {
		t.Errorf("Server.MaxBodySize = %d, want exactly %d (10MB)", cfg.Server.MaxBodySize, expectedBytes)
	}

	// Verify it's positive (not unlimited/zero)
	if cfg.Server.MaxBodySize <= 0 {
		t.Error("Server.MaxBodySize should be positive (not unlimited)")
	}

	// Verify it's reasonable (not too small, not too large)
	if cfg.Server.MaxBodySize < 1<<20 { // Less than 1MB
		t.Error("Server.MaxBodySize should be at least 1MB")
	}
	if cfg.Server.MaxBodySize > 100<<20 { // More than 100MB
		t.Error("Server.MaxBodySize should not exceed 100MB by default")
	}
}

// TestConfigDefaults_MaxBodySize_Megabytes verifies the calculation.
func TestConfigDefaults_MaxBodySize_Megabytes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify it's exactly 10 megabytes
	megabytes := cfg.Server.MaxBodySize / (1024 * 1024)
	if megabytes != 10 {
		t.Errorf("Server.MaxBodySize should be 10MB, got %dMB", megabytes)
	}
}

// TestConfigDefaults_PprofConfig_Disabled verifies PprofConfig defaults.
// WAVE-002: Security - pprof disabled by default.
func TestConfigDefaults_PprofConfig_Disabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Pprof should be disabled by default for security
	if cfg.Pprof.Enabled {
		t.Error("Pprof.Enabled should be false by default (security)")
	}
}

// TestConfigDefaults_PprofConfig_WhenEnabledCanBeSet verifies pprof can be enabled.
func TestConfigDefaults_PprofConfig_WhenEnabledCanBeSet(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify we can enable it
	cfg.Pprof.Enabled = true
	if !cfg.Pprof.Enabled {
		t.Error("Pprof.Enabled should be settable to true")
	}
}

// TestConfigDefaults_AllServerDefaults verifies all ServerConfig defaults together.
func TestConfigDefaults_AllServerDefaults(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	tests := []struct {
		got      interface{}
		expected interface{}
		name     string
	}{
		{"Host", cfg.Server.Host, "0.0.0.0"},
		{"Port", cfg.Server.Port, 1344},
		{"MaxConnections", cfg.Server.MaxConnections, 15000},
		{"MaxBodySize", cfg.Server.MaxBodySize, int64(10485760)},
		{"Streaming", cfg.Server.Streaming, true},
		{"ReadTimeout", cfg.Server.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", cfg.Server.WriteTimeout, 30 * time.Second},
		{"TLS.Enabled", cfg.Server.TLS.Enabled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch got := tt.got.(type) {
			case int:
				if got != tt.expected.(int) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
				}
			case int64:
				if got != tt.expected.(int64) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
				}
			case string:
				if got != tt.expected.(string) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
				}
			case bool:
				if got != tt.expected.(bool) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
				}
			case time.Duration:
				if got != tt.expected.(time.Duration) {
					t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
				}
			default:
				t.Errorf("Unhandled type for %s", tt.name)
			}
		})
	}
}

// TestConfigDefaults_MaxConnections_HighTrafficVerify ensures value supports high traffic.
func TestConfigDefaults_MaxConnections_HighTrafficVerify(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify MaxConnections >= RateLimit.Burst for traffic handling
	if cfg.Server.MaxConnections < cfg.RateLimit.Burst {
		t.Errorf("MaxConnections (%d) should be >= RateLimit.Burst (%d)",
			cfg.Server.MaxConnections, cfg.RateLimit.Burst)
	}

	// Verify MaxConnections >= RateLimit.RequestsPerSecond
	if float64(cfg.Server.MaxConnections) < cfg.RateLimit.RequestsPerSecond {
		t.Errorf("MaxConnections (%d) should be >= RequestsPerSecond (%f)",
			cfg.Server.MaxConnections, cfg.RateLimit.RequestsPerSecond)
	}
}

// TestConfigDefaults_PprofConfig_Isolated verifies PprofConfig is independent.
func TestConfigDefaults_PprofConfig_Isolated(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Pprof config should be independent of other configs
	originalPprof := cfg.Pprof.Enabled

	// Change other configs
	cfg.Metrics.Enabled = false
	cfg.Health.Enabled = false

	// Pprof should remain unchanged
	if cfg.Pprof.Enabled != originalPprof {
		t.Error("Pprof.Enabled should not be affected by other config changes")
	}
}

// TestConfigDefaults_EdgeCases tests edge cases for default values.
func TestConfigDefaults_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("MaxBodySize_is_positive", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.SetDefaults()
		if cfg.Server.MaxBodySize <= 0 {
			t.Error("MaxBodySize must be positive for security")
		}
	})

	t.Run("MaxConnections_is_reasonable", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.SetDefaults()
		// Should be between 1000 and 100000
		if cfg.Server.MaxConnections < 1000 || cfg.Server.MaxConnections > 100000 {
			t.Errorf("MaxConnections %d is outside reasonable range [1000, 100000]",
				cfg.Server.MaxConnections)
		}
	})

	t.Run("multiple_SetDefaults_calls_safe", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.SetDefaults()
		firstMax := cfg.Server.MaxConnections
		cfg.SetDefaults()
		cfg.SetDefaults()
		if cfg.Server.MaxConnections != firstMax {
			t.Errorf("Multiple SetDefaults() calls should be idempotent")
		}
	})
}

// TestConfigDefaults_StorageQueueSize_10000 verifies Storage QueueSize default is exactly 10000.
// PERFORMANCE: Optimized for high-load scenarios (10k RPS).
// Previously was 1000 which caused queue bottlenecks under high load.
func TestConfigDefaults_StorageQueueSize_10000(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expected := 10000
	if cfg.Storage.QueueSize != expected {
		t.Errorf("Storage.QueueSize = %d, want exactly %d", cfg.Storage.QueueSize, expected)
	}

	// Verify it's greater than old default (regression test)
	if cfg.Storage.QueueSize <= 1000 {
		t.Errorf("Storage.QueueSize should be > 1000 (old default), got %d", cfg.Storage.QueueSize)
	}

	// Verify QueueSize >= RateLimit.RequestsPerSecond for high-load scenarios
	if cfg.Storage.QueueSize < int(cfg.RateLimit.RequestsPerSecond) {
		t.Errorf("Storage.QueueSize (%d) should be >= RequestsPerSecond (%f)",
			cfg.Storage.QueueSize, cfg.RateLimit.RequestsPerSecond)
	}
}

// TestConfigDefaults_StorageQueueSize_LoadCapacity verifies queue can handle high load.
func TestConfigDefaults_StorageQueueSize_LoadCapacity(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify QueueSize can handle 1 second of high-load traffic (10k RPS)
	expected := 10000
	if cfg.Storage.QueueSize < expected {
		t.Errorf("Storage.QueueSize (%d) should be >= %d for high-load scenarios",
			cfg.Storage.QueueSize, expected)
	}

	// Verify QueueSize is not excessively large (resource waste)
	maxReasonable := 1000000
	if cfg.Storage.QueueSize > maxReasonable {
		t.Errorf("Storage.QueueSize (%d) should not exceed %d (resource waste)",
			cfg.Storage.QueueSize, maxReasonable)
	}
}

// TestConfigDefaults_StorageQueueSize_WithWorkers verifies QueueSize scales with Workers.
func TestConfigDefaults_StorageQueueSize_WithWorkers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// With 16 workers, each worker should have enough queue items
	itemsPerWorker := cfg.Storage.QueueSize / cfg.Storage.Workers
	minItemsPerWorker := 625

	if itemsPerWorker < minItemsPerWorker {
		t.Errorf("Each worker should have at least %d queue items, got %d (QueueSize=%d, Workers=%d)",
			minItemsPerWorker, itemsPerWorker, cfg.Storage.QueueSize, cfg.Storage.Workers)
	}
}

// TestConfigDefaults_StorageWorkers_16 verifies Storage Workers default is exactly 16.
// PERFORMANCE: Optimized for high-load scenarios (10k RPS).
// Previously was 4 which caused worker bottlenecks under high load.
func TestConfigDefaults_StorageWorkers_16(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expected := 16
	if cfg.Storage.Workers != expected {
		t.Errorf("Storage.Workers = %d, want exactly %d", cfg.Storage.Workers, expected)
	}

	// Verify it's greater than old default (regression test)
	if cfg.Storage.Workers <= 4 {
		t.Errorf("Storage.Workers should be > 4 (old default), got %d", cfg.Storage.Workers)
	}

	// Verify Workers scales with QueueSize for high-load scenarios
	itemsPerWorker := cfg.Storage.QueueSize / cfg.Storage.Workers
	minItemsPerWorker := 625
	if itemsPerWorker < minItemsPerWorker {
		t.Errorf("Each worker should have at least %d queue items, got %d (QueueSize=%d, Workers=%d)",
			minItemsPerWorker, itemsPerWorker, cfg.Storage.QueueSize, cfg.Storage.Workers)
	}
}

// TestConfigDefaults_StorageWorkers_LoadCapacity verifies workers can handle high load.
func TestConfigDefaults_StorageWorkers_LoadCapacity(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify Workers is reasonable for high-load scenarios (10k RPS)
	expected := 16
	if cfg.Storage.Workers < expected {
		t.Errorf("Storage.Workers (%d) should be >= %d for high-load scenarios",
			cfg.Storage.Workers, expected)
	}

	// Verify Workers is not excessively large (resource waste)
	maxReasonable := 64
	if cfg.Storage.Workers > maxReasonable {
		t.Errorf("Storage.Workers (%d) should not exceed %d (resource waste)",
			cfg.Storage.Workers, maxReasonable)
	}

	// Verify Workers can handle RateLimit.RequestsPerSecond (10k RPS)
	// Each worker should be able to handle at least 625 RPS
	expectedRPSPerWorker := cfg.RateLimit.RequestsPerSecond / float64(cfg.Storage.Workers)
	minRPSPerWorker := 625.0
	if expectedRPSPerWorker < minRPSPerWorker {
		t.Errorf("Each worker should handle at least %.0f RPS, got %.0f (Workers=%d, RPS=%.0f)",
			minRPSPerWorker, expectedRPSPerWorker, cfg.Storage.Workers, cfg.RateLimit.RequestsPerSecond)
	}
}
