// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestMockProcessor_Process tests the Process method of MockProcessor.
func TestMockProcessor_Process(t *testing.T) {
	log := createTestLogger(t)

	tests := []struct {
		scenario       *storage.Scenario
		name           string
		expectedStatus int
		expectError    bool
	}{
		{
			name: "returns scenario response status",
			scenario: &storage.Scenario{
				Name: "test-scenario",
				Match: storage.MatchRule{
					Methods: []string{icap.MethodREQMOD},
				},
				Response: storage.ResponseTemplate{
					ICAPStatus: 200,
					Headers:    map[string]string{"X-Custom": "value"},
				},
				Priority: 100,
			},
			expectedStatus: 200,
		},
		{
			name: "returns 204 for default scenario",
			scenario: &storage.Scenario{
				Name: "default-scenario",
				Match: storage.MatchRule{
					Methods: []string{icap.MethodREQMOD},
				},
				Response: storage.ResponseTemplate{
					ICAPStatus: 204,
				},
				Priority: 1,
			},
			expectedStatus: 204,
		},
		{
			name: "applies scenario delay",
			scenario: &storage.Scenario{
				Name: "delayed-scenario",
				Match: storage.MatchRule{
					Methods: []string{icap.MethodREQMOD},
				},
				Response: storage.ResponseTemplate{
					ICAPStatus: 200,
					Delay:      10 * time.Millisecond,
				},
				Priority: 100,
			},
			expectedStatus: 200,
		},
		{
			name: "returns error when scenario defines error",
			scenario: &storage.Scenario{
				Name: "error-scenario",
				Match: storage.MatchRule{
					Methods: []string{icap.MethodREQMOD},
				},
				Response: storage.ResponseTemplate{
					ICAPStatus: 500,
					Error:      "simulated error",
				},
				Priority: 100,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := storage.NewScenarioRegistry()
			if err := registry.Add(tt.scenario); err != nil {
				t.Fatalf("failed to add scenario: %v", err)
			}

			processor := NewMockProcessor(registry, log)
			req := createTestREQMODRequest(t)

			start := time.Now()
			resp, err := processor.Process(context.Background(), req)
			elapsed := time.Since(start)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			// Check delay was applied
			if tt.scenario.Response.Delay > 0 {
				if elapsed < tt.scenario.Response.Delay {
					t.Errorf("expected delay of %v, but only waited %v", tt.scenario.Response.Delay, elapsed)
				}
			}

			// Check headers
			for key, value := range tt.scenario.Response.Headers {
				if v, ok := resp.GetHeader(key); !ok || v != value {
					t.Errorf("expected header %s=%s, got %s", key, value, v)
				}
			}
		})
	}
}

func TestMockProcessor_RecordsScenarioMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(namedResponseScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc := NewMockProcessor(registry, createTestLogger(t))
	proc.SetMetrics(collector)

	_, err = proc.Process(context.Background(), createTestREQMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	count := scenarioRequestMetricValue(t, reg, "named-scenario", "clean")
	if count != 1 {
		t.Errorf("scenario request count = %v, want 1", count)
	}
}

func namedResponseScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:     "named-scenario",
		Match:    storage.MatchRule{Methods: []string{icap.MethodREQMOD}},
		Priority: 100,
		Response: storage.ResponseTemplate{
			ICAPStatus:   204,
			ResponseName: "clean",
		},
	}
}

