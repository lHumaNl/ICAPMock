// Copyright 2026 ICAP Mock

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	defaultDrainBodyLimit = 10 * 1024 * 1024
	defaultDrainTimeout   = 30 * time.Second
)

var errInvalidPreviewBody = errors.New("invalid ICAP preview body framing")

type previewContinuationReader struct {
	server *ICAPServer
	conn   *Connection
	reader *icap.ChunkedReader
	ctx    context.Context
	err    error
	once   sync.Once
}

type deferredPreviewBodyReader struct { //nolint:govet // fields mirror preview then continuation read order.
	preview      []byte
	continuation *previewContinuationReader
	previewRead  *bytes.Reader
}

func newDeferredPreviewBodyReader(preview []byte, continuation *previewContinuationReader) *deferredPreviewBodyReader {
	return &deferredPreviewBodyReader{
		preview: preview, previewRead: bytes.NewReader(preview), continuation: continuation,
	}
}

func (r *deferredPreviewBodyReader) Read(p []byte) (int, error) {
	if r.previewRead.Len() > 0 {
		return r.previewRead.Read(p)
	}
	return r.continuation.Read(p)
}

func (r *deferredPreviewBodyReader) PreviewBodySnapshot() []byte { return r.preview }

func (r *previewContinuationReader) Read(p []byte) (int, error) {
	stopCancellation := func() bool { return false }
	if r.ctx != nil {
		stopCancellation = context.AfterFunc(r.ctx, func() {
			now := time.Now()
			_ = r.conn.SetWriteDeadline(now)
			_ = r.conn.SetReadDeadline(now)
		})
	}
	defer stopCancellation()
	if r.ctx != nil && r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	r.once.Do(r.continueBody)
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		requestinfo.MarkScenarioBodyMaterialized(r.ctx)
	}
	return n, err
}

func (r *previewContinuationReader) continueBody() {
	if r.server.config.WriteTimeout > 0 {
		if err := wrapDeadlineSetupError(
			"preview continuation write",
			r.conn.SetWriteDeadline(time.Now().Add(r.server.config.WriteTimeout)),
		); err != nil {
			r.err = err
			return
		}
	}
	writer := r.conn.Writer()
	if _, err := writer.WriteString("ICAP/1.0 100 Continue\r\n\r\n"); err != nil {
		r.err = fmt.Errorf("writing preview continuation: %w", err)
		return
	}
	if err := writer.Flush(); err != nil {
		r.err = fmt.Errorf("flushing preview continuation: %w", err)
		return
	}
	if err := r.server.setActiveReadDeadline(r.conn); err != nil {
		r.err = err
		return
	}
	r.err = r.reader.ContinueAfterPreview()
}

type requestDeadlineReader struct {
	reader    BufferedReader
	activate  func() error
	started   bool
	activated bool
}

type deadlineSetupError struct {
	err       error
	operation string
}

func (e *deadlineSetupError) Error() string {
	return fmt.Sprintf("setting %s deadline: %v", e.operation, e.err)
}

func (e *deadlineSetupError) Unwrap() error {
	return e.err
}

func wrapDeadlineSetupError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &deadlineSetupError{operation: operation, err: err}
}

func isDeadlineSetupError(err error) bool {
	var deadlineErr *deadlineSetupError
	return errors.As(err, &deadlineErr)
}

func newRequestDeadlineReader(reader BufferedReader, activate func() error) *requestDeadlineReader {
	return &requestDeadlineReader{reader: reader, activate: activate}
}

func (r *requestDeadlineReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		return n, r.activateOnce(err)
	}
	return n, err
}

func (r *requestDeadlineReader) ReadByte() (byte, error) {
	b, err := r.reader.ReadByte()
	if err == nil {
		return b, r.activateOnce(nil)
	}
	return b, err
}

func (r *requestDeadlineReader) Buffered() int {
	return r.reader.Buffered()
}

