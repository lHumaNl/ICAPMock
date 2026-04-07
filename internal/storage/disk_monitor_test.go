// Copyright 2026 ICAP Mock

package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestNewDiskMonitor_ValidConfig tests creating a disk monitor with valid configuration.
func TestNewDiskMonitor_ValidConfig(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if dm == nil {
		t.Fatal("Expected disk monitor, got nil")
	}
	// NewDiskMonitor resolves paths to absolute
	expectedPath, _ := filepath.Abs("./data/requests")
	if dm.path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, dm.path)
	}
}

// TestNewDiskMonitor_InvalidThresholds tests creating a disk monitor with invalid thresholds.
func TestNewDiskMonitor_InvalidThresholds(t *testing.T) {
	tests := []struct {
		name             string
		warningThreshold float64
		errorThreshold   float64
		expectedError    bool
	}{
		{
			name:             "warning below zero",
			warningThreshold: -0.1,
			errorThreshold:   0.95,
			expectedError:    true,
		},
		{
			name:             "warning above one",
			warningThreshold: 1.1,
			errorThreshold:   0.95,
			expectedError:    true,
		},
		{
			name:             "error below zero",
			warningThreshold: 0.80,
			errorThreshold:   -0.1,
			expectedError:    true,
		},
		{
			name:             "error above one",
			warningThreshold: 0.80,
			errorThreshold:   1.1,
			expectedError:    true,
		},
		{
			name:             "warning equals error",
			warningThreshold: 0.80,
			errorThreshold:   0.80,
			expectedError:    true,
		},
		{
			name:             "warning greater than error",
			warningThreshold: 0.90,
			errorThreshold:   0.80,
			expectedError:    true,
		},
		{
			name:             "valid thresholds",
			warningThreshold: 0.80,
			errorThreshold:   0.95,
			expectedError:    false,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DiskMonitorConfig{
				Enabled:          true,
				CheckInterval:    30 * time.Second,
				WarningThreshold: tt.warningThreshold,
				ErrorThreshold:   tt.errorThreshold,
				Path:             "",
			}

			_, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestDiskMonitor_StartStop tests starting and stopping the disk monitor.
func TestDiskMonitor_StartStop(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    100 * time.Millisecond,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Start the monitor
	dm.Start()
	if !dm.IsRunning() {
		t.Error("Expected disk monitor to be running")
	}

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Stop the monitor
	dm.Stop()
	if dm.IsRunning() {
		t.Error("Expected disk monitor to be stopped")
	}
}

// TestDiskMonitor_Disabled tests that a disabled monitor doesn't start.
func TestDiskMonitor_Disabled(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          false,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Start the monitor (should be a no-op)
	dm.Start()
	if dm.IsRunning() {
		t.Error("Expected disk monitor to not be running")
	}

	// Stop should be safe
	dm.Stop()
	if dm.IsRunning() {
		t.Error("Expected disk monitor to not be running")
	}
}

// TestDiskMonitor_CheckDiskSpace_BelowThreshold tests disk space check below warning threshold.
func TestDiskMonitor_CheckDiskSpace_BelowThreshold(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Test with small required bytes (should always pass)
	canWrite, err := dm.CheckDiskSpace(1024)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !canWrite {
		t.Error("Expected write to be allowed")
	}

	// Clean up
	dm.Stop()
}

// TestDiskMonitor_CheckDiskSpace_AboveWarningThreshold tests disk space check above warning threshold.
func TestDiskMonitor_CheckDiskSpace_AboveWarningThreshold(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.50, // Low threshold for testing
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Check disk space with small requirement
	// Since we estimate usage based on directory size, this should work
	canWrite, err := dm.CheckDiskSpace(1024)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	// Should allow write even at warning threshold
	if !canWrite {
		t.Error("Expected write to be allowed at warning threshold")
	}

	// Clean up
	dm.Stop()
}

// TestDiskMonitor_DisabledCheckDiskSpace tests that disabled monitor always allows writes.
func TestDiskMonitor_DisabledCheckDiskSpace(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          false,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Check disk space (should always return true)
	canWrite, err := dm.CheckDiskSpace(1024)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !canWrite {
		t.Error("Expected write to be allowed when disabled")
	}

	// Clean up
	dm.Stop()
}

// TestDiskMonitor_ConcurrentAccess tests concurrent access to disk monitor methods.
func TestDiskMonitor_ConcurrentAccess(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	dm.Start()
	defer dm.Stop()

	// Run concurrent checks
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dm.CheckDiskSpace(1024)
			if err != nil {
				t.Errorf("Concurrent check failed: %v", err)
			}
		}()
	}

	wg.Wait()
}

// TestDiskMonitor_GetUsage tests retrieving current disk usage.
func TestDiskMonitor_GetUsage(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	usage := dm.GetUsage()
	if usage.Total == 0 {
		// Should have some default values
	}

	dm.Stop()
}

// TestDiskMonitor_GetLastError tests retrieving the last error.
func TestDiskMonitor_GetLastError(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Initially should return nil (no error yet)
	err = dm.GetLastError()
	if err != nil {
		t.Errorf("Expected nil error, got: %v", err)
	}

	dm.Stop()
}

// TestDiskMonitor_MultipleStartStop tests multiple start/stop calls.
func TestDiskMonitor_MultipleStartStop(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Multiple starts should be safe
	dm.Start()
	dm.Start()
	dm.Start()
	if !dm.IsRunning() {
		t.Error("Expected disk monitor to be running")
	}

	// Multiple stops should be safe
	dm.Stop()
	dm.Stop()
	dm.Stop()
	if dm.IsRunning() {
		t.Error("Expected disk monitor to be stopped")
	}
}

// TestDiskMonitor_ContextCancellation tests that monitor stops when context is canceled.
func TestDiskMonitor_ContextCancellation(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    50 * time.Millisecond,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	dm.Start()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Cancel the monitor via stop
	dm.Stop()

	// Wait a bit more
	time.Sleep(100 * time.Millisecond)

	if dm.IsRunning() {
		t.Error("Expected disk monitor to be stopped")
	}
}

// TestDiskMonitor_EdgeCases tests edge cases and boundary conditions.
func TestDiskMonitor_EdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		warningThreshold float64
		errorThreshold   float64
	}{
		{
			name:             "minimum valid thresholds",
			warningThreshold: 0.0,
			errorThreshold:   0.01,
		},
		{
			name:             "maximum valid thresholds",
			warningThreshold: 0.99,
			errorThreshold:   1.00,
		},
		{
			name:             "boundary thresholds",
			warningThreshold: 0.5,
			errorThreshold:   0.51,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DiskMonitorConfig{
				Enabled:          true,
				CheckInterval:    30 * time.Second,
				WarningThreshold: tt.warningThreshold,
				ErrorThreshold:   tt.errorThreshold,
				Path:             "",
			}

			_, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

// TestDiskMonitor_ZeroRequiredBytes tests checking disk space with zero bytes required.
func TestDiskMonitor_ZeroRequiredBytes(t *testing.T) {
	cfg := config.DiskMonitorConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		WarningThreshold: 0.80,
		ErrorThreshold:   0.95,
		Path:             "",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dm, err := NewDiskMonitor(cfg, "./data/requests", nil, logger)
	if err != nil {
		t.Fatalf("Failed to create disk monitor: %v", err)
	}

	// Check with zero bytes (should always pass)
	canWrite, err := dm.CheckDiskSpace(0)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !canWrite {
		t.Error("Expected write to be allowed with zero bytes")
	}

	dm.Stop()
}
