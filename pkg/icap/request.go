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
	"sync"
	"sync/atomic"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

// ICAP methods as defined in RFC 3507.
const (
	MethodREQMOD  = "REQMOD"  // Request modification
	MethodRESPMOD = "RESPMOD" // Response modification
	MethodOPTIONS = "OPTIONS" // Query server capabilities
)

// ICAP version.
const (
	Version      = "ICAP/1.0"
	VersionMajor = 1
	VersionMinor = 0
)

// Error definitions for request parsing.
var (
	ErrInvalidRequestLine = errors.New("invalid request line")
	ErrInvalidICAPVersion = errors.New("invalid or unsupported ICAP version")
	ErrInvalidMethod      = errors.New("invalid ICAP method")
	ErrMissingURI         = errors.New("missing URI")
	ErrInvalidURIScheme   = errors.New("invalid URI scheme, expected icap://")
	ErrMissingHost        = errors.New("missing Host header")
)

// StreamingBody wraps an io.Reader to provide a streaming body with O(1) memory usage.
// It implements io.ReadCloser and can be safely closed multiple times.
// This is used to avoid buffering large HTTP bodies in memory.
type StreamingBody struct {
	reader   io.Reader
	length   int64
	closed   atomic.Bool
	consumed atomic.Bool
}

// NewStreamingBody creates a new StreamingBody that wraps the given reader.
// The length parameter indicates the expected content length (-1 if unknown).
func NewStreamingBody(reader io.Reader, length int64) *StreamingBody {
	return &StreamingBody{
		reader: reader,
		length: length,
	}
}

// Read implements io.Reader. It reads from the underlying reader.
// Returns io.EOF if the body has been closed or fully consumed.
func (sb *StreamingBody) Read(p []byte) (n int, err error) {
	if sb.closed.Load() {
		return 0, io.EOF
	}
	n, err = sb.reader.Read(p)
	if err == io.EOF {
		sb.consumed.Store(true)
	}
	return n, err
}

// Close marks the body as closed. It is safe to call multiple times.
func (sb *StreamingBody) Close() error {
	sb.closed.Store(true)
	return nil
}

// ContentLength returns the expected content length.
// Returns -1 if the length is unknown.
func (sb *StreamingBody) ContentLength() int64 {
	return sb.length
}

// IsConsumed returns true if the body has been fully read.
func (sb *StreamingBody) IsConsumed() bool {
	return sb.consumed.Load() || sb.closed.Load()
}

// encapNotSet is the sentinel value indicating an Encapsulated offset was not set.
// Using -1 because 0 is a valid offset (e.g., req-body=0).
const encapNotSet = -1

// Encapsulated represents the parsed Encapsulated header values.
// It describes the offsets of various parts in the ICAP message body.
// Fields use -1 as sentinel to indicate "not present" since 0 is a valid offset.
type Encapsulated struct {
	ReqHdr   int // Offset to HTTP request headers (-1 = not present)
	ReqBody  int // Offset to HTTP request body (-1 = not present)
	ResHdr   int // Offset to HTTP response headers (-1 = not present)
	ResBody  int // Offset to HTTP response body (-1 = not present)
	NullBody int // Offset indicating no body (-1 = not present)
}

// NewEncapsulated creates a new Encapsulated with all offsets set to -1 (not present).
func NewEncapsulated() Encapsulated {
	return Encapsulated{
		ReqHdr:   encapNotSet,
		ReqBody:  encapNotSet,
		ResHdr:   encapNotSet,
		ResBody:  encapNotSet,
		NullBody: encapNotSet,
	}
}

// IsEmpty returns true if no offsets are set.
func (e Encapsulated) IsEmpty() bool {
	return e.ReqHdr == encapNotSet && e.ReqBody == encapNotSet &&
		e.ResHdr == encapNotSet && e.ResBody == encapNotSet &&
		e.NullBody == encapNotSet
}

// HasReqBody returns true if the message contains an HTTP request body.
func (e Encapsulated) HasReqBody() bool {
	return e.ReqBody != encapNotSet
}

