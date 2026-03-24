// Package main provides tests for CLI flag definitions.
package main

import (
	"testing"
)

// TestBuildInfoDefaults tests that build info variables have expected default values.
func TestBuildInfoDefaults(t *testing.T) {
	if version != "dev" {
		t.Errorf("version = %q, want 'dev'", version)
	}
	if gitCommit != "unknown" {
		t.Errorf("gitCommit = %q, want 'unknown'", gitCommit)
	}
	if buildDate != "unknown" {
		t.Errorf("buildDate = %q, want 'unknown'", buildDate)
	}
}

// TestServerCommand_FlagParsing tests that ServerCommand flags are properly parsed.
func TestServerCommand_FlagParsing(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{"-config", "test.yaml"})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if cmd.configFile != "test.yaml" {
		t.Errorf("configFile = %q, want 'test.yaml'", cmd.configFile)
	}
}

// TestServerCommand_ShortFormConfig tests -c alias for -config.
func TestServerCommand_ShortFormConfig(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{"-c", "test2.yaml"})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if cmd.configFile != "test2.yaml" {
		t.Errorf("configFile = %q, want 'test2.yaml'", cmd.configFile)
	}
}

// TestServerCommand_ValidateFlag tests that validate flag is properly parsed.
func TestServerCommand_ValidateFlagParsing(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{"-validate"})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if !cmd.validateFlag {
		t.Error("validateFlag = false, want true")
	}
}

// TestServerCommand_DebugFlag tests that debug flag is properly parsed.
func TestServerCommand_DebugFlagParsing(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{"-debug"})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if !cmd.debugFlag {
		t.Error("debugFlag = false, want true")
	}
}

// TestServerCommand_ServerFlags tests server-related flags.
func TestServerCommand_ServerFlags(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{
		"-server.host", "localhost",
		"-server.port", "8080",
	})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if cmd.host != "localhost" {
		t.Errorf("host = %q, want 'localhost'", cmd.host)
	}
	if cmd.port != 8080 {
		t.Errorf("port = %d, want 8080", cmd.port)
	}
}

// TestServerCommand_KebabCaseAliases tests that kebab-case aliases work for dot-notation flags.
func TestServerCommand_KebabCaseAliases(t *testing.T) {
	cmd := NewServerCommand()
	err := cmd.Parse([]string{
		"--server-host", "localhost",
		"--server-port", "8080",
		"--logging-level", "debug",
		"--metrics-port", "9091",
	})
	if err != nil {
		t.Fatalf("Failed to parse kebab-case flags: %v", err)
	}
	if cmd.host != "localhost" {
		t.Errorf("host = %q, want 'localhost'", cmd.host)
	}
	if cmd.port != 8080 {
		t.Errorf("port = %d, want 8080", cmd.port)
	}
	if cmd.logLevel != "debug" {
		t.Errorf("logLevel = %q, want 'debug'", cmd.logLevel)
	}
	if cmd.metricsPort != 9091 {
		t.Errorf("metricsPort = %d, want 9091", cmd.metricsPort)
	}
}

// TestServerCommand_KebabCaseBoolFlags tests kebab-case aliases for boolean flags.
func TestServerCommand_KebabCaseBoolFlags(t *testing.T) {
	cmd := NewServerCommand()
	err := cmd.Parse([]string{"--metrics-enabled", "--storage-enabled"})
	if err != nil {
		t.Fatalf("Failed to parse kebab-case bool flags: %v", err)
	}
	if !cmd.metricsEnabled {
		t.Error("metricsEnabled = false, want true")
	}
	if !cmd.storageEnabled {
		t.Error("storageEnabled = false, want true")
	}
	if !cmd.flagWasSet("metrics-enabled") {
		t.Error("flagWasSet('metrics-enabled') = false, want true")
	}
}

// TestServerCommand_MetricsFlags tests metrics-related flags.
func TestServerCommand_MetricsFlags(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{
		"-metrics.enabled",
		"-metrics.host", "127.0.0.1",
		"-metrics.port", "9091",
		"-metrics.path", "/custom-metrics",
	})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if !cmd.metricsEnabled {
		t.Error("metricsEnabled = false, want true")
	}
	if cmd.metricsHost != "127.0.0.1" {
		t.Errorf("metricsHost = %q, want '127.0.0.1'", cmd.metricsHost)
	}
	if cmd.metricsPort != 9091 {
		t.Errorf("metricsPort = %d, want 9091", cmd.metricsPort)
	}
	if cmd.metricsPath != "/custom-metrics" {
		t.Errorf("metricsPath = %q, want '/custom-metrics'", cmd.metricsPath)
	}
}

