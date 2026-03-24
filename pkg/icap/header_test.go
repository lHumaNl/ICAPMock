// Package icap provides ICAP (Internet Content Adaptation Protocol) data structures
// and utilities per RFC 3507.
package icap_test

import (
	"bytes"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestHeaderSetAndGet tests setting and getting headers with case-insensitive lookup.
func TestHeaderSetAndGet(t *testing.T) {
	tests := []struct {
		name       string
		setKey     string
		setValue   string
		getKey     string
		wantValue  string
		wantExists bool
	}{
		{
			name:       "simple header",
			setKey:     "Host",
			setValue:   "icap-server.net",
			getKey:     "Host",
			wantValue:  "icap-server.net",
			wantExists: true,
		},
		{
			name:       "case insensitive lookup - lowercase",
			setKey:     "Content-Type",
			setValue:   "text/plain",
			getKey:     "content-type",
			wantValue:  "text/plain",
			wantExists: true,
		},
		{
			name:       "case insensitive lookup - uppercase",
			setKey:     "content-length",
			setValue:   "1000",
			getKey:     "CONTENT-LENGTH",
			wantValue:  "1000",
			wantExists: true,
		},
		{
			name:       "case insensitive lookup - mixed case",
			setKey:     "X-Custom-Header",
			setValue:   "custom-value",
			getKey:     "x-CUSTOM-header",
			wantValue:  "custom-value",
			wantExists: true,
		},
		{
			name:       "non-existent header",
			setKey:     "Host",
			setValue:   "localhost",
			getKey:     "X-Not-Exists",
			wantValue:  "",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := make(icap.Header)
			h.Set(tt.setKey, tt.setValue)

			gotValue, gotExists := h.Get(tt.getKey)
			if gotExists != tt.wantExists {
				t.Errorf("Header.Get() exists = %v, want %v", gotExists, tt.wantExists)
			}
			if gotValue != tt.wantValue {
				t.Errorf("Header.Get() value = %q, want %q", gotValue, tt.wantValue)
			}
		})
	}
}