// HasResBody returns true if the message contains an HTTP response body.
func (e Encapsulated) HasResBody() bool {
	return e.ResBody != encapNotSet
}

// lazyLoadBody loads a body from a reader exactly once using sync.Once, storing
// the result in the provided body slice and error pointers. The errFmt parameter
// is used to wrap any read error (e.g., "loading body: %w").
func lazyLoadBody(once *sync.Once, mu *sync.RWMutex, reader *io.Reader, body *[]byte, loaded *bool, bodyErr *error, errFmt string) ([]byte, error) { //nolint:gocritic // ptrToRefParam: pointer needed to read from caller's io.Reader field
	once.Do(func() {
		if *reader == nil {
			return
		}
		data, err := io.ReadAll(*reader)
		mu.Lock()
		if err != nil {
			*bodyErr = fmt.Errorf(errFmt, err)
			*body = nil
		} else {
			*body = data
			*loaded = true
		}
		mu.Unlock()
	})

	mu.RLock()
	defer mu.RUnlock()
	return *body, *bodyErr
}

// HTTPMessage represents an embedded HTTP request or response.
// HTTP message bodies are lazy-loaded from BodyReader when GetBody() is called.
// The body-related methods (GetBody, HasBody, IsBodyLoaded, SetLoadedBody) are
// thread-safe and can be called from multiple goroutines.
//
// WARNING: BodyReader is consumed when GetBody() is first called. Direct access
// to BodyReader bypasses thread-safety and should only be done when you have
// exclusive access to the message.
type HTTPMessage struct {
	BodyReader io.Reader
	bodyErr    error
	Header     Header
	Method     string
	URI        string
	Status     string
	StatusText string
	Proto      string
	Body       []byte
	mu         sync.RWMutex
	bodyOnce   sync.Once
	bodyLoaded bool
}

// GetBody returns the body content, loading it lazily from BodyReader if needed.
// This method is thread-safe and can be called from multiple goroutines.
// sync.Once ensures that the body is loaded exactly once, even with concurrent calls.
// If an error occurred during loading, it is stored and returned on subsequent calls.
//
// For O(1) memory usage with large bodies, use BodyReader directly instead,
// but note that direct BodyReader access bypasses thread-safety.
func (m *HTTPMessage) GetBody() ([]byte, error) {
	return lazyLoadBody(&m.bodyOnce, &m.mu, &m.BodyReader, &m.Body, &m.bodyLoaded, &m.bodyErr, "loading body: %w")
}

// HasBody returns true if the message has a body available.
// This method is thread-safe and checks both the loaded body and the presence
// of a reader.
func (m *HTTPMessage) HasBody() bool {
	m.mu.RLock()
	loaded := m.bodyLoaded
	body := m.Body
	bodyReader := m.BodyReader
	m.mu.RUnlock()

	return (loaded && len(body) > 0) || bodyReader != nil
}

// IsBodyLoaded returns true if the body has been loaded into memory.
// This method is thread-safe.
func (m *HTTPMessage) IsBodyLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bodyLoaded
}

// SetLoadedBody sets the body and marks it as loaded.
// This method is thread-safe and is useful when creating a clone with an
// already-loaded body.
// It clears the BodyReader since the body is already loaded, and clears any
// previous error.
func (m *HTTPMessage) SetLoadedBody(body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mark as loaded so bodyOnce.Do won't try to load from BodyReader
	m.bodyLoaded = true
	m.Body = body
	m.BodyReader = nil // Clear reader since body is already loaded
	m.bodyErr = nil
}

