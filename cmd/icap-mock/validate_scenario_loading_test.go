// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"os"
	"path/filepath"
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

func writeValidationScenarioDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return dir
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
