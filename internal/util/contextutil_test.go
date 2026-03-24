// Package util provides tests for context utilities.
package util

import (
	"context"
	"testing"
	"time"
)

func TestRequestIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "context with request ID",
			ctx:      WithRequestID(context.Background(), "test-123"),
			expected: "test-123",
		},
		{
			name:     "context with empty request ID",
			ctx:      WithRequestID(context.Background(), ""),
			expected: "",
		},
		{
			name:     "context without request ID",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with different value type",
			ctx:      context.WithValue(context.Background(), RequestIDKey, 123),
			expected: "",
		},
		{
			name:     "context with nil value",
			ctx:      context.WithValue(context.Background(), RequestIDKey, nil),
			expected: "",
		},
		{
			name: "context with request ID in chain",
			ctx: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, ContextKey("other_key"), "other_value")
				ctx = WithRequestID(ctx, "chain-456")
				return ctx
			}(),
			expected: "chain-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RequestIDFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("RequestIDFromContext() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestWithRequestID(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		requestID string
	}{
		{
			name:      "background context",
			ctx:       context.Background(),
			requestID: "req-001",
		},
		{
			name:      "context with existing values",
			ctx:       context.WithValue(context.Background(), ContextKey("existing"), "value"),
			requestID: "req-002",
		},
		{
			name:      "empty request ID",
			ctx:       context.Background(),
			requestID: "",
		},
		{
			name:      "long request ID",
			ctx:       context.Background(),
			requestID: "very-long-request-id-with-many-characters-123456789",
		},
		{
			name:      "special characters",
			ctx:       context.Background(),
			requestID: "req_123-abc.456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newCtx := WithRequestID(tt.ctx, tt.requestID)

			// Verify the request ID is correctly stored
			result := RequestIDFromContext(newCtx)
			if result != tt.requestID {
				t.Errorf("WithRequestID() stored %v, want %v", result, tt.requestID)
			}

			// Verify original context is not modified
			originalResult := RequestIDFromContext(tt.ctx)
			if originalResult != "" {
				t.Errorf("Original context was modified, got %v", originalResult)
			}

			// Verify other values are preserved
			if existing := tt.ctx.Value(ContextKey("existing")); existing != nil {
				if newCtx.Value(ContextKey("existing")) != existing {
					t.Errorf("Existing value not preserved in new context")
				}
			}
		})
	}
}

func TestWithRequestIDChain(t *testing.T) {
	ctx := context.Background()

	// Chain multiple context values
	ctx = context.WithValue(ctx, ContextKey("step1"), "value1")
	ctx = WithRequestID(ctx, "req-100")
	ctx = context.WithValue(ctx, ContextKey("step2"), "value2")

	// Verify request ID is preserved
	if got := RequestIDFromContext(ctx); got != "req-100" {
		t.Errorf("Request ID not preserved in chain, got %v", got)
	}

	// Verify other values are preserved
	if got := ctx.Value(ContextKey("step1")); got != "value1" {
		t.Errorf("Step1 value not preserved, got %v", got)
	}
	if got := ctx.Value(ContextKey("step2")); got != "value2" {
		t.Errorf("Step2 value not preserved, got %v", got)
	}
}

func TestCheckCancellation(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		expectedReason ContextCancellationReason
		expectError    bool
	}{
		{
			name:           "non-cancelled context",
			ctx:            context.Background(),
			expectedReason: "",
			expectError:    false,
		},
		{
			name:           "cancelled context",
			ctx:            func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(),
			expectedReason: ReasonCanceled,
			expectError:    true,
		},
		{
			name: "deadline exceeded context",
			ctx: func() context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), -1*time.Second)
				defer cancel()
				return ctx
			}(),
			expectedReason: ReasonDeadlineExceeded,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, err := CheckCancellation(tt.ctx)

			if tt.expectError && err == nil {
				t.Error("CheckCancellation() expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("CheckCancellation() unexpected error: %v", err)
			}

			if reason != tt.expectedReason {
				t.Errorf("CheckCancellation() reason = %v, want %v", reason, tt.expectedReason)
			}
		})
	}
}

func TestRequestIDWithCancel(t *testing.T) {
	// Create a context with request ID and cancellation
	baseCtx, cancel := context.WithCancel(context.Background())
	ctx := WithRequestID(baseCtx, "cancel-test")

	// Verify request ID is accessible before cancellation
	if got := RequestIDFromContext(ctx); got != "cancel-test" {
		t.Errorf("Before cancel: got %v, want cancel-test", got)
	}

	// Cancel the context
	cancel()

	// Request ID should still be accessible after cancellation
	// (the context values are not affected by cancellation)
	if got := RequestIDFromContext(ctx); got != "cancel-test" {
		t.Errorf("After cancel: got %v, want cancel-test", got)
	}

	// But CheckCancellation should detect the cancellation
	_, err := CheckCancellation(ctx)
	if err == nil {
		t.Error("CheckCancellation() should detect cancelled context")
	}
}

func TestRequestIDWithTimeout(t *testing.T) {
	// Create a context with request ID and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ctx = WithRequestID(ctx, "timeout-test")

	// Verify request ID is accessible before timeout
	if got := RequestIDFromContext(ctx); got != "timeout-test" {
		t.Errorf("Before timeout: got %v, want timeout-test", got)
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Request ID should still be accessible after timeout
	if got := RequestIDFromContext(ctx); got != "timeout-test" {
		t.Errorf("After timeout: got %v, want timeout-test", got)
	}

	// But CheckCancellation should detect the deadline exceeded
	reason, err := CheckCancellation(ctx)
	if err == nil {
		t.Error("CheckCancellation() should detect timed out context")
	}
	if reason != ReasonDeadlineExceeded {
		t.Errorf("Reason = %v, want %v", reason, ReasonDeadlineExceeded)
	}
}

func TestContextKeyTypeSafety(t *testing.T) {
	// Test that using different ContextKey types doesn't interfere
	type customKey string
	const customRequestIDKey customKey = "request_id"

	ctx1 := WithRequestID(context.Background(), "req-001")
	ctx2 := context.WithValue(context.Background(), customRequestIDKey, "req-002")

	// The two keys should not interfere
	if got := RequestIDFromContext(ctx1); got != "req-001" {
		t.Errorf("Context 1: got %v, want req-001", got)
	}

	if got := RequestIDFromContext(ctx2); got != "" {
		t.Errorf("Context 2: got %v, want empty (key type mismatch)", got)
	}
}

func TestRequestIDConcurrentAccess(t *testing.T) {
	// Test concurrent access to request ID from multiple goroutines
	ctx := WithRequestID(context.Background(), "concurrent-test")
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- true }()
			if got := RequestIDFromContext(ctx); got != "concurrent-test" {
				t.Errorf("Concurrent access: got %v, want concurrent-test", got)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines; i++ {
		<-done
	}
}
