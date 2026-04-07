// Copyright 2026 ICAP Mock

package utils

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          10 * time.Second,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	if cb == nil {
		t.Fatal("expected circuit breaker to be created")
	}

	if cb.state != StateClosed {
		t.Errorf("expected initial state to be Closed, got %v", cb.state)
	}

	if cb.failures != 0 {
		t.Errorf("expected initial failures to be 0, got %d", cb.failures)
	}

	if cb.successes != 0 {
		t.Errorf("expected initial successes to be 0, got %d", cb.successes)
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	t.Run("Closed to Open on threshold failures", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		for i := 0; i < 3; i++ {
			err := cb.Execute(context.Background(), func() error {
				return errors.New("test error")
			})
			if err == nil {
				t.Errorf("expected error on failure %d", i+1)
			}
		}

		if cb.State() != StateOpen {
			t.Errorf("expected state to be Open after 3 failures, got %v", cb.State())
		}
	})

	t.Run("Open to HalfOpen after timeout", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		if cb.State() != StateOpen {
			t.Fatalf("expected state to be Open, got %v", cb.State())
		}

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		if cb.State() != StateHalfOpen {
			t.Errorf("expected state to be HalfOpen after timeout, got %v", cb.State())
		}
	})

	t.Run("HalfOpen to Closed on success threshold", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		time.Sleep(150 * time.Millisecond)

		for i := 0; i < 2; i++ {
			cb.Execute(context.Background(), func() error {
				return nil
			})
		}

		if cb.State() != StateClosed {
			t.Errorf("expected state to be Closed after 2 successes, got %v", cb.State())
		}
	})

	t.Run("HalfOpen to Open on failure", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		time.Sleep(150 * time.Millisecond)

		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
		if err == nil {
			t.Error("expected error on failure")
		}

		if cb.State() != StateOpen {
			t.Errorf("expected state to be Open after failure in HalfOpen, got %v", cb.State())
		}
	})
}

func TestCircuitBreaker_FailImmediatelyWhenOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be Open, got %v", cb.State())
	}

	executed := false
	err := cb.Execute(context.Background(), func() error {
		executed = true
		return nil
	})

	if !errors.Is(err, CircuitOpenError) {
		t.Errorf("expected CircuitOpenError, got %v", err)
	}

	if executed {
		t.Error("expected function not to be executed when circuit is open")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          10 * time.Second,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be Open, got %v", cb.State())
	}

	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected state to be Closed after reset, got %v", cb.State())
	}

	if cb.failures != 0 {
		t.Errorf("expected failures to be 0 after reset, got %d", cb.failures)
	}

	if cb.successes != 0 {
		t.Errorf("expected successes to be 0 after reset, got %d", cb.successes)
	}
}

func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 10,
		SuccessThreshold: 5,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := cb.Execute(context.Background(), func() error {
				if idx%3 == 0 {
					return errors.New("simulated error")
				}
				return nil
			})
			errChan <- err
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil && !errors.Is(err, CircuitOpenError) && err.Error() != "simulated error" {
			t.Errorf("unexpected error: %v", err)
		}
	}

	state := cb.State()
	if state != StateClosed && state != StateOpen && state != StateHalfOpen {
		t.Errorf("unexpected state after concurrent calls: %v", state)
	}
}

func TestCircuitBreaker_DifferentThresholds(t *testing.T) {
	tests := []struct {
		name             string
		failureThreshold int
		successThreshold int
	}{
		{"Low thresholds", 2, 1},
		{"Medium thresholds", 5, 3},
		{"High thresholds", 20, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CircuitBreakerConfig{
				FailureThreshold: tt.failureThreshold,
				SuccessThreshold: tt.successThreshold,
				Timeout:          100 * time.Millisecond,
				Enabled:          true,
			}

			cb := NewCircuitBreaker(config)

			for i := 0; i < tt.failureThreshold; i++ {
				cb.RecordFailure()
			}

			if cb.State() != StateOpen {
				t.Errorf("expected Open after %d failures, got %v", tt.failureThreshold, cb.State())
			}

			time.Sleep(150 * time.Millisecond)

			for i := 0; i < tt.successThreshold; i++ {
				cb.Execute(context.Background(), func() error {
					return nil
				})
			}

			if cb.State() != StateClosed {
				t.Errorf("expected Closed after %d successes, got %v", tt.successThreshold, cb.State())
			}
		})
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	t.Run("RecordSuccess in Closed state", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		cb.RecordFailure()
		cb.RecordFailure()

		if cb.failures != 2 {
			t.Errorf("expected 2 failures, got %d", cb.failures)
		}

		cb.RecordSuccess()

		if cb.failures != 1 {
			t.Errorf("expected 1 failure after success, got %d", cb.failures)
		}
	})

	t.Run("RecordSuccess in HalfOpen state", func(t *testing.T) {
		cb := NewCircuitBreaker(config)

		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		time.Sleep(150 * time.Millisecond)

		cb.Execute(context.Background(), func() error {
			return nil
		})

		if cb.State() != StateHalfOpen {
			t.Fatalf("expected HalfOpen, got %v", cb.State())
		}

		if cb.successes != 1 {
			t.Errorf("expected 1 success, got %d", cb.successes)
		}

		cb.Execute(context.Background(), func() error {
			return nil
		})

		if cb.State() != StateClosed {
			t.Errorf("expected Closed after 2 successes, got %v", cb.State())
		}
	})
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	cb.RecordFailure()

	if cb.failures != 1 {
		t.Errorf("expected 1 failure, got %d", cb.failures)
	}

	if cb.State() != StateClosed {
		t.Errorf("expected Closed after 1 failure, got %v", cb.State())
	}

	cb.RecordFailure()

	if cb.failures != 2 {
		t.Errorf("expected 2 failures, got %d", cb.failures)
	}

	if cb.State() != StateOpen {
		t.Errorf("expected Open after 2 failures, got %v", cb.State())
	}
}

