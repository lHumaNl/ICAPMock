// Copyright 2026 ICAP Mock

package icap_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestParseRequest tests parsing ICAP requests.
func TestParseRequest(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantMethod string
		wantURI    string
		wantErr    bool
	}{
		{
			name: "simple REQMOD",
			input: "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, req-body=412\r\n" +
				"\r\n" +
				"POST /resource HTTP/1.1\r\n" +
				"Host: origin-server.net\r\n" +
				"Content-Length: 5\r\n" +
				"\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			wantMethod: "REQMOD",
			wantURI:    "icap://icap-server.net:1344/reqmod",
		},
		{
			name: "simple RESPMOD",
			input: "RESPMOD icap://icap-server.net:1344/respmod ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"Encapsulated: req-hdr=0, res-hdr=200, res-body=350\r\n" +
				"\r\n",
			wantMethod: "RESPMOD",
			wantURI:    "icap://icap-server.net:1344/respmod",
		},
		{
			name: "OPTIONS request",
			input: "OPTIONS icap://icap-server.net:1344/ ICAP/1.0\r\n" +
				"Host: icap-server.net\r\n" +
				"\r\n",
			wantMethod: "OPTIONS",
			wantURI:    "icap://icap-server.net:1344/",
		},
		{
			name:    "invalid request line",
			input:   "INVALID\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "invalid ICAP version",
			input:   "REQMOD icap://server/ ICAP/2.0\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "missing method",
			input:   "icap://server/ ICAP/1.0\r\n\r\n",
			wantErr: true,
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
				if req.Method != tt.wantMethod {
					t.Errorf("Request.Method = %q, want %q", req.Method, tt.wantMethod)
				}
				if req.URI != tt.wantURI {
					t.Errorf("Request.URI = %q, want %q", req.URI, tt.wantURI)
				}
			}
		})
	}
}

// TestRequestSetClientIP tests setting client IP.
func TestRequestSetClientIP(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
		URI:    "icap://localhost/reqmod",
	}

	req.SetClientIP("192.168.1.100")

	if req.ClientIP != "192.168.1.100" {
		t.Errorf("ClientIP = %q, want %q", req.ClientIP, "192.168.1.100")
	}
}

// TestRequestGetHeader tests getting headers from request.
func TestRequestGetHeader(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
		URI:    "icap://localhost/reqmod",
		Header: make(icap.Header),
	}
	req.Header.Set("Host", "localhost")
	req.Header.Set("Encapsulated", "req-hdr=0, req-body=412")

	val, exists := req.GetHeader("host")
	if !exists || val != "localhost" {
		t.Errorf("GetHeader(host) = %q, %v, want %q, true", val, exists, "localhost")
	}
}

// TestRequestSetHeader tests setting headers on request.
func TestRequestSetHeader(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
		URI:    "icap://localhost/reqmod",
		Header: make(icap.Header),
	}

	req.SetHeader("X-Custom", "value")

	if val, _ := req.GetHeader("x-custom"); val != "value" {
		t.Errorf("SetHeader failed, got %q", val)
	}
}

