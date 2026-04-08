// Copyright 2026 ICAP Mock

package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestHandlerInterface tests that the Handler interface is correctly defined.
func TestHandlerInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler handler.Handler
		method  string
	}{
		{
			name: "mock handler returns correct method",
			handler: handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
				return icap.NewResponse(icap.StatusOK), nil
			}, "REQMOD"),
			method: "REQMOD",
		},
		{
			name: "mock handler returns RESPMOD",
			handler: handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
				return icap.NewResponse(icap.StatusOK), nil
			}, "RESPMOD"),
			method: "RESPMOD",
		},
		{
			name: "mock handler returns OPTIONS",
			handler: handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
				return icap.NewResponse(icap.StatusOK), nil
			}, "OPTIONS"),
			method: "OPTIONS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.handler.Method(); got != tt.method {
				t.Errorf("Handler.Method() = %q, want %q", got, tt.method)
			}
		})
	}
}

// TestHandlerFunc tests the HandlerFunc adapter.
func TestHandlerFunc(t *testing.T) {
	t.Parallel()

	t.Run("HandleFunc processes request", func(t *testing.T) {
		expectedResp := icap.NewResponse(icap.StatusOK)
		called := false

		hf := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			called = true
			return expectedResp, nil
		}, "REQMOD")

		req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := hf.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if !called {
			t.Error("Handler function was not called")
		}

		if resp != expectedResp {
			t.Errorf("Handle() returned wrong response")
		}
	})

	t.Run("HandleFunc propagates error", func(t *testing.T) {
		expectedErr := errors.New("test error")

		hf := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return nil, expectedErr
		}, "REQMOD")

		req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = hf.Handle(context.Background(), req)
		if !errors.Is(err, expectedErr) {
			t.Errorf("Handle() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("HandleFunc respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		hf := handler.WrapHandler(func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return icap.NewResponse(icap.StatusOK), nil
		}, "REQMOD")

		req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = hf.Handle(ctx, req)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Handle() error = %v, want %v", err, context.Canceled)
		}
	})
}

// TestWrapHandler tests the WrapHandler utility function.
func TestWrapHandler(t *testing.T) {
	t.Parallel()

	t.Run("WrapHandler wraps function correctly", func(t *testing.T) {
		expectedResp := icap.NewResponse(icap.StatusNoContentNeeded)

		h := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return expectedResp, nil
		}, "TEST")

		if h.Method() != "TEST" {
			t.Errorf("Method() = %q, want %q", h.Method(), "TEST")
		}

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if resp.StatusCode != icap.StatusNoContentNeeded {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
		}
	})
}

// TestHandlerChain tests chaining multiple handlers.
func TestHandlerChain(t *testing.T) {
	t.Parallel()

	t.Run("Chain returns first non-nil response", func(t *testing.T) {
		resp1 := icap.NewResponse(icap.StatusOK)
		resp2 := icap.NewResponse(icap.StatusNoContentNeeded)

		h1 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return nil, nil // Skip
		}, "REQMOD")

		h2 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return resp1, nil
		}, "REQMOD")

		h3 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return resp2, nil
		}, "REQMOD")

		chain := handler.Chain(h1, h2, h3)

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		resp, err := chain.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if resp.StatusCode != icap.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
		}
	})

	t.Run("Chain returns 204 when all return nil", func(t *testing.T) {
		h1 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return nil, nil
		}, "REQMOD")

		h2 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return nil, nil
		}, "REQMOD")

		chain := handler.Chain(h1, h2)

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		resp, err := chain.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if resp.StatusCode != icap.StatusNoContentNeeded {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
		}
	})

	t.Run("Chain propagates error", func(t *testing.T) {
		expectedErr := errors.New("handler error")

		h1 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return nil, expectedErr
		}, "REQMOD")

		h2 := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(icap.StatusOK), nil
		}, "REQMOD")

		chain := handler.Chain(h1, h2)

		req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
		_, err := chain.Handle(context.Background(), req)
		if !errors.Is(err, expectedErr) {
			t.Errorf("Handle() error = %v, want %v", err, expectedErr)
		}
	})
}
