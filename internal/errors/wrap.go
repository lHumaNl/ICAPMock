// Copyright 2026 ICAP Mock

package errors

import (
	stderrors "errors"
	"fmt"
)

// Wrap creates a new ICAP Error that wraps an existing error.
// If err is nil, Wrap returns nil (allowing chaining without nil checks).
//
// Parameters:
//   - err: The error to wrap (can be nil)
//   - code: Internal error code for the new error
//   - message: Human-readable description of the error context
//   - icapStatus: ICAP response status code
//
// Returns a new Error wrapping the original, or nil if err is nil.
//
// Example:
//
//	if err := doSomething(); err != nil {
//	    return errors.Wrap(err, 1001, "failed during processing", 500)
//	}
func Wrap(err error, code int, message string, icapStatus int) *Error {
	if err == nil {
		return nil
	}

	return &Error{
		Code:       code,
		Message:    message,
		ICAPStatus: icapStatus,
		Cause:      err,
	}
}

// Wrapf creates a new ICAP Error with a formatted message.
// The message is constructed using fmt.Sprintf with the provided format and args.
// If err is nil, Wrapf returns nil (allowing chaining without nil checks).
//
// Parameters:
//   - err: The error to wrap (can be nil)
//   - code: Internal error code for the new error
//   - icapStatus: ICAP response status code
//   - format: Format string (passed to fmt.Sprintf)
//   - args: Arguments for the format string
//
// Returns a new Error wrapping the original, or nil if err is nil.
//
// Example:
//
//	if err := processRequest(reqID); err != nil {
//	    return errors.Wrapf(err, 1001, 400, "request %s validation failed", reqID)
//	}
func Wrapf(err error, code int, icapStatus int, format string, args ...interface{}) *Error {
	if err == nil {
		return nil
	}

	return &Error{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		ICAPStatus: icapStatus,
		Cause:      err,
	}
}

// WrapPredefined wraps an error using a predefined error as a template.
// The Code and ICAPStatus are copied from the template, with a new cause.
// If err is nil, WrapPredefined returns nil.
//
// Parameters:
//   - err: The error to wrap (can be nil)
//   - template: A predefined Error to use for code and status
//
// Returns a new Error based on the template, or nil if err is nil.
//
// Example:
//
//	if err := handleTimeout(); err != nil {
//	    return errors.WrapPredefined(err, errors.ErrTimeout)
//	}
func WrapPredefined(err error, template *Error) *Error {
	if err == nil || template == nil {
		return nil
	}

	return &Error{
		Code:       template.Code,
		Message:    template.Message,
		ICAPStatus: template.ICAPStatus,
		HTTPStatus: template.HTTPStatus,
		Cause:      err,
	}
}

// Cause returns the root cause of an error chain.
// It unwraps the error until it finds an error with no underlying cause.
// If the error is nil, Cause returns nil.
// If the error does not implement Unwrap(), Cause returns the error itself.
//
// This is useful for logging and debugging to find the original error
// that triggered the error chain.
//
// Example:
//
//	err := someOperation()
//	root := errors.Cause(err)
//	log.Printf("root cause: %v", root)
func Cause(err error) error {
	if err == nil {
		return nil
	}

	// Unwrap until we find the root cause
	for {
		// Try to unwrap using standard errors.Unwrap
		// This handles both stdlib wrapped errors and our Error type
		unwrapped := stderrors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
