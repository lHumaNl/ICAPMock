// Copyright 2026 ICAP Mock

package server

import (
	"context"
	"sync"
	"time"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// terminalAccounting owns the exactly-once metrics for one timed request.
type terminalAccounting struct {
	ctx             context.Context
	started         time.Time
	server          *ICAPServer
	metrics         *metricsinternal.Collector
	request         *icap.Request
	metricsServer   string
	method          string
	mu              sync.Mutex
	timingStarted   bool
	finished        bool
	streamingActive bool
	requestInFlight bool
}

func newTerminalAccounting(
	ctx context.Context,
	server *ICAPServer,
	request *icap.Request,
	started time.Time,
) *terminalAccounting {
	accounting := newBaseTerminalAccounting(ctx, server, request)
	accounting.started = started
	accounting.timingStarted = true
	return accounting
}

func newPendingTerminalAccounting(
	ctx context.Context,
	server *ICAPServer,
	request *icap.Request,
) *terminalAccounting {
	return newBaseTerminalAccounting(ctx, server, request)
}

func newBaseTerminalAccounting(
	ctx context.Context,
	server *ICAPServer,
	request *icap.Request,
) *terminalAccounting {
	accounting := &terminalAccounting{server: server, ctx: ctx, request: request}
	if server == nil || request == nil {
		return accounting
	}
	accounting.metrics, accounting.metricsServer = server.metricsSnapshot()
	accounting.method = request.Method
	if accounting.metrics != nil {
		accounting.metrics.IncRequestsInFlightForServer(accounting.metricsServer, accounting.method)
		accounting.requestInFlight = true
	}
	return accounting
}

func (a *terminalAccounting) start() {
	a.startAt(time.Now())
}

func (a *terminalAccounting) startAt(started time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || a.timingStarted {
		return
	}
	a.started = started
	a.timingStarted = true
}

func (a *terminalAccounting) streamingStarted() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || a.streamingActive {
		return
	}
	a.streamingActive = true
	if a.metrics != nil {
		a.metrics.IncStreamingActiveForServer(a.metricsServer)
	}
}

func (a *terminalAccounting) finalize(response *icap.Response, outcome string) {
	wasStreaming, requestInFlight, timingStarted, started, claimed := a.claimTerminal()
	if !claimed {
		return
	}
	if wasStreaming && a.metrics != nil {
		a.metrics.DecStreamingActiveForServer(a.metricsServer)
	}
	if requestInFlight && a.metrics != nil {
		a.metrics.DecRequestsInFlightForServer(a.metricsServer, a.method)
	}
	if outcome == "" {
		outcome = effectiveRequestOutcome(a.ctx, response)
	}
	if timingStarted {
		a.server.recordTerminalMetrics(a.ctx, a.request, response, outcome, time.Since(started))
		return
	}
	a.server.recordIncomingRequestOutcome(a.ctx, a.request, response, outcome)
}

func (a *terminalAccounting) claimTerminal() (
	wasStreaming bool,
	requestInFlight bool,
	timingStarted bool,
	started time.Time,
	claimed bool,
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return false, false, false, time.Time{}, false
	}
	a.finished = true
	wasStreaming = a.streamingActive
	requestInFlight = a.requestInFlight
	timingStarted = a.timingStarted
	started = a.started
	a.streamingActive = false
	a.requestInFlight = false
	return wasStreaming, requestInFlight, timingStarted, started, true
}

func effectiveRequestOutcome(ctx context.Context, response *icap.Response) string {
	if metadata, ok := requestinfo.ContextScenarioMetadata(ctx); ok {
		if metadata.Outcome == metricsinternal.OutcomeAllowed || metadata.Outcome == metricsinternal.OutcomeBlocked {
			return metadata.Outcome
		}
	}
	if responseHasPartialStream(response) {
		return metricsinternal.OutcomeBlocked
	}
	return requestOutcome(response)
}

func responseHasPartialStream(response *icap.Response) bool {
	if response == nil {
		return false
	}
	return messageHasPartialStream(response.HTTPRequest) || messageHasPartialStream(response.HTTPResponse)
}

func messageHasPartialStream(message *icap.HTTPMessage) bool {
	if message == nil || message.BodyStream == nil {
		return false
	}
	mode := message.BodyStream.Plan.FinishMode()
	return mode == icap.StreamFinishFIN || mode == icap.StreamFinishTerm
}