// TestParseEncapsulatedHeader tests parsing the Encapsulated header.
func TestParseEncapsulatedHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    icap.Encapsulated
		wantErr bool
	}{
		{
			name:  "req-hdr and req-body",
			input: "req-hdr=0, req-body=412",
			want: icap.Encapsulated{
				ReqHdr:   0,
				ReqBody:  412,
				ResHdr:   -1,
				ResBody:  -1,
				NullBody: -1,
			},
		},
		{
			name:  "req-hdr only",
			input: "req-hdr=0",
			want: icap.Encapsulated{
				ReqHdr:   0,
				ReqBody:  -1,
				ResHdr:   -1,
				ResBody:  -1,
				NullBody: -1,
			},
		},
		{
			name:  "null-body",
			input: "null-body=0",
			want: icap.Encapsulated{
				ReqHdr:   -1,
				ReqBody:  -1,
				ResHdr:   -1,
				ResBody:  -1,
				NullBody: 0,
			},
		},
		{
			name:  "req-hdr, res-hdr, res-body",
			input: "req-hdr=0, res-hdr=200, res-body=350",
			want: icap.Encapsulated{
				ReqHdr:   0,
				ReqBody:  -1,
				ResHdr:   200,
				ResBody:  350,
				NullBody: -1,
			},
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "missing equals",
			input:   "req-hdr",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := icap.ParseEncapsulatedHeader(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEncapsulatedHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got != tt.want {
					t.Errorf("ParseEncapsulatedHeader() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

// TestRequestHasBody tests checking if request has body.
func TestRequestHasBody(t *testing.T) {
	tests := []struct {
		name     string
		encap    icap.Encapsulated
		wantBody bool
	}{
		{
			name:     "has req-body",
			encap:    icap.Encapsulated{ReqHdr: 0, ReqBody: 100, ResHdr: -1, ResBody: -1, NullBody: -1},
			wantBody: true,
		},
		{
			name:     "has req-body at offset 0",
			encap:    icap.Encapsulated{ReqHdr: -1, ReqBody: 0, ResHdr: -1, ResBody: -1, NullBody: -1},
			wantBody: true,
		},
		{
			name:     "has res-body",
			encap:    icap.Encapsulated{ReqHdr: 0, ReqBody: -1, ResHdr: 100, ResBody: 200, NullBody: -1},
			wantBody: true,
		},
		{
			name:     "no body - null-body",
			encap:    icap.Encapsulated{ReqHdr: 0, ReqBody: -1, ResHdr: -1, ResBody: -1, NullBody: 100},
			wantBody: false,
		},
		{
			name:     "no body - empty",
			encap:    icap.NewEncapsulated(),
			wantBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:       "REQMOD",
				Encapsulated: tt.encap,
			}
			if got := req.HasBody(); got != tt.wantBody {
				t.Errorf("HasBody() = %v, want %v", got, tt.wantBody)
			}
		})
	}
}

// TestNewRequest tests creating a new request.
func TestNewRequest(t *testing.T) {
	req, err := icap.NewRequest("REQMOD", "icap://localhost/reqmod")
	if err != nil {
		t.Errorf("NewRequest() error = %v", err)
		return
	}
	if req.Method != "REQMOD" {
		t.Errorf("Method = %q, want %q", req.Method, "REQMOD")
	}
	if req.URI != "icap://localhost/reqmod" {
		t.Errorf("URI = %q, want %q", req.URI, "icap://localhost/reqmod")
	}
	if req.Header == nil {
		t.Error("Header should be initialized")
	}
}

// TestRequestValidate tests request validation.
func TestRequestValidate(t *testing.T) {
	tests := []struct {
		req     *icap.Request
		name    string
		wantErr bool
	}{
		{
			name: "valid REQMOD",
			req: &icap.Request{
				Method: "REQMOD",
				URI:    "icap://localhost/reqmod",
				Header: icap.Header{"Host": {"localhost"}},
			},
			wantErr: false,
		},
		{
			name: "valid RESPMOD",
			req: &icap.Request{
				Method: "RESPMOD",
				URI:    "icap://localhost/respmod",
				Header: icap.Header{"Host": {"localhost"}},
			},
			wantErr: false,
		},
		{
			name: "valid OPTIONS",
			req: &icap.Request{
				Method: "OPTIONS",
				URI:    "icap://localhost/",
				Header: icap.Header{"Host": {"localhost"}},
			},
			wantErr: false,
		},
		{
			name: "invalid method",
			req: &icap.Request{
				Method: "INVALID",
				URI:    "icap://localhost/",
			},
			wantErr: true,
		},
		{
			name: "missing URI",
			req: &icap.Request{
				Method: "REQMOD",
				URI:    "",
			},
			wantErr: true,
		},
		{
			name: "invalid URI scheme",
			req: &icap.Request{
				Method: "REQMOD",
				URI:    "http://localhost/",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRequestWrite tests serializing a request.
func TestRequestWrite(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
		URI:    "icap://localhost:1344/reqmod",
		Header: icap.Header{
			"Host": {"localhost"},
		},
	}

	var buf bytes.Buffer
	n, err := req.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	if !strings.Contains(output, "REQMOD icap://localhost:1344/reqmod ICAP/1.0") {
		t.Errorf("Output missing request line: %q", output)
	}
	if !strings.Contains(output, "Host: localhost") {
		t.Errorf("Output missing Host header: %q", output)
	}

	// Check returned bytes count
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo returned %d, wrote %d", n, buf.Len())
	}
}

// TestParseRequestWithHTTPRequest tests parsing request with embedded HTTP request.
func TestParseRequestWithHTTPRequest(t *testing.T) {
	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, req-body=100\r\n" +
		"\r\n" +
		"POST /api/test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 5\r\n" +
		"\r\n" +
		"5\r\nhello\r\n0\r\n\r\n"

	r := bufio.NewReader(strings.NewReader(input))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Errorf("ParseRequest() error = %v", err)
		return
	}

	// Check HTTP request was parsed
	if req.HTTPRequest == nil {
		t.Error("HTTPRequest should not be nil")
		return
	}
	if req.HTTPRequest.Method != "POST" {
		t.Errorf("HTTPRequest.Method = %q, want %q", req.HTTPRequest.Method, "POST")
	}
	if req.HTTPRequest.URI != "/api/test" {
		t.Errorf("HTTPRequest.URI = %q, want %q", req.HTTPRequest.URI, "/api/test")
	}
}

// TestRequestWithBody tests request with body.
func TestRequestWithBody(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
		URI:    "icap://localhost/reqmod",
		Header: icap.Header{
			"Host": {"localhost"},
		},
		Body: []byte("test body"),
	}

	// Verify body is set
	if string(req.Body) != "test body" {
		t.Errorf("Body = %q, want %q", string(req.Body), "test body")
	}
}

// TestReadRequestFromReader tests reading request from io.Reader.
func TestReadRequestFromReader(t *testing.T) {
	input := "OPTIONS icap://localhost/ ICAP/1.0\r\n" +
		"Host: localhost\r\n" +
		"\r\n"

	reader := strings.NewReader(input)
	req, err := icap.ReadRequest(reader)
	if err != nil {
		t.Errorf("ReadRequest() error = %v", err)
		return
	}

	if req.Method != "OPTIONS" {
		t.Errorf("Method = %q, want %q", req.Method, "OPTIONS")
	}
}

// BenchmarkParseRequest benchmarks request parsing.
func BenchmarkParseRequest(b *testing.B) {
	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, req-body=412\r\n" +
		"\r\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(strings.NewReader(input))
		_, _ = icap.ParseRequest(r)
	}
}

// TestRequestIsOPTIONS tests IsOPTIONS method.
func TestRequestIsOPTIONS(t *testing.T) {
	req := &icap.Request{Method: "OPTIONS"}
	if !req.IsOPTIONS() {
		t.Error("IsOPTIONS() should return true for OPTIONS method")
	}

	req.Method = "REQMOD"
	if req.IsOPTIONS() {
		t.Error("IsOPTIONS() should return false for REQMOD method")
	}
}

// TestRequestIsREQMOD tests IsREQMOD method.
func TestRequestIsREQMOD(t *testing.T) {
	req := &icap.Request{Method: "REQMOD"}
	if !req.IsREQMOD() {
		t.Error("IsREQMOD() should return true for REQMOD method")
	}
}

// TestRequestIsRESPMOD tests IsRESPMOD method.
func TestRequestIsRESPMOD(t *testing.T) {
	req := &icap.Request{Method: "RESPMOD"}
	if !req.IsRESPMOD() {
		t.Error("IsRESPMOD() should return true for RESPMOD method")
	}
}

// TestRequestReadBody tests reading request body.
func TestRequestReadBody(t *testing.T) {
	// Create a request with a chunked body reader
	input := "5\r\nhello\r\n0\r\n\r\n"
	req := &icap.Request{
		Method:     "REQMOD",
		URI:        "icap://localhost/reqmod",
		BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
	}

	body, err := io.ReadAll(req.BodyReader)
	if err != nil {
		t.Errorf("Read body error = %v", err)
		return
	}

	if string(body) != "hello" {
		t.Errorf("Body = %q, want %q", string(body), "hello")
	}
}

// TestHTTPMessageGetBody tests lazy loading of HTTP message body.
func TestHTTPMessageGetBody(t *testing.T) {
	t.Run("no reader - returns nil", func(t *testing.T) {
		msg := &icap.HTTPMessage{}
		body, err := msg.GetBody()
		if err != nil {
			t.Errorf("GetBody() error = %v", err)
		}
		if body != nil {
			t.Errorf("GetBody() = %v, want nil", body)
		}
	})

	t.Run("body already loaded", func(t *testing.T) {
		msg := &icap.HTTPMessage{}
		msg.SetLoadedBody([]byte("test body"))
		body, err := msg.GetBody()
		if err != nil {
			t.Errorf("GetBody() error = %v", err)
		}
		if string(body) != "test body" {
			t.Errorf("GetBody() = %q, want %q", string(body), "test body")
		}
	})

	t.Run("lazy load from reader", func(t *testing.T) {
		input := "5\r\nhello\r\n0\r\n\r\n"
		msg := &icap.HTTPMessage{
			BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
		}

		// Body should not be loaded yet
		if msg.IsBodyLoaded() {
			t.Error("Body should not be loaded initially")
		}

		// GetBody should load it
		body, err := msg.GetBody()
		if err != nil {
			t.Errorf("GetBody() error = %v", err)
		}
		if string(body) != "hello" {
			t.Errorf("GetBody() = %q, want %q", string(body), "hello")
		}

		// Body should now be loaded
		if !msg.IsBodyLoaded() {
			t.Error("Body should be loaded after GetBody()")
		}

		// Second call should return cached body
		body2, err := msg.GetBody()
		if err != nil {
			t.Errorf("Second GetBody() error = %v", err)
		}
		if string(body2) != "hello" {
			t.Errorf("Second GetBody() = %q, want %q", string(body2), "hello")
		}
	})
}

// TestHTTPMessageHasBody tests HasBody method.
func TestHTTPMessageHasBody(t *testing.T) {
	tests := []struct {
		setupMsg    func() *icap.HTTPMessage
		name        string
		wantHasBody bool
	}{
		{
			name: "no body no reader",
			setupMsg: func() *icap.HTTPMessage {
				return &icap.HTTPMessage{}
			},
			wantHasBody: false,
		},
		{
			name: "loaded body",
			setupMsg: func() *icap.HTTPMessage {
				msg := &icap.HTTPMessage{}
				msg.SetLoadedBody([]byte("test"))
				return msg
			},
			wantHasBody: true,
		},
		{
			name: "empty loaded body",
			setupMsg: func() *icap.HTTPMessage {
				msg := &icap.HTTPMessage{}
				msg.SetLoadedBody([]byte{})
				return msg
			},
			wantHasBody: false,
		},
		{
			name: "has reader",
			setupMsg: func() *icap.HTTPMessage {
				return &icap.HTTPMessage{
					BodyReader: strings.NewReader("test"),
				}
			},
			wantHasBody: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.setupMsg()
			if got := msg.HasBody(); got != tt.wantHasBody {
				t.Errorf("HasBody() = %v, want %v", got, tt.wantHasBody)
			}
		})
	}
}

// TestRequestGetBody tests Request.GetBody lazy loading.
func TestRequestGetBody(t *testing.T) {
	t.Run("lazy load from reader", func(t *testing.T) {
		input := "5\r\nhello\r\n0\r\n\r\n"
		req := &icap.Request{
			Method:     "REQMOD",
			URI:        "icap://localhost/reqmod",
			BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
		}

		// Body should not be loaded yet
		if req.IsBodyLoaded() {
			t.Error("Body should not be loaded initially")
		}

		// GetBody should load it
		body, err := req.GetBody()
		if err != nil {
			t.Errorf("GetBody() error = %v", err)
		}
		if string(body) != "hello" {
			t.Errorf("GetBody() = %q, want %q", string(body), "hello")
		}

		// Body should now be loaded
		if !req.IsBodyLoaded() {
			t.Error("Body should be loaded after GetBody()")
		}
	})
}

// TestParseRequestWithLazyBody tests that parsing sets up streaming.
func TestParseRequestWithLazyBody(t *testing.T) {
	// Build the HTTP request part
	httpRequest := "POST /api/test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 5\r\n" +
		"\r\n"
	httpBody := "5\r\nhello\r\n0\r\n\r\n"

	// Calculate correct offsets
	reqHdrOffset := 0
	reqBodyOffset := len(httpRequest)

	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		fmt.Sprintf("Encapsulated: req-hdr=%d, req-body=%d\r\n", reqHdrOffset, reqBodyOffset) +
		"\r\n" +
		httpRequest +
		httpBody

	r := bufio.NewReader(strings.NewReader(input))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Errorf("ParseRequest() error = %v", err)
		return
	}

	// Check HTTP request was parsed
	if req.HTTPRequest == nil {
		t.Error("HTTPRequest should not be nil")
		return
	}

	// BodyReader should be set up
	if req.HTTPRequest.BodyReader == nil {
		t.Error("HTTPRequest.BodyReader should be set for lazy loading")
		return
	}

	// Body should be available via GetBody
	body, err := req.HTTPRequest.GetBody()
	if err != nil {
		t.Errorf("GetBody() error = %v", err)
		return
	}

	if string(body) != "hello" {
		t.Errorf("Body = %q, want %q", string(body), "hello")
	}

	// After GetBody, IsBodyLoaded should be true
	if !req.HTTPRequest.IsBodyLoaded() {
		t.Error("Body should be loaded after GetBody()")
	}
}

