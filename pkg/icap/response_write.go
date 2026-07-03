// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"io"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

func (r *Response) writeDirectTo(w io.Writer) (int64, error) {
	cw := &countingWriter{w: w}
	buf := pool.ResponseBufferPool.Get()
	defer pool.ResponseBufferPool.Put(buf)
	r.writeEnvelopeToBuffer(buf)
	if err := flushResponseBuffer(cw, buf); err != nil {
		return cw.n, err
	}
	if err := r.writeHTTPMessageDirect(cw, buf, r.HTTPRequest, true); err != nil {
		return cw.n, err
	}
	if err := r.writeHTTPMessageDirect(cw, buf, r.HTTPResponse, false); err != nil {
		return cw.n, err
	}
	return r.writeICAPBodyDirect(cw)
}

func (r *Response) writeHTTPMessageDirect(cw *countingWriter, buf *bytes.Buffer, m *HTTPMessage, isReq bool) error {
	if m == nil {
		return nil
	}
	r.writeHTTPMessageHead(buf, m, isReq)
	if err := flushResponseBuffer(cw, buf); err != nil {
		return err
	}
	return writeHTTPBodyDirect(cw, m)
}

func writeHTTPBodyDirect(cw *countingWriter, m *HTTPMessage) error {
	if m.BodyStream != nil {
		_, err := m.BodyStream.WriteTo(cw)
		return err
	}
	if len(m.Body) > 0 {
		return WriteChunkedBody(cw, m.Body)
	}
	return nil
}

func (r *Response) writeICAPBodyDirect(cw *countingWriter) (int64, error) {
	if len(r.Body) == 0 {
		return cw.n, nil
	}
	_, err := cw.Write(r.Body)
	return cw.n, err
}

func flushResponseBuffer(cw *countingWriter, buf *bytes.Buffer) error {
	if len(buf.Bytes()) == 0 {
		return nil
	}
	_, err := cw.Write(buf.Bytes())
	buf.Reset()
	return err
}
