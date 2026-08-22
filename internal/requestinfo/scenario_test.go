// Copyright 2026 ICAP Mock

package requestinfo

import (
	"context"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
)

func TestScenarioMetadataPreservesAuthoritativeOutcome(t *testing.T) {
	ctx := WithScenarioMetadata(context.Background())

	if accepted := SetScenarioOutcome(ctx, metrics.OutcomeBlocked); !accepted {
		t.Fatal("SetScenarioOutcome() rejected blocked outcome")
	}
	SetScenarioMetadata(ctx, "malware", "deny-page")

	metadata, ok := ContextScenarioMetadata(ctx)
	if !ok {
		t.Fatal("ContextScenarioMetadata() did not find metadata")
	}
	if metadata.Outcome != metrics.OutcomeBlocked {
		t.Fatalf("Outcome = %q, want %q", metadata.Outcome, metrics.OutcomeBlocked)
	}
	if metadata.Scenario != "malware" || metadata.Response != "deny-page" {
		t.Fatalf("metadata = %#v, want selected labels", metadata)
	}
}

func TestSetScenarioOutcomeAcceptsOnlyAuthoritativeSuccessOutcomes(t *testing.T) {
	for _, outcome := range []string{metrics.OutcomeAllowed, metrics.OutcomeBlocked} {
		t.Run(outcome, func(t *testing.T) {
			ctx := WithScenarioMetadata(context.Background())
			if accepted := SetScenarioOutcome(ctx, outcome); !accepted {
				t.Fatalf("SetScenarioOutcome(%q) rejected valid outcome", outcome)
			}
			metadata, ok := ContextScenarioMetadata(ctx)
			if !ok || metadata.Outcome != outcome {
				t.Fatalf("metadata = %#v, %v; want outcome %q", metadata, ok, outcome)
			}
		})
	}

	ctx := WithScenarioMetadata(context.Background())
	if accepted := SetScenarioOutcome(ctx, metrics.OutcomeError); accepted {
		t.Fatal("SetScenarioOutcome() accepted non-authoritative error outcome")
	}
	if _, ok := ContextScenarioMetadata(ctx); ok {
		t.Fatal("invalid outcome made empty scenario metadata observable")
	}
}

func TestScenarioTimingStartInvokesInstalledHook(t *testing.T) {
	calls := 0
	ctx := WithScenarioTimingStart(context.Background(), func() { calls++ })

	StartScenarioTiming(ctx)

	if calls != 1 {
		t.Fatalf("timing start calls = %d, want 1", calls)
	}
}

func TestScenarioTimingFallbackUsesMaterializationBoundary(t *testing.T) {
	routeStarted := time.Now().Add(-time.Second)
	ctx := WithScenarioTimingStart(context.Background(), func() {})

	MarkScenarioBodyMaterialized(ctx)
	fallback := ScenarioTimingFallback(ctx, routeStarted)

	if !fallback.After(routeStarted) {
		t.Fatalf("fallback = %v, want after route start %v", fallback, routeStarted)
	}
}