// TestHeaderDelete tests deleting headers with case-insensitive matching.
func TestHeaderDelete(t *testing.T) {
	h := make(icap.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", "100")
	h.Set("X-Custom", "value")

	// Delete with different case
	h.Del("content-type")

	if _, exists := h.Get("Content-Type"); exists {
		t.Error("Header.Del() failed to delete header with different case")
	}

	// Verify other headers still exist
	if _, exists := h.Get("Content-Length"); !exists {
		t.Error("Header.Del() deleted wrong header")
	}
}

// TestHeaderValues tests getting all values for a header.
func TestHeaderValues(t *testing.T) {
	h := make(icap.Header)
	h.Add("Accept", "text/html")
	h.Add("Accept", "application/json")

	values := h.Values("accept")
	if len(values) != 2 {
		t.Errorf("Header.Values() returned %d values, want 2", len(values))
	}

	// Verify we can iterate through values
	found := make(map[string]bool)
	for _, v := range values {
		found[v] = true
	}
	if !found["text/html"] || !found["application/json"] {
		t.Errorf("Header.Values() missing expected values, got %v", values)
	}
}

// TestHeaderClone tests cloning headers.
func TestHeaderClone(t *testing.T) {
	original := make(icap.Header)
	original.Set("Host", "localhost")
	original.Set("Content-Type", "application/json")

	clone := original.Clone()

	// Modify clone
	clone.Set("Host", "modified")

	// Original should be unchanged
	if val, _ := original.Get("Host"); val != "localhost" {
		t.Errorf("Clone did not create independent copy, original Host = %q", val)
	}

	// Clone should have new value
	if val, _ := clone.Get("Host"); val != "modified" {
		t.Errorf("Clone modification failed, clone Host = %q", val)
	}
}

// TestHeaderWalk tests walking through all headers.
func TestHeaderWalk(t *testing.T) {
	h := make(icap.Header)
	h.Set("Host", "localhost")
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", "100")

	collected := make(map[string]string)
	h.Walk(func(key, value string) bool {
		collected[key] = value
		return true
	})

	expected := map[string]string{
		"Host":           "localhost",
		"Content-Type":   "application/json",
		"Content-Length": "100",
	}

	for k, v := range expected {
		if collected[k] != v {
			t.Errorf("Header.Walk() missing %s = %s, got %s", k, v, collected[k])
		}
	}
}

// TestHeaderCanonicalKey tests getting canonical key format.
func TestHeaderCanonicalKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"host", "Host"},
		{"content-type", "Content-Type"},
		{"x-custom-header", "X-Custom-Header"},
		{"CACHE-CONTROL", "Cache-Control"},
		{"etag", "ETag"},
		{"te", "TE"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := icap.CanonicalHeaderKey(tt.input)
			if got != tt.want {
				t.Errorf("CanonicalHeaderKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestHeaderSetMultiple tests setting the same header multiple times.
func TestHeaderSetMultiple(t *testing.T) {
	h := make(icap.Header)
	h.Set("Content-Type", "text/plain")
	h.Set("Content-Type", "application/json") // Should replace

	val, exists := h.Get("Content-Type")
	if !exists {
		t.Error("Header.Set() failed to set header")
	}
	if val != "application/json" {
		t.Errorf("Header.Set() value = %q, want %q", val, "application/json")
	}
}

// TestHeaderAdd tests adding multiple values for the same header.
func TestHeaderAdd(t *testing.T) {
	h := make(icap.Header)
	h.Add("Accept", "text/html")
	h.Add("Accept", "application/json")
	h.Add("Accept", "application/xml")

	values := h.Values("Accept")
	if len(values) != 3 {
		t.Errorf("Header.Add() should allow multiple values, got %d", len(values))
	}
}

// TestHeaderLen tests getting the number of unique headers.
func TestHeaderLen(t *testing.T) {
	h := make(icap.Header)
	if h.Len() != 0 {
		t.Errorf("Empty header Len() = %d, want 0", h.Len())
	}

	h.Set("Host", "localhost")
	h.Set("Content-Type", "application/json")
	h.Add("Accept", "text/html")
	h.Add("Accept", "application/json") // Same header, different value

	// Len should count unique header names
	if h.Len() != 3 {
		t.Errorf("Header.Len() = %d, want 3", h.Len())
	}
}

// TestHeaderHas tests checking if a header exists.
func TestHeaderHas(t *testing.T) {
	h := make(icap.Header)
	h.Set("Content-Type", "application/json")

	if !h.Has("content-type") {
		t.Error("Header.Has() should return true for existing header (case-insensitive)")
	}

	if h.Has("X-Not-Exists") {
		t.Error("Header.Has() should return false for non-existing header")
	}
}

// BenchmarkHeaderWriteToBuffer benchmarks writing headers directly to a buffer.
func BenchmarkHeaderWriteToBuffer(b *testing.B) {
	h := make(icap.Header)
	h.Set("Host", "icap-server.net")
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Encapsulated", "req-hdr=0, req-body=100")
	h.Set("X-Custom-Header", "some-value")

	var buf bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		h.WriteToBuffer(&buf)
	}
}

// BenchmarkHeaderString benchmarks the String() method.
func BenchmarkHeaderString(b *testing.B) {
	h := make(icap.Header)
	h.Set("Host", "icap-server.net")
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Encapsulated", "req-hdr=0, req-body=100")
	h.Set("X-Custom-Header", "some-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.String()
	}
}

// BenchmarkCanonicalHeaderKey benchmarks header key canonicalization.
func BenchmarkCanonicalHeaderKey(b *testing.B) {
	b.Run("known", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			icap.CanonicalHeaderKey("content-type")
		}
	})
	b.Run("custom", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			icap.CanonicalHeaderKey("x-my-custom-header")
		}
	})
}
