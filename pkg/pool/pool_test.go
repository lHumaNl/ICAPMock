// Copyright 2026 ICAP Mock

package pool

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestBufferPoolGetPut tests basic Get and Put operations for SlicePool.
func TestBufferPoolGetPut(t *testing.T) {
	tests := []struct {
		name        string
		description string
		size        int
		expectCap   int
	}{
		{
			name:        "small buffer",
			size:        100,
			expectCap:   SizeSmall,
			description: "requesting 100 bytes should return small pool buffer",
		},
		{
			name:        "small buffer at boundary",
			size:        SizeSmall,
			expectCap:   SizeSmall,
			description: "requesting exactly 4KB should return small pool buffer",
		},
		{
			name:        "medium buffer",
			size:        SizeSmall + 1,
			expectCap:   SizeMedium,
			description: "requesting >4KB should return medium pool buffer",
		},
		{
			name:        "medium buffer at boundary",
			size:        SizeMedium,
			expectCap:   SizeMedium,
			description: "requesting exactly 8KB should return medium pool buffer",
		},
		{
			name:        "large buffer",
			size:        SizeMedium + 1,
			expectCap:   SizeLarge,
			description: "requesting >8KB should return large pool buffer",
		},
		{
			name:        "large buffer at boundary",
			size:        SizeLarge,
			expectCap:   SizeLarge,
			description: "requesting exactly 32KB should return large pool buffer",
		},
		{
			name:        "oversized buffer",
			size:        SizeLarge + 1,
			expectCap:   SizeLarge + 1,
			description: "requesting >32KB should allocate new buffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSlicePool()
			buf := p.Get(tt.size)

			if buf == nil {
				t.Fatal("Get returned nil buffer")
			}

			if cap(*buf) < tt.expectCap {
				t.Errorf("buffer capacity = %d, want at least %d", cap(*buf), tt.expectCap)
			}

			// Should be able to put back without panic
			p.Put(buf)
		})
	}
}

// TestBufferPoolReset tests that buffers are reset when put back.
func TestBufferPoolReset(t *testing.T) {
	p := NewSlicePool()

	// Get a buffer and write to it
	buf := p.Get(SizeSmall)
	*buf = append(*buf, make([]byte, 100)...)

	if len(*buf) != 100 {
		t.Errorf("buffer length = %d, want 100", len(*buf))
	}

	// Put it back
	p.Put(buf)

	// Get again - should be reset
	buf2 := p.Get(SizeSmall)
	if len(*buf2) != 0 {
		t.Errorf("buffer length after reset = %d, want 0", len(*buf2))
	}
}

// TestBufferPoolConvenienceMethods tests GetSmall, GetMedium, GetLarge.
func TestBufferPoolConvenienceMethods(t *testing.T) {
	p := NewSlicePool()

	small := p.GetSmall()
	if cap(*small) != SizeSmall {
		t.Errorf("GetSmall capacity = %d, want %d", cap(*small), SizeSmall)
	}

	medium := p.GetMedium()
	if cap(*medium) != SizeMedium {
		t.Errorf("GetMedium capacity = %d, want %d", cap(*medium), SizeMedium)
	}

	large := p.GetLarge()
	if cap(*large) != SizeLarge {
		t.Errorf("GetLarge capacity = %d, want %d", cap(*large), SizeLarge)
	}

	p.Put(small)
	p.Put(medium)
	p.Put(large)
}

// TestBufferPoolNilPut tests that Put(nil) doesn't panic.
func TestBufferPoolNilPut(t *testing.T) {
	p := NewSlicePool()
	p.Put(nil) // Should not panic
}

// TestBytesPoolGetPut tests basic Get and Put for BytesPool.
func TestBytesPoolGetPut(t *testing.T) {
	p := NewBytesPool()

	buf := p.Get()
	if buf == nil {
		t.Fatal("Get returned nil buffer")
	}

	// Write some data
	data := []byte("test data")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
	if buf.Len() != len(data) {
		t.Errorf("buffer length = %d, want %d", buf.Len(), len(data))
	}

	// Put and get again - should be reset
	p.Put(buf)
	buf2 := p.Get()
	if buf2.Len() != 0 {
		t.Errorf("buffer length after reset = %d, want 0", buf2.Len())
	}

	p.Put(buf2)
}

