// Copyright 2026 ICAP Mock

package icap

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

// ICAP status codes and their text descriptions as defined in RFC 3507.
const (
	StatusOK                  = 200 // Successful modification
	StatusNoContentNeeded     = 204 // Original message not modified
	StatusBadRequest          = 400 // Malformed request
	StatusNotFound            = 404 // ICAP service not found
	StatusMethodNotAllowed    = 405 // Method not allowed
	StatusInternalServerError = 500 // Server error
	StatusNotImplemented      = 501 // Method not implemented
	StatusBadGateway          = 502 // Bad gateway
	StatusServiceUnavailable  = 503 // Service overloaded
	StatusVersionNotSupported = 505 // ICAP version not supported
)

// StatusText returns the text for a status code.
func StatusText(code int) string {
	switch code {
	case StatusOK:
		return "OK"
	case StatusNoContentNeeded:
		return "No Content Needed"
	case StatusBadRequest:
		return "Bad Request"
	case StatusNotFound:
		return "ICAP Service not found"
	case StatusMethodNotAllowed:
		return "Method not allowed"
	case StatusInternalServerError:
		return "Server error"
	case StatusNotImplemented:
		return "Not implemented"
	case StatusBadGateway:
		return "Bad Gateway"
	case StatusServiceUnavailable:
		return "Service overloaded"
	case StatusVersionNotSupported:
		return "ICAP version not supported"
	default:
		return "Unknown"
	}
}

// Response represents an ICAP response.
type Response struct {
	BodyReader   io.Reader
	Header       Header
	HTTPRequest  *HTTPMessage
	HTTPResponse *HTTPMessage
	Proto        string
	Body         []byte
	StatusCode   int
}

// NewResponse creates a new ICAP response with the given status code.
func NewResponse(statusCode int) *Response {
	return &Response{
		StatusCode: statusCode,
		Proto:      Version,
		Header:     make(Header),
	}
}

// NewResponseError creates a new error response with a message.
func NewResponseError(statusCode int, message string) *Response {
	resp := NewResponse(statusCode)
	resp.SetHeader("Connection", "close")
	if message != "" {
		resp.Body = []byte(message + "\r\n")
	}
	return resp
}

// NewOptionsResponse creates a new OPTIONS response with server capabilities.
func NewOptionsResponse(istag string, methods []string, maxConnections, optionsTTL int) *Response {
	resp := NewResponse(StatusOK)

	resp.SetHeader("Methods", strings.Join(methods, ", "))
	resp.SetHeader("Service", "ICAP-Mock-Server/1.0")
	resp.SetHeader("ISTag", istag)
	resp.SetHeader("Max-Connections", strconv.Itoa(maxConnections))
	resp.SetHeader("Options-TTL", strconv.Itoa(optionsTTL))
	resp.SetHeader("Allow", "204")

	return resp
}

// GetHeader returns the value of a header (case-insensitive).
func (r *Response) GetHeader(key string) (string, bool) {
	if r.Header == nil {
		return "", false
	}
	return r.Header.Get(key)
}

// SetHeader sets a header value.
func (r *Response) SetHeader(key, value string) {
	if r.Header == nil {
		r.Header = make(Header)
	}
	r.Header.Set(key, value)
}

// SetBody sets the response body.
func (r *Response) SetBody(body []byte) {
	r.Body = body
}

// SetHTTPRequest sets the encapsulated HTTP request.
func (r *Response) SetHTTPRequest(req *HTTPMessage) {
	r.HTTPRequest = req
}

// SetHTTPResponse sets the encapsulated HTTP response.
func (r *Response) SetHTTPResponse(resp *HTTPMessage) {
	r.HTTPResponse = resp
}

// IsError returns true if the status code indicates an error (4xx or 5xx).
func (r *Response) IsError() bool {
	return r.StatusCode >= 400
}

