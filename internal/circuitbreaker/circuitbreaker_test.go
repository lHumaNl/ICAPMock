// Copyright 2026 ICAP Mock

package circuitbreaker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// mockMetricsRecorder implements MetricsRecorder for testing.
type mockMetricsRecorder struct {
	states      map[string]string
	failures    map[string]int
	transitions []transition
	mu          sync.Mutex
}

type transition struct {
	component string
	fromState string
	toState   string
}

// newMockMetricsRecorder creates a new mock metrics recorder.
func newMockMetricsRecorder() *mockMetricsRecorder {
	return &mockMetricsRecorder{
		states:   make(map[string]string),
		failures: make(map[string]int),
	}
}

// SetCircuitBreakerState records the current circuit breaker state.
func (m *mockMetricsRecorder) SetCircuitBreakerState(component, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[component] = state
}

// RecordCircuitBreakerTransition records a state transition.
func (m *mockMetricsRecorder) RecordCircuitBreakerTransition(component, fromState, toState string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitions = append(m.transitions, transition{
		component: component,
		fromState: fromState,
		toState:   toState,
	})
}

// RecordCircuitBreakerFailure records a failure event.
func (m *mockMetricsRecorder) RecordCircuitBreakerFailure(component string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[component]++
}

// GetState retrieves the recorded state for a component.
func (m *mockMetricsRecorder) GetState(component string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[component]
}

// GetFailureCount retrieves the failure count for a component.
func (m *mockMetricsRecorder) GetFailureCount(component string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failures[component]
}

// GetTransitionCount returns the total number of transitions.
func (m *mockMetricsRecorder) GetTransitionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.transitions)
}

// GetLastTransition returns the most recent transition.
func (m *mockMetricsRecorder) GetLastTransition() transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.transitions) == 0 {
		return transition{}
	}
	return m.transitions[len(m.transitions)-1]
}

// Reset clears all recorded metrics.
func (m *mockMetricsRecorder) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]string)
	m.transitions = nil
	m.failures = make(map[string]int)
}

// TestCircuitBreakerInitialState tests that the circuit breaker starts in CLOSED state.
func TestCircuitBreakerInitialState(t *testing.T) {
	config := DefaultConfig()
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)

	if cb.State() != StateClosed {
		t.Errorf("expected initial state to be CLOSED, got %v", cb.State())
	}

	if metrics.GetState("test") != "CLOSED" {
		t.Errorf("expected metrics state to be CLOSED, got %v", metrics.GetState("test"))
	}
}

// TestCircuitBreakerClosedToOpen tests transition from CLOSED to OPEN on failures.
func TestCircuitBreakerClosedToOpen(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	testErr := errors.New("test error")

	for i := 0; i < 3; i++ {
		err := cb.Call(ctx, func() error {
			return testErr
		})
		if !errors.Is(err, testErr) {
			t.Errorf("iteration %d: expected error, got %v", i, err)
		}
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state to be OPEN after %d failures, got %v", config.FailureThreshold, cb.State())
	}

	if metrics.GetState("test") != "OPEN" {
		t.Errorf("expected metrics state to be OPEN, got %v", metrics.GetState("test"))
	}

	if metrics.GetFailureCount("test") != 3 {
		t.Errorf("expected 3 failures, got %d", metrics.GetFailureCount("test"))
	}
}

// TestCircuitBreakerOpenToHalfOpen tests transition from OPEN to HALF_OPEN after timeout.
func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	config.OpenTimeout = 100 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	// Should transition to HALF_OPEN on next request
	err := cb.Call(ctx, func() error {
		return nil
	})

	if errors.Is(err, ErrCircuitOpen) {
		t.Error("expected request to be allowed after timeout, got circuit open error")
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("expected state to be HALF_OPEN after timeout, got %v", cb.State())
	}
}

