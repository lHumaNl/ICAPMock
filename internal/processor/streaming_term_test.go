// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestMockProcessor_NewTermPercentStreamCapsBodyBytesAndTerminates(t *testing.T) {
	scenario := newPercentTermStreamScenario(strings.Repeat("a", 40)+strings.Repeat("b", 60), 40)
	resp, err := processSingleScenario(t, scenario).Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	stream := resp.HTTPResponse.BodyStream
	if stream.Plan.FinishMode() != icap.StreamFinishTerm {
		t.Fatalf("FinishMode() = %q, want %q", stream.Plan.FinishMode(), icap.StreamFinishTerm)
	}
	if stream.Plan.BodyBytes() != 40 {
		t.Fatalf("BodyBytes() = %d, want 40", stream.Plan.BodyBytes())
	}
	stream.Sleeper = func(context.Context, time.Duration) error { return nil }
	assertStreamOutput(t, resp, "28\r\n"+strings.Repeat("a", 40)+"\r\n0\r\n\r\n")
}

func newPercentTermStreamScenario(body string, percent int) *storage.Scenario {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream = newPercentFINStream("body", body, percent)
	scenario.Response.Stream.End.Mode = icap.StreamFinishTerm
	scenario.Response.Stream.Throttle.TargetChunkSize = storage.SizeSpec{Min: 64, Max: 64, IsSet: true}
	return scenario
}
