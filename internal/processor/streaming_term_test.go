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
	if stream.FinishMode != icap.StreamFinishTerm {
		t.Fatalf("FinishMode = %q, want %q", stream.FinishMode, icap.StreamFinishTerm)
	}
	if stream.FinAfterBytes != 40 || !stream.FinAfterBytesSet {
		t.Fatalf("FinAfterBytes = %d/%v, want 40/true", stream.FinAfterBytes, stream.FinAfterBytesSet)
	}
	stream.Sleep = func(time.Duration) {}
	assertStreamOutput(t, resp, "28\r\n"+strings.Repeat("a", 40)+"\r\n0\r\n\r\n")
}

func newPercentTermStreamScenario(body string, percent int) *storage.Scenario {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream = newPercentFINStream("body", body, percent)
	scenario.Response.Stream.End.Mode = icap.StreamFinishTerm
	scenario.Response.Stream.Throttle.ChunkSize = storage.SizeSpec{Min: 64, Max: 64, IsSet: true}
	return scenario
}
