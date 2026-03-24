// Package icap_test provides tests for ICAP Preview mode (RFC 3507).
package icap_test

import (
	"bufio"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestParsePreviewHeader tests parsing the Preview header.
func TestParsePreviewHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:    "zero preview",
			input:   "0",
			want:    0,
			wantErr: false,
		},
		{
			name:    "preview 1024 bytes",
			input:   "1024",
			want:    1024,
			wantErr: false,
		},
		{
			name:    "preview 4096 bytes",
			input:   "4096",
			want:    4096,
			wantErr: false,
		},
		{
			name:    "large preview",
			input:   "1048576", // 1MB
			want:    1048576,
			wantErr: false,
		},
		{
			name:    "whitespace trimmed",
			input:   "  512  ",
			want:    512,
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   "-100",
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   "abc",
			want:    0,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    0,
			wantErr: true,
		},
		{
			name:    "float value",
			input:   "10.5",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := icap.ParsePreviewHeader(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePreviewHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParsePreviewHeader() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestRequestPreviewMode tests parsing requests with Preview header.
func TestRequestPreviewMode(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantPreview int
		wantMode    bool
		wantErr     bool
	}{
		{
			name: "REQMOD without preview",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"\r\n" +
				"POST /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 5\r\n" +
				"\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			wantPreview: 0,
			wantMode:    false,
			wantErr:     false,
		},
		{
			name: "REQMOD with preview 0",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"Preview: 0\r\n" +
				"\r\n" +
				"POST /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 5\r\n" +
				"\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			wantPreview: 0,
			wantMode:    false,
			wantErr:     false,
		},
		{
			name: "REQMOD with preview 1024",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"Preview: 1024\r\n" +
				"\r\n" +
				"POST /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 5\r\n" +
				"\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			wantPreview: 1024,
			wantMode:    true,
			wantErr:     false,
		},
		{
			name: "REQMOD with preview 4096",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"Preview: 4096\r\n" +
				"\r\n" +
				"POST /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 5\r\n" +
				"\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			wantPreview: 4096,
			wantMode:    true,
			wantErr:     false,
		},
		{
			name: "RESPMOD without preview",
			input: "RESPMOD icap://icap-server.net:1344/respmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, res-hdr=200, res-body=350\r\n" +
				"\r\n" +
				"GET /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"\r\n" +
				"HTTP/1.1 200 OK\r\n" +
				"\r\n",
			wantPreview: 0,
			wantMode:    false,
			wantErr:     false,
		},
		{
			name: "RESPMOD with preview",
			input: "RESPMOD icap://icap-server.net:1344/respmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, res-hdr=200, res-body=350\r\n" +
				"Preview: 512\r\n" +
				"\r\n" +
				"GET /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"\r\n" +
				"HTTP/1.1 200 OK\r\n" +
				"\r\n",
			wantPreview: 512,
			wantMode:    true,
			wantErr:     false,
		},
		{
			name: "invalid preview value",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"Preview: abc\r\n" +
				"\r\n",
			wantPreview: 0,
			wantMode:    false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tt.input))
			req, err := icap.ParseRequest(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if req.Preview != tt.wantPreview {
					t.Errorf("Request.Preview = %d, want %d", req.Preview, tt.wantPreview)
				}
				if req.IsPreviewMode() != tt.wantMode {
					t.Errorf("Request.IsPreviewMode() = %v, want %v", req.IsPreviewMode(), tt.wantMode)
				}
			}
		})
	}
}

// TestGetPreviewBody tests reading preview bodies from HTTP messages.
func TestGetPreviewBody(t *testing.T) {
	tests := []struct {
		name         string
		bodySize     int
		previewSize  int
		wantSize     int
		wantFullBody bool
	}{
		{
			name:         "no preview requested",
			bodySize:     1000,
			previewSize:  0,
			wantSize:     1000,
			wantFullBody: false, // When previewSize is 0, we load the full body intentionally
		},
		{
			name:         "preview larger than body",
			bodySize:     500,
			previewSize:  1000,
			wantSize:     500,
			wantFullBody: true,
		},
		{
			name:         "preview smaller than body",
			bodySize:     2000,
			previewSize:  500,
			wantSize:     500,
			wantFullBody: false,
		},
		{
			name:         "preview equal to body",
			bodySize:     1024,
			previewSize:  1024,
			wantSize:     1024,
			wantFullBody: true,
		},
		{
			name:         "preview 100 bytes",
			bodySize:     10000,
			previewSize:  100,
			wantSize:     100,
			wantFullBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test body
			body := make([]byte, tt.bodySize)
			for i := range body {
				body[i] = byte(i % 256)
			}

			// Create HTTP message with body reader
			msg := &icap.HTTPMessage{
				BodyReader: strings.NewReader(string(body)),
			}

			// Get preview body
			preview, err := msg.GetPreviewBody(tt.previewSize)
			if err != nil {
				t.Errorf("GetPreviewBody() error = %v", err)
				return
			}

			// Check size
			if len(preview) != tt.wantSize {
				t.Errorf("GetPreviewBody() size = %d, want %d", len(preview), tt.wantSize)
			}

			// Check content
			if len(preview) > 0 {
				for i, b := range preview {
					if b != byte(i%256) {
						t.Errorf("GetPreviewBody() byte at %d = %d, want %d", i, b, byte(i%256))
						break
					}
				}
			}

			// Check if full body was loaded
			if tt.wantFullBody && msg.IsBodyLoaded() {
				t.Errorf("GetPreviewBody() loaded full body when only preview was needed")
			}
		})
	}
}