// GetPreviewBody reads only the first N bytes from the body for preview mode.
// This is used when the ICAP client requests a preview of the HTTP body.
//
// Parameters:
//   - previewSize: The maximum number of bytes to read (0 means no limit)
//
// Returns:
//   - []byte: The preview body (may be smaller than previewSize if actual body is smaller)
//   - error: Any error reading the body
//
// This method is thread-safe. It avoids loading the entire body into memory
// when only a preview is needed.
func (m *HTTPMessage) GetPreviewBody(previewSize int) ([]byte, error) {
	if previewSize <= 0 {
		// No preview requested, read entire body
		return m.GetBody()
	}

	// Check if body is already loaded
	m.mu.RLock()
	loaded := m.bodyLoaded
	body := m.Body
	m.mu.RUnlock()

	// If body is already loaded, return just the preview portion
	if loaded && body != nil {
		if len(body) <= previewSize {
			return body, nil
		}
		return body[:previewSize], nil
	}

	// Body is not loaded yet, read only preview portion from reader
	// Note: BodyReader access is not mutex-protected by design (see doc comment on HTTPMessage).
	// This is safe because preview reading happens before any concurrent GetBody() call.
	if m.BodyReader == nil {
		return nil, nil // No body to preview
	}

	// Read only previewSize bytes from the reader
	preview := make([]byte, previewSize)
	n, err := io.ReadFull(m.BodyReader, preview)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("reading preview body: %w", err)
	}

	// Return the actual bytes read (may be less than previewSize)
	return preview[:n], nil
}

// Request represents an ICAP request.
// The ICAP body (Body/BodyReader) is always fully buffered during parsing.
// For HTTP message bodies, use HTTPRequest.GetBody() or HTTPResponse.GetBody()
// which support lazy loading.
//
// Body-related methods (GetBody, IsBodyLoaded) are thread-safe.
type Request struct {
	BodyReader   io.Reader
	bodyErr      error
	Header       Header
	HTTPRequest  *HTTPMessage
	HTTPResponse *HTTPMessage
	// Captures holds path-parameter values extracted from the ICAP URI by the
	// scenario matcher (e.g. "{id}" in the scenario endpoint yields
	// Captures["id"]). Nil or empty when no endpoint capture matched. Consumers
	// substitute "${name}" in response fields using this map.
	Captures     map[string]string
	RemoteAddr   string
	URI          string
	Proto        string
	Method       string
	ClientIP     string
	Body         []byte
	Encapsulated Encapsulated
	Preview      int
	mu           sync.RWMutex
	bodyOnce     sync.Once
	bodyLoaded   bool
}

// GetBody returns the ICAP body content, loading it lazily from BodyReader if needed.
// This method is thread-safe and can be called from multiple goroutines.
// sync.Once ensures that the body is loaded exactly once, even with concurrent calls.
// If an error occurred during loading, it is stored and returned on subsequent calls.
// For HTTP message bodies, use HTTPRequest.GetBody() or HTTPResponse.GetBody() instead.
//
// Note: The ICAP body is typically fully buffered during parsing, so this method
// usually returns immediately without I/O.
func (r *Request) GetBody() ([]byte, error) {
	return lazyLoadBody(&r.bodyOnce, &r.mu, &r.BodyReader, &r.Body, &r.bodyLoaded, &r.bodyErr, "loading ICAP body: %w")
}

// IsBodyLoaded returns true if the body has been loaded into memory.
// This method is thread-safe.
func (r *Request) IsBodyLoaded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bodyLoaded
}

// SetLoadedBody sets the body and marks it as loaded.
// This method is thread-safe and is useful when creating a clone with an
// already-loaded body.
// It clears the BodyReader since the body is already loaded, and clears any
// previous error.
func (r *Request) SetLoadedBody(body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Mark as loaded so bodyOnce.Do won't try to load from BodyReader
	r.bodyLoaded = true
	r.Body = body
	r.BodyReader = nil // Clear reader since body is already loaded
	r.bodyErr = nil
}

// NewRequest creates a new ICAP request with the given method and URI.
func NewRequest(method, uri string) (*Request, error) {
	if !isValidMethod(method) {
		return nil, ErrInvalidMethod
	}
	if uri == "" {
		return nil, ErrMissingURI
	}

	// Get header map from pool
	hdrPtr := pool.HeaderMapPool.Get()
	return &Request{
		Method:       method,
		URI:          uri,
		Proto:        Version,
		Header:       Header(*hdrPtr),
		Encapsulated: NewEncapsulated(),
	}, nil
}

