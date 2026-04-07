// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// BufferedReader defines the interface for buffered reading operations.
// Both *bufio.Reader and *pooledBuffer satisfy this interface.
type BufferedReader interface {
	io.Reader
	ReadString(delim byte) (string, error)
}

// BufferedWriter defines the interface for buffered writing operations.
// Both *bufio.Writer and *bufferedWriter satisfy this interface.
type BufferedWriter interface {
	io.Writer
	io.StringWriter
	Flush() error
}

// Protocol errors.
var (
	// ErrInvalidRequestLine indicates the request line is malformed.
	ErrInvalidRequestLine = errors.New("invalid request line")
	// ErrInvalidMethod indicates the ICAP method is not supported.
	ErrInvalidMethod = errors.New("invalid ICAP method")
	// ErrInvalidVersion indicates the ICAP version is not supported.
	ErrInvalidVersion = errors.New("invalid ICAP version")
	// ErrInvalidURIScheme indicates the URI scheme is not icap://.
	ErrInvalidURIScheme = errors.New("invalid URI scheme")
	// ErrMalformedHeaders indicates the headers are malformed.
	ErrMalformedHeaders = errors.New("malformed headers")
	// ErrBodyTooLarge indicates the body exceeds the size limit.
	ErrBodyTooLarge = errors.New("body too large")
)

// parseRequestLine parses an ICAP request line.
// Format: METHOD URI VERSION
// Example: REQMOD icap://localhost:1344/reqmod ICAP/1.0
//
// Returns the method, URI, version, and any error encountered.
func parseRequestLine(reader BufferedReader) (method, uri, version string, err error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", "", "", fmt.Errorf("reading request line: %w", err)
	}

	// Remove trailing \r\n or \n
	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")

	// Split into parts
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return "", "", "", ErrInvalidRequestLine
	}

	method = parts[0]
	uri = parts[1]
	version = parts[2]

	// Validate method
	if !isValidICAPMethod(method) {
		return "", "", "", ErrInvalidMethod
	}

	// Validate URI scheme
	if len(uri) < 7 || !strings.EqualFold(uri[:7], "icap://") {
		return "", "", "", ErrInvalidURIScheme
	}

	// Validate version
	if !isValidICAPVersion(version) {
		return "", "", "", ErrInvalidVersion
	}

	return method, uri, version, nil
}

// maxHeaders is the maximum number of headers allowed in a single ICAP request.
// This prevents OOM attacks from clients sending excessive headers.
const maxHeaders = 1000

// parseHeaders reads and parses ICAP headers from the reader.
// Headers are read until an empty line is encountered.
// Returns an error if more than maxHeaders headers are received.
//
// Returns the parsed headers and any error encountered.
func parseHeaders(reader BufferedReader) (icap.Header, error) {
	headers := make(icap.Header)
	count := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading headers: %w", err)
		}

		// Remove trailing \r\n or \n
		line = strings.TrimSuffix(line, "\r\n")
		line = strings.TrimSuffix(line, "\n")

		// Empty line signals end of headers
		if line == "" {
			break
		}

		// Parse header line: Key: Value
		key, value, err := parseHeaderLine(line)
		if err != nil {
			return nil, err
		}

		headers.Add(key, value)
		count++
		if count > maxHeaders {
			return nil, fmt.Errorf("too many headers (max %d)", maxHeaders)
		}
	}

	return headers, nil
}

// parseHeaderLine parses a single header line.
// Format: Key: Value.
func parseHeaderLine(line string) (key, value string, err error) {
	idx := strings.Index(line, ":")
	if idx == -1 {
		return "", "", ErrMalformedHeaders
	}

	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])

	if key == "" {
		return "", "", ErrMalformedHeaders
	}

	return key, value, nil
}

