// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/router"
)

func BenchmarkServerPersistentChunkedRESPMOD(b *testing.B) {
	for _, chunkSize := range []int{64 * 1024, 64} {
		b.Run(fmt.Sprintf("chunk-%d", chunkSize), func(b *testing.B) {
			srv := benchmarkChunkedServer(b)
			defer srv.Stop(context.Background())

			conn, err := net.Dial("tcp", srv.Addr().String())
			if err != nil {
				b.Fatal(err)
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			request := respmodChunkedRequestWithBody(
				srv.Addr().String(),
				"",
				string(benchmarkServerChunkedBody(64*1024, chunkSize)),
			)

			b.ReportAllocs()
			b.SetBytes(64 * 1024)
			b.ResetTimer()
			for b.Loop() {
				if _, err := conn.Write([]byte(request)); err != nil {
					b.Fatal(err)
				}
				if err := readBenchmarkICAPResponse(reader); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
		})
	}
}

func benchmarkChunkedServer(b *testing.B) *ICAPServer {
	b.Helper()
	cfg := &config.ServerConfig{
		Host: "127.0.0.1", Port: 0, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: 30 * time.Second, MaxConnections: 10, MaxBodySize: 1024 * 1024, Streaming: true,
	}
	srv, err := NewServer(cfg, NewConnectionPool(), nil)
	if err != nil {
		b.Fatal(err)
	}
	srv.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := router.NewRouter()
	if err := r.HandleFunc("/respmod", unreadBodyHandler(nil)); err != nil {
		b.Fatal(err)
	}
	srv.SetRouter(r)
	if err := srv.Start(context.Background()); err != nil {
		b.Fatal(err)
	}
	return srv
}

func benchmarkServerChunkedBody(total, chunkSize int) []byte {
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

func readBenchmarkICAPResponse(reader *bufio.Reader) error {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if line == "\r\n" {
			return nil
		}
	}
}
