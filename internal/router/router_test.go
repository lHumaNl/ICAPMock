// Copyright 2026 ICAP Mock

package router

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// mockHandler is a test handler implementation.
type mockHandler struct {
	err     error
	lastReq atomic.Pointer[icap.Request]
	resp    *icap.Response
	method  string
	called  atomic.Bool
}

// Handle implements Handler interface.
func (h *mockHandler) Handle(_ context.Context, req *icap.Request) (*icap.Response, error) {
	h.called.Store(true)
	h.lastReq.Store(req)
	if h.err != nil {
		return nil, h.err
	}
	if h.resp != nil {
		return h.resp, nil
	}
	return icap.NewResponse(icap.StatusOK), nil
}

// Method implements Handler interface.
func (h *mockHandler) Method() string {
	return h.method
}

// TestNewRouter tests creating a new router.
func TestNewRouter(t *testing.T) {
	r := NewRouter()
	if r == nil {
		t.Fatal("NewRouter() returned nil")
	}

	// New router should have no routes
	routes := r.Routes()
	if len(routes) != 0 {
		t.Errorf("New router should have no routes, got %d", len(routes))
	}
}

// TestRouter_Handle tests registering a handler.
func TestRouter_Handle(t *testing.T) {
	r := NewRouter()
	hdlr := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Fatalf("Expected 1 route, got %d", len(routes))
	}

	if routes[0].Path != "/reqmod" {
		t.Errorf("Route path = %q, want %q", routes[0].Path, "/reqmod")
	}
}

// TestRouter_Handle_EmptyPath tests that empty path returns error.
func TestRouter_Handle_EmptyPath(t *testing.T) {
	r := NewRouter()
	hdlr := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("", hdlr)
	if err == nil {
		t.Error("Handle() with empty path should return error")
	}
}

// TestRouter_Handle_NilHandler tests that nil handler returns error.
func TestRouter_Handle_NilHandler(t *testing.T) {
	r := NewRouter()

	err := r.Handle("/reqmod", nil)
	if err == nil {
		t.Error("Handle() with nil handler should return error")
	}
}

// TestRouter_Handle_DuplicatePath tests registering duplicate paths.
func TestRouter_Handle_DuplicatePath(t *testing.T) {
	r := NewRouter()
	handler1 := &mockHandler{method: icap.MethodREQMOD}
	handler2 := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", handler1)
	if err != nil {
		t.Fatalf("First Handle() error = %v", err)
	}

	// Registering duplicate should overwrite
	err = r.Handle("/reqmod", handler2)
	if err != nil {
		t.Fatalf("Second Handle() error = %v", err)
	}

	// Verify only one route exists
	routes := r.Routes()
	if len(routes) != 1 {
		t.Errorf("Expected 1 route after duplicate registration, got %d", len(routes))
	}
}

// TestRouter_HandleFunc tests registering a handler function.
func TestRouter_HandleFunc(t *testing.T) {
	r := NewRouter()
	called := false

	err := r.HandleFunc("/reqmod", func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		called = true
		return icap.NewResponse(icap.StatusOK), nil
	})
	if err != nil {
		t.Fatalf("HandleFunc() error = %v", err)
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Fatalf("Expected 1 route, got %d", len(routes))
	}

	if routes[0].Path != "/reqmod" {
		t.Errorf("Route path = %q, want %q", routes[0].Path, "/reqmod")
	}

	// Verify the handler works
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	_, err = r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	if !called {
		t.Error("Handler function was not called")
	}
}

// TestRouter_HandleFunc_EmptyPath tests HandleFunc with empty path.
func TestRouter_HandleFunc_EmptyPath(t *testing.T) {
	r := NewRouter()

	err := r.HandleFunc("", func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	})
	if err == nil {
		t.Error("HandleFunc() with empty path should return error")
	}
}

// TestRouter_HandleFunc_NilFunc tests HandleFunc with nil function.
func TestRouter_HandleFunc_NilFunc(t *testing.T) {
	r := NewRouter()

	err := r.HandleFunc("/reqmod", nil)
	if err == nil {
		t.Error("HandleFunc() with nil function should return error")
	}
}

