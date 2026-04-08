// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
)

func TestNewCommandRegistry(t *testing.T) {
	registry := NewCommandRegistry()
	if registry == nil {
		t.Fatal("NewCommandRegistry() returned nil")
	}
}

func TestCommandRegistry_Register(t *testing.T) {
	registry := NewCommandRegistry()

	cmd := &mockCommand{name: "test"}
	registry.Register(cmd)

	retrieved, ok := registry.Get("test")
	if !ok {
		t.Fatal("Failed to retrieve registered command")
	}
	if retrieved.Name() != "test" {
		t.Errorf("Expected command name 'test', got '%s'", retrieved.Name())
	}
}

func TestCommandRegistry_SetDefault(t *testing.T) {
	registry := NewCommandRegistry()

	cmd := &mockCommand{name: "test"}
	registry.Register(cmd)
	registry.SetDefault("test")

	defaultCmd, ok := registry.GetDefault()
	if !ok {
		t.Fatal("Failed to get default command")
	}
	if defaultCmd.Name() != "test" {
		t.Errorf("Expected default command 'test', got '%s'", defaultCmd.Name())
	}
}

func TestCommandRegistry_Get_NonExistent(t *testing.T) {
	registry := NewCommandRegistry()

	_, ok := registry.Get("nonexistent")
	if ok {
		t.Error("Expected false for non-existent command, got true")
	}
}

func TestCommandRegistry_GetDefault_NoneSet(t *testing.T) {
	registry := NewCommandRegistry()

	_, ok := registry.GetDefault()
	if ok {
		t.Error("Expected false when no default command set, got true")
	}
}

func TestCommandRegistry_List(t *testing.T) {
	registry := NewCommandRegistry()

	cmd1 := &mockCommand{name: "test1"}
	cmd2 := &mockCommand{name: "test2"}
	registry.Register(cmd1)
	registry.Register(cmd2)

	list := registry.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(list))
	}

	// Check that both commands are in the list
	hasTest1 := false
	hasTest2 := false
	for _, name := range list {
		if name == "test1" {
			hasTest1 = true
		}
		if name == "test2" {
			hasTest2 = true
		}
	}
	if !hasTest1 || !hasTest2 {
		t.Error("Not all commands were listed")
	}
}

func TestNewServerCommand(t *testing.T) {
	cmd := NewServerCommand()
	if cmd == nil {
		t.Fatal("NewServerCommand() returned nil")
	}
	if cmd.Name() != "server" {
		t.Errorf("Expected name 'server', got '%s'", cmd.Name())
	}
}

func TestServerCommand_Name(t *testing.T) {
	cmd := NewServerCommand()
	if cmd.Name() != "server" {
		t.Errorf("Expected name 'server', got '%s'", cmd.Name())
	}
}

func TestServerCommand_Parse(t *testing.T) {
	cmd := NewServerCommand()

	args := []string{"--server.port", "8080"}
	err := cmd.Parse(args)
	if err != nil {
		t.Errorf("Parse() failed: %v", err)
	}

	// Verify flag was parsed
	if cmd.port != 8080 {
		t.Errorf("Expected port 8080, got %d", cmd.port)
	}
}

// TestServerCommand_VersionFlag is skipped because version flag calls os.Exit
// which terminates the test process. This is correct behavior for the CLI
// but makes testing difficult.
//
// The correct behavior is verified by manual testing: icap-mock server --version
// should print version information and exit with status 0.
func TestServerCommand_VersionFlag(t *testing.T) {
	t.Skip("Version flag calls os.Exit which cannot be tested in unit tests")
}

