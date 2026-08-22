// Copyright 2026 ICAP Mock

package icap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Stream finish modes supported by scenario-driven body streaming.
const (
	StreamFinishComplete  = "complete"
	StreamFinishFIN       = "fin"
	StreamFinishTerm      = "term"
	StreamFinishWeighted  = "weighted"
	streamWriteBufferSize = 64 * 1024
)

var (
	// ErrBodyStreamCloneUnavailable is returned by clones of non-replayable reader streams.
	ErrBodyStreamCloneUnavailable = errors.New("body stream reader cannot be cloned")
	// ErrInvalidBodyStreamPlan is returned when a stream has no planned delivery schedule.
	ErrInvalidBodyStreamPlan = errors.New("body stream plan is invalid")
	// ErrStreamSourceSizeMismatch reports early EOF or data beyond the stable planned size.
	ErrStreamSourceSizeMismatch = errors.New("stream source size differs from plan")
)

// StreamClock supplies current time for stream pacing.
type StreamClock interface {
	Now() time.Time
}

// StreamSleeper waits for a pacing interval or context cancellation.
type StreamSleeper func(context.Context, time.Duration) error

// BodyStream describes a planned chunked encapsulated HTTP body.
type BodyStream struct {
	Reader  io.Reader
	Payload StreamPayload
	Clock   StreamClock
	Sleeper StreamSleeper
	Plan    BodyStreamPlan
}

type flushWriter interface{ Flush() error }

type countingWriter struct {
	w io.Writer
	n int64
}

type unavailableStreamPayload struct{}
type streamPayloadReleaser interface{ Release() error }

type systemStreamClock struct{}

type streamWriter struct {
	ctx     context.Context
	stream  *BodyStream
	reader  io.Reader
	writer  *countingWriter
	started time.Time
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *countingWriter) Flush() error { return flush(w.w) }

func (systemStreamClock) Now() time.Time { return time.Now() }

// Clone copies a stream while preserving its immutable plan.
// One-shot payload clones share consumption state by design.
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

// Release closes a prepared payload that was never consumed by delivery.
// It is safe to call after delivery when the payload releaser is idempotent.
func (s *BodyStream) Release() error {
	if s == nil || s.Payload == nil {
		return nil
	}
	if releaser, ok := s.Payload.(streamPayloadReleaser); ok {
		return releaser.Release()
	}
	return nil
}

func (unavailableStreamPayload) Open() (io.ReadCloser, error) {
	return nil, ErrBodyStreamCloneUnavailable
}

func (unavailableStreamPayload) SizeHint() (int64, bool) {
	return UnknownStreamPayloadSize, false
}

func (unavailableStreamPayload) Replayable() bool { return false }

// WriteTo writes the planned stream using a background context.
func (s *BodyStream) WriteTo(w io.Writer) (int64, error) {
	return s.WriteToContext(context.Background(), w)
}

// WriteToContext writes the planned stream and observes context cancellation.
func (s *BodyStream) WriteToContext(ctx context.Context, w io.Writer) (int64, error) {
	return s.writeToContext(ctx, w, nil)
}

func (s *BodyStream) writeToContext(ctx context.Context, w io.Writer, onStart func()) (int64, error) {
	if err := s.validateWrite(ctx, w); err != nil {
		return 0, err
	}
	reader, closeReader, err := s.openReader()
	if err != nil {
		return 0, err
	}
	return s.writeOpened(ctx, w, reader, closeReader, onStart)
}

func (s *BodyStream) validateWrite(ctx context.Context, w io.Writer) error {
	if s == nil || !validSelectedFinishMode(s.Plan.finishMode) {
		return ErrInvalidBodyStreamPlan
	}
	if w == nil {
		return errors.New("stream writer is nil")
	}
	return ctx.Err()
}

func (s *BodyStream) writeOpened(
	ctx context.Context,
	w io.Writer,
	reader io.Reader,
	closeReader func() error,
	onStart func(),
) (int64, error) {
	if err := s.validateOpenedSource(); err != nil {
		return 0, errors.Join(err, closeReader())
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(err, closeReader())
	}
	return s.deliver(ctx, w, reader, closeReader, onStart)
}

func (s *BodyStream) deliver(ctx context.Context, w io.Writer, reader io.Reader, closeReader func() error, onStart func()) (int64, error) {
	closed := false
	defer func() {
		if !closed {
			_ = closeReader()
		}
	}()
	cw := &countingWriter{w: w}
	if onStart != nil {
		onStart()
	}
	executor := streamWriter{ctx: ctx, stream: s, reader: reader, writer: cw, started: s.clock().Now()}
	completed, err := executor.writeChunks()
	err = executor.verifyCompletedSource(completed, err)
	err = errors.Join(err, closeReader())
	closed = true
	return s.finishDelivery(ctx, cw, w, err)
}

