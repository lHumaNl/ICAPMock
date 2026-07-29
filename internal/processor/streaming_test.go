// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestResolveStreamSource_ComposesPartsInOrder(t *testing.T) {
	file := writeProcessorTempFile(t, "footer")
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody("text/plain", []byte("REQ"))
	stream := &storage.StreamConfig{Parts: []storage.StreamPartConfig{
		{From: "request_body"},
		{From: "body", Body: "\n-- marker --\n"},
		{From: "body_file", BodyFile: file},
		{From: "request_http_body"},
	}}
	body, err := resolveStreamSource(stream, req)
	if err != nil {
		t.Fatalf("resolveStreamSource() error = %v", err)
	}
	if got, want := string(body), "REQ\n-- marker --\nfooterREQ"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestResolveStreamSource_MultipartSelectsFieldsAndFiles(t *testing.T) {
	body, contentType := multipartTestBody(t)
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody(contentType, body)
	stream := multipartStream(storage.StreamMultipartConfig{
		Fields: []string{"comment"},
		Files:  storage.StreamMultipartFilesConfig{Enabled: true, IsSet: true, Filename: []string{`.*\.exe$`}},
		IsSet:  true,
	})
	selected, err := resolveStreamSource(stream, req)
	if err != nil {
		t.Fatalf("resolveStreamSource() error = %v", err)
	}
	if got, want := string(selected), "helloEXE"; got != want {
		t.Fatalf("selected body = %q, want %q", got, want)
	}
}

func TestResolveStreamSource_RawFileFallbackForNonMultipart(t *testing.T) {
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody("application/octet-stream", []byte("RAW"))
	req.HTTPRequest.Header.Set("Content-Disposition", `attachment; filename="sample.exe"`)
	stream := multipartStream(storage.StreamMultipartConfig{Files: enabledFiles(), IsSet: true})
	stream.Fallback.RawFile = storage.StreamRawFileFallback{Enabled: true, IsSet: true, Filename: []string{`.*\.exe$`}}
	selected, err := resolveStreamSource(stream, req)
	if err != nil {
		t.Fatalf("resolveStreamSource() error = %v", err)
	}
	if string(selected) != "RAW" {
		t.Fatalf("selected body = %q, want RAW", selected)
	}
}

func TestResolveStreamSource_OversizedHTTPBodyFailsPredictably(t *testing.T) {
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithReader("application/octet-stream", bytes.NewReader([]byte("0123456789")))
	stream := &storage.StreamConfig{Source: storage.StreamSourceConfig{From: "request_http_body"}}

	body, err := resolveStreamSourceWithLimit(stream, req, 8)

	if !errors.Is(err, errStreamSourceBodyTooLarge) {
		t.Fatalf("error = %v, want stream body limit error", err)
	}
	if body != nil {
		t.Fatalf("body = %q, want nil", body)
	}
}

func TestMockProcessor_StreamResponseBodyReadsOnlyLimit(t *testing.T) {
	const limit int64 = 8
	reader := &fixedSizeCountingReader{remaining: limit + 64}
	req := createLazyRESPMODRequest(t, reader)
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(responseBodyStreamScenario(icap.StreamFinishComplete)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessorWithMaxBodySize(registry, createTestLogger(t), limit)
	resp, err := proc.Process(context.Background(), req)

	if !errors.Is(err, errStreamSourceBodyTooLarge) {
		t.Fatalf("Process() error = %v, want body limit error", err)
	}
	if resp != nil {
		t.Fatalf("Process() response = %v, want nil", resp)
	}
	if reader.read > limit+1 {
		t.Fatalf("read %d bytes, want at most %d", reader.read, limit+1)
	}
}

func TestMockProcessor_KnownOversizedLiveBodyFailsBeforeStreaming(t *testing.T) {
	const limit int64 = 8
	reader := &fixedSizeCountingReader{remaining: limit + 1}
	req := createLazyRESPMODRequest(t, reader)
	req.HTTPResponse.Header.Set("Content-Length", "9")
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(responseBodyStreamScenario(icap.StreamFinishComplete)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessorWithMaxBodySize(registry, createTestLogger(t), limit)
	_, err := proc.Process(context.Background(), req)

	if !errors.Is(err, errStreamSourceBodyTooLarge) {
		t.Fatalf("Process() error = %v, want body limit error", err)
	}
	assertNoBodyReadBeforeWrite(t, reader)
	if req.HTTPResponse.BodyReader == nil {
		t.Fatal("BodyReader was claimed before eager rejection")
	}
}

func TestMockProcessor_UnknownSizeStreamClearsStaleContentLength(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 4}
	req := createLazyREQMODRequest(t, reader)
	scenario := rawRequestHTTPBodyStreamScenario()
	scenario.Response.HTTPHeaders = map[string]string{"Content-Length": "999"}
	proc := processSingleScenario(t, scenario)

	resp, err := proc.Process(context.Background(), req)

	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if got, ok := resp.HTTPResponse.Header.Get("Content-Length"); !ok || got != "4" {
		t.Fatalf("Content-Length = %q, %v, want 4, true", got, ok)
	}
	assertFourBodyBytesReadDuringProcess(t, reader)
	assertBodyReaderCleared(t, req.HTTPRequest)
}

func TestMockProcessor_RequestHTTPBodyStreamReadsDuringProcess(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 4}
	req := createLazyREQMODRequest(t, reader)
	resp := processSingleScenario(t, rawRequestHTTPBodyStreamScenario())

	procResp, err := resp.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertFourBodyBytesReadDuringProcess(t, reader)
	assertBodyReaderCleared(t, req.HTTPRequest)
	assertStreamOutput(t, procResp, "2\r\nxx\r\n2\r\nxx\r\n0\r\n\r\n")
}

func TestMockProcessor_ResponseHTTPBodyStreamReadsDuringProcess(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 4}
	req := createLazyRESPMODRequest(t, reader)
	resp := processSingleScenario(t, rawResponseHTTPBodyStreamScenario())

	procResp, err := resp.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertFourBodyBytesReadDuringProcess(t, reader)
	assertBodyReaderCleared(t, req.HTTPResponse)
	assertStreamOutput(t, procResp, "2\r\nxx\r\n2\r\nxx\r\n0\r\n\r\n")
}

