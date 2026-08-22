// Copyright 2026 ICAP Mock

package icap_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestBytesStreamPayloadIsReplayable(t *testing.T) {
	payload := icap.NewBytesStreamPayload([]byte("abc"))
	for i := 0; i < 2; i++ {
		reader, err := payload.Open()
		if err != nil {
			t.Fatalf("Open() iteration %d error = %v", i+1, err)
		}
		assertPayloadBody(t, reader, "abc")
	}
	if size, known := payload.SizeHint(); size != 3 || !known || !payload.Replayable() {
		t.Fatalf("payload metadata = %d, %v, %v; want 3, true, true", size, known, payload.Replayable())
	}
}

func TestOneShotStreamPayloadCannotBeReopened(t *testing.T) {
	payload := icap.NewOneShotStreamPayload(io.NopCloser(strings.NewReader("abc")), 3)
	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	assertPayloadBody(t, reader, "abc")
	if _, err := payload.Open(); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("second Open() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestBodyStreamClonePreservesPlanAndPayloadSemantics(t *testing.T) {
	plan := mustPlanBodyStream(t, completePlanOptions(4))
	original := &icap.BodyStream{Payload: icap.NewBytesStreamPayload([]byte("abcd")), Plan: plan}
	clone := original.Clone()

	if clone == original {
		t.Fatal("Clone() returned original pointer")
	}
	if clone.Plan.EffectiveChunkSize() != original.Plan.EffectiveChunkSize() {
		t.Fatal("Clone() changed immutable plan")
	}
	assertStreamOutput(t, original, "4\r\nabcd\r\n0\r\n\r\n")
	assertStreamOutput(t, clone, "4\r\nabcd\r\n0\r\n\r\n")
}

func TestBodyStreamCloneOneShotPayloadFailsClearly(t *testing.T) {
	plan := mustPlanBodyStream(t, completePlanOptions(3))
	payload := icap.NewOneShotStreamPayload(io.NopCloser(strings.NewReader("abc")), 3)
	original := &icap.BodyStream{Payload: payload, Plan: plan}
	clone := original.Clone()

	assertStreamOutput(t, original, "3\r\nabc\r\n0\r\n\r\n")
	if _, err := clone.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("clone WriteTo() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestBodyStreamCloneReaderFallbackFailsClearly(t *testing.T) {
	plan := mustPlanBodyStream(t, completePlanOptions(3))
	original := &icap.BodyStream{Reader: strings.NewReader("abc"), Plan: plan}
	clone := original.Clone()

	if _, err := clone.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrBodyStreamCloneUnavailable) {
		t.Fatalf("clone WriteTo() error = %v, want ErrBodyStreamCloneUnavailable", err)
	}
	assertStreamOutput(t, original, "3\r\nabc\r\n0\r\n\r\n")
}

func TestHTTPMessageBodyStreamPayloadClaimsLiveReader(t *testing.T) {
	message := &icap.HTTPMessage{Header: icap.NewHeader(), BodyReader: strings.NewReader("abc")}
	payload, err := icap.NewHTTPMessageBodyStreamPayload(message, 0)
	if err != nil {
		t.Fatalf("NewHTTPMessageBodyStreamPayload() error = %v", err)
	}
	if message.BodyReader != nil {
		t.Fatal("BodyReader was not claimed")
	}
	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	assertPayloadBody(t, reader, "abc")
}

func TestLimitedStreamPayloadDetectsOverflow(t *testing.T) {
	source := icap.NewReplayableStreamPayload(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("abcd")), nil
	}, icap.UnknownStreamPayloadSize)
	payload, err := icap.NewLimitedStreamPayload(source, 3)
	if err != nil {
		t.Fatalf("NewLimitedStreamPayload() error = %v", err)
	}
	plan := mustPlanBodyStream(t, completePlanOptions(3))
	stream := &icap.BodyStream{Payload: payload, Plan: plan}

	if _, err := stream.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("WriteTo() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestSequenceStreamPayloadPreservesComponentCloseError(t *testing.T) {
	closeFailure := errors.New("component close failed")
	payload := icap.NewSequenceStreamPayload([]icap.StreamPayload{
		&eofStreamPayload{body: []byte("ab"), closeErr: closeFailure},
		icap.NewBytesStreamPayload([]byte("cd")),
	})
	plan := mustPlanBodyStream(t, completePlanOptions(4))
	stream := &icap.BodyStream{Payload: payload, Plan: plan}

	if _, err := stream.WriteTo(io.Discard); !errors.Is(err, closeFailure) {
		t.Fatalf("WriteTo() error = %v, want component close failure", err)
	}
}

type eofStreamPayload struct {
	body     []byte
	closeErr error
}

func (p *eofStreamPayload) Open() (io.ReadCloser, error) {
	return &eofReadCloser{body: p.body, closeErr: p.closeErr}, nil
}

func (p *eofStreamPayload) SizeHint() (int64, bool) { return int64(len(p.body)), true }

func (p *eofStreamPayload) Replayable() bool { return true }

type eofReadCloser struct {
	body     []byte
	closeErr error
}

func (r *eofReadCloser) Read(p []byte) (int, error) {
	if len(r.body) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.body)
	r.body = r.body[n:]
	return n, io.EOF
}

func (r *eofReadCloser) Close() error { return r.closeErr }

func assertPayloadBody(t *testing.T, reader io.ReadCloser, want string) {
	t.Helper()
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != want {
		t.Fatalf("payload body = %q, want %q", body, want)
	}
}

func assertStreamOutput(t *testing.T, stream *icap.BodyStream, want string) {
	t.Helper()
	var output bytes.Buffer
	if _, err := stream.WriteTo(&output); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := output.String(); got != want {
		t.Fatalf("stream output = %q, want %q", got, want)
	}
}
