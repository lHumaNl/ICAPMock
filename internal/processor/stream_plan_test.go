// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

type recordingStreamSelector struct {
	calls       map[[2]int64]int
	selectValue func(int64, int64) int64
}

func (s *recordingStreamSelector) SelectInclusive(minimum, maximum int64) int64 {
	s.calls[[2]int64{minimum, maximum}]++
	return s.selectValue(minimum, maximum)
}

func TestMockProcessor_SelectsDirectStreamRangesOnce(t *testing.T) {
	scenario := rangedDirectStreamScenario()
	processor := processSingleScenario(t, scenario)
	selector := newRecordingStreamSelector(func(_, maximum int64) int64 { return maximum })
	processor.streamSelector = selector

	response, err := processor.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	plan := response.HTTPResponse.BodyStream.Plan
	if plan.Duration() != 20*time.Millisecond || plan.Every() != 2*time.Millisecond {
		t.Fatalf("selected timing = %v/%v, want 20ms/2ms", plan.Duration(), plan.Every())
	}
	if plan.TargetChunkSize() != 16 || plan.BodyBytes() != 60 {
		t.Fatalf("selected size/bytes = %d/%d, want 16/60", plan.TargetChunkSize(), plan.BodyBytes())
	}
	assertSelectedOnce(t, selector, int64(10*time.Millisecond), int64(20*time.Millisecond))
	assertSelectedOnce(t, selector, 40, 60)
	assertSelectedOnce(t, selector, 8, 16)
	assertSelectedOnce(t, selector, int64(time.Millisecond), int64(2*time.Millisecond))
}

func TestMockProcessor_SelectsWeightedLegacyFinishOnce(t *testing.T) {
	scenario := weightedLegacyFINScenario()
	processor := processSingleScenario(t, scenario)
	selector := newRecordingStreamSelector(func(minimum, _ int64) int64 { return minimum })
	processor.streamSelector = selector

	response, err := processor.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	plan := response.HTTPResponse.BodyStream.Plan
	if plan.FinishMode() != icap.StreamFinishComplete {
		t.Fatalf("FinishMode() = %q, want complete", plan.FinishMode())
	}
	if response.CloseAfterWrite() {
		t.Fatal("CloseAfterWrite() = true for selected complete plan")
	}
	if plan.Duration() != 10*time.Millisecond || plan.TargetChunkSize() != 2 {
		t.Fatalf("legacy duration/target = %v/%d, want 10ms/2", plan.Duration(), plan.TargetChunkSize())
	}
	assertSelectedOnce(t, selector, 0, weightedFinishScale-1)
	assertSelectedOnce(t, selector, int64(10*time.Millisecond), int64(20*time.Millisecond))
	assertSelectedOnce(t, selector, 2, 4)
}

func TestMockProcessor_SelectedWeightedFINMarksConnectionClose(t *testing.T) {
	processor := processSingleScenario(t, weightedLegacyFINScenario())
	selector := newRecordingStreamSelector(func(_, maximum int64) int64 { return maximum })
	processor.streamSelector = selector

	response, err := processor.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	plan := response.HTTPResponse.BodyStream.Plan
	if plan.FinishMode() != icap.StreamFinishFIN || plan.BodyBytes() != 2 {
		t.Fatalf("selected finish/bytes = %q/%d, want fin/2", plan.FinishMode(), plan.BodyBytes())
	}
	if !response.CloseAfterWrite() {
		t.Fatal("CloseAfterWrite() = false for selected FIN plan")
	}
	assertSelectedOnce(t, selector, 0, weightedFinishScale-1)
	assertSelectedOnce(t, selector, 2, 2)
}

func TestMockProcessor_MapsDirectTargetChunks(t *testing.T) {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{From: "body", Body: "abcdef"}
	scenario.Response.Stream.Chunks = storage.StreamChunksConfig{}
	scenario.Response.Stream.Finish = storage.StreamFinishConfig{}
	scenario.Response.Stream.Throttle = storage.StreamThrottleConfig{IsSet: true, TargetChunks: 3}
	scenario.Response.Stream.End = storage.StreamEndConfig{IsSet: true, Mode: icap.StreamFinishComplete}

	response, err := processSingleScenario(t, scenario).Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	plan := response.HTTPResponse.BodyStream.Plan
	if plan.TargetChunks() != 3 || plan.ChunkCount() != 3 {
		t.Fatalf("target/planned chunks = %d/%d, want 3/3", plan.TargetChunks(), plan.ChunkCount())
	}
}

func TestMockProcessor_LegacyStreamWithoutChunkHintUsesAutomaticPlanning(t *testing.T) {
	body := strings.Repeat("x", 20*1024)
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{From: "body", Body: body}
	scenario.Response.Stream.Chunks = storage.StreamChunksConfig{}

	response, err := processSingleScenario(t, scenario).Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	plan := response.HTTPResponse.BodyStream.Plan
	if plan.TargetChunkSize() != 0 || plan.ChunkCount() != 2 {
		t.Fatalf("target/planned chunks = %d/%d, want automatic 0/2", plan.TargetChunkSize(), plan.ChunkCount())
	}
}

