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

type fixedRand int

func (r fixedRand) Intn(int) int { return int(r) }

func TestBodyStream_WriteTo_CompleteWritesFinalChunk(t *testing.T) {
	stream := &icap.BodyStream{
		Reader:     strings.NewReader("abcd"),
		ChunkSize:  2,
		FinishMode: icap.StreamFinishComplete,
	}
	var buf bytes.Buffer

	if _, err := stream.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := buf.String(); got != "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n" {
		t.Fatalf("stream output = %q", got)
	}
}

func TestBodyStream_WriteTo_FINOmitsFinalChunk(t *testing.T) {
	stream := &icap.BodyStream{
		Reader:        strings.NewReader("abcd"),
		ChunkSize:     2,
		FinishMode:    icap.StreamFinishFIN,
		FinAfterBytes: 3,
	}
	var buf bytes.Buffer

	if _, err := stream.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := buf.String(); got != "2\r\nab\r\n1\r\nc\r\n" {
		t.Fatalf("stream output = %q", got)
	}
}

func TestBodyStream_WriteTo_WeightedFinishDeterministic(t *testing.T) {
	stream := &icap.BodyStream{
		Reader:          strings.NewReader("xy"),
		ChunkSize:       1,
		FinishMode:      icap.StreamFinishWeighted,
		CompletePercent: 60,
		FinPercent:      40,
		Rand:            fixedRand(70),
	}
	var buf bytes.Buffer

	if _, err := stream.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if strings.Contains(buf.String(), "0\r\n\r\n") {
		t.Fatalf("FIN weighted output included final chunk: %q", buf.String())
	}
}

func TestResponseWriteTo_StreamsEncapsulatedBody(t *testing.T) {
	resp := icap.NewResponse(icap.StatusOK)
	resp.HTTPResponse = &icap.HTTPMessage{
		Proto:      "HTTP/1.1",
		Status:     "200",
		StatusText: "OK",
		Header:     icap.NewHeader(),
		BodyStream: &icap.BodyStream{
			Reader:     strings.NewReader("ok"),
			ChunkSize:  1,
			FinishMode: icap.StreamFinishComplete,
		},
	}
	var buf bytes.Buffer

	if _, err := resp.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Encapsulated: res-hdr=0, res-body=") {
		t.Fatalf("missing res-body encapsulation: %q", output)
	}
	if !strings.Contains(output, "Encapsulated: res-hdr=0, res-body=19") {
		t.Fatalf("unexpected res-body offset: %q", output)
	}
	if !strings.Contains(output, "1\r\no\r\n1\r\nk\r\n0\r\n\r\n") {
		t.Fatalf("missing streamed chunks: %q", output)
	}
}

func TestBodyStream_WriteTo_ReplayablePayloadWritesTwice(t *testing.T) {
	stream := &icap.BodyStream{
		Payload:    icap.NewBytesStreamPayload([]byte("abcd")),
		ChunkSize:  2,
		FinishMode: icap.StreamFinishComplete,
	}
	want := "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n"

	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		if _, err := stream.WriteTo(&buf); err != nil {
			t.Fatalf("WriteTo() iteration %d error = %v", i+1, err)
		}
		if got := buf.String(); got != want {
			t.Fatalf("WriteTo() iteration %d output = %q, want %q", i+1, got, want)
		}
	}
}

