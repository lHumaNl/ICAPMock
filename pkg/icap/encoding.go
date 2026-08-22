// Copyright 2026 ICAP Mock

package icap

import (
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

	// MaxChunkHeaderLength limits a chunk-size line, including the line terminator.
	MaxChunkHeaderLength = 4096

	// MaxTrailerLineLength limits one trailer line, including the line terminator.
	MaxTrailerLineLength = 8192

	// MaxTrailerCount limits the number of trailer header lines.
	MaxTrailerCount = 100

	// MaxTrailerBytes limits aggregate trailer bytes, including line terminators.
	MaxTrailerBytes = 32 * 1024

	// ChunkBufferSize is the default buffer size for chunked reading.
	ChunkBufferSize = 4096
)

var (
	// ErrChunkHeaderTooLong indicates that a chunk-size line exceeded the limit.
	ErrChunkHeaderTooLong = errors.New("chunk header line exceeds maximum length")

	// ErrTrailerLineTooLong indicates that one trailer line exceeded the limit.
	ErrTrailerLineTooLong = errors.New("trailer line exceeds maximum length")

	// ErrTooManyTrailers indicates that the trailer count exceeded the limit.
	ErrTooManyTrailers = errors.New("too many trailer headers")

	// ErrTrailersTooLarge indicates that aggregate trailer bytes exceeded the limit.
	ErrTrailersTooLarge = errors.New("trailer headers exceed maximum size")

	// ErrPreviewNotEnabled indicates that continuation was requested for an ordinary body.
	ErrPreviewNotEnabled = errors.New("chunked reader preview mode is not enabled")

	// ErrPreviewNotAtBoundary indicates that the preview terminator has not been read.
	ErrPreviewNotAtBoundary = errors.New("chunked reader is not at a preview boundary")

	// ErrPreviewIEOF indicates that the client declared the preview to be the complete body.
	ErrPreviewIEOF = errors.New("preview ended with ieof")

	// ErrPreviewAlreadyContinued indicates that the preview was already resumed.
	ErrPreviewAlreadyContinued = errors.New("preview continuation was already requested")
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
	err              error
	r                io.Reader
	byteReader       io.ByteReader
	closer           io.Closer
	n                int64
	finished         bool
	previewEnabled   bool
	previewBoundary  bool
	previewIEOF      bool
	previewContinued bool
}

// NewChunkedReader creates a new ChunkedReader that reads from r.
// It never adds another buffered reader because read-ahead would consume bytes
// belonging to the next request on a persistent connection.
func NewChunkedReader(r io.Reader) *ChunkedReader {
	closer, _ := r.(io.Closer)
	byteReader, ok := r.(io.ByteReader)
	if !ok {
		byteReader = &singleByteReader{r: r}
	}
	return &ChunkedReader{r: r, byteReader: byteReader, closer: closer}
}

type singleByteReader struct {
	r   io.Reader
	buf [1]byte
}

func (r *singleByteReader) ReadByte() (byte, error) {
	_, err := io.ReadFull(r.r, r.buf[:])
	return r.buf[0], err
}

// Close interrupts an in-progress read when the underlying reader is closable.
func (cr *ChunkedReader) Close() error {
	if cr.closer == nil {
		return nil
	}
	return cr.closer.Close()
}

// EnablePreview makes the first zero chunk a preview boundary. Call it before
// reading the body. A zero chunk with the ieof extension remains final.
func (cr *ChunkedReader) EnablePreview() {
	cr.previewEnabled = true
}

// PreviewBoundary reports whether reading reached a preview terminator and
// whether that terminator included the case-insensitive ieof extension.
func (cr *ChunkedReader) PreviewBoundary() (atBoundary, ieof bool) {
	return cr.previewBoundary, cr.previewIEOF
}

// ContinueAfterPreview resumes a body after a non-ieof preview terminator.
// A preview body can be resumed exactly once.
func (cr *ChunkedReader) ContinueAfterPreview() error {
	switch {
	case !cr.previewEnabled:
		return ErrPreviewNotEnabled
	case cr.previewContinued:
		return ErrPreviewAlreadyContinued
	case !cr.previewBoundary:
		return ErrPreviewNotAtBoundary
	case cr.previewIEOF:
		return ErrPreviewIEOF
	default:
		cr.previewBoundary = false
		cr.previewContinued = true
		return nil
	}
}

