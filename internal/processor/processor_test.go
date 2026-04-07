// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"errors"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestProcessorInterface verifies that the Processor interface is correctly defined
// and can be implemented by different types.
func TestProcessorInterface(t *testing.T) {
	tests := []struct {
		processor Processor
		name      string
	}{
		{
			name:      "ProcessorFunc",
			processor: ProcessorFunc(mockProcessFunc),
		},
		{
			name:      "ChainProcessor",
			processor: Chain(ProcessorFunc(mockProcessFunc)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify interface methods exist and work
			name := tt.processor.Name()
			if name == "" {
				t.Error("Name() returned empty string")
			}

			req := createTestRequest(t)
			ctx := context.Background()

			resp, err := tt.processor.Process(ctx, req)
			if err != nil {
				t.Errorf("Process() returned unexpected error: %v", err)
			}
			if resp == nil {
				t.Error("Process() returned nil response")
			}
		})
	}
}

// TestProcessorFunc tests the ProcessorFunc adapter.
func TestProcessorFunc(t *testing.T) {
	t.Run("returns response", func(t *testing.T) {
		expectedStatus := 204
		p := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(expectedStatus), nil
		})

		req := createTestRequest(t)
		resp, err := p.Process(context.Background(), req)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != expectedStatus {
			t.Errorf("expected status %d, got %d", expectedStatus, resp.StatusCode)
		}
	})

	t.Run("returns error", func(t *testing.T) {
		expectedErr := context.Canceled
		p := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return nil, expectedErr
		})

		req := createTestRequest(t)
		_, err := p.Process(context.Background(), req)

		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})

	t.Run("Name returns ProcessorFunc", func(t *testing.T) {
		p := ProcessorFunc(mockProcessFunc)
		if p.Name() != "ProcessorFunc" {
			t.Errorf("expected name 'ProcessorFunc', got %q", p.Name())
		}
	})
}

// TestChainProcessor tests the Chain processor.
func TestChainProcessor(t *testing.T) {
	t.Run("returns first non-nil response", func(t *testing.T) {
		p1 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return nil, nil // pass through
		})
		p2 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(200), nil // return response
		})
		p3 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(500), nil // should not be called
		})

		chain := Chain(p1, p2, p3)
		req := createTestRequest(t)
		resp, err := chain.Process(context.Background(), req)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("returns error from processor", func(t *testing.T) {
		expectedErr := context.DeadlineExceeded
		p1 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return nil, expectedErr
		})

		chain := Chain(p1)
		req := createTestRequest(t)
		_, err := chain.Process(context.Background(), req)

		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
	})

	t.Run("returns 204 when all processors pass through", func(t *testing.T) {
		p1 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return nil, nil
		})
		p2 := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
			return nil, nil
		})

		chain := Chain(p1, p2)
		req := createTestRequest(t)
		resp, err := chain.Process(context.Background(), req)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != 204 {
			t.Errorf("expected status 204, got %d", resp.StatusCode)
		}
	})

	t.Run("Name returns ChainProcessor", func(t *testing.T) {
		chain := Chain()
		if chain.Name() != "ChainProcessor" {
			t.Errorf("expected name 'ChainProcessor', got %q", chain.Name())
		}
	})
}

// Helper functions

func mockProcessFunc(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return icap.NewResponse(icap.StatusNoContentNeeded), nil
}

func createTestRequest(t *testing.T) *icap.Request {
	t.Helper()
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/avscan")
	if err != nil {
		t.Fatalf("failed to create test request: %v", err)
	}
	return req
}
