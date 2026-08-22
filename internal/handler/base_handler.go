// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// baseHandler contains the shared logic for REQMOD and RESPMOD handlers.
// It is not exported; ReqmodHandler and RespmodHandler embed it.
type baseHandler struct {
	processorVal atomic.Value
	metricsVal   atomic.Value
	logger       *slog.Logger
	method       string
	server       string
}

func newBaseHandlerForServer(
	server string,
	method string,
	proc processor.Processor,
	m *metrics.Collector,
	logger *slog.Logger,
) baseHandler {
	h := baseHandler{
		method: method,
		server: server,
		logger: logger,
	}
	if proc != nil {
		h.processorVal.Store(proc)
	}
	if m != nil {
		h.metricsVal.Store(m)
	}
	return h
}

func (h *baseHandler) getProcessor() processor.Processor {
	v := h.processorVal.Load()
	if v == nil {
		return nil
	}
	return v.(processor.Processor) //nolint:errcheck
}

func (h *baseHandler) getMetrics() *metrics.Collector {
	v := h.metricsVal.Load()
	if v == nil {
		return nil
	}
	return v.(*metrics.Collector) //nolint:errcheck
}

// SetProcessor allows updating the processor at runtime.
// This is useful for dynamic configuration changes.
func (h *baseHandler) SetProcessor(p processor.Processor) {
	if p != nil {
		h.processorVal.Store(p)
	}
}

// SetMetrics allows updating the metrics collector at runtime.
func (h *baseHandler) SetMetrics(m *metrics.Collector) {
	if m != nil {
		h.metricsVal.Store(m)
	}
}

// Method returns the ICAP method this handler processes.
func (h *baseHandler) Method() string {
	return h.method
}

// handle contains all the shared request-handling logic for REQMOD and RESPMOD.
func (h *baseHandler) handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	start := time.Now()
	requestMetrics := h.getMetrics()

	if err := ctx.Err(); err != nil {
		h.logContextCancellation(ctx, req, err)
		h.recordRequestError(
			ctx, requestMetrics, req,
			metrics.RequestErrorStageContext, contextCancellationType(err), metrics.OutcomeError,
		)
		return nil, err
	}

	if requestMetrics != nil {
		requestMetrics.IncRequestsProcessingInFlightForServer(h.server, h.method)
		defer requestMetrics.DecRequestsProcessingInFlightForServer(h.server, h.method)
	}

	if h.getProcessor() == nil {
		if requestMetrics != nil {
			requestMetrics.RecordErrorForServer(h.server, "nil_processor")
			h.recordRequestError(
				ctx,
				requestMetrics,
				req,
				metrics.RequestErrorStageProcessorBuild,
				metrics.RequestErrorTypeNilProcessor,
				metrics.OutcomeError,
			)
		}
		return nil, ErrNilProcessor
	}

	if req.IsPreviewMode() {
		return h.handlePreview(ctx, req, start, requestMetrics)
	}

	return h.handleNonPreview(ctx, req, start, requestMetrics)
}

// handlePreview processes preview mode requests (RFC 3507 Section 4.6).
func (h *baseHandler) handlePreview(
	ctx context.Context,
	req *icap.Request,
	start time.Time,
	requestMetrics *metrics.Collector,
) (*icap.Response, error) {
	if h.logger != nil {
		h.logger.DebugContext(ctx, fmt.Sprintf("processing %s request in preview mode", h.method),
			"request_id", util.RequestIDFromContext(ctx),
			"preview_bytes", req.Preview,
		)
	}

	if requestMetrics != nil {
		requestMetrics.RecordPreviewRequestForServer(h.server, h.method)
	}

	resp, err := h.getProcessor().Process(ctx, req)

	if cancelErr := h.checkPostProcessCancellation(ctx, requestMetrics, req, err); cancelErr != nil {
		return nil, cancelErr
	}

	if err == nil && resp != nil {
		return h.resolvePreviewResponse(ctx, resp)
	}

	h.recordRequestMetrics(requestMetrics, start, err, "preview_processing_error")
	return resp, err
}

