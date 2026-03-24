// Package errors_test provides comprehensive tests for the ICAP Mock Server error package.
package errors_test

import (
	"errors"
	"fmt"
	"testing"

	icaperrors "github.com/icap-mock/icap-mock/internal/errors"
)

// TestError_Struct verifies the Error struct contains all expected fields.
func TestError_Struct(t *testing.T) {
	err := &icaperrors.Error{
		Code:       1001,
		Message:    "test message",
		ICAPStatus: 400,
		HTTPStatus: 400,
		Cause:      nil,
	}

	if err.Code != 1001 {
		t.Errorf("expected Code 1001, got %d", err.Code)
	}
	if err.Message != "test message" {
		t.Errorf("expected Message 'test message', got %s", err.Message)
	}
	if err.ICAPStatus != 400 {
		t.Errorf("expected ICAPStatus 400, got %d", err.ICAPStatus)
	}
	if err.HTTPStatus != 400 {
		t.Errorf("expected HTTPStatus 400, got %d", err.HTTPStatus)
	}
	if err.Cause != nil {
		t.Errorf("expected Cause nil, got %v", err.Cause)
	}
}

// TestError_Error tests the Error() method implementation.
func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *icaperrors.Error
		expected string
	}{
		{
			name: "basic error",
			err: &icaperrors.Error{
				Code:    1001,
				Message: "invalid request",
			},
			expected: "[1001] invalid request",
		},
		{
			name: "error with ICAP status",
			err: &icaperrors.Error{
				Code:       1001,
				Message:    "invalid request",
				ICAPStatus: 400,
			},
			expected: "[1001] invalid request",
		},
		{
			name: "empty message",
			err: &icaperrors.Error{
				Code:    1000,
				Message: "",
			},
			expected: "[1000] ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestError_Unwrap tests the Unwrap() method for error chain support.
func TestError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &icaperrors.Error{
		Code:       1001,
		Message:    "wrapped error",
		ICAPStatus: 400,
		Cause:      cause,
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("Unwrap() returned nil, expected non-nil")
	}
	if unwrapped.Error() != "underlying error" {
		t.Errorf("Unwrap() = %q, want %q", unwrapped.Error(), "underlying error")
	}

	// Test nil cause
	errNoCause := &icaperrors.Error{Code: 1001, Message: "no cause"}
	if errNoCause.Unwrap() != nil {
		t.Error("Unwrap() should return nil when Cause is nil")
	}
}

// TestError_Is tests the Is() method for errors.Is() support.
func TestError_Is(t *testing.T) {
	err1 := &icaperrors.Error{Code: 1001, Message: "error one"}
	err2 := &icaperrors.Error{Code: 1001, Message: "error two"}
	err3 := &icaperrors.Error{Code: 1002, Message: "error three"}

	// Same code should match
	if !err1.Is(err2) {
		t.Error("errors with same code should match")
	}

	// Different code should not match
	if err1.Is(err3) {
		t.Error("errors with different codes should not match")
	}

	// nil target should not match
	if err1.Is(nil) {
		t.Error("error should not match nil target")
	}

	// Non-Error target should not match
	if err1.Is(errors.New("standard error")) {
		t.Error("error should not match non-Error type")
	}
}

// TestError_ErrorsIsIntegration tests integration with errors.Is().
func TestError_ErrorsIsIntegration(t *testing.T) {
	baseErr := icaperrors.ErrInvalidRequest
	wrappedErr := &icaperrors.Error{
		Code:       1001,
		Message:    "specific invalid request",
		ICAPStatus: 400,
		Cause:      nil,
	}

	// errors.Is should match based on code
	if !errors.Is(wrappedErr, baseErr) {
		t.Error("errors.Is() should match errors with same code")
	}
}

// TestError_ErrorsAsIntegration tests integration with errors.As().
func TestError_ErrorsAsIntegration(t *testing.T) {
	cause := errors.New("underlying")
	err := &icaperrors.Error{
		Code:       1001,
		Message:    "test",
		ICAPStatus: 400,
		Cause:      cause,
	}

	var target *icaperrors.Error
	if !errors.As(err, &target) {
		t.Error("errors.As() should extract Error from error interface")
	}
	if target.Code != 1001 {
		t.Errorf("extracted error code = %d, want 1001", target.Code)
	}
}

// TestPredefinedErrors verifies all predefined errors have correct values.
func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        *icaperrors.Error
		code       int
		icapStatus int
	}{
		{"ErrInvalidRequest", icaperrors.ErrInvalidRequest, 1001, 400},
		{"ErrUnknownMethod", icaperrors.ErrUnknownMethod, 1002, 501},
		{"ErrServiceUnavailable", icaperrors.ErrServiceUnavailable, 1003, 503},
		{"ErrTimeout", icaperrors.ErrTimeout, 1004, 504},
		{"ErrInternalServerError", icaperrors.ErrInternalServerError, 1005, 500},
		{"ErrScenarioNotFound", icaperrors.ErrScenarioNotFound, 1006, 404},
		{"ErrRateLimitExceeded", icaperrors.ErrRateLimitExceeded, 1007, 503},
		{"ErrBodyTooLarge", icaperrors.ErrBodyTooLarge, 1008, 413},
		{"ErrConnectionDropped", icaperrors.ErrConnectionDropped, 1009, 502},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s is nil", tt.name)
			}
			if tt.err.Code != tt.code {
				t.Errorf("%s.Code = %d, want %d", tt.name, tt.err.Code, tt.code)
			}
			if tt.err.ICAPStatus != tt.icapStatus {
				t.Errorf("%s.ICAPStatus = %d, want %d", tt.name, tt.err.ICAPStatus, tt.icapStatus)
			}
		})
	}
}

