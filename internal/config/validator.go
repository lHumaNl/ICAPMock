// Package config provides configuration validation
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	// Field is the configuration field that failed validation.
	Field string

	// Message describes the validation error.
	Message string

	// Value is the invalid value that was provided.
	Value interface{}
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (value: %v)", e.Field, e.Message, e.Value)
}

// Validator validates configuration values.
type Validator struct {
	validLogLevels  map[string]bool
	validLogFormats map[string]bool
	validModes      map[string]bool
	validAlgorithms map[string]bool
}

// NewValidator creates a new configuration validator.
func NewValidator() *Validator {
	return &Validator{
		validLogLevels: map[string]bool{
			"debug": true,
			"info":  true,
			"warn":  true,
			"error": true,
		},
		validLogFormats: map[string]bool{
			"json": true,
			"text": true,
		},
		validModes: map[string]bool{
			"echo":   true,
			"mock":   true,
			"script": true,
		},
		validAlgorithms: map[string]bool{
			"token_bucket":         true,
			"sliding_window":       true,
			"sharded_token_bucket": true,
		},
	}
}

// Validate validates the entire configuration and returns a list of errors.
// Returns an empty slice if the configuration is valid.
func (v *Validator) Validate(cfg *Config) []ValidationError {
	var errors []ValidationError

	// Validate server configuration
	errors = append(errors, v.validateServer(&cfg.Server)...)

	// Validate logging configuration
	errors = append(errors, v.validateLogging(&cfg.Logging)...)

	// Validate metrics configuration
	errors = append(errors, v.validateMetrics(&cfg.Metrics)...)

	// Validate mock configuration
	errors = append(errors, v.validateMock(&cfg.Mock)...)

	// Validate chaos configuration
	if cfg.Chaos.Enabled {
		errors = append(errors, v.validateChaos(&cfg.Chaos)...)
	}

	// Validate storage configuration
	errors = append(errors, v.validateStorage(&cfg.Storage)...)

	// Validate rate limit configuration
	if cfg.RateLimit.Enabled {
		errors = append(errors, v.validateRateLimit(&cfg.RateLimit)...)
	}

	// Validate health configuration
	errors = append(errors, v.validateHealth(&cfg.Health)...)

	// Validate replay configuration
	if cfg.Replay.Enabled {
		errors = append(errors, v.validateReplay(&cfg.Replay)...)
	}

	return errors
}

// validateServer validates server configuration.
func (v *Validator) validateServer(cfg *ServerConfig) []ValidationError {
	var errors []ValidationError

	// Validate port
	if cfg.Port < 1 || cfg.Port > 65535 {
		errors = append(errors, ValidationError{
			Field:   "server.port",
			Message: "port must be between 1 and 65535",
			Value:   cfg.Port,
		})
	}

	// Validate timeouts
	if cfg.ReadTimeout < 0 {
		errors = append(errors, ValidationError{
			Field:   "server.read_timeout",
			Message: "read timeout must be non-negative",
			Value:   cfg.ReadTimeout,
		})
	}
	if cfg.WriteTimeout < 0 {
		errors = append(errors, ValidationError{
			Field:   "server.write_timeout",
			Message: "write timeout must be non-negative",
			Value:   cfg.WriteTimeout,
		})
	}

	// Validate max connections
	if cfg.MaxConnections < 0 {
		errors = append(errors, ValidationError{
			Field:   "server.max_connections",
			Message: "max connections must be non-negative",
			Value:   cfg.MaxConnections,
		})
	}

	// Validate max body size
	if cfg.MaxBodySize < 0 {
		errors = append(errors, ValidationError{
			Field:   "server.max_body_size",
			Message: "max body size must be non-negative",
			Value:   cfg.MaxBodySize,
		})
	}

	// Validate TLS configuration
	if cfg.TLS.Enabled {
		if cfg.TLS.CertFile == "" {
			errors = append(errors, ValidationError{
				Field:   "server.tls.cert_file",
				Message: "TLS certificate file is required when TLS is enabled",
				Value:   cfg.TLS.CertFile,
			})
		} else if _, err := os.Stat(cfg.TLS.CertFile); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Field:   "server.tls.cert_file",
				Message: "TLS certificate file not found",
				Value:   cfg.TLS.CertFile,
			})
		}
		if cfg.TLS.KeyFile == "" {
			errors = append(errors, ValidationError{
				Field:   "server.tls.key_file",
				Message: "TLS key file is required when TLS is enabled",
				Value:   cfg.TLS.KeyFile,
			})
		} else if _, err := os.Stat(cfg.TLS.KeyFile); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Field:   "server.tls.key_file",
				Message: "TLS key file not found",
				Value:   cfg.TLS.KeyFile,
			})
		}
	}

	return errors
}