// TestRouter_Serve tests serving requests.
func TestRouter_Serve(t *testing.T) {
	r := NewRouter()
	hdlr := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	resp, err := r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	if !hdlr.called.Load() {
		t.Error("Handler was not called")
	}

	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	if hdlr.lastReq.Load() != req {
		t.Error("Handler did not receive the request")
	}
}

// TestRouter_Serve_NotFound tests 404 for unknown paths.
func TestRouter_Serve_NotFound(t *testing.T) {
	r := NewRouter()
	hdlr := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/unknown")
	resp, err := r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	if resp.StatusCode != icap.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d (404)", resp.StatusCode, icap.StatusNotFound)
	}

	if hdlr.called.Load() {
		t.Error("Handler should not be called for unknown path")
	}
}

// TestRouter_Serve_CustomHandlerResponse tests custom handler response.
func TestRouter_Serve_CustomHandlerResponse(t *testing.T) {
	r := NewRouter()
	customResp := icap.NewResponse(icap.StatusNoContentNeeded)
	customResp.SetHeader("X-Custom", "value")

	hdlr := &mockHandler{
		method: icap.MethodREQMOD,
		resp:   customResp,
	}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	resp, err := r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
	}

	if val, ok := resp.GetHeader("X-Custom"); !ok || val != "value" {
		t.Errorf("X-Custom header = %q, want %q", val, "value")
	}
}

// TestRouter_Serve_MultipleRoutes tests routing to multiple handlers.
func TestRouter_Serve_MultipleRoutes(t *testing.T) {
	r := NewRouter()

	reqmodHandler := &mockHandler{method: icap.MethodREQMOD}
	respmodHandler := &mockHandler{method: icap.MethodRESPMOD}
	optionsHandler := &mockHandler{method: icap.MethodOPTIONS}

	if err := r.Handle("/reqmod", reqmodHandler); err != nil {
		t.Fatalf("Handle() reqmod error = %v", err)
	}
	if err := r.Handle("/respmod", respmodHandler); err != nil {
		t.Fatalf("Handle() respmod error = %v", err)
	}
	if err := r.Handle("/options", optionsHandler); err != nil {
		t.Fatalf("Handle() options error = %v", err)
	}

	// Test REQMOD
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	r.Serve(context.Background(), req)
	if !reqmodHandler.called.Load() {
		t.Error("REQMOD handler not called")
	}

	// Test RESPMOD
	req, _ = icap.NewRequest(icap.MethodRESPMOD, "icap://localhost:1344/respmod")
	r.Serve(context.Background(), req)
	if !respmodHandler.called.Load() {
		t.Error("RESPMOD handler not called")
	}

	// Test OPTIONS
	req, _ = icap.NewRequest(icap.MethodOPTIONS, "icap://localhost:1344/options")
	r.Serve(context.Background(), req)
	if !optionsHandler.called.Load() {
		t.Error("OPTIONS handler not called")
	}
}

// TestRouter_Routes tests getting all routes.
func TestRouter_Routes(t *testing.T) {
	r := NewRouter()

	handlers := map[string]*mockHandler{
		"/reqmod":  {method: icap.MethodREQMOD},
		"/respmod": {method: icap.MethodRESPMOD},
		"/options": {method: icap.MethodOPTIONS},
	}

	for path, h := range handlers {
		if err := r.Handle(path, h); err != nil {
			t.Fatalf("Handle() %s error = %v", path, err)
		}
	}

	routes := r.Routes()
	if len(routes) != 3 {
		t.Fatalf("Expected 3 routes, got %d", len(routes))
	}

	// Verify all paths are present
	routeMap := make(map[string]bool)
	for _, route := range routes {
		routeMap[route.Path] = true
	}

	for path := range handlers {
		if !routeMap[path] {
			t.Errorf("Route %s not found in Routes()", path)
		}
	}
}

