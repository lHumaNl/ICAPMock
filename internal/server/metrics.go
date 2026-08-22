// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"strconv"
	"time"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func (s *ICAPServer) recordIncomingRequest(ctx context.Context, req *icap.Request, resp *icap.Response) {
	s.recordIncomingRequestOutcome(ctx, req, resp, effectiveRequestOutcome(ctx, resp))
}

func (s *ICAPServer) recordTerminalMetrics(
	ctx context.Context,
	req *icap.Request,
	resp *icap.Response,
	outcome string,
	duration time.Duration,
) {
	metricsCollector, metricsServer := s.metricsSnapshot()
	if metricsCollector == nil || req == nil {
		return
	}
	contentTypeLabel, metadata := s.requestMetricSnapshot(ctx, req, resp)
	s.recordIncomingRequestWithLabel(req, contentTypeLabel, outcome, metadata)
	metricsCollector.RecordScenarioResponseDurationForServer(
		metricsServer,
		req.Method,
		contentTypeLabel,
		outcome,
		metadata.scenario,
		metadata.response,
		duration,
	)
}

func (s *ICAPServer) requestMetricSnapshot(
	ctx context.Context,
	req *icap.Request,
	resp *icap.Response,
) (string, requestMetricLabels) {
	contentTypeLabel, ok := requestinfo.ContextContentTypeLabel(ctx)
	if !ok {
		contentTypeLabel = s.canonicalContentTypeLabel(req)
	}
	return contentTypeLabel, requestMetricMetadata(ctx, resp)
}

func (s *ICAPServer) recordIncomingRequestOutcome(
	ctx context.Context,
	req *icap.Request,
	resp *icap.Response,
	outcome string,
) {
	metricsCollector, _ := s.metricsSnapshot()
	if metricsCollector == nil || req == nil {
		return
	}
	contentTypeLabel, ok := requestinfo.ContextContentTypeLabel(ctx)
	if !ok {
		contentTypeLabel = s.canonicalContentTypeLabel(req)
	}
	metadata := requestMetricMetadata(ctx, resp)
	s.recordIncomingRequestWithLabel(req, contentTypeLabel, outcome, metadata)
}

func (s *ICAPServer) recordIncomingRequestWithLabel(
	req *icap.Request,
	contentTypeLabel string,
	outcome string,
	metadata requestMetricLabels,
) {
	metricsCollector, metricsServer := s.metricsSnapshot()
	if metricsCollector == nil || req == nil {
		return
	}
	metricsCollector.RecordRequestForServerWithContentTypeLabel(
		metricsServer,
		req.Method,
		contentTypeLabel,
		outcome,
		metadata.response,
		metadata.scenario,
	)
}

type requestMetricLabels struct {
	response string
	scenario string
}

func requestMetricMetadata(ctx context.Context, resp *icap.Response) requestMetricLabels {
	if metadata, ok := requestinfo.ContextScenarioMetadata(ctx); ok {
		return requestMetricLabels{response: metadata.Response, scenario: metadata.Scenario}
	}
	return requestMetricLabels{response: responseMetricLabel(resp), scenario: metricsinternal.NoScenarioMetricLabel}
}

func responseMetricLabel(resp *icap.Response) string {
	if resp == nil {
		return metricsinternal.OutcomeError
	}
	return strconv.Itoa(resp.StatusCode)
}

func (s *ICAPServer) canonicalContentTypeLabel(req *icap.Request) string {
	contentType := requestinfo.ContentType(req)
	if metricsCollector, _ := s.metricsSnapshot(); metricsCollector != nil {
		return metricsCollector.ContentTypeLabel(contentType)
	}
	return metricsinternal.NormalizeContentTypeLabel(contentType)
}

func requestOutcome(resp *icap.Response) string {
	if resp == nil || resp.StatusCode >= icap.StatusBadRequest {
		return metricsinternal.OutcomeError
	}
	if encapsulatedHTTPStatusCode(resp) >= icap.StatusBadRequest {
		return metricsinternal.OutcomeBlocked
	}
	return metricsinternal.OutcomeAllowed
}

func encapsulatedHTTPStatusCode(resp *icap.Response) int {
	if resp == nil || resp.HTTPResponse == nil {
		return 0
	}
	statusCode, err := strconv.Atoi(resp.HTTPResponse.Status)
	if err != nil {
		return 0
	}
	return statusCode
}