// TestBytesPoolNilPut tests that Put(nil) doesn't panic.
func TestBytesPoolNilPut(t *testing.T) {
	p := NewBytesPool()
	p.Put(nil) // Should not panic
}

// TestBytesPoolLargeBuffer tests that oversized buffers are discarded.
func TestBytesPoolLargeBuffer(t *testing.T) {
	p := NewBytesPool()

	buf := p.Get()

	// Grow the buffer beyond SizeLarge
	largeData := make([]byte, SizeLarge+1)
	buf.Write(largeData)

	// Put should not return this to the pool
	p.Put(buf)

	// Get a new buffer - should not be the oversized one
	buf2 := p.Get()
	if buf2.Cap() > SizeLarge {
		t.Errorf("got oversized buffer with capacity %d", buf2.Cap())
	}

	p.Put(buf2)
}

// TestBuilderPoolGetPut tests basic Get and Put for BuilderPool.
func TestBuilderPoolGetPut(t *testing.T) {
	p := NewBuilderPool()

	builder := p.Get()
	if builder == nil {
		t.Fatal("Get returned nil builder")
	}

	// Write some data
	builder.WriteString("test string")
	if builder.Len() != 11 {
		t.Errorf("builder length = %d, want 11", builder.Len())
	}

	// Put and get again - should be reset
	p.Put(builder)
	builder2 := p.Get()
	if builder2.Len() != 0 {
		t.Errorf("builder length after reset = %d, want 0", builder2.Len())
	}

	p.Put(builder2)
}

// TestBuilderPoolNilPut tests that Put(nil) doesn't panic.
func TestBuilderPoolNilPut(t *testing.T) {
	p := NewBuilderPool()
	p.Put(nil) // Should not panic
}

// TestBuilderPoolLargeBuilder tests that oversized builders are discarded.
func TestBuilderPoolLargeBuilder(t *testing.T) {
	p := NewBuilderPool()

	builder := p.Get()

	// Grow the builder beyond SizeLarge
	largeData := string(make([]byte, SizeLarge+1))
	builder.WriteString(largeData)

	// Put should not return this to the pool
	p.Put(builder)

	// Get a new builder - should not be the oversized one
	builder2 := p.Get()
	if builder2.Cap() > SizeLarge {
		t.Errorf("got oversized builder with capacity %d", builder2.Cap())
	}

	p.Put(builder2)
}

// TestResponsePoolGetPut tests basic Get and Put for ResponsePool.
func TestResponsePoolGetPut(t *testing.T) {
	p := NewResponsePool()

	buf := p.Get()
	if buf == nil {
		t.Fatal("Get returned nil buffer")
	}

	// Write some data
	data := []byte("ICAP/1.0 200 OK\r\n\r\n")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}
	if buf.Len() != len(data) {
		t.Errorf("buffer length = %d, want %d", buf.Len(), len(data))
	}

	// Put and get again - should be reset
	p.Put(buf)
	buf2 := p.Get()
	if buf2.Len() != 0 {
		t.Errorf("buffer length after reset = %d, want 0", buf2.Len())
	}

	p.Put(buf2)
}

// TestResponsePoolNilPut tests that Put(nil) doesn't panic.
func TestResponsePoolNilPut(t *testing.T) {
	p := NewResponsePool()
	p.Put(nil) // Should not panic
}

// TestResponsePoolLargeBuffer tests that oversized buffers are discarded.
func TestResponsePoolLargeBuffer(t *testing.T) {
	p := NewResponsePool()

	buf := p.Get()

	// Grow the buffer beyond 2*SizeLarge (64KB)
	largeData := make([]byte, 2*SizeLarge+1)
	buf.Write(largeData)

	// Put should not return this to the pool
	p.Put(buf)

	// Get a new buffer - should not be the oversized one
	buf2 := p.Get()
	if buf2.Cap() > 2*SizeLarge {
		t.Errorf("got oversized buffer with capacity %d", buf2.Cap())
	}

	p.Put(buf2)
}