func scenarioRequestMetricValue(t *testing.T, reg prometheus.Gatherer, scenario, response string) float64 {
	t.Helper()
	for _, mf := range gatherProcessorTestMetrics(t, reg) {
		if mf.GetName() != "icap_scenario_requests_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if metricHasLabels(metric, scenario, response) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func gatherProcessorTestMetrics(t *testing.T, reg prometheus.Gatherer) []*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	return mfs
}

func metricHasLabels(metric *dto.Metric, scenario, response string) bool {
	labels := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels["scenario"] == scenario && labels["response"] == response
}

// TestMockProcessor_NoMatch tests behavior when no scenario matches.
func TestMockProcessor_NoMatch(t *testing.T) {
	log := createTestLogger(t)

	// Create registry with a scenario that only matches RESPMOD
	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "respmod-only",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodRESPMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)

	// Create REQMOD request which won't match
	req := createTestREQMODRequest(t)
	_, err = processor.Process(context.Background(), req)

	// The default scenario should match (returns 204)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestMockProcessor_ContextCancellation tests context cancellation during delay.
func TestMockProcessor_ContextCancellation(t *testing.T) {
	log := createTestLogger(t)

	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "slow-scenario",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodREQMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			Delay:      5 * time.Second, // Long delay
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)
	req := createTestREQMODRequest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = processor.Process(ctx, req)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected context deadline exceeded error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	// Should return quickly due to cancellation, not wait full delay
	if elapsed > 500*time.Millisecond {
		t.Errorf("context cancellation took too long: %v", elapsed)
	}
}

// TestMockProcessor_ResponseBody tests response body handling.
func TestMockProcessor_ResponseBody(t *testing.T) {
	log := createTestLogger(t)

	expectedBody := "test response body"
	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "body-scenario",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodREQMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			Body:       expectedBody,
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)
	req := createTestREQMODRequest(t)

	resp, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(resp.Body) != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, string(resp.Body))
	}
}

// TestMockProcessor_HTTPHeadersModification tests HTTP header modification.
func TestMockProcessor_HTTPHeadersModification(t *testing.T) {
	log := createTestLogger(t)

	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "header-mod-scenario",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodREQMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus:  200,
			HTTPHeaders: map[string]string{"X-Modified": "true", "X-Custom": "value"},
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)

	// Create request with HTTP request embedded
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/test")
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "GET",
		URI:    "http://example.com/test",
		Proto:  "HTTP/1.1",
		Header: icap.NewHeader(),
	}

	resp, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.HTTPRequest == nil {
		t.Fatal("expected HTTP request in response")
	}

	// Check modified headers
	if v, ok := resp.HTTPRequest.Header.Get("X-Modified"); !ok || v != "true" {
		t.Errorf("expected X-Modified=true, got %s", v)
	}
	if v, ok := resp.HTTPRequest.Header.Get("X-Custom"); !ok || v != "value" {
		t.Errorf("expected X-Custom=value, got %s", v)
	}
}

// TestMockProcessor_RESPMOD tests RESPMOD handling.
func TestMockProcessor_RESPMOD(t *testing.T) {
	log := createTestLogger(t)

	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "respmod-scenario",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodRESPMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus:  200,
			HTTPStatus:  200,
			HTTPHeaders: map[string]string{"X-RespMod": "true"},
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)

	// Create RESPMOD request with HTTP response embedded
	req, _ := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/test")
	req.HTTPResponse = &icap.HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     "404",
		StatusText: "Not Found",
		Header:     icap.NewHeader(),
	}

	resp, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.HTTPResponse == nil {
		t.Fatal("expected HTTP response in response")
	}

	// Check modified headers
	if v, ok := resp.HTTPResponse.Header.Get("X-RespMod"); !ok || v != "true" {
		t.Errorf("expected X-RespMod=true, got %s", v)
	}
}

// TestMockProcessor_Name tests the Name method.
func TestMockProcessor_Name(t *testing.T) {
	processor := NewMockProcessor(nil, nil)
	expected := "MockProcessor"

	if processor.Name() != expected {
		t.Errorf("expected name %q, got %q", expected, processor.Name())
	}
}

// TestMockProcessor_ThreadSafety tests thread safety.
func TestMockProcessor_ThreadSafety(t *testing.T) {
	log := createTestLogger(t)

	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "concurrent-test",
		Match: storage.MatchRule{
			Methods: []string{icap.MethodREQMOD},
		},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("failed to add scenario: %v", err)
	}

	processor := NewMockProcessor(registry, log)

	const goroutines = 50
	done := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			req := createTestREQMODRequest(t)
			_, err := processor.Process(context.Background(), req)
			done <- err
		}()
	}

	timeout := time.After(5 * time.Second)
	for i := 0; i < goroutines; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		case <-timeout:
			t.Fatal("timeout waiting for goroutines")
		}
	}
}