func TestMockProcessor_AdaptedHTTPBodyStreamSelectsSourceByICAPMethod(t *testing.T) {
	tests := []struct {
		request func(*testing.T, io.Reader) *icap.Request
		method  string
		name    string
	}{
		{name: "REQMOD", method: icap.MethodREQMOD, request: createLazyREQMODRequest},
		{name: "RESPMOD", method: icap.MethodRESPMOD, request: createLazyRESPMODRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fixedSizeCountingReader{remaining: 4}
			req := tt.request(t, reader)
			scenario := rawHTTPBodyStreamScenario(tt.method, "adapted_http_body")
			proc := processSingleScenario(t, scenario)

			resp, err := proc.Process(context.Background(), req)
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			assertFourBodyBytesReadDuringProcess(t, reader)
			assertStreamOutput(t, resp, "2\r\nxx\r\n2\r\nxx\r\n0\r\n\r\n")
		})
	}
}

func TestMockProcessor_ResponseHTTPBodyStreamPreservesChunkDelayAndFINSettings(t *testing.T) {
	scenario := rawResponseHTTPBodyStreamScenario()
	scenario.Response.Stream.Chunks.Delay = storage.DurationSpec{
		Min:   2 * time.Millisecond,
		Max:   2 * time.Millisecond,
		IsSet: true,
	}
	scenario.Response.Stream.StartDelay = storage.DurationSpec{
		Min:   5 * time.Millisecond,
		Max:   7 * time.Millisecond,
		IsSet: true,
	}
	scenario.Response.Stream.Finish = storage.StreamFinishConfig{
		Mode: icap.StreamFinishFIN,
		Fin: storage.StreamFINConfig{
			Close: "clean",
			After: storage.StreamFINAfterConfig{
				Bytes: storage.SizeSpec{Min: 3, Max: 3, IsSet: true},
			},
		},
	}

	resp, err := processSingleScenario(t, scenario).Process(context.Background(), createLazyRESPMODRequest(t, strings.NewReader("wxyz")))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if resp.HTTPResponse == nil || resp.HTTPResponse.BodyStream == nil {
		t.Fatal("response BodyStream = nil")
	}
	stream := resp.HTTPResponse.BodyStream
	if stream.ChunkSize != 2 {
		t.Fatalf("ChunkSize = %d, want 2", stream.ChunkSize)
	}
	if stream.Delay != 2*time.Millisecond {
		t.Fatalf("Delay = %v, want %v", stream.Delay, 2*time.Millisecond)
	}
	if stream.StartDelay != 5*time.Millisecond || stream.StartDelayMax != 7*time.Millisecond {
		t.Fatalf("StartDelay = %v-%v, want 5ms-7ms", stream.StartDelay, stream.StartDelayMax)
	}
	if stream.FinishMode != icap.StreamFinishFIN {
		t.Fatalf("FinishMode = %q, want %q", stream.FinishMode, icap.StreamFinishFIN)
	}
	if stream.FinAfterBytes != 3 {
		t.Fatalf("FinAfterBytes = %d, want 3", stream.FinAfterBytes)
	}
	if got, ok := resp.GetHeader("Connection"); ok {
		t.Fatalf("Connection header = %q, %v, want absent", got, ok)
	}
	if !resp.CloseAfterWrite() {
		t.Fatal("CloseAfterWrite = false, want true")
	}
}

func TestMockProcessor_NewFINPercentStreamCapsBodyBytes(t *testing.T) {
	scenario := newPercentFINStreamScenario("abcdefghij", 40)
	resp, err := processSingleScenario(t, scenario).Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	stream := resp.HTTPResponse.BodyStream
	if stream.FinAfterBytes != 4 || !stream.FinAfterBytesSet {
		t.Fatalf("FinAfterBytes = %d/%v, want 4/true", stream.FinAfterBytes, stream.FinAfterBytesSet)
	}
	stream.Sleep = func(time.Duration) {}
	assertStreamOutput(t, resp, "3\r\nabc\r\n1\r\nd\r\n")
}

func TestMockProcessor_NewCompleteDurationWithThrottleSetsStopLimit(t *testing.T) {
	scenario := newCompleteDurationThrottleScenario()
	resp, err := processSingleScenario(t, scenario).Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	stream := resp.HTTPResponse.BodyStream
	if stream.Delay != 3*time.Millisecond {
		t.Fatalf("Delay = %v, want 3ms", stream.Delay)
	}
	if stream.DelayStopAfter != 5*time.Millisecond {
		t.Fatalf("DelayStopAfter = %v, want 5ms", stream.DelayStopAfter)
	}
}

func TestMockProcessor_NewFINPercentRequiresKnownSize(t *testing.T) {
	scenario := newRawPercentFINStreamScenario()
	proc := processSingleScenario(t, scenario)
	resp, err := proc.Process(context.Background(), createLazyRESPMODRequest(t, strings.NewReader("abcd")))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if resp.HTTPResponse.BodyStream.FinAfterBytes != 1 {
		t.Fatalf("FinAfterBytes = %d, want 1", resp.HTTPResponse.BodyStream.FinAfterBytes)
	}
}

func TestMockProcessor_MultipartSelectorReadsDuringProcess(t *testing.T) {
	body, contentType := multipartTestBody(t)
	reader := newProcessorByteCountingReader(body)
	req := createLazyREQMODRequest(t, reader)
	req.HTTPRequest.Header.Set("Content-Type", contentType)
	proc := processSingleScenario(t, multipartSelectorStreamScenario(selectedMultipart()))

	resp, err := proc.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertByteBodyReadDuringProcess(t, reader, int64(len(body)))
	assertBodyReaderCleared(t, req.HTTPRequest)
	assertStreamOutput(t, resp, "8\r\nhelloEXE\r\n0\r\n\r\n")
}

func TestMockProcessor_MultipartStreamingEnforcesSourceLimit(t *testing.T) {
	body, contentType := multipartTestBody(t)
	reader := newProcessorByteCountingReader(body)
	req := createLazyREQMODRequest(t, reader)
	req.HTTPRequest.Header.Set("Content-Type", contentType)
	proc := processSingleScenarioWithLimit(t, multipartSelectorStreamScenario(selectedMultipart()), 8)

	_, err := proc.Process(context.Background(), req)
	if !errors.Is(err, errStreamSourceBodyTooLarge) {
		t.Fatalf("Process() error = %v, want stream body limit error", err)
	}
}

func TestMockProcessor_MultipartStreamingNoMatchModes(t *testing.T) {
	body, contentType := multipartTestBody(t)
	for _, tt := range multipartStreamingNoMatchCases() {
		t.Run(tt.name, func(t *testing.T) {
			req := createLazyMultipartREQMODRequest(t, body, contentType)
			resp, err := processSingleScenario(t, tt.scenario).Process(context.Background(), req)
			assertMultipartStreamingNoMatch(t, resp, err, tt.want, tt.wantErr)
		})
	}
}

