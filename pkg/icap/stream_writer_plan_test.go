// Copyright 2026 ICAP Mock

package icap_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestPlannedBodyStreamWritesCompleteFINAndTERMFraming(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "complete", mode: icap.StreamFinishComplete, want: "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n"},
		{name: "fin", mode: icap.StreamFinishFIN, want: "2\r\nab\r\n2\r\ncd\r\n"},
		{name: "term", mode: icap.StreamFinishTerm, want: "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := newTestBodyStream(t, "abcd", tt.mode, planTargets{chunks: 2})
			var output bytes.Buffer
			if _, err := stream.WriteTo(&output); err != nil {
				t.Fatalf("WriteTo() error = %v", err)
			}
			if got := output.String(); got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlannedBodyStreamUsesScheduledBoundaries(t *testing.T) {
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "abc", icap.StreamFinishComplete, planTargets{
		duration: 100 * time.Millisecond,
		chunks:   3,
	})
	stream.Clock = clock
	stream.Sleeper = clock.Sleep
	writer := &timedStreamWriter{clock: clock}

	if _, err := stream.WriteToContext(context.Background(), writer); err != nil {
		t.Fatalf("WriteToContext() error = %v", err)
	}
	want := []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond}
	if !equalDurations(writer.bodyWriteOffsets, want) {
		t.Fatalf("body write offsets = %v, want %v", writer.bodyWriteOffsets, want)
	}
}

func TestPlannedBodyStreamOneByteWaitsUntilDuration(t *testing.T) {
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "x", icap.StreamFinishComplete, planTargets{duration: time.Second})
	stream.Clock = clock
	stream.Sleeper = clock.Sleep

	if _, err := stream.WriteTo(&bytes.Buffer{}); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := clock.sleeps; !equalDurations(got, []time.Duration{time.Second}) {
		t.Fatalf("sleeps = %v, want [1s]", got)
	}
}

func TestPlannedPartialStreamWritesFinalChunkDespiteWakeupJitter(t *testing.T) {
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "abcd", icap.StreamFinishFIN, planTargets{
		duration: 100 * time.Millisecond,
		chunks:   1,
	})
	stream.Clock = clock
	stream.Sleeper = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clock.advance(delay + time.Nanosecond)
		return nil
	}
	var output bytes.Buffer

	if _, err := stream.WriteToContext(context.Background(), &output); err != nil {
		t.Fatalf("WriteToContext() error = %v", err)
	}
	if got, want := output.String(), "4\r\nabcd\r\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPlannedPartialStreamStopsWhenIntermediateSleepOverrunsDuration(t *testing.T) {
	for _, mode := range []string{icap.StreamFinishFIN, icap.StreamFinishTerm} {
		t.Run(mode, func(t *testing.T) {
			clock := newFakeStreamClock()
			stream := newTestBodyStream(t, "abcdef", mode, planTargets{
				duration: 100 * time.Millisecond,
				chunks:   3,
			})
			stream.Clock = clock
			stream.Sleeper = func(ctx context.Context, delay time.Duration) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				clock.advance(delay + 100*time.Millisecond)
				return nil
			}
			var output bytes.Buffer

			if _, err := stream.WriteToContext(context.Background(), &output); err != nil {
				t.Fatalf("WriteToContext() error = %v", err)
			}
			want := "2\r\nab\r\n"
			if mode == icap.StreamFinishTerm {
				want += "0\r\n\r\n"
			}
			if got := output.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestPlannedPartialStreamStopsWhenIntermediateSleepReachesExactDuration(t *testing.T) {
	for _, mode := range []string{icap.StreamFinishFIN, icap.StreamFinishTerm} {
		t.Run(mode, func(t *testing.T) {
			clock := newFakeStreamClock()
			stream := newTestBodyStream(t, "abcdef", mode, planTargets{
				duration: 100 * time.Millisecond,
				chunks:   3,
			})
			stream.Clock = clock
			stream.Sleeper = func(ctx context.Context, delay time.Duration) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				clock.advance(delay + 50*time.Millisecond)
				return nil
			}
			var output bytes.Buffer

			if _, err := stream.WriteToContext(context.Background(), &output); err != nil {
				t.Fatalf("WriteToContext() error = %v", err)
			}
			want := "2\r\nab\r\n"
			if mode == icap.StreamFinishTerm {
				want += "0\r\n\r\n"
			}
			if got := output.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestPlannedBodyStreamCompleteBackpressureSkipsRemainingSleeps(t *testing.T) {
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "abcdef", icap.StreamFinishComplete, planTargets{
		duration: 100 * time.Millisecond,
		chunks:   3,
	})
	stream.Clock = clock
	stream.Sleeper = clock.Sleep
	writer := &laggingStreamWriter{clock: clock, lagAfter: "ab", lag: 150 * time.Millisecond}

	if _, err := stream.WriteTo(writer); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !strings.Contains(writer.String(), "2\r\nef\r\n0\r\n\r\n") {
		t.Fatalf("complete output truncated after duration: %q", writer.String())
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("sleeps after transport overrun = %v, want none", clock.sleeps)
	}
}

func TestPlannedBodyStreamFINStopsAtBoundaryAfterDurationOverrun(t *testing.T) {
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "abcdef", icap.StreamFinishFIN, planTargets{
		duration: 100 * time.Millisecond,
		chunks:   3,
	})
	stream.Clock = clock
	stream.Sleeper = clock.Sleep
	writer := &laggingStreamWriter{clock: clock, lagAfter: "ab", lag: 150 * time.Millisecond}

	if _, err := stream.WriteTo(writer); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := writer.String(); got != "2\r\nab\r\n" {
		t.Fatalf("FIN output = %q, want one complete protocol chunk", got)
	}
}

func TestPlannedBodyStreamCancellationInterruptsPacing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := newFakeStreamClock()
	stream := newTestBodyStream(t, "ab", icap.StreamFinishComplete, planTargets{duration: time.Second})
	stream.Clock = clock
	stream.Sleeper = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := stream.WriteToContext(ctx, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteToContext() error = %v, want context.Canceled", err)
	}
}

func TestPlannedBodyStreamFillsChunksAcrossShortReads(t *testing.T) {
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize:   4,
		TargetChunks: 2,
		FinishMode:   icap.StreamFinishComplete,
	})
	stream := &icap.BodyStream{Reader: &oneByteReader{data: []byte("abcd")}, Plan: plan}
	var output bytes.Buffer

	if _, err := stream.WriteTo(&output); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := output.String(); got != "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n" {
		t.Fatalf("output = %q, want planned chunks", got)
	}
}

