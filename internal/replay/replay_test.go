// Copyright 2026 ICAP Mock

package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestReplayerStart tests that the replayer starts correctly.
func TestReplayerStart(t *testing.T) {
	// Create temp directory with test requests
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 3)

	// Create storage adapter
	store := NewFileStorageAdapter(tmpDir)

	// Create replayer
	cfg := &config.ReplayConfig{
		Enabled:     true,
		RequestsDir: tmpDir,
		Speed:       0, // No delay
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	// Track callback invocations
	var callbackMu sync.Mutex
	callbackCount := 0

	opts := ReplayOptions{
		Speed: 0, // No delay for fast test
		Callback: func(req *icap.Request, resp *icap.Response, err error) {
			callbackMu.Lock()
			callbackCount++
			callbackMu.Unlock()
		},
	}

	// Start replay
	ctx := context.Background()
	err = replayer.Start(ctx, opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify callback was called for all requests
	callbackMu.Lock()
	if callbackCount != 3 {
		t.Errorf("Expected 3 callback invocations, got %d", callbackCount)
	}
	callbackMu.Unlock()

	// Verify stats
	stats := replayer.Stats()
	if stats.TotalRequests != 3 {
		t.Errorf("Expected 3 total requests, got %d", stats.TotalRequests)
	}
}

// TestReplayerSpeed tests that speed control works correctly.
func TestReplayerSpeed(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 2)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := &config.ReplayConfig{Speed: 1.0}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	// Test with speed 0 (no delay)
	// Speed=0 means maximum speed (no timing delays between requests)
	start := time.Now()
	opts := ReplayOptions{Speed: 0}
	err = replayer.Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	elapsed := time.Since(start)

	// Should be very fast - allow 200ms for test overhead on slow systems
	if elapsed > 200*time.Millisecond {
		t.Errorf("Speed=0 replay took too long: %v (expected no delays)", elapsed)
	}
}

// TestReplayerFilter tests that filtering works correctly.
func TestReplayerFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create requests with different methods
	now := time.Now()
	requests := []*storage.StoredRequest{
		{
			ID:        "req-001",
			Timestamp: now,
			Method:    "REQMOD",
			URI:       "icap://localhost:1344/reqmod",
		},
		{
			ID:        "req-002",
			Timestamp: now.Add(time.Second),
			Method:    "RESPMOD",
			URI:       "icap://localhost:1344/respmod",
		},
		{
			ID:        "req-003",
			Timestamp: now.Add(2 * time.Second),
			Method:    "REQMOD",
			URI:       "icap://localhost:1344/reqmod",
		},
	}

	for _, req := range requests {
		data, _ := json.Marshal(req)
		path := filepath.Join(tmpDir, req.ID+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	// Filter for REQMOD only
	opts := ReplayOptions{
		Speed: 0,
		Filter: storage.RequestFilter{
			Method: "REQMOD",
		},
	}

	var replayedMethods []string
	var mu sync.Mutex
	opts.Callback = func(req *icap.Request, resp *icap.Response, err error) {
		mu.Lock()
		replayedMethods = append(replayedMethods, req.Method)
		mu.Unlock()
	}

	err = replayer.Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify only REQMOD was replayed
	mu.Lock()
	if len(replayedMethods) != 2 {
		t.Errorf("Expected 2 REQMOD requests, got %d", len(replayedMethods))
	}
	for _, m := range replayedMethods {
		if m != "REQMOD" {
			t.Errorf("Expected only REQMOD, got %s", m)
		}
	}
	mu.Unlock()
}

// TestReplayerCallback tests that the callback is called correctly.
func TestReplayerCallback(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 2)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	var callbacks []struct {
		req *icap.Request
		err error
	}
	var mu sync.Mutex

	opts := ReplayOptions{
		Speed: 0,
		Callback: func(req *icap.Request, resp *icap.Response, err error) {
			mu.Lock()
			callbacks = append(callbacks, struct {
				req *icap.Request
				err error
			}{req: req, err: err})
			mu.Unlock()
		},
	}

	err = replayer.Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mu.Lock()
	if len(callbacks) != 2 {
		t.Errorf("Expected 2 callbacks, got %d", len(callbacks))
	}
	mu.Unlock()
}

