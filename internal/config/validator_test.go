// Copyright 2026 ICAP Mock

package config

import (
	"os"
	"testing"
	"time"
)

// TestValidator_Validate_ValidConfig tests validation of a valid configuration.
func TestValidator_Validate_ValidConfig(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	validator := NewValidator()
	errors := validator.Validate(cfg)

	if len(errors) > 0 {
		t.Errorf("Valid config should have no errors, got: %v", errors)
	}
}

// TestValidator_Validate_ServerNameOptional verifies single-server configs do
// not need an explicit name for metrics labels.
func TestValidator_Validate_ServerNameOptional(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()
	cfg.Server.Name = ""

	errors := NewValidator().Validate(cfg)
	for _, err := range errors {
		if err.Field == "server.name" {
			t.Fatalf("server.name should be optional, got validation error: %v", err)
		}
	}
}

// TestValidator_Validate_ServerPort tests port validation.
func TestValidator_Validate_ServerPort(t *testing.T) {
	tests := []struct {
		name        string
		port        int
		expectError bool
	}{
		{"valid port 80", 80, false},
		{"valid port 1344", 1344, false},
		{"valid port 8080", 8080, false},
		{"valid port 65535", 65535, false},
		{"invalid port 0", 0, true},
		{"invalid port -1", -1, true},
		{"invalid port 65536", 65536, true},
		{"invalid port 100000", 100000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Server.Port = tt.port

			validator := NewValidator()
			errors := validator.Validate(cfg)

			hasPortError := false
			for _, err := range errors {
				if err.Field == "server.port" {
					hasPortError = true
					break
				}
			}

			if tt.expectError && !hasPortError {
				t.Errorf("Expected error for port %d, got none", tt.port)
			}
			if !tt.expectError && hasPortError {
				t.Errorf("Unexpected error for valid port %d", tt.port)
			}
		})
	}
}

// TestValidator_Validate_LogLevel tests log level validation.
func TestValidator_Validate_LogLevel(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		expectError bool
	}{
		{"valid debug", "debug", false},
		{"valid info", "info", false},
		{"valid warn", "warn", false},
		{"valid error", "error", false},
		{"invalid trace", "trace", true},
		{"invalid fatal", "fatal", true},
		{"invalid empty", "", true},
		{"invalid random", "random", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Logging.Level = tt.level

			validator := NewValidator()
			errors := validator.Validate(cfg)

			hasLevelError := false
			for _, err := range errors {
				if err.Field == "logging.level" {
					hasLevelError = true
					break
				}
			}

			if tt.expectError && !hasLevelError {
				t.Errorf("Expected error for level %s, got none", tt.level)
			}
			if !tt.expectError && hasLevelError {
				t.Errorf("Unexpected error for valid level %s", tt.level)
			}
		})
	}
}

// TestValidator_Validate_LogFormat tests log format validation.
func TestValidator_Validate_LogFormat(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		expectError bool
	}{
		{"valid json", "json", false},
		{"valid text", "text", false},
		{"invalid xml", "xml", true},
		{"invalid empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Logging.Format = tt.format

			validator := NewValidator()
			errors := validator.Validate(cfg)

			hasFormatError := false
			for _, err := range errors {
				if err.Field == "logging.format" {
					hasFormatError = true
					break
				}
			}

			if tt.expectError && !hasFormatError {
				t.Errorf("Expected error for format %s, got none", tt.format)
			}
			if !tt.expectError && hasFormatError {
				t.Errorf("Unexpected error for valid format %s", tt.format)
			}
		})
	}
}

