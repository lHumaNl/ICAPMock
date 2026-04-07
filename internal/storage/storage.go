// Copyright 2026 ICAP Mock

package storage

import (
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
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
	Method     string              `json:"method,omitempty"`
	URI        string              `json:"uri,omitempty"`
	Status     string              `json:"status,omitempty"`
	StatusText string              `json:"status_text,omitempty"`
	Proto      string              `json:"proto"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"` // Base64 encoded
}

// FromICAPRequest converts an icap.Request to a StoredRequest.
// This creates a snapshot suitable for JSON serialization.
func FromICAPRequest(req *icap.Request, responseStatus int, processingTime time.Duration) *StoredRequest {
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
		sr.HTTPRequest = &HTTPMessageRecord{
			Method:  req.HTTPRequest.Method,
			URI:     req.HTTPRequest.URI,
			Proto:   req.HTTPRequest.Proto,
			Headers: make(map[string][]string),
		}
		for k, v := range req.HTTPRequest.Header {
			sr.HTTPRequest.Headers[k] = append([]string(nil), v...)
		}
		// Lazy load body for storage
		if body, err := req.HTTPRequest.GetBody(); err == nil && len(body) > 0 {
			sr.HTTPRequest.Body = string(body)
		}
	}

	// Copy HTTP response if present
	if req.HTTPResponse != nil {
		sr.HTTPResponse = &HTTPMessageRecord{
			Status:     req.HTTPResponse.Status,
			StatusText: req.HTTPResponse.StatusText,
			Proto:      req.HTTPResponse.Proto,
			Headers:    make(map[string][]string),
		}
		for k, v := range req.HTTPResponse.Header {
			sr.HTTPResponse.Headers[k] = append([]string(nil), v...)
		}
		// Lazy load body for storage
		if body, err := req.HTTPResponse.GetBody(); err == nil && len(body) > 0 {
			sr.HTTPResponse.Body = string(body)
		}
	}

	return sr
}

// GenerateRequestID generates a unique request ID based on the timestamp.
// Format: "req-YYYYMMDD-NNN" where NNN is a sequence number.
func GenerateRequestID(t time.Time) string {
	return "req-" + t.Format("20060102-150405.000")
}
