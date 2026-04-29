// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
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

func TestParseRESPMODWithRequestBodyBeforeResponse(t *testing.T) {
	t.Parallel()

	requestHeader := "POST /upload HTTP/1.1\r\nHost: origin.example\r\nContent-Length: 5\r\n\r\n"
	requestBody := "5\r\nabcde\r\n0\r\n\r\n"
	responseHeader := "HTTP/1.1 200 OK\r\nServer: origin\r\nContent-Length: 7\r\n\r\n"
	responseBody := "7\r\nblocked\r\n0\r\n\r\n"
	rawRequest := buildSegmentedRESPMOD(requestHeader, requestBody, responseHeader, responseBody)

	req, err := parseICAPRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parseICAPRequest() error = %v", err)
	}
	assertSegmentedRESPMODRequest(t, req)
	assertSegmentedRESPMODResponse(t, req)
}

func TestParseRESPMODRejectsInvalidSegmentOffsets(t *testing.T) {
	t.Parallel()

	request := "RESPMOD icap://localhost/respmod ICAP/1.0\r\n" +
		"Encapsulated: req-hdr=0, req-body=40, res-hdr=20, res-body=60\r\n\r\n"
	err := parseRequestError(request)
	if err == nil || !strings.Contains(err.Error(), "invalid RESPMOD encapsulated offsets") {
		t.Fatalf("parseICAPRequest() error = %v, want invalid offset error", err)
	}
}

func TestParseRESPMODRejectsOversizedSegmentedRequestBody(t *testing.T) {
	t.Parallel()

	requestHeader := "GET / HTTP/1.1\r\nHost: origin.example\r\n\r\n"
	reqBodyOffset := len(requestHeader)
	resHdrOffset := reqBodyOffset + maxSegmentedRESPMODRequestBodyBytes + 1
	rawRequest := fmt.Sprintf("RESPMOD icap://localhost/respmod ICAP/1.0\r\n"+
		"Encapsulated: req-hdr=0, req-body=%d, res-hdr=%d, res-body=%d\r\n\r\n%s",
		reqBodyOffset, resHdrOffset, resHdrOffset, requestHeader)

	err := parseRequestError(rawRequest)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("parseICAPRequest() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestParseICAPRequestRejectsOversizedRequestLine(t *testing.T) {
	request := "REQMOD icap://localhost/" + strings.Repeat("a", maxProtocolRequestLineBytes) + " ICAP/1.0\r\n\r\n"
	err := parseRequestError(request)
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("parseICAPRequest() error = %v, want ErrLineTooLong", err)
	}
}

func TestParseICAPRequestRejectsOversizedHeaderLine(t *testing.T) {
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\n" +
		"X-Large: " + strings.Repeat("a", maxProtocolHeaderLineBytes) + "\r\n\r\n"
	err := parseRequestError(request)
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("parseICAPRequest() error = %v, want ErrLineTooLong", err)
	}
}

func TestParseICAPRequestRejectsOversizedEmbeddedHTTPRequestLine(t *testing.T) {
	request := "REQMOD icap://localhost/reqmod ICAP/1.0\r\n" +
		"Encapsulated: req-hdr=0, null-body=0\r\n\r\n" +
		"GET /" + strings.Repeat("a", maxProtocolRequestLineBytes) + " HTTP/1.1\r\n\r\n"
	err := parseRequestError(request)
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("parseICAPRequest() error = %v, want ErrLineTooLong", err)
	}
}

func TestParseICAPRequestRejectsOversizedEmbeddedHTTPStatusLine(t *testing.T) {
	request := "RESPMOD icap://localhost/respmod ICAP/1.0\r\n" +
		"Encapsulated: req-hdr=0, res-hdr=27, null-body=0\r\n\r\n" +
		"GET / HTTP/1.1\r\nHost: origin\r\n\r\n" +
		"HTTP/1.1 200 " + strings.Repeat("a", maxProtocolStatusLineBytes) + "\r\n\r\n"
	err := parseRequestError(request)
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("parseICAPRequest() error = %v, want ErrLineTooLong", err)
	}
}