// TestReplayerLoop tests that loop mode works correctly.
func TestReplayerLoop(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 2)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	var callbackCount int
	var mu sync.Mutex

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	opts := ReplayOptions{
		Speed: 0,
		Loop:  true,
		Callback: func(req *icap.Request, resp *icap.Response, err error) {
			mu.Lock()
			callbackCount++
			// Stop after 5 iterations to avoid infinite loop
			if callbackCount >= 5 {
				cancel()
			}
			mu.Unlock()
		},
	}

	_ = replayer.Start(ctx, opts)

	mu.Lock()
	if callbackCount < 5 {
		t.Errorf("Expected at least 5 callbacks in loop mode, got %d", callbackCount)
	}
	mu.Unlock()
}

// TestLoadRequestFiles tests loading request files.
func TestLoadRequestFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test requests
	now := time.Now()
	requests := []*storage.StoredRequest{
		{
			ID:        "req-001",
			Timestamp: now,
			Method:    "REQMOD",
			URI:       "icap://localhost:1344/reqmod",
		},
		{
			ID:        "req-002",
			Timestamp: now.Add(time.Second),
			Method:    "RESPMOD",
			URI:       "icap://localhost:1344/respmod",
		},
	}

	for _, req := range requests {
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(tmpDir, req.ID+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Load without filter
	loaded, err := LoadRequestFiles(tmpDir, storage.RequestFilter{})
	if err != nil {
		t.Fatalf("LoadRequestFiles failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(loaded))
	}

	// Load with method filter
	loaded, err = LoadRequestFiles(tmpDir, storage.RequestFilter{Method: "REQMOD"})
	if err != nil {
		t.Fatalf("LoadRequestFiles with filter failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("Expected 1 REQMOD request, got %d", len(loaded))
	}
}

// TestParseFilterFromFlags tests parsing filter from CLI flags.
func TestParseFilterFromFlags(t *testing.T) {
	tests := []struct {
		checkFilter func(t *testing.T, f storage.RequestFilter)
		name        string
		from        string
		to          string
		method      string
		wantErr     bool
	}{
		{
			name:    "empty filter",
			from:    "",
			to:      "",
			method:  "",
			wantErr: false,
			checkFilter: func(t *testing.T, f storage.RequestFilter) {
				if !f.Start.IsZero() || !f.End.IsZero() || f.Method != "" {
					t.Error("Expected empty filter")
				}
			},
		},
		{
			name:    "date only",
			from:    "2024-01-01",
			to:      "2024-01-15",
			method:  "",
			wantErr: false,
			checkFilter: func(t *testing.T, f storage.RequestFilter) {
				expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				if !f.Start.Equal(expected) {
					t.Errorf("Start date mismatch: got %v, want %v", f.Start, expected)
				}
			},
		},
		{
			name:    "method filter",
			from:    "",
			to:      "",
			method:  "reqmod",
			wantErr: false,
			checkFilter: func(t *testing.T, f storage.RequestFilter) {
				if f.Method != "REQMOD" {
					t.Errorf("Method mismatch: got %v, want REQMOD", f.Method)
				}
			},
		},
		{
			name:    "invalid date",
			from:    "invalid-date",
			to:      "",
			method:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilterFromFlags(tt.from, tt.to, tt.method)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if tt.checkFilter != nil {
				tt.checkFilter(t, filter)
			}
		})
	}
}

// TestClient tests the ICAP client.
func TestClient(t *testing.T) {
	// Start a mock ICAP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	// Handle connections in goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleMockConnection(t, conn)
		}
	}()

	// Create client
	client := NewClient(5 * time.Second)

	// Create test request
	req, err := icap.NewRequest(icap.MethodOPTIONS, fmt.Sprintf("icap://%s/options", listener.Addr().String()))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Send request
	resp, err := client.Do(context.Background(), fmt.Sprintf("icap://%s/options", listener.Addr().String()), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestReplayerMetrics tests that metrics are recorded correctly.
func TestReplayerMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 2)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create metrics collector
	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, collector)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	opts := ReplayOptions{Speed: 0}
	err = replayer.Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := replayer.Stats()
	if stats.TotalRequests != 2 {
		t.Errorf("Expected 2 total requests, got %d", stats.TotalRequests)
	}
}

