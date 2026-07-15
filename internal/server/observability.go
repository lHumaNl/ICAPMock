// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"errors"
	"io"
	"net"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func (s *ICAPServer) logRequestError(ctx context.Context, req *icap.Request, stage, errorType string, err error) {
	if s.logger == nil || req == nil {
		return
	}
	s.logger.ErrorContext(ctx, "ICAP request error", s.requestErrorAttrs(ctx, req, stage, errorType, err)...)
}

func connectionReadCloseReason(err error, keepAliveWait bool) string {
	if errors.Is(err, io.EOF) {
		return "client_closed"
	}
	if isNetTimeout(err) {
		if keepAliveWait {
			return "idle_timeout"
		}
		return "read_timeout"
	}
	return "read_error"
}

func parseErrorCloseReason(err error, started, keepAliveWait bool) string {
	if !started {
		return connectionReadCloseReason(err, keepAliveWait)
	}
	if isNetTimeout(err) {
		return "read_timeout"
	}
	if errors.Is(err, io.EOF) {
		return "client_closed_mid_request"
	}
	return "malformed_request"
}

func (s *ICAPServer) recordRequestError(ctx context.Context, req *icap.Request, stage, errorType, response string) {
	if s.metrics == nil || req == nil {
		return
	}
	metadata := requestMetricMetadata(ctx, nil)
	if response != "" {
		metadata.response = response
	}
	s.metrics.RecordRequestErrorForServer(
		s.metricsServerName,
		req.Method,
		stage,
		errorType,
		metadata.scenario,
		metadata.response,
	)
}

func (s *ICAPServer) requestErrorAttrs(
	ctx context.Context,
	req *icap.Request,
	stage string,
	errorType string,
	err error,
) []any {
	attrs := []any{
		"server", s.metricsServerName,
		"stage", stage,
		"error_type", errorType,
		"method", req.Method,
		"uri", req.URI,
		"content_type", s.canonicalContentTypeLabel(req),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	if req.ClientIP != "" {
		attrs = append(attrs, "client_ip", req.ClientIP)
	}
	if metadata, ok := requestinfo.ContextScenarioMetadata(ctx); ok {
		attrs = append(attrs, "scenario", metadata.Scenario, "response", metadata.Response)
	}
	return attrs
}

func bodyReceiveErrorType(err error) string {
	if errors.Is(err, ErrBodyTooLarge) {
		return metricsinternal.RequestErrorTypeBodyTooLarge
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return metricsinternal.RequestErrorTypeDeadlineExceeded
	}
	return metricsinternal.RequestErrorTypeBodyReceiveFailed
}
