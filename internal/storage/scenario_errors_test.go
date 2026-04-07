// Copyright 2026 ICAP Mock

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScenarioError_Error tests the ScenarioError.Error() method.
func TestScenarioError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ScenarioError
		contains string
	}{
		{
			name: "full error",
			err: &ScenarioError{
				Operation:    "load",
				FilePath:     "/path/to/scenarios.yaml",
				ScenarioName: "test-scenario",
				Field:        "match.path_pattern",
				Value:        "[invalid",
				Message:      "invalid regex pattern",
				Suggestion:   "fix the regex syntax",
				Cause:        errors.New("underlying error"),
			},
			contains: "operation: load",
		},
		{
			name: "error with cause only",
			err: &ScenarioError{
				Operation: "validate",
				Message:   "scenario name is required",
				Cause:     errors.New("some cause"),
			},
			contains: "cause: some cause",
		},
		{
			name: "minimal error",
			err: &ScenarioError{
				Message: "something went wrong",
			},
			contains: "error: something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if !strings.Contains(errStr, tt.contains) {
				t.Errorf("Error() = %q, want to contain %q", errStr, tt.contains)
			}
		})
	}
}

// TestScenarioError_Unwrap tests the ScenarioError.Unwrap() method.
func TestScenarioError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &ScenarioError{
		Message: "test",
		Cause:   cause,
	}

	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, cause) {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Test nil case
	var nilErr *ScenarioError
	if nilErr.Unwrap() != nil {
		t.Error("Unwrap() on nil should return nil")
	}
}

// TestNewScenarioLoadError tests NewScenarioLoadError function.
func TestNewScenarioLoadError(t *testing.T) {
	cause := os.ErrNotExist
	err := NewScenarioLoadError("/path/to/file.yaml", cause)

	if err.Operation != "load" {
		t.Errorf("Operation = %q, want load", err.Operation)
	}
	if err.FilePath != "/path/to/file.yaml" {
		t.Errorf("FilePath = %q, want /path/to/file.yaml", err.FilePath)
	}
	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if !strings.Contains(err.Suggestion, "verify") {
		t.Errorf("Suggestion should contain 'verify', got %q", err.Suggestion)
	}
}

// TestNewScenarioParseError tests NewScenarioParseError function.
func TestNewScenarioParseError(t *testing.T) {
	cause := errors.New("yaml: line 5: invalid indentation")
	err := NewScenarioParseError("/path/to/file.yaml", cause)

	if err.Operation != "parse" {
		t.Errorf("Operation = %q, want parse", err.Operation)
	}
	if err.FilePath != "/path/to/file.yaml" {
		t.Errorf("FilePath = %q, want /path/to/file.yaml", err.FilePath)
	}
	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if !strings.Contains(err.Suggestion, "YAML") {
		t.Errorf("Suggestion should contain 'YAML', got %q", err.Suggestion)
	}
}

// TestNewScenarioValidationError tests NewScenarioValidationError function.
func TestNewScenarioValidationError(t *testing.T) {
	err := NewScenarioValidationError(
		"/path/to/file.yaml",
		"test-scenario",
		"match.path_pattern",
		"[invalid",
		"invalid regex pattern",
		"fix the regex syntax",
	)

	if err.Operation != "validate" {
		t.Errorf("Operation = %q, want validate", err.Operation)
	}
	if err.FilePath != "/path/to/file.yaml" {
		t.Errorf("FilePath = %q, want /path/to/file.yaml", err.FilePath)
	}
	if err.ScenarioName != "test-scenario" {
		t.Errorf("ScenarioName = %q, want test-scenario", err.ScenarioName)
	}
	if err.Field != "match.path_pattern" {
		t.Errorf("Field = %q, want match.path_pattern", err.Field)
	}
	if err.Value != "[invalid" {
		t.Errorf("Value = %q, want [invalid", err.Value)
	}
	if err.Message != "invalid regex pattern" {
		t.Errorf("Message = %q, want 'invalid regex pattern'", err.Message)
	}
	if err.Suggestion != "fix the regex syntax" {
		t.Errorf("Suggestion = %q, want 'fix the regex syntax'", err.Suggestion)
	}
}

