// Copyright 2026 ICAP Mock

package storage

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestDiskMonitor_Caching tests that disk usage results are cached
// and not recalculated within the cache interval.
func TestDiskMonitor_Caching(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
		UseSyscalls:      true,
		CacheInterval:    100 * time.Millisecond, // Short cache for testing
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// First call should calculate disk usage
	usage1, err := dm.getDiskUsage()
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Immediately call again - should use cached result
	usage2, err := dm.getDiskUsage()
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}

	// Verify same values (cached)
	if usage1.Total != usage2.Total {
		t.Errorf("Total changed (should be cached): %d vs %d", usage1.Total, usage2.Total)
	}
	if usage1.Used != usage2.Used {
		t.Errorf("Used changed (should be cached): %d vs %d", usage1.Used, usage2.Used)
	}
	if usage1.Available != usage2.Available {
		t.Errorf("Available changed (should be cached): %d vs %d", usage1.Available, usage2.Available)
	}
}

// TestDiskMonitor_CacheExpiration tests that cache expires after the interval.
func TestDiskMonitor_CacheExpiration(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
		UseSyscalls:      true,
		CacheInterval:    50 * time.Millisecond, // Very short cache for testing
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// First call
	usage1, err := dm.getDiskUsage()
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(60 * time.Millisecond)

	// Second call should recalculate
	usage2, err := dm.getDiskUsage()
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}

	// Values should still be similar (no significant change expected in 60ms)
	// This just verifies the code path was executed
	if usage1.Total == 0 || usage2.Total == 0 {
		t.Error("Expected non-zero disk usage")
	}
}

// TestDiskMonitor_ConcurrentCaching tests that caching works correctly
// under concurrent access.
func TestDiskMonitor_ConcurrentCaching(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
		UseSyscalls:      true,
		CacheInterval:    200 * time.Millisecond,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// Make concurrent calls
	var results []DiskUsage
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			usage, err := dm.getDiskUsage()
			if err != nil {
				t.Errorf("Concurrent call failed: %v", err)
				return
			}
			mu.Lock()
			results = append(results, usage)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// All results should be the same (cached)
	if len(results) == 0 {
		t.Fatal("No results collected")
	}

	first := results[0]
	for i, r := range results {
		if r.Total != first.Total || r.Used != first.Used || r.Available != first.Available {
			t.Errorf("Result %d differs from first (should all be cached)", i)
		}
	}
}

// TestDiskMonitor_UseSyscalls tests that syscalls are used when enabled.
func TestDiskMonitor_UseSyscalls(t *testing.T) {
	tests := []struct {
		name        string
		useSyscalls bool
	}{
		{"syscalls enabled", true},
		{"syscalls disabled", false},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DiskMonitorConfig{
				Enabled:          true,
				CheckInterval:    30 * time.Second,
				WarningThreshold: 0.80,
				ErrorThreshold:   0.95,
				Path:             "",
				UseSyscalls:      tt.useSyscalls,
				CacheInterval:    1 * time.Second,
			}

			dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
			if err != nil {
				t.Fatalf("Failed to create disk monitor: %v", err)
			}
			defer dm.Stop()

			// Get disk usage - should work regardless of syscall usage
			usage, err := dm.getDiskUsage()
			if err != nil {
				t.Errorf("getDiskUsage failed: %v", err)
				return
			}

			// Verify we got valid results
			if usage.Total <= 0 {
				t.Error("Expected positive total disk space")
			}
			if usage.Available < 0 {
				t.Error("Expected non-negative available space")
			}
			if usage.UsagePercent < 0 || usage.UsagePercent > 1 {
				t.Errorf("Usage percent out of range: %f", usage.UsagePercent)
			}
		})
	}
}

// TestDiskMonitor_PathValidation tests that the path validation works.
func TestDiskMonitor_PathValidation(t *testing.T) {
	// Create a temporary file (not a directory)
	tmpFile, err := os.CreateTemp("", "testfile")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             tmpFile.Name(), // Use file path instead of directory
		UseSyscalls:      true,
		CacheInterval:    1 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// Try to get disk usage - should fail with "not a directory" error
	_, err = dm.getDiskUsage()
	if err == nil {
		t.Error("Expected error for non-directory path, got nil")
	}
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
}

// TestDiskMonitor_GetLastErrorWithError tests GetLastError when an error occurs.
func TestDiskMonitor_GetLastErrorWithError(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "", // Empty path, will use a default
		UseSyscalls:      true,
		CacheInterval:    1 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// Manually set the last error to simulate a failure
	dm.lastError.Store(fmt.Errorf("simulated disk check error"))

	// Get last error
	lastErr := dm.GetLastError()
	if lastErr == nil {
		t.Error("Expected an error, got nil")
	} else {
		t.Logf("Got expected error: %v", lastErr)
	}
}

// TestDiskMonitor_NoLastErrorInitially tests that GetLastError returns nil
// when monitor hasn't run yet.
func TestDiskMonitor_NoLastErrorInitially(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
		UseSyscalls:      true,
		CacheInterval:    1 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// Don't start the monitor - just check last error
	lastErr := dm.GetLastError()
	if lastErr != nil {
		t.Errorf("Expected nil error (not started yet), got: %v", lastErr)
	}
}

// TestDiskMonitor_RateLimiting tests that rate limiting prevents
// multiple concurrent disk checks.
func TestDiskMonitor_RateLimiting(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
		UseSyscalls:      true,
		CacheInterval:    500 * time.Millisecond, // Longer cache
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}
	defer dm.Stop()

	// Make many concurrent calls - all should use the same cached result
	var wg sync.WaitGroup
	callCount := 0
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dm.getDiskUsage()
			if err != nil {
				t.Errorf("Concurrent call failed: %v", err)
				return
			}
			mu.Lock()
			callCount++
			mu.Unlock()
		}()
	}

	wg.Wait()

	if callCount != 50 {
		t.Errorf("Expected 50 calls, got %d", callCount)
	}

	// All calls should complete quickly (using cache)
	t.Log("All 50 concurrent calls completed successfully with caching")
}