// resolvePreviewResponse determines the appropriate response for a preview request.
func (h *baseHandler) resolvePreviewResponse(ctx context.Context, resp *icap.Response) (*icap.Response, error) {
	if resp.StatusCode == icap.StatusNoContentNeeded {
		if h.logger != nil {
			h.logger.DebugContext(ctx, "preview request returned 204 No Content Needed",
				"request_id", util.RequestIDFromContext(ctx),
			)
		}
		return resp, nil
	}

	if !h.isModifiedResponse(resp) {
		if h.logger != nil {
			h.logger.DebugContext(ctx, "preview body unmodified, returning 204",
				"request_id", util.RequestIDFromContext(ctx),
			)
		}
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}

	if h.logger != nil {
		h.logger.DebugContext(ctx, "preview body modified, returning 200",
			"request_id", util.RequestIDFromContext(ctx),
		)
	}
	return resp, nil
}

// handleNonPreview processes non-preview requests.
func (h *baseHandler) handleNonPreview(
	ctx context.Context,
	req *icap.Request,
	start time.Time,
	requestMetrics *metrics.Collector,
) (*icap.Response, error) {
	resp, err := h.getProcessor().Process(ctx, req)

	if cancelErr := h.checkPostProcessCancellation(ctx, requestMetrics, req, err); cancelErr != nil {
		return nil, cancelErr
	}

	h.recordRequestMetrics(requestMetrics, start, err, "processing_error")
	return resp, err
}

// checkPostProcessCancellation checks if context was canceled after processing.
// Returns a cancellation error if canceled, nil if not.
func (h *baseHandler) checkPostProcessCancellation(
	ctx context.Context,
	requestMetrics *metrics.Collector,
	req *icap.Request,
	procErr error,
) error {
	if procErr != nil || ctx.Err() == nil {
		return nil //nolint:nilerr // procErr is handled by caller; we only add cancellation error when procErr is nil
	}
	reason, ctxErr := util.CheckCancellation(ctx)
	h.logContextCancellation(ctx, req, ctxErr)
	if requestMetrics != nil {
		requestMetrics.RecordRequestContextCancellationForServer(h.server, h.method, string(reason))
		requestMetrics.RecordRequestCancellationForServer(h.server, h.method)
	}
	h.recordRequestError(
		ctx, requestMetrics, req,
		metrics.RequestErrorStageContext, contextCancellationType(ctxErr), metrics.OutcomeError,
	)
	return ctxErr
}

// recordRequestMetrics records request error metrics.
func (h *baseHandler) recordRequestMetrics(
	requestMetrics *metrics.Collector,
	_ time.Time,
	err error,
	errorLabel string,
) {
	if requestMetrics == nil {
		return
	}
	if err != nil {
		requestMetrics.RecordErrorForServer(h.server, errorLabel)
	}
}

func (h *baseHandler) recordRequestError(
	ctx context.Context,
	requestMetrics *metrics.Collector,
	req *icap.Request,
	stage, errorType, response string,
) {
	if requestMetrics == nil || req == nil {
		return
	}
	metadata := requestMetricMetadata(ctx)
	requestMetrics.RecordRequestErrorForServer(h.server, h.method, stage, errorType, metadata.Scenario, response)
}

func (h *baseHandler) logContextCancellation(ctx context.Context, req *icap.Request, err error) {
	if h.logger == nil || req == nil {
		return
	}
	h.logger.ErrorContext(ctx, "request context canceled",
		"request_id", util.RequestIDFromContext(ctx),
		"server", h.server,
		"stage", metrics.RequestErrorStageContext,
		"error_type", contextCancellationType(err),
		"method", req.Method,
		"uri", req.URI,
		"content_type", requestinfo.ContentTypeLabel(ctx, req),
		"client_ip", req.ClientIP,
		"error", err.Error(),
	)
}

func contextCancellationType(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return metrics.RequestErrorTypeDeadlineExceeded
	}
	return metrics.RequestErrorTypeContextCanceled
}

func requestMetricMetadata(ctx context.Context) requestinfo.ScenarioMetadata {
	metadata, ok := requestinfo.ContextScenarioMetadata(ctx)
	if !ok {
		return requestinfo.ScenarioMetadata{Scenario: metrics.NoScenarioMetricLabel, Response: metrics.OutcomeError}
	}
	return metadata
}

// isModifiedResponse checks if the response contains any modifications.
// Returns true if the response has a non-empty body or encapsulated HTTP message.
func (h *baseHandler) isModifiedResponse(resp *icap.Response) bool {
	if resp == nil {
		return false
	}
	if len(resp.Body) > 0 {
		return true
	}
	if resp.HTTPRequest != nil || resp.HTTPResponse != nil {
		return true
	}
	return false
}
