// Copyright 2026 ICAP Mock

package errors_test

import (
	"errors"
	"fmt"
	"testing"

	icaperrors "github.com/icap-mock/icap-mock/internal/errors"
)

// TestWrap tests the Wrap function.
func TestWrap(t *testing.T) {
	cause := errors.New("original error")
	err := icaperrors.Wrap(cause, 1001, "wrapped message", 400)

	if err == nil {
		t.Fatal("Wrap() returned nil")
	}
	if err.Code != 1001 {
		t.Errorf("Code = %d, want 1001", err.Code)
	}
	if err.Message != "wrapped message" {
		t.Errorf("Message = %q, want %q", err.Message, "wrapped message")
	}
	if err.ICAPStatus != 400 {
		t.Errorf("ICAPStatus = %d, want 400", err.ICAPStatus)
	}
	if !errors.Is(err.Cause, cause) {
		t.Error("Cause not set correctly")
	}
}

// TestWrap_NilError tests Wrap with nil error (should return nil).
func TestWrap_NilError(t *testing.T) {
	err := icaperrors.Wrap(nil, 1001, "message", 400)
	if err != nil {
		t.Errorf("Wrap(nil, ...) = %v, want nil", err)
	}
}

// TestWrapf tests the Wrapf function with format specifiers.
func TestWrapf(t *testing.T) {
	cause := errors.New("original error")
	err := icaperrors.Wrapf(cause, 1001, 400, "error for user %s with id %d", "john", 42)

	if err == nil {
		t.Fatal("Wrapf() returned nil")
	}
	expected := "error for user john with id 42"
	if err.Message != expected {
		t.Errorf("Message = %q, want %q", err.Message, expected)
	}
	if err.Code != 1001 {
		t.Errorf("Code = %d, want 1001", err.Code)
	}
	if err.ICAPStatus != 400 {
		t.Errorf("ICAPStatus = %d, want 400", err.ICAPStatus)
	}
	if !errors.Is(err.Cause, cause) {
		t.Error("Cause not set correctly")
	}
}

// TestWrapf_NilError tests Wrapf with nil error (should return nil).
func TestWrapf_NilError(t *testing.T) {
	err := icaperrors.Wrapf(nil, 1001, 400, "message %d", 1)
	if err != nil {
		t.Errorf("Wrapf(nil, ...) = %v, want nil", err)
	}
}

// TestWrapPredefined tests wrapping with predefined errors.
func TestWrapPredefined(t *testing.T) {
	cause := errors.New("network timeout")
	err := icaperrors.WrapPredefined(cause, icaperrors.ErrTimeout)

	if err == nil {
		t.Fatal("WrapPredefined() returned nil")
	}
	if err.Code != icaperrors.ErrTimeout.Code {
		t.Errorf("Code = %d, want %d", err.Code, icaperrors.ErrTimeout.Code)
	}
	if err.ICAPStatus != icaperrors.ErrTimeout.ICAPStatus {
		t.Errorf("ICAPStatus = %d, want %d", err.ICAPStatus, icaperrors.ErrTimeout.ICAPStatus)
	}
	if !errors.Is(err.Cause, cause) {
		t.Error("Cause not set correctly")
	}
}

// TestWrapPredefined_NilError tests WrapPredefined with nil error.
func TestWrapPredefined_NilError(t *testing.T) {
	err := icaperrors.WrapPredefined(nil, icaperrors.ErrTimeout)
	if err != nil {
		t.Errorf("WrapPredefined(nil, ...) = %v, want nil", err)
	}
}

// TestCause tests the Cause function to get root error.
func TestCause(t *testing.T) {
	root := errors.New("root error")
	level1 := icaperrors.Wrap(root, 1001, "level 1", 400)
	level2 := icaperrors.Wrap(level1, 1002, "level 2", 500)

	cause := icaperrors.Cause(level2)
	if !errors.Is(cause, root) {
		t.Errorf("Cause() = %v, want %v", cause, root)
	}
}

// TestCause_NoWrap tests Cause on non-wrapped error.
func TestCause_NoWrap(t *testing.T) {
	err := errors.New("simple error")
	cause := icaperrors.Cause(err)
	if !errors.Is(cause, err) {
		t.Errorf("Cause() on non-wrapped error should return same error")
	}
}

// TestCause_Nil tests Cause with nil error.
func TestCause_Nil(t *testing.T) {
	cause := icaperrors.Cause(nil)
	if cause != nil {
		t.Errorf("Cause(nil) = %v, want nil", cause)
	}
}

// TestCause_StandardErrorChain tests Cause with standard error chain.
func TestCause_StandardErrorChain(t *testing.T) {
	root := errors.New("root")
	wrapped := fmtErrorf("wrapped: %w", root)

	cause := icaperrors.Cause(wrapped)
	if !errors.Is(cause, root) {
		t.Errorf("Cause() = %v, want %v", cause, root)
	}
}

// TestWrapf_EmptyFormat tests Wrapf with empty format string.
func TestWrapf_EmptyFormat(t *testing.T) {
	cause := errors.New("cause")
	err := icaperrors.Wrapf(cause, 1001, 400, "")
	if err == nil {
		t.Fatal("Wrapf() returned nil")
	}
	if err.Message != "" {
		t.Errorf("Message = %q, want empty", err.Message)
	}
}

// TestWrapf_VerboseFormat tests Wrapf with complex format.
func TestWrapf_VerboseFormat(t *testing.T) {
	cause := errors.New("cause")
	err := icaperrors.Wrapf(cause, 1001, 400, "request %s failed after %d retries, last status: %d", "POST /api", 3, 500)

	expected := "request POST /api failed after 3 retries, last status: 500"
	if err.Message != expected {
		t.Errorf("Message = %q, want %q", err.Message, expected)
	}
}

// TestWrap_ICAPError tests wrapping an existing ICAP Error.
func TestWrap_ICAPError(t *testing.T) {
	inner := icaperrors.NewICAPError(1001, "inner error", 400, nil)
	outer := icaperrors.Wrap(inner, 1002, "outer error", 500)

	// Chain should be preserved
	if !errors.Is(outer.Cause, inner) {
		t.Error("outer.Cause should be inner error")
	}

	// errors.Is should work through the chain
	if !errors.Is(outer, inner) {
		t.Error("errors.Is should match through chain")
	}
}

// TestCause_DeepChain tests Cause with deeply nested errors.
func TestCause_DeepChain(t *testing.T) {
	root := errors.New("deepest error")
	current := root
	for i := 0; i < 10; i++ {
		current = icaperrors.Wrap(current, 1000+i, "level", 400)
	}

	cause := icaperrors.Cause(current)
	if !errors.Is(cause, root) {
		t.Errorf("Cause() on deep chain = %v, want %v", cause, root)
	}
}

// fmtErrorf is a helper to create wrapped errors using standard library.
func fmtErrorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