// TestNewICAPError tests the NewICAPError constructor.
func TestNewICAPError(t *testing.T) {
	cause := errors.New("root cause")
	err := icaperrors.NewICAPError(2001, "custom error", 418, cause)

	if err.Code != 2001 {
		t.Errorf("Code = %d, want 2001", err.Code)
	}
	if err.Message != "custom error" {
		t.Errorf("Message = %s, want 'custom error'", err.Message)
	}
	if err.ICAPStatus != 418 {
		t.Errorf("ICAPStatus = %d, want 418", err.ICAPStatus)
	}
	if err.Cause != cause {
		t.Error("Cause not set correctly")
	}
}

// TestNewICAPError_NilCause tests NewICAPError with nil cause.
func TestNewICAPError_NilCause(t *testing.T) {
	err := icaperrors.NewICAPError(2001, "no cause error", 400, nil)

	if err.Cause != nil {
		t.Error("Cause should be nil")
	}
}

// TestNewConnectionError tests the NewConnectionError constructor.
func TestNewConnectionError(t *testing.T) {
	cause := errors.New("connection reset")
	err := icaperrors.NewConnectionError("client disconnected", cause)

	if err.Code != icaperrors.ErrConnectionDropped.Code {
		t.Errorf("Code = %d, want %d", err.Code, icaperrors.ErrConnectionDropped.Code)
	}
	if err.Message != "client disconnected" {
		t.Errorf("Message = %s, want 'client disconnected'", err.Message)
	}
	if err.ICAPStatus != 502 {
		t.Errorf("ICAPStatus = %d, want 502", err.ICAPStatus)
	}
	if err.Cause != cause {
		t.Error("Cause not set correctly")
	}
}

// TestGetCode tests the GetCode helper function.
func TestGetCode(t *testing.T) {
	err := icaperrors.NewICAPError(1234, "test", 400, nil)
	code := icaperrors.GetCode(err)
	if code != 1234 {
		t.Errorf("GetCode() = %d, want 1234", code)
	}

	// Non-Error type should return 0
	standardErr := errors.New("standard error")
	code = icaperrors.GetCode(standardErr)
	if code != 0 {
		t.Errorf("GetCode() for standard error = %d, want 0", code)
	}
}

// TestGetICAPStatus tests the GetICAPStatus helper function.
func TestGetICAPStatus(t *testing.T) {
	err := icaperrors.NewICAPError(1234, "test", 418, nil)
	status := icaperrors.GetICAPStatus(err)
	if status != 418 {
		t.Errorf("GetICAPStatus() = %d, want 418", status)
	}

	// Non-Error type should return 500 (internal server error default)
	standardErr := errors.New("standard error")
	status = icaperrors.GetICAPStatus(standardErr)
	if status != 500 {
		t.Errorf("GetICAPStatus() for standard error = %d, want 500", status)
	}
}

