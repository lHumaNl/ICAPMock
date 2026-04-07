// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestChaosProcessor_Process tests the Process method of ChaosProcessor.
func TestChaosProcessor_Process(t *testing.T) {
	delegate := NewEchoProcessor()

	tests := []struct {
		name        string
		config      ChaosConfig
		expectError bool
	}{
		{
			name: "disabled - passes through",
			config: ChaosConfig{
				Enabled: false,
			},
			expectError: false,
		},
		{
			name: "enabled with no rates - passes through",
			config: ChaosConfig{
				Enabled:            true,
				ErrorRate:          0,
				TimeoutRate:        0,
				ConnectionDropRate: 0,
			},
			expectError: false,
		},
		{
			name: "always inject error",
			config: ChaosConfig{
				Enabled:   true,
				ErrorRate: 1.0,
			},
			expectError: true,
		},
		{
			name: "always inject timeout",
			config: ChaosConfig{
				Enabled:     true,
				TimeoutRate: 1.0,
			},
			expectError: true,
		},
		{
			name: "always inject connection drop",
			config: ChaosConfig{
				Enabled:            true,
				ConnectionDropRate: 1.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewChaosProcessor(tt.config, delegate, nil)

			ctx := context.Background()
			if tt.config.TimeoutRate == 1.0 {
				// Use a short timeout so the test doesn't hang
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
			}

			req := createTestRequest(t)
			resp, err := processor.Process(ctx, req)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != icap.StatusNoContentNeeded {
				t.Errorf("expected status %d, got %d", icap.StatusNoContentNeeded, resp.StatusCode)
			}
		})
	}
}

// TestChaosProcessor_LatencyInjection tests latency injection.
func TestChaosProcessor_LatencyInjection(t *testing.T) {
	delegate := NewEchoProcessor()

	tests := []struct {
		name     string
		minMs    int
		maxMs    int
		minDelay time.Duration
	}{
		{
			name:     "fixed latency",
			minMs:    50,
			maxMs:    50,
			minDelay: 45 * time.Millisecond, // Allow some margin
		},
		{
			name:     "range latency",
			minMs:    20,
			maxMs:    100,
			minDelay: 15 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ChaosConfig{
				Enabled:      true,
				MinLatencyMs: tt.minMs,
				MaxLatencyMs: tt.maxMs,
				LatencyRate:  1.0,
			}
			processor := NewChaosProcessor(config, delegate, nil)

			req := createTestRequest(t)
			start := time.Now()
			_, err := processor.Process(context.Background(), req)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if elapsed < tt.minDelay {
				t.Errorf("expected at least %v latency, got %v", tt.minDelay, elapsed)
			}
		})
	}
}

// TestChaosProcessor_ErrorInjection tests error injection.
func TestChaosProcessor_ErrorInjection(t *testing.T) {
	delegate := NewEchoProcessor()

	// Use deterministic seed for reproducibility
	processor := NewChaosProcessor(ChaosConfig{
		Enabled:   true,
		ErrorRate: 0.5, // 50% error rate
	}, delegate, nil)
	processor.Seed(42) // Fixed seed for reproducibility

	errorCount := 0
	successCount := 0
	iterations := 100

	for i := 0; i < iterations; i++ {
		req := createTestRequest(t)
		_, err := processor.Process(context.Background(), req)

		if err != nil {
			errorCount++
			// Verify it's our injected error
			var icapErr *apperrors.Error
			if !errors.As(err, &icapErr) {
				t.Errorf("expected ICAP error, got %T: %v", err, err)
			}
		} else {
			successCount++
		}
	}

	// With 50% rate and 100 iterations, we should have roughly 50 errors
	// Allow a wide margin for randomness
	if errorCount < 20 || errorCount > 80 {
		t.Errorf("expected ~50 errors (20-80), got %d", errorCount)
	}
}

