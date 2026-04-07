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

	// Check for context cancellation first
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Record request start
	if h.getMetrics() != nil {
		h.getMetrics().IncRequestsInFlight(h.method)
		defer h.getMetrics().DecRequestsInFlight(h.method)
	}

	// Check for nil processor
	if h.getProcessor() == nil {
		if h.getMetrics() != nil {
			h.getMetrics().RecordError("nil_processor")
		}
		return nil, ErrNilProcessor
	}

	// Handle preview mode (RFC 3507 Section 4.6)
	if req.IsPreviewMode() {
		if h.logger != nil {
			h.logger.DebugContext(ctx, fmt.Sprintf("processing %s request in preview mode", h.method),
				"request_id", util.RequestIDFromContext(ctx),
				"preview_bytes", req.Preview,
			)
		}

		// Check preview rate limit
		if h.previewRateLimiter != nil {
			exceeded, remaining, resetIn := h.previewRateLimiter.CheckLimit(req)
			if exceeded {
				// Return 429 Too Many Requests with appropriate headers
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
				return resp, nil
			}
		}

		// Record preview metrics
		if h.getMetrics() != nil {
			h.getMetrics().RecordPreviewRequest(h.method, true)
		}

		// Process the preview
		resp, err := h.getProcessor().Process(ctx, req)

		// Check for context cancellation after processing
		if err == nil && ctx.Err() != nil {
			reason, ctxErr := util.CheckCancellation(ctx)
			if h.logger != nil {
				h.logger.WarnContext(ctx, "preview request context canceled after processing",
					"request_id", util.RequestIDFromContext(ctx),
					"reason", reason,
					"error", ctxErr,
				)
			}
			if h.getMetrics() != nil {
				h.getMetrics().RecordRequestContextCancellation(h.method, string(reason))
				h.getMetrics().RecordRequestCancellation(h.method)
			}
			return nil, ctxErr
		}

		// For preview mode: return 204 if no modification needed
		// If processor returned no response or response indicates no modification, return 204
		if err == nil && resp != nil {
			if resp.StatusCode == icap.StatusNoContentNeeded {
				// Processor already returned 204, use as-is
				if h.logger != nil {
					h.logger.DebugContext(ctx, "preview request returned 204 No Content Needed",
						"request_id", util.RequestIDFromContext(ctx),
					)
				}
				return resp, nil
			}

			// Check if the processor modified anything
			// If no body was added/modified and status is 200, we should return 204
			if !h.isModifiedResponse(resp) {
				// Return 204 (No Content Needed) for unmodified preview
				if h.logger != nil {
					h.logger.DebugContext(ctx, "preview body unmodified, returning 204",
						"request_id", util.RequestIDFromContext(ctx),
					)
				}
				return icap.NewResponse(icap.StatusNoContentNeeded), nil
			}

			// Return 200 with modified preview body
			if h.logger != nil {
				h.logger.DebugContext(ctx, "preview body modified, returning 200",
					"request_id", util.RequestIDFromContext(ctx),
				)
			}
			return resp, nil
		}

		// Record preview metrics
		duration := time.Since(start)
		if h.getMetrics() != nil {
			h.getMetrics().RecordRequest(h.method)
			h.getMetrics().RecordRequestDuration(h.method, duration)

			if err != nil {
				h.getMetrics().RecordError("preview_processing_error")
			}

			if resp != nil && len(resp.Body) > 0 {
				h.getMetrics().RecordResponseSize(h.method, int64(len(resp.Body)))
			}
		}

		return resp, err
	}

	// Record non-preview metrics
	if h.getMetrics() != nil {
		h.getMetrics().RecordPreviewRequest(h.method, false)
	}

	// Process the request
	resp, err := h.getProcessor().Process(ctx, req)

	// Check for context cancellation AFTER processor.Process
	if err == nil && ctx.Err() != nil {
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
		return nil, ctxErr
	}

	// Record metrics
	duration := time.Since(start)
	if h.getMetrics() != nil {
		h.getMetrics().RecordRequest(h.method)
		h.getMetrics().RecordRequestDuration(h.method, duration)

		if err != nil {
			h.getMetrics().RecordError("processing_error")
		}

		if resp != nil && len(resp.Body) > 0 {
			h.getMetrics().RecordResponseSize(h.method, int64(len(resp.Body)))
		}
	}

	return resp, err
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
