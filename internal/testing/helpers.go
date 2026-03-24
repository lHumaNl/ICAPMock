// Package testing provides utilities and helpers for testing the ICAP Mock Server.
//
// It includes:
//   - ICAP request/response builders
//   - Server harness for integration tests
//   - Concurrent test helpers
//   - Mock implementations for dependencies
//   - Assertion helpers
//
// Usage:
//
//	import "github.com/icap-mock/icap-mock/internal/testing"
//
//	req := testing.BuildICAPRequest("REQMOD", "icap://localhost/reqmod", nil, nil)
//	harness := testing.NewServerHarness(t, testConfig)
//	harness.Start()
//	defer harness.Stop(ctx)
package testing

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// BuildICAPRequest creates a new ICAP request with the specified parameters.
//
// Parameters:
//   - method: ICAP method ("REQMOD", "RESPMOD", or "OPTIONS")
//   - uri: ICAP URI (e.g., "icap://localhost/reqmod")
//   - headers: Optional ICAP headers map
//   - body: Optional request body
//
// Returns:
//   - A new ICAP request
//
// Example:
//
//	req := BuildICAPRequest("REQMOD", "icap://localhost/reqmod",
//	    map[string]string{"Host": "example.com"}, nil)
func BuildICAPRequest(method, uri string, headers map[string]string, body []byte) *icap.Request {
	req := &icap.Request{
		Method: method,
		URI:    uri,
		Header: make(icap.Header),
	}

	if headers != nil {
		for k, v := range headers {
			req.SetHeader(k, v)
		}
	}

	if body != nil {
		req.Body = body
	}

	return req
}

// BuildICAPRequestWithHTTP creates an ICAP request with an encapsulated HTTP message.
//
// Parameters:
//   - method: ICAP method
//   - uri: ICAP URI
//   - httpMethod: HTTP method (e.g., "GET", "POST")
//   - httpURL: HTTP URL
//   - httpHeaders: Optional HTTP headers
//   - httpBody: Optional HTTP body
//
// Returns:
//   - A new ICAP request with encapsulated HTTP message
//
// Example:
//
//	req := BuildICAPRequestWithHTTP("REQMOD", "icap://localhost/reqmod",
//	    "GET", "http://example.com", map[string]string{"Host": "example.com"}, nil)
func BuildICAPRequestWithHTTP(method, uri, httpMethod, httpURL string, httpHeaders map[string]string, httpBody []byte) *icap.Request {
	req := &icap.Request{
		Method: method,
		URI:    uri,
		Header: make(icap.Header),
	}

	httpReq := &icap.HTTPMessage{
		Header: make(icap.Header),
	}

	if httpHeaders != nil {
		for k, v := range httpHeaders {
			httpReq.Header.Set(k, v)
		}
	}

	if httpBody != nil {
		httpReq.Body = httpBody
	}

	httpReq.Method = httpMethod
	httpReq.URI = httpURL

	if method == icap.MethodREQMOD {
		req.HTTPRequest = httpReq
	} else if method == icap.MethodRESPMOD {
		req.HTTPResponse = httpReq
	}

	return req
}

// BuildICAPResponse creates a new ICAP response with the specified parameters.
//
// Parameters:
//   - statusCode: ICAP status code (e.g., 200, 204)
//   - headers: Optional ICAP headers map
//   - body: Optional response body
//
// Returns:
//   - A new ICAP response
//
// Example:
//
//	resp := BuildICAPResponse(200, map[string]string{"ISTag": "test123"}, nil)
func BuildICAPResponse(statusCode int, headers map[string]string, body []byte) *icap.Response {
	resp := icap.NewResponse(statusCode)

	if headers != nil {
		for k, v := range headers {
			resp.SetHeader(k, v)
		}
	}

	if body != nil {
		resp.Body = body
	}

	return resp
}

// BuildICAPResponseWithHTTP creates an ICAP response with an encapsulated HTTP message.
//
// Parameters:
//   - statusCode: ICAP status code
//   - httpStatusCode: HTTP status code (e.g., 200, 404)
//   - httpHeaders: Optional HTTP headers
//   - httpBody: Optional HTTP body
//
// Returns:
//   - A new ICAP response with encapsulated HTTP message
//
// Example:
//
//	resp := BuildICAPResponseWithHTTP(200, 200,
//	    map[string]string{"Content-Type": "text/html"}, []byte("<html>...</html>"))
func BuildICAPResponseWithHTTP(statusCode, httpStatusCode int, httpHeaders map[string]string, httpBody []byte) *icap.Response {
	resp := icap.NewResponse(statusCode)

	httpResp := &icap.HTTPMessage{
		Header: make(icap.Header),
	}

	if httpHeaders != nil {
		for k, v := range httpHeaders {
			httpResp.Header.Set(k, v)
		}
	}

	if httpBody != nil {
		httpResp.Body = httpBody
	}

	httpResp.Status = strconv.Itoa(httpStatusCode)

	resp.HTTPResponse = httpResp

	return resp
}