func (s *BodyStream) finishDelivery(ctx context.Context, cw *countingWriter, target io.Writer, err error) (int64, error) {
	if err != nil || s.Plan.finishMode == StreamFinishFIN {
		return cw.n, err
	}
	err = writeFinalChunkContext(ctx, cw, target)
	return cw.n, err
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
	return s.Reader, func() error { return nil }, nil
}

func (s *BodyStream) validateOpenedSource() error {
	if s.Payload == nil {
		return nil
	}
	size, known := s.Payload.SizeHint()
	if known && size != s.Plan.sourceSize {
		return fmt.Errorf("%w: payload is %d bytes, plan is %d", ErrStreamSourceSizeMismatch, size, s.Plan.sourceSize)
	}
	return nil
}

func (s *BodyStream) clock() StreamClock {
	if s.Clock != nil {
		return s.Clock
	}
	return systemStreamClock{}
}

func (s *BodyStream) sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	if s.Sleeper != nil {
		return s.Sleeper(ctx, delay)
	}
	return sleepWithContext(ctx, delay)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *streamWriter) writeChunks() (bool, error) {
	for index := 0; index < e.stream.Plan.chunkCount; index++ {
		proceed, err := e.waitForChunk(index)
		if err != nil || !proceed {
			return false, err
		}
		size, _ := e.stream.Plan.ChunkSize(index)
		if err := e.writeChunk(size); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (e *streamWriter) waitForChunk(index int) (bool, error) {
	if err := e.ctx.Err(); err != nil {
		return false, err
	}
	if e.partialDeadlinePassed() {
		return false, nil
	}
	offset, _ := e.stream.Plan.ChunkOffset(index)
	delay := e.started.Add(offset).Sub(e.stream.clock().Now())
	if err := e.stream.sleep(e.ctx, delay); err != nil && delay > 0 {
		return false, err
	}
	// A planned final partial chunk is intentionally released at the duration
	// boundary. Only an overrun observed before waiting triggers first-wins;
	// scheduler jitter after the planned sleep must not suppress that chunk.
	if e.partialDeadlinePassed() && offset < e.stream.Plan.duration {
		return false, nil
	}
	return true, nil
}

func (e *streamWriter) partialDeadlinePassed() bool {
	return e.partial() && e.deadlinePassed(e.stream.clock().Now())
}

func (e *streamWriter) partial() bool {
	return partialFinishMode(e.stream.Plan.finishMode) && e.stream.Plan.duration > 0
}

func (e *streamWriter) deadlinePassed(now time.Time) bool {
	return !now.Before(e.started.Add(e.stream.Plan.duration))
}

func (e *streamWriter) writeChunk(size int64) error {
	if err := writeAll(e.ctx, e.writer, chunkHeader(size)); err != nil {
		return err
	}
	if err := e.writeChunkData(size); err != nil {
		return err
	}
	if err := writeAll(e.ctx, e.writer, crlfBytes); err != nil {
		return err
	}
	return flushContext(e.ctx, e.writer.w)
}

func (e *streamWriter) writeChunkData(size int64) error {
	buffer := make([]byte, minInt64(size, streamWriteBufferSize))
	for remaining := size; remaining > 0; {
		block := minInt64(remaining, int64(len(buffer)))
		n, err := io.ReadFull(e.reader, buffer[:block])
		if n > 0 {
			if writeErr := writeAll(e.ctx, e.writer, buffer[:n]); writeErr != nil {
				return writeErr
			}
			remaining -= int64(n)
		}
		if err != nil {
			return streamReadError(err)
		}
	}
	return nil
}

func streamReadError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", ErrStreamSourceSizeMismatch, io.ErrUnexpectedEOF)
	}
	return err
}

func (e *streamWriter) verifySourceEnd() error {
	var probe [1]byte
	for range 100 {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		n, err := e.reader.Read(probe[:])
		if n > 0 {
			return fmt.Errorf("%w: source contains additional data", ErrStreamSourceSizeMismatch)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return io.ErrNoProgress
}

func (e *streamWriter) verifyCompletedSource(completed bool, err error) error {
	if err != nil || !completed || e.stream.Plan.bodyBytes != e.stream.Plan.sourceSize {
		return err
	}
	return e.verifySourceEnd()
}

func partialFinishMode(mode string) bool {
	return mode == StreamFinishFIN || mode == StreamFinishTerm
}

func writeAll(ctx context.Context, writer io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func chunkHeader(size int64) []byte {
	var header [20]byte
	out := strconv.AppendInt(header[:0], size, 16)
	return append(out, '\r', '\n')
}

func writeFinalChunkContext(ctx context.Context, cw *countingWriter, target io.Writer) error {
	if err := writeAll(ctx, cw, []byte("0\r\n\r\n")); err != nil {
		return err
	}
	return flushContext(ctx, target)
}

func flushContext(ctx context.Context, writer io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return flush(writer)
}

func flush(w io.Writer) error {
	if flusher, ok := w.(flushWriter); ok {
		return flusher.Flush()
	}
	return nil
}