// Clone creates a deep copy of the response.
func (r *Response) Clone() *Response {
	clone := NewResponse(r.StatusCode)
	clone.Proto = r.Proto

	if r.Header != nil {
		clone.Header = r.Header.Clone()
	}

	if len(r.Body) > 0 {
		clone.Body = make([]byte, len(r.Body))
		copy(clone.Body, r.Body)
	}

	if r.HTTPRequest != nil {
		clone.HTTPRequest = cloneHTTPMessage(r.HTTPRequest)
	}

	if r.HTTPResponse != nil {
		clone.HTTPResponse = cloneHTTPMessage(r.HTTPResponse)
	}

	return clone
}

// cloneHTTPMessage creates a deep copy of an HTTPMessage.
func cloneHTTPMessage(m *HTTPMessage) *HTTPMessage {
	clone := &HTTPMessage{
		Method:     m.Method,
		URI:        m.URI,
		Status:     m.Status,
		StatusText: m.StatusText,
		Proto:      m.Proto,
	}

	if m.Header != nil {
		clone.Header = m.Header.Clone()
	}

	if len(m.Body) > 0 {
		clone.Body = make([]byte, len(m.Body))
		copy(clone.Body, m.Body)
	}

	return clone
}

// WriteTo writes the response to an io.Writer.
func (r *Response) WriteTo(w io.Writer) (int64, error) {
	buf := pool.ResponseBufferPool.Get()
	defer pool.ResponseBufferPool.Put(buf)

	r.writeToBuffer(buf)
	return buf.WriteTo(w)
}

// writeToBuffer writes the response content to a bytes.Buffer.
// This is used by both WriteTo and String to avoid code duplication.
func (r *Response) writeToBuffer(buf *bytes.Buffer) {
	// Write status line
	proto := r.Proto
	if proto == "" {
		proto = Version
	}
	buf.WriteString(proto)
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(r.StatusCode))
	buf.WriteByte(' ')
	buf.WriteString(StatusText(r.StatusCode))
	buf.WriteString("\r\n")

	// Write ICAP headers
	if r.Header != nil {
		r.Header.WriteToBuffer(buf)
	}

	// Write encapsulated HTTP message if present
	if r.HTTPRequest != nil || r.HTTPResponse != nil {
		// Build and write Encapsulated header
		encap := r.BuildEncapsulatedHeader()
		if encap != "" {
			buf.WriteString("Encapsulated: ")
			buf.WriteString(encap)
			buf.WriteString("\r\n")
		}
	}

	// Write blank line
	buf.WriteString("\r\n")

	// Write encapsulated content
	if r.HTTPRequest != nil {
		r.writeHTTPMessage(buf, r.HTTPRequest, true)
	}
	if r.HTTPResponse != nil {
		r.writeHTTPMessage(buf, r.HTTPResponse, false)
	}

	// Write body
	if len(r.Body) > 0 {
		buf.Write(r.Body)
	}
}

// writeHTTPMessage writes an HTTP message to the buffer.
func (r *Response) writeHTTPMessage(buf *bytes.Buffer, m *HTTPMessage, isRequest bool) {
	if isRequest {
		// Write request line
		buf.WriteString(m.Method)
		buf.WriteByte(' ')
		buf.WriteString(m.URI)
		buf.WriteByte(' ')
		buf.WriteString(m.Proto)
		buf.WriteString("\r\n")
	} else {
		// Write status line
		buf.WriteString(m.Proto)
		buf.WriteByte(' ')
		buf.WriteString(m.Status)
		buf.WriteByte(' ')
		buf.WriteString(m.StatusText)
		buf.WriteString("\r\n")
	}

	// Write headers
	if m.Header != nil {
		m.Header.WriteToBuffer(buf)
	}
	buf.WriteString("\r\n")

	// Write body
	if len(m.Body) > 0 {
		buf.Write(m.Body)
	}
}