// TestValidator_Validate_TLS tests TLS configuration validation.
func TestValidator_Validate_TLS(t *testing.T) {
	// Create temporary certificate files for testing
	tempDir := t.TempDir()
	validCertFile := tempDir + "/cert.pem"
	validKeyFile := tempDir + "/key.pem"

	// Create the temp files
	if err := os.WriteFile(validCertFile, []byte("test cert"), 0o644); err != nil {
		t.Fatalf("Failed to create temp cert file: %v", err)
	}
	if err := os.WriteFile(validKeyFile, []byte("test key"), 0o644); err != nil {
		t.Fatalf("Failed to create temp key file: %v", err)
	}

	tests := []struct {
		name        string
		certFile    string
		keyFile     string
		enabled     bool
		expectError bool
	}{
		{"disabled - no files needed", "", "", false, false},
		{"enabled - valid files", validCertFile, validKeyFile, true, false},
		{"enabled - missing cert", "", validKeyFile, true, true},
		{"enabled - missing key", validCertFile, "", true, true},
		{"enabled - both missing", "", "", true, true},
		{"enabled - cert file not found", "/nonexistent/cert.pem", validKeyFile, true, true},
		{"enabled - key file not found", validCertFile, "/nonexistent/key.pem", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Server.TLS.Enabled = tt.enabled
			cfg.Server.TLS.CertFile = tt.certFile
			cfg.Server.TLS.KeyFile = tt.keyFile

			validator := NewValidator()
			errors := validator.Validate(cfg)

			hasTLSError := false
			for _, err := range errors {
				if err.Field == "server.tls.cert_file" ||
					err.Field == "server.tls.key_file" {
					hasTLSError = true
					break
				}
			}

			if tt.expectError && !hasTLSError {
				t.Errorf("Expected error for TLS config (%v, %s, %s), got none",
					tt.enabled, tt.certFile, tt.keyFile)
			}
			if !tt.expectError && hasTLSError {
				t.Errorf("Unexpected error for valid TLS config (%v, %s, %s)",
					tt.enabled, tt.certFile, tt.keyFile)
			}
		})
	}
}

// TestValidator_Validate_Timeout tests timeout validation.
func TestValidator_Validate_Timeout(t *testing.T) {
	tests := []struct {
		name        string
		readTimeout time.Duration
		expectError bool
	}{
		{"valid timeout", 30 * time.Second, false},
		{"valid timeout 1s", 1 * time.Second, false},
		{"valid timeout 0", 0, false}, // 0 means no timeout
		{"invalid negative", -1 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Server.ReadTimeout = tt.readTimeout

			validator := NewValidator()
			errors := validator.Validate(cfg)

			hasTimeoutError := false
			for _, err := range errors {
				if err.Field == "server.read_timeout" {
					hasTimeoutError = true
					break
				}
			}

			if tt.expectError && !hasTimeoutError {
				t.Errorf("Expected error for timeout %v, got none", tt.readTimeout)
			}
			if !tt.expectError && hasTimeoutError {
				t.Errorf("Unexpected error for valid timeout %v", tt.readTimeout)
			}
		})
	}
}

// TestValidationError tests ValidationError structure.
func TestValidationError(t *testing.T) {
	err := ValidationError{
		Field:   "server.port",
		Message: "port must be between 1 and 65535",
		Value:   0,
	}

	if err.Field != "server.port" {
		t.Errorf("Field = %s, want server.port", err.Field)
	}
	if err.Message != "port must be between 1 and 65535" {
		t.Errorf("Message = %s, want 'port must be between 1 and 65535'", err.Message)
	}
	if err.Value != 0 {
		t.Errorf("Value = %v, want 0", err.Value)
	}

	expectedStr := "server.port: port must be between 1 and 65535 (value: 0)"
	if err.Error() != expectedStr {
		t.Errorf("Error() = %s, want %s", err.Error(), expectedStr)
	}
}

// TestValidator_Validate_MultipleErrors tests that multiple errors are returned.
func TestValidator_Validate_MultipleErrors(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Port = -1           // invalid
	cfg.Logging.Level = "invalid"  // invalid
	cfg.Logging.Format = "invalid" // invalid

	validator := NewValidator()
	errors := validator.Validate(cfg)

	if len(errors) < 3 {
		t.Errorf("Expected at least 3 errors, got %d: %v", len(errors), errors)
	}
}
