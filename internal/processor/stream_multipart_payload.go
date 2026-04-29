// Copyright 2026 ICAP Mock

package processor

import (
	"errors"
	"io"
	"mime/multipart"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

type multipartSelectorPayload struct {
	source   icap.StreamPayload
	fallback icap.StreamPayload
	cfg      *storage.StreamMultipartConfig
	boundary string
	limit    int64
}

type multipartSelectorReader struct {
	source         io.ReadCloser
	current        io.Reader
	fallbackCloser io.Closer
	fallback       icap.StreamPayload
	reader         *multipart.Reader
	cfg            *storage.StreamMultipartConfig
	limit          int64
	selectedRead   int64
	matched        bool
	currentPart    bool
	fallbackOpened bool
	exhausted      bool
	closed         bool
}

func (p multipartSelectorPayload) Open() (io.ReadCloser, error) {
	reader, err := p.source.Open()
	if err != nil {
		return nil, err
	}
	return &multipartSelectorReader{
		source: reader, reader: multipart.NewReader(reader, p.boundary), cfg: p.cfg,
		fallback: p.fallback, limit: p.limit,
	}, nil
}

func (p multipartSelectorPayload) SizeHint() (int64, bool) {
	return icap.UnknownStreamPayloadSize, false
}

func (p multipartSelectorPayload) Replayable() bool {
	return p.source.Replayable() && (p.fallback == nil || p.fallback.Replayable())
}

func (r *multipartSelectorReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	for {
		if r.current != nil {
			n, err := r.readCurrent(p)
			if n > 0 || err != nil {
				return n, err
			}
			continue
		}
		if r.exhausted {
			return 0, io.EOF
		}
		if err := r.nextPart(); err != nil {
			return 0, err
		}
	}
}

func (r *multipartSelectorReader) Close() error {
	r.closed = true
	return errors.Join(r.closeFallback(), r.source.Close())
}

func (r *multipartSelectorReader) closeFallback() error {
	if r.fallbackCloser == nil {
		return nil
	}
	err := r.fallbackCloser.Close()
	r.fallbackCloser = nil
	return err
}

func (r *multipartSelectorReader) readCurrent(p []byte) (int, error) {
	if r.currentPart {
		return r.readSelectedPart(p)
	}
	n, err := r.current.Read(p)
	if errors.Is(err, io.EOF) {
		r.current = nil
	}
	return n, streamBodyReadError(err, r.limit)
}

func (r *multipartSelectorReader) readSelectedPart(p []byte) (int, error) {
	if err := r.ensureSelectedCapacity(); err != nil {
		return 0, err
	}
	p = r.trimToSelectedLimit(p)
	n, err := r.current.Read(p)
	r.selectedRead += int64(n)
	if errors.Is(err, io.EOF) {
		r.current, r.currentPart = nil, false
		if n > 0 {
			err = nil
		}
	}
	return n, streamBodyReadError(err, r.limit)
}

func (r *multipartSelectorReader) ensureSelectedCapacity() error {
	if streamBodyLimitUnlimited(r.limit) || r.selectedRead < r.limit {
		return nil
	}
	return r.probeSelectedOverflow()
}

func (r *multipartSelectorReader) trimToSelectedLimit(p []byte) []byte {
	if streamBodyLimitUnlimited(r.limit) {
		return p
	}
	if remaining := r.limit - r.selectedRead; int64(len(p)) > remaining {
		return p[:remaining]
	}
	return p
}

func (r *multipartSelectorReader) probeSelectedOverflow() error {
	var probe [1]byte
	n, err := r.current.Read(probe[:])
	if n > 0 {
		return streamBodyLimitError(r.limit)
	}
	if errors.Is(err, io.EOF) {
		r.current, r.currentPart = nil, false
		return nil
	}
	return streamBodyReadError(err, r.limit)
}

func (r *multipartSelectorReader) nextPart() error {
	part, err := r.reader.NextPart()
	if errors.Is(err, io.EOF) {
		return r.finishSource()
	}
	if err != nil {
		return streamBodyReadError(err, r.limit)
	}
	return r.handlePart(part)
}

func (r *multipartSelectorReader) handlePart(part *multipart.Part) error {
	if multipartPartMatches(part, *r.cfg) {
		r.matched, r.current, r.currentPart = true, part, true
		return nil
	}
	_, err := io.Copy(io.Discard, part)
	return streamBodyReadError(err, r.limit)
}

func (r *multipartSelectorReader) finishSource() error {
	r.exhausted = true
	if r.matched || r.cfg.AllowEmpty {
		return io.EOF
	}
	return r.openFallbackOrNoMatch()
}

func (r *multipartSelectorReader) openFallbackOrNoMatch() error {
	if r.fallback == nil || r.fallbackOpened {
		return errNoMultipartPartsMatched
	}
	reader, err := r.fallback.Open()
	if err != nil {
		return err
	}
	r.current, r.fallbackCloser = reader, reader
	r.fallbackOpened = true
	return nil
}
