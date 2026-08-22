// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"context"
	"io"

	"github.com/icap-mock/icap-mock/pkg/pool"
)

type contextWriter struct {
	ctx context.Context
	w   io.Writer
}

type streamStartObserver struct {
	callback func()
	started  bool
}

func (r *Response) writeDirectToContext(
	ctx context.Context,
	w io.Writer,
	options ResponseWriteOptions,
) (int64, error) {
	if err := validateResponseWriter(ctx, w); err != nil {
		return 0, err
	}
	target := &contextWriter{ctx: ctx, w: w}
	cw := &countingWriter{w: target}
	observer := &streamStartObserver{callback: options.OnStreamingStart}
	err := r.writeResponseParts(ctx, cw, observer)
	if err == nil {
		err = flushContext(ctx, target)
	}
	return cw.n, err
}

func validateResponseWriter(ctx context.Context, writer io.Writer) error {
	if writer == nil {
		return io.ErrClosedPipe
	}
	return ctx.Err()
}

func (r *Response) writeResponseParts(
	ctx context.Context,
	cw *countingWriter,
	observer *streamStartObserver,
) error {
	buf := pool.ResponseBufferPool.Get()
	defer pool.ResponseBufferPool.Put(buf)
	r.writeEnvelopeToBuffer(buf)
	if err := flushResponseBuffer(ctx, cw, buf); err != nil {
		return err
	}
	if err := r.writeHTTPMessageDirect(ctx, cw, buf, r.HTTPRequest, true, observer); err != nil {
		return err
	}
	if err := r.writeHTTPMessageDirect(ctx, cw, buf, r.HTTPResponse, false, observer); err != nil {
		return err
	}
	return r.writeICAPBodyDirect(ctx, cw)
}

func (r *Response) writeHTTPMessageDirect(
	ctx context.Context,
	cw *countingWriter,
	buf *bytes.Buffer,
	message *HTTPMessage,
	isRequest bool,
	observer *streamStartObserver,
) error {
	if message == nil {
		return nil
	}
	r.writeHTTPMessageHead(buf, message, isRequest)
	if err := flushResponseBuffer(ctx, cw, buf); err != nil {
		return err
	}
	return writeHTTPBodyDirect(ctx, cw, message, observer)
}

func writeHTTPBodyDirect(
	ctx context.Context,
	cw *countingWriter,
	message *HTTPMessage,
	observer *streamStartObserver,
) error {
	if message.BodyStream != nil {
		_, err := message.BodyStream.writeToContext(ctx, cw, observer.notify)
		return err
	}
	if len(message.Body) > 0 {
		return WriteChunkedBody(cw, message.Body)
	}
	return nil
}

func (r *Response) writeICAPBodyDirect(ctx context.Context, cw *countingWriter) error {
	if len(r.Body) == 0 {
		return nil
	}
	return writeAll(ctx, cw, r.Body)
}

func flushResponseBuffer(ctx context.Context, cw *countingWriter, buf *bytes.Buffer) error {
	if buf.Len() == 0 {
		return nil
	}
	err := writeAll(ctx, cw, buf.Bytes())
	buf.Reset()
	return err
}

func (w *contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (w *contextWriter) Flush() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	return flush(w.w)
}

func (o *streamStartObserver) notify() {
	if o == nil || o.started {
		return
	}
	o.started = true
	if o.callback != nil {
		o.callback()
	}
}
