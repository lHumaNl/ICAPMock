// Copyright 2026 ICAP Mock

package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	defaultDrainBodyLimit = 10 * 1024 * 1024
	defaultDrainTimeout   = 30 * time.Second
)

type requestDeadlineReader struct {
	reader    BufferedReader
	activate  func() error
	started   bool
	activated bool
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
	if err := r.activate(); err != nil && readErr == nil {
		return err
	}
	return readErr
}

func (s *ICAPServer) setWaitReadDeadline(conn *Connection, keepAliveWait bool) error {
	timeout := s.waitReadTimeout(keepAliveWait)
	if timeout <= 0 {
		return conn.SetReadDeadline(time.Time{})
	}
	return conn.SetReadDeadline(time.Now().Add(timeout))
}

func (s *ICAPServer) setActiveReadDeadline(conn *Connection) error {
	if s.config.ReadTimeout <= 0 {
		return conn.SetReadDeadline(time.Time{})
	}
	return conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
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

func (s *ICAPServer) handleParseError(conn *Connection, err error, started, keepAliveWait bool) {
	if !isNetTimeout(err) || started || !keepAliveWait || s.config.IdleTimeout <= 0 {
		return
	}
	s.logger.Warn("connection closed due to idle timeout",
		"remote_addr", conn.RemoteAddr(),
		"idle_duration", time.Since(conn.LastActivity()),
		"idle_timeout", conn.config.IdleTimeout,
	)
	if s.metrics != nil {
		s.metrics.RecordIdleConnectionClosedForServer(s.metricsServerName, "idle")
	}
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
		return nil
	}
	if err := s.setActiveReadDeadline(conn); err != nil {
		return err
	}
	return receiveRequestBodies(req, s.drainBodyLimit())
}

func (s *ICAPServer) setDrainReadDeadline(conn *Connection) error {
	timeout := s.config.ReadTimeout
	if timeout <= 0 {
		timeout = defaultDrainTimeout
	}
	return conn.SetReadDeadline(time.Now().Add(timeout))
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
