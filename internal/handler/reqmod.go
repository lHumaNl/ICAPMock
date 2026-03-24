// Package handler provides ICAP request handlers for the ICAP Mock Server.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// ErrNilProcessor is returned when a handler is created with a nil processor.
var ErrNilProcessor = errors.New("processor cannot be nil")

// ErrNilRequest is returned when Handle is called with a nil request.
var ErrNilRequest = errors.New("request cannot be nil")

// ReqmodHandler handles ICAP REQMOD requests.
// REQMOD is used to modify HTTP requests before they are sent to the origin server.
//
// The handler delegates request processing to a Processor and records metrics
// for each request handled.
type ReqmodHandler struct {
	baseHandler
}

// NewReqmodHandler creates a new ReqmodHandler with the given dependencies.
//
// Parameters:
//   - proc: The processor to use for handling requests (must not be nil)
//   - m: The metrics collector for recording request metrics (can be nil)
//   - logger: The logger for structured logging (can be nil)
//   - previewRateLimiter: The preview rate limiter (can be nil)
//
// Returns a new ReqmodHandler instance.
//
// Example:
//
//	h := handler.NewReqmodHandler(processor, metricsCollector, logger, previewRateLimiter)
func NewReqmodHandler(proc processor.Processor, m *metrics.Collector, logger *slog.Logger, previewRateLimiter *PreviewRateLimiter) *ReqmodHandler {
	return &ReqmodHandler{
		baseHandler: newBaseHandler(icap.MethodREQMOD, proc, m, logger, previewRateLimiter),
	}
}

// Handle processes a REQMOD request and returns the modified response.
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
//   - Returns context.Canceled or context.DeadlineExceeded if context is cancelled
//   - Returns ErrNilProcessor if the processor is nil
//   - Propagates processor errors
func (h *ReqmodHandler) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return h.handle(ctx, req)
}
