// Copyright 2026 ICAP Mock

package icap_test

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// typicalREQMOD is a realistic REQMOD request with HTTP headers and a small body.
const typicalREQMOD = "REQMOD icap://icap-server.example.net:1344/reqmod ICAP/1.0\r\n" +
	"Host: icap-server.example.net\r\n" +
	"Encapsulated: req-hdr=0, req-body=137\r\n" +
	"Preview: 0\r\n" +
	"\r\n" +
	"POST /api/v1/upload HTTP/1.1\r\n" +
	"Host: origin-server.example.net\r\n" +
	"Content-Type: application/octet-stream\r\n" +
	"Content-Length: 11\r\n" +
	"\r\n" +
	"b\r\nhello world\r\n0\r\n\r\n"

// BenchmarkParseRequest_Typical benchmarks parsing a typical REQMOD request with
// HTTP headers and a small chunked body.
func BenchmarkParseRequest_Typical(b *testing.B) {
	data := []byte(typicalREQMOD)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(bytes.NewReader(data))
		req, err := icap.ParseRequest(r)
		if err != nil {
			b.Fatalf("ParseRequest() unexpected error: %v", err)
		}
		// Consume the body so BodyReader is fully exercised.
		if req.HTTPRequest != nil && req.HTTPRequest.BodyReader != nil {
			buf := make([]byte, 64)
			for {
				_, rerr := req.HTTPRequest.BodyReader.Read(buf)
				if rerr != nil {
					break
				}
			}
		}
	}
}

// BenchmarkParseRequest_Large benchmarks parsing a REQMOD request whose HTTP body
// is 64 KB of data encoded in chunked transfer encoding.
func BenchmarkParseRequest_Large(b *testing.B) {
	const bodySize = 64 * 1024
	body := strings.Repeat("A", bodySize)

	// Build the chunked-encoded body: "<hex-len>\r\n<data>\r\n0\r\n\r\n"
	chunk := fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", bodySize, body)

	// HTTP headers for the embedded request.
	httpHeaders := fmt.Sprintf(
		"POST /api/v1/upload HTTP/1.1\r\nHost: origin-server.example.net\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\n\r\n",
		bodySize,
	)

	icapHeaders := fmt.Sprintf(
		"REQMOD icap://icap-server.example.net:1344/reqmod ICAP/1.0\r\nHost: icap-server.example.net\r\nEncapsulated: req-hdr=0, req-body=%d\r\n\r\n",
		len(httpHeaders),
	)

	data := []byte(icapHeaders + httpHeaders + chunk)

	b.SetBytes(int64(bodySize))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(bytes.NewReader(data))
		req, err := icap.ParseRequest(r)
		if err != nil {
			b.Fatalf("ParseRequest() unexpected error: %v", err)
		}
		// Drain the streaming body so we exercise the chunked reader path.
		if req.HTTPRequest != nil && req.HTTPRequest.BodyReader != nil {
			buf := make([]byte, 4096)
			for {
				_, rerr := req.HTTPRequest.BodyReader.Read(buf)
				if rerr != nil {
					break
				}
			}
		}
	}
}

// BenchmarkNewResponse benchmarks creating a new ICAP response, setting common
// headers, and serializing it to a bytes.Buffer.
func BenchmarkNewResponse(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp := icap.NewResponse(icap.StatusOK)
		resp.SetHeader("ISTag", "benchmark-server-v1")
		resp.SetHeader("Encapsulated", "req-hdr=0, null-body=72")
		resp.SetHeader("X-Infection-Found", "Type=0; Resolution=2; Threat=;")

		resp.HTTPRequest = &icap.HTTPMessage{
			Method: "POST",
			URI:    "/api/v1/upload",
			Proto:  "HTTP/1.1",
		}
		resp.HTTPRequest.Header = make(icap.Header)
		resp.HTTPRequest.Header.Set("Host", "origin-server.example.net")
		resp.HTTPRequest.Header.Set("Content-Type", "application/octet-stream")

		var buf bytes.Buffer
		if _, err := resp.WriteTo(&buf); err != nil {
			b.Fatalf("WriteTo() unexpected error: %v", err)
		}
	}
}