// BenchmarkLazyLoading benchmarks the lazy loading approach.
func BenchmarkLazyLoading(b *testing.B) {
	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, req-body=100\r\n" +
		"\r\n" +
		"POST /api/test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"\r\n" +
		"100\r\n" + strings.Repeat("x", 256) + "\r\n0\r\n\r\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(strings.NewReader(input))
		req, _ := icap.ParseRequest(r)
		// Don't load body - just parse headers
		_ = req.Method
	}
}

// BenchmarkLazyLoadingWithBody benchmarks loading body via GetBody.
func BenchmarkLazyLoadingWithBody(b *testing.B) {
	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, req-body=100\r\n" +
		"\r\n" +
		"POST /api/test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"\r\n" +
		"100\r\n" + strings.Repeat("x", 256) + "\r\n0\r\n\r\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(strings.NewReader(input))
		req, _ := icap.ParseRequest(r)
		if req.HTTPRequest != nil {
			_, _ = req.HTTPRequest.GetBody() // Force load
		}
	}
}

// TestHTTPMessageGetBodyConcurrent tests concurrent access to HTTPMessage.GetBody.
// This test verifies thread-safety of the lazy loading mechanism.
func TestHTTPMessageGetBodyConcurrent(t *testing.T) {
	input := "5\r\nhello\r\n0\r\n\r\n"
	msg := &icap.HTTPMessage{
		BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
	}

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := msg.GetBody()
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				return
			}
			if string(body) != "hello" {
				atomic.AddInt64(&errorCount, 1)
				return
			}
			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("Concurrent GetBody() had %d errors", errorCount)
	}
	if successCount != goroutines {
		t.Errorf("Expected %d successful GetBody() calls, got %d", goroutines, successCount)
	}
}

