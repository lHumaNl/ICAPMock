// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestCreateProcessorChainFallsBackToEchoWhenNoScenarioMatches(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	registry := storage.NewScenarioRegistry()
	proc, cleanup := createProcessorChain(cfg, registry, newTestIntegrationLogger(t), nil, "default", 0)
	defer cleanup(context.Background())

	resp, err := proc.Process(context.Background(), requestWithHTTPBody("payload"))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusNoContentNeeded)
	}
	if resp.HTTPResponse != nil {
		t.Fatal("HTTPResponse is set, want echo fallback without scenario response")
	}
}

func TestCreateProcessorChainMockModeUsesScenarioMatching(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(blockingREQMODScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc, cleanup := createProcessorChain(cfg, registry, newTestIntegrationLogger(t), nil, "default", 0)
	defer cleanup(context.Background())

	resp, err := proc.Process(context.Background(), requestWithHTTPBody("payload"))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if resp.StatusCode != icap.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
	}
	if resp.HTTPResponse == nil || resp.HTTPResponse.Status != "403" {
		t.Fatalf("HTTPResponse = %#v, want 403 response", resp.HTTPResponse)
	}
}

func TestCreateStorageStackWiresMiddlewareMetrics(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Storage.Enabled = true
	cfg.Storage.RequestsDir = t.TempDir()
	_, collector, err := createMetricsCollector()
	if err != nil {
		t.Fatalf("createMetricsCollector() error = %v", err)
	}
	store, storageMW, err := createStorageStack(cfg, collector, newTestIntegrationLogger(t))
	if err != nil {
		t.Fatalf("createStorageStack() error = %v", err)
	}
	t.Cleanup(func() {
		if storageMW != nil {
			_ = storageMW.Shutdown(context.Background())
		}
		if store != nil {
			_ = store.Close()
		}
	})

	if storageMW == nil {
		t.Fatal("storage middleware is nil")
	}
	metricsField := reflect.ValueOf(storageMW).Elem().FieldByName("metrics")
	if metricsField.IsNil() {
		t.Fatal("storage middleware metrics collector is nil")
	}
}

func TestCreateProcessorChainUsesEntryMaxBodySize(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Server.MaxBodySize = 1024
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(streamRequestBodyScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc, cleanup := createProcessorChain(cfg, registry, newTestIntegrationLogger(t), nil, "default", 8)
	defer cleanup(context.Background())

	resp, err := proc.Process(context.Background(), oversizedStreamRequest())
	if err == nil {
		t.Fatal("Process() error = nil, want per-server body limit error")
	}
	if resp != nil {
		t.Fatalf("Process() response = %v, want nil", resp)
	}
}

func TestCreateProcessorChainAppliesMockDefaultTimeout(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Mock.DefaultTimeout = 10 * time.Millisecond
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(delayedREQMODScenario(100 * time.Millisecond)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc, cleanup := createProcessorChain(cfg, registry, newTestIntegrationLogger(t), nil, "default", 0)
	defer cleanup(context.Background())

	resp, err := proc.Process(context.Background(), requestWithHTTPBody("payload"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Process() error = %v, want context deadline exceeded", err)
	}
	if resp != nil {
		t.Fatalf("Process() response = %v, want nil", resp)
	}
}

func streamRequestBodyScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:     "stream-request-body",
		Match:    storage.MatchRule{Methods: storage.MethodList{icap.MethodREQMOD}},
		Priority: 100,
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			HTTPStatus: 403,
			Stream: &storage.StreamConfig{
				Source: storage.StreamSourceConfig{From: "request_http_body"},
				Chunks: storage.StreamChunksConfig{Size: storage.SizeSpec{Min: 1, Max: 1, IsSet: true}},
			},
		},
	}
}

func blockingREQMODScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:     "block-upload",
		Match:    storage.MatchRule{Methods: storage.MethodList{icap.MethodREQMOD}},
		Priority: 100,
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			HTTPStatus: 403,
		},
	}
}

func delayedREQMODScenario(delay time.Duration) *storage.Scenario {
	scenario := blockingREQMODScenario()
	scenario.Name = "delayed-upload"
	scenario.Response.Delay = delay
	return scenario
}

func oversizedStreamRequest() *icap.Request {
	return &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/scan",
		HTTPRequest: &icap.HTTPMessage{
			Method:     "POST",
			URI:        "http://example.test/scan",
			BodyReader: bytes.NewReader([]byte("0123456789")),
		},
	}
}