func (r *requestDeadlineReader) ReadBoundedLine(limit int) (string, error) {
	reader, ok := r.reader.(interface {
		ReadBoundedLine(int) (string, error)
	})
	if !ok {
		line, err := readBoundedLineBytes(r.reader, limit)
		if line != "" {
			return line, r.activateOnce(err)
		}
		return line, err
	}
	line, err := reader.ReadBoundedLine(limit)
	if line != "" {
		return line, r.activateOnce(err)
	}
	return line, err
}

func readBoundedLineBytes(reader io.ByteReader, limit int) (string, error) {
	if limit <= 0 {
		return "", io.ErrShortBuffer
	}
	line := make([]byte, 0, min(limit, 128))
	for len(line) < limit {
		b, err := reader.ReadByte()
		if err != nil {
			return string(line), err
		}
		line = append(line, b)
		if b == '\n' {
			return string(line), nil
		}
	}
	return string(line), io.ErrShortBuffer
}

func (r *requestDeadlineReader) ReadString(delim byte) (string, error) {
	s, err := r.reader.ReadString(delim)
	if s != "" {
		return s, r.activateOnce(err)
	}
	return s, err
}

func (r *requestDeadlineReader) Started() bool {
	return r.started
}

func (r *requestDeadlineReader) Close() error {
	if closer, ok := r.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (r *requestDeadlineReader) activateOnce(readErr error) error {
	r.started = true
	if r.activated || r.activate == nil {
		return readErr
	}
	r.activated = true
	if deadlineErr := r.activate(); deadlineErr != nil {
		if readErr != nil {
			return errors.Join(readErr, deadlineErr)
		}
		return deadlineErr
	}
	return readErr
}

func (s *ICAPServer) setWaitReadDeadline(conn *Connection, keepAliveWait bool) error {
	timeout := s.waitReadTimeout(keepAliveWait)
	if timeout <= 0 {
		return wrapDeadlineSetupError("wait read", conn.SetReadDeadline(time.Time{}))
	}
	return wrapDeadlineSetupError("wait read", conn.SetReadDeadline(time.Now().Add(timeout)))
}

func (s *ICAPServer) setActiveReadDeadline(conn *Connection) error {
	timeout := s.config.ReadTimeout
	if timeout <= 0 {
		timeout = defaultDrainTimeout
	}
	return wrapDeadlineSetupError("active read", conn.SetReadDeadline(time.Now().Add(timeout)))
}

func (s *ICAPServer) waitReadTimeout(keepAliveWait bool) time.Duration {
	if keepAliveWait && s.config.IdleTimeout > 0 {
		return s.config.IdleTimeout
	}
	if s.config.ReadTimeout > 0 {
		return s.config.ReadTimeout
	}
	return s.config.IdleTimeout
}

func (s *ICAPServer) handleParseError(ctx context.Context, conn *Connection, err error, started, keepAliveWait bool) {
	if isDeadlineSetupError(err) {
		s.logConnectionError(
			ctx, nil, errorStageSetDeadline, "deadline_setup_failed", conn.RemoteAddr(), err,
		)
		return
	}
	if !started && (errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
		return
	}
	if isNetTimeout(err) && !started && keepAliveWait && s.config.IdleTimeout > 0 {
		s.logger.Warn("connection closed due to idle timeout",
			"remote_addr", extractPeerIP(conn.RemoteAddr()),
			"idle_duration", time.Since(conn.LastActivity()),
			"idle_timeout", conn.config.IdleTimeout,
		)
		if metricsCollector, metricsServer := s.metricsSnapshot(); metricsCollector != nil {
			metricsCollector.RecordIdleConnectionClosedForServer(metricsServer, "idle")
		}
		return
	}
	s.logConnectionError(
		ctx, nil, errorStageParseRequest,
		parseErrorCloseReason(err, started, keepAliveWait), conn.RemoteAddr(), err,
	)
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func responseHasConnectionClose(resp *icap.Response) bool {
	if resp == nil {
		return false
	}
	connHeader, ok := resp.GetHeader("Connection")
	return ok && headerValueHasToken(connHeader, "close")
}

func shouldCloseAfterResponse(resp *icap.Response) bool {
	return resp.CloseAfterWrite() || responseHasConnectionClose(resp)
}

func headerHasToken(headers icap.Header, key, token string) bool {
	value, ok := headers.Get(key)
	return ok && headerValueHasToken(value, token)
}

func headerValueHasToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func (s *ICAPServer) drainRequestBodies(conn *Connection, req *icap.Request) error {
	if err := s.setDrainReadDeadline(conn); err != nil {
		return err
	}
	return drainRequestBodies(req, s.drainBodyLimit())
}

func (s *ICAPServer) receiveRequestBodies(conn *Connection, req *icap.Request) error {
	if shouldDeferBodyReceive(req) {
		return s.receivePreviewBody(conn, req)
	}
	if err := s.setActiveReadDeadline(conn); err != nil {
		return err
	}
	return receiveRequestBodies(req, s.drainBodyLimit())
}

func (s *ICAPServer) receivePreviewBody(conn *Connection, req *icap.Request) error {
	message := previewBodyMessage(req)
	if message == nil || message.IsBodyLoaded() || message.BodyReader == nil {
		return nil
	}
	reader, ok := message.BodyReader.(*icap.ChunkedReader)
	if !ok {
		return fmt.Errorf("%w: preview body is not chunked", errInvalidPreviewBody)
	}
	if err := s.setActiveReadDeadline(conn); err != nil {
		return err
	}
	reader.EnablePreview()
	limit := int64(req.Preview)
	if bodyLimit := s.drainBodyLimit(); bodyLimit > 0 && bodyLimit < limit {
		limit = bodyLimit
	}
	preview, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return fmt.Errorf("receiving preview body: %w", err)
	}
	if int64(len(preview)) > limit {
		return fmt.Errorf("%w: received more than %d preview bytes", errInvalidPreviewBody, limit)
	}
	atBoundary, ieof := reader.PreviewBoundary()
	if !atBoundary {
		return fmt.Errorf("%w: missing preview terminator", errInvalidPreviewBody)
	}
	if len(preview) > req.Preview {
		return fmt.Errorf("%w: received %d bytes, declared %d", errInvalidPreviewBody, len(preview), req.Preview)
	}
	if ieof {
		message.SetLoadedBody(preview)
		return nil
	}
	continuation := &previewContinuationReader{server: s, conn: conn, reader: reader}
	message.BodyReader = newDeferredPreviewBodyReader(preview, continuation)
	return nil
}

func setPreviewContinuationContext(ctx context.Context, req *icap.Request) {
	message := previewBodyMessage(req)
	if message == nil {
		return
	}
	reader, ok := message.BodyReader.(*deferredPreviewBodyReader)
	if ok {
		reader.continuation.ctx = ctx
	}
}

func previewBodyMessage(req *icap.Request) *icap.HTTPMessage {
	if req == nil {
		return nil
	}
	if req.Method == icap.MethodREQMOD {
		return req.HTTPRequest
	}
	if req.Method == icap.MethodRESPMOD {
		return req.HTTPResponse
	}
	return nil
}

func (s *ICAPServer) setDrainReadDeadline(conn *Connection) error {
	timeout := s.config.ReadTimeout
	if timeout <= 0 {
		timeout = defaultDrainTimeout
	}
	return wrapDeadlineSetupError("drain read", conn.SetReadDeadline(time.Now().Add(timeout)))
}

func (s *ICAPServer) drainBodyLimit() int64 {
	return s.config.EffectiveMaxBodySize(defaultDrainBodyLimit)
}

func shouldDeferBodyReceive(req *icap.Request) bool {
	return req != nil && req.IsPreviewMode()
}

func shouldDrainAfterResponse(req *icap.Request) bool {
	return !shouldDeferBodyReceive(req) || requestBodiesLoaded(req)
}

func requestBodiesLoaded(req *icap.Request) bool {
	return (req.HTTPRequest == nil || req.HTTPRequest.IsBodyLoaded()) &&
		(req.HTTPResponse == nil || req.HTTPResponse.IsBodyLoaded()) && req.IsBodyLoaded()
}

func drainRequestBodies(req *icap.Request, maxBytes int64) error {
	hadHTTPBodyReader := hasHTTPBodyReader(req)
	if err := drainHTTPMessageBody(req.HTTPRequest, maxBytes, "HTTP request body"); err != nil {
		return err
	}
	if err := drainHTTPMessageBody(req.HTTPResponse, maxBytes, "HTTP response body"); err != nil {
		return err
	}
	if !hadHTTPBodyReader && shouldDrainRawBody(req) {
		return drainRawChunkedBody(req, maxBytes)
	}
	return nil
}

func receiveRequestBodies(req *icap.Request, maxBytes int64) error {
	hadHTTPBodyReader := hasHTTPBodyReader(req)
	if err := receiveHTTPMessageBody(req.HTTPRequest, maxBytes, "HTTP request body"); err != nil {
		return err
	}
	if err := receiveHTTPMessageBody(req.HTTPResponse, maxBytes, "HTTP response body"); err != nil {
		return err
	}
	if !hadHTTPBodyReader && shouldDrainRawBody(req) {
		return receiveRawChunkedBody(req, maxBytes)
	}
	return nil
}

func hasHTTPBodyReader(req *icap.Request) bool {
	return (req.HTTPRequest != nil && req.HTTPRequest.BodyReader != nil) ||
		(req.HTTPResponse != nil && req.HTTPResponse.BodyReader != nil)
}

func drainHTTPMessageBody(message *icap.HTTPMessage, maxBytes int64, name string) error {
	if message == nil || message.BodyReader == nil {
		return nil
	}
	if err := drainLimited(message.BodyReader, maxBytes, name); err != nil {
		return err
	}
	message.BodyReader = nil
	return nil
}

func receiveHTTPMessageBody(message *icap.HTTPMessage, maxBytes int64, name string) error {
	if message == nil || message.IsBodyLoaded() || message.BodyReader == nil {
		return nil
	}
	body, err := loadHTTPMessageBody(message, maxBytes)
	if err != nil {
		return fmt.Errorf("receiving %s: %w", name, err)
	}
	message.SetLoadedBody(body)
	return nil
}

func loadHTTPMessageBody(message *icap.HTTPMessage, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return message.GetBody()
	}
	return message.GetBodyLimited(maxBytes)
}