// TestResponsePoolInitialCapacity tests that buffers have correct initial capacity.
func TestResponsePoolInitialCapacity(t *testing.T) {
	p := NewResponsePool()

	buf := p.Get()
	if buf.Cap() < SizeMedium {
		t.Errorf("buffer capacity = %d, want at least %d", buf.Cap(), SizeMedium)
	}

	p.Put(buf)
}

// TestGlobalResponseBufferPool tests that the global ResponseBufferPool is initialized.
func TestGlobalResponseBufferPool(t *testing.T) {
	if ResponseBufferPool == nil {
		t.Error("global ResponseBufferPool is nil")
	}

	// Test that it works
	buf := ResponseBufferPool.Get()
	if buf == nil {
		t.Error("ResponseBufferPool.Get() returned nil")
	}
	ResponseBufferPool.Put(buf)
}

// TestHeaderPoolGetPut tests basic Get and Put for HeaderPool.
func TestHeaderPoolGetPut(t *testing.T) {
	p := NewHeaderPool()

	hdr := p.Get()
	if hdr == nil {
		t.Fatal("Get returned nil header map")
	}

	// Add some headers
	(*hdr)["Host"] = []string{"example.com"}
	(*hdr)["Content-Type"] = []string{"text/html", "charset=utf-8"}
	(*hdr)["Content-Length"] = []string{"1234"}

	if len(*hdr) != 3 {
		t.Errorf("header count = %d, want 3", len(*hdr))
	}

	// Verify values
	if v := (*hdr)["Host"]; len(v) != 1 || v[0] != "example.com" {
		t.Errorf("Host header = %v, want [example.com]", v)
	}

	// Put and get again - should be reset
	p.Put(hdr)
	hdr2 := p.Get()
	if len(*hdr2) != 0 {
		t.Errorf("header count after reset = %d, want 0", len(*hdr2))
	}

	p.Put(hdr2)
}

// TestHeaderPoolNilPut tests that Put(nil) doesn't panic.
func TestHeaderPoolNilPut(t *testing.T) {
	p := NewHeaderPool()
	p.Put(nil) // Should not panic
}

// TestHeaderPoolLargeMap tests that oversized maps are discarded.
func TestHeaderPoolLargeMap(t *testing.T) {
	p := NewHeaderPool()

	hdr := p.Get()

	// Add many headers to exceed HeaderMapMaxSize
	for i := 0; i < HeaderMapMaxSize+1; i++ {
		(*hdr)[fmt.Sprintf("X-Header-%d", i)] = []string{"value"}
	}

	if len(*hdr) <= HeaderMapMaxSize {
		t.Errorf("header count = %d, want more than %d", len(*hdr), HeaderMapMaxSize)
	}

	// Put should not return this to the pool
	p.Put(hdr)

	// Get a new map - should not be the oversized one
	hdr2 := p.Get()
	if len(*hdr2) > HeaderMapMaxSize {
		t.Errorf("got oversized map with %d keys", len(*hdr2))
	}

	p.Put(hdr2)
}

// TestHeaderPoolReset tests that maps are properly reset when returned.
func TestHeaderPoolReset(t *testing.T) {
	p := NewHeaderPool()

	// Get a map and add headers
	hdr := p.Get()
	(*hdr)["Host"] = []string{"example.com"}
	(*hdr)["User-Agent"] = []string{"test"}

	if len(*hdr) != 2 {
		t.Errorf("header count = %d, want 2", len(*hdr))
	}

	// Put it back
	p.Put(hdr)

	// Get again - should be empty
	hdr2 := p.Get()
	if len(*hdr2) != 0 {
		t.Errorf("header count after reset = %d, want 0", len(*hdr2))
	}

	// Verify we can add new headers
	(*hdr2)["New-Header"] = []string{"new-value"}
	if len(*hdr2) != 1 {
		t.Errorf("header count = %d, want 1", len(*hdr2))
	}

	p.Put(hdr2)
}