// TestCircuitBreakerHalfOpenToClosed tests transition from HALF_OPEN to CLOSED on successes.
func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	config.SuccessThreshold = 2
	config.HalfOpenMaxRequests = 5
	config.OpenTimeout = 100 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	// Send first success (should be allowed)
	err := cb.Call(ctx, func() error {
		return nil
	})

	if errors.Is(err, ErrCircuitOpen) {
		t.Error("expected first HALF_OPEN request to succeed, got circuit open error")
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("expected state to be HALF_OPEN after 1 success, got %v", cb.State())
	}

	// Send second success (should close circuit)
	err = cb.Call(ctx, func() error {
		return nil
	})

	if errors.Is(err, ErrCircuitOpen) {
		t.Error("expected second HALF_OPEN request to succeed, got circuit open error")
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to be CLOSED after %d successes, got %v", config.SuccessThreshold, cb.State())
	}
}

// TestCircuitBreakerHalfOpenToOpen tests transition from HALF_OPEN to OPEN on failure.
func TestCircuitBreakerHalfOpenToOpen(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	config.SuccessThreshold = 2
	config.OpenTimeout = 100 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	// Send failing request in HALF_OPEN (should reopen circuit)
	err := cb.Call(ctx, func() error {
		return errors.New("half-open failure")
	})

	if err == nil {
		t.Error("expected error from failed request, got nil")
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state to be OPEN after failure in HALF_OPEN, got %v", cb.State())
	}
}

// TestCircuitBreakerRejectsRequestsWhenOpen tests that requests are rejected when OPEN.
func TestCircuitBreakerRejectsRequestsWhenOpen(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Verify requests are rejected
	callCount := 0
	for i := 0; i < 5; i++ {
		err := cb.Call(ctx, func() error {
			callCount++
			return nil
		})
		if !errors.Is(err, ErrCircuitOpen) {
			t.Errorf("iteration %d: expected ErrCircuitOpen, got %v", i, err)
		}
	}

	if callCount > 0 {
		t.Errorf("expected no calls to execute when OPEN, got %d", callCount)
	}
}

// TestCircuitBreakerSlidingWindow tests that failures expire after rolling window.
func TestCircuitBreakerSlidingWindow(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 5
	config.RollingWindow = 200 * time.Millisecond
	config.WindowBuckets = 2
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Generate enough failures to open circuit
	for i := 0; i < 5; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.Failures != 5 {
		t.Errorf("expected 5 failures in stats, got %d", stats.Failures)
	}

	// Reset circuit
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected state to be CLOSED after reset, got %v", cb.State())
	}

	// Wait for rolling window to expire
	time.Sleep(config.RollingWindow + 10*time.Millisecond)

	// Generate new failures
	for i := 0; i < 5; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state to be OPEN after new failures, got %v", cb.State())
	}

	stats = cb.Stats()
	if stats.Failures != 5 {
		t.Errorf("expected 5 failures in stats, got %d", stats.Failures)
	}
}

// TestCircuitBreakerDisabled tests that disabled circuit breaker bypasses logic.
func TestCircuitBreakerDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	testErr := errors.New("test error")

	// Should execute all requests even if they fail
	for i := 0; i < 100; i++ {
		err := cb.Call(ctx, func() error {
			return testErr
		})
		if !errors.Is(err, testErr) {
			t.Errorf("iteration %d: expected test error, got %v", i, err)
		}
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to remain CLOSED when disabled, got %v", cb.State())
	}
}

// TestCircuitBreakerReset tests that Reset() closes the circuit.
func TestCircuitBreakerReset(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Reset circuit
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected state to be CLOSED after reset, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.Failures != 0 {
		t.Errorf("expected 0 failures after reset, got %d", stats.Failures)
	}
}

// TestCircuitBreakerStats tests that Stats() returns accurate statistics.
func TestCircuitBreakerStats(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 10
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Generate successes
	for i := 0; i < 5; i++ {
		cb.Call(ctx, func() error {
			return nil
		})
	}

	// Generate failures
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	stats := cb.Stats()

	if stats.State != StateClosed {
		t.Errorf("expected state CLOSED, got %v", stats.State)
	}

	if stats.Successes != 5 {
		t.Errorf("expected 5 successes, got %d", stats.Successes)
	}

	if stats.Failures != 3 {
		t.Errorf("expected 3 failures, got %d", stats.Failures)
	}

	if stats.Requests != 8 {
		t.Errorf("expected 8 total requests, got %d", stats.Requests)
	}
}