// TestNewScenarioRegexError tests NewScenarioRegexError function.
func TestNewScenarioRegexError(t *testing.T) {
	cause := errors.New("error parsing regexp")
	err := NewScenarioRegexError(
		"/path/to/file.yaml",
		"test-scenario",
		"match.path_pattern",
		"[invalid(regex",
		cause,
	)

	if err.Operation != "compile" {
		t.Errorf("Operation = %q, want compile", err.Operation)
	}
	if err.FilePath != "/path/to/file.yaml" {
		t.Errorf("FilePath = %q, want /path/to/file.yaml", err.FilePath)
	}
	if err.ScenarioName != "test-scenario" {
		t.Errorf("ScenarioName = %q, want test-scenario", err.ScenarioName)
	}
	if err.Field != "match.path_pattern" {
		t.Errorf("Field = %q, want match.path_pattern", err.Field)
	}
	if err.Value != "[invalid(regex" {
		t.Errorf("Value = %q, want [invalid(regex", err.Value)
	}
	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if !strings.Contains(err.Suggestion, "regex") {
		t.Errorf("Suggestion should contain 'regex', got %q", err.Suggestion)
	}
}

// TestNewScenarioBodyFileError tests NewScenarioBodyFileError function.
func TestNewScenarioBodyFileError(t *testing.T) {
	cause := os.ErrNotExist
	err := NewScenarioBodyFileError(
		"/path/to/file.yaml",
		"test-scenario",
		"/path/to/body.txt",
		cause,
	)

	if err.Operation != "load_body" {
		t.Errorf("Operation = %q, want load_body", err.Operation)
	}
	if err.FilePath != "/path/to/file.yaml" {
		t.Errorf("FilePath = %q, want /path/to/file.yaml", err.FilePath)
	}
	if err.ScenarioName != "test-scenario" {
		t.Errorf("ScenarioName = %q, want test-scenario", err.ScenarioName)
	}
	if err.Field != "response.body_file" {
		t.Errorf("Field = %q, want response.body_file", err.Field)
	}
	if err.Value != "/path/to/body.txt" {
		t.Errorf("Value = %q, want /path/to/body.txt", err.Value)
	}
	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
}

// TestNewScenarioMatchError tests NewScenarioMatchError function.
func TestNewScenarioMatchError(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("body read failed")
		err := NewScenarioMatchError("failed to match request body", cause)

		if err.Operation != "match" {
			t.Errorf("Operation = %q, want match", err.Operation)
		}
		if err.Message != "failed to match request body" {
			t.Errorf("Message = %q, want 'failed to match request body'", err.Message)
		}
		if !errors.Is(err.Cause, cause) {
			t.Errorf("Cause = %v, want %v", err.Cause, cause)
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := NewScenarioMatchError("no matching scenario", nil)

		if err.Operation != "match" {
			t.Errorf("Operation = %q, want match", err.Operation)
		}
		if err.Message != "no matching scenario" {
			t.Errorf("Message = %q, want 'no matching scenario'", err.Message)
		}
		if err.Suggestion == "" {
			t.Error("Suggestion should not be empty")
		}
	})
}

// TestFormatError tests FormatError function.
func TestFormatError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if FormatError(nil) != "" {
			t.Errorf("FormatError(nil) = %q, want empty string", FormatError(nil))
		}
	})

	t.Run("standard error", func(t *testing.T) {
		err := errors.New("standard error")
		if FormatError(err) != "standard error" {
			t.Errorf("FormatError() = %q, want 'standard error'", FormatError(err))
		}
	})

	t.Run("scenario error", func(t *testing.T) {
		err := &ScenarioError{
			Operation:    "load",
			FilePath:     "/test/scenarios.yaml",
			ScenarioName: "test-scenario",
			Field:        "match.path_pattern",
			Value:        "[invalid",
			Message:      "invalid regex pattern",
			Suggestion:   "fix the regex syntax",
		}

		formatted := FormatError(err)
		if !strings.Contains(formatted, "Operation:") {
			t.Error("Formatted output should contain Operation:")
		}
		if !strings.Contains(formatted, "File:") {
			t.Error("Formatted output should contain File:")
		}
		if !strings.Contains(formatted, "Scenario:") {
			t.Error("Formatted output should contain Scenario:")
		}
		if !strings.Contains(formatted, "Field:") {
			t.Error("Formatted output should contain Field:")
		}
		if !strings.Contains(formatted, "Value:") {
			t.Error("Formatted output should contain Value:")
		}
		if !strings.Contains(formatted, "Error:") {
			t.Error("Formatted output should contain Error:")
		}
		if !strings.Contains(formatted, "Suggestion:") {
			t.Error("Formatted output should contain Suggestion:")
		}
	})
}

