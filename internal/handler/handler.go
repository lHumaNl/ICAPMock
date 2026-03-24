// Package handler provides ICAP request handlers for the ICAP Mock Server.
// It defines the Handler interface and implementations for REQMOD, RESPMOD, and OPTIONS methods.
//
// The Handler interface is the core abstraction for processing ICAP requests:
//
//	type Handler interface {
//	    Handle(ctx context.Context, req *icap.Request) (*icap.Response, error)
//	    Method() string
//	}
//
// Available handlers:
//   - ReqmodHandler: Handles REQMOD requests (HTTP request modification)
//   - RespmodHandler: Handles RESPMOD requests (HTTP response modification)
//   - OptionsHandler: Handles OPTIONS requests (server capabilities)
//
// Example usage:
//
//	previewRateLimiter := handler.NewPreviewRateLimiter(
//	    handler.PreviewRateLimiterConfig{
//	        Enabled:       true,
//	        MaxRequests:   100,
//	        WindowSeconds: 60,
//	    },
//	    metrics, logger,
//	)
//	handler := handler.NewReqmodHandler(processor, metrics, logger, previewRateLimiter)
//	resp, err := handler.Handle(ctx, req)
//	if err != nil {
//	    // handle error
//	}
package handler

import (
	"context"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Handler defines the interface for ICAP request handlers.
// Implementations handle incoming ICAP requests for specific methods
// and return appropriate responses.
//
// All implementations must be thread-safe as they may be called concurrently
// from multiple goroutines.
type Handler interface {
	// Handle processes an ICAP request and returns a response.
	// The context can be used for cancellation and timeout handling.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadline propagation
	//   - req: The ICAP request to process
	//
	// Returns:
	//   - resp: The ICAP response (may be nil if an error occurs)
	//   - err: An error if processing failed
	Handle(ctx context.Context, req *icap.Request) (*icap.Response, error)

	// Method returns the ICAP method this handler processes.
	// Valid values are "REQMOD", "RESPMOD", and "OPTIONS".
	Method() string
}

// HandlerFunc is an adapter type that allows using ordinary functions as Handlers.
// This is useful for simple handlers, middleware, or testing.
//
// Example:
//
//	hf := handler.HandlerFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	    return icap.NewResponse(204), nil
//	}, "REQMOD")
type HandlerFunc func(ctx context.Context, req *icap.Request) (*icap.Response, error)

// Handle implements the Handler interface for HandlerFunc.
// It simply calls the underlying function.
func (hf HandlerFunc) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return hf(ctx, req)
}

// methodHandler wraps a HandlerFunc with a method string.
type methodHandler struct {
	HandlerFunc
	method string
}

// Method returns the ICAP method for this handler.
func (h *methodHandler) Method() string {
	return h.method
}

// WrapHandler wraps a HandlerFunc with a method name to create a full Handler.
// This is a convenience function for creating handlers from functions.
//
// Example:
//
//	h := handler.WrapHandler(myHandlerFunc, "REQMOD")
func WrapHandler(hf HandlerFunc, method string) Handler {
	return &methodHandler{
		HandlerFunc: hf,
		method:      method,
	}
}

// chainHandler chains multiple handlers together.
type chainHandler struct {
	handlers []Handler
	method   string
}

// Handle implements the Handler interface for chainHandler.
// It calls each handler in order until one returns a non-nil response.
// If all handlers return nil, a default 204 response is returned.
func (c *chainHandler) Handle(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	for _, h := range c.handlers {
		resp, err := h.Handle(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil
		}
	}
	// Default response when no handler returns a response
	return icap.NewResponse(icap.StatusNoContentNeeded), nil
}

// Method returns the ICAP method for this chain.
func (c *chainHandler) Method() string {
	return c.method
}

// Chain creates a handler that chains multiple handlers together.
// Handlers are called in order until one returns a non-nil response.
// If all handlers return nil, a default 204 response is returned.
//
// This is useful for building processing pipelines where multiple
// handlers may process a request.
//
// Example:
//
//	chain := handler.Chain(authHandler, rateLimitHandler, mainHandler)
func Chain(handlers ...Handler) Handler {
	method := ""
	if len(handlers) > 0 {
		method = handlers[0].Method()
	}
	return &chainHandler{
		handlers: handlers,
		method:   method,
	}
}

// Middleware is a function that wraps a Handler to add functionality.
// Middleware can be used for logging, metrics, authentication, etc.
//
// Example:
//
//	func LoggingMiddleware(logger *slog.Logger) handler.Middleware {
//	    return func(next handler.Handler) handler.Handler {
//	        return handler.WrapHandler(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	            start := time.Now()
//	            resp, err := next.Handle(ctx, req)
//	            logger.Info("request processed", "duration", time.Since(start))
//	            return resp, err
//	        }, next.Method())
//	    }
//	}
type Middleware func(Handler) Handler

// Use applies middleware to a handler in the order they are provided.
// The first middleware is applied first, so it will be the outermost.
//
// Example:
//
//	h := handler.Use(baseHandler, loggingMiddleware, metricsMiddleware)
func Use(h Handler, middlewares ...Middleware) Handler {
	// Apply in reverse order so first middleware is outermost
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
