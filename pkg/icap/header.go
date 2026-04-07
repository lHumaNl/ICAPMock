// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"strings"
	"sync"
)

// Header represents ICAP headers with case-insensitive key lookup.
// It implements the same semantics as http.Header but for ICAP protocol.
type Header map[string][]string

// canonicalHeaderKeys maps common header names to their canonical form.
// This follows the HTTP/1.1 header canonicalization rules used by ICAP.
var canonicalHeaderKeys = map[string]string{
	"host":                 "Host",
	"encapsulated":         "Encapsulated",
	"istag":                "ISTag",
	"service":              "Service",
	"service-id":           "Service-ID",
	"max-connections":      "Max-Connections",
	"options-ttl":          "Options-TTL",
	"date":                 "Date",
	"allow":                "Allow",
	"preview":              "Preview",
	"x-client-ip":          "X-Client-IP",
	"x-authenticated-user": "X-Authenticated-User",
	"content-type":         "Content-Type",
	"content-length":       "Content-Length",
	"transfer-encoding":    "Transfer-Encoding",
	"connection":           "Connection",
	"cache-control":        "Cache-Control",
	"etag":                 "ETag",
	"te":                   "TE",
	"methods":              "Methods",
}

// CanonicalHeaderKey returns the canonical format of a header key.
// Header keys are case-insensitive, but canonical form is used for storage.
func CanonicalHeaderKey(key string) string {
	// Fast path: check common headers
	lower := strings.ToLower(key)
	if v, ok := canonicalHeaderKeys[lower]; ok {
		return v
	}

	// Slow path: canonicalize the key using text canonicalization
	return textCanonicalize(key)
}

// canonicalCache caches results of textCanonicalize for non-standard header names.
// Header names are a small finite set per application, so this won't grow unbounded.
var canonicalCache sync.Map

// textCanonicalize converts a header key to canonical form.
// E.g., "content-type" -> "Content-Type", "x-custom-header" -> "X-Custom-Header".
func textCanonicalize(s string) string {
	if s == "" {
		return ""
	}

	// Check cache first
	if v, ok := canonicalCache.Load(s); ok {
		return v.(string) //nolint:errcheck
	}

	upper := true
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if upper && 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		}
		if !upper && 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
		upper = c == '-'
	}
	canonical := string(result)
	canonicalCache.Store(s, canonical)
	return canonical
}

// NewHeader creates a new Header instance.
func NewHeader() Header {
	return make(Header)
}

// Get returns the first value associated with the given key.
// The key is case-insensitive.
func (h Header) Get(key string) (string, bool) {
	if h == nil {
		return "", false
	}
	v := h[CanonicalHeaderKey(key)]
	if len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// Set sets the header entries associated with key to the single element value.
// It replaces any existing values associated with key.
// The key is case-insensitive.
func (h Header) Set(key, value string) {
	h[CanonicalHeaderKey(key)] = []string{value}
}

// Add adds the key, value pair to the header.
// It appends to any existing values associated with key.
// The key is case-insensitive.
func (h Header) Add(key, value string) {
	key = CanonicalHeaderKey(key)
	h[key] = append(h[key], value)
}

// Del deletes the values associated with key.
// The key is case-insensitive.
func (h Header) Del(key string) {
	delete(h, CanonicalHeaderKey(key))
}

// Values returns all values associated with the given key.
// The key is case-insensitive. It returns nil if the key is not present.
func (h Header) Values(key string) []string {
	if h == nil {
		return nil
	}
	return h[CanonicalHeaderKey(key)]
}

// Clone returns a deep copy of the header.
func (h Header) Clone() Header {
	if h == nil {
		return nil
	}
	clone := make(Header, len(h))
	for k, v := range h {
		clone[k] = append([]string(nil), v...)
	}
	return clone
}

// Walk iterates over all header entries, calling fn for each.
// If fn returns false, iteration stops.
// The keys are passed in canonical form.
func (h Header) Walk(fn func(key, value string) bool) {
	if h == nil {
		return
	}
	for k, values := range h {
		for _, v := range values {
			if !fn(k, v) {
				return
			}
		}
	}
}

// Len returns the number of unique header keys.
func (h Header) Len() int {
	if h == nil {
		return 0
	}
	return len(h)
}

// Has reports whether the header contains the given key.
// The key is case-insensitive.
func (h Header) Has(key string) bool {
	_, exists := h.Get(key)
	return exists
}

// SetAll sets multiple headers from a map.
// It replaces any existing values for each key.
func (h Header) SetAll(headers map[string]string) {
	for k, v := range headers {
		h.Set(k, v)
	}
}

// ToMap converts the header to a map with single values.
// If a header has multiple values, only the first is included.
func (h Header) ToMap() map[string]string {
	result := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}

// WriteToBuffer writes the formatted header representation directly to buf.
// Format: "Key: Value\r\n" for each header.
func (h Header) WriteToBuffer(buf *bytes.Buffer) {
	if h == nil {
		return
	}
	for k, values := range h {
		for _, v := range values {
			buf.WriteString(k)
			buf.WriteString(": ")
			buf.WriteString(v)
			buf.WriteString("\r\n")
		}
	}
}

// String returns a formatted string representation of all headers.
// Format: "Key: Value\r\n" for each header.
func (h Header) String() string {
	if h == nil {
		return ""
	}
	var buf bytes.Buffer
	h.WriteToBuffer(&buf)
	return buf.String()
}