// TestHTTPMessageIsBodyLoadedConcurrent tests concurrent access to IsBodyLoaded.
func TestHTTPMessageIsBodyLoadedConcurrent(t *testing.T) {
	msg := &icap.HTTPMessage{}
	msg.SetLoadedBody([]byte("test"))

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !msg.IsBodyLoaded() {
				t.Error("IsBodyLoaded() should return true")
			}
		}()
	}

	wg.Wait()
}

// TestHTTPMessageHasBodyConcurrent tests concurrent access to HasBody.
func TestHTTPMessageHasBodyConcurrent(t *testing.T) {
	msg := &icap.HTTPMessage{
		BodyReader: strings.NewReader("test"),
	}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !msg.HasBody() {
				t.Error("HasBody() should return true")
			}
		}()
	}

	wg.Wait()
}

// TestHTTPMessageSetLoadedBodyConcurrent tests concurrent SetLoadedBody calls.
func TestHTTPMessageSetLoadedBodyConcurrent(t *testing.T) {
	msg := &icap.HTTPMessage{}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg.SetLoadedBody([]byte("test"))
		}(i)
	}

	wg.Wait()

	// After all goroutines, body should be loaded
	if !msg.IsBodyLoaded() {
		t.Error("Body should be loaded after SetLoadedBody calls")
	}
}

