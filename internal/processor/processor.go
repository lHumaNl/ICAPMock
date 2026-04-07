// Copyright 2026 ICAP Mock

package processor

import (
	"context"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Processor defines the interface for ICAP request processors.
// Implementations handle incoming ICAP requests and return appropriate responses.
//
// All implementations must be thread-safe as they may be called concurrently
// from multiple goroutines.
type Processor interface {
	// Process handles an ICAP request and returns a response.
	// The context can be used for cancellation and timeout handling.
	//
	// Parameters:
	//   - ctx: Context for cancellation and deadline propagation
	//   - req: The ICAP request to process
	//
	// Returns:
	//   - resp: The ICAP response (may be nil if an error occurs)
	//   - err: An error if processing failed
	Process(ctx context.Context, req *icap.Request) (*icap.Response, error)

	// Name returns the processor's name for logging and metrics.
	// This should be a unique, human-readable identifier.
	Name() string
}

// ProcessorFunc is an adapter type that allows using ordinary functions as Processors.
// This is useful for simple processors or testing.
//
// Example:
//
//	processor := ProcessorFunc(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
//	    return icap.NewResponse(204), nil
//	})
type ProcessorFunc func(ctx context.Context, req *icap.Request) (*icap.Response, error)

// Process implements the Processor interface for ProcessorFunc.
func (f ProcessorFunc) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return f(ctx, req)
}

// Name returns "ProcessorFunc" as the processor name.
func (f ProcessorFunc) Name() string {
	return "ProcessorFunc"
}

// Chain creates a processor that chains multiple processors together.
// Processors are called in order until one returns a non-nil response.
// If all processors return nil, a default 204 response is returned.
//
// Deprecated: Use middleware pattern instead for better flexibility.
func Chain(processors ...Processor) Processor {
	return &chainProcessor{processors: processors}
}

// chainProcessor chains multiple processors.
type chainProcessor struct {
	processors []Processor
}

// Process implements the Processor interface.
func (c *chainProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	for _, p := range c.processors {
		resp, err := p.Process(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil
		}
	}
	// Default response
	return icap.NewResponse(icap.StatusNoContentNeeded), nil
}

// Name returns "ChainProcessor" as the processor name.
func (c *chainProcessor) Name() string {
	return "ChainProcessor"
}
