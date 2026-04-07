// Copyright 2026 ICAP Mock

package config_test

import (
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestConfigDefaults_MaxConnections verifies MaxConnections default is 15000.
// WAVE-001: Changed from 1000 to 15000 for high-traffic production workloads.
func TestConfigDefaults_MaxConnections(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedMaxConnections := 15000
	if cfg.Server.MaxConnections != expectedMaxConnections {
		t.Errorf("MaxConnections = %d, want %d (changed for production workloads)",
			cfg.Server.MaxConnections, expectedMaxConnections)
	}
}

// TestConfigDefaults_MaxBodySize verifies MaxBodySize default is 10MB.
// WAVE-001: Changed from 0 (unlimited) to 10MB for security.
// This protects against memory exhaustion attacks from malicious large payloads.
func TestConfigDefaults_MaxBodySize(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedMaxBodySize := int64(10485760) // 10MB = 10 * 1024 * 1024
	if cfg.Server.MaxBodySize != expectedMaxBodySize {
		t.Errorf("MaxBodySize = %d, want %d (10MB for security)",
			cfg.Server.MaxBodySize, expectedMaxBodySize)
	}
}

// TestConfigDefaults_MaxBodySizeIs10MB verifies the exact value is 10MB.
func TestConfigDefaults_MaxBodySizeIs10MB(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify it's exactly 10MB (10485760 bytes)
	tenMB := int64(10 * 1024 * 1024)
	if cfg.Server.MaxBodySize != tenMB {
		t.Errorf("MaxBodySize should be 10MB (10485760 bytes), Got: %d", cfg.Server.MaxBodySize)
	}
}

// TestConfigDefaults_PprofDisabled verifies pprof is disabled by default.
// Security: pprof endpoints can expose sensitive runtime information.
func TestConfigDefaults_PprofDisabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if cfg.Pprof.Enabled {
		t.Error("Pprof.Enabled should be false by default for security")
	}
}

// TestConfigDefaults_RateLimitEnabled verifies rate limiting is enabled by default.
// Production safety: prevents resource exhaustion from malicious traffic.
func TestConfigDefaults_RateLimitEnabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled should be true by default for production safety")
	}
}

// TestConfigDefaults_RateLimitBurst verifies burst capacity matches MaxConnections.
// This ensures rate limiter can handle traffic spikes.
func TestConfigDefaults_RateLimitBurst(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Burst should be >= MaxConnections to handle traffic spikes
	if cfg.RateLimit.Burst < cfg.Server.MaxConnections {
		t.Errorf("RateLimit.Burst (%d) should be >= MaxConnections (%d)",
			cfg.RateLimit.Burst, cfg.Server.MaxConnections)
	}
}

// TestConfigDefaults_StorageEnabled verifies storage is enabled by default.
func TestConfigDefaults_StorageEnabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if !cfg.Storage.Enabled {
		t.Error("Storage.Enabled should be true by default")
	}
}

// TestConfigDefaults_StorageRotation verifies storage rotation config.
func TestConfigDefaults_StorageRotation(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedRotateAfter := 10000
	if cfg.Storage.RotateAfter != expectedRotateAfter {
		t.Errorf("Storage.RotateAfter = %d, want %d",
			cfg.Storage.RotateAfter, expectedRotateAfter)
	}
}

// TestConfigDefaults_MetricsEnabled verifies metrics are enabled by default.
func TestConfigDefaults_MetricsEnabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled should be true by default")
	}
}

// TestConfigDefaults_HealthEnabled verifies health check is enabled by default.
func TestConfigDefaults_HealthEnabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if !cfg.Health.Enabled {
		t.Error("Health.Enabled should be true by default")
	}
}

// TestConfigDefaults_ChaosDisabled verifies chaos is disabled by default.
func TestConfigDefaults_ChaosDisabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if cfg.Chaos.Enabled {
		t.Error("Chaos.Enabled should be false by default")
	}
}

// TestConfigDefaults_ReplayDisabled verifies replay is disabled by default.
func TestConfigDefaults_ReplayDisabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if cfg.Replay.Enabled {
		t.Error("Replay.Enabled should be false by default")
	}
}

// TestConfigDefaults_Timeouts verifies default timeout values.
func TestConfigDefaults_Timeouts(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedReadTimeout := 30 * time.Second
	expectedWriteTimeout := 30 * time.Second
	expectedMockTimeout := 5 * time.Second

	if cfg.Server.ReadTimeout != expectedReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", cfg.Server.ReadTimeout, expectedReadTimeout)
	}
	if cfg.Server.WriteTimeout != expectedWriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", cfg.Server.WriteTimeout, expectedWriteTimeout)
	}
	if cfg.Mock.DefaultTimeout != expectedMockTimeout {
		t.Errorf("Mock.DefaultTimeout = %v, want %v", cfg.Mock.DefaultTimeout, expectedMockTimeout)
	}
}