func TestCircuitBreaker_StatePersistence(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	for i := 0; i < 2; i++ {
		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
		if err == nil {
			t.Errorf("expected error on attempt %d", i+1)
		}
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to be Closed after 2 failures, got %v", cb.State())
	}

	if cb.failures != 2 {
		t.Errorf("expected 2 failures, got %d", cb.failures)
	}

	err := cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})
	if err == nil {
		t.Error("expected error on third attempt")
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state to be Open after 3 failures, got %v", cb.State())
	}
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          false,
	}

	cb := NewCircuitBreaker(config)

	for i := 0; i < 5; i++ {
		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
		if err == nil {
			t.Errorf("expected error on attempt %d", i+1)
		}
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to remain Closed when disabled, got %v", cb.State())
	}
}

func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := cb.Execute(ctx, func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			return nil
		}
	})

	if err == nil {
		t.Error("expected error on context cancellation")
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to remain Closed after context cancellation (not a server failure), got %v", cb.State())
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          10 * time.Second,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()

	if stats["state"] != "Closed" {
		t.Errorf("expected state 'Closed', got %v", stats["state"])
	}

	if stats["failures"] != 2 {
		t.Errorf("expected failures 2, got %v", stats["failures"])
	}

	if stats["successes"] != 0 {
		t.Errorf("expected successes 0, got %v", stats["successes"])
	}

	if stats["enabled"] != true {
		t.Errorf("expected enabled true, got %v", stats["enabled"])
	}
}

func TestCircuitBreaker_DefaultConfigs(t *testing.T) {
	tests := []struct {
		creator         func() *CircuitBreaker
		name            string
		expectedFailure int
		expectedTimeout time.Duration
	}{
		{"Metrics", DefaultMetricsCircuitBreaker, 10, 30 * time.Second},
		{"Config", DefaultConfigCircuitBreaker, 5, 60 * time.Second},
		{"Control", DefaultControlCircuitBreaker, 3, 30 * time.Second},
		{"Scenarios", DefaultScenariosCircuitBreaker, 5, 60 * time.Second},
		{"Replay", DefaultReplayCircuitBreaker, 3, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := tt.creator()

			if cb == nil {
				t.Fatal("expected circuit breaker to be created")
			}

			if cb.config.FailureThreshold != tt.expectedFailure {
				t.Errorf("expected failure threshold %d, got %d", tt.expectedFailure, cb.config.FailureThreshold)
			}

			if cb.config.Timeout != tt.expectedTimeout {
				t.Errorf("expected timeout %v, got %v", tt.expectedTimeout, cb.config.Timeout)
			}

			if !cb.config.Enabled {
				t.Error("expected circuit breaker to be enabled")
			}
		})
	}
}

func TestCircuitBreaker_RaceDetection(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          50 * time.Millisecond,
		Enabled:          true,
	}

	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			switch idx % 4 {
			case 0:
				cb.Execute(context.Background(), func() error {
					time.Sleep(10 * time.Millisecond)
					if idx%7 == 0 {
						return errors.New("error")
					}
					return nil
				})
			case 1:
				cb.RecordSuccess()
			case 2:
				cb.RecordFailure()
			case 3:
				_ = cb.State()
				_ = cb.Stats()
			}
		}(i)
	}

	wg.Wait()

	state := cb.State()
	if state != StateClosed && state != StateOpen && state != StateHalfOpen {
		t.Errorf("unexpected state: %v", state)
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		expected string
		state    CircuitState
	}{
		{StateClosed, "Closed"},
		{StateHalfOpen, "HalfOpen"},
		{StateOpen, "Open"},
		{CircuitState(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.state.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.state.String())
			}
		})
	}
}