// validateLogging validates logging configuration.
func (v *Validator) validateLogging(cfg *LoggingConfig) []ValidationError {
	var errors []ValidationError

	// Validate log level
	if !v.validLogLevels[strings.ToLower(cfg.Level)] {
		errors = append(errors, ValidationError{
			Field:   "logging.level",
			Message: "invalid log level, must be one of: debug, info, warn, error",
			Value:   cfg.Level,
		})
	}

	// Validate log format
	if !v.validLogFormats[strings.ToLower(cfg.Format)] {
		errors = append(errors, ValidationError{
			Field:   "logging.format",
			Message: "invalid log format, must be one of: json, text",
			Value:   cfg.Format,
		})
	}

	// Validate output (allow any non-empty string for flexibility)
	if cfg.Output == "" {
		errors = append(errors, ValidationError{
			Field:   "logging.output",
			Message: "log output cannot be empty",
			Value:   cfg.Output,
		})
	}

	// Validate max size
	if cfg.MaxSize < 0 {
		errors = append(errors, ValidationError{
			Field:   "logging.max_size",
			Message: "max size must be non-negative",
			Value:   cfg.MaxSize,
		})
	}

	// Validate max backups
	if cfg.MaxBackups < 0 {
		errors = append(errors, ValidationError{
			Field:   "logging.max_backups",
			Message: "max backups must be non-negative",
			Value:   cfg.MaxBackups,
		})
	}

	// Validate max age
	if cfg.MaxAge < 0 {
		errors = append(errors, ValidationError{
			Field:   "logging.max_age",
			Message: "max age must be non-negative",
			Value:   cfg.MaxAge,
		})
	}

	return errors
}

// validateMetrics validates metrics configuration.
func (v *Validator) validateMetrics(cfg *MetricsConfig) []ValidationError {
	var errors []ValidationError

	if cfg.Enabled {
		// Validate port
		if cfg.Port < 1 || cfg.Port > 65535 {
			errors = append(errors, ValidationError{
				Field:   "metrics.port",
				Message: "metrics port must be between 1 and 65535",
				Value:   cfg.Port,
			})
		}

		// Validate path
		if cfg.Path == "" {
			errors = append(errors, ValidationError{
				Field:   "metrics.path",
				Message: "metrics path cannot be empty",
				Value:   cfg.Path,
			})
		}
		if !strings.HasPrefix(cfg.Path, "/") {
			errors = append(errors, ValidationError{
				Field:   "metrics.path",
				Message: "metrics path must start with /",
				Value:   cfg.Path,
			})
		}
	}

	return errors
}

// validateMock validates mock configuration.
func (v *Validator) validateMock(cfg *MockConfig) []ValidationError {
	var errors []ValidationError

	// Validate default mode - empty string is also invalid
	if cfg.DefaultMode == "" || !v.validModes[strings.ToLower(cfg.DefaultMode)] {
		errors = append(errors, ValidationError{
			Field:   "mock.default_mode",
			Message: "invalid mock mode, must be one of: echo, mock, script",
			Value:   cfg.DefaultMode,
		})
	}

	// Validate default timeout
	if cfg.DefaultTimeout < 0 {
		errors = append(errors, ValidationError{
			Field:   "mock.default_timeout",
			Message: "default timeout must be non-negative",
			Value:   cfg.DefaultTimeout,
		})
	}

	return errors
}

// validateChaos validates chaos configuration.
func (v *Validator) validateChaos(cfg *ChaosConfig) []ValidationError {
	var errors []ValidationError

	// Validate error rate
	if cfg.ErrorRate < 0 || cfg.ErrorRate > 1 {
		errors = append(errors, ValidationError{
			Field:   "chaos.error_rate",
			Message: "error rate must be between 0 and 1",
			Value:   cfg.ErrorRate,
		})
	}

	// Validate timeout rate
	if cfg.TimeoutRate < 0 || cfg.TimeoutRate > 1 {
		errors = append(errors, ValidationError{
			Field:   "chaos.timeout_rate",
			Message: "timeout rate must be between 0 and 1",
			Value:   cfg.TimeoutRate,
		})
	}

	// Validate connection drop rate
	if cfg.ConnectionDropRate < 0 || cfg.ConnectionDropRate > 1 {
		errors = append(errors, ValidationError{
			Field:   "chaos.connection_drop_rate",
			Message: "connection drop rate must be between 0 and 1",
			Value:   cfg.ConnectionDropRate,
		})
	}

	// Validate latency range - always check for negative values
	if cfg.MinLatencyMs < 0 {
		errors = append(errors, ValidationError{
			Field:   "chaos.latency",
			Message: "minimum latency must be non-negative",
			Value:   cfg.MinLatencyMs,
		})
	}
	if cfg.MaxLatencyMs < 0 {
		errors = append(errors, ValidationError{
			Field:   "chaos.latency",
			Message: "maximum latency must be non-negative",
			Value:   cfg.MaxLatencyMs,
		})
	}
	// Check min > max only if both are non-negative
	if cfg.MinLatencyMs >= 0 && cfg.MaxLatencyMs >= 0 && cfg.MaxLatencyMs > 0 && cfg.MinLatencyMs > cfg.MaxLatencyMs {
		errors = append(errors, ValidationError{
			Field:   "chaos.latency",
			Message: "minimum latency cannot be greater than maximum latency",
			Value:   fmt.Sprintf("min=%d, max=%d", cfg.MinLatencyMs, cfg.MaxLatencyMs),
		})
	}

	return errors
}