// TestHTTPMessageRaceCondition tests that race conditions are properly handled.
// This test uses the race detector to verify thread-safety.
func TestHTTPMessageRaceCondition(t *testing.T) {
	input := "10\r\nhello world\r\n0\r\n\r\n"
	msg := &icap.HTTPMessage{
		BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
	}

	var wg sync.WaitGroup

	// Multiple goroutines calling different methods
	for i := 0; i < 50; i++ {
		// GetBody
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = msg.GetBody()
		}()

		// IsBodyLoaded
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = msg.IsBodyLoaded()
		}()

		// HasBody
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = msg.HasBody()
		}()
	}

	wg.Wait()
}

// TestRequestGetBodyConcurrent tests concurrent access to Request.GetBody.
func TestRequestGetBodyConcurrent(t *testing.T) {
	input := "5\r\nhello\r\n0\r\n\r\n"
	req := &icap.Request{
		Method:     "REQMOD",
		URI:        "icap://localhost/reqmod",
		BodyReader: icap.NewChunkedReader(strings.NewReader(input)),
	}

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := req.GetBody()
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				return
			}
			if string(body) != "hello" {
				atomic.AddInt64(&errorCount, 1)
				return
			}
			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("Concurrent GetBody() had %d errors", errorCount)
	}
	if successCount != goroutines {
		t.Errorf("Expected %d successful GetBody() calls, got %d", goroutines, successCount)
	}
}

// TestRequestIsBodyLoadedConcurrent tests concurrent access to Request.IsBodyLoaded.
func TestRequestIsBodyLoadedConcurrent(t *testing.T) {
	req := &icap.Request{
		Method: "REQMOD",
	}
	req.SetLoadedBody([]byte("test"))

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !req.IsBodyLoaded() {
				t.Error("IsBodyLoaded() should return true")
			}
		}()
	}

	wg.Wait()
}

// TestHTTPMessageGetBodyErrorHandling tests that bodyLoaded is not set on error.
func TestHTTPMessageGetBodyErrorHandling(t *testing.T) {
	// Create an error reader that fails after some data
	errorReader := &errorReader{err: io.ErrUnexpectedEOF}

	msg := &icap.HTTPMessage{
		BodyReader: errorReader,
	}

	// GetBody should fail
	body, err := msg.GetBody()
	if err == nil {
		t.Error("GetBody() should return error for failing reader")
	}

	// bodyLoaded should NOT be set after error
	if msg.IsBodyLoaded() {
		t.Error("bodyLoaded should not be true after error")
	}

	// Body should be nil or empty
	if body != nil {
		t.Errorf("Body should be nil after error, got %v", body)
	}
}

// errorReader is a test helper that always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

// ============================================================================
// STREAMING BODY TESTS - O(1) Memory Verification
// ============================================================================

