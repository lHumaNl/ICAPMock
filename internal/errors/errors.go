// Package errors provides custom error types for the ICAP Mock Server.
// It supports error codes, ICAP status codes, HTTP status codes, and error wrapping
// with full compatibility with the standard library's errors package.
//
// Example usage:
//
//	err := NewICAPError(1001, "invalid request", 400, nil)
//	fmt.Println(err.Error()) // [1001] invalid request
//
//	err = Wrap(err, 1002, "request processing failed", 500)
//	root := Cause(err)
package errors

import (
	stderrors "errors"
	"fmt"
)

// Error represents a structured error with ICAP-specific fields.
// It implements the error interface and supports error wrapping and comparison.
type Error struct {
	// Code is the internal error code used for categorization and lookup.
	Code int
	// Message is the human-readable error description.
	Message string
	// ICAPStatus is the ICAP response status code (e.g., 400, 500, 503).
	ICAPStatus int
	// HTTPStatus is the HTTP status code if applicable (0 if not applicable).
	HTTPStatus int
	// Cause is the underlying error that caused this error (may be nil).
	Cause error
}

// Predefined errors for common ICAP error conditions.
// These errors can be used directly or as templates for wrapped errors.
var (
	// ErrInvalidRequest indicates a malformed or invalid ICAP request.
	ErrInvalidRequest = &Error{
		Code:       1001,
		Message:    "invalid request",
		ICAPStatus: 400,
	}

	// ErrUnknownMethod indicates the requested ICAP method is not supported.
	ErrUnknownMethod = &Error{
		Code:       1002,
		Message:    "unknown method",
		ICAPStatus: 501,
	}

	// ErrServiceUnavailable indicates the ICAP service is temporarily unavailable.
	ErrServiceUnavailable = &Error{
		Code:       1003,
		Message:    "service unavailable",
		ICAPStatus: 503,
	}

	// ErrTimeout indicates the ICAP request timed out.
	ErrTimeout = &Error{
		Code:       1004,
		Message:    "request timeout",
		ICAPStatus: 504,
	}

	// ErrInternalServerError indicates an unexpected internal error.
	ErrInternalServerError = &Error{
		Code:       1005,
		Message:    "internal server error",
		ICAPStatus: 500,
	}

	// ErrScenarioNotFound indicates the requested mock scenario was not found.
	ErrScenarioNotFound = &Error{
		Code:       1006,
		Message:    "scenario not found",
		ICAPStatus: 404,
	}

	// ErrRateLimitExceeded indicates the client has exceeded rate limits.
	ErrRateLimitExceeded = &Error{
		Code:       1007,
		Message:    "rate limit exceeded",
		ICAPStatus: 503,
	}

	// ErrBodyTooLarge indicates the request body exceeds size limits.
	ErrBodyTooLarge = &Error{
		Code:       1008,
		Message:    "body too large",
		ICAPStatus: 413,
	}

	// ErrConnectionDropped indicates the connection was unexpectedly closed.
	ErrConnectionDropped = &Error{
		Code:       1009,
		Message:    "connection dropped",
		ICAPStatus: 502,
	}

	// ErrReadTimeout indicates a read operation timed out.
	ErrReadTimeout = &Error{
		Code:       1010,
		Message:    "read operation timeout",
		ICAPStatus: 504,
	}

	// ErrWriteTimeout indicates a write operation timed out.
	ErrWriteTimeout = &Error{
		Code:       1011,
		Message:    "write operation timeout",
		ICAPStatus: 504,
	}

	// ErrContextDeadlineExceeded indicates a context deadline was exceeded.
	ErrContextDeadlineExceeded = &Error{
		Code:       1012,
		Message:    "context deadline exceeded",
		ICAPStatus: 504,
	}

	// ErrIdleTimeout indicates a connection idle timeout occurred.
	ErrIdleTimeout = &Error{
		Code:       1013,
		Message:    "connection idle timeout",
		ICAPStatus: 504,
	}
)

// Error implements the error interface and returns a formatted error string.
// The format is: [code] message
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause of the error.
// Returns nil if there is no underlying cause.
// This enables errors.Unwrap() and errors.Is() support.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is compares this error with target for errors.Is() compatibility.
// Two errors are considered equal if they have the same error code.
// This allows matching any error with the same code, regardless of message.
func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}

	t, ok := target.(*Error)
	if !ok {
		return false
	}

	return e.Code == t.Code
}

// Format implements fmt.Formatter for verbose error output.
// Supported verbs:
//   - %s or %q - standard error format: [code] message
//   - %v  - standard error format: [code] message
//   - %+v - verbose format with all details including cause
func (e *Error) Format(f fmt.State, verb rune) {
	if e == nil {
		return
	}

	switch verb {
	case 'v':
		if f.Flag('+') {
			// Verbose format
			fmt.Fprintf(f, "Code: %d\nMessage: %s\nICAPStatus: %d", e.Code, e.Message, e.ICAPStatus)
			if e.HTTPStatus != 0 {
				fmt.Fprintf(f, "\nHTTPStatus: %d", e.HTTPStatus)
			}
			if e.Cause != nil {
				fmt.Fprintf(f, "\nCause: %v", e.Cause)
			}
			return
		}
		fallthrough
	case 's', 'q':
		if verb == 'q' {
			fmt.Fprintf(f, `"%s"`, e.Error())
		} else {
			fmt.Fprintf(f, "[%d] %s", e.Code, e.Message)
		}
	default:
		fmt.Fprintf(f, "[%d] %s", e.Code, e.Message)
	}
}