// ParseRequest reads and parses an ICAP request from a bufio.Reader.
func ParseRequest(r *bufio.Reader) (*Request, error) { //nolint:gocyclo // ICAP request parsing is inherently sequential
	// Read request line
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading request line: %w", err)
	}

	// Parse request line: METHOD URI VERSION
	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return nil, ErrInvalidRequestLine
	}

	method, uri, proto := parts[0], parts[1], parts[2]

	// Validate method
	if !isValidMethod(method) {
		return nil, ErrInvalidMethod
	}

	// Validate URI
	if uri == "" {
		return nil, ErrMissingURI
	}
	if len(uri) < 7 || !strings.EqualFold(uri[:7], "icap://") {
		return nil, ErrInvalidURIScheme
	}

	// Validate ICAP version
	if !isValidVersion(proto) {
		return nil, ErrInvalidICAPVersion
	}

	// Read headers using textproto for proper handling
	tp := textproto.NewReader(r)
	headerMap, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading headers: %w", err)
	}

	// Get header map from pool
	hdrPtr := pool.HeaderMapPool.Get()
	header := Header(*hdrPtr)
	for k, v := range headerMap {
		header[k] = v
	}

	req := &Request{
		Method:       method,
		URI:          uri,
		Proto:        proto,
		Header:       header,
		Encapsulated: NewEncapsulated(),
	}

	// Parse Encapsulated header
	if encapStr, exists := header.Get("Encapsulated"); exists {
		req.Encapsulated, err = ParseEncapsulatedHeader(encapStr)
		if err != nil {
			return nil, fmt.Errorf("parsing Encapsulated header: %w", err)
		}

		// Parse embedded HTTP message if present
		if parseErr := req.parseEncapsulatedMessage(r); parseErr != nil {
			return nil, fmt.Errorf("parsing encapsulated message: %w", parseErr)
		}
	}

	// Parse Preview header (RFC 3507 Section 4.6)
	if previewStr, exists := header.Get("Preview"); exists {
		req.Preview, err = ParsePreviewHeader(previewStr)
		if err != nil {
			return nil, fmt.Errorf("parsing Preview header: %w", err)
		}
	}

	return req, nil
}

// parseEncapsulatedMessage parses the embedded HTTP request or response.
// This implementation uses TRUE O(1) STREAMING - it does NOT buffer the body.
// HTTP headers are parsed directly from the stream, and the remaining stream
// (the chunked HTTP body) is passed to HTTPMessage.BodyReader for lazy consumption.
//
// Memory usage is constant regardless of body size:
// - Before: O(body_size) per request (buffered entire body)
// - After: O(1) per request (streaming, no buffering)
//
// For RESPMOD, the message contains both the HTTP request (req-hdr) and
// the HTTP response (res-hdr) being modified. We parse both.
func (r *Request) parseEncapsulatedMessage(reader *bufio.Reader) error {
	if r.Encapsulated.IsEmpty() {
		return nil
	}

	// For REQMOD: parse HTTP request
	// For RESPMOD: parse HTTP request first (it's at the start of the body)
	if r.Method == MethodREQMOD && r.Encapsulated.ReqHdr >= 0 {
		if err := r.parseHTTPRequestStreaming(reader); err != nil {
			return err
		}
	} else if r.Method == MethodRESPMOD && r.Encapsulated.ReqHdr >= 0 {
		// For RESPMOD, we need to parse/skip the HTTP request first
		// to get to the HTTP response
		if err := r.parseHTTPRequestStreaming(reader); err != nil {
			return err
		}
	}

	// Parse HTTP response if res-hdr is present (RESPMOD only)
	if r.Method == MethodRESPMOD && r.Encapsulated.ResHdr > 0 {
		if err := r.parseHTTPResponseStreaming(reader); err != nil {
			return err
		}
	}

	return nil
}

