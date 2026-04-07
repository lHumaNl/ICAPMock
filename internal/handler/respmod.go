// Copyright 2026 ICAP Mock

package handler

import (
	"context"
	"log/slog"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// RespmodHandler handles ICAP RESPMOD requests.
// RESPMOD is used to modify HTTP responses before they are returned to the client.
//
// The handler delegates request processing to a Processor and records metrics
// for each request handled.
type RespmodHandler struct {
	baseHandler
}

// NewRespmodHandler creates a new RespmodHandler with the given dependencies.
//
// Parameters:
//   - proc: The processor to use for handling requests (must not be nil)
//   - m: The metrics collector for recording request metrics (can be nil)
//   - logger: The logger for structured logging (can be nil)
//   - previewRateLimiter: The preview rate limiter (can be nil)
//
// Returns a new RespmodHandler instance.
//
// Example:
//
//	h := handler.NewRespmodHandler(processor, metricsCollector, logger, previewRateLimiter)
func NewRespmodHandler(proc processor.Processor, m *metrics.Collector, logger *slog.Logger, previewRateLimiter *PreviewRateLimiter) *RespmodHandler {
	return &RespmodHandler{
		baseHandler: newBaseHandler(icap.MethodRESPMOD, proc, m, logger, previewRateLimiter),
	}
}

// Handle processes a RESPMOD request and returns the modified response.
// It delegates to the processor and records metrics for the operation.
//
// The handler performs the following steps:
//  1. Checks for context cancellation before processing
//  2. Records request start metrics
//  3. Handles preview mode if requested (RFC 3507 Section 4.6)
//  4. Delegates to the processor
//  5. Checks for context cancellation AFTER processor.Process() (P0 fix)
//  6. Records request completion metrics and cancellation if needed
//  7. Returns the processor response or cancellation error
//
// Error handling:
//   - Returns context.Canceled or context.DeadlineExceeded if context is canceled
//   - Returns ErrNilProcessor if the processor is nil
//   - Propagates processor errors
func (h *RespmodHandler) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return h.handle(ctx, req)
}
