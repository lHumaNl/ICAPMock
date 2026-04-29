// Copyright 2026 ICAP Mock

package storage

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

var requestIDSequence atomic.Uint64

const (
	bodyOmittedTooLarge  = "body_size_limit_exceeded"
	bodyOmittedReadError = "body_read_error"
	unlimitedBodyLimit   = -1
)

// Storage defines the full interface for persisting ICAP requests.
// It composes RequestReader and RequestWriter interfaces following the
// Interface Segregation Principle (ISP), allowing clients to depend
// only on the operations they need.
//
// This interface is provided for backward compatibility and for clients
// that need both read and write capabilities. Clients with more specific
// needs should use:
//   - RequestReader - for read/query operations (replay engines, monitoring)
//   - RequestWriter - for write/lifecycle operations (request handlers)
//
// Implementations must be thread-safe for concurrent access.
type Storage interface {
	RequestReader
	RequestWriter
}

// RequestFilter defines criteria for filtering stored requests.
type RequestFilter struct {
	// Start is the earliest timestamp to include (inclusive).
	Start time.Time

	// End is the latest timestamp to include (exclusive).
	End time.Time

	// Method filters by ICAP method (e.g., "REQMOD", "RESPMOD").
	// Empty string matches all methods.
	Method string

	// ClientIP filters by client IP address.
	// Empty string matches all clients.
	ClientIP string

	// Limit restricts the number of results returned.
	// 0 means no limit (use with caution).
	Limit int

	// Offset skips the first N results (for pagination).
	Offset int
}

// StoredRequest represents a persisted ICAP request with metadata.
// This is the JSON-serializable format used for storage.
type StoredRequest struct {
	// ID is a unique identifier for the request.
	// Format: "req-YYYYMMDD-NNN" (e.g., "req-20240115-001")
	ID string `json:"id"`

	// Timestamp is when the request was received.
	Timestamp time.Time `json:"timestamp"`

	// Method is the ICAP method (REQMOD, RESPMOD, OPTIONS).
	Method string `json:"method"`

	// URI is the ICAP URI (icap://host/service).
	URI string `json:"uri"`

	// Headers contains the ICAP headers.
	Headers map[string][]string `json:"headers,omitempty"`

	// HTTPRequest contains the embedded HTTP request (for REQMOD).
	HTTPRequest *HTTPMessageRecord `json:"http_request,omitempty"`

	// HTTPResponse contains the embedded HTTP response (for RESPMOD).
	HTTPResponse *HTTPMessageRecord `json:"http_response,omitempty"`

	// ClientIP is the IP address of the client.
	ClientIP string `json:"client_ip,omitempty"`

	// RemoteAddr is the remote address string.
	RemoteAddr string `json:"remote_addr,omitempty"`

	// ProcessingTimeMs is the time taken to process the request.
	ProcessingTimeMs int64 `json:"processing_time_ms"`

	// ResponseStatus is the ICAP response status code sent to the client.
	ResponseStatus int `json:"response_status"`
}

// HTTPMessageRecord represents a serialized HTTP message.
type HTTPMessageRecord struct {
	Method            string              `json:"method,omitempty"`
	URI               string              `json:"uri,omitempty"`
	Status            string              `json:"status,omitempty"`
	StatusText        string              `json:"status_text,omitempty"`
	Proto             string              `json:"proto"`
	Headers           map[string][]string `json:"headers,omitempty"`
	Body              string              `json:"body,omitempty"` // Base64 encoded
	BodyOmittedReason string              `json:"body_omitted_reason,omitempty"`
	BodyTruncated     bool                `json:"body_truncated,omitempty"`
	BodyLimit         int64               `json:"body_limit,omitempty"`
}

// FromICAPRequest converts an icap.Request to a StoredRequest.
// This creates a snapshot suitable for JSON serialization.
func FromICAPRequest(req *icap.Request, responseStatus int, processingTime time.Duration) *StoredRequest {
	return FromICAPRequestWithBodyLimit(req, responseStatus, processingTime, 0)
}

// FromICAPRequestWithBodyLimit converts an icap.Request to a StoredRequest,
// limiting lazy body reads to maxBodySize+1 bytes when maxBodySize is positive.
func FromICAPRequestWithBodyLimit(
	req *icap.Request,
	responseStatus int,
	processingTime time.Duration,
	maxBodySize int64,
) *StoredRequest {
	sr := &StoredRequest{
		ID:               GenerateRequestID(time.Now()),
		Timestamp:        time.Now(),
		Method:           req.Method,
		URI:              req.URI,
		Headers:          make(map[string][]string),
		ClientIP:         req.ClientIP,
		RemoteAddr:       req.RemoteAddr,
		ProcessingTimeMs: processingTime.Milliseconds(),
		ResponseStatus:   responseStatus,
	}

	// Copy ICAP headers
	for k, v := range req.Header {
		sr.Headers[k] = append([]string(nil), v...)
	}

	// Copy HTTP request if present
	if req.HTTPRequest != nil {
		sr.HTTPRequest = fromHTTPRequest(req.HTTPRequest, maxBodySize)
	}

	// Copy HTTP response if present
	if req.HTTPResponse != nil {
		sr.HTTPResponse = fromHTTPResponse(req.HTTPResponse, maxBodySize)
	}

	return sr
}

func fromHTTPRequest(msg *icap.HTTPMessage, maxBodySize int64) *HTTPMessageRecord {
	record := &HTTPMessageRecord{Method: msg.Method, URI: msg.URI, Proto: msg.Proto, Headers: copyHeaders(msg.Header)}
	copyHTTPBody(record, msg, maxBodySize)
	return record
}

func fromHTTPResponse(msg *icap.HTTPMessage, maxBodySize int64) *HTTPMessageRecord {
	record := &HTTPMessageRecord{Status: msg.Status, StatusText: msg.StatusText, Proto: msg.Proto, Headers: copyHeaders(msg.Header)}
	copyHTTPBody(record, msg, maxBodySize)
	return record
}

func copyHeaders(headers icap.Header) map[string][]string {
	copied := make(map[string][]string)
	for k, v := range headers {
		copied[k] = append([]string(nil), v...)
	}
	return copied
}

func copyHTTPBody(record *HTTPMessageRecord, msg *icap.HTTPMessage, maxBodySize int64) {
	body, err := getHTTPBodyForStorage(msg, maxBodySize)
	if err != nil {
		markBodyOmitted(record, err, maxBodySize)
		return
	}
	if len(body) > 0 {
		record.Body = string(body)
	}
}

func getHTTPBodyForStorage(msg *icap.HTTPMessage, maxBodySize int64) ([]byte, error) {
	if maxBodySize <= 0 {
		return msg.GetBodyLimited(unlimitedBodyLimit)
	}
	return msg.GetBodyLimited(maxBodySize)
}

func markBodyOmitted(record *HTTPMessageRecord, err error, maxBodySize int64) {
	if maxBodySize > 0 {
		record.BodyLimit = maxBodySize
	}
	if errors.Is(err, icap.ErrBodyTooLarge) {
		record.BodyTruncated = true
		record.BodyOmittedReason = bodyOmittedTooLarge
		return
	}
	record.BodyOmittedReason = bodyOmittedReadError
}

// GenerateRequestID generates a unique request ID based on timestamp plus a process-local sequence.
func GenerateRequestID(t time.Time) string {
	seq := requestIDSequence.Add(1)
	return fmt.Sprintf("req-%s-%06d", t.Format("20060102-150405.000"), seq)
}
