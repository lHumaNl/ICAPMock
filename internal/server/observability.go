// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	errorStageSetDeadline   = "set_deadline"
	errorStageParseRequest  = "parse_request"
	errorStageWriteResponse = metricsinternal.RequestErrorStageWriteResponse
	errorStageDrainBody     = "drain_body"
)

func (s *ICAPServer) logRequestError(ctx context.Context, req *icap.Request, stage, errorType string, err error) {
	if s.logger == nil || req == nil {
		return
	}
	s.logger.ErrorContext(ctx, "ICAP request error", s.requestErrorAttrs(ctx, req, stage, errorType, err)...)
}

func (s *ICAPServer) logConnectionError(
	ctx context.Context,
	req *icap.Request,
	stage string,
	errorType string,
	remoteAddr string,
	err error,
) {
	if s.logger == nil || err == nil {
		return
	}
	_, metricsServer := s.metricsSnapshot()
	attrs := []any{
		"server", metricsServer,
		"stage", stage,
		"error_type", errorType,
		"error", err.Error(),
		"description", errorDescription(err),
	}
	if remoteAddr != "" {
		attrs = append(attrs, "remote_addr", extractPeerIP(remoteAddr))
	}
	if req != nil {
		attrs = append(attrs,
			"method", req.Method,
			"uri", req.URI,
			"content_type", s.canonicalContentTypeLabel(req),
		)
		if req.ClientIP != "" {
			attrs = append(attrs, "client_ip", req.ClientIP)
		}
	}
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	s.logger.ErrorContext(ctx, "ICAP connection error", attrs...)
}

func errorDescription(err error) string {
	if err == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	for current := err; current != nil; current = errors.Unwrap(current) {
		parts = append(parts, current.Error())
	}
	return strings.Join(parts, "; caused by: ")
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
	metricsCollector, metricsServer := s.metricsSnapshot()
	if metricsCollector == nil || req == nil {
		return
	}
	metadata := requestMetricMetadata(ctx, nil)
	if response != "" {
		metadata.response = response
	}
	metricsCollector.RecordRequestErrorForServer(
		metricsServer,
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
	_, metricsServer := s.metricsSnapshot()
	attrs := []any{
		"server", metricsServer,
		"stage", stage,
		"error_type", errorType,
		"method", req.Method,
		"uri", req.URI,
		"content_type", s.canonicalContentTypeLabel(req),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error(), "description", errorDescription(err))
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
