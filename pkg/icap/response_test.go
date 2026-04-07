// Copyright 2026 ICAP Mock

package icap_test

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestNewResponse tests creating a new response.
func TestNewResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantNil    bool
	}{
		{
			name:       "200 OK",
			statusCode: 200,
		},
		{
			name:       "204 No Content",
			statusCode: 204,
		},
		{
			name:       "400 Bad Request",
			statusCode: 400,
		},
		{
			name:       "500 Server Error",
			statusCode: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := icap.NewResponse(tt.statusCode)
			if resp == nil {
				t.Error("NewResponse() returned nil")
				return
			}
			if resp.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.statusCode)
			}
			if resp.Header == nil {
				t.Error("Header should be initialized")
			}
		})
	}
}

// TestResponseStatusText tests getting status text for status codes.
func TestResponseStatusText(t *testing.T) {
	tests := []struct {
		wantText string
		code     int
	}{
		{200, "OK"},
		{204, "No Content Needed"},
		{400, "Bad Request"},
		{404, "ICAP Service not found"},
		{405, "Method not allowed"},
		{500, "Server error"},
		{501, "Not implemented"},
		{502, "Bad Gateway"},
		{503, "Service overloaded"},
		{505, "ICAP version not supported"},
		{999, "Unknown"}, // Unknown code
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			got := icap.StatusText(tt.code)
			if got != tt.wantText {
				t.Errorf("StatusText(%d) = %q, want %q", tt.code, got, tt.wantText)
			}
		})
	}
}

// TestResponseSetHeader tests setting response headers.
func TestResponseSetHeader(t *testing.T) {
	resp := icap.NewResponse(200)

	resp.SetHeader("ISTag", "W3E4R5")
	resp.SetHeader("Service", "ICAP-Mock-Server")

	if val, _ := resp.GetHeader("ISTag"); val != "W3E4R5" {
		t.Errorf("ISTag = %q, want %q", val, "W3E4R5")
	}
	if val, _ := resp.GetHeader("Service"); val != "ICAP-Mock-Server" {
		t.Errorf("Service = %q, want %q", val, "ICAP-Mock-Server")
	}
}

// TestResponseGetHeader tests getting response headers.
func TestResponseGetHeader(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetHeader("Content-Type", "text/plain")

	val, exists := resp.GetHeader("content-type")
	if !exists {
		t.Error("GetHeader should find header (case-insensitive)")
	}
	if val != "text/plain" {
		t.Errorf("GetHeader = %q, want %q", val, "text/plain")
	}
}

// TestResponseWriteTo tests serializing response.
func TestResponseWriteTo(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetHeader("ISTag", "W3E4R5")
	resp.SetHeader("Service", "Test")

	var buf bytes.Buffer
	n, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()

	// Check status line
	if !strings.Contains(output, "ICAP/1.0 200 OK") {
		t.Errorf("Output missing status line: %q", output)
	}

	// Check headers
	if !strings.Contains(output, "ISTag: W3E4R5") {
		t.Errorf("Output missing ISTag header: %q", output)
	}
	if !strings.Contains(output, "Service: Test") {
		t.Errorf("Output missing Service header: %q", output)
	}

	// Check returned bytes count
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo returned %d, wrote %d", n, buf.Len())
	}
}

// TestResponseWithBody tests response with body.
func TestResponseWithBody(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetHeader("Encapsulated", "res-body=0")
	resp.Body = []byte("test response body")

	var buf bytes.Buffer
	_, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	if !strings.Contains(output, "test response body") {
		t.Errorf("Output missing body: %q", output)
	}
}

// TestResponseWithChunkedBody tests response with chunked body.
func TestResponseWithChunkedBody(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetHeader("Encapsulated", "res-body=0")

	// Create chunked body
	var bodyBuf bytes.Buffer
	chunked := icap.NewChunkedWriter(&bodyBuf)
	chunked.Write([]byte("hello"))
	chunked.Close()
	resp.Body = bodyBuf.Bytes()

	var buf bytes.Buffer
	_, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	// Should contain chunk size
	if !strings.Contains(output, "5\r\nhello") {
		t.Errorf("Output missing chunked body: %q", output)
	}
}

// TestResponse204NoContent tests 204 response without body.
func TestResponse204NoContent(t *testing.T) {
	resp := icap.NewResponse(204)

	var buf bytes.Buffer
	_, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	if !strings.Contains(output, "ICAP/1.0 204 No Content Needed") {
		t.Errorf("Output missing 204 status: %q", output)
	}
}

// TestResponseWithHTTPRequest tests response with embedded HTTP request.
func TestResponseWithHTTPRequest(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.HTTPRequest = &icap.HTTPMessage{
		Method: "POST",
		URI:    "/test",
		Proto:  "HTTP/1.1",
		Header: icap.Header{"Host": {"localhost"}},
	}

	// Calculate Encapsulated header
	resp.SetHeader("Encapsulated", "req-hdr=0")

	var buf bytes.Buffer
	_, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	if !strings.Contains(output, "POST /test HTTP/1.1") {
		t.Errorf("Output missing HTTP request: %q", output)
	}
}

