// Package handler_test provides tests for the OPTIONS handler.
package handler_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestOptionsHandler tests the OPTIONS handler basic functionality.
func TestOptionsHandler(t *testing.T) {
	t.Parallel()

	t.Run("Handle returns correct response", func(t *testing.T) {
		opts := handler.OptionsHandlerConfig{
			ServiceTag:     `"test-tag-123"`,
			Methods:        []string{"REQMOD", "RESPMOD"},
			MaxConnections: 100,
			OptionsTTL:     3600 * time.Second,
		}

		h := handler.NewOptionsHandler(opts)
		req, err := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}

		if resp.StatusCode != icap.StatusOK {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
		}
	})

	t.Run("Method returns OPTIONS", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{})
		if h.Method() != icap.MethodOPTIONS {
			t.Errorf("Method() = %q, want %q", h.Method(), icap.MethodOPTIONS)
		}
	})
}

// TestOptionsHandlerHeaders tests that all required headers are set correctly.
func TestOptionsHandlerHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		config         handler.OptionsHandlerConfig
		expectedMethod string
		expectedTTL    int
		expectedMax    int
		expectedSID    string
	}{
		{
			name: "standard configuration",
			config: handler.OptionsHandlerConfig{
				ServiceTag:     `"production-1"`,
				ServiceID:      "icap-service-1",
				Methods:        []string{"REQMOD", "RESPMOD"},
				MaxConnections: 200,
				OptionsTTL:     1800 * time.Second,
			},
			expectedMethod: "REQMOD, RESPMOD",
			expectedTTL:    1800,
			expectedMax:    200,
			expectedSID:    "icap-service-1",
		},
		{
			name: "REQMOD only",
			config: handler.OptionsHandlerConfig{
				ServiceTag:     `"reqmod-only"`,
				ServiceID:      "icap-service-reqmod",
				Methods:        []string{"REQMOD"},
				MaxConnections: 50,
				OptionsTTL:     900 * time.Second,
			},
			expectedMethod: "REQMOD",
			expectedTTL:    900,
			expectedMax:    50,
			expectedSID:    "icap-service-reqmod",
		},
		{
			name: "RESPMOD only",
			config: handler.OptionsHandlerConfig{
				ServiceTag:     `"respmod-only"`,
				ServiceID:      "icap-service-respmod",
				Methods:        []string{"RESPMOD"},
				MaxConnections: 75,
				OptionsTTL:     450 * time.Second,
			},
			expectedMethod: "RESPMOD",
			expectedTTL:    450,
			expectedMax:    75,
			expectedSID:    "icap-service-respmod",
		},
		{
			name: "default Service-ID",
			config: handler.OptionsHandlerConfig{
				ServiceTag:     `"default"`,
				ServiceID:      "icap-mock",
				Methods:        []string{"REQMOD"},
				MaxConnections: 100,
				OptionsTTL:     3600 * time.Second,
			},
			expectedMethod: "REQMOD",
			expectedTTL:    3600,
			expectedMax:    100,
			expectedSID:    "icap-mock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewOptionsHandler(tt.config)
			req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Fatalf("Handle() returned error: %v", err)
			}

			// Check Methods header
			if methods, ok := resp.GetHeader("Methods"); ok {
				if methods != tt.expectedMethod {
					t.Errorf("Methods header = %q, want %q", methods, tt.expectedMethod)
				}
			} else {
				t.Error("Methods header not found")
			}

			// Check Service header
			if service, ok := resp.GetHeader("Service"); ok {
				if service != "ICAP-Mock-Server/1.0" {
					t.Errorf("Service header = %q, want %q", service, "ICAP-Mock-Server/1.0")
				}
			} else {
				t.Error("Service header not found")
			}

			// Check ISTag header
			if istag, ok := resp.GetHeader("ISTag"); ok {
				if istag != tt.config.ServiceTag {
					t.Errorf("ISTag header = %q, want %q", istag, tt.config.ServiceTag)
				}
			} else {
				t.Error("ISTag header not found")
			}

			// Check Service-ID header
			if sid, ok := resp.GetHeader("Service-ID"); ok {
				if sid != tt.expectedSID {
					t.Errorf("Service-ID header = %q, want %q", sid, tt.expectedSID)
				}
			} else {
				t.Error("Service-ID header not found")
			}

			// Check Max-Connections header
			if maxConn, ok := resp.GetHeader("Max-Connections"); ok {
				if maxConn != strconv.Itoa(tt.expectedMax) {
					t.Errorf("Max-Connections header = %q, want %q", maxConn, strconv.Itoa(tt.expectedMax))
				}
			} else {
				t.Error("Max-Connections header not found")
			}

			// Check Options-TTL header
			if ttl, ok := resp.GetHeader("Options-TTL"); ok {
				if ttl != strconv.Itoa(tt.expectedTTL) {
					t.Errorf("Options-TTL header = %q, want %q", ttl, strconv.Itoa(tt.expectedTTL))
				}
			} else {
				t.Error("Options-TTL header not found")
			}

			// Check Allow header
			if allow, ok := resp.GetHeader("Allow"); ok {
				if allow != "204" {
					t.Errorf("Allow header = %q, want %q", allow, "204")
				}
			} else {
				t.Error("Allow header not found")
			}

			// Check Preview header (default PreviewSize is 0)
			if preview, ok := resp.GetHeader("Preview"); ok {
				if preview != "0" {
					t.Errorf("Preview header = %q, want %q", preview, "0")
				}
			} else {
				t.Error("Preview header not found")
			}
		})
	}
}

