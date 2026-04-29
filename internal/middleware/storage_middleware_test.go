// Copyright 2026 ICAP Mock

package middleware

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestStorageMiddleware_WrapWithBodyLimitBoundsSnapshotRead(t *testing.T) {
	const limit int64 = 8
	store := newCapturingStorage()
	mw := newTestStorageMiddleware(store)
	t.Cleanup(func() { shutdownStorageMiddleware(t, mw) })
	reader := &middlewareCountingReader{remaining: limit + 64}
	req := middlewareRequestWithBody(reader)

	_, err := mw.WrapWithBodyLimit(okHandler(), limit).Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	saved := store.waitForRequest(t)

	if reader.read > limit+1 {
		t.Fatalf("read %d bytes, want at most %d", reader.read, limit+1)
	}
	if !saved.HTTPRequest.BodyTruncated {
		t.Fatal("BodyTruncated = false, want true")
	}
}

func newTestStorageMiddleware(store storage.Storage) *StorageMiddleware {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := StorageMiddlewareConfig{Workers: 1, QueueSize: 1, CircuitBreaker: CircuitBreakerConfig{Enabled: false}}
	return StorageMiddlewareWithPool(store, logger, cfg)
}

func shutdownStorageMiddleware(t *testing.T, mw *StorageMiddleware) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mw.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func okHandler() handler.Handler {
	return handler.WrapHandler(handler.Func(func(context.Context, *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusOK), nil
	}), icap.MethodREQMOD)
}

func middlewareRequestWithBody(body io.Reader) *icap.Request {
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/reqmod")
	req.HTTPRequest = &icap.HTTPMessage{Method: "POST", URI: "/upload", Proto: "HTTP/1.1", Header: icap.NewHeader()}
	req.HTTPRequest.BodyReader = body
	return req
}

type middlewareCountingReader struct {
	remaining int64
	read      int64
}

func (r *middlewareCountingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	r.remaining -= n
	r.read += n
	return int(n), nil
}

type capturingStorage struct {
	saved chan *storage.StoredRequest
	mu    sync.Mutex
	items []*storage.StoredRequest
}

func newCapturingStorage() *capturingStorage {
	return &capturingStorage{saved: make(chan *storage.StoredRequest, 1)}
}

func (s *capturingStorage) SaveRequest(_ context.Context, req *storage.StoredRequest) error {
	s.mu.Lock()
	s.items = append(s.items, req)
	s.mu.Unlock()
	s.saved <- req
	return nil
}

func (s *capturingStorage) waitForRequest(t *testing.T) *storage.StoredRequest {
	t.Helper()
	select {
	case req := <-s.saved:
		return req
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for saved request")
	}
	return nil
}

func (s *capturingStorage) GetRequest(context.Context, string) (*storage.StoredRequest, error) {
	return nil, storage.ErrRequestNotFound
}

func (s *capturingStorage) ListRequests(context.Context, storage.RequestFilter) ([]*storage.StoredRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*storage.StoredRequest(nil), s.items...), nil
}

func (s *capturingStorage) DeleteRequest(context.Context, string) error { return nil }

func (s *capturingStorage) Close() error { return nil }

func (s *capturingStorage) Flush(context.Context) error { return nil }

func (s *capturingStorage) Clear(context.Context) (int64, error) { return 0, nil }

func (s *capturingStorage) DeleteRequests(context.Context, storage.RequestFilter) (int64, error) {
	return 0, nil
}