// TestConcurrentHeaderPool tests concurrent access to HeaderPool.
func TestConcurrentHeaderPool(t *testing.T) {
	p := NewHeaderPool()
	var wg sync.WaitGroup

	// Run many goroutines getting and putting maps
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				hdr := p.Get()
				(*hdr)["Test-Header"] = []string{fmt.Sprintf("value-%d", j)}
				p.Put(hdr)
			}
		}()
	}

	wg.Wait()
}

// TestGlobalHeaderMapPool tests that the global HeaderMapPool is initialized.
func TestGlobalHeaderMapPool(t *testing.T) {
	if HeaderMapPool == nil {
		t.Error("global HeaderMapPool is nil")
	}

	// Test that it works
	hdr := HeaderMapPool.Get()
	if hdr == nil {
		t.Error("HeaderMapPool.Get() returned nil")
	}
	HeaderMapPool.Put(hdr)
}

// TestGlobalPools tests that global pools are initialized.
func TestGlobalPools(t *testing.T) {
	if BufferPool == nil {
		t.Error("global BufferPool is nil")
	}
	if BytesBufferPool == nil {
		t.Error("global BytesBufferPool is nil")
	}
	if StringBuilderPool == nil {
		t.Error("global StringBuilderPool is nil")
	}
	if ResponseBufferPool == nil {
		t.Error("global ResponseBufferPool is nil")
	}

	// Test that they work
	buf := BufferPool.Get(SizeSmall)
	BufferPool.Put(buf)

	bb := BytesBufferPool.Get()
	BytesBufferPool.Put(bb)

	sb := StringBuilderPool.Get()
	StringBuilderPool.Put(sb)

	rb := ResponseBufferPool.Get()
	ResponseBufferPool.Put(rb)
}

// TestConcurrentBufferPool tests concurrent access to SlicePool.
func TestConcurrentBufferPool(t *testing.T) {
	p := NewSlicePool()
	var wg sync.WaitGroup

	// Run many goroutines getting and putting buffers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf := p.Get(SizeMedium)
				*buf = append(*buf, make([]byte, 100)...)
				p.Put(buf)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentBytesPool tests concurrent access to BytesPool.
func TestConcurrentBytesPool(t *testing.T) {
	p := NewBytesPool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf := p.Get()
				buf.Write([]byte("test data"))
				p.Put(buf)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentBuilderPool tests concurrent access to BuilderPool.
func TestConcurrentBuilderPool(t *testing.T) {
	p := NewBuilderPool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				builder := p.Get()
				builder.WriteString("test string")
				p.Put(builder)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentResponsePool tests concurrent access to ResponsePool.
func TestConcurrentResponsePool(t *testing.T) {
	p := NewResponsePool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf := p.Get()
				buf.WriteString("ICAP/1.0 200 OK\r\n\r\n")
				p.Put(buf)
			}
		}()
	}

	wg.Wait()
}

// Benchmarks

// BenchmarkBufferPoolNew benchmarks creating new byte slices without pooling.
func BenchmarkBufferPoolNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := make([]byte, SizeMedium)
		_ = buf
	}
}

// BenchmarkBufferPoolGetPut benchmarks getting and putting from the pool.
func BenchmarkBufferPoolGetPut(b *testing.B) {
	p := NewSlicePool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get(SizeMedium)
		p.Put(buf)
	}
}

// BenchmarkBufferPoolGetOnly benchmarks only getting from the pool.
func BenchmarkBufferPoolGetOnly(b *testing.B) {
	p := NewSlicePool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get(SizeMedium)
		_ = buf
	}
}

// BenchmarkBytesBufferNew benchmarks creating new bytes.Buffer without pooling.
func BenchmarkBytesBufferNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, SizeSmall))
		_ = buf
	}
}

// BenchmarkBytesPoolGetPut benchmarks getting and putting from the pool.
func BenchmarkBytesPoolGetPut(b *testing.B) {
	p := NewBytesPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get()
		p.Put(buf)
	}
}

// BenchmarkBytesPoolWithWrite benchmarks pool with actual writes.
func BenchmarkBytesPoolWithWrite(b *testing.B) {
	p := NewBytesPool()
	data := make([]byte, 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get()
		buf.Write(data)
		p.Put(buf)
	}
}