func TestPlannedBodyStreamReportsSourceIntegrityErrors(t *testing.T) {
	closeFailure := errors.New("close failed")
	tests := []struct {
		name    string
		stream  func(*testing.T) *icap.BodyStream
		wantErr error
	}{
		{
			name: "early EOF",
			stream: func(t *testing.T) *icap.BodyStream {
				plan := mustPlanBodyStream(t, completePlanOptions(4))
				return &icap.BodyStream{Reader: strings.NewReader("abc"), Plan: plan}
			},
			wantErr: icap.ErrStreamSourceSizeMismatch,
		},
		{
			name: "extra data",
			stream: func(t *testing.T) *icap.BodyStream {
				plan := mustPlanBodyStream(t, completePlanOptions(3))
				return &icap.BodyStream{Reader: strings.NewReader("abcd"), Plan: plan}
			},
			wantErr: icap.ErrStreamSourceSizeMismatch,
		},
		{
			name: "close failure",
			stream: func(t *testing.T) *icap.BodyStream {
				plan := mustPlanBodyStream(t, completePlanOptions(3))
				payload := &testStreamPayload{body: "abc", size: 3, closeErr: closeFailure}
				return &icap.BodyStream{Payload: payload, Plan: plan}
			},
			wantErr: closeFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			_, err := tt.stream(t).WriteTo(&output)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("WriteTo() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(output.String(), "0\r\n\r\n") {
				t.Fatalf("source failure wrote terminator: %q", output.String())
			}
		})
	}
}

func TestPlannedBodyStreamReportsNoProgressWhileVerifyingSourceEnd(t *testing.T) {
	plan := mustPlanBodyStream(t, completePlanOptions(1))
	stream := &icap.BodyStream{Reader: &zeroProgressAfterDataReader{data: []byte("x")}, Plan: plan}
	var output bytes.Buffer

	_, err := stream.WriteTo(&output)
	if !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("WriteTo() error = %v, want io.ErrNoProgress", err)
	}
	if strings.Contains(output.String(), "0\r\n\r\n") {
		t.Fatalf("output contains terminator after source verification failure: %q", output.String())
	}
}

func TestPlannedBodyStreamBoundsReadBufferForLargeChunk(t *testing.T) {
	const sourceSize = 2 * 1024 * 1024
	reader := &readSizeRecorder{remaining: sourceSize}
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize:   sourceSize,
		TargetChunks: 1,
		FinishMode:   icap.StreamFinishComplete,
	})
	stream := &icap.BodyStream{Reader: reader, Plan: plan}

	if _, err := stream.WriteTo(io.Discard); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if reader.maximumRead > 64*1024 {
		t.Fatalf("maximum source read = %d, want at most 64 KiB", reader.maximumRead)
	}
}