// parseICAPRequest reads and parses a complete ICAP request.
// This includes the request line, headers, and any encapsulated body.
//
// The function supports streaming for large bodies:
//   - req.BodyReader provides direct access to the raw body stream
//   - req.HTTPRequest.BodyReader provides access to the HTTP body stream (chunked)
//   - Use GetBody() methods to load body into memory only when needed
//
// Returns the parsed ICAP Request and any error encountered.
func parseICAPRequest(reader BufferedReader) (*icap.Request, error) {
	// Parse request line
	method, uri, proto, err := parseRequestLine(reader)
	if err != nil {
		return nil, err
	}

	// Parse headers
	headers, err := parseHeaders(reader)
	if err != nil {
		return nil, err
	}

	req := &icap.Request{
		Method: method,
		URI:    uri,
		Proto:  proto,
		Header: headers,
	}

	// Parse Encapsulated header if present
	if encapStr, ok := headers.Get("Encapsulated"); ok {
		req.Encapsulated, err = icap.ParseEncapsulatedHeader(encapStr)
		if err != nil {
			return nil, fmt.Errorf("parsing Encapsulated header: %w", err)
		}

		// IMPORTANT: Do NOT wrap in ChunkedReader here!
		// The encapsulated content contains HTTP headers (plain text) followed by
		// HTTP body (chunked). We need to parse HTTP headers first, then wrap
		// the remaining stream in ChunkedReader for the HTTP body.
		// This provides TRUE O(1) memory usage.
		req.BodyReader = reader

		// Parse embedded HTTP message using streaming approach
		if err := parseEmbeddedHTTP(req); err != nil {
			return nil, err
		}
	}

	return req, nil
}

// parseEmbeddedHTTP parses the embedded HTTP message in an ICAP request.
// This function implements TRUE O(1) STREAMING by:
//  1. Parsing HTTP headers directly from the stream (small, constant size)
//  2. Passing the remaining stream as a ChunkedReader for the HTTP body
//  3. Never buffering the entire body in memory
//
// Memory usage is constant regardless of body size:
//   - Before: O(body_size) per request (buffered entire body)
//   - After: O(1) per request (streaming, no buffering)
func parseEmbeddedHTTP(req *icap.Request) error {
	if req.Encapsulated.IsEmpty() {
		return nil
	}

	// Parse embedded HTTP message using streaming approach
	// The BodyReader is the raw stream after ICAP headers, NOT a ChunkedReader
	if req.BodyReader == nil {
		return nil
	}

	// For REQMOD: parse HTTP request
	// For RESPMOD: parse HTTP request first (it's at the start), then HTTP response
	if req.Method == icap.MethodREQMOD && req.Encapsulated.ReqHdr >= 0 {
		if err := parseEmbeddedHTTPRequestStreaming(req); err != nil {
			return err
		}
	} else if req.Method == icap.MethodRESPMOD && req.Encapsulated.ReqHdr >= 0 {
		// For RESPMOD, parse HTTP request first to skip to HTTP response
		if err := parseEmbeddedHTTPRequestStreaming(req); err != nil {
			return err
		}
		// Then parse HTTP response
		if req.Encapsulated.ResHdr > 0 {
			if err := parseEmbeddedHTTPResponseStreaming(req); err != nil {
				return err
			}
		}
	}

	return nil
}

// parseEmbeddedHTTPRequestStreaming parses the embedded HTTP request directly from the stream.
// This provides TRUE O(1) MEMORY USAGE by:
//  1. Parsing HTTP headers from the stream (small, constant size)
//  2. Passing the remaining stream as a ChunkedReader for the HTTP body
//  3. Never buffering the entire body in memory
func parseEmbeddedHTTPRequestStreaming(req *icap.Request) error {
	// The BodyReader is the raw stream after ICAP headers
	// We need to read it as a BufferedReader for header parsing
	reader, ok := req.BodyReader.(BufferedReader)
	if !ok {
		// Wrap in bufio if not already a BufferedReader
		reader = bufio.NewReader(req.BodyReader)
		// Update BodyReader to use the same buffered reader
		// This ensures subsequent parsing uses the same buffer
		req.BodyReader = reader
	}

	// Parse HTTP request line
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP request line: %w", err)
	}

	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return errors.New("invalid embedded HTTP request line")
	}

	req.HTTPRequest = &icap.HTTPMessage{
		Method: parts[0],
		URI:    parts[1],
		Proto:  parts[2],
		Header: make(icap.Header),
	}

	// Parse HTTP headers
	httpHeaders, err := parseHeaders(reader)
	if err != nil {
		return fmt.Errorf("parsing HTTP headers: %w", err)
	}
	req.HTTPRequest.Header = httpHeaders

	// Set up body reader if there's a body - the remaining stream IS the chunked body
	// This is the KEY for O(1) memory: we don't buffer the body
	if req.Encapsulated.HasReqBody() {
		// The remaining data in the reader IS the chunked HTTP body
		// Wrap it in a ChunkedReader for proper ICAP chunked encoding handling
		req.HTTPRequest.BodyReader = icap.NewChunkedReader(reader)
	}

	return nil
}

