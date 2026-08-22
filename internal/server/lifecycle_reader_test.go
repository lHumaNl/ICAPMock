// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestPreviewContinuationCancellationInterruptsBlockedContinueWrite(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	conn := newConnection(serverConn, &ConnectionConfig{})
	defer conn.Abort()
	chunked := icap.NewChunkedReader(strings.NewReader("0\r\n\r\n0\r\n\r\n"))
	chunked.EnablePreview()
	if _, err := io.ReadAll(chunked); err != nil {
		t.Fatalf("reading preview boundary: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := &previewContinuationReader{
		server: &ICAPServer{config: &config.ServerConfig{}},
		conn:   conn,
		reader: chunked,
		ctx:    ctx,
	}
	done := make(chan error, 1)
	go func() {
		var one [1]byte
		_, err := reader.Read(one[:])
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("preview continuation read error = nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("preview continuation write remained blocked after cancellation")
	}
}

func TestRequestDeadlineReaderProvidesByteReaderWithoutReadAhead(t *testing.T) {
	activations := 0
	reader := newRequestDeadlineReader(bufio.NewReader(strings.NewReader("x")), func() error {
		activations++
		return nil
	})
	byteReader, ok := any(reader).(io.ByteReader)
	if !ok {
		t.Fatal("requestDeadlineReader does not implement io.ByteReader")
	}
	got, err := byteReader.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte() error = %v", err)
	}
	if got != 'x' {
		t.Fatalf("ReadByte() = %q, want x", got)
	}
	if activations != 1 {
		t.Fatalf("deadline activations = %d, want 1", activations)
	}
	if !reader.Started() {
		t.Fatal("Started() = false after ReadByte")
	}
}

func TestPooledBufferReadBoundedLinePreservesFollowingBytes(t *testing.T) {
	source := bytes.NewBufferString("1\r\na\r\n0\r\n\r\nNEXT")
	reader := &pooledBuffer{rw: source, buf: make([]byte, 32)}

	line, err := reader.ReadBoundedLine(16)
	if err != nil {
		t.Fatalf("ReadBoundedLine() error = %v", err)
	}
	if line != "1\r\n" {
		t.Fatalf("ReadBoundedLine() = %q, want %q", line, "1\\r\\n")
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got, want := string(rest), "a\r\n0\r\n\r\nNEXT"; got != want {
		t.Fatalf("remaining bytes = %q, want %q", got, want)
	}
}

func TestPooledBufferReadBoundedLineEnforcesLimit(t *testing.T) {
	source := bytes.NewBufferString("abcdef\n")
	reader := &pooledBuffer{rw: source, buf: make([]byte, 16)}

	line, err := reader.ReadBoundedLine(4)
	if !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("ReadBoundedLine() error = %v, want io.ErrShortBuffer", err)
	}
	if line != "abcd" {
		t.Fatalf("ReadBoundedLine() = %q, want abcd", line)
	}
}

func TestPooledBufferReadBoundedLineAcceptsNewlineAtLimit(t *testing.T) {
	source := bytes.NewBufferString("abc\nNEXT")
	reader := &pooledBuffer{rw: source, buf: make([]byte, 16)}

	line, err := reader.ReadBoundedLine(4)
	if err != nil {
		t.Fatalf("ReadBoundedLine() error = %v", err)
	}
	if line != "abc\n" {
		t.Fatalf("ReadBoundedLine() = %q, want %q", line, "abc\\n")
	}
}

func TestPooledBufferDelaysSavedErrorUntilBufferedDataIsConsumed(t *testing.T) {
	customErr := errors.New("read complete")
	for _, readErr := range []error{io.EOF, customErr} {
		t.Run(readErr.Error(), func(t *testing.T) {
			source := &dataErrorReadWriter{data: []byte("1\r\na\r\n"), err: readErr}
			reader := &pooledBuffer{rw: source, buf: make([]byte, 16)}

			line, err := reader.ReadBoundedLine(16)
			if err != nil {
				t.Fatalf("ReadBoundedLine() error = %v", err)
			}
			if line != "1\r\n" {
				t.Fatalf("ReadBoundedLine() = %q, want %q", line, "1\\r\\n")
			}
			body := make([]byte, 3)
			n, err := io.ReadFull(reader, body)
			if err != nil {
				t.Fatalf("ReadFull() error = %v", err)
			}
			if n != len(body) || string(body) != "a\r\n" {
				t.Fatalf("buffered body = %q (%d bytes), want %q", body, n, "a\\r\\n")
			}
			one := make([]byte, 1)
			if _, err := reader.Read(one); !errors.Is(err, readErr) {
				t.Fatalf("final Read() error = %v, want %v", err, readErr)
			}
		})
	}
}