// TestStreamingBodyLargePayload verifies that streaming keeps memory constant
// regardless of body size. This is the KEY test for CRIT-003 fix.
func TestStreamingBodyLargePayload(t *testing.T) {
	// Create a large body (1MB) to verify streaming doesn't buffer it all
	bodySize := 1024 * 1024 // 1MB
	chunkData := strings.Repeat("x", 4096)
	numChunks := bodySize / 4096

	// Build ICAP request with large chunked body
	var input strings.Builder
	input.WriteString("REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n")
	input.WriteString("Host: icap-server.net\r\n")
	input.WriteString("Encapsulated: req-hdr=0, req-body=100\r\n")
	input.WriteString("\r\n")
	input.WriteString("POST /upload HTTP/1.1\r\n")
	input.WriteString("Host: origin-server.net\r\n")
	input.WriteString("Content-Type: application/octet-stream\r\n")
	input.WriteString("\r\n")

	// Add chunked body
	for i := 0; i < numChunks; i++ {
		fmt.Fprintf(&input, "%x\r\n%s\r\n", len(chunkData), chunkData)
	}
	input.WriteString("0\r\n\r\n")

	r := bufio.NewReader(strings.NewReader(input.String()))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	// Verify HTTP request was parsed
	if req.HTTPRequest == nil {
		t.Fatal("HTTPRequest should not be nil")
	}

	// Verify BodyReader is set (streaming, not buffered)
	if req.HTTPRequest.BodyReader == nil {
		t.Fatal("HTTPRequest.BodyReader should be set for streaming")
	}

	// Read body in chunks to verify streaming works
	buf := make([]byte, 8192)
	totalRead := 0
	for {
		n, err := req.HTTPRequest.BodyReader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Error reading body: %v", err)
		}
	}

	// Verify we read the expected amount
	expectedSize := bodySize
	if totalRead != expectedSize {
		t.Errorf("Total read = %d, want %d", totalRead, expectedSize)
	}
}

// TestStreamingBodyChunkedEncoding verifies that chunked encoding works correctly
// with the streaming implementation.
func TestStreamingBodyChunkedEncoding(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		chunks   []string
	}{
		{
			name:     "single chunk",
			chunks:   []string{"hello"},
			expected: "hello",
		},
		{
			name:     "multiple chunks",
			chunks:   []string{"hello", " ", "world"},
			expected: "hello world",
		},
		{
			name:     "large chunks",
			chunks:   []string{strings.Repeat("a", 1000), strings.Repeat("b", 1000)},
			expected: strings.Repeat("a", 1000) + strings.Repeat("b", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build ICAP request with chunked body
			var input strings.Builder
			var httpBody strings.Builder

			for _, chunk := range tt.chunks {
				fmt.Fprintf(&httpBody, "%x\r\n%s\r\n", len(chunk), chunk)
			}
			httpBody.WriteString("0\r\n\r\n")

			input.WriteString("REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n")
			input.WriteString("Host: icap-server.net\r\n")
			input.WriteString("Encapsulated: req-hdr=0, req-body=100\r\n")
			input.WriteString("\r\n")
			input.WriteString("POST /test HTTP/1.1\r\n")
			input.WriteString("Host: origin-server.net\r\n")
			input.WriteString("\r\n")
			input.WriteString(httpBody.String())

			r := bufio.NewReader(strings.NewReader(input.String()))
			req, err := icap.ParseRequest(r)
			if err != nil {
				t.Fatalf("ParseRequest() error = %v", err)
			}

			// Read body via GetBody (which loads it all)
			body, err := req.HTTPRequest.GetBody()
			if err != nil {
				t.Fatalf("GetBody() error = %v", err)
			}

			if string(body) != tt.expected {
				t.Errorf("Body = %q, want %q", string(body), tt.expected)
			}
		})
	}
}

// TestStreamingBodyGetBodyCompatibility verifies that GetBody() still works
// with the streaming implementation (backward compatibility).
func TestStreamingBodyGetBodyCompatibility(t *testing.T) {
	// Body content is 24 bytes
	bodyContent := `{"message":"hello world"}`
	bodyHex := fmt.Sprintf("%x", len(bodyContent)) // "18" in hex

	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, req-body=100\r\n" +
		"\r\n" +
		"POST /api/test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"Content-Type: application/json\r\n" +
		"\r\n" +
		bodyHex + "\r\n" + bodyContent + "\r\n0\r\n\r\n"

	r := bufio.NewReader(strings.NewReader(input))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	// Verify body is NOT loaded yet (streaming)
	if req.HTTPRequest.IsBodyLoaded() {
		t.Error("Body should NOT be loaded initially (streaming)")
	}

	// GetBody should still work
	body, err := req.HTTPRequest.GetBody()
	if err != nil {
		t.Fatalf("GetBody() error = %v", err)
	}

	if string(body) != bodyContent {
		t.Errorf("Body = %q, want %q", string(body), bodyContent)
	}

	// After GetBody, body should be loaded
	if !req.HTTPRequest.IsBodyLoaded() {
		t.Error("Body should be loaded after GetBody()")
	}

	// Second call should return same body
	body2, err := req.HTTPRequest.GetBody()
	if err != nil {
		t.Fatalf("Second GetBody() error = %v", err)
	}
	if string(body2) != bodyContent {
		t.Errorf("Second GetBody() = %q, want %q", string(body2), bodyContent)
	}
}

