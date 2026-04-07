// Copyright 2026 ICAP Mock

package processor

import (
	"context"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// EchoProcessor is a simple pass-through processor that returns
// ICAP 204 No Content Needed for all requests.
// This indicates to the ICAP client that the original message
// should not be modified.
//
// EchoProcessor is useful for:
//   - Testing ICAP connectivity
//   - Baseline performance measurements
//   - Implementing "pass-through" mode
//
// EchoProcessor is thread-safe and can be used concurrently.
type EchoProcessor struct{}

// NewEchoProcessor creates a new EchoProcessor instance.
// The returned processor always returns 204 No Content Needed.
func NewEchoProcessor() *EchoProcessor {
	return &EchoProcessor{}
}

// Process handles the ICAP request by returning 204 No Content Needed.
// This implements the pass-through mode where the original HTTP message
// is not modified.
//
// The request is always handled successfully (no error is returned).
func (p *EchoProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	return &icap.Response{
		StatusCode: icap.StatusNoContentNeeded,
		Proto:      icap.Version,
		Header:     icap.NewHeader(),
	}, nil
}

// Name returns "EchoProcessor" as the processor name.
func (p *EchoProcessor) Name() string {
	return "EchoProcessor"
}
