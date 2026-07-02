// Copyright 2026 ICAP Mock

package icap_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestBodyStream_WriteTo_TermWritesPartialBodyAndFinalChunk(t *testing.T) {
	body := strings.Repeat("a", 40) + strings.Repeat("b", 60)
	stream := &icap.BodyStream{
		Payload:          icap.NewBytesStreamPayload([]byte(body)),
		ChunkSize:        64,
		FinishMode:       icap.StreamFinishTerm,
		FinAfterBytes:    40,
		FinAfterBytesSet: true,
	}
	var buf bytes.Buffer

	if _, err := stream.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	want := "28\r\n" + body[:40] + "\r\n0\r\n\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("stream output = %q, want %q", got, want)
	}
}
