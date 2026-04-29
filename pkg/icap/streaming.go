// Copyright 2026 ICAP Mock

package icap

import (
	"errors"
	"io"
	"math/rand"
	"strconv"
	"time"
)

// Stream finish modes supported by scenario-driven body streaming.
const (
	StreamFinishComplete = "complete"
	StreamFinishFIN      = "fin"
	StreamFinishWeighted = "weighted"
)

// ErrBodyStreamCloneUnavailable is returned by clones of non-replayable reader streams.
var ErrBodyStreamCloneUnavailable = errors.New("body stream reader cannot be cloned")

// IntnRandom is the small random interface needed for deterministic tests.
type IntnRandom interface{ Intn(n int) int }

// BodyStream describes a chunked encapsulated HTTP body written over time.
//
//nolint:govet // field order groups injectable dependencies before scalar settings.
type BodyStream struct {
	Reader          io.Reader
	Payload         StreamPayload
	Rand            IntnRandom
	FinishMode      string
	Sleep           func(time.Duration)
	ChunkSize       int
	ChunkSizeMax    int
	CompletePercent int
	FinPercent      int
	FinAfterBytes   int64
	TotalBytes      int64
	Delay           time.Duration
	DelayMax        time.Duration
	Duration        time.Duration
	FinAfterTime    time.Duration
}

type flushWriter interface{ Flush() error }

type countingWriter struct {
	w io.Writer
	n int64
}

type unavailableStreamPayload struct{}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *countingWriter) Flush() error { return flush(w.w) }

// Clone copies stream settings without sharing mutable BodyStream state.
// Replayable payloads can be reused safely. One-shot payloads intentionally
// share consumption state so writing the original and clone fails clearly with
// ErrStreamPayloadConsumed. Plain Reader streams cannot be cloned safely.
func (s *BodyStream) Clone() *BodyStream {
	if s == nil {
		return nil
	}
	clone := *s
	if s.Payload == nil && s.Reader != nil {
		clone.Reader = nil
		clone.Payload = unavailableStreamPayload{}
	}
	return &clone
}

func (unavailableStreamPayload) Open() (io.ReadCloser, error) {
	return nil, ErrBodyStreamCloneUnavailable
}

func (unavailableStreamPayload) SizeHint() (int64, bool) { return UnknownStreamPayloadSize, false }

func (unavailableStreamPayload) Replayable() bool { return false }

// WriteTo writes chunked body data and optionally the terminating chunk.
func (s *BodyStream) WriteTo(w io.Writer) (written int64, err error) {
	reader, closeReader, err := s.openReader()
	if err != nil {
		return 0, err
	}
	defer func() { err = joinCloseError(err, closeReader) }()
	cw := &countingWriter{w: w}
	mode := s.resolveFinishMode()
	_, err = s.writeChunks(cw, reader, mode)
	if err != nil || mode == StreamFinishFIN {
		return cw.n, err
	}
	return cw.n, writeFinalChunk(cw, w)
}

func (s *BodyStream) openReader() (io.Reader, func() error, error) {
	if s.Payload == nil {
		return s.readerFallback()
	}
	reader, err := s.Payload.Open()
	if err != nil {
		return nil, nil, err
	}
	if reader == nil {
		return nil, nil, errors.New("stream payload reader is nil")
	}
	return reader, reader.Close, nil
}

func (s *BodyStream) readerFallback() (io.Reader, func() error, error) {
	if s.Reader == nil {
		return nil, nil, errors.New("stream reader is nil")
	}
	return s.Reader, nil, nil
}

func joinCloseError(err error, closeReader func() error) error {
	if closeReader == nil {
		return err
	}
	return errors.Join(err, closeReader())
}

func (s *BodyStream) writeChunks(w *countingWriter, reader io.Reader, mode string) (int64, error) {
	buf := make([]byte, s.nextChunkSize())
	bodyBytes := int64(0)
	started := time.Now()
	for !s.finTriggered(mode, bodyBytes, started) {
		n, readErr := s.readNext(reader, buf, bodyBytes, mode)
		if n > 0 {
			if err := writeChunk(w, w.w, buf[:n]); err != nil {
				return bodyBytes, err
			}
			bodyBytes += int64(n)
			s.sleepBetweenChunks(readErr)
		}
		if errors.Is(readErr, io.EOF) {
			return bodyBytes, nil
		}
		if readErr != nil {
			return bodyBytes, readErr
		}
		buf = resizeBuffer(buf, s.nextChunkSize())
	}
	return bodyBytes, nil
}

