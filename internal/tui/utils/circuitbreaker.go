package utils

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

var CircuitOpenError = errors.New("circuit breaker is open")

type CircuitState int

const (
	StateClosed CircuitState = iota
	StateHalfOpen
	StateOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateHalfOpen:
		return "HalfOpen"
	case StateOpen:
		return "Open"
	default:
		return "Unknown"
	}
}

type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	Enabled          bool
}

type CircuitBreaker struct {
	config      CircuitBreakerConfig
	state       CircuitState
	failures    int
	successes   int
	lastFailure time.Time
	mu          sync.RWMutex
}

func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  StateClosed,
	}
}

func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.RLock()
	enabled := cb.config.Enabled
	state := cb.state
	cb.mu.RUnlock()

	if !enabled {
		return fn()
	}

	if state == StateOpen {
		cb.mu.RLock()
		lastFailure := cb.lastFailure
		timeout := cb.config.Timeout
		cb.mu.RUnlock()

		if time.Since(lastFailure) > timeout {
			cb.mu.Lock()
			if cb.state == StateOpen {
				cb.state = StateHalfOpen
				cb.successes = 0
				log.Printf("[CircuitBreaker] Transition: Open -> HalfOpen (after %v timeout)", timeout)
			}
			cb.mu.Unlock()
		} else {
			return CircuitOpenError
		}
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.RecordFailureLocked()
	} else {
		cb.RecordSuccessLocked()
	}

	return err
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.RecordSuccessLocked()
}

func (cb *CircuitBreaker) RecordSuccessLocked() {
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = StateClosed
			cb.failures = 0
			cb.successes = 0
			log.Printf("[CircuitBreaker] Transition: HalfOpen -> Closed (after %d successes)", cb.config.SuccessThreshold)
		}
	} else if cb.state == StateClosed {
		cb.failures = max(0, cb.failures-1)
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.RecordFailureLocked()
}

func (cb *CircuitBreaker) RecordFailureLocked() {
	cb.lastFailure = time.Now()

	if cb.state == StateClosed {
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.successes = 0
			log.Printf("[CircuitBreaker] Transition: Closed -> Open (after %d failures)", cb.config.FailureThreshold)
		}
	} else if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.successes = 0
		log.Printf("[CircuitBreaker] Transition: HalfOpen -> Open (failure in half-open state)")
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := cb.state
	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastFailure = time.Time{}

	if oldState != StateClosed {
		log.Printf("[CircuitBreaker] Reset: %s -> Closed", oldState)
	}
}

func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state.String(),
		"failures":          cb.failures,
		"successes":         cb.successes,
		"last_failure":      cb.lastFailure.Format(time.RFC3339),
		"enabled":           cb.config.Enabled,
		"failure_threshold": cb.config.FailureThreshold,
		"success_threshold": cb.config.SuccessThreshold,
		"timeout":           cb.config.Timeout.String(),
	}
}

func DefaultMetricsCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		Enabled:          true,
	})
}

func DefaultConfigCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          60 * time.Second,
		Enabled:          true,
	})
}

func DefaultControlCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		Enabled:          true,
	})
}

func DefaultScenariosCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          60 * time.Second,
		Enabled:          true,
	})
}

func DefaultReplayCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		Enabled:          true,
	})
}