// TestMockProcessor_Interface verifies MockProcessor implements Processor interface.
func TestMockProcessor_Interface(_ *testing.T) {
	var _ Processor = NewMockProcessor(nil, nil)
}

func TestMockProcessor_StreamRequestBodyAndHTTPReason(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name:  "stream-request-body",
		Match: storage.MatchRule{Methods: []string{icap.MethodREQMOD}},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			HTTPStatus: 403,
			Stream: &storage.StreamConfig{
				Source: storage.StreamSourceConfig{From: "request_body"},
				Chunks: storage.StreamChunksConfig{Size: storage.SizeSpec{Min: 2, Max: 2, IsSet: true}},
				Finish: storage.StreamFinishConfig{Mode: icap.StreamFinishComplete},
			},
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessor(registry, createTestLogger(t))
	req := createTestREQMODRequest(t)
	req.HTTPRequest = &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPRequest.SetLoadedBody([]byte("abcd"))
	resp, err := proc.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var out bytes.Buffer
	if _, err := resp.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !strings.Contains(out.String(), "HTTP/1.1 403 Forbidden") {
		t.Fatalf("HTTP reason phrase not written correctly: %q", out.String())
	}
	if !strings.Contains(out.String(), "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n") {
		t.Fatalf("streamed request body missing: %q", out.String())
	}
}

