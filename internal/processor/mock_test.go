// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
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
					Method: icap.MethodREQMOD,
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
					Method: icap.MethodREQMOD,
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
					Method: icap.MethodREQMOD,
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
					Method: icap.MethodREQMOD,
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

// TestMockProcessor_NoMatch tests behavior when no scenario matches.
func TestMockProcessor_NoMatch(t *testing.T) {
	log := createTestLogger(t)

	// Create registry with a scenario that only matches RESPMOD
	registry := storage.NewScenarioRegistry()
	err := registry.Add(&storage.Scenario{
		Name: "respmod-only",
		Match: storage.MatchRule{
			Method: icap.MethodRESPMOD,
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
			Method: icap.MethodREQMOD,
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
			Method: icap.MethodREQMOD,
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
			Method: icap.MethodREQMOD,
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
			Method: icap.MethodRESPMOD,
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
			Method: icap.MethodREQMOD,
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
func TestMockProcessor_Interface(t *testing.T) {
	var _ Processor = NewMockProcessor(nil, nil)
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