// parseHTTPRequestStreaming parses the embedded HTTP request directly from the stream.
// This method provides TRUE O(1) MEMORY USAGE by:
// 1. Parsing HTTP headers from the stream (small, constant size)
// 2. Passing the remaining stream (chunked body) directly to BodyReader
// 3. Never buffering the entire body in memory
//
// The HTTP body is only loaded into memory when GetBody() is called,
// allowing consumers to process the body in streaming fashion.
func (r *Request) parseHTTPRequestStreaming(reader *bufio.Reader) error {
	// Check if there's actually data to read (peek at first byte)
	// This handles the case where Encapsulated header exists but no body was sent
	if _, err := reader.Peek(1); err != nil {
		if err == io.EOF {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("checking for HTTP request: %w", err)
	}

	// Read request line
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP request line: %w", err)
	}

	line = strings.TrimSuffix(line, "\r\n")
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return errors.New("invalid HTTP request line")
	}

	// Get header map from pool
	hdrPtr := pool.HeaderMapPool.Get()
	r.HTTPRequest = &HTTPMessage{
		Method: parts[0],
		URI:    parts[1],
		Proto:  parts[2],
		Header: Header(*hdrPtr),
	}

	// Read HTTP headers using textproto
	tp := textproto.NewReader(reader)
	headerMap, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading HTTP headers: %w", err)
	}
	for k, v := range headerMap {
		r.HTTPRequest.Header[k] = v
	}

	// If there's a body, pass the remaining stream directly as the body reader
	// This is the KEY for O(1) memory: we don't buffer the body
	if r.Encapsulated.HasReqBody() {
		// The remaining data in the reader IS the chunked HTTP body
		// Wrap it in a ChunkedReader for proper ICAP chunked encoding handling
		r.HTTPRequest.BodyReader = NewChunkedReader(reader)
	}

	return nil
}

// parseHTTPResponseStreaming parses the embedded HTTP response directly from the stream.
// This method provides TRUE O(1) MEMORY USAGE by parsing headers from the stream
// and passing the remaining chunked body directly to BodyReader without buffering.
func (r *Request) parseHTTPResponseStreaming(reader *bufio.Reader) error {
	// Check if there's actually data to read (peek at first byte)
	// This handles the case where Encapsulated header exists but no body was sent
	if _, err := reader.Peek(1); err != nil {
		if err == io.EOF {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("checking for HTTP response: %w", err)
	}

	// Read status line
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			// No body content, nothing to parse
			return nil
		}
		return fmt.Errorf("reading HTTP response line: %w", err)
	}

	line = strings.TrimSuffix(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return errors.New("invalid HTTP response line")
	}

	// Get header map from pool
	hdrPtr := pool.HeaderMapPool.Get()
	r.HTTPResponse = &HTTPMessage{
		Proto:      parts[0],
		Status:     parts[1],
		StatusText: parts[2],
		Header:     Header(*hdrPtr),
	}

	// Read HTTP headers using textproto
	tp := textproto.NewReader(reader)
	headerMap, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading HTTP headers: %w", err)
	}
	for k, v := range headerMap {
		r.HTTPResponse.Header[k] = v
	}

	// If there's a body, pass the remaining stream directly as the body reader
	// This is the KEY for O(1) memory: we don't buffer the body
	if r.Encapsulated.HasResBody() {
		// The remaining data in the reader IS the chunked HTTP body
		r.HTTPResponse.BodyReader = NewChunkedReader(reader)
	}

	return nil
}

// ReadRequest reads an ICAP request from an io.Reader.
func ReadRequest(r io.Reader) (*Request, error) {
	return ParseRequest(bufio.NewReader(r))
}

// Validate validates the request for required fields.
func (r *Request) Validate() error {
	if !isValidMethod(r.Method) {
		return ErrInvalidMethod
	}
	if r.URI == "" {
		return ErrMissingURI
	}
	if len(r.URI) < 7 || !strings.EqualFold(r.URI[:7], "icap://") {
		return ErrInvalidURIScheme
	}
	return nil
}

// GetHeader returns the value of a header (case-insensitive).
func (r *Request) GetHeader(key string) (string, bool) {
	if r.Header == nil {
		return "", false
	}
	return r.Header.Get(key)
}

// SetHeader sets a header value.
func (r *Request) SetHeader(key, value string) {
	if r.Header == nil {
		r.Header = make(Header)
	}
	r.Header.Set(key, value)
}