// validateStorage validates storage configuration.
func (v *Validator) validateStorage(cfg *StorageConfig) []ValidationError {
	var errors []ValidationError

	if cfg.Enabled {
		// Validate max file size
		if cfg.MaxFileSize < 0 {
			errors = append(errors, ValidationError{
				Field:   "storage.max_file_size",
				Message: "max file size must be non-negative",
				Value:   cfg.MaxFileSize,
			})
		}

		// Validate rotate after
		if cfg.RotateAfter < 0 {
			errors = append(errors, ValidationError{
				Field:   "storage.rotate_after",
				Message: "rotate after must be non-negative",
				Value:   cfg.RotateAfter,
			})
		}
	}

	return errors
}

// validateRateLimit validates rate limit configuration.
func (v *Validator) validateRateLimit(cfg *RateLimitConfig) []ValidationError {
	var errors []ValidationError

	// Validate requests per second
	if cfg.RequestsPerSecond < 0 {
		errors = append(errors, ValidationError{
			Field:   "rate_limit.requests_per_second",
			Message: "requests per second must be non-negative",
			Value:   cfg.RequestsPerSecond,
		})
	}

	// Validate burst
	if cfg.Burst < 0 {
		errors = append(errors, ValidationError{
			Field:   "rate_limit.burst",
			Message: "burst must be non-negative",
			Value:   cfg.Burst,
		})
	}

	// Validate algorithm - empty string is also invalid when rate limit is enabled
	if cfg.Algorithm == "" || !v.validAlgorithms[strings.ToLower(cfg.Algorithm)] {
		errors = append(errors, ValidationError{
			Field:   "rate_limit.algorithm",
			Message: "invalid rate limit algorithm, must be one of: token_bucket, sliding_window, sharded_token_bucket",
			Value:   cfg.Algorithm,
		})
	}

	return errors
}

// validateHealth validates health check configuration.
func (v *Validator) validateHealth(cfg *HealthConfig) []ValidationError {
	var errors []ValidationError

	if cfg.Enabled {
		// Validate port
		if cfg.Port < 1 || cfg.Port > 65535 {
			errors = append(errors, ValidationError{
				Field:   "health.port",
				Message: "health port must be between 1 and 65535",
				Value:   cfg.Port,
			})
		}

		// Validate paths
		if cfg.HealthPath == "" {
			errors = append(errors, ValidationError{
				Field:   "health.health_path",
				Message: "health path cannot be empty",
				Value:   cfg.HealthPath,
			})
		}
		if cfg.ReadyPath == "" {
			errors = append(errors, ValidationError{
				Field:   "health.ready_path",
				Message: "ready path cannot be empty",
				Value:   cfg.ReadyPath,
			})
		}
	}

	return errors
}

// validateReplay validates replay configuration.
func (v *Validator) validateReplay(cfg *ReplayConfig) []ValidationError {
	var errors []ValidationError

	// Validate speed
	if cfg.Speed <= 0 {
		errors = append(errors, ValidationError{
			Field:   "replay.speed",
			Message: "replay speed must be positive",
			Value:   cfg.Speed,
		})
	}

	return errors
}

// ValidatePort validates a port number.
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	return nil
}

// ValidateDuration validates a duration is non-negative.
func ValidateDuration(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("duration must be non-negative, got %v", d)
	}
	return nil
}

// ValidateRate validates a rate is between 0 and 1.
func ValidateRate(rate float64) error {
	if rate < 0 || rate > 1 {
		return fmt.Errorf("rate must be between 0 and 1, got %f", rate)
	}
	return nil
}