// AssertICAPResponse asserts that two ICAP responses are equal.
//
// Parameters:
//   - t: Testing instance
//   - got: The actual response received
//   - want: The expected response
//
// It checks:
//   - Status code
//   - Protocol version
//   - Headers
//   - Body
//   - Encapsulated HTTP message (if present)
func AssertICAPResponse(t *testing.T, got, want *icap.Response) {
	t.Helper()

	if got.StatusCode != want.StatusCode {
		t.Errorf("StatusCode = %d, want %d", got.StatusCode, want.StatusCode)
	}

	if got.Proto != want.Proto {
		t.Errorf("Proto = %q, want %q", got.Proto, want.Proto)
	}

	if len(got.Header) != len(want.Header) {
		t.Errorf("Header count = %d, want %d", len(got.Header), len(want.Header))
	}

	for k, wantVals := range want.Header {
		gotVals, ok := got.Header[k]
		if !ok {
			t.Errorf("Header missing key %q", k)
			continue
		}
		if len(gotVals) != len(wantVals) {
			t.Errorf("Header[%q] length = %d, want %d", k, len(gotVals), len(wantVals))
		}
		for i, v := range wantVals {
			if i >= len(gotVals) || gotVals[i] != v {
				t.Errorf("Header[%q][%d] = %q, want %q", k, i, gotVals[i], v)
			}
		}
	}

	if !bytes.Equal(got.Body, want.Body) {
		t.Errorf("Body length = %d, want %d", len(got.Body), len(want.Body))
	}

	if want.HTTPRequest != nil {
		if got.HTTPRequest == nil {
			t.Error("HTTPRequest is nil, want non-nil")
		} else {
			assertHTTPMessage(t, got.HTTPRequest, want.HTTPRequest, "HTTPRequest")
		}
	}

	if want.HTTPResponse != nil {
		if got.HTTPResponse == nil {
			t.Error("HTTPResponse is nil, want non-nil")
		} else {
			assertHTTPMessage(t, got.HTTPResponse, want.HTTPResponse, "HTTPResponse")
		}
	}
}

// assertHTTPMessage asserts that two HTTP messages are equal.
func assertHTTPMessage(t *testing.T, got, want *icap.HTTPMessage, prefix string) {
	t.Helper()

	if got.Method != want.Method {
		t.Errorf("%s.Method = %q, want %q", prefix, got.Method, want.Method)
	}

	if got.URI != want.URI {
		t.Errorf("%s.URI = %q, want %q", prefix, got.URI, want.URI)
	}

	if got.Status != want.Status {
		t.Errorf("%s.Status = %q, want %q", prefix, got.Status, want.Status)
	}

	if len(got.Header) != len(want.Header) {
		t.Errorf("%s.Header count = %d, want %d", prefix, len(got.Header), len(want.Header))
	}

	for k, wantVals := range want.Header {
		gotVals, ok := got.Header[k]
		if !ok {
			t.Errorf("%s.Header missing key %q", prefix, k)
			continue
		}
		if len(gotVals) != len(wantVals) {
			t.Errorf("%s.Header[%q] length = %d, want %d", prefix, k, len(gotVals), len(wantVals))
		}
		for i, v := range wantVals {
			if i >= len(gotVals) || gotVals[i] != v {
				t.Errorf("%s.Header[%q][%d] = %q, want %q", prefix, k, i, gotVals[i], v)
			}
		}
	}

	if !bytes.Equal(got.Body, want.Body) {
		t.Errorf("%s.Body length = %d, want %d", prefix, len(got.Body), len(want.Body))
	}
}

// AssertICAPRequest asserts that two ICAP requests are equal.
//
// Parameters:
//   - t: Testing instance
//   - got: The actual request received
//   - want: The expected request
//
// It checks:
//   - Method
//   - URI
//   - Protocol version
//   - Headers
//   - Body
//   - Encapsulated HTTP message (if present)
func AssertICAPRequest(t *testing.T, got, want *icap.Request) {
	t.Helper()

	if got.Method != want.Method {
		t.Errorf("Method = %q, want %q", got.Method, want.Method)
	}

	if got.URI != want.URI {
		t.Errorf("URI = %q, want %q", got.URI, want.URI)
	}

	if got.Proto != want.Proto {
		t.Errorf("Proto = %q, want %q", got.Proto, want.Proto)
	}

	if len(got.Header) != len(want.Header) {
		t.Errorf("Header count = %d, want %d", len(got.Header), len(want.Header))
	}

	for k, wantVals := range want.Header {
		gotVals, ok := got.Header[k]
		if !ok {
			t.Errorf("Header missing key %q", k)
			continue
		}
		if len(gotVals) != len(wantVals) {
			t.Errorf("Header[%q] length = %d, want %d", k, len(gotVals), len(wantVals))
		}
		for i, v := range wantVals {
			if i >= len(gotVals) || gotVals[i] != v {
				t.Errorf("Header[%q][%d] = %q, want %q", k, i, gotVals[i], v)
			}
		}
	}

	if !bytes.Equal(got.Body, want.Body) {
		t.Errorf("Body length = %d, want %d", len(got.Body), len(want.Body))
	}

	if want.HTTPRequest != nil {
		if got.HTTPRequest == nil {
			t.Error("HTTPRequest is nil, want non-nil")
		} else {
			assertHTTPMessage(t, got.HTTPRequest, want.HTTPRequest, "HTTPRequest")
		}
	}

	if want.HTTPResponse != nil {
		if got.HTTPResponse == nil {
			t.Error("HTTPResponse is nil, want non-nil")
		} else {
			assertHTTPMessage(t, got.HTTPResponse, want.HTTPResponse, "HTTPResponse")
		}
	}
}