// parseEmbeddedHTTPRequest parses the embedded HTTP request from buffered body.
// Deprecated: Use parseEmbeddedHTTPRequestStreaming for O(1) memory usage.
// This function is kept for backwards compatibility but loads the entire body.
func parseEmbeddedHTTPRequest(req *icap.Request) error {
	if len(req.Body) == 0 {
		return nil
	}

	reader := bufio.NewReader(strings.NewReader(string(req.Body)))

	// Parse HTTP request line
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return errors.New("invalid embedded HTTP request line")
	}

	req.HTTPRequest = &icap.HTTPMessage{
		Method: parts[0],
		URI:    parts[1],
		Proto:  parts[2],
		Header: make(icap.Header),
	}

	// Parse HTTP headers
	httpHeaders, err := parseHeaders(reader)
	if err != nil {
		return err
	}
	req.HTTPRequest.Header = httpHeaders

	// Set up body reader if there's a body - uses lazy loading
	if req.Encapsulated.HasReqBody() {
		bodyStart := req.Encapsulated.ReqBody
		if bodyStart > 0 && bodyStart < len(req.Body) {
			// Set up streaming reader - body is NOT loaded into Body field
			req.HTTPRequest.BodyReader = icap.NewChunkedReader(
				strings.NewReader(string(req.Body[bodyStart:])),
			)
		}
	}

	return nil
}

// parseEmbeddedHTTPResponseStreaming parses the embedded HTTP response directly from the stream.
// This provides TRUE O(1) MEMORY USAGE by:
//  1. Parsing HTTP headers from the stream (small, constant size)
//  2. Passing the remaining stream as a ChunkedReader for the HTTP body
//  3. Never buffering the entire body in memory
func parseEmbeddedHTTPResponseStreaming(req *icap.Request) error {
	// Get the reader - this should be the stream after parsing HTTP request
	// (for RESPMOD, we've already parsed the HTTP request headers)
	reader, ok := req.BodyReader.(BufferedReader)
	if !ok {
		// Wrap in bufio if not already a BufferedReader
		reader = bufio.NewReader(req.BodyReader)
	}

	// Parse HTTP status line
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP response line: %w", err)
	}

	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return errors.New("invalid embedded HTTP response line")
	}

	req.HTTPResponse = &icap.HTTPMessage{
		Proto:      parts[0],
		Status:     parts[1],
		StatusText: parts[2],
		Header:     make(icap.Header),
	}

	// Parse HTTP headers
	httpHeaders, err := parseHeaders(reader)
	if err != nil {
		return fmt.Errorf("parsing HTTP headers: %w", err)
	}
	req.HTTPResponse.Header = httpHeaders

	// Set up body reader if there's a body - the remaining stream IS the chunked body
	// This is the KEY for O(1) memory: we don't buffer the body
	if req.Encapsulated.HasResBody() {
		// The remaining data in the reader IS the chunked HTTP body
		req.HTTPResponse.BodyReader = icap.NewChunkedReader(reader)
	}

	return nil
}