// TestServerCommand_LoggingFlags tests logging-related flags.
func TestServerCommand_LoggingFlags(t *testing.T) {
	cmd := NewServerCommand()

	err := cmd.Parse([]string{
		"-logging.level", "debug",
		"-logging.format", "text",
		"-logging.output", "/var/log/app.log",
	})
	if err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}
	if cmd.logLevel != "debug" {
		t.Errorf("logLevel = %q, want 'debug'", cmd.logLevel)
	}
	if cmd.logFormat != "text" {
		t.Errorf("logFormat = %q, want 'text'", cmd.logFormat)
	}
	if cmd.logOutput != "/var/log/app.log" {
		t.Errorf("logOutput = %q, want '/var/log/app.log'", cmd.logOutput)
	}
}

// TestServerCommand_ShortFormAliases tests that short form aliases work.
func TestServerCommand_ShortFormAliases(t *testing.T) {
	// Test -p alias for -server.port
	cmd := NewServerCommand()
	if err := cmd.Parse([]string{"-p", "9999"}); err != nil {
		t.Fatalf("Failed to parse -p flag: %v", err)
	}
	if cmd.port != 9999 {
		t.Errorf("-p flag: port = %d, want 9999", cmd.port)
	}

	// Test -l alias for -logging.level
	cmd = NewServerCommand()
	if err := cmd.Parse([]string{"-l", "warn"}); err != nil {
		t.Fatalf("Failed to parse -l flag: %v", err)
	}
	if cmd.logLevel != "warn" {
		t.Errorf("-l flag: logLevel = %q, want 'warn'", cmd.logLevel)
	}
}

// TestServerCommand_FlagDefaults tests that all flag defaults are zero values.
func TestServerCommand_FlagDefaults(t *testing.T) {
	cmd := NewServerCommand()

	// Server flags
	if cmd.host != "" {
		t.Errorf("host default = %q, want empty", cmd.host)
	}
	if cmd.port != 0 {
		t.Errorf("port default = %d, want 0", cmd.port)
	}
	if cmd.maxConns != 0 {
		t.Errorf("maxConns default = %d, want 0", cmd.maxConns)
	}
	if cmd.maxBodySize != 0 {
		t.Errorf("maxBodySize default = %d, want 0", cmd.maxBodySize)
	}
	if cmd.streaming {
		t.Error("streaming default = true, want false")
	}

	// Boolean flags
	if cmd.metricsEnabled {
		t.Error("metricsEnabled default = true, want false")
	}
	if cmd.chaosEnabled {
		t.Error("chaosEnabled default = true, want false")
	}
	if cmd.storageEnabled {
		t.Error("storageEnabled default = true, want false")
	}
	if cmd.rateLimitEnabled {
		t.Error("rateLimitEnabled default = true, want false")
	}
	if cmd.healthEnabled {
		t.Error("healthEnabled default = true, want false")
	}
	if cmd.pluginEnabled {
		t.Error("pluginEnabled default = true, want false")
	}
	if cmd.pprofEnabled {
		t.Error("pprofEnabled default = true, want false")
	}

	// Global flags
	if cmd.configFile != "" {
		t.Errorf("configFile default = %q, want empty", cmd.configFile)
	}
	if cmd.validateFlag {
		t.Error("validateFlag default = true, want false")
	}
	if cmd.versionFlag {
		t.Error("versionFlag default = true, want false")
	}
	if cmd.debugFlag {
		t.Error("debugFlag default = true, want false")
	}
}

// TestServerCommand_FlagWasSet tests the flagWasSet helper.
func TestServerCommand_FlagWasSet(t *testing.T) {
	cmd := NewServerCommand()

	// Before parsing, no flags are set
	if cmd.flagWasSet("metrics.enabled") {
		t.Error("flagWasSet('metrics.enabled') = true before parsing, want false")
	}

	// Parse with --metrics.enabled
	if err := cmd.Parse([]string{"--metrics.enabled"}); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if !cmd.flagWasSet("metrics.enabled") {
		t.Error("flagWasSet('metrics.enabled') = false after parsing, want true")
	}
	if cmd.flagWasSet("server.port") {
		t.Error("flagWasSet('server.port') = true (not parsed), want false")
	}
}