func TestMockProcessor_MultipartRawFileFallbackRemainsBuffered(t *testing.T) {
	body, contentType := multipartTestBody(t)
	reader := newProcessorByteCountingReader(body)
	req := createLazyREQMODRequest(t, reader)
	req.HTTPRequest.Header.Set("Content-Type", contentType)
	proc := processSingleScenario(t, multipartRawFileFallbackScenario())

	_, err := proc.Process(context.Background(), req)
	if err == nil {
		t.Fatal("Process() error = nil, want no-match error")
	}
	if reader.read != int64(len(body)) {
		t.Fatalf("read %d bytes before Process returned, want buffered %d", reader.read, len(body))
	}
}

func TestMockProcessor_BufferedHTTPBodyStreamWritesTwice(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 4}
	proc := processSingleScenario(t, rawResponseHTTPBodyStreamScenario())
	resp, err := proc.Process(context.Background(), createLazyRESPMODRequest(t, reader))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if _, err := resp.WriteTo(&bytes.Buffer{}); err != nil {
		t.Fatalf("first WriteTo() error = %v", err)
	}
	if _, err := resp.WriteTo(&bytes.Buffer{}); err != nil {
		t.Fatalf("second WriteTo() error = %v", err)
	}
}

func TestMockProcessor_RepeatedLiveHTTPBodyPartsFailFast(t *testing.T) {
	for _, tt := range repeatedLiveHTTPBodyPartCases() {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fixedSizeCountingReader{remaining: 4}
			proc := processSingleScenario(t, rawHTTPBodyPartsScenario(tt.method, tt.parts))

			_, err := proc.Process(context.Background(), tt.request(t, reader))

			if !errors.Is(err, errRepeatedLiveStreamPart) {
				t.Fatalf("Process() error = %v, want repeated live part error", err)
			}
			assertNoBodyReadBeforeWrite(t, reader)
		})
	}
}

func TestMockProcessor_RepeatedCachedHTTPBodyPartsAreReplayable(t *testing.T) {
	parts := []storage.StreamPartConfig{{From: "response_http_body"}, {From: "response_body"}}
	proc := processSingleScenario(t, rawHTTPBodyPartsScenario(icap.MethodRESPMOD, parts))

	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))

	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertStreamOutput(t, resp, "1\r\nw\r\n1\r\nx\r\n1\r\ny\r\n1\r\nz\r\n1\r\nw\r\n1\r\nx\r\n1\r\ny\r\n1\r\nz\r\n")
}

func BenchmarkMockProcessor_RawHTTPBodyStreamBuffersPayload(b *testing.B) {
	proc := processSingleScenarioB(b, rawResponseHTTPBodyStreamScenario())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader := &fixedSizeCountingReader{remaining: 64 << 20}
		if _, err := proc.Process(context.Background(), createLazyRESPMODRequestB(b, reader)); err == nil {
			b.Fatal("Process() error = nil, want body limit error")
		}
		if reader.read == 0 {
			b.Fatal("body was not read during Process")
		}
	}
}

// BenchmarkMockProcessor_RawHTTPBodyStreamProcessOnlyLarge measures Process()
// for a large live HTTP body after buffering it for response safety.
func BenchmarkMockProcessor_RawHTTPBodyStreamProcessOnlyLarge(b *testing.B) {
	const bodySize = 16 << 20
	proc := processSingleScenarioUnlimitedB(b, benchmarkRawHTTPBodyStreamScenario())
	ctx := context.Background()

	b.SetBytes(bodySize)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := &fixedSizeCountingReader{remaining: bodySize}
		if _, err := proc.Process(ctx, createLazyRESPMODRequestB(b, reader)); err != nil {
			b.Fatalf("Process() error = %v", err)
		}
		if reader.read != bodySize {
			b.Fatalf("read %d bytes during Process, want %d", reader.read, bodySize)
		}
	}
}

// BenchmarkMockProcessor_RawHTTPBodyStreamProcessAndWriteDiscardLarge measures the
// end-to-end buffered input and streaming output path for a large live HTTP body.
func BenchmarkMockProcessor_RawHTTPBodyStreamProcessAndWriteDiscardLarge(b *testing.B) {
	const bodySize = 16 << 20
	proc := processSingleScenarioUnlimitedB(b, benchmarkRawHTTPBodyStreamScenario())
	ctx := context.Background()

	b.SetBytes(bodySize)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := &fixedSizeCountingReader{remaining: bodySize}
		resp, err := proc.Process(ctx, createLazyRESPMODRequestB(b, reader))
		if err != nil {
			b.Fatalf("Process() error = %v", err)
		}
		if reader.read != bodySize {
			b.Fatalf("read %d bytes during Process, want %d", reader.read, bodySize)
		}
		if _, err := resp.WriteTo(io.Discard); err != nil {
			b.Fatalf("WriteTo() error = %v", err)
		}
	}
}

// BenchmarkMockProcessor_BodyFileStreamProcessAndWriteDiscardLarge measures
// Process()+WriteTo(io.Discard) for a large body_file-backed response stream.
func BenchmarkMockProcessor_BodyFileStreamProcessAndWriteDiscardLarge(b *testing.B) {
	const bodySize = 16 << 20
	path := writeBenchmarkSizedFile(b, "body-file.bin", bodySize)
	proc := processSingleScenarioUnlimitedB(b, benchmarkBodyFileStreamScenario(path))
	ctx := context.Background()

	b.SetBytes(bodySize)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := proc.Process(ctx, createTestRESPMODRequestB(b))
		if err != nil {
			b.Fatalf("Process() error = %v", err)
		}
		if _, err := resp.WriteTo(io.Discard); err != nil {
			b.Fatalf("WriteTo() error = %v", err)
		}
	}
}

// BenchmarkMockProcessor_PartsStreamProcessAndWriteDiscardLarge measures a mixed
// parts response that combines inline data, body_file data, and a live HTTP body.
func BenchmarkMockProcessor_PartsStreamProcessAndWriteDiscardLarge(b *testing.B) {
	const (
		fileBodySize = 8 << 20
		httpBodySize = 8 << 20
	)
	path := writeBenchmarkSizedFile(b, "parts-body.bin", fileBodySize)
	proc := processSingleScenarioUnlimitedB(b, benchmarkPartsStreamScenario(path))
	ctx := context.Background()
	totalBodySize := int64(len("prefix-")) + fileBodySize + int64(len("-middle-")) + httpBodySize + int64(len("-suffix"))

	b.SetBytes(totalBodySize)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := &fixedSizeCountingReader{remaining: httpBodySize}
		resp, err := proc.Process(ctx, createLazyRESPMODRequestB(b, reader))
		if err != nil {
			b.Fatalf("Process() error = %v", err)
		}
		if reader.read != 0 {
			b.Fatalf("read %d bytes before WriteTo, want 0", reader.read)
		}
		if _, err := resp.WriteTo(io.Discard); err != nil {
			b.Fatalf("WriteTo() error = %v", err)
		}
		if reader.read != httpBodySize {
			b.Fatalf("read %d bytes after WriteTo, want %d", reader.read, httpBodySize)
		}
	}
}