// TestErrorWithCause tests error formatting with cause.
func TestErrorWithCause(t *testing.T) {
	cause := errors.New("root cause")
	err := &icaperrors.Error{
		Code:       1001,
		Message:    "wrapped",
		ICAPStatus: 400,
		Cause:      cause,
	}

	// Error string should include cause information
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}
}

// TestError_ChainUnwrap tests multiple levels of error wrapping.
func TestError_ChainUnwrap(t *testing.T) {
	root := errors.New("root error")
	level1 := &icaperrors.Error{Code: 1001, Message: "level 1", Cause: root}
	level2 := &icaperrors.Error{Code: 1002, Message: "level 2", Cause: level1}

	// Unwrap twice should get to root
	unwrapped1 := errors.Unwrap(level2)
	if unwrapped1 == nil {
		t.Fatal("first Unwrap returned nil")
	}

	unwrapped2 := errors.Unwrap(unwrapped1)
	if unwrapped2 == nil {
		t.Fatal("second Unwrap returned nil")
	}

	if unwrapped2.Error() != "root error" {
		t.Errorf("final unwrap = %q, want 'root error'", unwrapped2.Error())
	}
}

// TestError_Format tests the Format implementation for fmt.Formatter.
func TestError_Format(t *testing.T) {
	cause := errors.New("underlying cause")
	err := &icaperrors.Error{
		Code:       1001,
		Message:    "test error",
		ICAPStatus: 400,
		HTTPStatus: 400,
		Cause:      cause,
	}

	tests := []struct {
		name     string
		format   string
		contains string
	}{
		{"default format", "%v", "[1001]"},
		{"verbose format", "%+v", "Code: 1001"},
		{"with cause", "%+v", "Cause: underlying cause"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf(tt.format, err)
			if !containsString(got, tt.contains) {
				t.Errorf("Format(%s) = %q, should contain %q", tt.format, got, tt.contains)
			}
		})
	}
}

// containsString is a helper function for string containment checks.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestTimeoutErrorTypes verifies all timeout error types have correct values.
func TestTimeoutErrorTypes(t *testing.T) {
	tests := []struct {
		name       string
		err        *icaperrors.Error
		code       int
		icapStatus int
	}{
		{"ErrReadTimeout", icaperrors.ErrReadTimeout, 1010, 504},
		{"ErrWriteTimeout", icaperrors.ErrWriteTimeout, 1011, 504},
		{"ErrContextDeadlineExceeded", icaperrors.ErrContextDeadlineExceeded, 1012, 504},
		{"ErrIdleTimeout", icaperrors.ErrIdleTimeout, 1013, 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s is nil", tt.name)
			}
			if tt.err.Code != tt.code {
				t.Errorf("%s.Code = %d, want %d", tt.name, tt.err.Code, tt.code)
			}
			if tt.err.ICAPStatus != tt.icapStatus {
				t.Errorf("%s.ICAPStatus = %d, want %d", tt.name, tt.err.ICAPStatus, tt.icapStatus)
			}
			if tt.err.Message == "" {
				t.Errorf("%s.Message should not be empty", tt.name)
			}
		})
	}
}

// TestNewReadTimeout tests the NewReadTimeout constructor.
func TestNewReadTimeout(t *testing.T) {
	operation := "read ICAP header"
	err := icaperrors.NewReadTimeout(operation)

	if err.Code != icaperrors.ErrReadTimeout.Code {
		t.Errorf("Code = %d, want %d", err.Code, icaperrors.ErrReadTimeout.Code)
	}
	if err.ICAPStatus != 504 {
		t.Errorf("ICAPStatus = %d, want 504", err.ICAPStatus)
	}
	if err.Cause != nil {
		t.Error("Cause should be nil")
	}
	expectedMsg := "read operation timeout: " + operation
	if err.Message != expectedMsg {
		t.Errorf("Message = %s, want %s", err.Message, expectedMsg)
	}
}

