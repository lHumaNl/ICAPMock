package processor

import (
	"context"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestEchoProcessor_Process tests the Process method of EchoProcessor.
func TestEchoProcessor_Process(t *testing.T) {
	processor := NewEchoProcessor()

	tests := []struct {
		name   string
		method string
		uri    string
	}{
		{
			name:   "REQMOD request",
			method: icap.MethodREQMOD,
			uri:    "icap://localhost/avscan",
		},
		{
			name:   "RESPMOD request",
			method: icap.MethodRESPMOD,
			uri:    "icap://localhost/avscan",
		},
		{
			name:   "OPTIONS request",
			method: icap.MethodOPTIONS,
			uri:    "icap://localhost/avscan",
		},
		{
			name:   "REQMOD with different service",
			method: icap.MethodREQMOD,
			uri:    "icap://localhost/url-filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := icap.NewRequest(tt.method, tt.uri)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			resp, err := processor.Process(context.Background(), req)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("expected status %d, got %d", icap.StatusNoContentNeeded, resp.StatusCode)
			}
			if resp.Proto != icap.Version {
				t.Errorf("expected proto %q, got %q", icap.Version, resp.Proto)
			}
		})
	}
}

// TestEchoProcessor_Name tests the Name method.
func TestEchoProcessor_Name(t *testing.T) {
	processor := NewEchoProcessor()
	expected := "EchoProcessor"

	if processor.Name() != expected {
		t.Errorf("expected name %q, got %q", expected, processor.Name())
	}
}

// TestEchoProcessor_ThreadSafety tests that EchoProcessor is thread-safe.
func TestEchoProcessor_ThreadSafety(t *testing.T) {
	processor := NewEchoProcessor()
	ctx := context.Background()

	// Run multiple goroutines concurrently
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
			resp, err := processor.Process(ctx, req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("unexpected status: %d", resp.StatusCode)
			}
			done <- true
		}()
	}

	// Wait for all goroutines with timeout
	timeout := time.After(5 * time.Second)
	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
			// OK
		case <-timeout:
			t.Fatal("timeout waiting for goroutines")
		}
	}
}

// TestEchoProcessor_ContextCancellation tests context cancellation handling.
func TestEchoProcessor_ContextCancellation(t *testing.T) {
	processor := NewEchoProcessor()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")

	// EchoProcessor should still return 204 even with cancelled context
	// since it's a fast, non-blocking operation
	resp, err := processor.Process(ctx, req)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Errorf("expected status %d, got %d", icap.StatusNoContentNeeded, resp.StatusCode)
	}
}

// TestEchoProcessor_Interface verifies EchoProcessor implements Processor interface.
func TestEchoProcessor_Interface(t *testing.T) {
	// This is a compile-time check
	var _ Processor = NewEchoProcessor()
}
