// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"strings"
	"testing"
)

// TestStreamingNoBodyBuffer verifies O(1) memory usage by ensuring the body
// is NOT loaded into memory during parsing. The body should only be accessible
// through the streaming BodyReader, not through the Body field.
func TestStreamingNoBodyBuffer(t *testing.T) {
	tests := []struct {
		name        string
		icapRequest string
		wantMethod  string
		wantURI     string
		hasBody     bool
	}{
		{
			name: "REQMOD with empty body - streaming mode",
			icapRequest: "REQMOD icap://localhost:1344/reqmod ICAP/1.0\r\n" +
				"Host: localhost\r\n" +
				"Encapsulated: req-hdr=0, req-body=63\r\n" +
				"\r\n" +
				"GET /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"\r\n" +
				"0\r\n\r\n",
			wantMethod: "GET",
			wantURI:    "/resource",
			hasBody:    true, // Has a body section (even if empty)
		},
		{
			name: "REQMOD with content - streaming mode",
			icapRequest: "REQMOD icap://localhost:1344/reqmod ICAP/1.0\r\n" +
				"Host: localhost\r\n" +
				"Encapsulated: req-hdr=0, req-body=85\r\n" +
				"\r\n" +
				"POST /submit HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 11\r\n" +
				"\r\n" +
				"b\r\n" + // chunk size: 11 bytes
				"hello world\r\n" +
				"0\r\n\r\n",
			wantMethod: "POST",
			wantURI:    "/submit",
			hasBody:    true,
		},
		{
			name: "OPTIONS - no encapsulated body",
			icapRequest: "OPTIONS icap://localhost:1344/ ICAP/1.0\r\n" +
				"Host: localhost\r\n" +
				"\r\n",
			wantMethod: "",
			wantURI:    "",
			hasBody:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.icapRequest))
			req, err := parseICAPRequest(reader)

			if err != nil {
				t.Fatalf("parseICAPRequest() error = %v", err)
			}

			// Verify ICAP Body field is NOT populated (streaming mode)
			// This is the KEY assertion for O(1) memory usage
			if len(req.Body) > 0 {
				t.Errorf("ICAP Body should be empty in streaming mode, got %d bytes", len(req.Body))
			}

			if tt.wantMethod == "" {
				// No encapsulated HTTP message expected
				if req.HTTPRequest != nil {
					t.Error("Expected no HTTPRequest for OPTIONS request")
				}
				return
			}

			// Verify HTTP request was parsed correctly
			if req.HTTPRequest == nil {
				t.Fatal("Expected HTTPRequest to be set")
			}

			if req.HTTPRequest.Method != tt.wantMethod {
				t.Errorf("Method = %q, want %q", req.HTTPRequest.Method, tt.wantMethod)
			}

			if req.HTTPRequest.URI != tt.wantURI {
				t.Errorf("URI = %q, want %q", req.HTTPRequest.URI, tt.wantURI)
			}

			// Verify streaming reader is available
			if tt.hasBody && req.HTTPRequest.BodyReader == nil {
				t.Error("Expected BodyReader to be set for streaming")
			}

			// Verify HTTP Body field is NOT populated (lazy loading)
			if len(req.HTTPRequest.Body) > 0 {
				t.Errorf("HTTP Body should be empty (lazy loading), got %d bytes", len(req.HTTPRequest.Body))
			}
		})
	}
}

// TestStreamingBodyCanBeRead verifies that the body CAN still be read through
// the streaming reader when needed, while maintaining O(1) memory during parsing.
func TestStreamingBodyCanBeRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		icapRequest string
		wantBody    string
		hasBody     bool
	}{
		{
			name: "empty body",
			icapRequest: "REQMOD icap://localhost:1344/reqmod ICAP/1.0\r\n" +
				"Host: localhost\r\n" +
				"Encapsulated: req-hdr=0, null-body=0\r\n" +
				"\r\n",
			wantBody: "",
			hasBody:  false,
		},
		{
			name: "chunked body with hello world",
			icapRequest: "REQMOD icap://localhost:1344/reqmod ICAP/1.0\r\n" +
				"Host: localhost\r\n" +
				"Encapsulated: req-hdr=0, req-body=85\r\n" +
				"\r\n" +
				"POST /submit HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 11\r\n" +
				"\r\n" +
				"b\r\n" + // chunk size: 11 bytes in hex
				"hello world\r\n" +
				"0\r\n\r\n",
			wantBody: "hello world",
			hasBody:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.icapRequest))
			req, err := parseICAPRequest(reader)

			if err != nil {
				t.Fatalf("parseICAPRequest() error = %v", err)
			}

			// Verify ICAP Body field is empty (streaming mode)
			if len(req.Body) > 0 {
				t.Errorf("ICAP Body should be empty in streaming mode, got %d bytes", len(req.Body))
			}

			// Check if body is expected
			if !tt.hasBody {
				// No HTTPRequest for null-body case
				return
			}

			// Verify streaming reader is available
			if req.HTTPRequest == nil {
				t.Fatal("Expected HTTPRequest to be set")
			}

			if tt.hasBody && req.HTTPRequest.BodyReader == nil {
				t.Error("Expected BodyReader to be set for streaming")
			}

			// Read body if expected
			if tt.wantBody != "" {
				body, err := req.HTTPRequest.GetBody()
				if err != nil {
					t.Fatalf("GetBody() error = %v", err)
				}
				if string(body) != tt.wantBody {
					t.Errorf("GetBody() = %q, want %q", string(body), tt.wantBody)
				}
			}
		})
	}
}

// TestStreamingWithHelloWorldBody verifies the specific body content can be read.
func TestStreamingWithHelloWorldBody(t *testing.T) {
	t.Parallel()

	icapRequest := "REQMOD icap://localhost:1344/reqmod ICAP/1.0\r\n" +
		"Host: localhost\r\n" +
		"Encapsulated: req-hdr=0, req-body=85\r\n" +
		"\r\n" +
		"POST /submit HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"Content-Length: 11\r\n" +
		"\r\n" +
		"b\r\n" + // chunk size: 11 bytes in hex
		"hello world\r\n" +
		"0\r\n\r\n"

	reader := bufio.NewReader(strings.NewReader(icapRequest))
	req, err := parseICAPRequest(reader)
	if err != nil {
		t.Fatalf("parseICAPRequest() error = %v", err)
	}

	// Body should NOT be loaded yet
	if len(req.Body) > 0 {
		t.Errorf("ICAP Body should be empty, got %d bytes", len(req.Body))
	}

	if req.HTTPRequest == nil {
		t.Fatal("Expected HTTPRequest to be set")
	}

	// HTTP Body should NOT be loaded yet (lazy loading)
	if len(req.HTTPRequest.Body) > 0 {
		t.Errorf("HTTP Body should be empty before GetBody(), got %d bytes", len(req.HTTPRequest.Body))
	}

	// But we CAN read it through the streaming reader
	body, err := req.HTTPRequest.GetBody()
	if err != nil {
		t.Fatalf("GetBody() error = %v", err)
	}

	expectedBody := "hello world"
	if string(body) != expectedBody {
		t.Errorf("GetBody() = %q, want %q", string(body), expectedBody)
	}
}