// NewICAPError creates a new ICAP error with the specified parameters.
//
// Parameters:
//   - code: Internal error code for categorization
//   - message: Human-readable error description
//   - icapStatus: ICAP response status code
//   - cause: Underlying error (can be nil)
//
// Returns a pointer to the new Error.
//
// Example:
//
//	err := NewICAPError(2001, "custom validation failed", 400, originalErr)
func NewICAPError(code int, message string, icapStatus int, cause error) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		ICAPStatus: icapStatus,
		Cause:      cause,
	}
}

// NewConnectionError creates a new connection-related error.
// This is a convenience function that uses ErrConnectionDropped as a template.
//
// Parameters:
//   - message: Human-readable error description
//   - cause: Underlying error (can be nil)
//
// Returns a new Error with ErrConnectionDropped's code and status.
//
// Example:
//
//	err := NewConnectionError("client closed connection unexpectedly", netErr)
func NewConnectionError(message string, cause error) *Error {
	return &Error{
		Code:       ErrConnectionDropped.Code,
		Message:    message,
		ICAPStatus: ErrConnectionDropped.ICAPStatus,
		Cause:      cause,
	}
}

// NewReadTimeout creates a new read timeout error with a specific operation name.
// This function wraps the underlying timeout with descriptive context about what operation timed out.
//
// Parameters:
//   - operation: Name of the read operation that timed out (e.g., "read ICAP header", "read request body")
//
// Returns a new Error with ErrReadTimeout's code and status.
//
// Example:
//
//	err := NewReadTimeout("read ICAP request header")
func NewReadTimeout(operation string) *Error {
	return &Error{
		Code:       ErrReadTimeout.Code,
		Message:    fmt.Sprintf("read operation timeout: %s", operation),
		ICAPStatus: ErrReadTimeout.ICAPStatus,
	}
}

// NewWriteTimeout creates a new write timeout error with a specific operation name.
// This function wraps the underlying timeout with descriptive context about what operation timed out.
//
// Parameters:
//   - operation: Name of the write operation that timed out (e.g., "write ICAP response", "write response body")
//
// Returns a new Error with ErrWriteTimeout's code and status.
//
// Example:
//
//	err := NewWriteTimeout("write ICAP response")
func NewWriteTimeout(operation string) *Error {
	return &Error{
		Code:       ErrWriteTimeout.Code,
		Message:    fmt.Sprintf("write operation timeout: %s", operation),
		ICAPStatus: ErrWriteTimeout.ICAPStatus,
	}
}

// NewIdleTimeout creates a new idle timeout error.
// This error is used when a connection is closed due to inactivity.
//
// Returns a new Error with ErrIdleTimeout's code and status.
//
// Example:
//
//	err := NewIdleTimeout()
func NewIdleTimeout() *Error {
	return &Error{
		Code:       ErrIdleTimeout.Code,
		Message:    ErrIdleTimeout.Message,
		ICAPStatus: ErrIdleTimeout.ICAPStatus,
	}
}

// GetCode extracts the internal error code from an error.
// Returns 0 if the error is nil or not an ICAP Error.
//
// Example:
//
//	code := GetCode(err)
//	if code == 0 {
//	    // Not an ICAP error
//	}
func GetCode(err error) int {
	if err == nil {
		return 0
	}

	var e *Error
	if stderrors.As(err, &e) {
		return e.Code
	}
	return 0
}

// GetICAPStatus extracts the ICAP status code from an error.
// Returns 500 (Internal Server Error) if the error is nil or not an ICAP Error.
// This provides a safe default for error responses.
//
// Example:
//
//	status := GetICAPStatus(err)
//	// Use status for ICAP response
func GetICAPStatus(err error) int {
	if err == nil {
		return 500
	}

	var e *Error
	if stderrors.As(err, &e) {
		return e.ICAPStatus
	}
	return 500
}

// IsTimeout checks if an error is any type of timeout error.
// This includes read timeouts, write timeouts, idle timeouts, and context deadline exceeded.
// It handles wrapped errors using errors.Is().
//
// Parameters:
//   - err: The error to check (can be nil)
//
// Returns true if the error is any timeout type, false otherwise.
//
// Example:
//
//	if IsTimeout(err) {
//	    // Handle timeout-specific logic
//	}
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}

	// Check for all timeout error types
	return stderrors.Is(err, ErrReadTimeout) ||
		stderrors.Is(err, ErrWriteTimeout) ||
		stderrors.Is(err, ErrIdleTimeout) ||
		stderrors.Is(err, ErrContextDeadlineExceeded)
}

// GetTimeoutType extracts the timeout type label from an error.
// This is useful for recording metrics with specific timeout types.
//
// Parameters:
//   - err: The error to extract timeout type from (can be nil)
//
// Returns a timeout type label: "read", "write", "idle", "deadline", or "other" if not a timeout.
//
// Example:
//
//	timeoutType := GetTimeoutType(err)
//	if timeoutType != "other" {
//	    metrics.RecordError(timeoutType)
//	}
func GetTimeoutType(err error) string {
	if err == nil {
		return "other"
	}

	// Check for specific timeout types
	if stderrors.Is(err, ErrReadTimeout) {
		return "read"
	}
	if stderrors.Is(err, ErrWriteTimeout) {
		return "write"
	}
	if stderrors.Is(err, ErrIdleTimeout) {
		return "idle"
	}
	if stderrors.Is(err, ErrContextDeadlineExceeded) {
		return "deadline"
	}

	return "other"
}
