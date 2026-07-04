// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
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
type EchoProcessor struct {
	metrics *metrics.Collector
	server  string
}

// NewEchoProcessor creates a new EchoProcessor instance.
// The returned processor always returns 204 No Content Needed.
func NewEchoProcessor() *EchoProcessor {
	return &EchoProcessor{server: "default"}
}

// SetMetricsForServer enables fallback scenario metrics for echo responses.
func (p *EchoProcessor) SetMetricsForServer(collector *metrics.Collector, server string) {
	p.metrics = collector
	p.server = server
}

// Process handles the ICAP request by returning 204 No Content Needed.
// This implements the pass-through mode where the original HTTP message
// is not modified.
//
// The request is always handled successfully (no error is returned).
func (p *EchoProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	start := time.Now()
	const fallbackResponse = "204"
	resp := &icap.Response{
		StatusCode: icap.StatusNoContentNeeded,
		Proto:      icap.Version,
		Header:     icap.NewHeader(),
	}
	requestinfo.SetScenarioMetadata(ctx, metrics.FallbackScenarioMetricLabel, fallbackResponse)
	if p.metrics != nil {
		p.recordFallbackDuration(ctx, req, fallbackResponse, start)
	}
	return resp, nil
}

func (p *EchoProcessor) recordFallbackDuration(
	ctx context.Context,
	req *icap.Request,
	response string,
	start time.Time,
) {
	method := ""
	if req != nil {
		method = req.Method
	}
	p.metrics.RecordScenarioResponseDurationForServer(
		p.server, method, requestinfo.ContentTypeLabel(ctx, req), metrics.OutcomeAllowed,
		metrics.FallbackScenarioMetricLabel, response, time.Since(start),
	)
}

// Name returns "EchoProcessor" as the processor name.
func (p *EchoProcessor) Name() string {
	return "EchoProcessor"
}
