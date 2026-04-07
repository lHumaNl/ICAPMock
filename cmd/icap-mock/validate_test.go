// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestValidateMode_ValidConfig tests that validate mode succeeds with valid configuration.
func TestValidateMode_ValidConfig(t *testing.T) {
	t.Parallel()

	// Create a valid configuration
	cfg := &config.Config{}
	cfg.SetDefaults()

	// Create a temp scenarios directory
	tmpDir := t.TempDir()
	scenariosDir := filepath.Join(tmpDir, "scenarios")
	if err := os.Mkdir(scenariosDir, 0755); err != nil {
		t.Fatalf("Failed to create scenarios dir: %v", err)
	}

	// Create a valid scenario file
	scenarioFile := filepath.Join(scenariosDir, "test.yaml")
	scenarioContent := `
name: test-scenario
triggers:
  - service: REQMOD
responses:
  - status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(scenarioContent), 0644); err != nil {
		t.Fatalf("Failed to write scenario file: %v", err)
	}

	cfg.Mock.ScenariosDir = scenariosDir

	var buf bytes.Buffer

	err := RunValidateMode(&buf, cfg)

	output := buf.String()

	if err != nil {
		t.Errorf("RunValidateMode() should succeed with valid config, got error: %v", err)
	}

	// Verify expected output sections
	expectedSections := []string{
		"Validating configuration...",
		"Server Configuration:",
		"Logging Configuration:",
		"Mock Configuration:",
		"Health Configuration:",
		"Configuration validation: PASSED",
	}

	for _, section := range expectedSections {
		if !bytes.Contains([]byte(output), []byte(section)) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", section, output)
		}
	}
}

// TestValidateMode_MissingScenariosDir tests that validate mode fails when scenarios directory is missing.
func TestValidateMode_MissingScenariosDir(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Set a non-existent scenarios directory
	cfg.Mock.ScenariosDir = "/nonexistent/scenarios/directory"

	var buf bytes.Buffer

	err := RunValidateMode(&buf, cfg)

	output := buf.String()

	// Should return error for missing directory
	if err == nil {
		t.Error("RunValidateMode() should return error when scenarios directory is missing")
	}

	// Verify warning message is in output
	if !bytes.Contains([]byte(output), []byte("WARNING: scenarios directory not found")) {
		t.Errorf("Expected warning about missing scenarios directory, got:\n%s", output)
	}
}

// TestValidateMode_EmptyScenariosDir tests validation with empty scenarios directory.
func TestValidateMode_EmptyScenariosDir(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Create an empty scenarios directory
	tmpDir := t.TempDir()
	scenariosDir := filepath.Join(tmpDir, "empty-scenarios")
	if err := os.Mkdir(scenariosDir, 0755); err != nil {
		t.Fatalf("Failed to create scenarios dir: %v", err)
	}

	cfg.Mock.ScenariosDir = scenariosDir

	var buf bytes.Buffer

	err := RunValidateMode(&buf, cfg)

	output := buf.String()

	// Empty directory should still pass (0 scenarios is valid)
	if err != nil {
		t.Errorf("RunValidateMode() should succeed with empty scenarios dir, got: %v", err)
	}

	// Should show 0 scenarios loaded
	if !bytes.Contains([]byte(output), []byte("scenarios loaded: 0 files found")) {
		t.Errorf("Expected '0 files found' in output, got:\n%s", output)
	}
}

// TestValidateMode_WithAllFeatures tests validation with all optional features enabled.
func TestValidateMode_WithAllFeatures(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Create temp scenarios directory
	tmpDir := t.TempDir()
	scenariosDir := filepath.Join(tmpDir, "scenarios")
	if err := os.Mkdir(scenariosDir, 0755); err != nil {
		t.Fatalf("Failed to create scenarios dir: %v", err)
	}
	cfg.Mock.ScenariosDir = scenariosDir

	// Enable all optional features
	cfg.Metrics.Enabled = true
	cfg.Metrics.Host = "0.0.0.0"
	cfg.Metrics.Port = 9090
	cfg.Metrics.Path = "/metrics"

	cfg.Chaos.Enabled = true
	cfg.Chaos.ErrorRate = 0.1
	cfg.Chaos.TimeoutRate = 0.05
	cfg.Chaos.MinLatencyMs = 50
	cfg.Chaos.MaxLatencyMs = 200
	cfg.Chaos.ConnectionDropRate = 0.01

	cfg.Storage.Enabled = true
	cfg.Storage.RequestsDir = "./data/requests"
	cfg.Storage.MaxFileSize = 104857600
	cfg.Storage.RotateAfter = 10000

	cfg.RateLimit.Enabled = true
	cfg.RateLimit.RequestsPerSecond = 10000
	cfg.RateLimit.Burst = 15000
	cfg.RateLimit.Algorithm = "token_bucket"

	cfg.Health.Enabled = true
	cfg.Health.Port = 8080
	cfg.Health.HealthPath = "/health"
	cfg.Health.ReadyPath = "/ready"

	var buf bytes.Buffer

	err := RunValidateMode(&buf, cfg)

	output := buf.String()

	if err != nil {
		t.Errorf("RunValidateMode() should succeed with all features, got: %v", err)
	}

	// Verify all sections are printed
	expectedSections := []string{
		"Metrics Configuration:",
		"Chaos Configuration:",
		"Storage Configuration:",
		"Rate Limit Configuration:",
		"Health Configuration:",
	}

	for _, section := range expectedSections {
		if !bytes.Contains([]byte(output), []byte(section)) {
			t.Errorf("Expected output to contain %q when feature enabled", section)
		}
	}
}

// TestValidateMode_TLSConfiguration tests validation output with TLS enabled.
func TestValidateMode_TLSConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		expectedOutput string
		tlsEnabled     bool
	}{
		{
			name:           "TLS disabled",
			tlsEnabled:     false,
			expectedOutput: "tls: disabled",
		},
		{
			name:           "TLS enabled",
			tlsEnabled:     true,
			expectedOutput: "tls: enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetDefaults()

			cfg.Server.TLS.Enabled = tt.tlsEnabled
			cfg.Server.TLS.CertFile = "/path/to/cert.pem"
			cfg.Mock.ScenariosDir = "" // Skip scenarios check

			var buf bytes.Buffer

			_ = RunValidateMode(&buf, cfg)

			output := buf.String()

			if !bytes.Contains([]byte(output), []byte(tt.expectedOutput)) {
				t.Errorf("Expected output to contain %q, got:\n%s", tt.expectedOutput, output)
			}
		})
	}
}

// TestValidateMode_ScenarioFileCount tests that scenario files are counted correctly.
func TestValidateMode_ScenarioFileCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fileNames     []string
		expectedCount int
	}{
		{
			name:          "no files",
			fileNames:     []string{},
			expectedCount: 0,
		},
		{
			name:          "one yaml file",
			fileNames:     []string{"scenario1.yaml"},
			expectedCount: 1,
		},
		{
			name:          "multiple yaml files",
			fileNames:     []string{"scenario1.yaml", "scenario2.yml", "scenario3.yaml"},
			expectedCount: 3,
		},
		{
			name:          "mixed files - only yaml counted",
			fileNames:     []string{"scenario.yaml", "readme.txt", "config.json"},
			expectedCount: 1,
		},
		{
			name:          "yml extension",
			fileNames:     []string{"scenario1.yml", "scenario2.yml"},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetDefaults()

			// Create temp directory with files
			tmpDir := t.TempDir()
			scenariosDir := filepath.Join(tmpDir, "scenarios")
			if err := os.Mkdir(scenariosDir, 0755); err != nil {
				t.Fatalf("Failed to create scenarios dir: %v", err)
			}

			for _, fileName := range tt.fileNames {
				filePath := filepath.Join(scenariosDir, fileName)
				if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
					t.Fatalf("Failed to write file %s: %v", fileName, err)
				}
			}

			cfg.Mock.ScenariosDir = scenariosDir

			var buf bytes.Buffer

			_ = RunValidateMode(&buf, cfg)

			output := buf.String()

			// Use Contains with the pattern
			if tt.expectedCount > 0 || len(tt.fileNames) == 0 {
				// For 0 or expected counts, verify the count appears
				countStr := string(rune('0' + tt.expectedCount))
				if tt.expectedCount != 0 {
					if !bytes.Contains([]byte(output), []byte(countStr+" file")) {
						t.Errorf("Expected %d files in output, got:\n%s", tt.expectedCount, output)
					}
				}
			}
		})
	}
}

// TestValidateMode_OutputFormat tests that output format is correct.
func TestValidateMode_OutputFormat(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify key configuration values are printed
	expectedValues := []string{
		"host: 0.0.0.0",
		"port: 1344",
		"level: info",
		"format: json",
		"enabled: true", // Health enabled
	}

	for _, val := range expectedValues {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q", val)
		}
	}
}

// TestPrintValidationErrors tests that validation errors are printed correctly.
func TestPrintValidationErrors(t *testing.T) {
	t.Parallel()

	errors := []config.ValidationError{
		{Field: "server.port", Message: "port must be between 1 and 65535", Value: 0},
		{Field: "logging.level", Message: "invalid log level", Value: "invalid"},
	}

	var buf bytes.Buffer

	PrintValidationErrors(&buf, errors)

	output := buf.String()

	// Verify header and errors are printed
	if !bytes.Contains([]byte(output), []byte("Configuration validation failed:")) {
		t.Error("Expected validation failed header")
	}
	if !bytes.Contains([]byte(output), []byte("server.port")) {
		t.Error("Expected server.port error")
	}
	if !bytes.Contains([]byte(output), []byte("logging.level")) {
		t.Error("Expected logging.level error")
	}
}

// TestValidateMode_Integration tests the full validation flow.
func TestValidateMode_Integration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupConfig func(*config.Config, string)
		name        string
		expectError bool
	}{
		{
			name: "valid default config with scenarios",
			setupConfig: func(cfg *config.Config, tmpDir string) {
				scenariosDir := filepath.Join(tmpDir, "scenarios")
				os.Mkdir(scenariosDir, 0755)
				cfg.Mock.ScenariosDir = scenariosDir
			},
			expectError: false,
		},
		{
			name: "missing scenarios directory",
			setupConfig: func(cfg *config.Config, tmpDir string) {
				cfg.Mock.ScenariosDir = filepath.Join(tmpDir, "nonexistent")
			},
			expectError: true,
		},
		{
			name: "no scenarios directory configured",
			setupConfig: func(cfg *config.Config, tmpDir string) {
				cfg.Mock.ScenariosDir = ""
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetDefaults()

			tmpDir := t.TempDir()
			tt.setupConfig(cfg, tmpDir)

			var buf bytes.Buffer
			err := RunValidateMode(&buf, cfg)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestValidateMode_TimeoutDisplay tests that timeout values are displayed correctly.
func TestValidateMode_TimeoutDisplay(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Server.ReadTimeout = 60 * time.Second
	cfg.Server.WriteTimeout = 45 * time.Second
	cfg.Mock.DefaultTimeout = 10 * time.Second
	cfg.Mock.ScenariosDir = ""

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify timeout values are displayed
	expectedTimeouts := []string{
		"read_timeout: 1m0s",
		"write_timeout: 45s",
		"default_timeout: 10s",
	}

	for _, timeout := range expectedTimeouts {
		if !bytes.Contains([]byte(output), []byte(timeout)) {
			t.Errorf("Expected output to contain %q", timeout)
		}
	}
}

// TestValidateMode_ChaosConfiguration tests chaos configuration output.
func TestValidateMode_ChaosConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.Chaos.Enabled = true
	cfg.Chaos.ErrorRate = 0.15
	cfg.Chaos.TimeoutRate = 0.05
	cfg.Chaos.MinLatencyMs = 100
	cfg.Chaos.MaxLatencyMs = 500
	cfg.Chaos.ConnectionDropRate = 0.02

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify chaos configuration values
	expectedChaos := []string{
		"Chaos Configuration:",
		"enabled: true",
		"error_rate: 0.15",
		"timeout_rate: 0.05",
		"latency: 100-500 ms",
		"connection_drop_rate: 0.02",
	}

	for _, val := range expectedChaos {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q, got:\n%s", val, output)
		}
	}
}

// TestValidateMode_RateLimitConfiguration tests rate limit configuration output.
func TestValidateMode_RateLimitConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.RateLimit.Enabled = true
	cfg.RateLimit.RequestsPerSecond = 5000
	cfg.RateLimit.Burst = 7500
	cfg.RateLimit.Algorithm = "sliding_window"

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify rate limit configuration values
	expectedRateLimit := []string{
		"Rate Limit Configuration:",
		"requests_per_second: 5000",
		"burst: 7500",
		"algorithm: sliding_window",
	}

	for _, val := range expectedRateLimit {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q, got:\n%s", val, output)
		}
	}
}

// TestValidateMode_StorageConfiguration tests storage configuration output.
func TestValidateMode_StorageConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.Storage.Enabled = true
	cfg.Storage.RequestsDir = "./data/custom-requests"
	cfg.Storage.MaxFileSize = 209715200
	cfg.Storage.RotateAfter = 5000

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify storage configuration values
	expectedStorage := []string{
		"Storage Configuration:",
		"requests_dir: ./data/custom-requests",
		"max_file_size: 209715200 bytes",
		"rotate_after: 5000 requests",
	}

	for _, val := range expectedStorage {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q, got:\n%s", val, output)
		}
	}
}

// TestValidateMode_ReturnsError tests that RunValidateMode returns appropriate errors.
func TestValidateMode_ReturnsError(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = "/this/path/does/not/exist"

	var buf bytes.Buffer
	err := RunValidateMode(&buf, cfg)

	if err == nil {
		t.Error("Expected error for non-existent scenarios directory")
	}

	if !errors.Is(err, errors.New("configuration validation failed")) {
		// Check that it's a validation error
		if err.Error() != "configuration validation failed" {
			t.Logf("Got error: %v", err)
		}
	}
}

// TestValidateMode_DisabledFeatures tests that disabled features are not shown in output.
func TestValidateMode_DisabledFeatures(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""

	// Ensure all optional features are disabled
	cfg.Metrics.Enabled = false
	cfg.Chaos.Enabled = false
	cfg.Storage.Enabled = false
	cfg.RateLimit.Enabled = false

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// These sections should NOT appear when features are disabled
	unexpectedSections := []string{
		"Chaos Configuration:",
		"Storage Configuration:",
		"Rate Limit Configuration:",
	}

	for _, section := range unexpectedSections {
		if bytes.Contains([]byte(output), []byte(section)) {
			t.Errorf("Unexpected section %q in output when feature is disabled", section)
		}
	}
}

// TestValidateMode_LoggingConfiguration tests logging configuration output.
func TestValidateMode_LoggingConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.Logging.Level = "debug"
	cfg.Logging.Format = "text"
	cfg.Logging.Output = "/var/log/icap.log"
	cfg.Logging.MaxSize = 200
	cfg.Logging.MaxBackups = 10
	cfg.Logging.MaxAge = 60

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify logging configuration values
	expectedLogging := []string{
		"Logging Configuration:",
		"level: debug",
		"format: text",
		"output: /var/log/icap.log",
		"max_size: 200 MB",
		"max_backups: 10",
		"max_age: 60 days",
	}

	for _, val := range expectedLogging {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q, got:\n%s", val, output)
		}
	}
}

// TestValidateMode_HealthConfiguration tests health configuration output.
func TestValidateMode_HealthConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		checkShown  []string
		checkHidden []string
		enabled     bool
	}{
		{
			name:    "health enabled",
			enabled: true,
			checkShown: []string{
				"Health Configuration:",
				"enabled: true",
				"port: 8080",
				"health_path: /health",
				"ready_path: /ready",
			},
			checkHidden: []string{},
		},
		{
			name:    "health disabled",
			enabled: false,
			checkShown: []string{
				"Health Configuration:",
				"enabled: false",
			},
			checkHidden: []string{
				"health_path:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetDefaults()
			cfg.Mock.ScenariosDir = ""
			cfg.Health.Enabled = tt.enabled

			var buf bytes.Buffer

			_ = RunValidateMode(&buf, cfg)

			output := buf.String()

			for _, val := range tt.checkShown {
				if !bytes.Contains([]byte(output), []byte(val)) {
					t.Errorf("Expected output to contain %q", val)
				}
			}

			for _, val := range tt.checkHidden {
				if bytes.Contains([]byte(output), []byte(val)) {
					t.Errorf("Unexpected output containing %q", val)
				}
			}
		})
	}
}

// TestValidateMode_MetricsConfiguration tests metrics configuration output.
func TestValidateMode_MetricsConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.Metrics.Enabled = true
	cfg.Metrics.Host = "127.0.0.1"
	cfg.Metrics.Port = 9091
	cfg.Metrics.Path = "/custom-metrics"

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify metrics configuration values
	expectedMetrics := []string{
		"Metrics Configuration:",
		"enabled: true",
		"host: 127.0.0.1",
		"port: 9091",
		"path: /custom-metrics",
	}

	for _, val := range expectedMetrics {
		if !bytes.Contains([]byte(output), []byte(val)) {
			t.Errorf("Expected output to contain %q, got:\n%s", val, output)
		}
	}
}

// TestValidateMode_ServerBodySize tests body size formatting.
func TestValidateMode_ServerBodySize(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = ""
	cfg.Server.MaxBodySize = 10485760 // 10MB

	var buf bytes.Buffer

	_ = RunValidateMode(&buf, cfg)

	output := buf.String()

	// Verify body size is shown
	if !bytes.Contains([]byte(output), []byte("max_body_size: 10485760 bytes")) {
		t.Errorf("Expected output to contain body size, got:\n%s", output)
	}
}

// TestValidateMode_MultipleScenarioFiles tests counting multiple scenario files.
func TestValidateMode_MultipleScenarioFiles(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	// Create temp directory with multiple scenario files
	tmpDir := t.TempDir()
	scenariosDir := filepath.Join(tmpDir, "scenarios")
	if err := os.Mkdir(scenariosDir, 0755); err != nil {
		t.Fatalf("Failed to create scenarios dir: %v", err)
	}

	// Create multiple scenario files
	for i := 1; i <= 5; i++ {
		fileName := filepath.Join(scenariosDir, "scenario"+string(rune('0'+i))+".yaml")
		if err := os.WriteFile(fileName, []byte("name: test"), 0644); err != nil {
			t.Fatalf("Failed to write scenario file: %v", err)
		}
	}

	cfg.Mock.ScenariosDir = scenariosDir

	var buf bytes.Buffer

	err := RunValidateMode(&buf, cfg)

	output := buf.String()

	// Should pass and show 5 files
	if err != nil {
		t.Errorf("Expected no error with valid scenarios dir, got: %v", err)
	}

	if !bytes.Contains([]byte(output), []byte("5 files found")) {
		t.Errorf("Expected '5 files found' in output, got:\n%s", output)
	}
}
