// Copyright 2026 ICAP Mock

package processor

import (
	"context"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func (p *MockProcessor) logScenarioMatched(
	ctx context.Context,
	req *icap.Request,
	scenario string,
	response *storage.ResponseTemplate,
) {
	if p.logger == nil || req == nil || response == nil {
		return
	}
	attrs := []any{
		"server", p.server,
		"scenario", scenario,
		"response", scenarioResponseLabel(response),
		"method", req.Method,
		"uri", req.URI,
		"content_type", requestinfo.ContentTypeLabel(ctx, req),
	}
	if req.ClientIP != "" {
		attrs = append(attrs, "client_ip", req.ClientIP)
	}
	p.logger.Info("scenario matched", attrs...)
}

func (p *MockProcessor) logProcessorError(
	ctx context.Context,
	req *icap.Request,
	stage string,
	errorType string,
	scenario string,
	response string,
	err error,
) {
	if p.logger == nil || req == nil || err == nil {
		return
	}
	p.logger.ErrorContext(ctx, "scenario processing error", p.processorErrorAttrs(
		ctx, req, stage, errorType, scenario, response, err,
	)...)
}

func (p *MockProcessor) recordRequestError(
	req *icap.Request,
	stage string,
	errorType string,
	scenario string,
	response string,
) {
	if p.metrics == nil || req == nil {
		return
	}
	p.metrics.RecordRequestErrorForServer(p.server, req.Method, stage, errorType, scenario, response)
}

func (p *MockProcessor) processorErrorAttrs(
	ctx context.Context,
	req *icap.Request,
	stage string,
	errorType string,
	scenario string,
	response string,
	err error,
) []any {
	attrs := []any{
		"server", p.server,
		"stage", stage,
		"error_type", errorType,
		"method", req.Method,
		"uri", req.URI,
		"content_type", requestinfo.ContentTypeLabel(ctx, req),
		"error", err.Error(),
	}
	attrs = appendOptionalProcessorErrorAttrs(ctx, attrs, req, scenario, response)
	return attrs
}

func appendOptionalProcessorErrorAttrs(
	ctx context.Context,
	attrs []any,
	req *icap.Request,
	scenario string,
	response string,
) []any {
	if requestID := util.RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	if req.ClientIP != "" {
		attrs = append(attrs, "client_ip", req.ClientIP)
	}
	if scenario != metrics.NoScenarioMetricLabel && scenario != "" {
		attrs = append(attrs, "scenario", scenario)
	}
	if response != "" {
		attrs = append(attrs, "response", response)
	}
	return attrs
}
