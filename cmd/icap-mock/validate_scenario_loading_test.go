// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
)

func TestValidateMode_LoadsScenarioFiles(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.ScenariosDir = writeValidationScenarioDir(t, invalidStreamScenarioYAML())

	var buf bytes.Buffer
	err := RunValidateMode(&buf, cfg)

	if err == nil {
		t.Fatal("RunValidateMode() error = nil, want validation failure")
	}
	if !bytes.Contains(buf.Bytes(), []byte("scenarios validation failed")) {
		t.Fatalf("validation output missing scenario error:\n%s", buf.String())
	}
}

func TestValidateScenariosCommand_UsesRuntimeStreamValidation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invalid-stream.yaml")
	if err := os.WriteFile(path, []byte(invalidStreamScenarioYAML()), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if validateFile(path, map[string]string{}) {
		t.Fatal("validateFile() = true, want false")
	}
}

func TestServerCommandValidate_AllowsDuplicateScenarioNamesAcrossServers(t *testing.T) {
	root := t.TempDir()
	firstDir := writeValidationScenarioDirAt(t, root, "first", validValidationScenarioYAML("shared-name"))
	secondDir := writeValidationScenarioDirAt(t, root, "second", validValidationScenarioYAML("shared-name"))
	configPath := writeMultiServerValidationConfig(t, root, firstDir, secondDir)

	cmd := NewServerCommand()
	if err := cmd.Parse([]string{"--validate", "-c", configPath}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if err := cmd.Run(t.Context()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestValidateMode_MultiServerRejectsDuplicateNamesWithinServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeValidationScenarioFile(t, dir, "one.yaml", validValidationScenarioYAML("shared-name"))
	writeValidationScenarioFile(t, dir, "two.yaml", validValidationScenarioYAML("shared-name"))
	cfg := multiServerValidationConfig(dir)

	var buf bytes.Buffer
	err := RunValidateMode(&buf, cfg)

	if err == nil {
		t.Fatal("RunValidateMode() error = nil, want validation failure")
	}
	if !bytes.Contains(buf.Bytes(), []byte("duplicate name")) {
		t.Fatalf("validation output missing duplicate error:\n%s", buf.String())
	}
}

func TestValidateMode_MultiServerRejectsMissingScenarioDirectory(t *testing.T) {
	t.Parallel()
	cfg := multiServerValidationConfig(filepath.Join(t.TempDir(), "missing"))

	var buf bytes.Buffer
	err := RunValidateMode(&buf, cfg)

	if err == nil {
		t.Fatal("RunValidateMode() error = nil, want validation failure")
	}
	if !bytes.Contains(buf.Bytes(), []byte("scenarios directory not found")) {
		t.Fatalf("validation output missing missing-directory warning:\n%s", buf.String())
	}
}

func TestValidateMode_MultiServerRejectsInvalidScenario(t *testing.T) {
	t.Parallel()
	cfg := multiServerValidationConfig(writeValidationScenarioDir(t, invalidStreamScenarioYAML()))

	var buf bytes.Buffer
	err := RunValidateMode(&buf, cfg)

	if err == nil {
		t.Fatal("RunValidateMode() error = nil, want validation failure")
	}
	if !bytes.Contains(buf.Bytes(), []byte("scenarios validation failed")) {
		t.Fatalf("validation output missing scenario validation error:\n%s", buf.String())
	}
}

func writeValidationScenarioDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeValidationScenarioFile(t, dir, "scenario.yaml", content)
	return dir
}

func writeValidationScenarioDirAt(t *testing.T, root, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	writeValidationScenarioFile(t, dir, "scenario.yaml", content)
	return dir
}

func writeValidationScenarioFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeMultiServerValidationConfig(t *testing.T, root, firstDir, secondDir string) string {
	t.Helper()
	configPath := filepath.Join(root, "config.yaml")
	content := strings.Join([]string{
		"servers:",
		"  first:",
		"    port: 1344",
		"    scenarios_dir: " + firstDir,
		"  second:",
		"    port: 1345",
		"    scenarios_dir: " + secondDir,
		"metrics:",
		"  enabled: false",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return configPath
}

func multiServerValidationConfig(scenariosDir string) *config.Config {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Servers = map[string]config.ServerEntryConfig{
		"first": {
			Port:         1344,
			ScenariosDir: scenariosDir,
		},
	}
	return cfg
}

func validValidationScenarioYAML(name string) string {
	return "defaults:\n  method: REQMOD\n  endpoint: /scan\nscenarios:\n  " + name + ":\n    status: 204\n"
}

func invalidStreamScenarioYAML() string {
	return `defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  invalid-stream:
    status: 200
    stream:
      source: { from: body, body: data }
      send:
        percent: 0%
        duration: 1ms
      end:
        mode: fin
`
}