// BenchmarkMockProcessor_MultipartSelectedFileProcessAndWriteDiscardLarge measures
// the safe reader-backed multipart path when the selected file arrives late.
func BenchmarkMockProcessor_MultipartSelectedFileProcessAndWriteDiscardLarge(b *testing.B) {
	const selectedFileSize = 8 << 20
	multipartPath, contentType, multipartSize := writeBenchmarkMultipartFile(b, 8<<20, selectedFileSize)
	proc := processSingleScenarioUnlimitedB(b, benchmarkMultipartSelectorScenario(storage.StreamMultipartConfig{
		Files: storage.StreamMultipartFilesConfig{Enabled: true, IsSet: true, Filename: []string{`selected\.exe$`}},
		IsSet: true,
	}))
	ctx := context.Background()

	b.SetBytes(multipartSize)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		file, err := os.Open(multipartPath)
		if err != nil {
			b.Fatalf("Open() error = %v", err)
		}
		reader := &benchmarkCountingReadCloser{ReadCloser: file}
		resp, err := proc.Process(ctx, createLazyMultipartREQMODRequestB(b, reader, contentType))
		if err != nil {
			_ = file.Close()
			b.Fatalf("Process() error = %v", err)
		}
		if reader.read != 0 {
			_ = file.Close()
			b.Fatalf("read %d bytes before WriteTo, want 0", reader.read)
		}
		if _, err := resp.WriteTo(io.Discard); err != nil {
			_ = file.Close()
			b.Fatalf("WriteTo() error = %v", err)
		}
	}
}

func TestMockProcessor_StreamInlineBodyDoesNotCloneOriginalBody(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 64}
	req := createLazyRESPMODRequest(t, reader)
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(inlineBodyStreamScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	proc := NewMockProcessorWithMaxBodySize(registry, createTestLogger(t), 8)
	_, err := proc.Process(context.Background(), req)

	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if reader.read != 0 {
		t.Fatalf("read %d original body bytes, want 0", reader.read)
	}
}

func TestMockProcessor_StreamInlineBodyPayloadIsReplayable(t *testing.T) {
	proc := processSingleScenario(t, inlineBodyStreamScenario())
	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		if _, err := resp.WriteTo(&out); err != nil {
			t.Fatalf("WriteTo() iteration %d error = %v", i+1, err)
		}
		if !strings.Contains(out.String(), "2\r\nok\r\n0\r\n\r\n") {
			t.Fatalf("WriteTo() iteration %d output = %q", i+1, out.String())
		}
	}
}

func TestMockProcessor_BodyFileStreamsAfterWriteToAndCloses(t *testing.T) {
	reader := newProcessorTrackingReadCloser("file")
	state := stubProcessorStreamFile(t, writeProcessorTempFile(t, "ignored"), 4, reader)
	proc := processSingleScenario(t, bodyFileStreamScenario(state.path, icap.StreamFinishComplete))

	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if state.opens != 0 || reader.read != 0 {
		t.Fatalf("opened/read before WriteTo = %d/%d, want 0/0", state.opens, reader.read)
	}
	assertStreamOutput(t, resp, "2\r\nfi\r\n2\r\nle\r\n0\r\n\r\n")
	if !reader.closed {
		t.Fatal("body_file reader was not closed")
	}
}

func TestMockProcessor_BodyFileMaxSizeOverflowDuringWrite(t *testing.T) {
	reader := newProcessorTrackingReadCloser("abcd")
	state := stubProcessorStreamFile(t, writeProcessorTempFile(t, "ignored"), 3, reader)
	proc := processSingleScenarioWithLimit(t, bodyFileStreamScenario(state.path, icap.StreamFinishComplete), 3)

	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	var out bytes.Buffer
	_, err = resp.WriteTo(&out)
	if !errors.Is(err, icap.ErrBodyTooLarge) {
		t.Fatalf("WriteTo() error = %v, want ErrBodyTooLarge", err)
	}
	if strings.Contains(out.String(), "0\r\n\r\n") {
		t.Fatalf("overflow wrote final chunk: %q", out.String())
	}
}

func TestMockProcessor_SimplePartsStreamInOrderWithoutPreConcat(t *testing.T) {
	reader := newProcessorTrackingReadCloser("B")
	state := stubProcessorStreamFile(t, writeProcessorTempFile(t, "ignored"), 1, reader)
	scenario := partsStreamScenario([]storage.StreamPartConfig{
		{Body: "A"},
		{BodyFile: state.path},
		{Body: "C"},
	}, icap.StreamFinishComplete)
	proc := processSingleScenario(t, scenario)

	resp, err := proc.Process(context.Background(), createTestRESPMODRequest(t))
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if state.opens != 0 || reader.read != 0 {
		t.Fatalf("opened/read before WriteTo = %d/%d, want 0/0", state.opens, reader.read)
	}
	if got, _ := resp.HTTPResponse.Header.Get("Content-Length"); got != "3" {
		t.Fatalf("Content-Length = %q, want 3", got)
	}
	assertStreamOutput(t, resp, "1\r\nA\r\n1\r\nB\r\n1\r\nC\r\n0\r\n\r\n")
}

func TestMockProcessor_FINWorksForBodyFileAndParts(t *testing.T) {
	bodyFile := writeProcessorTempFile(t, "abcd")
	tests := []struct {
		name     string
		scenario *storage.Scenario
		want     string
	}{
		{name: "body_file", scenario: bodyFileStreamScenario(bodyFile, icap.StreamFinishFIN), want: "2\r\nab\r\n1\r\nc\r\n"},
		{name: "parts", scenario: partsStreamScenario([]storage.StreamPartConfig{
			{Body: "ab"}, {Body: "cd"},
		}, icap.StreamFinishFIN), want: "1\r\na\r\n1\r\nb\r\n1\r\nc\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := processSingleScenario(t, tt.scenario).Process(context.Background(), createTestRESPMODRequest(t))
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			assertStreamOutput(t, resp, tt.want)
		})
	}
}

