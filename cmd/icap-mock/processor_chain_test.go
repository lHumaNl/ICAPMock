// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestCreateProcessorChainUsesEntryMaxBodySize(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Server.MaxBodySize = 1024
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(streamRequestBodyScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc, cleanup := createProcessorChain(cfg, registry, newTestIntegrationLogger(t), 8)
	defer cleanup(context.Background())

	resp, err := proc.Process(context.Background(), oversizedStreamRequest())
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	_, err = resp.WriteTo(&bytes.Buffer{})
	if !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("WriteTo() error = %v, want per-server max_body_size error", err)
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