// TestGetPreviewBodyEmpty tests preview with empty body.
func TestGetPreviewBodyEmpty(t *testing.T) {
	msg := &icap.HTTPMessage{}

	// Get preview with no body
	preview, err := msg.GetPreviewBody(1024)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}
	if preview != nil {
		t.Errorf("GetPreviewBody() = %v, want nil", preview)
	}

	// Get preview with nil body reader
	msg.BodyReader = nil
	preview, err = msg.GetPreviewBody(1024)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}
	if preview != nil {
		t.Errorf("GetPreviewBody() = %v, want nil", preview)
	}
}

// TestGetPreviewBodyAlreadyLoaded tests preview when body is already loaded.
func TestGetPreviewBodyAlreadyLoaded(t *testing.T) {
	body := []byte("hello world this is a test body")
	msg := &icap.HTTPMessage{
		Body: body,
	}
	msg.SetLoadedBody(body)

	// Get preview of first 5 bytes
	preview, err := msg.GetPreviewBody(5)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}
	if string(preview) != "hello" {
		t.Errorf("GetPreviewBody() = %s, want 'hello'", string(preview))
	}

	// Get preview of all bytes
	preview, err = msg.GetPreviewBody(1000)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}
	if string(preview) != string(body) {
		t.Errorf("GetPreviewBody() = %s, want %s", string(preview), string(body))
	}
}

// TestRequestGetPreviewBody tests getting preview body from request.
func TestRequestGetPreviewBody(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		hasBody     bool
		previewSize int
		wantBody    bool
	}{
		{
			name:        "REQMOD with body",
			method:      "REQMOD",
			hasBody:     true,
			previewSize: 100,
			wantBody:    true,
		},
		{
			name:        "REQMOD without body",
			method:      "REQMOD",
			hasBody:     false,
			previewSize: 100,
			wantBody:    false,
		},
		{
			name:        "RESPMOD with body",
			method:      "RESPMOD",
			hasBody:     true,
			previewSize: 100,
			wantBody:    true,
		},
		{
			name:        "RESPMOD without body",
			method:      "RESPMOD",
			hasBody:     false,
			previewSize: 100,
			wantBody:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:  tt.method,
				Preview: tt.previewSize,
			}

			if tt.hasBody {
				body := "test body content"
				if tt.method == "REQMOD" {
					req.HTTPRequest = &icap.HTTPMessage{
						Body: []byte(body),
					}
					req.HTTPRequest.SetLoadedBody([]byte(body))
				} else {
					req.HTTPResponse = &icap.HTTPMessage{
						Body: []byte(body),
					}
					req.HTTPResponse.SetLoadedBody([]byte(body))
				}
			}

			preview, err := req.GetPreviewBody()
			if err != nil {
				t.Errorf("GetPreviewBody() error = %v", err)
				return
			}

			if tt.wantBody && preview == nil {
				t.Errorf("GetPreviewBody() = nil, want non-nil")
			}
			if !tt.wantBody && preview != nil {
				t.Errorf("GetPreviewBody() = %v, want nil", preview)
			}

			if tt.wantBody && preview != nil {
				if len(preview) > tt.previewSize {
					t.Errorf("GetPreviewBody() size = %d, want <= %d", len(preview), tt.previewSize)
				}
			}
		})
	}
}

