// Copyright 2026 ICAP Mock

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	internalerrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestErrorDescriptionIncludesWrappedCause(t *testing.T) {
	inner := internalerrors.NewICAPError(2001, "response failed", 500, errors.New("disk full"))
	err := wrapDeadlineSetupError("response write", inner)

	description := errorDescription(err)

	if !strings.Contains(description, "setting response write deadline") ||
		!strings.Contains(description, "response failed") ||
		!strings.Contains(description, "disk full") {
		t.Fatalf("errorDescription() = %q, want message and root cause", description)
	}
}

func TestRequestDeadlineReaderPreservesReadAndDeadlineErrors(t *testing.T) {
	readErr := errors.New("read failed")
	deadlineErr := wrapDeadlineSetupError("active read", errors.New("deadline failed"))
	reader := newRequestDeadlineReader(&stubBufferedReader{data: "R", err: readErr}, func() error {
		return deadlineErr
	})

	_, err := reader.ReadString('\n')

	if !errors.Is(err, readErr) || !errors.Is(err, deadlineErr) {
		t.Fatalf("ReadString() error = %v, want both read and deadline failures", err)
	}
}

func TestLogConnectionErrorIncludesRequestContextAndDescription(t *testing.T) {
	var output bytes.Buffer
	srv := &ICAPServer{
		logger:            slog.New(slog.NewJSONHandler(&output, nil)),
		metricsServerName: "edge",
	}
	ctx := context.WithValue(context.Background(), requestIDKey, "request-1")
	req := &icap.Request{
		Method:     icap.MethodREQMOD,
		URI:        "icap://localhost/scan",
		ClientIP:   "192.0.2.10",
		RemoteAddr: "192.0.2.10:12345",
	}
	err := internalerrors.NewICAPError(2001, "response failed", 500, errors.New("broken pipe"))

	srv.logConnectionError(ctx, req, "write_response", "response_write_failed", "192.0.2.10:12345", err)

	entry := decodeServerLogEntry(t, output.Bytes())
	assertServerLogField(t, entry, "level", "ERROR")
	assertServerLogField(t, entry, "stage", "write_response")
	assertServerLogField(t, entry, "error_type", "response_write_failed")
	assertServerLogField(t, entry, "request_id", "request-1")
	assertServerLogField(t, entry, "method", icap.MethodREQMOD)
	assertServerLogField(t, entry, "remote_addr", "192.0.2.10:12345")
	if description, _ := entry["description"].(string); !strings.Contains(description, "broken pipe") {
		t.Fatalf("description = %v, want wrapped root cause", entry["description"])
	}
}

func TestHandleParseErrorLogsMalformedRequestButNotCleanEOF(t *testing.T) {
	var output bytes.Buffer
	srv := &ICAPServer{
		logger:            slog.New(slog.NewJSONHandler(&output, nil)),
		metricsServerName: "edge",
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	conn := newConnection(serverConn, &ConnectionConfig{})

	srv.handleParseError(context.Background(), conn, errors.New("invalid request line"), true, false)
	entry := decodeServerLogEntry(t, output.Bytes())
	assertServerLogField(t, entry, "level", "ERROR")
	assertServerLogField(t, entry, "stage", "parse_request")
	assertServerLogField(t, entry, "error_type", "malformed_request")

	output.Reset()
	srv.handleParseError(context.Background(), conn, io.EOF, false, false)
	if output.Len() == 0 {
		return
	}
	t.Fatalf("clean connection close unexpectedly logged: %s", output.String())
}

func decodeServerLogEntry(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; log = %q", err, data)
	}
	return entry
}

func assertServerLogField(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	if got, ok := entry[key].(string); !ok || got != want {
		t.Fatalf("%s = %v, want %q", key, entry[key], want)
	}
}

type stubBufferedReader struct {
	data string
	err  error
}

func (r *stubBufferedReader) Read(p []byte) (int, error) {
	return copy(p, r.data), r.err
}

func (r *stubBufferedReader) ReadString(_ byte) (string, error) {
	return r.data, r.err
}

func (r *stubBufferedReader) Buffered() int {
	return 0
}
