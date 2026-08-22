// Copyright 2026 ICAP Mock

package requestinfo

import (
	"context"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
)

type scenarioMetadataKey struct{}
type scenarioTimingKey struct{}

type scenarioTimingHolder struct {
	start          func()
	materializedAt time.Time
	mu             sync.RWMutex
}

type scenarioMetadataHolder struct {
	metadata ScenarioMetadata
	mu       sync.RWMutex
}

// ScenarioMetadata contains bounded scenario labels selected while processing a request.
type ScenarioMetadata struct {
	Scenario string
	Response string
	Outcome  string
}

// WithScenarioMetadata stores a mutable holder for request-scoped scenario metadata.
func WithScenarioMetadata(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scenarioMetadataKey{}, &scenarioMetadataHolder{})
}

// SetScenarioMetadata records the matched scenario and response labels on the request context.
func SetScenarioMetadata(ctx context.Context, scenario, response string) {
	holder, ok := scenarioMetadataHolderFromContext(ctx)
	if !ok {
		return
	}
	holder.mu.Lock()
	defer holder.mu.Unlock()
	holder.metadata.Scenario = scenario
	holder.metadata.Response = response
}

// SetScenarioOutcome records an authoritative allowed or blocked outcome.
// Delivery failures remain server-owned and always override this value.
func SetScenarioOutcome(ctx context.Context, outcome string) bool {
	if outcome != metrics.OutcomeAllowed && outcome != metrics.OutcomeBlocked {
		return false
	}
	holder, ok := scenarioMetadataHolderFromContext(ctx)
	if !ok {
		return false
	}
	holder.mu.Lock()
	defer holder.mu.Unlock()
	holder.metadata.Outcome = outcome
	return true
}

// WithScenarioTimingStart installs the server-owned start boundary used by
// preview processing after any required body continuation has completed.
func WithScenarioTimingStart(ctx context.Context, start func()) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scenarioTimingKey{}, &scenarioTimingHolder{start: start})
}

// StartScenarioTiming starts terminal latency accounting when a timing hook is installed.
func StartScenarioTiming(ctx context.Context) {
	if ctx == nil {
		return
	}
	if holder, ok := scenarioTimingHolderFromContext(ctx); ok && holder.start != nil {
		holder.start()
	}
}

// MarkScenarioBodyMaterialized records the end of deferred preview upload for
// custom-handler timing fallback without starting production scenario timing.
func MarkScenarioBodyMaterialized(ctx context.Context) {
	holder, ok := scenarioTimingHolderFromContext(ctx)
	if !ok {
		return
	}
	holder.mu.Lock()
	defer holder.mu.Unlock()
	if holder.materializedAt.IsZero() {
		holder.materializedAt = time.Now()
	}
}

// ScenarioTimingFallback returns the end of deferred materialization when
// available, otherwise the supplied route start boundary.
func ScenarioTimingFallback(ctx context.Context, routeStarted time.Time) time.Time {
	if materializedAt, ok := ScenarioBodyMaterializedAt(ctx); ok {
		return materializedAt
	}
	return routeStarted
}

// ScenarioBodyMaterializedAt returns the recorded deferred-upload completion boundary.
func ScenarioBodyMaterializedAt(ctx context.Context) (time.Time, bool) {
	holder, ok := scenarioTimingHolderFromContext(ctx)
	if !ok {
		return time.Time{}, false
	}
	holder.mu.RLock()
	defer holder.mu.RUnlock()
	return holder.materializedAt, !holder.materializedAt.IsZero()
}

func scenarioTimingHolderFromContext(ctx context.Context) (*scenarioTimingHolder, bool) {
	if ctx == nil {
		return nil, false
	}
	holder, ok := ctx.Value(scenarioTimingKey{}).(*scenarioTimingHolder)
	return holder, ok && holder != nil
}

// ContextScenarioMetadata returns scenario metadata recorded during request processing.
func ContextScenarioMetadata(ctx context.Context) (ScenarioMetadata, bool) {
	holder, ok := scenarioMetadataHolderFromContext(ctx)
	if !ok {
		return ScenarioMetadata{}, false
	}
	holder.mu.RLock()
	defer holder.mu.RUnlock()
	return holder.metadata, holder.metadata.Scenario != "" || holder.metadata.Response != "" || holder.metadata.Outcome != ""
}

func scenarioMetadataHolderFromContext(ctx context.Context) (*scenarioMetadataHolder, bool) {
	if ctx == nil {
		return nil, false
	}
	holder, ok := ctx.Value(scenarioMetadataKey{}).(*scenarioMetadataHolder)
	return holder, ok && holder != nil
}
