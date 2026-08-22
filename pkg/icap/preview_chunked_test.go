// Copyright 2026 ICAP Mock

package icap_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestChunkedReaderPausesAndContinuesAtPreviewBoundary(t *testing.T) {
	input := "5\r\nhello\r\n0; vendor=value\r\n\r\n5\r\nworld\r\n0\r\n\r\n"
	reader := icap.NewChunkedReader(strings.NewReader(input))
	reader.EnablePreview()

	assertReadAll(t, reader, "hello")
	assertPreviewBoundary(t, reader, true, false)

	if err := reader.ContinueAfterPreview(); err != nil {
		t.Fatalf("ContinueAfterPreview() error = %v", err)
	}
	assertPreviewBoundary(t, reader, false, false)
	assertReadAll(t, reader, "world")
	assertPreviewBoundary(t, reader, false, false)
}

func TestChunkedReaderRecognizesCaseInsensitiveIEOFExtension(t *testing.T) {
	for _, extension := range []string{"ieof", "IEOF", "IeOf", "vendor=value; IEOF"} {
		t.Run(extension, func(t *testing.T) {
			reader := icap.NewChunkedReader(strings.NewReader("5\r\nhello\r\n0; " + extension + "\r\n\r\n"))
			reader.EnablePreview()

			assertReadAll(t, reader, "hello")
			assertPreviewBoundary(t, reader, true, true)
			if err := reader.ContinueAfterPreview(); !errors.Is(err, icap.ErrPreviewIEOF) {
				t.Fatalf("ContinueAfterPreview() error = %v, want %v", err, icap.ErrPreviewIEOF)
			}
		})
	}
}

func TestChunkedReaderAllowsPreviewContinuationExactlyOnce(t *testing.T) {
	reader := icap.NewChunkedReader(strings.NewReader("0\r\n\r\n0\r\n\r\n"))
	reader.EnablePreview()
	assertReadAll(t, reader, "")

	if err := reader.ContinueAfterPreview(); err != nil {
		t.Fatalf("first ContinueAfterPreview() error = %v", err)
	}
	if err := reader.ContinueAfterPreview(); !errors.Is(err, icap.ErrPreviewAlreadyContinued) {
		t.Fatalf("second ContinueAfterPreview() error = %v, want %v", err, icap.ErrPreviewAlreadyContinued)
	}
	assertReadAll(t, reader, "")
}

func TestChunkedReaderRejectsContinuationOutsidePreviewBoundary(t *testing.T) {
	reader := icap.NewChunkedReader(strings.NewReader("0\r\n\r\n"))
	if err := reader.ContinueAfterPreview(); !errors.Is(err, icap.ErrPreviewNotEnabled) {
		t.Fatalf("ContinueAfterPreview() error = %v, want %v", err, icap.ErrPreviewNotEnabled)
	}

	reader.EnablePreview()
	if err := reader.ContinueAfterPreview(); !errors.Is(err, icap.ErrPreviewNotAtBoundary) {
		t.Fatalf("ContinueAfterPreview() error = %v, want %v", err, icap.ErrPreviewNotAtBoundary)
	}
}

func assertReadAll(t *testing.T, reader io.Reader, want string) {
	t.Helper()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != want {
		t.Fatalf("ReadAll() = %q, want %q", got, want)
	}
}

func assertPreviewBoundary(t *testing.T, reader *icap.ChunkedReader, wantBoundary, wantIEOF bool) {
	t.Helper()
	boundary, ieof := reader.PreviewBoundary()
	if boundary != wantBoundary || ieof != wantIEOF {
		t.Fatalf("PreviewBoundary() = (%v, %v), want (%v, %v)", boundary, ieof, wantBoundary, wantIEOF)
	}
}