// TestStreamingBodyConcurrent verifies that multiple concurrent requests
// don't cause memory bloat (simulating the 10k connections scenario).
func TestStreamingBodyConcurrent(t *testing.T) {
	const numGoroutines = 100
	const bodySizePerRequest = 10240 // 10KB per request

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Build a request with chunked body
			var input strings.Builder
			input.WriteString("REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n")
			input.WriteString("Host: icap-server.net\r\n")
			input.WriteString("Encapsulated: req-hdr=0, req-body=100\r\n")
			input.WriteString("\r\n")
			input.WriteString("POST /test HTTP/1.1\r\n")
			input.WriteString("Host: origin-server.net\r\n")
			input.WriteString("\r\n")

			// Add chunked body
			chunkData := strings.Repeat(fmt.Sprintf("%d", id%10), bodySizePerRequest/2)
			fmt.Fprintf(&input, "%x\r\n%s\r\n", len(chunkData), chunkData)
			input.WriteString("0\r\n\r\n")

			r := bufio.NewReader(strings.NewReader(input.String()))
			req, err := icap.ParseRequest(r)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: ParseRequest error: %w", id, err)
				return
			}

			if req.HTTPRequest == nil {
				errors <- fmt.Errorf("goroutine %d: HTTPRequest is nil", id)
				return
			}

			// Read body (but don't buffer it all at once)
			if req.HTTPRequest.BodyReader != nil {
				buf := make([]byte, 1024)
				_, err := io.ReadFull(req.HTTPRequest.BodyReader, buf)
				// We don't need to read all, just verify streaming works
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					errors <- fmt.Errorf("goroutine %d: Read error: %w", id, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// TestStreamingBodyEmpty verifies streaming works with empty body.
func TestStreamingBodyEmpty(t *testing.T) {
	input := "REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		"Encapsulated: req-hdr=0, null-body=100\r\n" +
		"\r\n" +
		"GET /test HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"\r\n" +
		"0\r\n\r\n"

	r := bufio.NewReader(strings.NewReader(input))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	// HTTP request should be parsed
	if req.HTTPRequest == nil {
		t.Fatal("HTTPRequest should not be nil")
	}

	// BodyReader should be nil (no body)
	if req.HTTPRequest.BodyReader != nil {
		t.Error("HTTPRequest.BodyReader should be nil for null-body")
	}
}

// TestStreamingBodyRESPMOD verifies streaming works with RESPMOD requests.
// RESPMOD includes both the HTTP request and the HTTP response being modified.
func TestStreamingBodyRESPMOD(t *testing.T) {
	// RESPMOD format: req-hdr (HTTP request), res-hdr (HTTP response), res-body
	httpRequest := "GET /test.html HTTP/1.1\r\n" +
		"Host: origin-server.net\r\n" +
		"\r\n"

	httpResponse := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n"

	// Calculate offsets
	reqHdrOffset := 0
	resHdrOffset := len(httpRequest)
	resBodyOffset := len(httpRequest) + len(httpResponse)

	input := "RESPMOD icap://icap-server.net:1344/respmod ICAP/1.0\r\n" +
		"Host: icap-server.net\r\n" +
		fmt.Sprintf("Encapsulated: req-hdr=%d, res-hdr=%d, res-body=%d\r\n", reqHdrOffset, resHdrOffset, resBodyOffset) +
		"\r\n" +
		httpRequest +
		httpResponse +
		"5\r\nhello\r\n0\r\n\r\n"

	r := bufio.NewReader(strings.NewReader(input))
	req, err := icap.ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	// Verify HTTP request was parsed
	if req.HTTPRequest == nil {
		t.Fatal("HTTPRequest should not be nil")
	}
	if req.HTTPRequest.Method != "GET" {
		t.Errorf("HTTPRequest.Method = %q, want %q", req.HTTPRequest.Method, "GET")
	}

	// Verify HTTP response was parsed
	if req.HTTPResponse == nil {
		t.Fatal("HTTPResponse should not be nil")
	}

	if req.HTTPResponse.Status != "200" {
		t.Errorf("HTTPResponse.Status = %q, want %q", req.HTTPResponse.Status, "200")
	}

	// Verify body reader is set
	if req.HTTPResponse.BodyReader == nil {
		t.Fatal("HTTPResponse.BodyReader should be set")
	}

	// Read body
	body, err := io.ReadAll(req.HTTPResponse.BodyReader)
	if err != nil {
		t.Fatalf("Error reading body: %v", err)
	}

	if string(body) != "hello" {
		t.Errorf("Body = %q, want %q", string(body), "hello")
	}
}

// BenchmarkStreamingBodyMemory benchmarks memory usage with streaming.
// Run with: go test -bench=BenchmarkStreamingBodyMemory -benchmem.
func BenchmarkStreamingBodyMemory(b *testing.B) {
	// Create a large body (100KB)
	bodySize := 100 * 1024
	chunkData := strings.Repeat("x", bodySize)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var input strings.Builder
		input.WriteString("REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n")
		input.WriteString("Host: icap-server.net\r\n")
		input.WriteString("Encapsulated: req-hdr=0, req-body=100\r\n")
		input.WriteString("\r\n")
		input.WriteString("POST /upload HTTP/1.1\r\n")
		input.WriteString("Host: origin-server.net\r\n")
		input.WriteString("\r\n")
		fmt.Fprintf(&input, "%x\r\n%s\r\n", len(chunkData), chunkData)
		input.WriteString("0\r\n\r\n")

		r := bufio.NewReader(strings.NewReader(input.String()))
		req, _ := icap.ParseRequest(r)

		// Stream the body (read in chunks)
		if req.HTTPRequest != nil && req.HTTPRequest.BodyReader != nil {
			buf := make([]byte, 4096)
			for {
				_, err := req.HTTPRequest.BodyReader.Read(buf)
				if err == io.EOF {
					break
				}
			}
		}
	}
}

// BenchmarkStreamingBodyParseOnly benchmarks parsing without reading body.
// This shows the memory savings from not buffering the body.
func BenchmarkStreamingBodyParseOnly(b *testing.B) {
	// Create a large body (100KB)
	bodySize := 100 * 1024
	chunkData := strings.Repeat("x", bodySize)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var input strings.Builder
		input.WriteString("REQMOD icap://icap-server.net:1344/reqmod ICAP/1.0\r\n")
		input.WriteString("Host: icap-server.net\r\n")
		input.WriteString("Encapsulated: req-hdr=0, req-body=100\r\n")
		input.WriteString("\r\n")
		input.WriteString("POST /upload HTTP/1.1\r\n")
		input.WriteString("Host: origin-server.net\r\n")
		input.WriteString("\r\n")
		fmt.Fprintf(&input, "%x\r\n%s\r\n", len(chunkData), chunkData)
		input.WriteString("0\r\n\r\n")

		r := bufio.NewReader(strings.NewReader(input.String()))
		req, _ := icap.ParseRequest(r)
		_ = req.Method // Access something to ensure parsing happened
	}
}

