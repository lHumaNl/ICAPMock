// Copyright 2026 ICAP Mock

package icap

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

type readerOnly struct {
	io.Reader
}

func BenchmarkChunkedReaderReaderOnly(b *testing.B) {
	for _, chunkSize := range []int{64 * 1024, 64} {
		input := benchmarkChunkedBody(64*1024, chunkSize)
		b.Run(fmt.Sprintf("chunk-%d", chunkSize), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(64 * 1024)
			for b.Loop() {
				reader := NewChunkedReader(readerOnly{Reader: bytes.NewReader(input)})
				if _, err := io.Copy(io.Discard, reader); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkChunkedBody(total, chunkSize int) []byte {
	var encoded bytes.Buffer
	payload := bytes.Repeat([]byte{'x'}, chunkSize)
	for remaining := total; remaining > 0; {
		size := min(chunkSize, remaining)
		fmt.Fprintf(&encoded, "%x\r\n", size)
		encoded.Write(payload[:size])
		encoded.WriteString("\r\n")
		remaining -= size
	}
	encoded.WriteString("0\r\n\r\n")
	return encoded.Bytes()
}