// SetClientIP sets the client IP address.
func (r *Request) SetClientIP(ip string) {
	r.ClientIP = ip
}

// HasBody returns true if the request has a body.
func (r *Request) HasBody() bool {
	return r.Encapsulated.HasReqBody() || r.Encapsulated.HasResBody()
}

// IsOPTIONS returns true if the request method is OPTIONS.
func (r *Request) IsOPTIONS() bool {
	return r.Method == MethodOPTIONS
}

// IsREQMOD returns true if the request method is REQMOD.
func (r *Request) IsREQMOD() bool {
	return r.Method == MethodREQMOD
}

// IsRESPMOD returns true if the request method is RESPMOD.
func (r *Request) IsRESPMOD() bool {
	return r.Method == MethodRESPMOD
}

// IsPreviewMode returns true if the request is in preview mode (Preview > 0).
func (r *Request) IsPreviewMode() bool {
	return r.Preview > 0
}

// GetPreviewBody reads the appropriate preview body based on the request method.
// For REQMOD, it reads from HTTPRequest body.
// For RESPMOD, it reads from HTTPResponse body.
// Returns nil if there is no body to preview.
func (r *Request) GetPreviewBody() ([]byte, error) {
	if !r.IsPreviewMode() {
		return nil, nil
	}

	switch r.Method {
	case MethodREQMOD:
		if r.HTTPRequest != nil {
			return r.HTTPRequest.GetPreviewBody(r.Preview)
		}
	case MethodRESPMOD:
		if r.HTTPResponse != nil {
			return r.HTTPResponse.GetPreviewBody(r.Preview)
		}
	}

	return nil, nil
}

// WriteTo writes the request to an io.Writer.
func (r *Request) WriteTo(w io.Writer) (int64, error) {
	var buf bytes.Buffer

	// Write request line
	buf.WriteString(r.Method)
	buf.WriteByte(' ')
	buf.WriteString(r.URI)
	buf.WriteByte(' ')
	if r.Proto == "" {
		buf.WriteString(Version)
	} else {
		buf.WriteString(r.Proto)
	}
	buf.WriteString("\r\n")

	// Write headers
	if r.Header != nil {
		r.Header.WriteToBuffer(&buf)
	}

	// Write blank line
	buf.WriteString("\r\n")

	// Write body if present
	if len(r.Body) > 0 {
		buf.Write(r.Body)
	}

	return buf.WriteTo(w)
}

// ParseEncapsulatedHeader parses the Encapsulated header value.
// Format: "req-hdr=0, req-body=412" or "req-hdr=0, res-hdr=200, res-body=350".
func ParseEncapsulatedHeader(s string) (Encapsulated, error) {
	encap := NewEncapsulated()

	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return encap, fmt.Errorf("invalid encapsulated part: %s", part)
		}

		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			return encap, fmt.Errorf("invalid offset value: %s", kv[1])
		}

		switch key {
		case "req-hdr":
			encap.ReqHdr = value
		case "req-body":
			encap.ReqBody = value
		case "res-hdr":
			encap.ResHdr = value
		case "res-body":
			encap.ResBody = value
		case "null-body":
			encap.NullBody = value
		}
	}

	return encap, nil
}

// ParsePreviewHeader parses the Preview header value (RFC 3507 Section 4.6).
// Format: "0" (no preview) or "N" where N is the number of bytes to preview.
// Returns the number of bytes to read for preview.
func ParsePreviewHeader(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty preview value")
	}

	// Parse the preview count
	value, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid preview value: %s", s)
	}

	// Validate that the value is non-negative
	if value < 0 {
		return 0, fmt.Errorf("preview value must be non-negative: %d", value)
	}

	return value, nil
}

// isValidMethod returns true if the method is a valid ICAP method.
func isValidMethod(method string) bool {
	switch method {
	case MethodREQMOD, MethodRESPMOD, MethodOPTIONS:
		return true
	default:
		return false
	}
}

// isValidVersion returns true if the version is a supported ICAP version.
func isValidVersion(version string) bool {
	return version == "ICAP/1.0"
}
