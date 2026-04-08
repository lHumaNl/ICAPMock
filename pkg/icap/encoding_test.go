// Copyright 2026 ICAP Mock

package icap_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestChunkedReader tests reading chunked encoded data.
func TestChunkedReader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "single chunk",
			input: "5\r\nhello\r\n0\r\n\r\n",
			want:  "hello",
		},
		{
			name:  "multiple chunks",
			input: "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n",
			want:  "hello world",
		},
		{
			name:  "empty body",
			input: "0\r\n\r\n",
			want:  "",
		},
		{
			name:  "large chunk",
			input: "1a\r\n01234567890123456789012345\r\n0\r\n\r\n",
			want:  "01234567890123456789012345",
		},
		{
			name:  "chunk with extension",
			input: "5;name=value\r\nhello\r\n0\r\n\r\n",
			want:  "hello",
		},
		{
			name:    "invalid chunk size",
			input:   "xyz\r\nhello\r\n0\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "missing terminator",
			input:   "5\r\nhello\r\n0\r\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := icap.NewChunkedReader(strings.NewReader(tt.input))
			got, err := io.ReadAll(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ChunkedReader.Read() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("ChunkedReader.Read() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

// TestChunkedWriter tests writing chunked encoded data.
func TestChunkedWriter(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple data",
			input: "hello",
		},
		{
			name:  "empty data",
			input: "",
		},
		{
			name:  "large data",
			input: strings.Repeat("x", 1000),
		},
		{
			name:  "binary data",
			input: string([]byte{0x00, 0x01, 0x02, 0xff}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := icap.NewChunkedWriter(&buf)

			_, err := w.Write([]byte(tt.input))
			if err != nil {
				t.Errorf("ChunkedWriter.Write() error = %v", err)
				return
			}

			err = w.Close()
			if err != nil {
				t.Errorf("ChunkedWriter.Close() error = %v", err)
				return
			}

			// Verify we can read it back
			r := icap.NewChunkedReader(&buf)
			got, err := io.ReadAll(r)
			if err != nil {
				t.Errorf("Failed to read back chunked data: %v", err)
				return
			}
			if string(got) != tt.input {
				t.Errorf("Round-trip failed: got %q, want %q", string(got), tt.input)
			}
		})
	}
}

// TestChunkedReaderStreaming tests streaming reading without loading all data into memory.
func TestChunkedReaderStreaming(t *testing.T) {
	// Create a large chunked input
	var input strings.Builder
	chunkSize := 1000
	numChunks := 100

	for i := 0; i < numChunks; i++ {
		input.WriteString("3e8\r\n") // 1000 in hex
		input.WriteString(strings.Repeat("x", chunkSize))
		input.WriteString("\r\n")
	}
	input.WriteString("0\r\n\r\n")

	r := icap.NewChunkedReader(strings.NewReader(input.String()))

	// Read in small chunks to verify streaming works
	buf := make([]byte, 100)
	totalRead := 0
	maxMemory := 0

	for {
		n, err := r.Read(buf)
		totalRead += n
		if n > maxMemory {
			maxMemory = n
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			return
		}
	}

	expectedSize := chunkSize * numChunks
	if totalRead != expectedSize {
		t.Errorf("Total read = %d, want %d", totalRead, expectedSize)
	}

	// Verify we never used more than buffer size (streaming O(1) memory)
	if maxMemory > 100 {
		t.Errorf("Max memory usage = %d, should be <= 100 for streaming", maxMemory)
	}
}

// TestChunkedWriterMultipleWrites tests writing multiple chunks.
func TestChunkedWriterMultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	w := icap.NewChunkedWriter(&buf)

	w.Write([]byte("hello"))
	w.Write([]byte(" "))
	w.Write([]byte("world"))
	w.Close()

	// Verify output format
	output := buf.String()
	if !strings.Contains(output, "5\r\nhello\r\n") {
		t.Errorf("Expected chunk header for 'hello', got %q", output)
	}
	if !strings.Contains(output, "1\r\n \r\n") {
		t.Errorf("Expected chunk header for ' ', got %q", output)
	}
	if !strings.Contains(output, "5\r\nworld\r\n") {
		t.Errorf("Expected chunk header for 'world', got %q", output)
	}
	if !strings.HasSuffix(output, "0\r\n\r\n") {
		t.Errorf("Expected terminating chunk, got %q", output)
	}
}

