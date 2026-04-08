// Copyright 2026 ICAP Mock

// Package pool provides object pooling for efficient resource reuse.
package pool

import (
	"bytes"
	"strings"
	"sync"
)

// Buffer sizes for different use cases.
const (
	// SizeSmall is 4KB, suitable for small messages and headers.
	SizeSmall = 4 * 1024
	// SizeMedium is 8KB, suitable for typical request/response bodies.
	SizeMedium = 8 * 1024
	// SizeLarge is 32KB, suitable for larger payloads.
	SizeLarge = 32 * 1024
)

// SlicePool provides pooled byte slices of various sizes.
// It maintains three internal pools for small (4KB), medium (8KB), and large (32KB) buffers.
// Callers should use Get() to obtain a buffer and Put() to return it when done.
//
// Thread-safe: Multiple goroutines can safely use the pool concurrently.
type SlicePool struct {
	small  *sync.Pool // 4KB buffers
	medium *sync.Pool // 8KB buffers
	large  *sync.Pool // 32KB buffers
}

// NewSlicePool creates a new SlicePool with three size tiers.
// Each tier is lazily populated as buffers are requested.
func NewSlicePool() *SlicePool {
	return &SlicePool{
		small: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, SizeSmall)
				return &buf
			},
		},
		medium: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, SizeMedium)
				return &buf
			},
		},
		large: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, SizeLarge)
				return &buf
			},
		},
	}
}

// Get returns a byte slice of at least the requested size.
// The returned slice should be returned to the pool using Put().
// If size is larger than SizeLarge, a new slice is allocated (not pooled).
//
// Important: Reset the slice (set length to 0) before use if needed.
// Example: buf := (*bufPtr)[:0].
func (p *SlicePool) Get(size int) *[]byte {
	var bufPtr interface{}

	switch {
	case size <= SizeSmall:
		bufPtr = p.small.Get()
	case size <= SizeMedium:
		bufPtr = p.medium.Get()
	case size <= SizeLarge:
		bufPtr = p.large.Get()
	default:
		// For sizes larger than our largest pool, allocate fresh
		buf := make([]byte, size)
		return &buf
	}

	return bufPtr.(*[]byte) //nolint:errcheck
}

// Put returns a byte slice to the appropriate pool.
// Do not use the slice after calling Put().
// The slice is reset to zero length before being returned to the pool.
//
// Important: Only put slices that were obtained from Get().
// Putting slices that were not from the pool may cause issues.
func (p *SlicePool) Put(buf *[]byte) {
	if buf == nil {
		return
	}

	// Reset slice to zero length but keep capacity
	*buf = (*buf)[:0]

	switch cap(*buf) {
	case SizeSmall:
		p.small.Put(buf)
	case SizeMedium:
		p.medium.Put(buf)
	case SizeLarge:
		p.large.Put(buf)
		// Silently drop buffers that don't match our pool sizes
		// This prevents pool pollution
	}
}

// GetSmall returns a 4KB buffer from the small pool.
// This is a convenience method for when you know you need a small buffer.
func (p *SlicePool) GetSmall() *[]byte {
	return p.Get(SizeSmall)
}

// GetMedium returns an 8KB buffer from the medium pool.
// This is a convenience method for when you know you need a medium buffer.
func (p *SlicePool) GetMedium() *[]byte {
	return p.Get(SizeMedium)
}

// GetLarge returns a 32KB buffer from the large pool.
// This is a convenience method for when you know you need a large buffer.
func (p *SlicePool) GetLarge() *[]byte {
	return p.Get(SizeLarge)
}

// BytesPool provides pooled bytes.Buffer objects.
// Use this for building response bodies or any scenario where you need
// a growable buffer with convenient write methods.
//
// Thread-safe: Multiple goroutines can safely use the pool concurrently.
type BytesPool struct {
	pool *sync.Pool
}

// NewBytesPool creates a new BytesPool.
// Buffers are created with an initial capacity of 4KB.
func NewBytesPool() *BytesPool {
	return &BytesPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, SizeSmall))
			},
		},
	}
}

// Get returns a bytes.Buffer from the pool.
// The buffer is reset to empty before being returned.
// Always call Put() when done with the buffer.
func (p *BytesPool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer) //nolint:errcheck
	buf.Reset()
	return buf
}

// Put returns a bytes.Buffer to the pool.
// Do not use the buffer after calling Put().
// The buffer is reset before being returned to the pool.
func (p *BytesPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	// Reset the buffer for reuse
	buf.Reset()

	// If buffer has grown too large, let it be garbage collected
	// This prevents memory bloat from oversized buffers staying in the pool
	if buf.Cap() > SizeLarge {
		return
	}

	p.pool.Put(buf)
}

// BuilderPool provides pooled strings.Builder objects.
// Use this for building strings efficiently, such as ICAP headers.
//
// Thread-safe: Multiple goroutines can safely use the pool concurrently.
type BuilderPool struct {
	pool *sync.Pool
}