// TestPreviewModeRFC3507 tests RFC 3507 preview mode behavior.
func TestPreviewModeRFC3507(t *testing.T) {
	t.Run("Preview header is parsed correctly", func(t *testing.T) {
		input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
			"Host: icap-server.net\r\n" +
			"Encapsulated: req-hdr=0, req-body=412\r\n" +
			"Preview: 1024\r\n" +
			"\r\n" +
			"POST /resource HTTP/1.1\r\n" +
			"Host: origin-server.net\r\n" +
			"Content-Length: 5\r\n" +
			"\r\n" +
			"5\r\nhello\r\n0\r\n\r\n"

		r := bufio.NewReader(strings.NewReader(input))
		req, err := icap.ParseRequest(r)
		if err != nil {
			t.Fatalf("ParseRequest() error = %v", err)
		}

		if req.Preview != 1024 {
			t.Errorf("Preview = %d, want 1024", req.Preview)
		}
		if !req.IsPreviewMode() {
			t.Errorf("IsPreviewMode() = false, want true")
		}
	})

	t.Run("Preview:0 means no preview", func(t *testing.T) {
		input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
			"Host: icap-server.net\r\n" +
			"Encapsulated: req-hdr=0, req-body=412\r\n" +
			"Preview: 0\r\n" +
			"\r\n" +
			"POST /resource HTTP/1.1\r\n" +
			"Host: origin-server.net\r\n" +
			"Content-Length: 5\r\n" +
			"\r\n" +
			"5\r\nhello\r\n0\r\n\r\n"

		r := bufio.NewReader(strings.NewReader(input))
		req, err := icap.ParseRequest(r)
		if err != nil {
			t.Fatalf("ParseRequest() error = %v", err)
		}

		if req.Preview != 0 {
			t.Errorf("Preview = %d, want 0", req.Preview)
		}
		if req.IsPreviewMode() {
			t.Errorf("IsPreviewMode() = true, want false")
		}
	})

	t.Run("Missing Preview header defaults to 0", func(t *testing.T) {
		input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
			"Host: icap-server.net\r\n" +
			"Encapsulated: req-hdr=0, req-body=412\r\n" +
			"\r\n" +
			"POST /resource HTTP/1.1\r\n" +
			"Host: origin-server.net\r\n" +
			"Content-Length: 5\r\n" +
			"\r\n" +
			"5\r\nhello\r\n0\r\n\r\n"

		r := bufio.NewReader(strings.NewReader(input))
		req, err := icap.ParseRequest(r)
		if err != nil {
			t.Fatalf("ParseRequest() error = %v", err)
		}

		if req.Preview != 0 {
			t.Errorf("Preview = %d, want 0", req.Preview)
		}
		if req.IsPreviewMode() {
			t.Errorf("IsPreviewMode() = true, want false")
		}
	})
}

// TestPreviewWithChunkedEncoding tests preview mode with chunked encoding.
func TestPreviewWithChunkedEncoding(t *testing.T) {
	// Create a chunked body
	chunked := "5\r\nhello\r\n5\r\nworld\r\n5\r\ntest\r\n0\r\n\r\n"
	body := []byte("helloworldtest")

	msg := &icap.HTTPMessage{
		BodyReader: icap.NewChunkedReader(strings.NewReader(chunked)),
	}

	// Get preview of first 10 bytes
	preview, err := msg.GetPreviewBody(10)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}

	if len(preview) != 10 {
		t.Errorf("GetPreviewBody() size = %d, want 10", len(preview))
	}

	// Should have read from chunked reader
	expected := body[:10]
	if string(preview) != string(expected) {
		t.Errorf("GetPreviewBody() = %s, want %s", string(preview), string(expected))
	}
}

// TestPreviewFromStreamReader tests preview from an actual reader.
func TestPreviewFromStreamReader(t *testing.T) {
	// Create a large body
	largeBody := make([]byte, 100000)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	msg := &icap.HTTPMessage{
		BodyReader: strings.NewReader(string(largeBody)),
	}

	// Get preview of first 1024 bytes
	previewSize := 1024
	preview, err := msg.GetPreviewBody(previewSize)
	if err != nil {
		t.Errorf("GetPreviewBody() error = %v", err)
	}

	if len(preview) != previewSize {
		t.Errorf("GetPreviewBody() size = %d, want %d", len(preview), previewSize)
	}

	// Verify content
	for i := 0; i < previewSize; i++ {
		if preview[i] != byte(i%256) {
			t.Errorf("GetPreviewBody() byte at %d = %d, want %d", i, preview[i], byte(i%256))
			break
		}
	}

	// Verify full body was not loaded
	if msg.IsBodyLoaded() {
		t.Errorf("GetPreviewBody() loaded full body when only preview was requested")
	}
}
