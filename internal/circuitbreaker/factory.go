// Package circuitbreaker provides factory functions for creating circuit breakers
// from configuration.
package circuitbreaker

import (
	"log/slog"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
)

// Factory creates circuit breakers from configuration.
// It provides a convenient way to create multiple circuit breakers
// with consistent configuration.
//
// Example usage:
//
//	factory := circuitbreaker.NewFactory(cfg.CircuitBreaker, logger, metrics)
//	storageCB := factory.Create("storage")
//	scenarioCB := factory.Create("scenario_loader")
type Factory struct {
	config  config.CircuitBreakerGlobalConfig
	logger  *slog.Logger
	metrics *metrics.Collector
}

// NewFactory creates a new circuit breaker factory.
//
// Parameters:
//   - cfg: Global circuit breaker configuration
//   - logger: Structured logger for circuit breakers
//   - metrics: Metrics collector for recording circuit breaker events
//
// Returns a new Factory instance.
func NewFactory(
	cfg config.CircuitBreakerGlobalConfig,
	logger *slog.Logger,
	metrics *metrics.Collector,
) *Factory {
	return &Factory{
		config:  cfg,
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a circuit breaker for the given component name.
//
// If the component has custom configuration in cfg.Components, it uses that.
// Otherwise, it uses the default configuration from cfg.Defaults.
//
// Parameters:
//   - name: Component name (e.g., "storage", "scenario_loader", "metrics_server")
//
// Returns a new CircuitBreaker instance configured for the component.
//
// Example:
//
//	storageCB := factory.Create("storage")
//	scenarioCB := factory.Create("scenario_loader")
func (f *Factory) Create(name string) *CircuitBreaker {
	// Get component-specific configuration, or use defaults
	componentConfig := f.config.Defaults
	if customCfg, ok := f.config.Components[name]; ok {
		componentConfig = customCfg
	}

	// Build circuit breaker configuration
	cbConfig := Config{
		FailureThreshold:    componentConfig.FailureThreshold,
		SuccessThreshold:    componentConfig.SuccessThreshold,
		OpenTimeout:         componentConfig.OpenTimeout,
		HalfOpenMaxRequests: componentConfig.HalfOpenMaxRequests,
		RollingWindow:       componentConfig.RollingWindow,
		WindowBuckets:       componentConfig.WindowBuckets,
		Enabled:             f.config.Enabled,
	}

	// Create and return circuit breaker
	return NewCircuitBreaker(name, cbConfig, f.logger, f.metrics)
}