// BuildEncapsulatedHeader builds the Encapsulated header value based on content.
func (r *Response) BuildEncapsulatedHeader() string {
	var parts []string
	offset := 0

	if r.HTTPRequest != nil {
		parts = append(parts, fmt.Sprintf("req-hdr=%d", offset))
		// Calculate offset for body
		offset += r.calculateHTTPMessageSize(r.HTTPRequest, true)
		if len(r.HTTPRequest.Body) > 0 {
			parts = append(parts, fmt.Sprintf("req-body=%d", offset))
		}
	}

	if r.HTTPResponse != nil {
		parts = append(parts, fmt.Sprintf("res-hdr=%d", offset))
		offset += r.calculateHTTPMessageSize(r.HTTPResponse, false)
		if len(r.HTTPResponse.Body) > 0 {
			parts = append(parts, fmt.Sprintf("res-body=%d", offset))
		}
	}

	// No body case
	if len(parts) == 0 && len(r.Body) == 0 {
		return "null-body=0"
	}

	return strings.Join(parts, ", ")
}

// calculateHTTPMessageSize calculates the approximate size of an HTTP message.
func (r *Response) calculateHTTPMessageSize(m *HTTPMessage, isRequest bool) int {
	var size int

	if isRequest {
		// Request line: METHOD URI VERSION\r\n
		size += len(m.Method) + 1 + len(m.URI) + 1 + len(m.Proto) + 2
	} else {
		// Status line: VERSION STATUS TEXT\r\n
		size += len(m.Proto) + 1 + len(m.Status) + 1 + len(m.StatusText) + 2
	}

	// Headers
	if m.Header != nil {
		m.Header.Walk(func(key, value string) bool {
			size += len(key) + 2 + len(value) + 2
			return true
		})
	}

	// Blank line
	size += 2

	return size
}

// WriteChunkedBody writes the body using chunked transfer encoding.
func (r *Response) WriteChunkedBody(w io.Writer) (int64, error) {
	if len(r.Body) == 0 {
		// Write terminating chunk
		n, err := w.Write([]byte("0\r\n\r\n"))
		return int64(n), err
	}

	cw := NewChunkedWriter(w)
	n, err := cw.Write(r.Body)
	if err != nil {
		return int64(n), err
	}
	err = cw.Close()
	return int64(n), err
}

// String returns a string representation of the response.
func (r *Response) String() string {
	buf := pool.ResponseBufferPool.Get()
	defer pool.ResponseBufferPool.Put(buf)

	r.writeToBuffer(buf)
	return buf.String()
}

// MaxResponseBodySize is the maximum allowed size for reading response bodies (100 MB).
const MaxResponseBodySize = 100 * 1024 * 1024

// ReadResponse reads and parses an ICAP response from an io.Reader.
func ReadResponse(r io.Reader) (*Response, error) { //nolint:gocyclo // ICAP response parsing is inherently sequential
	data, err := io.ReadAll(io.LimitReader(r, MaxResponseBodySize))
	if err != nil {
		return nil, err
	}

	// Parse status line
	lines := bytes.SplitN(data, []byte("\r\n"), 2)
	if len(lines) < 1 {
		return nil, fmt.Errorf("invalid response: empty")
	}

	statusLine := string(lines[0])
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid status line: %s", statusLine)
	}

	// Parse version and status code
	proto := parts[0]
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}

	resp := NewResponse(statusCode)
	resp.Proto = proto

	// Parse headers and body from remainder
	if len(lines) < 2 || len(lines[1]) == 0 {
		return resp, nil
	}

	// Use textproto to parse ICAP headers
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(lines[1])))
	headerMap, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading headers: %w", err)
	}

	for k, v := range headerMap {
		resp.Header[CanonicalHeaderKey(k)] = v
	}

	// Parse Encapsulated header if present
	if encapStr, exists := resp.Header.Get("Encapsulated"); exists {
		encap, err := ParseEncapsulatedHeader(encapStr)
		if err != nil {
			return nil, fmt.Errorf("parsing Encapsulated header: %w", err)
		}

		// Read the body: everything after headers (after the blank line)
		// Find the blank line that separates headers from body
		headerEnd := bytes.Index(lines[1], []byte("\r\n\r\n"))
		if headerEnd >= 0 {
			bodyData := lines[1][headerEnd+4:]
			if len(bodyData) > 0 && !encap.IsEmpty() && encap.NullBody == encapNotSet {
				resp.Body = bodyData
			}
		}
	}

	return resp, nil
}
