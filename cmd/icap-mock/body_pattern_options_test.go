// Copyright 2026 ICAP Mock

package main

import (
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

func TestBodyPatternOptionsRuntimeWiring(t *testing.T) {
	matching := config.MockMatchingConfig{
		BodyPatternLimit:       config.NewBodySizeLimit(64),
		BodyPatternLimitAction: config.BodyPatternLimitActionError,
	}

	options := bodyPatternOptions(matching, 16)
	if options.Limit != 16 {
		t.Fatalf("Limit = %d, want 16", options.Limit)
	}
	if options.LimitAction != storage.BodyPatternLimitActionError {
		t.Fatalf("LimitAction = %s, want error", options.LimitAction)
	}
}

func TestBodyPatternOptionsRuntimeWiringUnlimited(t *testing.T) {
	matching := config.MockMatchingConfig{
		BodyPatternLimit:       config.NewUnlimitedBodySizeLimit(),
		BodyPatternLimitAction: config.BodyPatternLimitActionNoMatch,
	}

	options := bodyPatternOptions(matching, 0)
	if options.Limit != -1 {
		t.Fatalf("Limit = %d, want -1 for unlimited", options.Limit)
	}
}

func TestBodyPatternOptionsNormalizesMixedCaseAction(t *testing.T) {
	matching := config.MockMatchingConfig{
		BodyPatternLimit:       config.NewBodySizeLimit(64),
		BodyPatternLimitAction: "No_Match",
	}

	options := bodyPatternOptions(matching, 0)
	if options.LimitAction != storage.BodyPatternLimitActionNoMatch {
		t.Fatalf("LimitAction = %s, want no_match", options.LimitAction)
	}
}