// TestRouter_ConcurrentAccess tests thread-safe route registration and serving.
func TestRouter_ConcurrentAccess(_ *testing.T) {
	r := NewRouter()
	var wg sync.WaitGroup

	// Concurrent route registration
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/path" + string(rune('0'+i%10))
			h := &mockHandler{method: icap.MethodREQMOD}
			r.Handle(path, h)
		}(i)
	}

	// Concurrent route serving
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/path" + string(rune('0'+i%10))
			req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344"+path)
			r.Serve(context.Background(), req)
		}(i)
	}

	// Concurrent route listing
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Routes()
		}()
	}

	wg.Wait()
	// If we get here without race condition, test passes
}

// TestRouter_Serve_InvalidURI tests handling of invalid URIs.
func TestRouter_Serve_InvalidURI(t *testing.T) {
	r := NewRouter()
	hdlr := &mockHandler{method: icap.MethodREQMOD}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Create request manually with malformed URI
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "invalid-uri", // Missing icap:// scheme
	}

	resp, err := r.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	// Should return 404 for unknown path
	if resp.StatusCode != icap.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNotFound)
	}
}

// TestRouter_Serve_ExtractPath tests path extraction from various URI formats.
func TestRouter_Serve_ExtractPath(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantPath string
	}{
		{
			name:     "standard path",
			uri:      "icap://localhost:1344/reqmod",
			wantPath: "/reqmod",
		},
		{
			name:     "with query string",
			uri:      "icap://localhost:1344/reqmod?foo=bar",
			wantPath: "/reqmod",
		},
		{
			name:     "nested path",
			uri:      "icap://localhost:1344/api/v1/reqmod",
			wantPath: "/api/v1/reqmod",
		},
		{
			name:     "root path",
			uri:      "icap://localhost:1344/",
			wantPath: "/",
		},
		{
			name:     "custom port",
			uri:      "icap://server.example.com:8080/custom-service",
			wantPath: "/custom-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter()
			hdlr := &mockHandler{method: icap.MethodREQMOD}

			err := r.Handle(tt.wantPath, hdlr)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			req, _ := icap.NewRequest(icap.MethodREQMOD, tt.uri)
			resp, err := r.Serve(context.Background(), req)
			if err != nil {
				t.Fatalf("Serve() error = %v", err)
			}

			if !hdlr.called.Load() {
				t.Errorf("Handler not called for URI %s (expected path %s)", tt.uri, tt.wantPath)
			}

			if resp.StatusCode != icap.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
			}
		})
	}
}

// TestHandlerFunc tests the handler.HandlerFunc type used via router.HandleFunc.
func TestHandlerFunc(t *testing.T) {
	called := false

	hf := handler.HandlerFunc(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		called = true
		return icap.NewResponse(icap.StatusOK), nil
	})

	// Wrap it to create a full handler.Handler
	h := handler.WrapHandler(hf, "")

	// Test Handle method
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/test")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if !called {
		t.Error("HandlerFunc was not called")
	}

	if resp.StatusCode != icap.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}

	// Test Method method (should return empty for generic hdlr)
	if h.Method() != "" {
		t.Errorf("Handler.Method() = %q, want empty", h.Method())
	}
}

// TestRouter_Serve_ContextCancellation tests that context cancellation is handled.
func TestRouter_Serve_ContextCancellation(t *testing.T) {
	r := NewRouter()

	// Handler that respects context
	err := r.HandleFunc("/slow", func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return icap.NewResponse(icap.StatusOK), nil
		}
	})
	if err != nil {
		t.Fatalf("HandleFunc() error = %v", err)
	}

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/slow")
	_, err = r.Serve(ctx, req)

	// The handler returns ctx.Err() which is context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// TestRouter_Serve_Error tests handler returning error.
func TestRouter_Serve_Error(t *testing.T) {
	r := NewRouter()

	hdlr := &mockHandler{
		method: icap.MethodREQMOD,
		err:    context.DeadlineExceeded,
	}

	err := r.Handle("/reqmod", hdlr)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost:1344/reqmod")
	_, err = r.Serve(context.Background(), req)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded error, got %v", err)
	}
}
