// Copyright 2026 ICAP Mock

package requestinfo

import (
	"context"
	"sync"
)

type scenarioMetadataKey struct{}

type scenarioMetadataHolder struct {
	metadata ScenarioMetadata
	mu       sync.RWMutex
}

// ScenarioMetadata contains bounded scenario labels selected while processing a request.
type ScenarioMetadata struct {
	Scenario string
	Response string
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
	holder.metadata = ScenarioMetadata{Scenario: scenario, Response: response}
}

// ContextScenarioMetadata returns scenario metadata recorded during request processing.
func ContextScenarioMetadata(ctx context.Context) (ScenarioMetadata, bool) {
	holder, ok := scenarioMetadataHolderFromContext(ctx)
	if !ok {
		return ScenarioMetadata{}, false
	}
	holder.mu.RLock()
	defer holder.mu.RUnlock()
	return holder.metadata, holder.metadata.Scenario != "" || holder.metadata.Response != ""
}

func scenarioMetadataHolderFromContext(ctx context.Context) (*scenarioMetadataHolder, bool) {
	if ctx == nil {
		return nil, false
	}
	holder, ok := ctx.Value(scenarioMetadataKey{}).(*scenarioMetadataHolder)
	return holder, ok && holder != nil
}