// TestCircuitBreakerConcurrentCalls tests thread-safety under concurrent load.
func TestCircuitBreakerConcurrentCalls(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 50
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100
	callsPerGoroutine := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				cb.Call(ctx, func() error {
					time.Sleep(1 * time.Microsecond)
					return nil
				})
			}
		}(i)
	}

	wg.Wait()

	stats := cb.Stats()
	expectedRequests := int64(numGoroutines * callsPerGoroutine)

	if stats.Requests != expectedRequests {
		t.Errorf("expected %d requests, got %d", expectedRequests, stats.Requests)
	}

	if cb.State() != StateClosed {
		t.Errorf("expected state to remain CLOSED, got %v", cb.State())
	}
}

// TestCircuitBreakerRecordResult tests manual result recording.
func TestCircuitBreakerRecordResult(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)

	// Record failures manually
	cb.RecordResult(false)
	cb.RecordResult(false)
	cb.RecordResult(false)

	if cb.State() != StateOpen {
		t.Errorf("expected state to be OPEN after 3 failures, got %v", cb.State())
	}
}

// TestCircuitBreakerHalfOpenMaxRequests tests request limiting in HALF_OPEN.
func TestCircuitBreakerHalfOpenMaxRequests(t *testing.T) {
	config := DefaultConfig()
	config.FailureThreshold = 3
	config.SuccessThreshold = 5
	config.HalfOpenMaxRequests = 2
	config.OpenTimeout = 100 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("test", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.Call(ctx, func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state to be OPEN, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	// Send allowed requests in HALF_OPEN
	for i := 0; i < 2; i++ {
		err := cb.Call(ctx, func() error {
			return nil
		})
		if err != nil {
			t.Errorf("iteration %d: expected no error, got %v", i, err)
		}
	}

	// Request beyond max should be rejected
	err := cb.Call(ctx, func() error {
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen after max requests, got %v", err)
	}
}

// TestCircuitBreakerStateString tests State.String() method.
func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		expected string
		state    State
	}{
		{"CLOSED", StateClosed},
		{"HALF_OPEN", StateHalfOpen},
		{"OPEN", StateOpen},
		{"UNKNOWN", State(99)},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("state %d: expected %s, got %s", tt.state, tt.expected, got)
		}
	}
}

// BenchmarkCircuitBreakerCallClosed benchmarks Call() in CLOSED state.
func BenchmarkCircuitBreakerCallClosed(b *testing.B) {
	config := DefaultConfig()
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("bench", config, logger, metrics)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Call(ctx, func() error {
				return nil
			})
		}
	})
}

// BenchmarkCircuitBreakerCallOpen benchmarks Call() in OPEN state.
func BenchmarkCircuitBreakerCallOpen(b *testing.B) {
	config := DefaultConfig()
	config.FailureThreshold = 1
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("bench", config, logger, metrics)

	// Open the circuit
	cb.Call(context.Background(), func() error {
		return errors.New("open circuit")
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Call(context.Background(), func() error {
				return nil
			})
		}
	})
}

// BenchmarkCircuitBreakerCallHalfOpen benchmarks Call() in HALF_OPEN state.
func BenchmarkCircuitBreakerCallHalfOpen(b *testing.B) {
	config := DefaultConfig()
	config.FailureThreshold = 1
	config.HalfOpenMaxRequests = 100
	config.OpenTimeout = 10 * time.Millisecond
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("bench", config, logger, metrics)
	ctx := context.Background()

	// Open the circuit
	cb.Call(ctx, func() error {
		return errors.New("open circuit")
	})

	time.Sleep(config.OpenTimeout + 10*time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Call(ctx, func() error {
				return nil
			})
		}
	})
}

// BenchmarkCircuitBreakerRecordResult benchmarks RecordResult().
func BenchmarkCircuitBreakerRecordResult(b *testing.B) {
	config := DefaultConfig()
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("bench", config, logger, metrics)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.RecordResult(i%2 == 0)
	}
}

// BenchmarkCircuitBreakerStats benchmarks Stats().
func BenchmarkCircuitBreakerStats(b *testing.B) {
	config := DefaultConfig()
	logger := slog.Default()
	metrics := newMockMetricsRecorder()

	cb := NewCircuitBreaker("bench", config, logger, metrics)

	// Generate some data
	for i := 0; i < 1000; i++ {
		cb.RecordResult(i%2 == 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Stats()
	}
}