func TestMockProcessor_StreamResponseBody(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(responseBodyStreamScenario(icap.StreamFinishComplete)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessor(registry, createTestLogger(t))
	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var out bytes.Buffer
	if _, err := resp.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !strings.Contains(out.String(), "3\r\nwxy\r\n1\r\nz\r\n0\r\n\r\n") {
		t.Fatalf("streamed response body missing: %q", out.String())
	}
}

func TestMockProcessor_StreamFINModeSetsConnectionClose(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(responseBodyStreamScenario(icap.StreamFinishFIN)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessor(registry, createTestLogger(t))
	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got, ok := resp.GetHeader("Connection"); !ok || got != "close" {
		t.Fatalf("Connection header = %q, %v; want close, true", got, ok)
	}
}

func TestMockProcessor_UsesSelectedBranchAndWeightedErrors(t *testing.T) {
	tests := []struct {
		scenario *storage.Scenario
		name     string
		want     string
	}{
		{name: "branch", want: "branch error", scenario: branchErrorScenario()},
		{name: "weighted", want: "weighted error", scenario: weightedErrorScenario()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processScenarioError(t, tt.scenario)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Process() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func branchErrorScenario() *storage.Scenario {
	return &storage.Scenario{
		Name: "branch-error", Match: storage.MatchRule{Methods: []string{icap.MethodREQMOD}}, Priority: 100,
		Response: storage.ResponseTemplate{ICAPStatus: 204, Error: "scenario error"},
		Branches: []storage.Branch{{Response: storage.ResponseTemplate{ICAPStatus: 500, Error: "branch error"}}},
	}
}

func weightedErrorScenario() *storage.Scenario {
	return &storage.Scenario{
		Name: "weighted-error", Match: storage.MatchRule{Methods: []string{icap.MethodREQMOD}}, Priority: 100,
		Response:          storage.ResponseTemplate{ICAPStatus: 204},
		WeightedResponses: []storage.WeightedResponse{{Weight: 1, ICAPStatus: 500, Error: "weighted error"}},
	}
}

func processScenarioError(t *testing.T, scenario *storage.Scenario) error {
	t.Helper()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	_, err := NewMockProcessor(registry, createTestLogger(t)).Process(context.Background(), createTestREQMODRequest(t))
	return err
}

func responseBodyStreamScenario(mode string) *storage.Scenario {
	finish := storage.StreamFinishConfig{Mode: mode}
	if mode == icap.StreamFinishFIN {
		finish.Fin = storage.StreamFINConfig{Close: "clean"}
	}
	return &storage.Scenario{
		Name:  "stream-response-body",
		Match: storage.MatchRule{Methods: []string{icap.MethodRESPMOD}},
		Response: storage.ResponseTemplate{
			ICAPStatus: 200,
			Stream: &storage.StreamConfig{
				Source: storage.StreamSourceConfig{From: "response_body"},
				Chunks: storage.StreamChunksConfig{Size: storage.SizeSpec{Min: 3, Max: 3, IsSet: true}},
				Finish: finish,
			},
		},
		Priority: 100,
	}
}

// Helper functions

func createTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "debug", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func createTestREQMODRequest(t *testing.T) *icap.Request {
	t.Helper()
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/avscan")
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	return req
}

func createTestRESPMODRequest(t *testing.T) *icap.Request {
	t.Helper()
	req, err := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/avscan")
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.HTTPRequest = &icap.HTTPMessage{Method: "GET", URI: "/download", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPResponse = &icap.HTTPMessage{Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader()}
	req.HTTPResponse.SetLoadedBody([]byte("wxyz"))
	return req
}

func TestSubstituteString_WithCaptures(t *testing.T) {
	vars := map[string]string{"id": "42", "env": "prod"}
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"${id}", "42"},
		{"id=${id} env=${env}", "id=42 env=prod"},
		{"${missing}", ""},
		{"$${id}", "${id}"},
		{"a $${env} b ${env} c", "a ${env} b prod c"},
	}
	for _, tc := range cases {
		got := substituteString(tc.in, vars)
		if got != tc.want {
			t.Errorf("substituteString(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSubstituteCaptures_StreamNestedFields(t *testing.T) {
	resp := storage.ResponseTemplate{Stream: &storage.StreamConfig{
		Body: "top-${id}", BodyFile: "/tmp/${id}.bin",
		Source: storage.StreamSourceConfig{Body: "source-${id}", BodyFile: "/src/${id}"},
		Parts:  []storage.StreamPartConfig{{Body: "part-${id}", BodyFile: "/part/${id}"}},
		Fallback: storage.StreamFallbackConfig{
			Body: "fallback-${id}", BodyFile: "/fallback/${id}",
			RawFile: storage.StreamRawFileFallback{Filename: []string{"${id}\\.dat"}},
		},
		Multipart: storage.StreamMultipartConfig{
			Fields: []string{"field-${id}"},
			Files:  storage.StreamMultipartFilesConfig{Filename: []string{"file-${id}"}},
		},
	}}

	got := substituteCaptures(resp, map[string]string{"id": "42"}).Stream
	assertStreamSubstituted(t, got)
}

func assertStreamSubstituted(t *testing.T, got *storage.StreamConfig) {
	t.Helper()
	checks := []string{got.Body, got.BodyFile, got.Source.Body, got.Source.BodyFile, got.Parts[0].Body,
		got.Parts[0].BodyFile, got.Fallback.Body, got.Fallback.BodyFile, got.Fallback.RawFile.Filename[0],
		got.Multipart.Fields[0], got.Multipart.Files.Filename[0]}
	for _, value := range checks {
		if strings.Contains(value, "${id}") {
			t.Fatalf("unsubstituted stream value %q", value)
		}
	}
}

func TestMockProcessor_ShardedRegistryCaptureSubstitutionOnRepeatedMatch(t *testing.T) {
	registry := storage.NewShardedScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name:     "capture-response",
		Match:    storage.MatchRule{Paths: []string{"/env/{id}/scan"}},
		Response: storage.ResponseTemplate{ICAPStatus: 200, Body: "id=${id}"},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessor(registry, createTestLogger(t))
	_, err = proc.Process(context.Background(), captureSubstitutionRequest())
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	resp, err := proc.Process(context.Background(), captureSubstitutionRequest())
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	if string(resp.Body) != "id=abc" {
		t.Fatalf("response body = %q, want id=abc", string(resp.Body))
	}
}

func captureSubstitutionRequest() *icap.Request {
	return &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/env/abc/scan",
		Header: icap.NewHeader(),
	}
}