// TestOptionsHandlerContextCancellation tests context cancellation handling.
func TestOptionsHandlerContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("handles cancelled context", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
			ServiceTag: `"test"`,
			Methods:    []string{"REQMOD"},
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
		resp, err := h.Handle(ctx, req)

		// OPTIONS handler should still return a response even with cancelled context
		// since it's a simple operation
		if err != nil {
			t.Errorf("Handle() returned error: %v", err)
		}
		if resp == nil {
			t.Error("Handle() returned nil response")
		}
	})
}

// TestOptionsHandlerMultipleRequests tests that the handler handles multiple requests.
func TestOptionsHandlerMultipleRequests(t *testing.T) {
	t.Parallel()

	h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		ServiceTag:     `"concurrent-test"`,
		ServiceID:      "icap-mock-concurrent",
		Methods:        []string{"REQMOD", "RESPMOD"},
		MaxConnections: 100,
		OptionsTTL:     3600 * time.Second,
	})

	// Run multiple concurrent requests
	const numRequests = 10
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Errorf("Handle() returned error: %v", err)
			}
			if resp.StatusCode != icap.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, icap.StatusOK)
			}
			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for concurrent requests")
		}
	}
}

// TestOptionsHandlerServiceID tests Service-ID header functionality.
func TestOptionsHandlerServiceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      handler.OptionsHandlerConfig
		expectedSID string
	}{
		{
			name: "custom Service-ID",
			config: handler.OptionsHandlerConfig{
				ServiceID: "custom-service-123",
			},
			expectedSID: "custom-service-123",
		},
		{
			name: "default Service-ID",
			config: handler.OptionsHandlerConfig{
				ServiceID: "icap-mock",
			},
			expectedSID: "icap-mock",
		},
		{
			name: "Service-ID with version",
			config: handler.OptionsHandlerConfig{
				ServiceID: "icap-service-v1.0",
			},
			expectedSID: "icap-service-v1.0",
		},
		{
			name: "Service-ID with environment",
			config: handler.OptionsHandlerConfig{
				ServiceID: "icap-service-production",
			},
			expectedSID: "icap-service-production",
		},
		{
			name: "empty Service-ID",
			config: handler.OptionsHandlerConfig{
				ServiceID: "",
			},
			expectedSID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewOptionsHandler(tt.config)
			req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
			resp, err := h.Handle(context.Background(), req)
			if err != nil {
				t.Fatalf("Handle() returned error: %v", err)
			}

			// Check Service-ID header
			if sid, ok := resp.GetHeader("Service-ID"); ok {
				if sid != tt.expectedSID {
					t.Errorf("Service-ID header = %q, want %q", sid, tt.expectedSID)
				}
			} else {
				t.Error("Service-ID header not found")
			}
		})
	}
}