// TestNewWriteTimeout tests the NewWriteTimeout constructor.
func TestNewWriteTimeout(t *testing.T) {
	operation := "write ICAP response"
	err := icaperrors.NewWriteTimeout(operation)

	if err.Code != icaperrors.ErrWriteTimeout.Code {
		t.Errorf("Code = %d, want %d", err.Code, icaperrors.ErrWriteTimeout.Code)
	}
	if err.ICAPStatus != 504 {
		t.Errorf("ICAPStatus = %d, want 504", err.ICAPStatus)
	}
	if err.Cause != nil {
		t.Error("Cause should be nil")
	}
	expectedMsg := "write operation timeout: " + operation
	if err.Message != expectedMsg {
		t.Errorf("Message = %s, want %s", err.Message, expectedMsg)
	}
}

// TestNewIdleTimeout tests the NewIdleTimeout constructor.
func TestNewIdleTimeout(t *testing.T) {
	err := icaperrors.NewIdleTimeout()

	if err.Code != icaperrors.ErrIdleTimeout.Code {
		t.Errorf("Code = %d, want %d", err.Code, icaperrors.ErrIdleTimeout.Code)
	}
	if err.ICAPStatus != 504 {
		t.Errorf("ICAPStatus = %d, want 504", err.ICAPStatus)
	}
	if err.Cause != nil {
		t.Error("Cause should be nil")
	}
	if err.Message != icaperrors.ErrIdleTimeout.Message {
		t.Errorf("Message = %s, want %s", err.Message, icaperrors.ErrIdleTimeout.Message)
	}
}

