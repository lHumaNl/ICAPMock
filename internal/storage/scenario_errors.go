// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"errors"
	"fmt"
	"strings"
)

// ScenarioError represents a detailed error that occurred during scenario
// operations such as loading, validation, or matching.
// It provides rich context to help diagnose and fix configuration issues.
type ScenarioError struct {
	// Operation is the operation that failed (e.g., "load", "validate", "match").
	Operation string

	// FilePath is the path to the scenario file, if applicable.
	FilePath string

	// ScenarioName is the name of the scenario that caused the error.
	ScenarioName string

	// Field is the specific field that has an issue (e.g., "match.path_pattern").
	Field string

	// Value is the invalid value that was provided.
	Value interface{}

	// Message describes what went wrong.
	Message string

	// Suggestion provides guidance on how to fix the issue.
	Suggestion string

	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface and returns a formatted error string.
// The format includes all available context in a human-readable format.
func (e *ScenarioError) Error() string {
	var parts []string

	// Build the main error message
	if e.Operation != "" {
		parts = append(parts, fmt.Sprintf("operation: %s", e.Operation))
	}
	if e.FilePath != "" {
		parts = append(parts, fmt.Sprintf("file: %s", e.FilePath))
	}
	if e.ScenarioName != "" {
		parts = append(parts, fmt.Sprintf("scenario: %q", e.ScenarioName))
	}
	if e.Field != "" {
		parts = append(parts, fmt.Sprintf("field: %s", e.Field))
	}

	// Add the main message
	if e.Message != "" {
		parts = append(parts, fmt.Sprintf("error: %s", e.Message))
	}

	// Add value if present and not nil
	if e.Value != nil {
		parts = append(parts, fmt.Sprintf("value: %v", e.Value))
	}

	// Add cause if present
	if e.Cause != nil {
		parts = append(parts, fmt.Sprintf("cause: %v", e.Cause))
	}

	// Add suggestion if present
	if e.Suggestion != "" {
		parts = append(parts, fmt.Sprintf("suggestion: %s", e.Suggestion))
	}

	return strings.Join(parts, ", ")
}

// Unwrap returns the underlying cause of the error.
// This enables errors.Unwrap() and errors.Is() support.
func (e *ScenarioError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewScenarioLoadError creates an error for scenario file loading failures.
func NewScenarioLoadError(filePath string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:  "load",
		FilePath:   filePath,
		Message:    "failed to load scenario file",
		Cause:      cause,
		Suggestion: "verify the file exists and is readable",
	}
}

// NewScenarioParseError creates an error for YAML parsing failures.
func NewScenarioParseError(filePath string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:  "parse",
		FilePath:   filePath,
		Message:    "failed to parse YAML content",
		Cause:      cause,
		Suggestion: "check YAML syntax: ensure proper indentation, quotes around special characters, and valid structure",
	}
}

// NewScenarioValidationError creates an error for scenario validation failures.
func NewScenarioValidationError(filePath, scenarioName, field string, value interface{}, message, suggestion string) *ScenarioError {
	return &ScenarioError{
		Operation:    "validate",
		FilePath:     filePath,
		ScenarioName: scenarioName,
		Field:        field,
		Value:        value,
		Message:      message,
		Suggestion:   suggestion,
	}
}

// NewScenarioRegexError creates an error for regex compilation failures.
func NewScenarioRegexError(filePath, scenarioName, field, pattern string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:    "compile",
		FilePath:     filePath,
		ScenarioName: scenarioName,
		Field:        field,
		Value:        pattern,
		Message:      fmt.Sprintf("invalid regular expression: %v", cause),
		Cause:        cause,
		Suggestion:   "use a valid regex pattern; consider using regex testing tools to validate",
	}
}

// NewScenarioBodyFileError creates an error for body file loading failures.
func NewScenarioBodyFileError(filePath, scenarioName, bodyFilePath string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:    "load_body",
		FilePath:     filePath,
		ScenarioName: scenarioName,
		Field:        "response.body_file",
		Value:        bodyFilePath,
		Message:      fmt.Sprintf("failed to load body file: %v", cause),
		Cause:        cause,
		Suggestion:   "ensure the body file path is correct and the file is readable",
	}
}

// NewScenarioCIDRError creates an error for CIDR validation failures.
func NewScenarioCIDRError(filePath, scenarioName, cidrRange string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:    "validate",
		FilePath:     filePath,
		ScenarioName: scenarioName,
		Field:        "match.cidr_ranges",
		Value:        cidrRange,
		Message:      fmt.Sprintf("invalid CIDR range: %v", cause),
		Cause:        cause,
		Suggestion:   "use a valid CIDR notation, e.g., '192.168.1.0/24' or '10.0.0.0/8'",
	}
}

// NewScenarioMatchError creates an error for scenario matching failures.
func NewScenarioMatchError(message string, cause error) *ScenarioError {
	suggestion := "check that at least one scenario matches the request criteria"
	if cause != nil {
		suggestion = fmt.Sprintf("resolve the underlying issue: %v", cause)
	}
	return &ScenarioError{
		Operation:  "match",
		Message:    message,
		Cause:      cause,
		Suggestion: suggestion,
	}
}

// FormatError formats an error with additional context for display.
// It returns a multi-line string with detailed error information.
func FormatError(err error) string {
	if err == nil {
		return ""
	}

	var se *ScenarioError
	if AsScenarioError(err, &se) {
		var sb strings.Builder

		sb.WriteString("=== Scenario Error ===\n")
		if se.Operation != "" {
			sb.WriteString(fmt.Sprintf("  Operation:   %s\n", se.Operation))
		}
		if se.FilePath != "" {
			sb.WriteString(fmt.Sprintf("  File:        %s\n", se.FilePath))
		}
		if se.ScenarioName != "" {
			sb.WriteString(fmt.Sprintf("  Scenario:    %s\n", se.ScenarioName))
		}
		if se.Field != "" {
			sb.WriteString(fmt.Sprintf("  Field:       %s\n", se.Field))
		}
		if se.Value != nil {
			sb.WriteString(fmt.Sprintf("  Value:       %v\n", se.Value))
		}
		sb.WriteString(fmt.Sprintf("  Error:       %s\n", se.Message))
		if se.Suggestion != "" {
			sb.WriteString(fmt.Sprintf("  Suggestion:  %s\n", se.Suggestion))
		}
		if se.Cause != nil {
			sb.WriteString(fmt.Sprintf("  Cause:       %v\n", se.Cause))
		}
		sb.WriteString("======================")

		return sb.String()
	}

	return err.Error()
}

// AsScenarioError attempts to extract a ScenarioError from an error chain.
// Returns true if a ScenarioError was found.
func AsScenarioError(err error, target **ScenarioError) bool {
	if err == nil || target == nil {
		return false
	}

	if se, ok := err.(*ScenarioError); ok {
		*target = se
		return true
	}

	// Check wrapped errors using errors.As for proper unwrapping
	return errors.As(err, target)
}

// IsScenarioError checks if an error is or wraps a ScenarioError.
func IsScenarioError(err error) bool {
	var se *ScenarioError
	return AsScenarioError(err, &se)
}