// TestChaosProcessor_TimeoutInjection tests timeout injection.
func TestChaosProcessor_TimeoutInjection(t *testing.T) {
	delegate := NewEchoProcessor()

	processor := NewChaosProcessor(ChaosConfig{
		Enabled:     true,
		TimeoutRate: 1.0, // Always timeout
	}, delegate, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := createTestRequest(t)
	start := time.Now()
	_, err := processor.Process(ctx, req)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	// Should return within reasonable time
	if elapsed > 200*time.Millisecond {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

// TestChaosProcessor_ConnectionDropInjection tests connection drop injection.
func TestChaosProcessor_ConnectionDropInjection(t *testing.T) {
	delegate := NewEchoProcessor()

	processor := NewChaosProcessor(ChaosConfig{
		Enabled:            true,
		ConnectionDropRate: 1.0, // Always drop
	}, delegate, nil)

	req := createTestRequest(t)
	_, err := processor.Process(context.Background(), req)

	if err == nil {
		t.Fatal("expected connection drop error")
	}

	var connErr *apperrors.Error
	if !errors.As(err, &connErr) {
		t.Errorf("expected ICAP error, got %T: %v", err, err)
	}
}

// TestChaosProcessor_ContextCancellation tests context cancellation.
func TestChaosProcessor_ContextCancellation(t *testing.T) {
	delegate := NewEchoProcessor()

	processor := NewChaosProcessor(ChaosConfig{
		Enabled:      true,
		MinLatencyMs: 5000, // 5 second delay
		MaxLatencyMs: 5000,
		LatencyRate:  1.0,
	}, delegate, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req := createTestRequest(t)
	start := time.Now()
	_, err := processor.Process(ctx, req)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should return quickly due to cancellation
	if elapsed > 200*time.Millisecond {
		t.Errorf("context cancellation took too long: %v", elapsed)
	}
}

// TestChaosProcessor_AlreadyCancelledContext tests with already canceled context.
func TestChaosProcessor_AlreadyCancelledContext(t *testing.T) {
	delegate := NewEchoProcessor()

	processor := NewChaosProcessor(ChaosConfig{
		Enabled: true,
	}, delegate, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := createTestRequest(t)
	_, err := processor.Process(ctx, req)

	if err == nil {
		t.Error("expected context canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestChaosProcessor_Name tests the Name method.
func TestChaosProcessor_Name(t *testing.T) {
	processor := NewChaosProcessor(ChaosConfig{}, nil, nil)
	expected := "ChaosProcessor"

	if processor.Name() != expected {
		t.Errorf("expected name %q, got %q", expected, processor.Name())
	}
}

// TestChaosProcessor_SetDelegate tests SetDelegate method.
func TestChaosProcessor_SetDelegate(t *testing.T) {
	processor := NewChaosProcessor(ChaosConfig{Enabled: false}, nil, nil)

	// Should work after setting delegate
	processor.SetDelegate(NewEchoProcessor())

	req := createTestRequest(t)
	resp, err := processor.Process(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Errorf("expected status %d, got %d", icap.StatusNoContentNeeded, resp.StatusCode)
	}
}

// TestChaosProcessor_SetConfig tests SetConfig method.
func TestChaosProcessor_SetConfig(t *testing.T) {
	processor := NewChaosProcessor(ChaosConfig{Enabled: false}, NewEchoProcessor(), nil)

	// Initially should pass through
	req := createTestRequest(t)
	resp, err := processor.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != icap.StatusNoContentNeeded {
		t.Errorf("expected status %d, got %d", icap.StatusNoContentNeeded, resp.StatusCode)
	}

	// Change config to always error
	processor.SetConfig(ChaosConfig{Enabled: true, ErrorRate: 1.0})
	_, err = processor.Process(context.Background(), req)
	if err == nil {
		t.Error("expected error after config change")
	}
}

// TestChaosProcessor_Config tests Config method.
func TestChaosProcessor_Config(t *testing.T) {
	config := ChaosConfig{
		Enabled:      true,
		ErrorRate:    0.5,
		MinLatencyMs: 10,
		MaxLatencyMs: 100,
	}
	processor := NewChaosProcessor(config, nil, nil)

	got := processor.Config()
	if got.Enabled != config.Enabled {
		t.Errorf("expected Enabled %v, got %v", config.Enabled, got.Enabled)
	}
	if got.ErrorRate != config.ErrorRate {
		t.Errorf("expected ErrorRate %v, got %v", config.ErrorRate, got.ErrorRate)
	}
}

// TestChaosProcessor_Seed tests deterministic seeding.
func TestChaosProcessor_Seed(t *testing.T) {
	delegate := NewEchoProcessor()

	config := ChaosConfig{
		Enabled:   true,
		ErrorRate: 0.5,
	}

	// Create two processors with same seed
	p1 := NewChaosProcessor(config, delegate, nil)
	p1.Seed(12345)

	p2 := NewChaosProcessor(config, delegate, nil)
	p2.Seed(12345)

	// They should produce identical results
	req := createTestRequest(t)
	for i := 0; i < 10; i++ {
		_, err1 := p1.Process(context.Background(), req)
		_, err2 := p2.Process(context.Background(), req)

		if (err1 == nil) != (err2 == nil) {
			t.Errorf("iteration %d: p1 error=%v, p2 error=%v", i, err1, err2)
		}
	}
}

// TestChaosProcessor_ThreadSafety tests thread safety.
func TestChaosProcessor_ThreadSafety(t *testing.T) {
	delegate := NewEchoProcessor()

	processor := NewChaosProcessor(ChaosConfig{
		Enabled:      true,
		ErrorRate:    0.1,
		MinLatencyMs: 1,
		MaxLatencyMs: 5,
		LatencyRate:  1.0,
	}, delegate, nil)

	const goroutines = 50
	done := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			req := createTestRequest(t)
			_, err := processor.Process(context.Background(), req)
			done <- err
		}()
	}

	timeout := time.After(5 * time.Second)
	for i := 0; i < goroutines; i++ {
		select {
		case err := <-done:
			// Error or nil is fine, we just want to verify no panics
			_ = err
		case <-timeout:
			t.Fatal("timeout waiting for goroutines")
		}
	}
}

// TestChaosProcessor_Interface verifies ChaosProcessor implements Processor interface.
func TestChaosProcessor_Interface(t *testing.T) {
	var _ Processor = NewChaosProcessor(ChaosConfig{}, nil, nil)
}

// TestChaosProcessor_LatencyEdgeCases tests edge cases in latency calculation.
func TestChaosProcessor_LatencyEdgeCases(t *testing.T) {
	delegate := NewEchoProcessor()

	tests := []struct {
		name  string
		minMs int
		maxMs int
	}{
		{
			name:  "zero latency",
			minMs: 0,
			maxMs: 0,
		},
		{
			name:  "max less than min - uses min",
			minMs: 100,
			maxMs: 50,
		},
		{
			name:  "only min set",
			minMs: 50,
			maxMs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ChaosConfig{
				Enabled:      true,
				MinLatencyMs: tt.minMs,
				MaxLatencyMs: tt.maxMs,
				LatencyRate:  1.0,
			}
			processor := NewChaosProcessor(config, delegate, nil)

			req := createTestRequest(t)
			// Should not panic and should complete
			_, err := processor.Process(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