// TestResponseWithHTTPResponse tests response with embedded HTTP response.
func TestResponseWithHTTPResponse(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.HTTPResponse = &icap.HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     "200",
		StatusText: "OK",
		Header:     icap.Header{"Content-Type": {"text/plain"}},
		Body:       []byte("Hello World"),
	}

	resp.SetHeader("Encapsulated", "res-hdr=0")

	var buf bytes.Buffer
	_, err := resp.WriteTo(&buf)
	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
		return
	}

	output := buf.String()
	if !strings.Contains(output, "HTTP/1.1 200 OK") {
		t.Errorf("Output missing HTTP response: %q", output)
	}
}

// TestResponseClone tests cloning a response.
func TestResponseClone(t *testing.T) {
	original := icap.NewResponse(200)
	original.SetHeader("ISTag", "original")
	original.Body = []byte("original body")

	clone := original.Clone()

	// Modify clone
	clone.SetHeader("ISTag", "modified")
	clone.Body = []byte("modified body")

	// Original should be unchanged
	if val, _ := original.GetHeader("ISTag"); val != "original" {
		t.Error("Clone should not affect original headers")
	}
	if string(original.Body) != "original body" {
		t.Error("Clone should not affect original body")
	}

	// Clone should have new values
	if val, _ := clone.GetHeader("ISTag"); val != "modified" {
		t.Error("Clone modification failed")
	}
}

// TestResponseError tests creating error responses.
func TestResponseError(t *testing.T) {
	tests := []struct {
		message      string
		wantInOutput string
		code         int
	}{
		{400, "Invalid request format", "Invalid request format"},
		{404, "Service not found", "Service not found"},
		{500, "Internal error", "Internal error"},
		{501, "Method not supported", "Method not supported"},
		{503, "Server busy", "Server busy"},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			resp := icap.NewResponseError(tt.code, tt.message)

			if resp.StatusCode != tt.code {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.code)
			}

			var buf bytes.Buffer
			resp.WriteTo(&buf)
			output := buf.String()

			if !strings.Contains(output, tt.wantInOutput) {
				t.Errorf("Output should contain %q, got %q", tt.wantInOutput, output)
			}
		})
	}
}

// TestResponseIsError tests checking if response is an error.
func TestResponseIsError(t *testing.T) {
	tests := []struct {
		code    int
		isError bool
	}{
		{200, false},
		{204, false},
		{400, true},
		{404, true},
		{405, true},
		{500, true},
		{501, true},
		{502, true},
		{503, true},
		{505, true},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			resp := icap.NewResponse(tt.code)
			if resp.IsError() != tt.isError {
				t.Errorf("IsError() for code %d = %v, want %v", tt.code, resp.IsError(), tt.isError)
			}
		})
	}
}

// TestResponseBuildEncapsulated tests building Encapsulated header.
func TestResponseBuildEncapsulated(t *testing.T) {
	resp := icap.NewResponse(200)

	// Set HTTP request
	resp.HTTPRequest = &icap.HTTPMessage{
		Method: "GET",
		URI:    "/test",
		Proto:  "HTTP/1.1",
	}

	// Set body
	resp.Body = []byte("test")

	// Build encapsulated
	encap := resp.BuildEncapsulatedHeader()

	if encap == "" {
		t.Error("BuildEncapsulatedHeader should return non-empty string")
	}

	// Should contain req-hdr
	if !strings.Contains(encap, "req-hdr") {
		t.Errorf("Encapsulated header should contain req-hdr: %s", encap)
	}
}

// TestResponseWriteChunkedBody tests writing body with chunked encoding.
func TestResponseWriteChunkedBody(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetBody([]byte("hello world"))

	var buf bytes.Buffer
	_, err := resp.WriteChunkedBody(&buf)
	if err != nil {
		t.Errorf("WriteChunkedBody() error = %v", err)
		return
	}

	// Verify output is chunked
	output := buf.String()
	if !strings.HasSuffix(output, "0\r\n\r\n") {
		t.Errorf("Chunked body should end with 0\\r\\n\\r\\n, got: %q", output)
	}

	// Verify content
	r := icap.NewChunkedReader(strings.NewReader(output))
	got, _ := io.ReadAll(r)
	if string(got) != "hello world" {
		t.Errorf("Chunked content = %q, want %q", string(got), "hello world")
	}
}

// TestResponseSetBody tests setting body.
func TestResponseSetBody(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetBody([]byte("test body"))

	if string(resp.Body) != "test body" {
		t.Errorf("Body = %q, want %q", string(resp.Body), "test body")
	}
}