func (s *BodyStream) readNext(reader io.Reader, buf []byte, written int64, mode string) (int, error) {
	limit := s.remainingFINBytes(written, mode)
	if limit == 0 {
		return 0, io.EOF
	}
	if limit > 0 && int64(len(buf)) > limit {
		buf = buf[:limit]
	}
	return reader.Read(buf)
}

func (s *BodyStream) remainingFINBytes(written int64, mode string) int64 {
	if mode != StreamFinishFIN || s.FinAfterBytes <= 0 {
		return -1
	}
	remaining := s.FinAfterBytes - written
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *BodyStream) finTriggered(mode string, written int64, started time.Time) bool {
	if mode != StreamFinishFIN {
		return false
	}
	if s.FinAfterBytes > 0 && written >= s.FinAfterBytes {
		return true
	}
	return s.FinAfterTime > 0 && time.Since(started) >= s.FinAfterTime
}

func (s *BodyStream) resolveFinishMode() string {
	if s.FinishMode != StreamFinishWeighted {
		return s.FinishMode
	}
	if s.randomInt(100) < s.CompletePercent {
		return StreamFinishComplete
	}
	return StreamFinishFIN
}

func (s *BodyStream) sleepBetweenChunks(readErr error) {
	delay := s.nextDelay()
	if readErr != nil || delay <= 0 {
		return
	}
	if s.Sleep != nil {
		s.Sleep(delay)
		return
	}
	time.Sleep(delay)
}

func (s *BodyStream) nextDelay() time.Duration {
	if s.Duration > 0 {
		return s.durationDelay()
	}
	if s.DelayMax <= s.Delay {
		return s.Delay
	}
	return s.Delay + time.Duration(s.randomInt(int(s.DelayMax-s.Delay)))
}

func (s *BodyStream) durationDelay() time.Duration {
	chunks := s.estimatedChunks()
	if chunks <= 1 {
		return 0
	}
	return s.Duration / time.Duration(chunks-1)
}

func (s *BodyStream) estimatedChunks() int64 {
	chunkSize := int64(s.effectiveChunkSize())
	if s.TotalBytes <= 0 || chunkSize <= 0 {
		return 0
	}
	return (s.TotalBytes + chunkSize - 1) / chunkSize
}

func (s *BodyStream) nextChunkSize() int {
	minSize := s.effectiveChunkSize()
	if s.ChunkSizeMax <= minSize {
		return minSize
	}
	return minSize + s.randomInt(s.ChunkSizeMax-minSize+1)
}

func (s *BodyStream) effectiveChunkSize() int {
	if s.ChunkSize > 0 {
		return s.ChunkSize
	}
	return 1
}

func (s *BodyStream) randomInt(n int) int {
	if n <= 0 {
		return 0
	}
	if s.Rand != nil {
		return s.Rand.Intn(n)
	}
	return rand.Intn(n) //nolint:gosec // deterministic injection is available for tests
}

func resizeBuffer(buf []byte, size int) []byte {
	if len(buf) == size {
		return buf
	}
	return make([]byte, size)
}

func writeChunk(cw *countingWriter, target io.Writer, p []byte) error {
	if _, err := cw.Write(chunkHeader(len(p))); err != nil {
		return err
	}
	if _, err := cw.Write(p); err != nil {
		return err
	}
	if _, err := cw.Write(crlfBytes); err != nil {
		return err
	}
	return flush(target)
}

func chunkHeader(size int) []byte {
	var hdr [20]byte
	out := strconv.AppendInt(hdr[:0], int64(size), 16)
	return append(out, '\r', '\n')
}

func writeFinalChunk(cw *countingWriter, target io.Writer) error {
	if _, err := cw.Write([]byte("0\r\n\r\n")); err != nil {
		return err
	}
	return flush(target)
}

func flush(w io.Writer) error {
	if f, ok := w.(flushWriter); ok {
		return f.Flush()
	}
	return nil
}