// TestIsScenarioError tests IsScenarioError function.
func TestIsScenarioError(t *testing.T) {
	t.Run("scenario error", func(t *testing.T) {
		err := &ScenarioError{Message: "test"}
		if !IsScenarioError(err) {
			t.Error("IsScenarioError should return true for ScenarioError")
		}
	})

	t.Run("standard error", func(t *testing.T) {
		err := errors.New("standard error")
		if IsScenarioError(err) {
			t.Error("IsScenarioError should return false for standard error")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		if IsScenarioError(nil) {
			t.Error("IsScenarioError should return false for nil")
		}
	})
}

// TestScenarioRegistry_Load_DetailedError tests that Load returns detailed errors.
func TestScenarioRegistry_Load_DetailedError(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		registry := NewScenarioRegistry()
		err := registry.Load("/nonexistent/path/to/scenarios.yaml")

		if err == nil {
			t.Fatal("Load() should return error for nonexistent file")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Load() should return ScenarioError")
		}

		if se.Operation != "load" {
			t.Errorf("Operation = %q, want 'load'", se.Operation)
		}
		if se.FilePath != "/nonexistent/path/to/scenarios.yaml" {
			t.Errorf("FilePath = %q, want '/nonexistent/path/to/scenarios.yaml'", se.FilePath)
		}
		if !strings.Contains(se.Suggestion, "verify") {
			t.Errorf("Suggestion should mention verifying file, got %q", se.Suggestion)
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "invalid.yaml")

		if err := os.WriteFile(scenarioFile, []byte("invalid: yaml: content: ["), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		registry := NewScenarioRegistry()
		err := registry.Load(scenarioFile)

		if err == nil {
			t.Fatal("Load() should return error for invalid YAML")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Load() should return ScenarioError")
		}

		if se.Operation != "parse" {
			t.Errorf("Operation = %q, want 'parse'", se.Operation)
		}
		if !strings.Contains(se.Suggestion, "YAML") {
			t.Errorf("Suggestion should mention YAML syntax, got %q", se.Suggestion)
		}
	})

	t.Run("invalid regex pattern", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "invalid_regex.yaml")

		yamlContent := `
scenarios:
  - name: "invalid-regex"
    priority: 100
    match:
      path_pattern: "[invalid(regex"
    response:
      icap_status: 204
`
		if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		registry := NewScenarioRegistry()
		err := registry.Load(scenarioFile)

		if err == nil {
			t.Fatal("Load() should return error for invalid regex")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Load() should return ScenarioError")
		}

		if se.Operation != "compile" {
			t.Errorf("Operation = %q, want 'compile'", se.Operation)
		}
		if se.ScenarioName != "invalid-regex" {
			t.Errorf("ScenarioName = %q, want 'invalid-regex'", se.ScenarioName)
		}
		if se.Field != "match.path_pattern" {
			t.Errorf("Field = %q, want 'match.path_pattern'", se.Field)
		}
		if !strings.Contains(se.Suggestion, "regex") {
			t.Errorf("Suggestion should mention regex, got %q", se.Suggestion)
		}
	})

	t.Run("missing scenario name", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "no_name.yaml")

		yamlContent := `
scenarios:
  - priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 204
`
		if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		registry := NewScenarioRegistry()
		err := registry.Load(scenarioFile)

		if err == nil {
			t.Fatal("Load() should return error for missing name")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Load() should return ScenarioError")
		}

		if se.Operation != "validate" {
			t.Errorf("Operation = %q, want 'validate'", se.Operation)
		}
		if se.Field != "name" {
			t.Errorf("Field = %q, want 'name'", se.Field)
		}
		if !strings.Contains(se.Suggestion, "name") {
			t.Errorf("Suggestion should mention name, got %q", se.Suggestion)
		}
	})

	t.Run("body file not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "missing_body.yaml")

		yamlContent := `
scenarios:
  - name: "missing-body-file"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
      body_file: "/nonexistent/body.txt"
`
		if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		registry := NewScenarioRegistry()
		err := registry.Load(scenarioFile)

		if err == nil {
			t.Fatal("Load() should return error for missing body file")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Load() should return ScenarioError")
		}

		if se.Operation != "load_body" {
			t.Errorf("Operation = %q, want 'load_body'", se.Operation)
		}
		if se.ScenarioName != "missing-body-file" {
			t.Errorf("ScenarioName = %q, want 'missing-body-file'", se.ScenarioName)
		}
		if se.Field != "response.body_file" {
			t.Errorf("Field = %q, want 'response.body_file'", se.Field)
		}
	})
}