func TestNewBodyStream_RejectsUnknownSourceSize(t *testing.T) {
	payload := icap.NewReplayableStreamPayload(
		func() (io.ReadCloser, error) { return io.NopCloser(&emptyReader{}), nil },
		icap.UnknownStreamPayloadSize,
	)
	_, err := newBodyStream(&storage.StreamConfig{}, payload, randomStreamSelector{})
	if err == nil || !strings.Contains(err.Error(), "must be known") {
		t.Fatalf("newBodyStream() error = %v, want known-size planning error", err)
	}
}

func TestMockProcessor_ClosesPreparedFileWhenPlanningFails(t *testing.T) {
	reader := newProcessorTrackingReadCloser("file")
	scenario := bodyFileStreamScenario(writeProcessorTempFile(t, "file"), icap.StreamFinishComplete)
	processor := processSingleScenario(t, scenario)
	state := stubProcessorStreamFile(t, scenario.Response.Stream.Source.BodyFile, 4, reader)
	processor.streamSelector = newRecordingStreamSelector(func(_, maximum int64) int64 { return maximum + 1 })

	response, err := processor.Process(context.Background(), createTestRESPMODRequest(t))
	if err == nil {
		t.Fatal("Process() error = nil, want selector error")
	}
	if response != nil {
		t.Fatalf("Process() response = %v, want nil", response)
	}
	if state.opens != 1 || state.stats != 1 || !reader.closed {
		t.Fatalf("file opens/stats/closed = %d/%d/%v, want 1/1/true", state.opens, state.stats, reader.closed)
	}
}

func TestMockProcessor_ResponseReleaseClosesPreparedFileBeforeDelivery(t *testing.T) {
	reader := newProcessorTrackingReadCloser("file")
	scenario := bodyFileStreamScenario(writeProcessorTempFile(t, "file"), icap.StreamFinishComplete)
	processor := processSingleScenario(t, scenario)
	state := stubProcessorStreamFile(t, scenario.Response.Stream.Source.BodyFile, 4, reader)

	response, err := processor.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	response.ReleaseBodies()

	if state.opens != 1 || state.stats != 1 || !reader.closed {
		t.Fatalf("file opens/stats/closed = %d/%d/%v, want 1/1/true", state.opens, state.stats, reader.closed)
	}
}

func rangedDirectStreamScenario() *storage.Scenario {
	body := "01234567890123456789012345678901234567890123456789" +
		"01234567890123456789012345678901234567890123456789"
	return &storage.Scenario{
		Name: "ranged-direct-stream", Match: storage.MatchRule{Methods: []string{icap.MethodRESPMOD}}, Priority: 100,
		Response: storage.ResponseTemplate{ICAPStatus: 200, Stream: &storage.StreamConfig{
			Source: storage.StreamSourceConfig{From: "body", Body: body},
			Send: storage.StreamSendConfig{
				IsSet:    true,
				Percent:  storage.PercentSpec{Min: 40, Max: 60, IsSet: true},
				Duration: storage.DurationSpec{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond, IsSet: true},
			},
			Throttle: storage.StreamThrottleConfig{
				IsSet:           true,
				TargetChunkSize: storage.SizeSpec{Min: 8, Max: 16, IsSet: true},
				Every:           storage.DurationSpec{Min: time.Millisecond, Max: 2 * time.Millisecond, IsSet: true},
			},
			End: storage.StreamEndConfig{Mode: icap.StreamFinishFIN, IsSet: true},
		}},
	}
}

func weightedLegacyFINScenario() *storage.Scenario {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{From: "body", Body: "abcd"}
	scenario.Response.Stream.Chunks.Size = storage.SizeSpec{Min: 2, Max: 4, IsSet: true}
	scenario.Response.Stream.Duration = storage.DurationSpec{
		Min: 10 * time.Millisecond, Max: 20 * time.Millisecond, IsSet: true,
	}
	scenario.Response.Stream.Finish = storage.StreamFinishConfig{
		Mode: icap.StreamFinishWeighted, CompletePercent: 50, FinPercent: 50,
		Fin: storage.StreamFINConfig{Close: "clean", After: storage.StreamFINAfterConfig{
			Bytes: storage.SizeSpec{Min: 2, Max: 2, IsSet: true},
		}},
	}
	return scenario
}

func newRecordingStreamSelector(selectValue func(int64, int64) int64) *recordingStreamSelector {
	return &recordingStreamSelector{calls: make(map[[2]int64]int), selectValue: selectValue}
}

func assertSelectedOnce(
	t *testing.T,
	selector *recordingStreamSelector,
	minimum, maximum int64,
) {
	t.Helper()
	if got := selector.calls[[2]int64{minimum, maximum}]; got != 1 {
		t.Fatalf("selection count for %d-%d = %d, want 1", minimum, maximum, got)
	}
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