func TestParseICAPRequestRejectsOversizedPreviewHeader(t *testing.T) {
	request := "OPTIONS icap://localhost/ ICAP/1.0\r\n" +
		"Preview: 1048577\r\n\r\n"
	err := parseRequestError(request)
	if err == nil || !strings.Contains(err.Error(), "Preview header") {
		t.Fatalf("parseICAPRequest() error = %v, want Preview header error", err)
	}
}

func TestExtractClientIPIgnoresSpoofedHeaderByDefault(t *testing.T) {
	headers := icap.Header{}
	headers.Set("X-Client-IP", "203.0.113.99")
	got := extractClientIP(headers, "192.0.2.10:12345", false, nil)
	if got != "192.0.2.10" {
		t.Fatalf("extractClientIP() = %q, want peer IP", got)
	}
}

func TestExtractClientIPHonorsTrustedProxyHeader(t *testing.T) {
	headers := icap.Header{}
	headers.Set("X-Client-IP", "203.0.113.99")
	got := extractClientIP(headers, "10.0.0.5:12345", true, []string{"10.0.0.0/24"})
	if got != "203.0.113.99" {
		t.Fatalf("extractClientIP() = %q, want forwarded client IP", got)
	}
}

func TestExtractClientIPRejectsUntrustedProxyHeader(t *testing.T) {
	headers := icap.Header{}
	headers.Set("X-Client-IP", "203.0.113.99")
	got := extractClientIP(headers, "192.0.2.10:12345", true, []string{"10.0.0.0/24"})
	if got != "192.0.2.10" {
		t.Fatalf("extractClientIP() = %q, want peer IP", got)
	}
}

func parseRequestError(raw string) error {
	_, err := parseICAPRequest(bufio.NewReader(strings.NewReader(raw)))
	return err
}

func buildSegmentedRESPMOD(requestHeader, requestBody, responseHeader, responseBody string) string {
	reqBodyOffset := len(requestHeader)
	resHdrOffset := reqBodyOffset + len(requestBody)
	resBodyOffset := resHdrOffset + len(responseHeader)
	return fmt.Sprintf("RESPMOD icap://localhost/respmod ICAP/1.0\r\n"+
		"Host: localhost\r\n"+
		"Encapsulated: req-hdr=0, req-body=%d, res-hdr=%d, res-body=%d\r\n\r\n"+
		"%s%s%s%s", reqBodyOffset, resHdrOffset, resBodyOffset, requestHeader, requestBody, responseHeader, responseBody)
}

func assertSegmentedRESPMODRequest(t *testing.T, req *icap.Request) {
	t.Helper()
	if req.HTTPRequest == nil {
		t.Fatal("Expected HTTPRequest to be set")
	}
	if req.HTTPRequest.Method != "POST" || req.HTTPRequest.URI != "/upload" {
		t.Fatalf("HTTPRequest = %s %s, want POST /upload", req.HTTPRequest.Method, req.HTTPRequest.URI)
	}
	body, err := req.HTTPRequest.GetBody()
	if err != nil {
		t.Fatalf("HTTPRequest.GetBody() error = %v", err)
	}
	if string(body) != "abcde" {
		t.Fatalf("HTTPRequest.GetBody() = %q, want %q", string(body), "abcde")
	}
}

func assertSegmentedRESPMODResponse(t *testing.T, req *icap.Request) {
	t.Helper()
	if req.HTTPResponse == nil {
		t.Fatal("Expected HTTPResponse to be set")
	}
	if req.HTTPResponse.Status != "200" || req.HTTPResponse.StatusText != "OK" {
		t.Fatalf("HTTPResponse = %s %s, want 200 OK", req.HTTPResponse.Status, req.HTTPResponse.StatusText)
	}
	body, err := req.HTTPResponse.GetBody()
	if err != nil {
		t.Fatalf("HTTPResponse.GetBody() error = %v", err)
	}
	if string(body) != "blocked" {
		t.Fatalf("HTTPResponse.GetBody() = %q, want %q", string(body), "blocked")
	}
}
