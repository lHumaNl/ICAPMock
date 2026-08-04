// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestExactRouteDispatchUsesActualICAPMethodForGlobalFallback(t *testing.T) {
	reqmod := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(211), nil
	}, "")
	respmod := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(212), nil
	}, "")
	options := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(213), nil
	}, "")
	dispatch := newMethodDispatchHandler(
		map[string]bool{icap.MethodRESPMOD: true},
		reqmod,
		respmod,
		options,
	)
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/resp-only")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := dispatch.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.StatusCode != 211 {
		t.Fatalf("StatusCode = %d, want REQMOD handler status 211", resp.StatusCode)
	}
}

func TestReservedPathDispatchUsesActualICAPMethod(t *testing.T) {
	reqmod := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(211), nil
	}, "")
	respmod := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(212), nil
	}, "")
	options := handler.WrapHandler(func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(213), nil
	}, "")
	dispatch := newMethodDispatchHandler(
		map[string]bool{icap.MethodREQMOD: true}, reqmod, respmod, options,
	)
	req, err := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/reqmod")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := dispatch.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.StatusCode != 212 {
		t.Fatalf("StatusCode = %d, want RESPMOD handler status 212", resp.StatusCode)
	}
}

func TestMatchExplainerUsesExactRoutePair(t *testing.T) {
	scenario := &storage.Scenario{
		Name: "exact",
		Match: storage.MatchRule{
			Routes: storage.RouteMap{
				icap.MethodREQMOD:  {"/req"},
				icap.MethodRESPMOD: {"/resp"},
			},
			Methods: storage.MethodList{icap.MethodREQMOD, icap.MethodRESPMOD},
			Paths:   []string{"/req", "/resp"},
		},
	}
	for _, tc := range []struct {
		method string
		path   string
		want   bool
	}{
		{icap.MethodREQMOD, "/req", true},
		{icap.MethodREQMOD, "/req?trace=1", true},
		{icap.MethodREQMOD, "/resp", false},
		{icap.MethodRESPMOD, "/resp", true},
	} {
		req, err := icap.NewRequest(tc.method, "icap://localhost"+tc.path)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		if got := explainMatch(scenario, req).matched; got != tc.want {
			t.Errorf("explainMatch(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRegisterHandlersSupportsExactLiteralCaptureRegexAndOverlapRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yaml")
	data := []byte(`
scenarios:
  literal:
    routes: {REQMOD: /literal}
    status: 204
  capture:
    routes: {REQMOD: "/tenant/{id}"}
    status: 204
  raw-regex:
    routes: {RESPMOD: "re:^/raw/[0-9]+$"}
    status: 204
  overlap-first:
    routes: {REQMOD: "/overlap/{id}"}
    status: 204
  overlap-second:
    routes: {RESPMOD: "re:^/overlap/[^/]+$"}
    status: 204
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	registry := storage.NewScenarioRegistry()
	if err := registry.Load(path); err != nil {
		t.Fatalf("registry.Load() error = %v", err)
	}
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "debug", Format: "text"}, io.Discard)
	if err != nil {
		t.Fatalf("logger.NewWithWriter() error = %v", err)
	}
	rtr := router.NewRouter()
	if err := registerHandlers(
		rtr,
		routeTestProcessor{},
		nil,
		nil,
		log,
		registry,
		serverEntry{name: "test", serverCfg: config.ServerConfig{MaxConnections: 10}},
	); err != nil {
		t.Fatalf("registerHandlers() error = %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
	}{
		{icap.MethodREQMOD, "/literal"},
		{icap.MethodREQMOD, "/tenant/acme"},
		{icap.MethodRESPMOD, "/raw/42"},
		{icap.MethodRESPMOD, "/overlap/value"},
	} {
		req, requestErr := icap.NewRequest(tc.method, "icap://localhost"+tc.path)
		if requestErr != nil {
			t.Fatalf("NewRequest() error = %v", requestErr)
		}
		resp, serveErr := rtr.Serve(context.Background(), req)
		if serveErr != nil {
			t.Fatalf("Serve(%s %s) error = %v", tc.method, tc.path, serveErr)
		}
		if resp.StatusCode != icap.StatusNoContentNeeded {
			t.Errorf("Serve(%s %s) status = %d, want 204", tc.method, tc.path, resp.StatusCode)
		}
	}
}

type routeTestProcessor struct{}

func (routeTestProcessor) Process(_ context.Context, _ *icap.Request) (*icap.Response, error) {
	return icap.NewResponse(icap.StatusNoContentNeeded), nil
}

func (routeTestProcessor) Name() string { return "route-test" }