// WithTimeout creates a new context with a timeout and registers cleanup.
//
// If the timeout is exceeded, the test is failed with a descriptive error.
//
// Parameters:
//   - t: Testing instance
//   - ctx: Parent context
//   - timeout: Timeout duration
//
// Returns:
//   - A new context with timeout
//
// Example:
//
//	ctx := WithTimeout(t, context.Background(), 5*time.Second)
//	defer ctx.Cancel()
func WithTimeout(t *testing.T, ctx context.Context, timeout time.Duration) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, timeout)

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			t.Errorf("Test timed out after %v", timeout)
		}
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return ctx
}

// ParseRawICAPRequest parses a raw ICAP request string into an ICAP request.
//
// Parameters:
//   - t: Testing instance
//   - raw: Raw ICAP request string
//
// Returns:
//   - Parsed ICAP request
//
// Example:
//
//	raw := "REQMOD icap://localhost/reqmod ICAP/1.0\r\n" +
//	       "Host: localhost\r\n\r\n"
//	req := ParseRawICAPRequest(t, raw)
func ParseRawICAPRequest(t *testing.T, raw string) *icap.Request {
	t.Helper()

	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader([]byte(raw))))

	line, err := reader.ReadLine()
	if err != nil {
		t.Fatalf("Failed to read request line: %v", err)
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		t.Fatalf("Invalid request line: %q", line)
	}

	req := &icap.Request{
		Method: parts[0],
		URI:    parts[1],
		Proto:  parts[2],
		Header: make(icap.Header),
	}

	mimeHeader, err := reader.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read headers: %v", err)
	}

	for k, v := range mimeHeader {
		req.Header[k] = v
	}

	return req
}

// ParseRawICAPResponse parses a raw ICAP response string into an ICAP response.
//
// Parameters:
//   - t: Testing instance
//   - raw: Raw ICAP response string
//
// Returns:
//   - Parsed ICAP response
//
// Example:
//
//	raw := "ICAP/1.0 200 OK\r\n" +
//	       "ISTag: test123\r\n\r\n"
//	resp := ParseRawICAPResponse(t, raw)
func ParseRawICAPResponse(t *testing.T, raw string) *icap.Response {
	t.Helper()

	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader([]byte(raw))))

	line, err := reader.ReadLine()
	if err != nil {
		t.Fatalf("Failed to read status line: %v", err)
	}

	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		t.Fatalf("Invalid status line: %q", line)
	}

	statusParts := strings.SplitN(parts[0], "/", 2)
	if len(statusParts) != 2 {
		t.Fatalf("Invalid protocol: %q", parts[0])
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("Invalid status code: %q", parts[1])
	}

	resp := &icap.Response{
		Proto:      statusParts[0],
		StatusCode: statusCode,
		Header:     make(icap.Header),
	}

	mimeHeader, err := reader.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read headers: %v", err)
	}

	for k, v := range mimeHeader {
		resp.Header[k] = v
	}

	return resp
}

// GetFreePort returns a free TCP port that can be used for testing.
//
// Parameters:
//   - t: Testing instance
//
// Returns:
//   - A free port number
//
// Example:
//
//	port := GetFreePort(t)
//	cfg := &config.ServerConfig{Port: port}
func GetFreePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	defer l.Close()

	addr := l.Addr().(*net.TCPAddr)
	return addr.Port
}

// GetLocalIP returns the local IP address for testing.
//
// Parameters:
//   - t: Testing instance
//
// Returns:
//   - Local IP address string
func GetLocalIP(t *testing.T) string {
	t.Helper()

	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		t.Fatalf("Failed to get local IP: %v", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// GenerateISTag generates a unique ICAP service tag for testing.
//
// Returns:
//   - A unique ISTag string
//
// Example:
//
//	istag := GenerateISTag()
//	resp.SetHeader("ISTag", istag)
func GenerateISTag() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}
