// Package util provides utilities for working with context.
package util

import (
	"context"
	"errors"
	"time"
)

// ContextKey is the type for context keys to prevent collisions.
type ContextKey string

// ContextCancellationReason defines the reason for context cancellation.
type ContextCancellationReason string

const (
	// RequestIDKey is the key used to store the request ID in context.
	RequestIDKey ContextKey = "request_id"

	// ReasonDeadlineExceeded - context was cancelled due to deadline exceeded
	ReasonDeadlineExceeded ContextCancellationReason = "deadline_exceeded"
	// ReasonCanceled - context was explicitly cancelled
	ReasonCanceled ContextCancellationReason = "canceled"
)

// CheckCancellation checks the context state and returns the cancellation reason.
// Returns an error if the context is cancelled, nil otherwise.
//
// Parameters:
//   - ctx: Context to check
//
// Returns:
//   - ContextCancellationReason: Cancellation reason (deadline_exceeded/canceled)
//   - error: Error if context is cancelled, nil otherwise
//
// Example:
//
//	reason, err := CheckCancellation(ctx)
//	if err != nil {
//	    // Handle context cancellation
//	    return nil, err
//	}
func CheckCancellation(ctx context.Context) (ContextCancellationReason, error) {
	if ctx.Err() == nil {
		return "", nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ReasonDeadlineExceeded, ctx.Err()
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return ReasonCanceled, ctx.Err()
	}

	return ReasonCanceled, ctx.Err()
}

// RequestIDFromContext retrieves the request ID from the context.
// Returns an empty string if not found.
//
// Parameters:
//   - ctx: Context to retrieve request ID from
//
// Returns:
//   - string: Request ID or empty string if not found
//
// Example:
//
//	requestID := RequestIDFromContext(ctx)
//	if requestID != "" {
//	    log.Printf("Processing request %s", requestID)
//	}
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(RequestIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// WithRequestID adds a request ID to the context.
// Returns a new context with the request ID set.
//
// Parameters:
//   - ctx: Parent context
//   - requestID: Request ID to store
//
// Returns:
//   - context.Context: New context with request ID
//
// Example:
//
//	ctx := WithRequestID(ctx, "req-123")
//	// Later in the code
//	requestID := RequestIDFromContext(ctx) // "req-123"
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// GenerateRequestID generates a unique request ID based on the timestamp.
// Format: "req-YYYYMMDD-NNN" where NNN is a sequence number.
func GenerateRequestID(t time.Time) string {
	return "req-" + t.Format("20060102-150405.000")
}
