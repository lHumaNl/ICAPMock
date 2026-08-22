// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
	BodyReader      io.Reader
	Header          Header
	HTTPRequest     *HTTPMessage
	HTTPResponse    *HTTPMessage
	Proto           string
	Body            []byte
	StatusCode      int
	closeAfterWrite bool
}

// ResponseWriteOptions configures delivery lifecycle observation.
type ResponseWriteOptions struct {
	// OnStreamingStart runs once after the stream source opens successfully and
	// immediately before body pacing or delivery begins.
	OnStreamingStart func()
}

// NewResponse creates a new ICAP response with the given status code.
func NewResponse(statusCode int) *Response {
	return &Response{
		StatusCode: statusCode,
		Proto:      Version,
		Header:     make(Header),
	}
}

// NewResponseError creates an ICAP error response with the diagnostic message
// in a framed, encapsulated HTTP response. The ICAP connection remains
// reusable; callers that require connection termination must request it
// explicitly.
func NewResponseError(statusCode int, message string) *Response {
	resp := NewResponse(statusCode)
	body := []byte(message)
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = StatusText(statusCode)
	}
	httpResp := &HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     strconv.Itoa(statusCode),
		StatusText: statusText,
		Header:     NewHeader(),
		Body:       body,
	}
	httpResp.Header.Set("Content-Type", "text/plain; charset=utf-8")
	httpResp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.SetHTTPResponse(httpResp)
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

// MarkCloseAfterWrite asks the server to close the connection after this
// response is written without serializing a Connection header.
func (r *Response) MarkCloseAfterWrite() {
	r.closeAfterWrite = true
}

// CloseAfterWrite reports whether the server should close the connection after
// writing this response, independent of serialized ICAP headers.
func (r *Response) CloseAfterWrite() bool {
	return r != nil && r.closeAfterWrite
}

// IsError returns true if the status code indicates an error (4xx or 5xx).
func (r *Response) IsError() bool {
	return r.StatusCode >= 400
}

// Clone creates a deep copy of the response.
func (r *Response) Clone() *Response {
	clone := NewResponse(r.StatusCode)
	clone.Proto = r.Proto
	clone.closeAfterWrite = r.closeAfterWrite

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
		BodyStream: m.BodyStream.Clone(),
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
	return r.WriteToContext(context.Background(), w, ResponseWriteOptions{})
}

// WriteToContext writes the response while honoring cancellation and reporting
// the boundary where actual streaming body delivery starts.
func (r *Response) WriteToContext(
	ctx context.Context,
	w io.Writer,
	options ResponseWriteOptions,
) (int64, error) {
	return r.writeDirectToContext(ctx, w, options)
}

// writeToBuffer writes the response content to a bytes.Buffer.
// This is used by both WriteTo and String to avoid code duplication.
func (r *Response) writeToBuffer(buf *bytes.Buffer) {
	r.writeEnvelopeToBuffer(buf)

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

// writeHTTPMessage writes an HTTP message to the buffer. Headers are written
// inline; the body (if any) is serialized in HTTP chunked transfer encoding,
// which is what ICAP mandates for req-body / res-body sections per RFC 3507.
func (r *Response) writeHTTPMessage(buf *bytes.Buffer, m *HTTPMessage, isRequest bool) {
	r.writeHTTPMessageHead(buf, m, isRequest)

	// Write body in chunked encoding when present. BuildEncapsulatedHeader
	// already advertises req-body / res-body at the offset right after this
	// blank line, so the bytes that follow must be valid chunked data.
	if len(m.Body) > 0 {
		_ = WriteChunkedBody(buf, m.Body)
	}
}

func (r *Response) writeHTTPMessageHead(buf *bytes.Buffer, m *HTTPMessage, isRequest bool) {
	if isRequest {
		buf.WriteString(m.Method)
		buf.WriteByte(' ')
		buf.WriteString(m.URI)
		buf.WriteByte(' ')
		buf.WriteString(m.Proto)
		buf.WriteString("\r\n")
	} else {
		buf.WriteString(m.Proto)
		buf.WriteByte(' ')
		buf.WriteString(m.Status)
		buf.WriteByte(' ')
		buf.WriteString(m.StatusText)
		buf.WriteString("\r\n")
	}
	if m.Header != nil {
		m.Header.WriteToBuffer(buf)
	}
	buf.WriteString("\r\n")
}

// BuildEncapsulatedHeader builds the Encapsulated header value based on content.
func (r *Response) BuildEncapsulatedHeader() string {
	var parts []string
	offset := 0
	finalSectionHasBody := false

	if r.HTTPRequest != nil {
		parts = append(parts, fmt.Sprintf("req-hdr=%d", offset))
		// Calculate offset for body
		offset += r.calculateHTTPMessageSize(r.HTTPRequest, true)
		if hasHTTPMessageBody(r.HTTPRequest) {
			finalSectionHasBody = true
			parts = append(parts, fmt.Sprintf("req-body=%d", offset))
			offset += calculateHTTPMessageBodySize(r.HTTPRequest)
		}
	}

	if r.HTTPResponse != nil {
		finalSectionHasBody = false
		parts = append(parts, fmt.Sprintf("res-hdr=%d", offset))
		offset += r.calculateHTTPMessageSize(r.HTTPResponse, false)
		if hasHTTPMessageBody(r.HTTPResponse) {
			finalSectionHasBody = true
			parts = append(parts, fmt.Sprintf("res-body=%d", offset))
		}
	}

	// A bodyless encapsulated message must terminate with null-body so clients
	// can determine the complete response boundary on a persistent connection.
	if !finalSectionHasBody && len(r.Body) == 0 {
		if len(parts) == 0 {
			return "null-body=0"
		}
		parts = append(parts, fmt.Sprintf("null-body=%d", offset))
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

func hasHTTPMessageBody(m *HTTPMessage) bool {
	return len(m.Body) > 0 || m.BodyStream != nil
}

func calculateHTTPMessageBodySize(m *HTTPMessage) int {
	if len(m.Body) > 0 {
		return chunkedEncodedSize(len(m.Body))
	}
	return 0
}

func chunkedEncodedSize(bodySize int) int {
	return len(strconv.FormatInt(int64(bodySize), 16)) + bodySize + len("\r\n\r\n0\r\n\r\n")
}

func (r *Response) writeEnvelopeToBuffer(buf *bytes.Buffer) {
	proto := r.Proto
	if proto == "" {
		proto = Version
	}
	buf.WriteString(proto + " " + strconv.Itoa(r.StatusCode) + " ")
	buf.WriteString(StatusText(r.StatusCode) + "\r\n")
	if r.Header != nil {
		r.Header.WriteToBuffer(buf)
	}
	encap := r.BuildEncapsulatedHeader()
	if encap != "" {
		buf.WriteString("Encapsulated: " + encap + "\r\n")
	}
	buf.WriteString("\r\n")
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