func TestOptimizedBoundedLineActivatesDeadlineAndActivityOnRefill(t *testing.T) {
	source := bytes.NewBufferString("1\r\n2\r\n")
	pooled := &pooledBuffer{rw: source, buf: make([]byte, 16)}
	activityUpdates := 0
	active := activityReader{reader: pooled, touch: func() { activityUpdates++ }}
	deadlineActivations := 0
	reader := newRequestDeadlineReader(active, func() error {
		deadlineActivations++
		return nil
	})

	for _, want := range []string{"1\r\n", "2\r\n"} {
		line, err := reader.ReadBoundedLine(16)
		if err != nil {
			t.Fatalf("ReadBoundedLine() error = %v", err)
		}
		if line != want {
			t.Fatalf("ReadBoundedLine() = %q, want %q", line, want)
		}
	}
	if activityUpdates != 1 {
		t.Fatalf("activity updates = %d, want one underlying refill", activityUpdates)
	}
	if deadlineActivations != 1 {
		t.Fatalf("deadline activations = %d, want 1", deadlineActivations)
	}
}

func TestOptimizedBoundedLinePreservesReadAndDeadlineErrors(t *testing.T) {
	readErr := errors.New("line read failed")
	deadlineErr := errors.New("deadline activation failed")
	reader := newRequestDeadlineReader(&stubBufferedReader{data: "partial", err: readErr}, func() error {
		return deadlineErr
	})

	line, err := reader.ReadBoundedLine(16)
	if line != "partial" {
		t.Fatalf("ReadBoundedLine() = %q, want partial", line)
	}
	if !errors.Is(err, readErr) || !errors.Is(err, deadlineErr) {
		t.Fatalf("ReadBoundedLine() error = %v, want read and deadline errors", err)
	}
}

func TestOptimizedChunkedReaderLineLimits(t *testing.T) {
	t.Run("chunk header exactly at limit", func(t *testing.T) {
		header := "1;" + strings.Repeat("a", icap.MaxChunkHeaderLength-4) + "\r\n"
		body, err := io.ReadAll(newOptimizedChunkedReader(header + "x\r\n0\r\n\r\n"))
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(body) != "x" {
			t.Fatalf("ReadAll() = %q, want x", body)
		}
	})

	t.Run("chunk header over limit", func(t *testing.T) {
		header := "1;" + strings.Repeat("a", icap.MaxChunkHeaderLength-3) + "\r\n"
		_, err := io.ReadAll(newOptimizedChunkedReader(header + "x\r\n0\r\n\r\n"))
		if !errors.Is(err, icap.ErrChunkHeaderTooLong) {
			t.Fatalf("ReadAll() error = %v, want ErrChunkHeaderTooLong", err)
		}
	})

	t.Run("trailer exactly at limit", func(t *testing.T) {
		trailer := "X:" + strings.Repeat("a", icap.MaxTrailerLineLength-4) + "\r\n"
		if _, err := io.ReadAll(newOptimizedChunkedReader("0\r\n" + trailer + "\r\n")); err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
	})

	t.Run("trailer over limit", func(t *testing.T) {
		trailer := "X:" + strings.Repeat("a", icap.MaxTrailerLineLength-3) + "\r\n"
		_, err := io.ReadAll(newOptimizedChunkedReader("0\r\n" + trailer + "\r\n"))
		if !errors.Is(err, icap.ErrTrailerLineTooLong) {
			t.Fatalf("ReadAll() error = %v, want ErrTrailerLineTooLong", err)
		}
	})
}

type dataErrorReadWriter struct {
	data []byte
	err  error
	done bool
}

func (r *dataErrorReadWriter) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), r.err
}

func (r *dataErrorReadWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func newOptimizedChunkedReader(input string) *icap.ChunkedReader {
	source := bytes.NewBufferString(input)
	pooled := &pooledBuffer{rw: source, buf: make([]byte, 8*1024)}
	active := activityReader{reader: pooled, touch: func() {}}
	return icap.NewChunkedReader(newRequestDeadlineReader(active, nil))
}