// BenchmarkBytesBufferNewWithWrite benchmarks new buffer with writes.
func BenchmarkBytesBufferNewWithWrite(b *testing.B) {
	data := make([]byte, 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, SizeSmall))
		buf.Write(data)
	}
}

// BenchmarkStringBuilderNew benchmarks creating new strings.Builder without pooling.
func BenchmarkStringBuilderNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		builder := new(strings.Builder)
		_ = builder
	}
}

// BenchmarkBuilderPoolGetPut benchmarks getting and putting from the pool.
func BenchmarkBuilderPoolGetPut(b *testing.B) {
	p := NewBuilderPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		builder := p.Get()
		p.Put(builder)
	}
}

// BenchmarkBuilderPoolWithWrite benchmarks pool with actual writes.
func BenchmarkBuilderPoolWithWrite(b *testing.B) {
	p := NewBuilderPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		builder := p.Get()
		builder.WriteString("ICAP/1.0 200 OK\r\n")
		p.Put(builder)
	}
}

// BenchmarkStringBuilderNewWithWrite benchmarks new builder with writes.
func BenchmarkStringBuilderNewWithWrite(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		builder := new(strings.Builder)
		builder.WriteString("ICAP/1.0 200 OK\r\n")
	}
}

// BenchmarkBufferPoolParallel benchmarks parallel access to SlicePool.
func BenchmarkBufferPoolParallel(b *testing.B) {
	p := NewSlicePool()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := p.Get(SizeMedium)
			*buf = append(*buf, make([]byte, 100)...)
			p.Put(buf)
		}
	})
}

// BenchmarkBytesPoolParallel benchmarks parallel access to BytesPool.
func BenchmarkBytesPoolParallel(b *testing.B) {
	p := NewBytesPool()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := p.Get()
			buf.Write([]byte("test"))
			p.Put(buf)
		}
	})
}

// BenchmarkBuilderPoolParallel benchmarks parallel access to BuilderPool.
func BenchmarkBuilderPoolParallel(b *testing.B) {
	p := NewBuilderPool()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			builder := p.Get()
			builder.WriteString("test")
			p.Put(builder)
		}
	})
}

// BenchmarkResponsePoolNew benchmarks creating new bytes.Buffer without pooling (8KB initial capacity).
func BenchmarkResponsePoolNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, SizeMedium))
		_ = buf
	}
}

// BenchmarkResponsePoolGetPut benchmarks getting and putting from ResponsePool.
func BenchmarkResponsePoolGetPut(b *testing.B) {
	p := NewResponsePool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get()
		p.Put(buf)
	}
}

// BenchmarkResponsePoolWithWrite benchmarks ResponsePool with typical ICAP response writes.
func BenchmarkResponsePoolWithWrite(b *testing.B) {
	p := NewResponsePool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get()
		buf.WriteString("ICAP/1.0 200 OK\r\n")
		buf.WriteString("ISTag: test-tag\r\n")
		buf.WriteString("Connection: keep-alive\r\n")
		buf.WriteString("\r\n")
		p.Put(buf)
	}
}

// BenchmarkResponseBufferNewWithWrite benchmarks new buffer with ICAP response writes.
func BenchmarkResponseBufferNewWithWrite(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := bytes.NewBuffer(make([]byte, 0, SizeMedium))
		buf.WriteString("ICAP/1.0 200 OK\r\n")
		buf.WriteString("ISTag: test-tag\r\n")
		buf.WriteString("Connection: keep-alive\r\n")
		buf.WriteString("\r\n")
	}
}

// BenchmarkResponsePoolParallel benchmarks parallel access to ResponsePool.
func BenchmarkResponsePoolParallel(b *testing.B) {
	p := NewResponsePool()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := p.Get()
			buf.WriteString("ICAP/1.0 200 OK\r\n\r\n")
			p.Put(buf)
		}
	})
}

