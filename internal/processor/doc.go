// Package processor provides request processing implementations for the ICAP Mock Server.
//
// The processor package defines the core Processor interface and provides several
// implementations for different processing modes. Processors handle incoming ICAP
// requests and return appropriate responses.
//
// # Processor Interface
//
// The Processor interface is the core abstraction:
//
//	type Processor interface {
//	    Process(ctx context.Context, req *icap.Request) (*icap.Response, error)
//	    Name() string
//	}
//
// All implementations must be thread-safe as they may be called concurrently
// from multiple goroutines.
//
// # Available Processors
//
// EchoProcessor - Simple pass-through that returns 204 No Content Needed.
// Use for testing connectivity and baseline performance:
//
//	echo := processor.NewEchoProcessor()
//	resp, err := echo.Process(ctx, req)
//	// resp.StatusCode == 204
//
// MockProcessor - Scenario-based response matching using ScenarioRegistry.
// Use for realistic mock responses based on request patterns:
//
//	registry := storage.NewScenarioRegistry()
//	registry.Add(&storage.Scenario{...})
//	mock := processor.NewMockProcessor(registry, logger)
//	resp, err := mock.Process(ctx, req)
//
// ChaosProcessor - Fault injection decorator for testing resilience.
// Wraps another processor and injects latency, errors, timeouts, and connection drops:
//
//	config := processor.ChaosConfig{
//	    Enabled:      true,
//	    ErrorRate:    0.1,  // 10% error rate
//	    MinLatencyMs: 10,
//	    MaxLatencyMs: 100,
//	}
//	chaos := processor.NewChaosProcessor(config, delegate, logger)
//	resp, err := chaos.Process(ctx, req)
//
// # Processor Chain
//
// Processors can be chained using the Chain function (deprecated, use middleware pattern):
//
//	chain := processor.Chain(
//	    loggingProcessor,
//	    rateLimitProcessor,
//	    mockProcessor,
//	)
//
// # Thread Safety
//
// All processor implementations are thread-safe and can be used concurrently.
// The ChaosProcessor uses a mutex-protected random number generator for thread safety.
package processor