// NewBuilderPool creates a new BuilderPool.
// Builders are created with an initial capacity of 1KB.
func NewBuilderPool() *BuilderPool {
	return &BuilderPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return new(strings.Builder)
			},
		},
	}
}

// Get returns a strings.Builder from the pool.
// The builder is reset to empty before being returned.
// Always call Put() when done with the builder.
func (p *BuilderPool) Get() *strings.Builder {
	builder := p.pool.Get().(*strings.Builder) //nolint:errcheck
	builder.Reset()
	return builder
}

// Put returns a strings.Builder to the pool.
// Do not use the builder after calling Put().
// The builder is reset before being returned to the pool.
func (p *BuilderPool) Put(builder *strings.Builder) {
	if builder == nil {
		return
	}

	// Reset the builder for reuse
	builder.Reset()

	// If builder has grown too large, let it be garbage collected
	// This prevents memory bloat from oversized builders staying in the pool
	if builder.Cap() > SizeLarge {
		return
	}

	p.pool.Put(builder)
}

// ResponsePool provides pooled bytes.Buffer objects optimized for ICAP responses.
// This pool uses a larger initial capacity (8KB) to accommodate typical ICAP response sizes
// including headers and encapsulated HTTP messages.
//
// Thread-safe: Multiple goroutines can safely use the pool concurrently.
type ResponsePool struct {
	pool *sync.Pool
}

// NewResponsePool creates a new ResponsePool.
// Buffers are created with an initial capacity of 8KB (SizeMedium).
func NewResponsePool() *ResponsePool {
	return &ResponsePool{
		pool: &sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, SizeMedium))
			},
		},
	}
}

// Get returns a bytes.Buffer from the pool.
// The buffer is reset to empty before being returned.
// Always call Put() when done with the buffer.
func (p *ResponsePool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer) //nolint:errcheck
	buf.Reset()
	return buf
}

// Put returns a bytes.Buffer to the pool.
// Do not use the buffer after calling Put().
// The buffer is reset before being returned to the pool.
// Buffers that have grown beyond 64KB are discarded to prevent memory bloat.
func (p *ResponsePool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	// Reset the buffer for reuse
	buf.Reset()

	// If buffer has grown too large (>64KB), let it be garbage collected
	// This prevents memory bloat from oversized buffers staying in the pool
	// We use 64KB here (2x SizeLarge) since responses can be larger
	if buf.Cap() > 2*SizeLarge {
		return
	}

	p.pool.Put(buf)
}

// HeaderMapMaxSize is the maximum number of header keys allowed in a pooled map.
// Maps larger than this are discarded to prevent memory bloat.
const HeaderMapMaxSize = 64

// HeaderPool provides pooled map[string][]string objects for HTTP/ICAP headers.
// This reduces allocations when parsing requests and responses with many headers.
//
// Thread-safe: Multiple goroutines can safely use the pool concurrently.
type HeaderPool struct {
	pool *sync.Pool
}

// NewHeaderPool creates a new HeaderPool.
// Maps are created with an initial capacity of 16 entries.
func NewHeaderPool() *HeaderPool {
	return &HeaderPool{
		pool: &sync.Pool{
			New: func() interface{} {
				// Pre-allocate with typical header count
				hdr := make(map[string][]string, 16)
				return &hdr
			},
		},
	}
}

// Get returns a header map from the pool.
// The map is cleared before being returned.
// Always call Put() when done with the map.
func (p *HeaderPool) Get() *map[string][]string { //nolint:gocritic // ptrToRefParam: pointer to map needed for sync.Pool
	hdr := p.pool.Get().(*map[string][]string) //nolint:errcheck
	// Clear the map for reuse
	for k := range *hdr {
		delete(*hdr, k)
	}
	return hdr
}

// Put returns a header map to the pool.
// Do not use the map after calling Put().
// The map is cleared before being returned to the pool.
// Maps that have grown beyond HeaderMapMaxSize keys are discarded to prevent memory bloat.
func (p *HeaderPool) Put(hdr *map[string][]string) { //nolint:gocritic // ptrToRefParam: pointer to map needed for sync.Pool
	if hdr == nil {
		return
	}

	// If map has grown too large, let it be garbage collected
	// This prevents memory bloat from oversized maps staying in the pool
	if len(*hdr) > HeaderMapMaxSize {
		return
	}

	// Clear the map for reuse
	for k := range *hdr {
		delete(*hdr, k)
	}

	p.pool.Put(hdr)
}

// Global pools initialized at startup.
// These are the primary pools used throughout the application.
var (
	// BufferPool is the global byte slice pool for raw byte buffers.
	BufferPool = NewSlicePool()
	// BytesBufferPool is the global bytes.Buffer pool for building byte sequences.
	BytesBufferPool = NewBytesPool()
	// StringBuilderPool is the global strings.Builder pool for building strings.
	StringBuilderPool = NewBuilderPool()
	// ResponseBufferPool is the global bytes.Buffer pool for ICAP response serialization.
	ResponseBufferPool = NewResponsePool()
	// HeaderMapPool is the global pool for map[string][]string (HTTP/ICAP headers).
	HeaderMapPool = NewHeaderPool()
)