func TestPlannedBodyStreamReturnsFramedByteCount(t *testing.T) {
	stream := newTestBodyStream(t, "abc", icap.StreamFinishComplete, planTargets{chunks: 1})
	var output bytes.Buffer

	written, err := stream.WriteTo(&output)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if written != int64(output.Len()) {
		t.Fatalf("WriteTo() bytes = %d, output length = %d", written, output.Len())
	}
}

func TestPlannedBodyStreamPropagatesWriteAndFlushErrors(t *testing.T) {
	writeFailure := errors.New("write failed")
	flushFailure := errors.New("flush failed")
	tests := []struct {
		name    string
		writer  io.Writer
		wantErr error
	}{
		{name: "write", writer: &failingStreamWriter{failAt: 2, err: writeFailure}, wantErr: writeFailure},
		{name: "flush", writer: &failingFlushWriter{err: flushFailure}, wantErr: flushFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := newTestBodyStream(t, "abc", icap.StreamFinishComplete, planTargets{chunks: 1})
			if _, err := stream.WriteTo(tt.writer); !errors.Is(err, tt.wantErr) {
				t.Fatalf("WriteTo() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type planTargets struct {
	duration time.Duration
	chunks   int
}

func newTestBodyStream(t *testing.T, body, mode string, targets planTargets) *icap.BodyStream {
	t.Helper()
	plan := mustPlanBodyStream(t, icap.BodyStreamPlanOptions{
		SourceSize:   int64(len(body)),
		Duration:     targets.duration,
		TargetChunks: targets.chunks,
		FinishMode:   mode,
	})
	return &icap.BodyStream{Payload: icap.NewBytesStreamPayload([]byte(body)), Plan: plan}
}

func completePlanOptions(size int64) icap.BodyStreamPlanOptions {
	return icap.BodyStreamPlanOptions{SourceSize: size, TargetChunks: 1, FinishMode: icap.StreamFinishComplete}
}

type fakeStreamClock struct {
	start  time.Time
	now    time.Time
	sleeps []time.Duration
}

func newFakeStreamClock() *fakeStreamClock {
	start := time.Unix(1_000, 0)
	return &fakeStreamClock{start: start, now: start}
}

func (c *fakeStreamClock) Now() time.Time { return c.now }

func (c *fakeStreamClock) Sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sleeps = append(c.sleeps, delay)
	c.now = c.now.Add(delay)
	return nil
}

func (c *fakeStreamClock) advance(delay time.Duration) { c.now = c.now.Add(delay) }

type timedStreamWriter struct {
	clock            *fakeStreamClock
	bodyWriteOffsets []time.Duration
}

func (w *timedStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 1 && p[0] >= 'a' && p[0] <= 'z' {
		w.bodyWriteOffsets = append(w.bodyWriteOffsets, w.clock.now.Sub(w.clock.start))
	}
	return len(p), nil
}

func (w *timedStreamWriter) Flush() error { return nil }

type laggingStreamWriter struct {
	bytes.Buffer
	clock    *fakeStreamClock
	lagAfter string
	lag      time.Duration
}

func (w *laggingStreamWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if string(p) == w.lagAfter {
		w.clock.advance(w.lag)
	}
	return n, err
}

func (w *laggingStreamWriter) Flush() error { return nil }

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

type zeroProgressAfterDataReader struct{ data []byte }

func (r *zeroProgressAfterDataReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, nil
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

type testStreamPayload struct {
	body     string
	size     int64
	openErr  error
	closeErr error
}

func (p *testStreamPayload) Open() (io.ReadCloser, error) {
	if p.openErr != nil {
		return nil, p.openErr
	}
	return &errorReadCloser{Reader: strings.NewReader(p.body), closeErr: p.closeErr}, nil
}

func (p *testStreamPayload) SizeHint() (int64, bool) { return p.size, true }

func (p *testStreamPayload) Replayable() bool { return true }

type errorReadCloser struct {
	*strings.Reader
	closeErr error
}

func (r *errorReadCloser) Close() error { return r.closeErr }

type readSizeRecorder struct {
	remaining   int
	maximumRead int
}

type failingStreamWriter struct {
	calls  int
	failAt int
	err    error
}

func (w *failingStreamWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

type failingFlushWriter struct{ err error }

func (w *failingFlushWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *failingFlushWriter) Flush() error { return w.err }

func (r *readSizeRecorder) Read(p []byte) (int, error) {
	if len(p) > r.maximumRead {
		r.maximumRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= n
	return n, nil
}

func equalDurations(got, want []time.Duration) bool {
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
