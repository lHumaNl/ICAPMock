// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// ChaosConfig holds the configuration for the ChaosProcessor.
// It defines the probabilities and parameters for fault injection.
type ChaosConfig struct {
	// Enabled determines whether chaos injection is active.
	Enabled bool

	// ErrorRate is the probability (0.0 to 1.0) of injecting an error response.
	// 0.0 means no errors, 1.0 means always error.
	ErrorRate float64

	// TimeoutRate is the probability (0.0 to 1.0) of simulating a timeout.
	// 0.0 means no timeouts, 1.0 means always timeout.
	TimeoutRate float64

	// MinLatencyMs is the minimum latency to inject in milliseconds.
	// Set to 0 to disable latency injection.
	MinLatencyMs int

	// MaxLatencyMs is the maximum latency to inject in milliseconds.
	// Must be >= MinLatencyMs for latency injection to work.
	MaxLatencyMs int

	// LatencyRate is the probability (0.0 to 1.0) of injecting latency.
	// 0.0 means no latency, 1.0 means always inject latency.
	LatencyRate float64

	// ConnectionDropRate is the probability (0.0 to 1.0) of dropping the connection.
	// 0.0 means no drops, 1.0 means always drop.
	ConnectionDropRate float64
}

// ChaosProcessor is a decorator processor that injects faults for testing resilience.
// It wraps another processor and can inject latency, errors, timeouts, and connection drops.
//
// ChaosProcessor is useful for:
//   - Testing client retry logic
//   - Testing timeout handling
//   - Simulating network issues
//   - Load testing with artificial delays
//
// ChaosProcessor is thread-safe and uses a thread-safe random number generator.
type ChaosProcessor struct {
	delegate Processor
	random   *rand.Rand
	logger   *logger.Logger
	config   ChaosConfig
	mu       sync.Mutex
}

// NewChaosProcessor creates a new ChaosProcessor with the given configuration.
//
// Parameters:
//   - config: Chaos configuration defining fault injection parameters
//   - delegate: The underlying processor to wrap (can be nil, but Process will panic)
//   - log: Optional logger for recording chaos events (can be nil)
//
// The processor uses a thread-safe random number generator with a time-based seed.
func NewChaosProcessor(config ChaosConfig, delegate Processor, log *logger.Logger) *ChaosProcessor {
	config = clampChaosConfig(config)
	return &ChaosProcessor{
		config:   config,
		random:   rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //nolint:gosec // crypto not needed here
		delegate: delegate,
		logger:   log,
	}
}

// clampChaosConfig clamps invalid values in a ChaosConfig to valid ranges.
func clampChaosConfig(cfg ChaosConfig) ChaosConfig {
	if cfg.ErrorRate < 0 {
		cfg.ErrorRate = 0
	}
	if cfg.TimeoutRate < 0 {
		cfg.TimeoutRate = 0
	}
	if cfg.LatencyRate < 0 {
		cfg.LatencyRate = 0
	}
	if cfg.ConnectionDropRate < 0 {
		cfg.ConnectionDropRate = 0
	}
	if cfg.MinLatencyMs < 0 {
		cfg.MinLatencyMs = 0
	}
	if cfg.MaxLatencyMs < 0 {
		cfg.MaxLatencyMs = 0
	}
	return cfg
}