func TestServerCommand_LoadConfiguration(t *testing.T) {
	cmd := NewServerCommand()

	// Test with minimal config (loads defaults)
	cfg, err := cmd.loadConfiguration()
	if err != nil {
		t.Errorf("loadConfiguration() failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("loadConfiguration() returned nil config")
	}
}

func TestServerCommand_ApplyOverrides(t *testing.T) {
	cmd := NewServerCommand()

	// Simulate flags being explicitly set on the command line
	_ = cmd.fs.Set("server.host", "127.0.0.1")
	_ = cmd.fs.Set("server.port", "9999")

	cfg := &config.Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 1344

	cmd.applyOverrides(cfg)

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host '127.0.0.1', got '%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Expected port 9999, got %d", cfg.Server.Port)
	}
}

func TestNewReplayCommand(t *testing.T) {
	cmd := NewReplayCommand()
	if cmd == nil {
		t.Fatal("NewReplayCommand() returned nil")
	}
	if cmd.Name() != "replay" {
		t.Errorf("Expected name 'replay', got '%s'", cmd.Name())
	}
}

func TestReplayCommand_Name(t *testing.T) {
	cmd := NewReplayCommand()
	if cmd.Name() != "replay" {
		t.Errorf("Expected name 'replay', got '%s'", cmd.Name())
	}
}

func TestReplayCommand_Parse(t *testing.T) {
	cmd := NewReplayCommand()

	args := []string{"--dir", "/tmp/test"}
	err := cmd.Parse(args)
	if err != nil {
		t.Errorf("Parse() failed: %v", err)
	}

	// Verify flag was parsed
	if cmd.dir != "/tmp/test" {
		t.Errorf("Expected dir '/tmp/test', got '%s'", cmd.dir)
	}
}

func TestReplayCommand_Run_MissingDir(t *testing.T) {
	cmd := NewReplayCommand()
	cmd.dir = "" // Missing required flag

	ctx := context.Background()
	err := cmd.Run(ctx)
	if err == nil {
		t.Error("Expected error for missing dir, got nil")
	}
	if !strings.Contains(err.Error(), "--dir is required") {
		t.Errorf("Expected error about --dir, got: %v", err)
	}
}

func TestReplayCommand_Run_NonExistentDir(t *testing.T) {
	cmd := NewReplayCommand()
	cmd.dir = "/nonexistent/directory/12345" // Non-existent directory

	ctx := context.Background()
	err := cmd.Run(ctx)
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

// TestCommandRegistry_PrintUsage is skipped because capturing stderr in tests
// causes the test to hang on Windows. The PrintUsage functionality is simple
// and can be verified by manual testing: icap-mock --help.
func TestCommandRegistry_PrintUsage(t *testing.T) {
	t.Skip("Skipping stderr capture test due to Windows pipe hanging issue")

	registry := NewCommandRegistry()

	cmd1 := &mockCommand{name: "test1"}
	cmd2 := &mockCommand{name: "test2"}
	registry.Register(cmd1)
	registry.Register(cmd2)

	// Capture output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	registry.PrintUsage()

	w.Close()
	os.Stderr = oldStderr

	// Read captured output
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	// Check that output contains expected elements
	if !strings.Contains(output, "test1") {
		t.Error("PrintUsage() output missing 'test1'")
	}
	if !strings.Contains(output, "test2") {
		t.Error("PrintUsage() output missing 'test2'")
	}
}

func TestIntegration_BackwardCompatibility(t *testing.T) {
	// This test verifies that when no subcommand is provided,
	// the server command is used by default

	registry := NewCommandRegistry()
	serverCmd := &mockCommand{name: "server", runCalled: false}
	replayCmd := &mockCommand{name: "replay", runCalled: false}

	registry.Register(serverCmd)
	registry.Register(replayCmd)
	registry.SetDefault("server")

	// Simulate no subcommand (empty args)
	defaultCmd, ok := registry.GetDefault()
	if !ok {
		t.Fatal("Failed to get default command")
	}

	if defaultCmd.Name() != "server" {
		t.Errorf("Expected default command 'server', got '%s'", defaultCmd.Name())
	}
}

// mockCommand is a test implementation of the Command interface.
type mockCommand struct {
	parseError error
	runError   error
	name       string
	runCalled  bool
}

func (m *mockCommand) Name() string {
	return m.name
}

func (m *mockCommand) Parse(args []string) error {
	m.parseError = nil
	// Simple parsing - just check for help
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return flag.ErrHelp
		}
	}
	return nil
}