func TestBodyStream_WriteTo_OneShotPayloadSecondWriteFails(t *testing.T) {
	stream := &icap.BodyStream{
		Payload:    icap.NewOneShotStreamPayload(newTrackingReadCloser("abc"), 3),
		ChunkSize:  3,
		FinishMode: icap.StreamFinishComplete,
	}

	if _, err := stream.WriteTo(&bytes.Buffer{}); err != nil {
		t.Fatalf("first WriteTo() error = %v", err)
	}
	if _, err := stream.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("second WriteTo() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestBodyStreamClone_ReplayablePayloadWritesIndependently(t *testing.T) {
	stream := &icap.BodyStream{
		Payload:    icap.NewBytesStreamPayload([]byte("abcd")),
		ChunkSize:  2,
		FinishMode: icap.StreamFinishComplete,
	}
	clone := stream.Clone()
	clone.ChunkSize = 1

	assertBodyStreamOutput(t, stream, "2\r\nab\r\n2\r\ncd\r\n0\r\n\r\n")
	assertBodyStreamOutput(t, clone, "1\r\na\r\n1\r\nb\r\n1\r\nc\r\n1\r\nd\r\n0\r\n\r\n")
}

func TestBodyStreamClone_OneShotPayloadReuseFailsClearly(t *testing.T) {
	stream := &icap.BodyStream{
		Payload:    icap.NewOneShotStreamPayload(newTrackingReadCloser("abc"), 3),
		ChunkSize:  3,
		FinishMode: icap.StreamFinishComplete,
	}
	clone := stream.Clone()

	assertBodyStreamOutput(t, stream, "3\r\nabc\r\n0\r\n\r\n")
	if _, err := clone.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("clone WriteTo() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestBodyStreamClone_ReaderFallbackFailsClearly(t *testing.T) {
	stream := &icap.BodyStream{Reader: strings.NewReader("abc"), ChunkSize: 3}
	clone := stream.Clone()

	if _, err := clone.WriteTo(&bytes.Buffer{}); !errors.Is(err, icap.ErrBodyStreamCloneUnavailable) {
		t.Fatalf("clone WriteTo() error = %v, want ErrBodyStreamCloneUnavailable", err)
	}
	assertBodyStreamOutput(t, stream, "3\r\nabc\r\n0\r\n\r\n")
}

func TestBodyStream_WriteTo_NilOneShotPayloadReaderFails(t *testing.T) {
	stream := &icap.BodyStream{Payload: icap.NewOneShotStreamPayload(nil, 0)}

	if _, err := stream.WriteTo(&bytes.Buffer{}); err == nil {
		t.Fatal("WriteTo() error = nil, want nil reader error")
	}
}

func TestBytesStreamPayload_OpensReplayableReaders(t *testing.T) {
	payload := icap.NewBytesStreamPayload([]byte("abc"))

	for i := 0; i < 2; i++ {
		reader, err := payload.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		assertPayloadRead(t, reader, "abc")
	}
}

func TestOneShotStreamPayload_SecondOpenFails(t *testing.T) {
	payload := icap.NewOneShotStreamPayload(newTrackingReadCloser("abc"), 3)

	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	assertPayloadRead(t, reader, "abc")

	if _, err := payload.Open(); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("second Open() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestOneShotStreamPayload_SizeHintAndReplayable(t *testing.T) {
	payload := icap.NewOneShotStreamPayload(newTrackingReadCloser("abc"), 3)
	size, known := payload.SizeHint()

	if size != 3 || !known {
		t.Fatalf("SizeHint() = %d, %v; want 3, true", size, known)
	}
	if payload.Replayable() {
		t.Fatal("Replayable() = true, want false")
	}
}

func TestBodyStream_WriteToClosesPayloadReader(t *testing.T) {
	reader := newTrackingReadCloser("ok")
	stream := &icap.BodyStream{
		Payload:    icap.NewOneShotStreamPayload(reader, 2),
		ChunkSize:  2,
		FinishMode: icap.StreamFinishComplete,
	}

	if _, err := stream.WriteTo(&bytes.Buffer{}); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !reader.closed {
		t.Fatal("payload reader was not closed")
	}
}

func TestBytesStreamPayload_SizeHint(t *testing.T) {
	payload := icap.NewBytesStreamPayload([]byte("abcd"))
	size, known := payload.SizeHint()

	if size != 4 || !known {
		t.Fatalf("SizeHint() = %d, %v; want 4, true", size, known)
	}
	if !payload.Replayable() {
		t.Fatal("Replayable() = false, want true")
	}
}

func TestHTTPMessageBodyStreamPayload_LiveReaderIsOneShot(t *testing.T) {
	msg := &icap.HTTPMessage{Header: icap.NewHeader(), BodyReader: strings.NewReader("abc")}
	payload, err := icap.NewHTTPMessageBodyStreamPayload(msg, 0)
	if err != nil {
		t.Fatalf("NewHTTPMessageBodyStreamPayload() error = %v", err)
	}
	if msg.BodyReader != nil {
		t.Fatal("BodyReader was not claimed")
	}
	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	assertPayloadRead(t, reader, "abc")
	if _, err := payload.Open(); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("second Open() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestHTTPMessageBodyStreamPayload_MaxBytesEnforcedDuringRead(t *testing.T) {
	msg := &icap.HTTPMessage{Header: icap.NewHeader(), BodyReader: strings.NewReader("abcd")}
	payload, err := icap.NewHTTPMessageBodyStreamPayload(msg, 3)
	if err != nil {
		t.Fatalf("NewHTTPMessageBodyStreamPayload() error = %v", err)
	}
	stream := &icap.BodyStream{Payload: payload, ChunkSize: 2, FinishMode: icap.StreamFinishComplete}
	var buf bytes.Buffer
	_, err = stream.WriteTo(&buf)
	if !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("WriteTo() error = %v, want ErrBodyTooLarge", err)
	}
	if strings.Contains(buf.String(), "0\r\n\r\n") {
		t.Fatalf("overflow wrote final chunk: %q", buf.String())
	}
}

func TestHTTPMessageBodyStreamPayload_KnownOversizedLiveBodyFailsEagerly(t *testing.T) {
	reader := newTrackingReadCloser("abcd")
	msg := &icap.HTTPMessage{Header: icap.NewHeader(), BodyReader: reader}
	msg.Header.Set("Content-Length", "4")

	payload, err := icap.NewHTTPMessageBodyStreamPayload(msg, 3)

	if !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("NewHTTPMessageBodyStreamPayload() error = %v, want ErrBodyTooLarge", err)
	}
	if payload != nil {
		t.Fatal("payload created for oversized live body")
	}
	if reader.reads != 0 {
		t.Fatalf("reader read calls = %d, want 0", reader.reads)
	}
	if msg.BodyReader == nil {
		t.Fatal("BodyReader was claimed before eager rejection")
	}
}

func TestSequenceStreamPayload_ReadsInOrderAndIsReplayable(t *testing.T) {
	payload := icap.NewSequenceStreamPayload([]icap.StreamPayload{
		icap.NewBytesStreamPayload([]byte("ab")),
		icap.NewBytesStreamPayload([]byte("cd")),
	})

	for i := 0; i < 2; i++ {
		reader, err := payload.Open()
		if err != nil {
			t.Fatalf("Open() iteration %d error = %v", i+1, err)
		}
		assertPayloadRead(t, reader, "abcd")
	}
}

func TestSequenceStreamPayload_OneShotSecondOpenFails(t *testing.T) {
	payload := icap.NewSequenceStreamPayload([]icap.StreamPayload{
		icap.NewOneShotStreamPayload(newTrackingReadCloser("ab"), 2),
		icap.NewBytesStreamPayload([]byte("cd")),
	})

	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	assertPayloadRead(t, reader, "abcd")
	if _, err := payload.Open(); !errors.Is(err, icap.ErrStreamPayloadConsumed) {
		t.Fatalf("second Open() error = %v, want ErrStreamPayloadConsumed", err)
	}
}

func TestLimitedStreamPayload_MaxBytesEnforcedDuringRead(t *testing.T) {
	source := icap.NewReplayableStreamPayload(func() (io.ReadCloser, error) {
		return newTrackingReadCloser("abcd"), nil
	}, icap.UnknownStreamPayloadSize)
	payload, err := icap.NewLimitedStreamPayload(source, 3)
	if err != nil {
		t.Fatalf("NewLimitedStreamPayload() error = %v", err)
	}
	stream := &icap.BodyStream{Payload: payload, ChunkSize: 2, FinishMode: icap.StreamFinishComplete}

	_, err = stream.WriteTo(&bytes.Buffer{})
	if !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("WriteTo() error = %v, want ErrBodyTooLarge", err)
	}
}

type trackingReadCloser struct {
	*strings.Reader
	closed bool
	reads  int
}

func newTrackingReadCloser(body string) *trackingReadCloser {
	return &trackingReadCloser{Reader: strings.NewReader(body)}
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func assertPayloadRead(t *testing.T, reader io.ReadCloser, want string) {
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

func assertBodyStreamOutput(t *testing.T, stream *icap.BodyStream, want string) {
	t.Helper()
	var buf bytes.Buffer
	if _, err := stream.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Fatalf("stream output = %q, want %q", got, want)
	}
}