// parseEmbeddedHTTPResponse parses the embedded HTTP response from buffered body.
// Deprecated: Use parseEmbeddedHTTPResponseStreaming for O(1) memory usage.
// This function is kept for backwards compatibility but loads the entire body.
func parseEmbeddedHTTPResponse(req *icap.Request) error {
	if len(req.Body) == 0 {
		return nil
	}

	offset := req.Encapsulated.ResHdr
	if offset < 0 || offset >= len(req.Body) {
		return nil
	}

	reader := bufio.NewReader(strings.NewReader(string(req.Body[offset:])))

	// Parse HTTP status line
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return errors.New("invalid embedded HTTP response line")
	}

	req.HTTPResponse = &icap.HTTPMessage{
		Proto:      parts[0],
		Status:     parts[1],
		StatusText: parts[2],
		Header:     make(icap.Header),
	}

	// Parse HTTP headers
	httpHeaders, err := parseHeaders(reader)
	if err != nil {
		return err
	}
	req.HTTPResponse.Header = httpHeaders

	// Set up body reader if there's a body - uses lazy loading
	if req.Encapsulated.HasResBody() {
		bodyStart := req.Encapsulated.ResBody
		if bodyStart > 0 && bodyStart < len(req.Body) {
			// Set up streaming reader - body is NOT loaded into Body field
			req.HTTPResponse.BodyReader = icap.NewChunkedReader(
				strings.NewReader(string(req.Body[bodyStart:])),
			)
		}
	}

	return nil
}

// maxChunkedBodySize is the default maximum size for reading chunked bodies into memory.
// Bodies larger than this will be rejected to prevent OOM.
const maxChunkedBodySize = 100 * 1024 * 1024 // 100MB

