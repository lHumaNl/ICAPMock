// Package server provides integration tests for circuit breaker.
package server_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/circuitbreaker"
	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/server"
	itesting "github.com/icap-mock/icap-mock/internal/testing"
	"github.com/prometheus/client_golang/prometheus"
)

// TestServerCircuitBreakerIntegration tests circuit breaker integration with server.
func TestServerCircuitBreakerIntegration(t *testing.T) {
	// Create metrics registry
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	// Create circuit breakers
	cbConfig := config.CircuitBreakerGlobalConfig{
		Enabled: true,
		Defaults: config.CircuitBreakerComponentConfig{
			FailureThreshold:    5,
			SuccessThreshold:    3,
			OpenTimeout:         100 * time.Millisecond,
			HalfOpenMaxRequests: 1,
			RollingWindow:       60 * time.Second,
			WindowBuckets:       60,
		},
	}

	logger := slog.Default()
	cbFactory := circuitbreaker.NewFactory(cbConfig, logger, collector)
	storageCB := cbFactory.Create("storage")

	// Create server
	serverCfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 10,
	}

	pool := server.NewConnectionPool()
	srv, err := server.NewServer(serverCfg, pool, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Set circuit breakers
	srv.SetCircuitBreakers(storageCB, nil, nil)

	// Start server
	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(ctx)

	// Verify circuit breaker is accessible
	if srv.StorageCircuitBreaker() == nil {
		t.Error("expected non-nil storage circuit breaker")
	}

	if srv.StorageCircuitBreaker().State() != circuitbreaker.StateClosed {
		t.Errorf("expected initial state CLOSED, got %v", srv.StorageCircuitBreaker().State())
	}
}

// TestServerCircuitBreakerStateTransitions tests state transitions.
func TestServerCircuitBreakerStateTransitions(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	cbConfig := config.CircuitBreakerGlobalConfig{
		Enabled: true,
		Defaults: config.CircuitBreakerComponentConfig{
			FailureThreshold:    2,
			SuccessThreshold:    2, // Need 2 successes to close circuit
			OpenTimeout:         50 * time.Millisecond,
			HalfOpenMaxRequests: 2, // Allow 2 requests in HALF_OPEN
			RollingWindow:       5 * time.Second,
			WindowBuckets:       5,
		},
	}

	logger := slog.Default()
	cbFactory := circuitbreaker.NewFactory(cbConfig, logger, collector)
	cb := cbFactory.Create("transitions_test")

	testCtx := context.Background()
	testErr := errors.New("test error")

	// CLOSED -> OPEN (failures)
	for i := 0; i < 2; i++ {
		err := cb.Call(testCtx, func() error {
			return testErr
		})
		if err != testErr {
			t.Errorf("iteration %d: expected test error, got %v", i, err)
		}
	}

	if cb.State() != circuitbreaker.StateOpen {
		t.Errorf("expected state OPEN after failures, got %v", cb.State())
	}

	// OPEN -> HALF_OPEN (timeout)
	time.Sleep(cbConfig.Defaults.OpenTimeout + 10*time.Millisecond)

	err = cb.Call(testCtx, func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected success in HALF_OPEN, got %v", err)
	}

	if cb.State() != circuitbreaker.StateHalfOpen {
		t.Errorf("expected state HALF_OPEN after timeout, got %v", cb.State())
	}

	// HALF_OPEN -> CLOSED (success)
	var callErr error
	callErr = cb.Call(testCtx, func() error {
		return nil
	})
	if callErr != nil {
		t.Errorf("expected success in HALF_OPEN, got %v", callErr)
	}
	if err != nil {
		t.Errorf("expected success in HALF_OPEN, got %v", err)
	}

	if cb.State() != circuitbreaker.StateClosed {
		t.Errorf("expected state CLOSED after success, got %v", cb.State())
	}
}

// TestServerCircuitBreakerWithHarness tests circuit breaker with server harness.
func TestServerCircuitBreakerWithHarness(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("Failed to create metrics collector: %v", err)
	}

	cbConfig := config.CircuitBreakerGlobalConfig{
		Enabled: true,
		Defaults: config.CircuitBreakerComponentConfig{
			FailureThreshold:    5,
			SuccessThreshold:    3,
			OpenTimeout:         100 * time.Millisecond,
			HalfOpenMaxRequests: 1,
			RollingWindow:       60 * time.Second,
			WindowBuckets:       60,
		},
	}

	logger := slog.Default()
	cbFactory := circuitbreaker.NewFactory(cbConfig, logger, collector)
	storageCB := cbFactory.Create("storage")

	// Create server harness
	serverCfg := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           0,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 10,
	}

	harness := itesting.NewServerHarness(t, serverCfg)
	if err := harness.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer harness.Stop(context.Background())

	// Verify initial state
	if storageCB.State() != circuitbreaker.StateClosed {
		t.Errorf("expected initial state CLOSED, got %v", storageCB.State())
	}

	// Record some successful operations
	for i := 0; i < 5; i++ {
		storageCB.Call(context.Background(), func() error {
			return nil
		})
	}

	// Verify circuit is still closed
	if storageCB.State() != circuitbreaker.StateClosed {
		t.Errorf("expected state CLOSED after successes, got %v", storageCB.State())
	}

	// Verify stats are accurate
	stats := storageCB.Stats()
	if stats.State != circuitbreaker.StateClosed {
		t.Errorf("expected stats state CLOSED, got %v", stats.State)
	}

	if stats.Successes != 5 {
		t.Errorf("expected 5 successes, got %d", stats.Successes)
	}
}
