// Copyright 2026 ICAP Mock

package icap

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Chunked encoding constants.
const (
	// MaxChunkSize limits the maximum size of a single chunk.
	MaxChunkSize = 1 << 20 // 1MB

	// ChunkBufferSize is the default buffer size for chunked reading.
	ChunkBufferSize = 4096
)

// crlfBytes is a pre-allocated CRLF byte slice to avoid repeated []byte("\r\n") conversions.
var crlfBytes = []byte("\r\n")

// crlfPool is a sync.Pool for reusing 2-byte buffers when reading CRLF terminators.
// This reduces GC pressure during high-throughput chunked encoding parsing.
var crlfPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 2)
		return &b
	},
}

// ChunkedReader implements io.Reader for chunked transfer encoding.
// It reads chunked data and provides O(1) memory usage for streaming.
type ChunkedReader struct {
	err      error
	r        *bufio.Reader
	n        int64
	finished bool
}

// NewChunkedReader creates a new ChunkedReader that reads from r.
// If r is already a *bufio.Reader with sufficient buffer size, it is reused
// to avoid double buffering.
func NewChunkedReader(r io.Reader) *ChunkedReader {
	br, ok := r.(*bufio.Reader)
	if !ok || br.Size() < ChunkBufferSize {
		br = bufio.NewReaderSize(r, ChunkBufferSize)
	}
	return &ChunkedReader{r: br}
}

// Read implements io.Reader. It reads from the chunked stream.
func (cr *ChunkedReader) Read(p []byte) (n int, err error) {
	if cr.err != nil {
		return 0, cr.err
	}

	if cr.finished {
		return 0, io.EOF
	}

	// Need to read next chunk header
	if cr.n == 0 {
		cr.n, cr.err = cr.readChunkHeader()
		if cr.err != nil {
			return 0, cr.err
		}
		if cr.n == 0 {
			cr.finished = true
			if err := cr.readTrailer(); err != nil {
				cr.err = err
				return 0, err
			}
			return 0, io.EOF
		}
	}

	// Read from current chunk
	toRead := int64(len(p))
	if toRead > cr.n {
		toRead = cr.n
	}

	n, err = io.ReadFull(cr.r, p[:toRead])
	cr.n -= int64(n)

	// Read trailing \r\n after chunk data
	if cr.n == 0 && err == nil {
		if e := cr.readCRLF(); e != nil {
			cr.err = e // Store error so subsequent reads fail
			return n, e
		}
	}

	return n, err
}

// readChunkHeader reads and parses a chunk header line.
func (cr *ChunkedReader) readChunkHeader() (int64, error) {
	line, err := cr.r.ReadString('\n')
	if err != nil {
		return 0, err
	}

	// Remove trailing \r\n
	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")

	// Parse chunk size (may include extensions)
	parts := strings.SplitN(line, ";", 2)
	size, err := ParseChunkSize(parts[0])
	if err != nil {
		return 0, err
	}

	if size > MaxChunkSize {
		return 0, errors.New("chunk size exceeds maximum")
	}

	return size, nil
}

// readCRLF reads the trailing \r\n after chunk data.
func (cr *ChunkedReader) readCRLF() error {
	bp := crlfPool.Get().(*[]byte) //nolint:errcheck
	defer crlfPool.Put(bp)
	b := *bp

	_, err := io.ReadFull(cr.r, b)
	if err != nil {
		return err
	}
	if b[0] != '\r' || b[1] != '\n' {
		return errors.New("malformed chunk terminator")
	}
	return nil
}

// readTrailer reads any trailer headers after the final chunk.
func (cr *ChunkedReader) readTrailer() error {
	for {
		line, err := cr.r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}
		// Empty line signals end of trailer
		if line == "\r\n" || line == "\n" {
			return nil
		}
		// Skip trailer headers for now
	}
}

// ChunkedWriter implements io.WriteCloser for chunked transfer encoding.
type ChunkedWriter struct {
	w       io.Writer
	flusher interface{ Flush() error }
	buf     *bytes.Buffer
	closed  bool
}

// NewChunkedWriter creates a new ChunkedWriter that writes to w.
func NewChunkedWriter(w io.Writer) *ChunkedWriter {
	cw := &ChunkedWriter{
		w:   w,
		buf: bytes.NewBuffer(nil),
	}
	if f, ok := w.(interface{ Flush() error }); ok {
		cw.flusher = f
	}
	return cw
}

// Write implements io.Writer. Data is buffered and written as chunks.
func (cw *ChunkedWriter) Write(p []byte) (n int, err error) {
	if cw.closed {
		return 0, errors.New("write on closed ChunkedWriter")
	}

	// Write each write as a separate chunk for streaming
	if len(p) == 0 {
		return 0, nil
	}

	// Format: <size>\r\n<data>\r\n
	var hdr [20]byte // enough for any hex int64 + \r\n
	hdrSlice := strconv.AppendInt(hdr[:0], int64(len(p)), 16)
	hdrSlice = append(hdrSlice, '\r', '\n')
	if _, err := cw.w.Write(hdrSlice); err != nil {
		return 0, err
	}
	if n, err := cw.w.Write(p); err != nil {
		return n, err
	}
	if _, err := cw.w.Write(crlfBytes); err != nil {
		return 0, err
	}

	return len(p), nil
}

// Flush flushes any buffered data to the underlying writer.
func (cw *ChunkedWriter) Flush() error {
	if cw.flusher != nil {
		return cw.flusher.Flush()
	}
	return nil
}

// Close writes the terminating chunk and closes the writer.
func (cw *ChunkedWriter) Close() error {
	if cw.closed {
		return nil
	}
	cw.closed = true

	// Write terminating chunk
	_, err := cw.w.Write([]byte("0\r\n\r\n"))
	return err
}

// ParseChunkSize parses a hexadecimal chunk size string.
// It handles extensions after the size (e.g., "1a;name=value").
func ParseChunkSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty chunk size")
	}

	// Handle extensions
	if idx := strings.IndexByte(s, ';'); idx >= 0 {
		s = s[:idx]
	}

	// Parse hex
	size, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, errors.New("invalid chunk size: " + s)
	}

	if size < 0 {
		return 0, errors.New("invalid chunk size: negative value")
	}

	return size, nil
}

// FormatChunkSize formats a size as a lowercase hexadecimal string.
func FormatChunkSize(n int64) string {
	return strconv.FormatInt(n, 16)
}

// ReadChunkedBody reads a complete chunked body and returns it as bytes.
// This loads the entire body into memory; for streaming use ChunkedReader.
func ReadChunkedBody(r io.Reader) ([]byte, error) {
	cr := NewChunkedReader(r)
	return io.ReadAll(cr)
}

// WriteChunkedBody writes data using chunked encoding.
// For streaming, use ChunkedWriter directly.
func WriteChunkedBody(w io.Writer, data []byte) error {
	cw := NewChunkedWriter(w)
	if _, err := cw.Write(data); err != nil {
		return err
	}
	return cw.Close()
}