// Process handles the ICAP request with potential fault injection.
//
// The fault injection order is:
//  1. Latency injection (if configured)
//  2. Error injection (if configured)
//  3. Timeout simulation (if configured)
//  4. Connection drop simulation (if configured)
//  5. Delegate to underlying processor (if no fault was injected)
//
// If the processor is disabled (Enabled=false), it passes through to the delegate
// without any fault injection.
func (p *ChaosProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	// Check context before processing
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Snapshot delegate under lock to avoid data race with SetDelegate
	p.mu.Lock()
	delegate := p.delegate
	config := p.config
	p.mu.Unlock()

	// Pass through if chaos is disabled
	if !config.Enabled {
		return delegate.Process(ctx, req)
	}

	// Inject latency (use local config snapshot to avoid data race)
	if p.shouldInjectLatency(config) {
		delay := p.calculateLatency(config)
		if p.logger != nil {
			p.logger.Debug("injecting latency",
				"request_id", util.RequestIDFromContext(ctx),
				"latency_ms", delay,
				"min_ms", config.MinLatencyMs,
				"max_ms", config.MaxLatencyMs,
			)
		}
		select {
		case <-time.After(delay):
			// Delay completed
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Check context after delay
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Inject error
	if p.shouldInject(config.ErrorRate) {
		if p.logger != nil {
			p.logger.Debug("injecting error",
				"request_id", util.RequestIDFromContext(ctx),
				"error_rate", config.ErrorRate,
			)
		}
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			"chaos: injected error",
			apperrors.ErrInternalServerError.ICAPStatus,
			nil,
		)
	}

	// Simulate timeout (wait until context deadline or very long time)
	if p.shouldInject(config.TimeoutRate) {
		if p.logger != nil {
			p.logger.Debug("injecting timeout",
				"request_id", util.RequestIDFromContext(ctx),
				"timeout_rate", config.TimeoutRate,
			)
		}
		// Wait for context to timeout or be canceled
		<-ctx.Done()
		return nil, context.DeadlineExceeded
	}

	// Simulate connection drop
	if p.shouldInject(config.ConnectionDropRate) {
		if p.logger != nil {
			p.logger.Debug("injecting connection drop",
				"request_id", util.RequestIDFromContext(ctx),
				"drop_rate", config.ConnectionDropRate,
			)
		}
		return nil, apperrors.NewConnectionError("chaos: injected connection drop", nil)
	}

	// Delegate to underlying processor
	return delegate.Process(ctx, req)
}

// shouldInject returns true if a fault should be injected based on the given rate.
func (p *ChaosProcessor) shouldInject(rate float64) bool {
	if rate <= 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.random.Float64() < rate
}

// shouldInjectLatency returns true if latency should be injected.
// Uses the provided config snapshot to avoid data races.
func (p *ChaosProcessor) shouldInjectLatency(config ChaosConfig) bool {
	if config.MinLatencyMs <= 0 && config.MaxLatencyMs <= 0 {
		return false
	}
	return p.shouldInject(config.LatencyRate)
}

// calculateLatency calculates the latency to inject.
// Uses the provided config snapshot to avoid data races.
func (p *ChaosProcessor) calculateLatency(config ChaosConfig) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	minMs := config.MinLatencyMs
	maxMs := config.MaxLatencyMs

	// Handle edge cases
	if minMs <= 0 && maxMs <= 0 {
		return 0
	}
	if minMs <= 0 {
		minMs = 0
	}
	if maxMs <= 0 {
		maxMs = minMs
	}
	if maxMs < minMs {
		maxMs = minMs
	}

	if minMs == maxMs {
		return time.Duration(minMs) * time.Millisecond
	}

	delay := p.random.IntN(maxMs-minMs+1) + minMs
	return time.Duration(delay) * time.Millisecond
}

// Name returns "ChaosProcessor" as the processor name.
func (p *ChaosProcessor) Name() string {
	return "ChaosProcessor"
}

// SetDelegate sets the underlying processor.
// This can be used to change the delegate after creation.
func (p *ChaosProcessor) SetDelegate(delegate Processor) {
	p.mu.Lock()
	p.delegate = delegate
	p.mu.Unlock()
}

// SetConfig updates the chaos configuration.
func (p *ChaosProcessor) SetConfig(config ChaosConfig) {
	config = clampChaosConfig(config)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config = config
}

// Config returns the current chaos configuration.
func (p *ChaosProcessor) Config() ChaosConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config
}

// SetLogger sets the logger for the processor.
func (p *ChaosProcessor) SetLogger(log *logger.Logger) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logger = log
}

// Seed sets the seed for the random number generator.
// This is useful for deterministic testing.
func (p *ChaosProcessor) Seed(seed uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.random = rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // crypto not needed here
}