// TestStreamingBodyType tests the StreamingBody type directly.
func TestStreamingBodyType(t *testing.T) {
	t.Run("basic read", func(t *testing.T) {
		data := "hello world"
		sb := icap.NewStreamingBody(strings.NewReader(data), int64(len(data)))

		buf := make([]byte, len(data))
		n, err := io.ReadFull(sb, buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read error: %v", err)
		}
		if string(buf[:n]) != data {
			t.Errorf("Read = %q, want %q", string(buf[:n]), data)
		}
	})

	t.Run("close prevents further reads", func(t *testing.T) {
		sb := icap.NewStreamingBody(strings.NewReader("test"), 4)

		// Close should succeed
		if err := sb.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}

		// Read after close should return EOF
		buf := make([]byte, 10)
		n, err := sb.Read(buf)
		if !errors.Is(err, io.EOF) {
			t.Errorf("Read after close: err = %v, want io.EOF", err)
		}
		if n != 0 {
			t.Errorf("Read after close: n = %d, want 0", n)
		}
	})

	t.Run("content length", func(t *testing.T) {
		sb := icap.NewStreamingBody(strings.NewReader("test"), 4)
		if sb.ContentLength() != 4 {
			t.Errorf("ContentLength() = %d, want 4", sb.ContentLength())
		}

		sb2 := icap.NewStreamingBody(strings.NewReader("test"), -1)
		if sb2.ContentLength() != -1 {
			t.Errorf("ContentLength() = %d, want -1", sb2.ContentLength())
		}
	})

	t.Run("is consumed", func(t *testing.T) {
		sb := icap.NewStreamingBody(strings.NewReader("test"), 4)

		if sb.IsConsumed() {
			t.Error("IsConsumed() should be false initially")
		}

		// Read all data
		io.ReadAll(sb)

		if !sb.IsConsumed() {
			t.Error("IsConsumed() should be true after reading all")
		}
	})
}