// Read implements io.Reader. It reads from the chunked stream.
func (cr *ChunkedReader) Read(p []byte) (n int, err error) {
	if cr.err != nil {
		return 0, cr.err
	}
	if cr.finished || cr.previewBoundary {
		return 0, io.EOF
	}
	if cr.n == 0 {
		if err := cr.startChunk(); err != nil {
			if !errors.Is(err, io.EOF) {
				cr.err = err
			}
			return 0, err
		}
	}
	return cr.readChunkData(p)
}

func (cr *ChunkedReader) readChunkData(p []byte) (n int, err error) {
	toRead := int64(len(p))
	if toRead > cr.n {
		toRead = cr.n
	}
	n, err = io.ReadFull(cr.r, p[:toRead])
	cr.n -= int64(n)
	if cr.n == 0 && err == nil {
		if e := cr.readCRLF(); e != nil {
			cr.err = e
			return n, e
		}
	}
	return n, err
}

func (cr *ChunkedReader) startChunk() error {
	header, err := cr.readChunkHeader()
	if err != nil {
		return err
	}
	if header.size > 0 {
		cr.n = header.size
		return nil
	}
	return cr.finishZeroChunk(header)
}

func (cr *ChunkedReader) finishZeroChunk(header parsedChunkHeader) error {
	if err := cr.readTrailer(); err != nil {
		return err
	}
	if cr.previewEnabled && !cr.previewContinued {
		cr.previewBoundary = true
		cr.previewIEOF = header.hasExtension("ieof")
		cr.finished = cr.previewIEOF
		return io.EOF
	}
	cr.finished = true
	return io.EOF
}

type parsedChunkHeader struct {
	extensions []string
	size       int64
}

func (h parsedChunkHeader) hasExtension(name string) bool {
	for _, extension := range h.extensions {
		extensionName, _, _ := strings.Cut(strings.TrimSpace(extension), "=")
		if strings.EqualFold(strings.TrimSpace(extensionName), name) {
			return true
		}
	}
	return false
}

// readChunkHeader reads and parses a chunk header line, retaining extensions.
func (cr *ChunkedReader) readChunkHeader() (parsedChunkHeader, error) {
	line, err := cr.readBoundedLine(MaxChunkHeaderLength, ErrChunkHeaderTooLong)
	if err != nil {
		return parsedChunkHeader{}, err
	}
	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	parts := strings.Split(line, ";")
	size, err := ParseChunkSize(parts[0])
	if err != nil {
		return parsedChunkHeader{}, err
	}
	if size > MaxChunkSize {
		return parsedChunkHeader{}, errors.New("chunk size exceeds maximum")
	}
	return parsedChunkHeader{size: size, extensions: parts[1:]}, nil
}

func (cr *ChunkedReader) readBoundedLine(limit int, limitErr error) (string, error) {
	if reader, ok := cr.r.(interface {
		ReadBoundedLine(int) (string, error)
	}); ok {
		line, err := reader.ReadBoundedLine(limit)
		if errors.Is(err, io.ErrShortBuffer) || len(line) > limit {
			return "", limitErr
		}
		if err != nil {
			return "", err
		}
		return line, nil
	}

	line := make([]byte, 0, limit)
	for len(line) < limit {
		b, err := cr.byteReader.ReadByte()
		if err != nil {
			return "", err
		}
		line = append(line, b)
		if b == '\n' {
			return string(line), nil
		}
	}
	return "", limitErr
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
	trailerCount := 0
	trailerBytes := 0

	for {
		line, err := cr.readBoundedLine(MaxTrailerLineLength, ErrTrailerLineTooLong)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return err
		}
		// Empty line signals end of trailer
		if line == "\r\n" || line == "\n" {
			return nil
		}

		trailerCount++
		if trailerCount > MaxTrailerCount {
			return ErrTooManyTrailers
		}

		trailerBytes += len(line)
		if trailerBytes > MaxTrailerBytes {
			return ErrTrailersTooLarge
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