// TestScenarioRegistry_Match_DetailedError tests that Match returns detailed errors.
func TestScenarioRegistry_Match_DetailedError(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		registry := NewScenarioRegistry()
		_, err := registry.Match(nil)

		if err == nil {
			t.Fatal("Match() should return error for nil request")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Match() should return ScenarioError")
		}

		if se.Operation != "match" {
			t.Errorf("Operation = %q, want 'match'", se.Operation)
		}
	})

	t.Run("no match with details", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		yamlContent := `
scenarios:
  - name: "specific-match"
    priority: 100
    match:
      icap_method: "REQMOD"
      http_method: "POST"
    response:
      icap_status: 204
`
		if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		registry := NewScenarioRegistry()
		if err := registry.Load(scenarioFile); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Remove the default scenario so we can test the no-match case
		registry.Remove("default")

		// Request that won't match (GET instead of POST)
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/reqmod",
			HTTPRequest: &icap.HTTPMessage{
				Method: "GET",
				URI:    "http://example.com/page",
			},
		}

		_, err := registry.Match(req)

		if err == nil {
			t.Fatal("Match() should return error when no scenario matches")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Match() should return ScenarioError")
		}

		if se.Operation != "match" {
			t.Errorf("Operation = %q, want 'match'", se.Operation)
		}
		if !strings.Contains(se.Message, "checked") {
			t.Errorf("Message should mention how many scenarios were checked, got %q", se.Message)
		}
		if !strings.Contains(se.Suggestion, "add a scenario") {
			t.Errorf("Suggestion should mention adding a scenario, got %q", se.Suggestion)
		}
	})
}

// TestScenarioRegistry_Add_DetailedError tests that Add returns detailed errors.
func TestScenarioRegistry_Add_DetailedError(t *testing.T) {
	t.Run("nil scenario", func(t *testing.T) {
		registry := NewScenarioRegistry()
		err := registry.Add(nil)

		if err == nil {
			t.Fatal("Add() should return error for nil scenario")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Add() should return ScenarioError")
		}

		if se.Operation != "add" {
			t.Errorf("Operation = %q, want 'add'", se.Operation)
		}
	})

	t.Run("invalid regex in scenario", func(t *testing.T) {
		registry := NewScenarioRegistry()
		scenario := &Scenario{
			Name: "test-scenario",
			Match: MatchRule{
				Path: "[invalid(regex",
			},
			Response: ResponseTemplate{
				ICAPStatus: 204,
			},
			Priority: 50,
		}

		err := registry.Add(scenario)

		if err == nil {
			t.Fatal("Add() should return error for invalid regex")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("Add() should return ScenarioError")
		}

		if se.Operation != "add" {
			t.Errorf("Operation = %q, want 'add'", se.Operation)
		}
		if se.ScenarioName != "test-scenario" {
			t.Errorf("ScenarioName = %q, want 'test-scenario'", se.ScenarioName)
		}
		if se.Field != "match.path_pattern" {
			t.Errorf("Field = %q, want 'match.path_pattern'", se.Field)
		}
	})
}

// TestResponseTemplate_GetBody_DetailedError tests GetBody error handling.
func TestResponseTemplate_GetBody_DetailedError(t *testing.T) {
	t.Run("missing body file returns detailed error", func(t *testing.T) {
		rt := ResponseTemplate{
			BodyFile: "/nonexistent/body.txt",
		}

		_, err := rt.GetBody()

		if err == nil {
			t.Fatal("GetBody() should return error for missing file")
		}

		var se *ScenarioError
		if !AsScenarioError(err, &se) {
			t.Fatal("GetBody() should return ScenarioError")
		}

		if se.Operation != "load_body" {
			t.Errorf("Operation = %q, want 'load_body'", se.Operation)
		}
		if se.Field != "response.body_file" {
			t.Errorf("Field = %q, want 'response.body_file'", se.Field)
		}
		if se.Value != "/nonexistent/body.txt" {
			t.Errorf("Value = %q, want '/nonexistent/body.txt'", se.Value)
		}
	})
}