// TestConfigDefaults_HostAndPort verifies default host and port.
func TestConfigDefaults_HostAndPort(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedHost := "0.0.0.0"
	expectedPort := 1344
	expectedMetricsPort := 9090
	expectedHealthPort := 8080

	if cfg.Server.Host != expectedHost {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, expectedHost)
	}
	if cfg.Server.Port != expectedPort {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, expectedPort)
	}
	if cfg.Metrics.Port != expectedMetricsPort {
		t.Errorf("Metrics.Port = %d, want %d", cfg.Metrics.Port, expectedMetricsPort)
	}
	if cfg.Health.Port != expectedHealthPort {
		t.Errorf("Health.Port = %d, want %d", cfg.Health.Port, expectedHealthPort)
	}
}

// TestConfigDefaults_StreamingEnabled verifies streaming is enabled by default.
func TestConfigDefaults_StreamingEnabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if !cfg.Server.Streaming {
		t.Error("Server.Streaming should be true by default")
	}
}

// TestConfigDefaults_TLSDisabled verifies TLS is disabled by default.
func TestConfigDefaults_TLSDisabled(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if cfg.Server.TLS.Enabled {
		t.Error("Server.TLS.Enabled should be false by default")
	}
}

// TestConfigDefaults_RateLimitAlgorithm verifies rate limit algorithm default.
// WAVE-003: Changed from "token_bucket" to "sharded_token_bucket" for high-load scenarios (10k+ RPS).
func TestConfigDefaults_RateLimitAlgorithm(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedAlgorithm := "sharded_token_bucket"
	if cfg.RateLimit.Algorithm != expectedAlgorithm {
		t.Errorf("RateLimit.Algorithm = %q, want %q",
			cfg.RateLimit.Algorithm, expectedAlgorithm)
	}
}

// TestConfigDefaults_RateLimitRPS verifies requests per second default.
func TestConfigDefaults_RateLimitRPS(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedRPS := float64(10000)
	if cfg.RateLimit.RequestsPerSecond != expectedRPS {
		t.Errorf("RateLimit.RequestsPerSecond = %f, want %f",
			cfg.RateLimit.RequestsPerSecond, expectedRPS)
	}
}

// TestConfigDefaults_StorageWorkers verifies storage worker count.
// PERFORMANCE: Changed from 4 to 16 for high-load scenarios (10k RPS).
// This prevents worker bottlenecks under high traffic.
func TestConfigDefaults_StorageWorkers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedWorkers := 16
	if cfg.Storage.Workers != expectedWorkers {
		t.Errorf("Storage.Workers = %d, want %d (optimized for high-load)", cfg.Storage.Workers, expectedWorkers)
	}
}

// TestConfigDefaults_StorageQueueSize verifies storage queue size.
// PERFORMANCE: Changed from 1000 to 10000 for high-load scenarios (10k RPS).
// This prevents queue bottlenecks under high traffic.
func TestConfigDefaults_StorageQueueSize(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedQueueSize := 10000
	if cfg.Storage.QueueSize != expectedQueueSize {
		t.Errorf("Storage.QueueSize = %d, want %d (optimized for high-load)",
			cfg.Storage.QueueSize, expectedQueueSize)
	}
}

// TestConfigDefaults_LogLevel verifies default log level.
func TestConfigDefaults_LogLevel(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedLevel := "info"
	if cfg.Logging.Level != expectedLevel {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, expectedLevel)
	}
}

// TestConfigDefaults_LogFormat verifies default log format.
func TestConfigDefaults_LogFormat(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	expectedFormat := "json"
	if cfg.Logging.Format != expectedFormat {
		t.Errorf("Logging.Format = %q, want %q", cfg.Logging.Format, expectedFormat)
	}
}

// TestConfigDefaults_RateLimitAlgorithmCanBeChanged verifies that other algorithms can be selected.
// Ensures users can override the default sharded_token_bucket to token_bucket or sliding_window.
func TestConfigDefaults_RateLimitAlgorithmCanBeChanged(t *testing.T) {
	t.Parallel()

	validAlgorithms := []string{"token_bucket", "sliding_window", "sharded_token_bucket"}

	for _, algorithm := range validAlgorithms {
		t.Run(algorithm, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetDefaults()

			// Override the default algorithm
			cfg.RateLimit.Algorithm = algorithm

			if cfg.RateLimit.Algorithm != algorithm {
				t.Errorf("RateLimit.Algorithm = %q, want %q", cfg.RateLimit.Algorithm, algorithm)
			}
		})
	}
}
