// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// baseHandler contains the shared logic for REQMOD and RESPMOD handlers.
// It is not exported; ReqmodHandler and RespmodHandler embed it.
type baseHandler struct {
	processorVal       atomic.Value
	metricsVal         atomic.Value
	logger             *slog.Logger
	previewRateLimiter *PreviewRateLimiter
	method             string
}

func newBaseHandler(method string, proc processor.Processor, m *metrics.Collector, logger *slog.Logger, previewRateLimiter *PreviewRateLimiter) baseHandler {
	h := baseHandler{
		method:             method,
		logger:             logger,
		previewRateLimiter: previewRateLimiter,
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if h.getMetrics() != nil {
		h.getMetrics().IncRequestsInFlight(h.method)
		defer h.getMetrics().DecRequestsInFlight(h.method)
	}

	if h.getProcessor() == nil {
		if h.getMetrics() != nil {
			h.getMetrics().RecordError("nil_processor")
		}
		return nil, ErrNilProcessor
	}

	if req.IsPreviewMode() {
		return h.handlePreview(ctx, req, start)
	}

	return h.handleNonPreview(ctx, req, start)
}

// handlePreview processes preview mode requests (RFC 3507 Section 4.6).
func (h *baseHandler) handlePreview(ctx context.Context, req *icap.Request, start time.Time) (*icap.Response, error) {
	if h.logger != nil {
		h.logger.DebugContext(ctx, fmt.Sprintf("processing %s request in preview mode", h.method),
			"request_id", util.RequestIDFromContext(ctx),
			"preview_bytes", req.Preview,
		)
	}

	// Check preview rate limit
	if h.previewRateLimiter != nil {
		if resp := h.checkPreviewRateLimit(ctx, req); resp != nil {
			return resp, nil
		}
	}

	if h.getMetrics() != nil {
		h.getMetrics().RecordPreviewRequest(h.method, true)
	}

	resp, err := h.getProcessor().Process(ctx, req)

	if cancelErr := h.checkPostProcessCancellation(ctx, err); cancelErr != nil {
		return nil, cancelErr
	}

	if err == nil && resp != nil {
		return h.resolvePreviewResponse(ctx, resp)
	}

	h.recordRequestMetrics(start, resp, err, "preview_processing_error")
	return resp, err
}

// checkPreviewRateLimit checks preview rate limit and returns a 429 response if exceeded, or nil if OK.
func (h *baseHandler) checkPreviewRateLimit(ctx context.Context, req *icap.Request) *icap.Response {
	exceeded, remaining, resetIn := h.previewRateLimiter.CheckLimit(req)
	if !exceeded {
		return nil
	}
	resp := icap.NewResponse(icap.StatusServiceUnavailable)
	resp.SetHeader("Retry-After", fmt.Sprintf("%d", int(resetIn.Seconds())))
	resp.SetHeader("X-RateLimit-Limit", fmt.Sprintf("%d", h.previewRateLimiter.config.MaxRequests))
	resp.SetHeader("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	resp.SetHeader("X-RateLimit-Reset", fmt.Sprintf("%d", int(resetIn.Seconds())))

	if h.logger != nil {
		h.logger.WarnContext(ctx, "preview rate limit exceeded, returning 429",
			"request_id", util.RequestIDFromContext(ctx),
			"remaining", remaining,
			"reset_in_seconds", resetIn.Seconds(),
		)
	}
	return resp
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
func (h *baseHandler) handleNonPreview(ctx context.Context, req *icap.Request, start time.Time) (*icap.Response, error) {
	if h.getMetrics() != nil {
		h.getMetrics().RecordPreviewRequest(h.method, false)
	}

	resp, err := h.getProcessor().Process(ctx, req)

	if cancelErr := h.checkPostProcessCancellation(ctx, err); cancelErr != nil {
		return nil, cancelErr
	}

	h.recordRequestMetrics(start, resp, err, "processing_error")
	return resp, err
}

// checkPostProcessCancellation checks if context was canceled after processing.
// Returns a cancellation error if canceled, nil if not.
func (h *baseHandler) checkPostProcessCancellation(ctx context.Context, procErr error) error {
	if procErr != nil || ctx.Err() == nil {
		return nil //nolint:nilerr // procErr is handled by caller; we only add cancellation error when procErr is nil
	}
	reason, ctxErr := util.CheckCancellation(ctx)
	if h.logger != nil {
		h.logger.WarnContext(ctx, "request context canceled after processing",
			"request_id", util.RequestIDFromContext(ctx),
			"reason", reason,
			"error", ctxErr,
		)
	}
	if h.getMetrics() != nil {
		h.getMetrics().RecordRequestContextCancellation(h.method, string(reason))
		h.getMetrics().RecordRequestCancellation(h.method)
	}
	return ctxErr
}

// recordRequestMetrics records request duration, error, and response size metrics.
func (h *baseHandler) recordRequestMetrics(start time.Time, resp *icap.Response, err error, errorLabel string) {
	if h.getMetrics() == nil {
		return
	}
	duration := time.Since(start)
	h.getMetrics().RecordRequest(h.method)
	h.getMetrics().RecordRequestDuration(h.method, duration)
	if err != nil {
		h.getMetrics().RecordError(errorLabel)
	}
	if resp != nil && len(resp.Body) > 0 {
		h.getMetrics().RecordResponseSize(h.method, int64(len(resp.Body)))
	}
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
