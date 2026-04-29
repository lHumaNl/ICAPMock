// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
)

// UnknownStreamPayloadSize indicates that a stream payload size is unknown.
const UnknownStreamPayloadSize int64 = -1

// StreamPayload opens readers for a BodyStream payload.
type StreamPayload interface {
	Open() (io.ReadCloser, error)
	SizeHint() (int64, bool)
	Replayable() bool
}

// StreamReaderFactory opens a new reader for a stream payload.
type StreamReaderFactory func() (io.ReadCloser, error)

var (
	// ErrStreamPayloadConsumed is returned when a one-shot payload is reopened.
	ErrStreamPayloadConsumed = errors.New("stream payload already consumed")

	// ErrStreamPayloadFactoryNil is returned when a replayable payload has no reader factory.
	ErrStreamPayloadFactoryNil = errors.New("stream payload reader factory is nil")
)

type replayableStreamPayload struct {
	factory  StreamReaderFactory
	sizeHint int64
}

type oneShotStreamPayload struct {
	reader   io.ReadCloser
	sizeHint int64
	opened   atomic.Bool
}

type limitedStreamPayload struct {
	payload  StreamPayload
	maxBytes int64
}

type sequenceStreamPayload struct {
	payloads   []StreamPayload
	sizeHint   int64
	replayable bool
	opened     atomic.Bool
}

type sequenceReadCloser struct {
	current  io.ReadCloser
	payloads []StreamPayload
	index    int
	closed   bool
}

type maxBytesReadCloser struct {
	reader    io.ReadCloser
	remaining int64
}

// NewBytesStreamPayload creates a replayable byte-backed stream payload.
func NewBytesStreamPayload(body []byte) StreamPayload {
	data := bytes.Clone(body)
	return NewReplayableStreamPayload(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}, int64(len(data)))
}

// NewReplayableStreamPayload creates a payload from a reader factory.
func NewReplayableStreamPayload(factory StreamReaderFactory, sizeHint int64) StreamPayload {
	return replayableStreamPayload{factory: factory, sizeHint: normalizeSizeHint(sizeHint)}
}

// NewOneShotStreamPayload creates a payload that can be opened once.
func NewOneShotStreamPayload(reader io.ReadCloser, sizeHint int64) StreamPayload {
	return &oneShotStreamPayload{reader: reader, sizeHint: normalizeSizeHint(sizeHint)}
}

// NewLimitedStreamPayload wraps a payload with a streaming max-byte guard.
func NewLimitedStreamPayload(payload StreamPayload, maxBytes int64) (StreamPayload, error) {
	if maxBytes <= 0 || payload == nil {
		return payload, nil
	}
	if size, known := payload.SizeHint(); known && size > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return limitedStreamPayload{payload: payload, maxBytes: maxBytes}, nil
}

// NewSequenceStreamPayload creates a payload by reading payloads in order.
func NewSequenceStreamPayload(payloads []StreamPayload) StreamPayload {
	items := append([]StreamPayload(nil), payloads...)
	sizeHint, replayable := sequencePayloadMetadata(items)
	return &sequenceStreamPayload{payloads: items, sizeHint: sizeHint, replayable: replayable}
}

// NewHTTPMessageBodyStreamPayload creates a payload from an HTTP message body.
// Cached bodies are replayable; live BodyReader values are moved into a
// one-shot payload so retries fail explicitly instead of silently rereading.
func NewHTTPMessageBodyStreamPayload(msg *HTTPMessage, maxBytes int64) (StreamPayload, error) {
	if msg == nil {
		return nil, errors.New("http message is nil")
	}
	if body, ok := msg.cachedBodyForStream(); ok {
		return bytesPayloadWithinLimit(body, maxBytes)
	}
	reader, sizeHint, err := msg.claimBodyReaderForStream(maxBytes)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return NewBytesStreamPayload(nil), nil
	}
	return NewOneShotStreamPayload(limitedReadCloser(reader, maxBytes), sizeHint), nil
}

func (p replayableStreamPayload) Open() (io.ReadCloser, error) {
	if p.factory == nil {
		return nil, ErrStreamPayloadFactoryNil
	}
	return p.factory()
}

func (p replayableStreamPayload) SizeHint() (int64, bool) {
	return streamPayloadSizeHint(p.sizeHint)
}

func (p replayableStreamPayload) Replayable() bool { return true }

func (p *oneShotStreamPayload) Open() (io.ReadCloser, error) {
	if p.opened.Swap(true) {
		return nil, ErrStreamPayloadConsumed
	}
	return p.reader, nil
}

func (p *oneShotStreamPayload) SizeHint() (int64, bool) {
	return streamPayloadSizeHint(p.sizeHint)
}

func (p *oneShotStreamPayload) Replayable() bool { return false }

func (p limitedStreamPayload) Open() (io.ReadCloser, error) {
	reader, err := p.payload.Open()
	if err != nil {
		return nil, err
	}
	return limitedReadCloser(reader, p.maxBytes), nil
}