// TestOptionsHandlerPreview tests the Preview header per RFC 3507.
func TestOptionsHandlerPreview(t *testing.T) {
	t.Parallel()

	t.Run("default preview size is 0", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
			Methods: []string{"REQMOD"},
		})
		req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Fatalf("Handle() returned error: %v", err)
		}
		if preview, ok := resp.GetHeader("Preview"); !ok {
			t.Error("Preview header not found")
		} else if preview != "0" {
			t.Errorf("Preview = %q, want %q", preview, "0")
		}
	})

	t.Run("custom preview size", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
			Methods:     []string{"REQMOD"},
			PreviewSize: 4096,
		})
		req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Fatalf("Handle() returned error: %v", err)
		}
		if preview, ok := resp.GetHeader("Preview"); !ok {
			t.Error("Preview header not found")
		} else if preview != "4096" {
			t.Errorf("Preview = %q, want %q", preview, "4096")
		}
	})

	t.Run("negative preview size disables header", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
			Methods:     []string{"REQMOD"},
			PreviewSize: -1,
		})
		req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Fatalf("Handle() returned error: %v", err)
		}
		if _, ok := resp.GetHeader("Preview"); ok {
			t.Error("Preview header should not be present when PreviewSize is negative")
		}
	})

	t.Run("UpdatePreviewSize at runtime", func(t *testing.T) {
		h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
			Methods: []string{"REQMOD"},
		})
		req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")

		h.UpdatePreviewSize(2048)
		resp, err := h.Handle(context.Background(), req)
		if err != nil {
			t.Fatalf("Handle() returned error: %v", err)
		}
		if preview, ok := resp.GetHeader("Preview"); !ok {
			t.Error("Preview header not found")
		} else if preview != "2048" {
			t.Errorf("Preview = %q, want %q", preview, "2048")
		}
	})
}

// TestOptionsHandlerUpdateServiceID tests runtime update of Service-ID.
func TestOptionsHandlerUpdateServiceID(t *testing.T) {
	t.Parallel()

	h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		ServiceTag: `"initial"`,
		ServiceID:  "initial-service-id",
		Methods:    []string{"REQMOD"},
	})

	req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")

	// Initial request
	resp1, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() returned error: %v", err)
	}

	if sid, ok := resp1.GetHeader("Service-ID"); ok {
		if sid != "initial-service-id" {
			t.Errorf("Initial Service-ID = %q, want %q", sid, "initial-service-id")
		}
	} else {
		t.Error("Service-ID header not found in initial response")
	}

	// Update Service-ID
	h.UpdateServiceID("updated-service-id")

	// Updated request
	resp2, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() returned error: %v", err)
	}

	if sid, ok := resp2.GetHeader("Service-ID"); ok {
		if sid != "updated-service-id" {
			t.Errorf("Updated Service-ID = %q, want %q", sid, "updated-service-id")
		}
	} else {
		t.Error("Service-ID header not found in updated response")
	}
}

// TestOptionsHandlerRFC3507Compliance tests RFC 3507 Service-ID compliance.
func TestOptionsHandlerRFC3507Compliance(t *testing.T) {
	t.Parallel()

	h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		ServiceTag:     `"rfc-compliant"`,
		ServiceID:      "icap-mock-rfc3507",
		Methods:        []string{"REQMOD", "RESPMOD"},
		MaxConnections: 1000,
		OptionsTTL:     3600 * time.Second,
	})

	req, _ := icap.NewRequest(icap.MethodOPTIONS, "icap://localhost/")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() returned error: %v", err)
	}

	// Service-ID header should be present (RFC 3507 Section 4.10)
	if _, ok := resp.GetHeader("Service-ID"); !ok {
		t.Error("Service-ID header is missing (RFC 3507 compliance)")
	}

	// Verify Service-ID value is not empty
	if sid, ok := resp.GetHeader("Service-ID"); ok {
		if sid == "" {
			t.Error("Service-ID header value is empty")
		}
	}

	// Ensure all required headers are present
	requiredHeaders := []string{"Methods", "Service", "ISTag", "Max-Connections", "Options-TTL", "Allow", "Preview"}
	for _, header := range requiredHeaders {
		if _, ok := resp.GetHeader(header); !ok {
			t.Errorf("Required header %q is missing", header)
		}
	}
}