// TestReplayerStop tests that Stop works correctly.
func TestReplayerStop(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 10)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	// Start replay with some delay
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		opts := ReplayOptions{Speed: 1.0} // Normal speed to allow time to stop
		_ = replayer.Start(ctx, opts)
	}()

	// Wait a bit then stop
	time.Sleep(100 * time.Millisecond)
	replayer.Stop()

	wg.Wait()

	// Should have stopped before completing all requests
	stats := replayer.Stats()
	if stats.TotalRequests == 10 {
		t.Error("Expected replay to be stopped before completing all requests")
	}
}

// TestReplayerContextCancellation tests context cancellation.
func TestReplayerContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 10)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		opts := ReplayOptions{Speed: 1.0}
		err := replayer.Start(ctx, opts)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}
	}()

	// Cancel after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Wait()
}

// TestReplayerAlreadyRunning tests that Start returns error when already running.
func TestReplayerAlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 5)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		opts := ReplayOptions{Speed: 1.0}
		_ = replayer.Start(ctx, opts)
	}()

	// Wait for replay to start
	time.Sleep(50 * time.Millisecond)

	// Try to start again - should fail
	err = replayer.Start(ctx, ReplayOptions{})
	if err == nil {
		t.Error("Expected error when starting replay while already running")
	}

	replayer.Stop()
	wg.Wait()
}

// TestReplayerProgress tests the progress callback.
func TestReplayerProgress(t *testing.T) {
	tmpDir := t.TempDir()
	createTestRequests(t, tmpDir, 3)

	store := NewFileStorageAdapter(tmpDir)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := &config.ReplayConfig{}
	replayer, err := NewReplayer(cfg, store, logger, nil)
	if err != nil {
		t.Fatalf("NewReplayer failed: %v", err)
	}

	var progressReports []struct{ current, total int }
	var mu sync.Mutex

	opts := ReplayOptions{
		Speed: 0,
		OnProgress: func(current, total int) {
			mu.Lock()
			progressReports = append(progressReports, struct{ current, total int }{current, total})
			mu.Unlock()
		},
	}

	err = replayer.Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mu.Lock()
	if len(progressReports) != 3 {
		t.Errorf("Expected 3 progress reports, got %d", len(progressReports))
	}
	for i, report := range progressReports {
		if report.current != i+1 || report.total != 3 {
			t.Errorf("Progress report %d: expected (%d, 3), got (%d, %d)", i, i+1, report.current, report.total)
		}
	}
	mu.Unlock()
}

// Helper functions

func createTestRequests(t *testing.T, dir string, count int) {
	t.Helper()
	now := time.Now()

	for i := 0; i < count; i++ {
		req := &storage.StoredRequest{
			ID:        fmt.Sprintf("req-%03d", i+1),
			Timestamp: now.Add(time.Duration(i) * 100 * time.Millisecond),
			Method:    "REQMOD",
			URI:       "icap://localhost:1344/reqmod",
			Headers:   map[string][]string{},
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(dir, req.ID+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func handleMockConnection(t *testing.T, conn net.Conn) {
	defer conn.Close()

	// Read request
	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return
	}

	// Send mock response
	response := "ICAP/1.0 200 OK\r\n" +
		"ISTag: \"test\"\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	_, _ = conn.Write([]byte(response))
}
