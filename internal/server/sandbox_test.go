// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestServerReceivesFullBodyBeforeNonStreamResponseAndDelay(t *testing.T) {
	delay := 120 * time.Millisecond
	var calls atomic.Int32
	srv := newSandboxServer(t, delayedNullBodyHandler(delay, &calls))
	defer srv.Stop(context.Background())

	conn, reader := dialSandboxServer(t, srv)
	defer conn.Close()

	writeREQMODPrefixAndChunk(t, conn, srv.Addr().String())
	requireNoEarlySandboxResponse(t, conn, reader)
	require.Equal(t, int32(0), calls.Load())

	start := time.Now()
	_, err := conn.Write([]byte("5\r\nworld\r\n0\r\n\r\n"))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	require.Contains(t, readICAPResponseStatus(t, reader), "200")
	require.GreaterOrEqual(t, time.Since(start), delay-20*time.Millisecond)
}

func TestServerReceivesFullBodyBeforeNoContentResponse(t *testing.T) {
	var calls atomic.Int32
	srv := newSandboxServer(t, noContentHandler(&calls))
	defer srv.Stop(context.Background())

	conn, reader := dialSandboxServer(t, srv)
	defer conn.Close()

	writeREQMODPrefixAndChunk(t, conn, srv.Addr().String())
	requireNoEarlySandboxResponse(t, conn, reader)
	require.Equal(t, int32(0), calls.Load())

	_, err := conn.Write([]byte("5\r\nworld\r\n0\r\n\r\n"))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	require.Contains(t, readICAPResponseStatus(t, reader), "204")
	require.Equal(t, int32(1), calls.Load())
}

func TestServerPreviewRespondsBeforeFullBody(t *testing.T) {
	var calls atomic.Int32
	srv := newSandboxServer(t, previewNoContentHandler(&calls))
	defer srv.Stop(context.Background())

	conn, reader := dialSandboxServer(t, srv)
	defer conn.Close()

	writePreviewREQMODPrefixAndChunk(t, conn, srv.Addr().String())
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	require.Contains(t, readICAPResponseStatus(t, reader), "204")
	require.Equal(t, int32(1), calls.Load())
}

func TestServerLoadedZeroMaxBodySizeIsUnlimited(t *testing.T) {
	cfg := loadSandboxServerConfig(t, "server:\n  max_body_size: 0\n")
	srv, err := NewServer(&cfg.Server, NewConnectionPool(), nil)
	require.NoError(t, err)
	require.Zero(t, srv.drainBodyLimit())
	req := &icap.Request{HTTPRequest: &icap.HTTPMessage{BodyReader: io.LimitReader(zeroReader{}, defaultDrainBodyLimit+1)}}
	require.NoError(t, receiveRequestBodies(req, srv.drainBodyLimit()))
	require.Len(t, req.HTTPRequest.Body, defaultDrainBodyLimit+1)
}

func newSandboxServer(
	t *testing.T,
	handler func(context.Context, *icap.Request) (*icap.Response, error),
) *ICAPServer {
	t.Helper()
	return newSandboxServerWithMaxBodySize(t, handler, 1024)
}

func newSandboxServerWithMaxBodySize(
	t *testing.T,
	handler func(context.Context, *icap.Request) (*icap.Response, error),
	maxBodySize int64,
) *ICAPServer {
	t.Helper()
	cfg := &config.ServerConfig{
		Host: "127.0.0.1", Port: 0, ReadTimeout: 2 * time.Second,
		WriteTimeout: time.Second, MaxConnections: 10, MaxBodySize: maxBodySize,
		Streaming: true,
	}
	srv, err := NewServer(cfg, NewConnectionPool(), nil)
	require.NoError(t, err)
	r := router.NewRouter()
	require.NoError(t, r.HandleFunc("/scan", handler))
	srv.SetRouter(r)
	require.NoError(t, srv.Start(context.Background()))
	return srv
}

func delayedNullBodyHandler(delay time.Duration, calls *atomic.Int32) func(context.Context, *icap.Request) (*icap.Response, error) {
	return func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		calls.Add(1)
		time.Sleep(delay)
		resp := icap.NewResponse(icap.StatusOK)
		resp.SetHeader("Encapsulated", "null-body=0")
		return resp, nil
	}
}

func noContentHandler(calls *atomic.Int32) func(context.Context, *icap.Request) (*icap.Response, error) {
	return func(_ context.Context, _ *icap.Request) (*icap.Response, error) {
		if calls != nil {
			calls.Add(1)
		}
		resp := icap.NewResponse(icap.StatusNoContentNeeded)
		resp.SetHeader("Encapsulated", "null-body=0")
		return resp, nil
	}
}

func previewNoContentHandler(calls *atomic.Int32) func(context.Context, *icap.Request) (*icap.Response, error) {
	return func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		preview, err := req.GetPreviewBody()
		if err != nil {
			return nil, err
		}
		if string(preview) != "hello" {
			return nil, fmt.Errorf("preview = %q, want hello", string(preview))
		}
		return noContentHandler(calls)(ctx, req)
	}
}

func dialSandboxServer(t *testing.T, srv *ICAPServer) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)
	return conn, bufio.NewReader(conn)
}

func writeREQMODPrefixAndChunk(t *testing.T, conn net.Conn, addr string) {
	t.Helper()
	_, err := conn.Write([]byte(reqmodChunkedPrefix(addr) + "5\r\nhello\r\n"))
	require.NoError(t, err)
}

func writePreviewREQMODPrefixAndChunk(t *testing.T, conn net.Conn, addr string) {
	t.Helper()
	_, err := conn.Write([]byte(reqmodPreviewChunkedPrefix(addr) + "5\r\nhello\r\n0\r\n\r\n"))
	require.NoError(t, err)
}

func reqmodChunkedPrefix(addr string) string {
	httpReq := "POST /upload HTTP/1.1\r\nHost: origin.example\r\nContent-Length: 10\r\n\r\n"
	encap := fmt.Sprintf("req-hdr=0, req-body=%d", len(httpReq))
	return fmt.Sprintf("REQMOD icap://%s/scan ICAP/1.0\r\nHost: localhost\r\nEncapsulated: %s\r\n\r\n%s", addr, encap, httpReq)
}

func reqmodPreviewChunkedPrefix(addr string) string {
	httpReq := "POST /upload HTTP/1.1\r\nHost: origin.example\r\nContent-Length: 10\r\n\r\n"
	encap := fmt.Sprintf("req-hdr=0, req-body=%d", len(httpReq))
	return fmt.Sprintf("REQMOD icap://%s/scan ICAP/1.0\r\nHost: localhost\r\nPreview: 5\r\nEncapsulated: %s\r\n\r\n%s", addr, encap, httpReq)
}

func loadSandboxServerConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := config.NewLoader().Load(config.LoadOptions{ConfigPath: path})
	require.NoError(t, err)
	return cfg
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func requireNoEarlySandboxResponse(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(80*time.Millisecond)))
	_, err := reader.Peek(1)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "timeout"), "unexpected read error: %v", err)
}