// TestResponseWithMethods tests OPTIONS response with Methods header.
func TestResponseWithMethods(t *testing.T) {
	resp := icap.NewOptionsResponse("test-istag", []string{"REQMOD", "RESPMOD"}, 100, 3600)

	if val, _ := resp.GetHeader("Methods"); val != "REQMOD, RESPMOD" {
		t.Errorf("Methods = %q, want %q", val, "REQMOD, RESPMOD")
	}

	if val, _ := resp.GetHeader("ISTag"); val != "test-istag" {
		t.Errorf("ISTag = %q, want %q", val, "test-istag")
	}

	if val, _ := resp.GetHeader("Max-Connections"); val != "100" {
		t.Errorf("Max-Connections = %q, want %q", val, "100")
	}

	if val, _ := resp.GetHeader("Options-TTL"); val != "3600" {
		t.Errorf("Options-TTL = %q, want %q", val, "3600")
	}
}

// TestResponseSetHTTPRequest tests setting HTTP request in response.
func TestResponseSetHTTPRequest(t *testing.T) {
	resp := icap.NewResponse(200)

	httpReq := &icap.HTTPMessage{
		Method: "POST",
		URI:    "/api/test",
		Proto:  "HTTP/1.1",
		Header: icap.Header{"Content-Type": {"application/json"}},
	}
	resp.SetHTTPRequest(httpReq)

	if resp.HTTPRequest != httpReq {
		t.Error("SetHTTPRequest failed to set HTTPRequest")
	}
}

// TestResponseSetHTTPResponse tests setting HTTP response in response.
func TestResponseSetHTTPResponse(t *testing.T) {
	resp := icap.NewResponse(200)

	httpResp := &icap.HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     "200",
		StatusText: "OK",
		Header:     icap.Header{"Content-Type": {"text/plain"}},
	}
	resp.SetHTTPResponse(httpResp)

	if resp.HTTPResponse != httpResp {
		t.Error("SetHTTPResponse failed to set HTTPResponse")
	}
}

// TestReadResponse tests parsing an ICAP response from raw bytes.
func TestReadResponse(t *testing.T) {
	tests := []struct {
		wantHeader     map[string]string
		name           string
		input          string
		wantProto      string
		wantBodyPrefix string
		wantStatus     int
		wantErr        bool
	}{
		{
			name:       "basic 204 no content",
			input:      "ICAP/1.0 204 No Content Needed\r\n\r\n",
			wantStatus: 204,
			wantProto:  "ICAP/1.0",
		},
		{
			name:       "200 with headers",
			input:      "ICAP/1.0 200 OK\r\nISTag: W3E4R5\r\nService: test\r\n\r\n",
			wantStatus: 200,
			wantProto:  "ICAP/1.0",
			wantHeader: map[string]string{"ISTag": "W3E4R5", "Service": "test"},
		},
		{
			name:           "200 with encapsulated body",
			input:          "ICAP/1.0 200 OK\r\nEncapsulated: res-body=0\r\n\r\nhello world",
			wantStatus:     200,
			wantProto:      "ICAP/1.0",
			wantHeader:     map[string]string{"Encapsulated": "res-body=0"},
			wantBodyPrefix: "hello world",
		},
		{
			name:       "200 with null-body",
			input:      "ICAP/1.0 200 OK\r\nEncapsulated: null-body=0\r\n\r\n",
			wantStatus: 200,
			wantProto:  "ICAP/1.0",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid status code",
			input:   "ICAP/1.0 abc OK\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "incomplete status line",
			input:   "ICAP/1.0 200\r\n\r\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := icap.ReadResponse(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if resp.Proto != tt.wantProto {
				t.Errorf("Proto = %q, want %q", resp.Proto, tt.wantProto)
			}
			for k, v := range tt.wantHeader {
				got, exists := resp.GetHeader(k)
				if !exists {
					t.Errorf("header %q not found", k)
				} else if got != v {
					t.Errorf("header %q = %q, want %q", k, got, v)
				}
			}
			if tt.wantBodyPrefix != "" {
				if !strings.HasPrefix(string(resp.Body), tt.wantBodyPrefix) {
					t.Errorf("Body = %q, want prefix %q", string(resp.Body), tt.wantBodyPrefix)
				}
			}
		})
	}
}

// BenchmarkResponseWrite benchmarks response writing.
func BenchmarkResponseWrite(b *testing.B) {
	resp := icap.NewResponse(200)
	resp.SetHeader("ISTag", "test")
	resp.SetHeader("Service", "test-service")

	var buf bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		resp.WriteTo(&buf)
	}
}

// TestResponseString tests String method.
func TestResponseString(t *testing.T) {
	resp := icap.NewResponse(200)
	resp.SetHeader("ISTag", "test")

	output := resp.String()

	if !strings.Contains(output, "ICAP/1.0 200 OK") {
		t.Errorf("String() missing status line: %q", output)
	}
}
