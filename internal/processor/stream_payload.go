// Copyright 2026 ICAP Mock

package processor

import (
	"errors"
	"io"
	"os"
	"sync"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

type streamFile interface {
	io.ReadCloser
	Stat() (os.FileInfo, error)
}

var openStreamFile = func(path string) (streamFile, error) {
	return os.Open(path) //nolint:gosec // scenario-controlled path
}

type preparedStreamPayload struct {
	icap.StreamPayload
	cleanup func() error
}

// Release closes prepared resources when response delivery never opens them.
func (p *preparedStreamPayload) Release() error {
	if p == nil || p.cleanup == nil {
		return nil
	}
	return p.cleanup()
}

type cleanupReadCloser struct {
	io.ReadCloser
	cleanup func() error
}

type onceReadCloser struct {
	io.ReadCloser
	err  error
	once sync.Once
}

func newPreparedFilePayload(file io.ReadCloser, size int64) icap.StreamPayload {
	reader := &onceReadCloser{ReadCloser: file}
	payload := icap.NewOneShotStreamPayload(reader, size)
	return &preparedStreamPayload{StreamPayload: payload, cleanup: reader.Close}
}

func newPreparedSequencePayload(payloads []icap.StreamPayload) icap.StreamPayload {
	sequence := icap.NewSequenceStreamPayload(payloads)
	return &preparedStreamPayload{
		StreamPayload: sequence,
		cleanup: func() error {
			return cleanupStreamPayloads(payloads)
		},
	}
}

func preserveStreamPayloadCleanup(payload, original icap.StreamPayload) icap.StreamPayload {
	prepared, ok := original.(*preparedStreamPayload)
	if !ok {
		return payload
	}
	return &preparedStreamPayload{StreamPayload: payload, cleanup: prepared.cleanup}
}

func (p *preparedStreamPayload) Open() (io.ReadCloser, error) {
	reader, err := p.StreamPayload.Open()
	if err != nil {
		return nil, errors.Join(err, p.cleanup())
	}
	return &cleanupReadCloser{ReadCloser: reader, cleanup: p.cleanup}, nil
}

func (r *cleanupReadCloser) Close() error {
	return errors.Join(r.ReadCloser.Close(), r.cleanup())
}

func (r *onceReadCloser) Close() error {
	r.once.Do(func() { r.err = r.ReadCloser.Close() })
	return r.err
}

func cleanupStreamPayload(payload icap.StreamPayload) error {
	prepared, ok := payload.(*preparedStreamPayload)
	if !ok {
		return nil
	}
	return prepared.cleanup()
}

func cleanupStreamPayloads(payloads []icap.StreamPayload) error {
	var err error
	for _, payload := range payloads {
		err = errors.Join(err, cleanupStreamPayload(payload))
	}
	return err
}
