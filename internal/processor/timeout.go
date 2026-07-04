// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// WithTimeout returns a processor that applies timeout to context-aware work.
func WithTimeout(next Processor, timeout time.Duration) Processor {
	if next == nil || timeout <= 0 {
		return next
	}
	return &timeoutProcessor{next: next, timeout: timeout}
}

type timeoutProcessor struct {
	next    Processor
	timeout time.Duration
}

func (p *timeoutProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.next.Process(timeoutCtx, req)
}

func (p *timeoutProcessor) Name() string {
	return "Timeout(" + p.next.Name() + ")"
}