func TestMockProcessor_CloneHTTPMessageBodyZeroLimitIsUnlimited(t *testing.T) {
	reader := &fixedSizeCountingReader{remaining: 64}
	msg := &icap.HTTPMessage{Proto: "HTTP/1.1", Header: icap.NewHeader(), BodyReader: reader}
	clone := &icap.HTTPMessage{Proto: "HTTP/1.1", Header: icap.NewHeader()}
	proc := NewMockProcessorWithMaxBodySize(storage.NewScenarioRegistry(), createTestLogger(t), 0)

	err := proc.cloneHTTPMessageBody(clone, msg)
	if err != nil {
		t.Fatalf("cloneHTTPMessageBody() error = %v", err)
	}
	body, err := clone.GetBody()
	if err != nil {
		t.Fatalf("clone.GetBody() error = %v", err)
	}
	if len(body) != 64 {
		t.Fatalf("cloned body len = %d, want 64", len(body))
	}
	if reader.read != 64 {
		t.Fatalf("read %d bytes, want 64", reader.read)
	}
}

func TestSelectMultipartBody_OversizedSelectedOutputFailsPredictably(t *testing.T) {
	body, contentType := multipartBodyWithLargeSelectedFile(t)
	source := streamHTTPSource{message: httpMessageWithBody(contentType, body), body: body}
	cfg := storage.StreamMultipartConfig{Files: enabledFiles(), IsSet: true}

	selected, err := selectMultipartBody(source, cfg, 8)

	if !errors.Is(err, errStreamSourceBodyTooLarge) {
		t.Fatalf("error = %v, want stream body limit error", err)
	}
	if selected != nil {
		t.Fatalf("selected = %q, want nil", selected)
	}
}

func TestResolveStreamSource_MultipartNoMatchSkipsRawFileFallback(t *testing.T) {
	body, contentType := multipartTestBody(t)
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody(contentType, body)
	stream := multipartRawFileFallbackStream()
	got, err := resolveStreamSource(stream, req)
	if err == nil {
		t.Fatalf("resolveStreamSource() error = nil, body = %q", got)
	}
	if bytes.Equal(got, body) {
		t.Fatal("raw_file fallback returned the multipart MIME envelope")
	}
}

func TestResolveStreamSource_MultipartNoMatchUsesBodyFileFallback(t *testing.T) {
	body, contentType := multipartTestBody(t)
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody(contentType, body)
	stream := multipartStream(noMatchMultipart())
	stream.Fallback.BodyFile = writeProcessorTempFile(t, "file-fallback")
	got, err := resolveStreamSource(stream, req)
	if err != nil {
		t.Fatalf("resolveStreamSource() error = %v", err)
	}
	if string(got) != "file-fallback" {
		t.Fatalf("body = %q, want file-fallback", got)
	}
}

