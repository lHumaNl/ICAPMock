// Copyright 2026 ICAP Mock

package icap

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

// FuzzParseRequest feeds random byte sequences into ParseRequest.
// The test fails if ParseRequest panics; errors are expected and ignored.
func FuzzParseRequest(f *testing.F) {
	// Seed: valid REQMOD request with encapsulated HTTP request headers and body.
	f.Add([]byte(
		"REQMOD icap://icap.example.com/modify ICAP/1.0\r\n" +
			"Host: icap.example.com\r\n" +
			"Encapsulated: req-hdr=0, req-body=49\r\n" +
			"\r\n" +
			"GET /index.html HTTP/1.1\r\n" +
			"Host: www.example.com\r\n" +
			"\r\n" +
			"c\r\n" +
			"Hello World!\r\n" +
			"0\r\n" +
			"\r\n",
	))

	// Seed: valid RESPMOD request with encapsulated HTTP request and response.
	f.Add([]byte(
		"RESPMOD icap://icap.example.com/filter ICAP/1.0\r\n" +
			"Host: icap.example.com\r\n" +
			"Encapsulated: req-hdr=0, res-hdr=49, res-body=126\r\n" +
			"\r\n" +
			"GET /page HTTP/1.1\r\n" +
			"Host: www.example.com\r\n" +
			"\r\n" +
			"HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/html\r\n" +
			"Content-Length: 5\r\n" +
			"\r\n" +
			"5\r\n" +
			"hello\r\n" +
			"0\r\n" +
			"\r\n",
	))

	// Seed: valid OPTIONS request (no body).
	f.Add([]byte(
		"OPTIONS icap://icap.example.com/info ICAP/1.0\r\n" +
			"Host: icap.example.com\r\n" +
			"\r\n",
	))

	// Seed: minimal valid REQMOD with null-body.
	f.Add([]byte(
		"REQMOD icap://icap.example.com/modify ICAP/1.0\r\n" +
			"Host: icap.example.com\r\n" +
			"Encapsulated: null-body=0\r\n" +
			"\r\n",
	))

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ParseRequest panicked: %v", r)
			}
		}()

		req, err := ParseRequest(bufio.NewReader(bytes.NewReader(data)))
		if err != nil {
			return
		}
		// Attempt to drain any body readers to exercise more parsing code paths.
		if req.HTTPRequest != nil && req.HTTPRequest.BodyReader != nil {
			_, _ = io.Copy(io.Discard, req.HTTPRequest.BodyReader)
		}
		if req.HTTPResponse != nil && req.HTTPResponse.BodyReader != nil {
			_, _ = io.Copy(io.Discard, req.HTTPResponse.BodyReader)
		}
	})
}

// FuzzParseChunkSize feeds random byte sequences into NewChunkedReader and reads
// all data from it, exercising the chunk-size parser and state machine.
func FuzzParseChunkSize(f *testing.F) {
	// Seed: single chunk "Hello".
	f.Add([]byte("5\r\nHello\r\n0\r\n\r\n"))

	// Seed: multiple chunks.
	f.Add([]byte("4\r\nWiki\r\n5\r\npedia\r\n0\r\n\r\n"))

	// Seed: chunk with extension.
	f.Add([]byte("a;name=value\r\n0123456789\r\n0\r\n\r\n"))

	// Seed: empty body (terminator only).
	f.Add([]byte("0\r\n\r\n"))

	// Seed: larger chunk.
	f.Add([]byte("1a\r\nabcdefghijklmnopqrstuvwxyz\r\n0\r\n\r\n"))

	// Seed: invalid hex size (error path).
	f.Add([]byte("gg\r\ndata\r\n0\r\n\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ChunkedReader panicked: %v", r)
			}
		}()

		cr := NewChunkedReader(bytes.NewReader(data))
		_, _ = io.Copy(io.Discard, cr)
	})
}

// FuzzParseEncapsulated feeds random strings into ParseEncapsulatedHeader,
// exercising the key=value offset parser.
func FuzzParseEncapsulated(f *testing.F) {
	// Seed: typical REQMOD encapsulated header.
	f.Add([]byte("req-hdr=0, req-body=412"))

	// Seed: RESPMOD with all sections.
	f.Add([]byte("req-hdr=0, res-hdr=200, res-body=350"))

	// Seed: null-body only.
	f.Add([]byte("null-body=0"))

	// Seed: res-body only.
	f.Add([]byte("res-hdr=0, res-body=77"))

	// Seed: whitespace variants.
	f.Add([]byte("req-hdr = 0 , req-body = 100"))

	// Seed: empty string (error path).
	f.Add([]byte(""))

	// Seed: malformed (error path).
	f.Add([]byte("req-hdr=abc"))

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ParseEncapsulatedHeader panicked: %v", r)
			}
		}()

		_, _ = ParseEncapsulatedHeader(string(data))
	})
}