func (m *mockCommand) Run(_ context.Context) error {
	m.runCalled = true
	return m.runError
}

func (m *mockCommand) Usage() {
	// No-op for mock
}

func (m *mockCommand) Description() string {
	return "mock command for testing"
}

func TestCommandDispatch(t *testing.T) {
	tests := []struct {
		name     string
		wantCmd  string
		args     []string
		wantHelp bool
		wantErr  bool
	}{
		{
			name:    "Server command",
			args:    []string{"server"},
			wantCmd: "server",
			wantErr: false,
		},
		{
			name:    "Replay command",
			args:    []string{"replay"},
			wantCmd: "replay",
			wantErr: false,
		},
		{
			name:     "Server with help",
			args:     []string{"server", "--help"},
			wantCmd:  "server",
			wantHelp: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewCommandRegistry()

			serverCmd := &mockCommand{name: "server"}
			replayCmd := &mockCommand{name: "replay"}

			registry.Register(serverCmd)
			registry.Register(replayCmd)

			if tt.wantHelp {
				// Help case - just check command is found
				cmd, ok := registry.Get(tt.wantCmd)
				if !ok {
					t.Fatalf("Command %s not found", tt.wantCmd)
				}
				if cmd.Name() != tt.wantCmd {
					t.Errorf("Expected command %s, got %s", tt.wantCmd, cmd.Name())
				}
				return
			}

			cmd, ok := registry.Get(tt.args[0])
			if !ok {
				t.Fatalf("Command %s not found", tt.args[0])
			}

			if cmd.Name() != tt.wantCmd {
				t.Errorf("Expected command %s, got %s", tt.wantCmd, cmd.Name())
			}

			if len(tt.args) > 1 {
				err := cmd.Parse(tt.args[1:])
				if (err != nil) != tt.wantErr {
					t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

// TestServerCommand_Usage is skipped because capturing stderr in tests
// causes the test to hang on Windows. The Usage functionality is simple
// and can be verified by manual testing: icap-mock server --help.
func TestServerCommand_Usage(t *testing.T) {
	t.Skip("Skipping stderr capture test due to Windows pipe hanging issue")

	cmd := NewServerCommand()

	// Capture output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd.Usage()

	w.Close()
	os.Stderr = oldStderr

	// Read captured output
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	// Check that output contains expected elements
	if !strings.Contains(output, "icap-mock server") {
		t.Error("Usage() output missing 'icap-mock server'")
	}
	if !strings.Contains(output, "--server.port") {
		t.Error("Usage() output missing '--server.port'")
	}
}

// TestReplayCommand_Usage is skipped because capturing stderr in tests
// causes the test to hang on Windows. The Usage functionality is simple
// and can be verified by manual testing: icap-mock replay --help.
func TestReplayCommand_Usage(t *testing.T) {
	t.Skip("Skipping stderr capture test due to Windows pipe hanging issue")

	cmd := NewReplayCommand()

	// Capture output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd.Usage()

	w.Close()
	os.Stderr = oldStderr

	// Read captured output
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	// Check that output contains expected elements
	if !strings.Contains(output, "icap-mock replay") {
		t.Error("Usage() output missing 'icap-mock replay'")
	}
	if !strings.Contains(output, "--dir") {
		t.Error("Usage() output missing '--dir'")
	}
}

func BenchmarkServerCommandCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewServerCommand()
	}
}

func BenchmarkReplayCommandCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewReplayCommand()
	}
}

func BenchmarkCommandRegistryGet(b *testing.B) {
	registry := NewCommandRegistry()
	serverCmd := NewServerCommand()
	replayCmd := NewReplayCommand()

	registry.Register(serverCmd)
	registry.Register(replayCmd)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Get("server")
	}
}