func TestResolveStreamSource_MultipartNoMatchUsesAdaptedBodyFallback(t *testing.T) {
	body, contentType := multipartTestBody(t)
	tests := []struct {
		prepare func(*testing.T) *icap.Request
		name    string
	}{
		{name: "REQMOD", prepare: func(t *testing.T) *icap.Request {
			req := createTestREQMODRequest(t)
			req.HTTPRequest = httpMessageWithBody(contentType, body)
			return req
		}},
		{name: "RESPMOD", prepare: func(t *testing.T) *icap.Request {
			req := createTestRESPMODRequest(t)
			req.HTTPResponse = httpMessageWithBody(contentType, body)
			return req
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := &storage.StreamConfig{
				Source:    storage.StreamSourceConfig{From: "adapted_http_body"},
				Multipart: noMatchMultipart(),
				Fallback:  storage.StreamFallbackConfig{From: "adapted_http_body"},
			}
			got, err := resolveStreamSource(stream, tt.prepare(t))
			if err != nil {
				t.Fatalf("resolveStreamSource() error = %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("body = %q, want original multipart body", got)
			}
		})
	}
}

func TestResolveStreamSource_MultipartNoMatchModes(t *testing.T) {
	body, contentType := multipartTestBody(t)
	for _, tt := range multipartNoMatchCases() {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestREQMODRequest(t)
			req.HTTPRequest = httpMessageWithBody(contentType, body)
			got, err := resolveStreamSource(tt.stream, req)
			assertNoMatchMode(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestResolveStreamSource_MultipartRejectsEmptyBoundary(t *testing.T) {
	req := createTestREQMODRequest(t)
	req.HTTPRequest = httpMessageWithBody("multipart/form-data; boundary=\"\"", []byte("ignored"))
	_, err := resolveStreamSource(multipartStream(selectedMultipart()), req)
	if err == nil || !strings.Contains(err.Error(), "boundary") {
		t.Fatalf("resolveStreamSource() error = %v, want empty boundary error", err)
	}
}

type multipartNoMatchCase struct {
	stream  *storage.StreamConfig
	name    string
	want    string
	wantErr bool
}

type multipartStreamingNoMatchCase struct {
	scenario *storage.Scenario
	name     string
	want     string
	wantErr  bool
}

type repeatedLiveHTTPBodyPartCase struct {
	request func(*testing.T, io.Reader) *icap.Request
	parts   []storage.StreamPartConfig
	name    string
	method  string
}

func repeatedLiveHTTPBodyPartCases() []repeatedLiveHTTPBodyPartCase {
	return []repeatedLiveHTTPBodyPartCase{
		{request: createLazyREQMODRequest, method: icap.MethodREQMOD, name: "request aliases", parts: []storage.StreamPartConfig{
			{From: "request_http_body"}, {Body: "-"}, {From: "request_body"},
		}},
		{request: createLazyRESPMODRequest, method: icap.MethodRESPMOD, name: "response aliases", parts: []storage.StreamPartConfig{
			{From: "response_body"}, {Body: "-"}, {From: "response_http_body"},
		}},
	}
}

func multipartNoMatchCases() []multipartNoMatchCase {
	return []multipartNoMatchCase{
		{name: "error", stream: multipartStream(noMatchMultipart()), wantErr: true},
		{name: "allow-empty", stream: multipartStream(allowEmptyMultipart())},
		{name: "fallback", stream: multipartFallbackStream(), want: "fallback"},
		{name: "raw-file-allow-empty", stream: multipartRawFileAllowEmptyStream()},
	}
}

func multipartStreamingNoMatchCases() []multipartStreamingNoMatchCase {
	return []multipartStreamingNoMatchCase{
		{name: "error", scenario: multipartSelectorStreamScenario(noMatchMultipart()), wantErr: true},
		{name: "allow-empty", scenario: multipartSelectorStreamScenario(allowEmptyMultipart())},
		{name: "fallback", scenario: multipartBodyFallbackScenario(), want: "fallback"},
	}
}

func noMatchMultipart() storage.StreamMultipartConfig {
	return storage.StreamMultipartConfig{Files: storage.StreamMultipartFilesConfig{
		Enabled: true, IsSet: true, Filename: []string{`nomatch$`},
	}, IsSet: true}
}

func allowEmptyMultipart() storage.StreamMultipartConfig {
	cfg := noMatchMultipart()
	cfg.AllowEmpty = true
	return cfg
}

func multipartFallbackStream() *storage.StreamConfig {
	stream := multipartStream(noMatchMultipart())
	stream.Fallback.Body = "fallback"
	return stream
}

func multipartRawFileFallbackStream() *storage.StreamConfig {
	stream := multipartStream(noMatchMultipart())
	stream.Fallback.RawFile = storage.StreamRawFileFallback{Enabled: true, IsSet: true}
	return stream
}

func multipartRawFileAllowEmptyStream() *storage.StreamConfig {
	stream := multipartRawFileFallbackStream()
	stream.Multipart.AllowEmpty = true
	return stream
}

func multipartStream(cfg storage.StreamMultipartConfig) *storage.StreamConfig {
	return &storage.StreamConfig{Source: storage.StreamSourceConfig{From: "request_http_body"}, Multipart: cfg}
}

func enabledFiles() storage.StreamMultipartFilesConfig {
	return storage.StreamMultipartFilesConfig{Enabled: true, IsSet: true}
}

func selectedMultipart() storage.StreamMultipartConfig {
	return storage.StreamMultipartConfig{
		Fields: []string{"comment"},
		Files:  storage.StreamMultipartFilesConfig{Enabled: true, IsSet: true, Filename: []string{`.*\.exe$`}},
		IsSet:  true,
	}
}

func multipartSelectorStreamScenario(cfg storage.StreamMultipartConfig) *storage.Scenario {
	scenario := rawRequestHTTPBodyStreamScenario()
	scenario.Response.Stream.Multipart = cfg
	scenario.Response.Stream.Chunks.Size = storage.SizeSpec{Min: 16, Max: 16, IsSet: true}
	return scenario
}

func multipartRawFileFallbackScenario() *storage.Scenario {
	scenario := multipartSelectorStreamScenario(noMatchMultipart())
	scenario.Response.Stream.Fallback.RawFile = storage.StreamRawFileFallback{Enabled: true, IsSet: true}
	return scenario
}

func multipartBodyFallbackScenario() *storage.Scenario {
	scenario := multipartSelectorStreamScenario(noMatchMultipart())
	scenario.Response.Stream.Fallback.Body = "fallback"
	return scenario
}

type fixedSizeCountingReader struct {
	remaining int64
	read      int64
}

type processorByteCountingReader struct {
	*bytes.Reader
	read int64
}

func (r *fixedSizeCountingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := int64(0); i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

func newProcessorByteCountingReader(body []byte) *processorByteCountingReader {
	return &processorByteCountingReader{Reader: bytes.NewReader(body)}
}

func (r *processorByteCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	return n, err
}

func createLazyRESPMODRequest(t *testing.T, body io.Reader) *icap.Request {
	t.Helper()
	req := createTestRESPMODRequest(t)
	req.HTTPResponse = &icap.HTTPMessage{Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader()}
	req.HTTPResponse.BodyReader = body
	return req
}

func createLazyREQMODRequest(t *testing.T, body io.Reader) *icap.Request {
	t.Helper()
	req := createTestREQMODRequest(t)
	req.HTTPRequest = &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPRequest.BodyReader = body
	return req
}

func createLazyMultipartREQMODRequest(t *testing.T, body []byte, contentType string) *icap.Request {
	t.Helper()
	req := createLazyREQMODRequest(t, newProcessorByteCountingReader(body))
	req.HTTPRequest.Header.Set("Content-Type", contentType)
	return req
}

func createLazyMultipartREQMODRequestB(b *testing.B, body io.Reader, contentType string) *icap.Request {
	b.Helper()
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/avscan")
	if err != nil {
		b.Fatalf("failed to create request: %v", err)
	}
	req.HTTPRequest = &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPRequest.Header.Set("Content-Type", contentType)
	req.HTTPRequest.BodyReader = body
	return req
}

func createLazyRESPMODRequestB(b *testing.B, body io.Reader) *icap.Request {
	b.Helper()
	req, err := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/avscan")
	if err != nil {
		b.Fatalf("failed to create request: %v", err)
	}
	req.HTTPRequest = &icap.HTTPMessage{Method: "GET", URI: "/download", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPResponse = &icap.HTTPMessage{Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader()}
	req.HTTPResponse.BodyReader = body
	return req
}

func createTestRESPMODRequestB(b *testing.B) *icap.Request {
	b.Helper()
	req, err := icap.NewRequest(icap.MethodRESPMOD, "icap://localhost/avscan")
	if err != nil {
		b.Fatalf("failed to create request: %v", err)
	}
	req.HTTPRequest = &icap.HTTPMessage{Method: "GET", URI: "/download", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPResponse = &icap.HTTPMessage{Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader()}
	req.HTTPResponse.SetLoadedBody([]byte("wxyz"))
	return req
}

func processSingleScenario(t *testing.T, scenario *storage.Scenario) *MockProcessor {
	t.Helper()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return NewMockProcessor(registry, nil)
}

func processSingleScenarioWithLimit(t *testing.T, scenario *storage.Scenario, limit int64) *MockProcessor {
	t.Helper()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return NewMockProcessorWithMaxBodySize(registry, nil, limit)
}

func processSingleScenarioB(b *testing.B, scenario *storage.Scenario) *MockProcessor {
	b.Helper()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(scenario); err != nil {
		b.Fatalf("Add() error = %v", err)
	}
	return NewMockProcessor(registry, nil)
}

func processSingleScenarioUnlimitedB(b *testing.B, scenario *storage.Scenario) *MockProcessor {
	b.Helper()
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(scenario); err != nil {
		b.Fatalf("Add() error = %v", err)
	}
	return NewMockProcessorWithMaxBodySize(registry, nil, 0)
}

func assertNoBodyReadBeforeWrite(t *testing.T, reader *fixedSizeCountingReader) {
	t.Helper()
	if reader.read != 0 {
		t.Fatalf("read %d bytes before WriteTo, want 0", reader.read)
	}
}

func assertFourBodyBytesReadDuringProcess(t *testing.T, reader *fixedSizeCountingReader) {
	t.Helper()
	if reader.read != 4 {
		t.Fatalf("read %d bytes during Process, want 4", reader.read)
	}
}

func assertByteBodyReadDuringProcess(t *testing.T, reader *processorByteCountingReader, want int64) {
	t.Helper()
	if reader.read != want {
		t.Fatalf("read %d bytes during Process, want %d", reader.read, want)
	}
}

func assertBodyReaderCleared(t *testing.T, message *icap.HTTPMessage) {
	t.Helper()
	if message.BodyReader != nil {
		t.Fatal("BodyReader was not cleared after Process")
	}
}

func inlineBodyStreamScenario() *storage.Scenario {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{From: "body", Body: "ok"}
	return scenario
}

func rawRequestHTTPBodyStreamScenario() *storage.Scenario {
	scenario := rawHTTPBodyStreamScenario(icap.MethodREQMOD, "request_http_body")
	scenario.Response.HTTPStatus = 403
	return scenario
}

func rawResponseHTTPBodyStreamScenario() *storage.Scenario {
	return rawHTTPBodyStreamScenario(icap.MethodRESPMOD, "response_http_body")
}

func rawHTTPBodyStreamScenario(method, source string) *storage.Scenario {
	return &storage.Scenario{
		Name: "raw-http-body-stream", Match: storage.MatchRule{Methods: []string{method}}, Priority: 100,
		Response: storage.ResponseTemplate{ICAPStatus: 200, Stream: &storage.StreamConfig{
			Source: storage.StreamSourceConfig{From: source},
			Chunks: storage.StreamChunksConfig{Size: storage.SizeSpec{Min: 2, Max: 2, IsSet: true}},
			Finish: storage.StreamFinishConfig{Mode: icap.StreamFinishComplete},
		}},
	}
}

func newPercentFINStreamScenario(body string, percent int) *storage.Scenario {
	scenario := responseBodyStreamScenario(icap.StreamFinishComplete)
	scenario.Response.Stream = newPercentFINStream("body", body, percent)
	return scenario
}

func newRawPercentFINStreamScenario() *storage.Scenario {
	scenario := rawHTTPBodyStreamScenario(icap.MethodRESPMOD, "response_http_body")
	scenario.Response.Stream = newPercentFINStream("response_http_body", "", 40)
	return scenario
}

func newCompleteDurationThrottleScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:  "complete-duration-throttle",
		Match: storage.MatchRule{Methods: []string{icap.MethodRESPMOD}},
		Response: storage.ResponseTemplate{ICAPStatus: 200, Stream: &storage.StreamConfig{
			Source:   storage.StreamSourceConfig{From: "body", Body: "abcd"},
			Send:     storage.StreamSendConfig{Duration: storage.DurationSpec{Min: 5 * time.Millisecond, Max: 5 * time.Millisecond, IsSet: true}},
			Throttle: storage.StreamThrottleConfig{Every: storage.DurationSpec{Min: 3 * time.Millisecond, Max: 3 * time.Millisecond, IsSet: true}},
		}},
		Priority: 100,
	}
}

func newPercentFINStream(source, body string, percent int) *storage.StreamConfig {
	return &storage.StreamConfig{
		Source: storage.StreamSourceConfig{From: source, Body: body},
		Send: storage.StreamSendConfig{
			Percent:  storage.PercentSpec{Min: percent, Max: percent, IsSet: true},
			Duration: storage.DurationSpec{Min: time.Millisecond, Max: time.Millisecond, IsSet: true},
		},
		Throttle: storage.StreamThrottleConfig{ChunkSize: storage.SizeSpec{Min: 3, Max: 3, IsSet: true}},
		End:      storage.StreamEndConfig{Mode: icap.StreamFinishFIN},
	}
}

func benchmarkStreamChunkSize() storage.SizeSpec {
	return storage.SizeSpec{Min: 32 << 10, Max: 32 << 10, IsSet: true}
}

func benchmarkRawHTTPBodyStreamScenario() *storage.Scenario {
	scenario := rawResponseHTTPBodyStreamScenario()
	scenario.Response.Stream.Chunks.Size = benchmarkStreamChunkSize()
	return scenario
}

func benchmarkBodyFileStreamScenario(path string) *storage.Scenario {
	scenario := bodyFileStreamScenario(path, icap.StreamFinishComplete)
	scenario.Response.Stream.Chunks.Size = benchmarkStreamChunkSize()
	return scenario
}

func benchmarkPartsStreamScenario(path string) *storage.Scenario {
	scenario := partsStreamScenario([]storage.StreamPartConfig{
		{Body: "prefix-"},
		{BodyFile: path},
		{Body: "-middle-"},
		{From: "response_http_body"},
		{Body: "-suffix"},
	}, icap.StreamFinishComplete)
	scenario.Response.Stream.Chunks.Size = benchmarkStreamChunkSize()
	return scenario
}

func benchmarkMultipartSelectorScenario(cfg storage.StreamMultipartConfig) *storage.Scenario {
	scenario := multipartSelectorStreamScenario(cfg)
	scenario.Response.Stream.Chunks.Size = benchmarkStreamChunkSize()
	return scenario
}

func rawHTTPBodyPartsScenario(method string, parts []storage.StreamPartConfig) *storage.Scenario {
	scenario := rawHTTPBodyStreamScenario(method, defaultRawHTTPBodySource(method))
	scenario.Response.Stream.Source = storage.StreamSourceConfig{}
	scenario.Response.Stream.Parts = parts
	scenario.Response.Stream.Chunks.Size = storage.SizeSpec{Min: 1, Max: 1, IsSet: true}
	return scenario
}

func defaultRawHTTPBodySource(method string) string {
	if method == icap.MethodREQMOD {
		return streamSourceRequestHTTPBody
	}
	return streamSourceResponseHTTPBody
}

func bodyFileStreamScenario(path, mode string) *storage.Scenario {
	scenario := responseBodyStreamScenario(mode)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{From: "body_file", BodyFile: path}
	scenario.Response.Stream.Chunks.Size = storage.SizeSpec{Min: 2, Max: 2, IsSet: true}
	if mode == icap.StreamFinishFIN {
		scenario.Response.Stream.Finish.Fin.After.Bytes = storage.SizeSpec{Min: 3, Max: 3, IsSet: true}
	}
	return scenario
}

func partsStreamScenario(parts []storage.StreamPartConfig, mode string) *storage.Scenario {
	scenario := responseBodyStreamScenario(mode)
	scenario.Response.Stream.Source = storage.StreamSourceConfig{}
	scenario.Response.Stream.Parts = parts
	scenario.Response.Stream.Chunks.Size = storage.SizeSpec{Min: 1, Max: 1, IsSet: true}
	if mode == icap.StreamFinishFIN {
		scenario.Response.Stream.Finish.Fin.After.Bytes = storage.SizeSpec{Min: 3, Max: 3, IsSet: true}
	}
	return scenario
}

func assertStreamOutput(t *testing.T, resp *icap.Response, wantChunked string) {
	t.Helper()
	var out bytes.Buffer
	if _, err := resp.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !strings.Contains(out.String(), wantChunked) {
		t.Fatalf("streamed body missing: %q, want chunked %q", out.String(), wantChunked)
	}
}

type processorStreamFileStub struct {
	path  string
	opens int
}

func stubProcessorStreamFile(
	t *testing.T,
	path string,
	size int64,
	reader io.ReadCloser,
) *processorStreamFileStub {
	t.Helper()
	state := &processorStreamFileStub{path: path}
	oldOpen, oldStat := openStreamFile, statStreamFile
	openStreamFile = func(_ string) (io.ReadCloser, error) {
		state.opens++
		return reader, nil
	}
	statStreamFile = func(string) (os.FileInfo, error) { return streamFileInfo{size: size}, nil }
	t.Cleanup(func() { openStreamFile, statStreamFile = oldOpen, oldStat })
	return state
}

type processorTrackingReadCloser struct {
	*strings.Reader
	read   int
	closed bool
}

type benchmarkCountingReadCloser struct {
	io.ReadCloser
	read int64
}

func newProcessorTrackingReadCloser(body string) *processorTrackingReadCloser {
	return &processorTrackingReadCloser{Reader: strings.NewReader(body)}
}

func (r *processorTrackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func (r *processorTrackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func (r *benchmarkCountingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += int64(n)
	return n, err
}

type streamFileInfo struct{ size int64 }

func (i streamFileInfo) Name() string       { return "body.bin" }
func (i streamFileInfo) Size() int64        { return i.size }
func (i streamFileInfo) Mode() os.FileMode  { return 0o644 }
func (i streamFileInfo) ModTime() time.Time { return time.Time{} }
func (i streamFileInfo) IsDir() bool        { return false }
func (i streamFileInfo) Sys() any           { return nil }

func assertNoMatchMode(t *testing.T, got []byte, err error, want string, wantErr bool) {
	t.Helper()
	if wantErr && err == nil {
		t.Fatal("resolveStreamSource() error = nil, want error")
	}
	if !wantErr && err != nil {
		t.Fatalf("resolveStreamSource() error = %v", err)
	}
	if !wantErr && string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func assertMultipartStreamingNoMatch(t *testing.T, resp *icap.Response, err error, want string, wantErr bool) {
	t.Helper()
	if wantErr && !errors.Is(err, errNoMultipartPartsMatched) {
		t.Fatalf("Process() error = %v, want no-match error", err)
	}
	if !wantErr && err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if wantErr {
		return
	}
	var out bytes.Buffer
	if _, err := resp.WriteTo(&out); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if want != "" && !strings.Contains(out.String(), want) {
		t.Fatalf("streamed body = %q, want %q", out.String(), want)
	}
}

func multipartTestBody(t *testing.T) (bodyBytes []byte, contentType string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeMultipartField(t, writer, "comment", "hello")
	writeMultipartFile(t, writer, "upload", "payload.exe", "EXE")
	writeMultipartFile(t, writer, "upload", "notes.txt", "TXT")
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func multipartBodyWithLargeSelectedFile(t *testing.T) (bodyBytes []byte, contentType string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeMultipartFile(t, writer, "upload", "large.bin", "0123456789")
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func writeMultipartField(t *testing.T, writer *multipart.Writer, name, value string) {
	t.Helper()
	if err := writer.WriteField(name, value); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
}

func writeMultipartFile(t *testing.T, writer *multipart.Writer, field, name, value string) {
	t.Helper()
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(value)); err != nil {
		t.Fatalf("part.Write() error = %v", err)
	}
}

func httpMessageWithBody(contentType string, body []byte) *icap.HTTPMessage {
	msg := &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	msg.Header.Set("Content-Type", contentType)
	msg.SetLoadedBody(body)
	return msg
}

func httpMessageWithReader(contentType string, body *bytes.Reader) *icap.HTTPMessage {
	msg := &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	msg.Header.Set("Content-Type", contentType)
	msg.BodyReader = body
	return msg
}

func writeProcessorTempFile(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/body.bin"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func writeBenchmarkSizedFile(b *testing.B, name string, size int64) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), name)
	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("Create() error = %v", err)
	}
	if err := writeRepeatedBenchmarkBytes(file, size, 'x'); err != nil {
		_ = file.Close()
		b.Fatalf("writeRepeatedBenchmarkBytes() error = %v", err)
	}
	if err := file.Close(); err != nil {
		b.Fatalf("Close() error = %v", err)
	}
	return path
}

func writeBenchmarkMultipartFile(b *testing.B, fillerSize, selectedFileSize int64) (path, contentType string, size int64) {
	b.Helper()
	path = filepath.Join(b.TempDir(), "multipart-body.bin")
	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("Create() error = %v", err)
	}
	writer := multipart.NewWriter(file)
	contentType = writer.FormDataContentType()
	writeMultipartFieldB(b, writer, "comment", "ignored")
	writeMultipartLargeFileB(b, writer, "upload", "skip.txt", fillerSize)
	writeMultipartLargeFileB(b, writer, "upload", "selected.exe", selectedFileSize)
	if err := writer.Close(); err != nil {
		_ = file.Close()
		b.Fatalf("writer.Close() error = %v", err)
	}
	if err := file.Close(); err != nil {
		b.Fatalf("Close() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("Stat() error = %v", err)
	}
	size = info.Size()
	return path, contentType, size
}

func writeMultipartFieldB(b *testing.B, writer *multipart.Writer, name, value string) {
	b.Helper()
	if err := writer.WriteField(name, value); err != nil {
		b.Fatalf("WriteField() error = %v", err)
	}
}

func writeMultipartLargeFileB(b *testing.B, writer *multipart.Writer, field, name string, size int64) {
	b.Helper()
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		b.Fatalf("CreateFormFile() error = %v", err)
	}
	if err := writeRepeatedBenchmarkBytes(part, size, 'm'); err != nil {
		b.Fatalf("writeRepeatedBenchmarkBytes() error = %v", err)
	}
}

func writeRepeatedBenchmarkBytes(w io.Writer, size int64, fill byte) error {
	block := bytes.Repeat([]byte{fill}, 32<<10)
	for remaining := size; remaining > 0; {
		n := min(int64(len(block)), remaining)
		if _, err := w.Write(block[:n]); err != nil {
			return err
		}
		remaining -= n
	}
	return nil
}
