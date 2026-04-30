// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestMultiServerOptionsUsesEntryEffectiveValues(t *testing.T) {
	cfg := multiServerOptionsConfig()
	entries := buildServerEntries(cfg)

	assertOptionsHeaders(t, entries[0], "av-service", 7)
	assertOptionsHeaders(t, entries[1], "global-service", 100)
}

func TestBuildServerEntriesSortsServerNames(t *testing.T) {
	entries := buildServerEntries(multiServerOptionsConfig())

	if entries[0].name != "av" || entries[1].name != "zproxy" {
		t.Fatalf("server order = %q, %q; want av, zproxy", entries[0].name, entries[1].name)
	}
}

func TestBuildServerEntriesSingleServerEmptyNameDefaultsToDefault(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 1344},
		Mock:   config.MockConfig{ServiceID: "global-service"},
	}

	entries := buildServerEntries(cfg)

	if len(entries) != 1 {
		t.Fatalf("entries count = %d, want 1", len(entries))
	}
	if entries[0].name != "default" {
		t.Fatalf("entry name = %q, want default", entries[0].name)
	}
}

func TestMultiServerCLIOverridesApplyToRuntimeEntries(t *testing.T) {
	cfg := multiServerOptionsConfig()
	cmd := NewServerCommand()
	err := cmd.Parse([]string{
		"--server.host", "127.0.0.1",
		"--server.port", "9444",
		"--server.max-connections", "42",
		"--server.max-body-size", "1234",
		"--server.read-timeout", "7s",
		"--server.write-timeout", "8s",
		"--server.shutdown-timeout", "9s",
		"--server.streaming=false",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cmd.applyOverrides(cfg)
	entries := buildServerEntries(cfg)
	for _, entry := range entries {
		assertServerCLIOverrides(t, entry.serverCfg)
	}
}

func multiServerOptionsConfig() *config.Config {
	return &config.Config{
		Mock: config.MockConfig{ServiceID: "global-service"},
		Defaults: config.DefaultsConfig{
			Host:           "0.0.0.0",
			MaxConnections: 100,
			MaxBodySize:    10 * 1024 * 1024,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
		},
		Server: config.ServerConfig{MaxConnections: 999},
		Servers: map[string]config.ServerEntryConfig{
			"zproxy": {Port: 1345, Host: "10.0.0.2"},
			"av":     {Port: 1344, Host: "10.0.0.1", ServiceID: "av-service", MaxConnections: 7},
		},
	}
}

func assertServerCLIOverrides(t *testing.T, got config.ServerConfig) {
	t.Helper()
	if got.Host != "127.0.0.1" || got.Port != 9444 {
		t.Fatalf("endpoint = %s:%d, want 127.0.0.1:9444", got.Host, got.Port)
	}
	if got.MaxConnections != 42 || got.MaxBodySize != 1234 {
		t.Fatalf("limits = %d/%d, want 42/1234", got.MaxConnections, got.MaxBodySize)
	}
	if got.ReadTimeout != 7*time.Second || got.WriteTimeout != 8*time.Second {
		t.Fatalf("timeouts = %v/%v, want 7s/8s", got.ReadTimeout, got.WriteTimeout)
	}
	if got.ShutdownTimeout != 9*time.Second || got.Streaming {
		t.Fatalf("shutdown/streaming = %v/%v, want 9s/false", got.ShutdownTimeout, got.Streaming)
	}
}

func assertOptionsHeaders(t *testing.T, entry serverEntry, serviceID string, maxConnections int) {
	t.Helper()
	resp := optionsResponse(t, optionsHandlerConfig(entry))
	assertHeader(t, resp, "Service-ID", serviceID)
	assertHeader(t, resp, "Max-Connections", strconv.Itoa(maxConnections))
}

func optionsResponse(t *testing.T, cfg handler.OptionsHandlerConfig) *icap.Response {
	t.Helper()
	req, err := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/options")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := handler.NewOptionsHandler(cfg).Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("OPTIONS Handle() error = %v", err)
	}
	return resp
}

func assertHeader(t *testing.T, resp *icap.Response, name, want string) {
	t.Helper()
	got, ok := resp.GetHeader(name)
	if !ok || got != want {
		t.Fatalf("%s = %q (present %v), want %q", name, got, ok, want)
	}
}
