// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
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
	// ErrLineTooLong indicates a protocol line exceeded the configured limit.
	ErrLineTooLong = errors.New("protocol line too long")
	// ErrHeadersTooLarge indicates the aggregate header section exceeded the limit.
	ErrHeadersTooLarge = errors.New("headers too large")
)

const (
	maxProtocolRequestLineBytes = 8 * 1024
	maxProtocolStatusLineBytes  = 4 * 1024
	maxProtocolHeaderLineBytes  = 8 * 1024
	maxProtocolHeaderBytes      = 64 * 1024

	// maxHeaders is the maximum number of headers allowed in a single ICAP request.
	// This prevents OOM attacks from clients sending excessive headers.
	maxHeaders = 1000
)

// parseRequestLine parses an ICAP request line.
// Format: METHOD URI VERSION
// Example: REQMOD icap://localhost:1344/reqmod ICAP/1.0
//
// Returns the method, URI, version, and any error encountered.
func parseRequestLine(reader BufferedReader) (method, uri, version string, err error) {
	line, err := readProtocolLine(reader, maxProtocolRequestLineBytes)
	if err != nil {
		return "", "", "", fmt.Errorf("reading request line: %w", err)
	}

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

// parseHeaders reads and parses ICAP headers from the reader.
// Headers are read until an empty line is encountered.
// Returns an error if more than maxHeaders headers are received.
//
// Returns the parsed headers and any error encountered.
func parseHeaders(reader BufferedReader) (icap.Header, error) {
	headers := make(icap.Header)
	count := 0
	totalBytes := 0

	for {
		line, err := readProtocolLine(reader, maxProtocolHeaderLineBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading headers: %w", err)
		}

		totalBytes += len(line) + len("\r\n")
		if totalBytes > maxProtocolHeaderBytes {
			return nil, fmt.Errorf("%w: max %d bytes", ErrHeadersTooLarge, maxProtocolHeaderBytes)
		}

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

func readProtocolLine(reader io.Reader, maxBytes int) (string, error) {
	buf := make([]byte, 0, 256)
	var one [1]byte
	for {
		n, err := reader.Read(one[:])
		if n > 0 {
			line, done, lineErr := appendProtocolLineByte(buf, one[0], maxBytes)
			buf = line
			if done || lineErr != nil {
				return string(trimProtocolLineEnding(buf)), lineErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(buf) > 0 {
				return string(trimProtocolLineEnding(buf)), nil
			}
			return "", err
		}
	}
}

func appendProtocolLineByte(buf []byte, b byte, maxBytes int) (line []byte, done bool, err error) {
	if b == '\n' {
		return buf, true, nil
	}
	if len(buf) >= maxBytes {
		return buf, false, fmt.Errorf("%w: max %d bytes", ErrLineTooLong, maxBytes)
	}
	return append(buf, b), false, nil
}

func trimProtocolLineEnding(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		return line[:len(line)-1]
	}
	return line
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
	if previewStr, ok := headers.Get("Preview"); ok {
		req.Preview, err = icap.ParsePreviewHeader(previewStr)
		if err != nil {
			return nil, fmt.Errorf("parsing Preview header: %w", err)
		}
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
	if needsSegmentedRESPMODParsing(req) {
		return parseSegmentedRESPMOD(req)
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
	line, err := readProtocolLine(reader, maxProtocolRequestLineBytes)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP request line: %w", err)
	}

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
	line, err := readProtocolLine(reader, maxProtocolStatusLineBytes)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP response line: %w", err)
	}

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

// extractClientIP extracts the canonical client IP address.
// X-Client-IP is honored only when explicitly enabled and sent by a trusted proxy.
//
// Parameters:
//   - headers: The request headers
//   - remoteAddr: The remote address string (e.g., "192.168.1.1:12345")
//
// Returns the extracted IP address.
func extractClientIP(headers icap.Header, remoteAddr string, trustHeader bool, trustedProxies []string) string {
	peerIP := extractPeerIP(remoteAddr)
	if trustHeader && isTrustedProxy(peerIP, trustedProxies) {
		if clientIP, ok := validClientIPHeader(headers); ok {
			return clientIP
		}
	}
	return peerIP
}

func extractPeerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func validClientIPHeader(headers icap.Header) (string, bool) {
	clientIP, ok := headers.Get("X-Client-IP")
	if !ok {
		return "", false
	}
	clientIP = strings.TrimSpace(strings.Split(clientIP, ",")[0])
	return clientIP, net.ParseIP(clientIP) != nil
}

func isTrustedProxy(peerIP string, trustedProxies []string) bool {
	if len(trustedProxies) == 0 {
		return true
	}
	ip := net.ParseIP(peerIP)
	return ip != nil && proxyListContainsIP(ip, trustedProxies)
}

func proxyListContainsIP(ip net.IP, trustedProxies []string) bool {
	for _, proxy := range trustedProxies {
		if proxyMatchesIP(ip, strings.TrimSpace(proxy)) {
			return true
		}
	}
	return false
}

func proxyMatchesIP(ip net.IP, proxy string) bool {
	if proxyIP := net.ParseIP(proxy); proxyIP != nil {
		return proxyIP.Equal(ip)
	}
	_, network, err := net.ParseCIDR(proxy)
	return err == nil && network.Contains(ip)
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