// TestIsTimeout tests the IsTimeout helper function.
func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ErrReadTimeout", icaperrors.ErrReadTimeout, true},
		{"ErrWriteTimeout", icaperrors.ErrWriteTimeout, true},
		{"ErrIdleTimeout", icaperrors.ErrIdleTimeout, true},
		{"ErrContextDeadlineExceeded", icaperrors.ErrContextDeadlineExceeded, true},
		{"NewReadTimeout", icaperrors.NewReadTimeout("test"), true},
		{"NewWriteTimeout", icaperrors.NewWriteTimeout("test"), true},
		{"NewIdleTimeout", icaperrors.NewIdleTimeout(), true},
		{"ErrInvalidRequest", icaperrors.ErrInvalidRequest, false},
		{"ErrConnectionDropped", icaperrors.ErrConnectionDropped, false},
		{"ErrTimeout (generic)", icaperrors.ErrTimeout, false},
		{"Standard error", errors.New("standard error"), false},
		{"Nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := icaperrors.IsTimeout(tt.err)
			if got != tt.want {
				t.Errorf("IsTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsTimeout_Wrapped tests IsTimeout with wrapped errors.
func TestIsTimeout_Wrapped(t *testing.T) {
	rootErr := icaperrors.ErrReadTimeout
	wrappedErr := fmt.Errorf("wrapped: %w", rootErr)

	if !icaperrors.IsTimeout(wrappedErr) {
		t.Error("IsTimeout() should return true for wrapped timeout error")
	}

	// Test double wrapping
	doubleWrapped := fmt.Errorf("double wrapped: %w", wrappedErr)
	if !icaperrors.IsTimeout(doubleWrapped) {
		t.Error("IsTimeout() should return true for double-wrapped timeout error")
	}

	// Test wrapped non-timeout error
	nonTimeoutErr := icaperrors.ErrInvalidRequest
	wrappedNonTimeout := fmt.Errorf("wrapped: %w", nonTimeoutErr)
	if icaperrors.IsTimeout(wrappedNonTimeout) {
		t.Error("IsTimeout() should return false for wrapped non-timeout error")
	}
}

// TestGetTimeoutType tests the GetTimeoutType helper function.
func TestGetTimeoutType(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrReadTimeout", icaperrors.ErrReadTimeout, "read"},
		{"ErrWriteTimeout", icaperrors.ErrWriteTimeout, "write"},
		{"ErrIdleTimeout", icaperrors.ErrIdleTimeout, "idle"},
		{"ErrContextDeadlineExceeded", icaperrors.ErrContextDeadlineExceeded, "deadline"},
		{"NewReadTimeout", icaperrors.NewReadTimeout("test"), "read"},
		{"NewWriteTimeout", icaperrors.NewWriteTimeout("test"), "write"},
		{"NewIdleTimeout", icaperrors.NewIdleTimeout(), "idle"},
		{"ErrInvalidRequest", icaperrors.ErrInvalidRequest, "other"},
		{"ErrConnectionDropped", icaperrors.ErrConnectionDropped, "other"},
		{"Standard error", errors.New("standard error"), "other"},
		{"Nil error", nil, "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := icaperrors.GetTimeoutType(tt.err)
			if got != tt.want {
				t.Errorf("GetTimeoutType() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestGetTimeoutType_Wrapped tests GetTimeoutType with wrapped errors.
func TestGetTimeoutType_Wrapped(t *testing.T) {
	rootErr := icaperrors.ErrReadTimeout
	wrappedErr := fmt.Errorf("wrapped: %w", rootErr)

	got := icaperrors.GetTimeoutType(wrappedErr)
	if got != "read" {
		t.Errorf("GetTimeoutType() = %s, want 'read'", got)
	}

	// Test double wrapping
	doubleWrapped := fmt.Errorf("double wrapped: %w", wrappedErr)
	got = icaperrors.GetTimeoutType(doubleWrapped)
	if got != "read" {
		t.Errorf("GetTimeoutType() = %s, want 'read'", got)
	}

	// Test wrapped non-timeout error
	nonTimeoutErr := icaperrors.ErrInvalidRequest
	wrappedNonTimeout := fmt.Errorf("wrapped: %w", nonTimeoutErr)
	got = icaperrors.GetTimeoutType(wrappedNonTimeout)
	if got != "other" {
		t.Errorf("GetTimeoutType() = %s, want 'other'", got)
	}
}

// TestTimeoutErrorCodesUnique verifies timeout error codes are unique.
func TestTimeoutErrorCodesUnique(t *testing.T) {
	codes := []int{
		icaperrors.ErrReadTimeout.Code,
		icaperrors.ErrWriteTimeout.Code,
		icaperrors.ErrContextDeadlineExceeded.Code,
		icaperrors.ErrIdleTimeout.Code,
	}

	seen := make(map[int]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("Error code %d is duplicated", code)
		}
		seen[code] = true
	}
}

// TestTimeoutErrorCodesFollowPattern verifies timeout error codes follow 1000+ pattern.
func TestTimeoutErrorCodesFollowPattern(t *testing.T) {
	codes := []int{
		icaperrors.ErrReadTimeout.Code,
		icaperrors.ErrWriteTimeout.Code,
		icaperrors.ErrContextDeadlineExceeded.Code,
		icaperrors.ErrIdleTimeout.Code,
	}

	for _, code := range codes {
		if code < 1000 || code > 1999 {
			t.Errorf("Error code %d does not follow 1000+ pattern", code)
		}
	}
}

// TestTimeoutErrorsMatchWithIs tests timeout errors with errors.Is().
func TestTimeoutErrorsMatchWithIs(t *testing.T) {
	// Test ErrReadTimeout
	newReadErr := icaperrors.NewReadTimeout("test operation")
	if !errors.Is(newReadErr, icaperrors.ErrReadTimeout) {
		t.Error("NewReadTimeout should match ErrReadTimeout with errors.Is()")
	}

	// Test ErrWriteTimeout
	newWriteErr := icaperrors.NewWriteTimeout("test operation")
	if !errors.Is(newWriteErr, icaperrors.ErrWriteTimeout) {
		t.Error("NewWriteTimeout should match ErrWriteTimeout with errors.Is()")
	}

	// Test ErrIdleTimeout
	newIdleErr := icaperrors.NewIdleTimeout()
	if !errors.Is(newIdleErr, icaperrors.ErrIdleTimeout) {
		t.Error("NewIdleTimeout should match ErrIdleTimeout with errors.Is()")
	}

	// Test ErrContextDeadlineExceeded
	if !errors.Is(icaperrors.ErrContextDeadlineExceeded, icaperrors.ErrContextDeadlineExceeded) {
		t.Error("ErrContextDeadlineExceeded should match itself with errors.Is()")
	}
}