// readChunkedBody reads a chunked body from the reader.
//
// Returns the body bytes and any error encountered.
// Limits the total body size to maxChunkedBodySize to prevent OOM on malicious input.
func readChunkedBody(reader BufferedReader) ([]byte, error) {
	cr := icap.NewChunkedReader(reader)
	lr := io.LimitReader(cr, maxChunkedBodySize+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxChunkedBodySize {
		return nil, fmt.Errorf("chunked body exceeds maximum size of %d bytes", maxChunkedBodySize)
	}
	return data, nil
}

// writeResponse writes an ICAP response to the writer.
//
// Parameters:
//   - writer: The buffered writer to write to
//   - statusCode: The ICAP status code (e.g., 200, 404, 500)
//   - headers: Optional headers to include in the response
//   - body: Optional body content
//
// Returns any error encountered during writing.
func writeResponse(writer BufferedWriter, statusCode int, headers map[string]string, body []byte) error {
	// Write status line
	statusText := icap.StatusText(statusCode)
	if _, err := writer.WriteString("ICAP/1.0 "); err != nil {
		return fmt.Errorf("writing status line: %w", err)
	}
	if _, err := writer.WriteString(strconv.Itoa(statusCode)); err != nil {
		return fmt.Errorf("writing status line: %w", err)
	}
	if _, err := writer.WriteString(" "); err != nil {
		return fmt.Errorf("writing status line: %w", err)
	}
	if _, err := writer.WriteString(statusText); err != nil {
		return fmt.Errorf("writing status line: %w", err)
	}
	if _, err := writer.WriteString("\r\n"); err != nil {
		return fmt.Errorf("writing status line: %w", err)
	}

	// Write headers
	for key, value := range headers {
		if _, err := writer.WriteString(key); err != nil {
			return fmt.Errorf("writing header %s: %w", key, err)
		}
		if _, err := writer.WriteString(": "); err != nil {
			return fmt.Errorf("writing header %s: %w", key, err)
		}
		if _, err := writer.WriteString(value); err != nil {
			return fmt.Errorf("writing header %s: %w", key, err)
		}
		if _, err := writer.WriteString("\r\n"); err != nil {
			return fmt.Errorf("writing header %s: %w", key, err)
		}
	}

	// Write blank line
	if _, err := writer.WriteString("\r\n"); err != nil {
		return fmt.Errorf("writing header terminator: %w", err)
	}

	// Write body if present
	if len(body) > 0 {
		if _, err := writer.Write(body); err != nil {
			return fmt.Errorf("writing body: %w", err)
		}
	}

	return writer.Flush()
}

// writeResponseFromICAP writes an icap.Response to the writer.
// This handles the full ICAP response including encapsulated HTTP messages.
//
// Returns any error encountered during writing.
func writeResponseFromICAP(writer BufferedWriter, resp *icap.Response) error {
	_, err := resp.WriteTo(writer)
	if err != nil {
		return fmt.Errorf("writing ICAP response: %w", err)
	}
	return writer.Flush()
}

// extractClientIP extracts the client IP address from headers or remote address.
// The X-Client-IP header takes precedence over the remote address.
//
// Parameters:
//   - headers: The request headers
//   - remoteAddr: The remote address string (e.g., "192.168.1.1:12345")
//
// Returns the extracted IP address.
func extractClientIP(headers icap.Header, remoteAddr string) string {
	// Check X-Client-IP header first (used by proxies)
	if clientIP, ok := headers.Get("X-Client-IP"); ok && clientIP != "" {
		return clientIP
	}

	// Fall back to remote address
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

// buildEncapsulatedHeader builds the Encapsulated header value.
// This describes the structure of the encapsulated HTTP message.
//
// httpReq and httpRes are the encapsulated HTTP messages (nil if not present).
// hasReqBody and hasResBody indicate whether the respective body is present.
// Returns the Encapsulated header value string with correct byte offsets.
func buildEncapsulatedHeader(httpReq, httpRes *icap.HTTPMessage, hasReqBody, hasResBody bool) string {
	var parts []string
	offset := 0

	if httpReq != nil {
		parts = append(parts, fmt.Sprintf("req-hdr=%d", offset))
		offset += httpMessageHeaderSize(httpReq, true)
		if hasReqBody {
			parts = append(parts, fmt.Sprintf("req-body=%d", offset))
		}
	}

	if httpRes != nil {
		parts = append(parts, fmt.Sprintf("res-hdr=%d", offset))
		offset += httpMessageHeaderSize(httpRes, false)
		if hasResBody {
			parts = append(parts, fmt.Sprintf("res-body=%d", offset))
		}
	}

	// If we have headers but no body was flagged, add null-body at the current offset
	if len(parts) > 0 && !hasReqBody && !hasResBody {
		parts = append(parts, fmt.Sprintf("null-body=%d", offset))
	}

	// No content case
	if len(parts) == 0 {
		return "null-body=0"
	}

	return strings.Join(parts, ", ")
}

// httpMessageHeaderSize computes the byte size of the serialized HTTP header
// section for an encapsulated HTTP message (request line or status line + headers + trailing CRLF).
func httpMessageHeaderSize(msg *icap.HTTPMessage, isRequest bool) int {
	var size int
	if isRequest {
		// Request line: "METHOD URI HTTP/1.1\r\n"
		size = len(msg.Method) + 1 + len(msg.URI) + 1 + len(msg.Proto) + 2
	} else {
		// Status line: "HTTP/1.1 STATUS STATUS_TEXT\r\n"
		size = len(msg.Proto) + 1 + len(msg.Status) + 1 + len(msg.StatusText) + 2
	}
	// Headers: "Key: Value\r\n" for each
	for k, values := range msg.Header {
		for _, v := range values {
			size += len(k) + 2 + len(v) + 2 // "Key: Value\r\n"
		}
	}
	// Trailing CRLF after headers
	size += 2
	return size
}

// isValidICAPMethod checks if the method is a valid ICAP method.
func isValidICAPMethod(method string) bool {
	switch method {
	case icap.MethodREQMOD, icap.MethodRESPMOD, icap.MethodOPTIONS:
		return true
	default:
		return false
	}
}

// isValidICAPVersion checks if the version is supported.
func isValidICAPVersion(version string) bool {
	return version == "ICAP/1.0"
}

// parseChunkSize parses a hexadecimal chunk size.
func parseChunkSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty chunk size")
	}

	// Handle extensions (e.g., "1a;name=value")
	if idx := strings.IndexByte(s, ';'); idx >= 0 {
		s = s[:idx]
	}

	size, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chunk size: %s", s)
	}

	return size, nil
}