func shouldDrainRawBody(req *icap.Request) bool {
	return req.BodyReader != nil && req.HTTPRequest == nil && req.HTTPResponse == nil &&
		(req.Encapsulated.HasReqBody() || req.Encapsulated.HasResBody())
}

func drainRawChunkedBody(req *icap.Request, maxBytes int64) error {
	reader := req.BodyReader
	if _, ok := reader.(*icap.ChunkedReader); !ok {
		reader = icap.NewChunkedReader(reader)
	}
	if err := drainLimited(reader, maxBytes, "raw ICAP body"); err != nil {
		return err
	}
	req.BodyReader = nil
	return nil
}

func receiveRawChunkedBody(req *icap.Request, maxBytes int64) error {
	if req.IsBodyLoaded() || req.BodyReader == nil {
		return nil
	}
	reader := req.BodyReader
	if _, ok := reader.(*icap.ChunkedReader); !ok {
		reader = icap.NewChunkedReader(reader)
	}
	body, err := readLimitedBody(reader, maxBytes)
	if err != nil {
		return fmt.Errorf("receiving raw ICAP body: %w", err)
	}
	req.SetLoadedBody(body)
	return nil
}

func readLimitedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("%w: max %d bytes", ErrBodyTooLarge, maxBytes)
	}
	return body, nil
}

func drainLimited(reader io.Reader, maxBytes int64, name string) error {
	if maxBytes <= 0 {
		_, err := io.Copy(io.Discard, reader)
		return err
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("draining %s: %w", name, err)
	}
	if limited.N == 0 {
		return fmt.Errorf("%w while draining %s: max %d bytes", ErrBodyTooLarge, name, maxBytes)
	}
	return nil
}
