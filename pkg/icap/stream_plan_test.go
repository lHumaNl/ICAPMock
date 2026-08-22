// Copyright 2026 ICAP Mock

package icap_test

import (
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestPlanBodyStreamAutomaticDuration(t *testing.T) {
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize: 100,
		Duration:   time.Second,
		FinishMode: icap.StreamFinishComplete,
	})

	if got := plan.ChunkCount(); got != 11 {
		t.Fatalf("ChunkCount() = %d, want 11", got)
	}
	if got := plan.EffectiveChunkSize(); got != 10 {
		t.Fatalf("EffectiveChunkSize() = %d, want 10", got)
	}
	assertPlanBoundaries(t, plan, 0, time.Second)
	assertBalancedPlan(t, plan, 100)
}

func TestPlanBodyStreamAutomaticDurationRoundsUpCadence(t *testing.T) {
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize: 10,
		Duration:   101 * time.Millisecond,
		FinishMode: icap.StreamFinishComplete,
	})

	if got := plan.ChunkCount(); got != 3 {
		t.Fatalf("ChunkCount() = %d, want 3", got)
	}
	assertPlanBoundaries(t, plan, 0, 101*time.Millisecond)
}

func TestPlanBodyStreamAdaptsTargetSizeToCadenceSlots(t *testing.T) {
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize:      100,
		Duration:        time.Second,
		Every:           400 * time.Millisecond,
		TargetChunkSize: 10,
		FinishMode:      icap.StreamFinishComplete,
	})

	if got := plan.ChunkCount(); got != 3 {
		t.Fatalf("ChunkCount() = %d, want 3", got)
	}
	if got := plan.TargetChunkSize(); got != 10 {
		t.Fatalf("TargetChunkSize() = %d, want 10", got)
	}
	if got := plan.EffectiveChunkSize(); got != 34 {
		t.Fatalf("EffectiveChunkSize() = %d, want 34", got)
	}
	assertMinimumCadence(t, plan, 400*time.Millisecond)
	assertPlanBoundaries(t, plan, 0, time.Second)
}

func TestPlanBodyStreamClampsExplicitHints(t *testing.T) {
	tests := []struct {
		name    string
		options icap.BodyStreamPlanOptions
	}{
		{
			name: "target chunks",
			options: icap.BodyStreamPlanOptions{
				SourceSize:   2_000,
				TargetChunks: 2_000,
				FinishMode:   icap.StreamFinishComplete,
			},
		},
		{
			name: "target size",
			options: icap.BodyStreamPlanOptions{
				SourceSize:      2_000,
				TargetChunkSize: 1,
				FinishMode:      icap.StreamFinishComplete,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustPlanBodyStream(t, tt.options)
			if got := plan.ChunkCount(); got != 1_000 {
				t.Fatalf("ChunkCount() = %d, want 1000", got)
			}
			if got := plan.EffectiveChunkSize(); got != 2 {
				t.Fatalf("EffectiveChunkSize() = %d, want 2", got)
			}
		})
	}
}

func TestPlanBodyStreamSmallAndNoDurationPayloads(t *testing.T) {
	tests := []struct {
		name       string
		sourceSize int64
		duration   time.Duration
		wantChunks int
		wantFirst  time.Duration
		wantLast   time.Duration
	}{
		{name: "empty", sourceSize: 0, duration: time.Second},
		{name: "one byte", sourceSize: 1, duration: time.Second, wantChunks: 1, wantFirst: time.Second, wantLast: time.Second},
		{name: "two bytes", sourceSize: 2, duration: time.Second, wantChunks: 2, wantLast: time.Second},
		{name: "default sixteen KiB", sourceSize: 32*1024 + 1, wantChunks: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
				SourceSize: tt.sourceSize,
				Duration:   tt.duration,
				FinishMode: icap.StreamFinishComplete,
			})
			if got := plan.ChunkCount(); got != tt.wantChunks {
				t.Fatalf("ChunkCount() = %d, want %d", got, tt.wantChunks)
			}
			if tt.wantChunks > 0 {
				assertPlanBoundaries(t, plan, tt.wantFirst, tt.wantLast)
				assertBalancedPlan(t, plan, tt.sourceSize)
			}
		})
	}
}

func TestPlanBodyStreamPartialUsesSelectedByteLimit(t *testing.T) {
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize:       100,
		SelectedBytes:    40,
		SelectedBytesSet: true,
		Duration:         time.Second,
		TargetChunks:     4,
		FinishMode:       icap.StreamFinishFIN,
	})

	if got := plan.BodyBytes(); got != 40 {
		t.Fatalf("BodyBytes() = %d, want 40", got)
	}
	if got := plan.SourceSize(); got != 100 {
		t.Fatalf("SourceSize() = %d, want 100", got)
	}
	assertBalancedPlan(t, plan, 40)
}

func TestPlanBodyStreamRejectsConflictingTargets(t *testing.T) {
	_, err := icap.PlanBodyStream(icap.BodyStreamPlanOptions{
		SourceSize:      10,
		TargetChunkSize: 2,
		TargetChunks:    5,
		FinishMode:      icap.StreamFinishComplete,
	})
	if err == nil {
		t.Fatal("PlanBodyStream() error = nil, want conflicting target error")
	}
}

func mustPlanBodyStream(t *testing.T, options icap.BodyStreamPlanOptions) icap.BodyStreamPlan {
	t.Helper()
	plan, err := icap.PlanBodyStream(options)
	if err != nil {
		t.Fatalf("PlanBodyStream() error = %v", err)
	}
	return plan
}

func assertPlanBoundaries(t *testing.T, plan icap.BodyStreamPlan, first, last time.Duration) {
	t.Helper()
	firstOffset, ok := plan.ChunkOffset(0)
	if !ok || firstOffset != first {
		t.Fatalf("ChunkOffset(0) = %v, %v; want %v, true", firstOffset, ok, first)
	}
	lastOffset, ok := plan.ChunkOffset(plan.ChunkCount() - 1)
	if !ok || lastOffset != last {
		t.Fatalf("last ChunkOffset() = %v, %v; want %v, true", lastOffset, ok, last)
	}
}

func assertBalancedPlan(t *testing.T, plan icap.BodyStreamPlan, total int64) {
	t.Helper()
	var sum, minimum, maximum int64
	for i := 0; i < plan.ChunkCount(); i++ {
		size, ok := plan.ChunkSize(i)
		if !ok {
			t.Fatalf("ChunkSize(%d) missing", i)
		}
		if i == 0 || size < minimum {
			minimum = size
		}
		if size > maximum {
			maximum = size
		}
		sum += size
	}
	if sum != total {
		t.Fatalf("planned bytes = %d, want %d", sum, total)
	}
	if maximum-minimum > 1 {
		t.Fatalf("chunk size range = %d..%d, want balanced", minimum, maximum)
	}
}

func assertMinimumCadence(t *testing.T, plan icap.BodyStreamPlan, minimum time.Duration) {
	t.Helper()
	previous, _ := plan.ChunkOffset(0)
	for i := 1; i < plan.ChunkCount(); i++ {
		current, _ := plan.ChunkOffset(i)
		if current-previous < minimum {
			t.Fatalf("chunk %d gap = %v, want at least %v", i, current-previous, minimum)
		}
		previous = current
	}
}
