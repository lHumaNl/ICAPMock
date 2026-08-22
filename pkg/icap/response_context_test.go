// Copyright 2026 ICAP Mock

package icap_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestResponseWriteToContextNotifiesAtActualStreamingStart(t *testing.T) {
	events := make([]string, 0, 3)
	payload := &eventStreamPayload{events: &events, body: "a"}
	response := responseWithPlannedPayload(t, payload, 1)
	writer := &eventStreamWriter{events: &events}
	options := icap.ResponseWriteOptions{
		OnStreamingStart: func() { events = append(events, "start") },
	}

	if _, err := response.WriteToContext(context.Background(), writer, options); err != nil {
		t.Fatalf("WriteToContext() error = %v", err)
	}
	want := []string{"open", "start", "body"}
	if !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestResponseWriteToContextDoesNotNotifyWhenSourceOpenFails(t *testing.T) {
	openFailure := errors.New("open failed")
	payload := &testStreamPayload{size: 1, openErr: openFailure}
	response := responseWithPlannedPayload(t, payload, 1)
	starts := 0

	_, err := response.WriteToContext(context.Background(), io.Discard, icap.ResponseWriteOptions{
		OnStreamingStart: func() { starts++ },
	})
	if !errors.Is(err, openFailure) {
		t.Fatalf("WriteToContext() error = %v, want open failure", err)
	}
	if starts != 0 {
		t.Fatalf("streaming starts = %d, want 0", starts)
	}
}

func TestResponseWriteToContextHonorsPreWriteCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := icap.NewResponse(icap.StatusOK)
	writer := &eventStreamWriter{}

	_, err := response.WriteToContext(ctx, writer, icap.ResponseWriteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteToContext() error = %v, want context.Canceled", err)
	}
	if writer.writes != 0 {
		t.Fatalf("writer calls = %d, want 0", writer.writes)
	}
}

func responseWithPlannedPayload(t *testing.T, payload icap.StreamPayload, size int64) *icap.Response {
	t.Helper()
	plan := mustPlanBodyStream(t, completePlanOptions(size))
	response := icap.NewResponse(icap.StatusOK)
	response.HTTPResponse = &icap.HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     "200",
		StatusText: "OK",
		Header:     icap.NewHeader(),
		BodyStream: &icap.BodyStream{Payload: payload, Plan: plan},
	}
	return response
}

type eventStreamPayload struct {
	events *[]string
	body   string
}

func (p *eventStreamPayload) Open() (io.ReadCloser, error) {
	*p.events = append(*p.events, "open")
	return io.NopCloser(strings.NewReader(p.body)), nil
}

func (p *eventStreamPayload) SizeHint() (int64, bool) { return int64(len(p.body)), true }

func (p *eventStreamPayload) Replayable() bool { return true }

type eventStreamWriter struct {
	events *[]string
	writes int
}

func (w *eventStreamWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.events != nil && string(p) == "a" {
		*w.events = append(*w.events, "body")
	}
	return len(p), nil
}

func (w *eventStreamWriter) Flush() error { return nil }

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
