// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"strconv"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func (s *ICAPServer) recordIncomingRequest(ctx context.Context, req *icap.Request, resp *icap.Response) {
	s.recordIncomingRequestOutcome(ctx, req, resp, requestOutcome(resp))
}

func (s *ICAPServer) recordIncomingRequestOutcome(
	ctx context.Context,
	req *icap.Request,
	resp *icap.Response,
	outcome string,
) {
	if s.metrics == nil || req == nil {
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
	if s.metrics == nil || req == nil {
		return
	}
	s.metrics.RecordRequestForServerWithContentTypeLabel(
		s.metricsServerName,
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
	if s.metrics != nil {
		return s.metrics.ContentTypeLabel(contentType)
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