func (p limitedStreamPayload) SizeHint() (int64, bool) { return p.payload.SizeHint() }

func (p limitedStreamPayload) Replayable() bool { return p.payload.Replayable() }

func (p *sequenceStreamPayload) Open() (io.ReadCloser, error) {
	if !p.replayable && p.opened.Swap(true) {
		return nil, ErrStreamPayloadConsumed
	}
	return &sequenceReadCloser{payloads: p.payloads}, nil
}

func (p *sequenceStreamPayload) SizeHint() (int64, bool) {
	return streamPayloadSizeHint(p.sizeHint)
}

func (p *sequenceStreamPayload) Replayable() bool { return p.replayable }

func (r *sequenceReadCloser) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	return r.readNext(p)
}

func (r *sequenceReadCloser) Close() error {
	r.closed = true
	if r.current == nil {
		return nil
	}
	err := r.current.Close()
	r.current = nil
	return err
}

func (r *maxBytesReadCloser) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return r.probeOverflow()
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (r *maxBytesReadCloser) Close() error { return r.reader.Close() }

func (r *maxBytesReadCloser) probeOverflow() (int, error) {
	var probe [1]byte
	n, err := r.reader.Read(probe[:])
	if n > 0 {
		return 0, ErrBodyTooLarge
	}
	return 0, err
}

func (m *HTTPMessage) cachedBodyForStream() ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Body, m.bodyLoaded || len(m.Body) > 0
}

func (m *HTTPMessage) claimBodyReaderForStream(maxBytes int64) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bodyReader := m.BodyReader
	sizeHint := m.contentLengthHint()
	if bodyReader != nil && sizeHintExceedsLimit(sizeHint, maxBytes) {
		return nil, sizeHint, ErrBodyTooLarge
	}
	m.BodyReader = nil
	return readCloserFor(bodyReader), sizeHint, nil
}

func bytesPayloadWithinLimit(body []byte, maxBytes int64) (StreamPayload, error) {
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return NewBytesStreamPayload(body), nil
}

func sizeHintExceedsLimit(sizeHint, maxBytes int64) bool {
	return maxBytes > 0 && sizeHint >= 0 && sizeHint > maxBytes
}

func readCloserFor(reader io.Reader) io.ReadCloser {
	if reader == nil {
		return nil
	}
	if closer, ok := reader.(io.ReadCloser); ok {
		return closer
	}
	return io.NopCloser(reader)
}

func limitedReadCloser(reader io.ReadCloser, maxBytes int64) io.ReadCloser {
	if maxBytes <= 0 {
		return reader
	}
	return &maxBytesReadCloser{reader: reader, remaining: maxBytes}
}

func (m *HTTPMessage) contentLengthHint() int64 {
	if m.Header == nil {
		return UnknownStreamPayloadSize
	}
	value, ok := m.Header.Get("Content-Length")
	if !ok {
		return UnknownStreamPayloadSize
	}
	return parseContentLengthHint(value)
}

func parseContentLengthHint(value string) int64 {
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size < 0 {
		return UnknownStreamPayloadSize
	}
	return size
}

func normalizeSizeHint(sizeHint int64) int64 {
	if sizeHint < 0 {
		return UnknownStreamPayloadSize
	}
	return sizeHint
}

func streamPayloadSizeHint(sizeHint int64) (int64, bool) {
	return sizeHint, sizeHint >= 0
}

func sequencePayloadMetadata(payloads []StreamPayload) (int64, bool) {
	var total int64
	knownTotal := true
	replayable := true
	for _, payload := range payloads {
		if payload == nil {
			knownTotal = false
			replayable = false
			continue
		}
		if !payload.Replayable() {
			replayable = false
		}
		size, known := payload.SizeHint()
		if !known {
			knownTotal = false
			continue
		}
		total += size
	}
	if !knownTotal {
		return UnknownStreamPayloadSize, replayable
	}
	return total, replayable
}

func (r *sequenceReadCloser) readNext(p []byte) (int, error) {
	for {
		if err := r.ensureCurrent(); err != nil {
			return 0, err
		}
		if r.current == nil {
			return 0, io.EOF
		}
		n, err := r.current.Read(p)
		if n > 0 {
			r.advanceOnEOF(err)
			return n, nil
		}
		if !errors.Is(err, io.EOF) {
			return 0, err
		}
		if err := r.advance(); err != nil {
			return 0, err
		}
	}
}

func (r *sequenceReadCloser) ensureCurrent() error {
	if r.current != nil || r.index >= len(r.payloads) {
		return nil
	}
	reader, err := r.payloads[r.index].Open()
	if err != nil {
		return err
	}
	r.current = reader
	return nil
}

func (r *sequenceReadCloser) advance() error {
	err := r.current.Close()
	r.current = nil
	r.index++
	return err
}

func (r *sequenceReadCloser) advanceOnEOF(err error) {
	if errors.Is(err, io.EOF) {
		_ = r.advance()
	}
}