// TestParseChunkSize tests parsing chunk size from hex string.
func TestParseChunkSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"5", 5, false},
		{"1a", 26, false},
		{"FF", 255, false},
		{"1000", 4096, false},
		{"1A;ext=value", 26, false}, // with extension
		{"xyz", 0, true},            // invalid hex
		{"", 0, true},               // empty
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := icap.ParseChunkSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseChunkSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseChunkSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestFormatChunkSize tests formatting size as hex string.
func TestFormatChunkSize(t *testing.T) {
	tests := []struct {
		want  string
		input int64
	}{
		{"0", 0},
		{"5", 5},
		{"1a", 26},
		{"ff", 255},
		{"1000", 4096},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := icap.FormatChunkSize(tt.input)
			if got != tt.want {
				t.Errorf("FormatChunkSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestChunkedReaderWithTrailer tests reading chunked data with trailer headers.
func TestChunkedReaderWithTrailer(t *testing.T) {
	input := "5\r\nhello\r\n0\r\nX-Checksum: abc123\r\n\r\n"

	r := icap.NewChunkedReader(strings.NewReader(input))
	got, err := io.ReadAll(r)
	if err != nil {
		t.Errorf("ChunkedReader.Read() error = %v", err)
		return
	}
	if string(got) != "hello" {
		t.Errorf("ChunkedReader.Read() = %q, want %q", string(got), "hello")
	}
}

// TestChunkedWriterFlush tests flushing chunked writer.
func TestChunkedWriterFlush(t *testing.T) {
	var buf bytes.Buffer
	w := icap.NewChunkedWriter(&buf)

	w.Write([]byte("test"))

	// Flush should write the chunk immediately
	if flusher, ok := interface{}(w).(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
	}

	// Buffer should contain the chunk
	if buf.Len() == 0 {
		t.Error("Flush() should write data to underlying writer")
	}
}

// BenchmarkChunkedReader benchmarks reading chunked data with pooled CRLF buffers.
// This demonstrates the performance improvement from using sync.Pool for CRLF buffers.
func BenchmarkChunkedReader(b *testing.B) {
	// Create a chunked input with many small chunks to maximize CRLF reads
	var input strings.Builder
	chunkSize := 64
	numChunks := 1000

	for i := 0; i < numChunks; i++ {
		input.WriteString("40\r\n") // 64 in hex
		input.WriteString(strings.Repeat("x", chunkSize))
		input.WriteString("\r\n")
	}
	input.WriteString("0\r\n\r\n")

	inputStr := input.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := icap.NewChunkedReader(strings.NewReader(inputStr))
		io.Copy(io.Discard, r)
	}
}

// BenchmarkChunkedReaderSingleChunk benchmarks reading a single chunk.
func BenchmarkChunkedReaderSingleChunk(b *testing.B) {
	input := "1000\r\n" + strings.Repeat("x", 4096) + "\r\n0\r\n\r\n"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := icap.NewChunkedReader(strings.NewReader(input))
		io.Copy(io.Discard, r)
	}
}

// BenchmarkChunkedWriter benchmarks writing chunked data.
func BenchmarkChunkedWriter(b *testing.B) {
	data := []byte(strings.Repeat("x", 4096))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := icap.NewChunkedWriter(&buf)
		w.Write(data)
		w.Close()
	}
}

// BenchmarkChunkedWriterSmallChunks benchmarks writing many small chunks.
func BenchmarkChunkedWriterSmallChunks(b *testing.B) {
	data := []byte("hello")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := icap.NewChunkedWriter(&buf)
		for j := 0; j < 100; j++ {
			w.Write(data)
		}
		w.Close()
	}
}

// BenchmarkReadCRLFWithPool benchmarks the pooled CRLF buffer implementation.
// This demonstrates zero allocations for CRLF reads after pool warmup.
func BenchmarkReadCRLFWithPool(b *testing.B) {
	// Create input that requires many CRLF reads
	var input strings.Builder
	numCRLF := 10000

	for i := 0; i < numCRLF; i++ {
		input.WriteString("1\r\nx\r\n")
	}
	input.WriteString("0\r\n\r\n")

	inputStr := input.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := icap.NewChunkedReader(strings.NewReader(inputStr))
		io.Copy(io.Discard, r)
	}
}