// BenchmarkMixedWorkload simulates a realistic ICAP request handling scenario.
func BenchmarkMixedWorkload(b *testing.B) {
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Simulate reading request into buffer
			reqBuf := BufferPool.GetMedium()
			*reqBuf = append(*reqBuf, make([]byte, 2048)...)

			// Simulate building response headers
			headers := StringBuilderPool.Get()
			headers.WriteString("ICAP/1.0 200 OK\r\n")
			headers.WriteString("Connection: keep-alive\r\n")
			headers.WriteString("\r\n")

			// Simulate building response body
			respBuf := BytesBufferPool.Get()
			respBuf.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

			// Return everything to pools
			BufferPool.Put(reqBuf)
			StringBuilderPool.Put(headers)
			BytesBufferPool.Put(respBuf)
		}
	})
}

// BenchmarkMixedWorkloadNoPool simulates the same scenario without pools.
func BenchmarkMixedWorkloadNoPool(b *testing.B) {
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Allocate fresh buffers
			reqBuf := make([]byte, 2048)

			// Build headers
			headers := new(strings.Builder)
			headers.WriteString("ICAP/1.0 200 OK\r\n")
			headers.WriteString("Connection: keep-alive\r\n")
			headers.WriteString("\r\n")

			// Build response body
			respBuf := bytes.NewBuffer(make([]byte, 0, SizeSmall))
			respBuf.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

			// Let GC clean up
			_ = reqBuf
			_ = headers
			_ = respBuf
		}
	})
}

// BenchmarkHeaderMapNew benchmarks creating new header maps without pooling.
func BenchmarkHeaderMapNew(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hdr := make(map[string][]string, 16)
		hdr["Host"] = []string{"example.com"}
		hdr["Content-Type"] = []string{"text/html"}
		hdr["Content-Length"] = []string{"1234"}
		_ = hdr
	}
}

// BenchmarkHeaderPoolGetPut benchmarks getting and putting from the HeaderPool.
func BenchmarkHeaderPoolGetPut(b *testing.B) {
	p := NewHeaderPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hdr := p.Get()
		(*hdr)["Host"] = []string{"example.com"}
		(*hdr)["Content-Type"] = []string{"text/html"}
		(*hdr)["Content-Length"] = []string{"1234"}
		p.Put(hdr)
	}
}

// BenchmarkHeaderPoolWithHeaders benchmarks pool with typical header operations.
func BenchmarkHeaderPoolWithHeaders(b *testing.B) {
	p := NewHeaderPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hdr := p.Get()
		// Simulate typical ICAP headers
		(*hdr)["Host"] = []string{"icap-server:1344"}
		(*hdr)["Encapsulated"] = []string{"req-hdr=0, req-body=412"}
		(*hdr)["ISTag"] = []string{"\"W3E4R7U9-L2H4\""}
		(*hdr)["X-Client-IP"] = []string{"192.168.1.100"}
		// HTTP headers
		(*hdr)["Content-Type"] = []string{"text/html"}
		(*hdr)["Content-Length"] = []string{"1234"}
		(*hdr)["User-Agent"] = []string{"Mozilla/5.0"}
		p.Put(hdr)
	}
}

// BenchmarkHeaderPoolParallel benchmarks parallel access to HeaderPool.
func BenchmarkHeaderPoolParallel(b *testing.B) {
	p := NewHeaderPool()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hdr := p.Get()
			(*hdr)["Host"] = []string{"example.com"}
			(*hdr)["Content-Type"] = []string{"text/html"}
			p.Put(hdr)
		}
	})
}

// BenchmarkHeaderPoolClear benchmarks the clear operation.
func BenchmarkHeaderPoolClear(b *testing.B) {
	p := NewHeaderPool()
	hdr := p.Get()
	// Fill with headers
	for i := 0; i < 20; i++ {
		(*hdr)[fmt.Sprintf("X-Header-%d", i)] = []string{"value"}
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Clear the map
		for k := range *hdr {
			delete(*hdr, k)
		}
		// Re-fill (simulating reuse)
		for j := 0; j < 20; j++ {
			(*hdr)[fmt.Sprintf("X-Header-%d", j)] = []string{"value"}
		}
	}
	p.Put(hdr)
}
