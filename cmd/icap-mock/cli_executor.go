// Package main provides CLI command execution for the ICAP Mock Server.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	"github.com/icap-mock/icap-mock/internal/health"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/middleware"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/ratelimit"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/internal/server"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrintVersion prints version information to stdout.
func PrintVersion() {
	fmt.Printf("icap-mock version %s\n", version)
	fmt.Printf("  git commit: %s\n", gitCommit)
	fmt.Printf("  build date: %s\n", buildDate)
	fmt.Printf("  go version: %s\n", runtime.Version())
	fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// RunValidateMode validates the configuration and prints results.
// This is the implementation of --validate/--dry-run mode.
func RunValidateMode(w io.Writer, cfg *config.Config) error {
	fmt.Fprintln(w, "Validating configuration...")
	fmt.Fprintln(w)

	// Track validation status
	allPassed := true

	// Validate server configuration
	printServerConfig(w, cfg)
	printLoggingConfig(w, cfg)
	printMetricsConfig(w, cfg)
	printMockConfig(w, cfg, &allPassed)
	printChaosConfig(w, cfg)
	printStorageConfig(w, cfg)
	printRateLimitConfig(w, cfg)
	printHealthConfig(w, cfg)
	printPluginConfig(w, cfg)

	// Print validation summary
	if allPassed {
		fmt.Fprintln(w, "Configuration validation: PASSED")
		return nil
	}

	return errors.New("configuration validation failed")
}

// printServerConfig prints server configuration for validation.
func printServerConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Server Configuration:\n")
	fmt.Fprintf(w, "  host: %s\n", cfg.Server.Host)
	fmt.Fprintf(w, "  port: %d\n", cfg.Server.Port)
	fmt.Fprintf(w, "  read_timeout: %s\n", cfg.Server.ReadTimeout)
	fmt.Fprintf(w, "  write_timeout: %s\n", cfg.Server.WriteTimeout)
	fmt.Fprintf(w, "  max_connections: %d\n", cfg.Server.MaxConnections)
	fmt.Fprintf(w, "  max_body_size: %d bytes\n", cfg.Server.MaxBodySize)
	fmt.Fprintf(w, "  streaming: %v\n", cfg.Server.Streaming)
	if cfg.Server.TLS.Enabled {
		fmt.Fprintf(w, "  tls: enabled (cert=%s)\n", cfg.Server.TLS.CertFile)
	} else {
		fmt.Fprintf(w, "  tls: disabled\n")
	}
	fmt.Fprintln(w)
}

// printLoggingConfig prints logging configuration for validation.
func printLoggingConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Logging Configuration:\n")
	fmt.Fprintf(w, "  level: %s\n", cfg.Logging.Level)
	fmt.Fprintf(w, "  format: %s\n", cfg.Logging.Format)
	fmt.Fprintf(w, "  output: %s\n", cfg.Logging.Output)
	fmt.Fprintf(w, "  max_size: %d MB\n", cfg.Logging.MaxSize)
	fmt.Fprintf(w, "  max_backups: %d\n", cfg.Logging.MaxBackups)
	fmt.Fprintf(w, "  max_age: %d days\n", cfg.Logging.MaxAge)
	fmt.Fprintln(w)
}

// printMetricsConfig prints metrics configuration for validation.
func printMetricsConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Metrics Configuration:\n")
	fmt.Fprintf(w, "  enabled: %v\n", cfg.Metrics.Enabled)
	if cfg.Metrics.Enabled {
		fmt.Fprintf(w, "  host: %s\n", cfg.Metrics.Host)
		fmt.Fprintf(w, "  port: %d\n", cfg.Metrics.Port)
		fmt.Fprintf(w, "  path: %s\n", cfg.Metrics.Path)
	}
	fmt.Fprintln(w)
}

// printMockConfig prints mock configuration for validation.
func printMockConfig(w io.Writer, cfg *config.Config, allPassed *bool) {
	fmt.Fprintf(w, "Mock Configuration:\n")
	fmt.Fprintf(w, "  default_mode: %s\n", cfg.Mock.DefaultMode)
	fmt.Fprintf(w, "  scenarios_dir: %s\n", cfg.Mock.ScenariosDir)
	fmt.Fprintf(w, "  default_timeout: %s\n", cfg.Mock.DefaultTimeout)

	// Check scenarios directory
	if cfg.Mock.ScenariosDir != "" {
		if info, err := os.Stat(cfg.Mock.ScenariosDir); err == nil && info.IsDir() {
			// Count scenario files
			files, err := os.ReadDir(cfg.Mock.ScenariosDir)
			if err == nil {
				scenarioCount := 0
				for _, f := range files {
					name := f.Name()
					if !f.IsDir() && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
						scenarioCount++
					}
				}
				fmt.Fprintf(w, "  scenarios loaded: %d files found\n", scenarioCount)
			}
		} else {
			fmt.Fprintf(w, "  WARNING: scenarios directory not found: %s\n", cfg.Mock.ScenariosDir)
			*allPassed = false
		}
	}
	fmt.Fprintln(w)
}

// printChaosConfig prints chaos configuration for validation.
func printChaosConfig(w io.Writer, cfg *config.Config) {
	if cfg.Chaos.Enabled {
		fmt.Fprintf(w, "Chaos Configuration:\n")
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Chaos.Enabled)
		fmt.Fprintf(w, "  error_rate: %.2f\n", cfg.Chaos.ErrorRate)
		fmt.Fprintf(w, "  timeout_rate: %.2f\n", cfg.Chaos.TimeoutRate)
		fmt.Fprintf(w, "  latency: %d-%d ms\n", cfg.Chaos.MinLatencyMs, cfg.Chaos.MaxLatencyMs)
		fmt.Fprintf(w, "  connection_drop_rate: %.2f\n", cfg.Chaos.ConnectionDropRate)
		fmt.Fprintln(w)
	}
}

// printStorageConfig prints storage configuration for validation.
func printStorageConfig(w io.Writer, cfg *config.Config) {
	if cfg.Storage.Enabled {
		fmt.Fprintf(w, "Storage Configuration:\n")
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Storage.Enabled)
		fmt.Fprintf(w, "  requests_dir: %s\n", cfg.Storage.RequestsDir)
		fmt.Fprintf(w, "  max_file_size: %d bytes\n", cfg.Storage.MaxFileSize)
		fmt.Fprintf(w, "  rotate_after: %d requests\n", cfg.Storage.RotateAfter)
		fmt.Fprintln(w)
	}
}

// printRateLimitConfig prints rate limit configuration for validation.
func printRateLimitConfig(w io.Writer, cfg *config.Config) {
	if cfg.RateLimit.Enabled {
		fmt.Fprintf(w, "Rate Limit Configuration:\n")
		fmt.Fprintf(w, "  enabled: %v\n", cfg.RateLimit.Enabled)
		fmt.Fprintf(w, "  requests_per_second: %.0f\n", cfg.RateLimit.RequestsPerSecond)
		fmt.Fprintf(w, "  burst: %d\n", cfg.RateLimit.Burst)
		fmt.Fprintf(w, "  algorithm: %s\n", cfg.RateLimit.Algorithm)
		fmt.Fprintln(w)
	}
}

// printHealthConfig prints health configuration for validation.
func printHealthConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Health Configuration:\n")
	fmt.Fprintf(w, "  enabled: %v\n", cfg.Health.Enabled)
	if cfg.Health.Enabled {
		fmt.Fprintf(w, "  port: %d\n", cfg.Health.Port)
		fmt.Fprintf(w, "  health_path: %s\n", cfg.Health.HealthPath)
		fmt.Fprintf(w, "  ready_path: %s\n", cfg.Health.ReadyPath)
	}
	fmt.Fprintln(w)
}

// printPluginConfig prints plugin configuration for validation.
func printPluginConfig(w io.Writer, cfg *config.Config) {
	if cfg.Plugin.Enabled {
		fmt.Fprintf(w, "Plugin Configuration:\n")
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Plugin.Enabled)
		fmt.Fprintf(w, "  dir: %s\n", cfg.Plugin.Dir)
		fmt.Fprintln(w)
	}
}

// Run starts the ICAP server with the given configuration.
// The provided context controls the server lifecycle — cancelling it triggers graceful shutdown.
func Run(ctx context.Context, cfg *config.Config) error {
	// Initialize logger
	log, err := logger.New(cfg.Logging)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	defer log.Close()

	// Create metrics collector
	metricsRegistry, collector, err := createMetricsCollector(cfg)
	if err != nil {
		return fmt.Errorf("creating metrics collector: %w", err)
	}

	// Create rate limiter (used for request throttling)
	limiter := createRateLimiter(cfg)

	// Create storage manager
	store, err := createStorageManager(cfg, collector)
	if err != nil {
		return fmt.Errorf("creating storage manager: %w", err)
	}

	// Create storage middleware for async request saving
	var storageMiddleware *middleware.StorageMiddleware
	if store != nil && cfg.Storage.Enabled {
		storageCfg := middleware.StorageMiddlewareConfig{
			Workers:   cfg.Storage.Workers,
			QueueSize: cfg.Storage.QueueSize,
			CircuitBreaker: middleware.CircuitBreakerConfig{
				Enabled:          cfg.Storage.CircuitBreaker.Enabled,
				MaxFailures:      cfg.Storage.CircuitBreaker.MaxFailures,
				ResetTimeout:     cfg.Storage.CircuitBreaker.ResetTimeout,
				SuccessThreshold: cfg.Storage.CircuitBreaker.SuccessThreshold,
			},
		}
		storageMiddleware, err = middleware.NewStorageMiddlewareWithPool(store, log.Logger, storageCfg)
		if err != nil {
			return fmt.Errorf("creating storage middleware: %w", err)
		}
		defer storageMiddleware.Shutdown(context.Background())
	}

	// Determine server entries: use Servers map if present, otherwise fall back to legacy config
	type serverEntry struct {
		name            string
		serverCfg       config.ServerConfig
		scenariosDir    string
		serviceID       string
		inlineScenarios map[string]config.InlineScenarioEntry
	}
	var serverEntries []serverEntry

	if len(cfg.Servers) > 0 {
		// New multi-server mode
		for name, entry := range cfg.Servers {
			srvCfg := entry.ToServerConfig(cfg.Defaults)
			sid := entry.ServiceID
			if sid == "" {
				sid = cfg.Mock.ServiceID
			}
			serverEntries = append(serverEntries, serverEntry{
				name:            name,
				serverCfg:       srvCfg,
				scenariosDir:    entry.ScenariosDir,
				serviceID:       sid,
				inlineScenarios: entry.Scenarios,
			})
		}
	} else {
		// Legacy single-server mode (backward compatible)
		serverEntries = append(serverEntries, serverEntry{
			name:         "default",
			serverCfg:    cfg.Server,
			scenariosDir: cfg.Mock.ScenariosDir,
			serviceID:    cfg.Mock.ServiceID,
		})
	}

	// Load plugins if enabled
	var pluginLoader *plugin.Loader
	if cfg.Plugin.Enabled {
		var pluginErr error
		pluginLoader, pluginErr = loadPlugins(cfg, log)
		if pluginErr != nil {
			log.Warn("failed to load plugins", "error", pluginErr)
		}
	}
	defer func() {
		if pluginLoader != nil {
			pluginLoader.Close()
		}
	}()

	// Start health check server if enabled
	var healthServer *health.HealthServer
	if cfg.Health.Enabled {
		healthServer, err = health.NewHealthServer(&cfg.Health)
		if err != nil {
			return fmt.Errorf("creating health server: %w", err)
		}
	}

	// Start metrics server if enabled
	if cfg.Metrics.Enabled {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("panic in metrics server", "error", r)
				}
			}()
			if err := startMetricsServer(ctx, cfg, log, metricsRegistry, collector); err != nil {
				log.Error("metrics server error", "error", err)
			}
		}()
	}

	// Start all ICAP servers
	var allServers []*server.ICAPServer
	var firstRegistry storage.ScenarioRegistry
	shutdownTimeout := cfg.Server.ShutdownTimeout
	if cfg.Defaults.ShutdownTimeout != 0 {
		shutdownTimeout = cfg.Defaults.ShutdownTimeout
	}

	for _, entry := range serverEntries {
		// Create scenario registry for this server
		registry := storage.NewShardedScenarioRegistry()
		if firstRegistry == nil {
			firstRegistry = registry
		}

		// Load scenarios
		if entry.scenariosDir != "" {
			entryCfg := &config.Config{
				Mock: config.MockConfig{ScenariosDir: entry.scenariosDir},
			}
			if err := loadScenarios(entryCfg, registry, log); err != nil {
				log.Warn("failed to load scenarios", "server", entry.name, "error", err)
			}
		}

		// Merge inline scenarios (higher priority than file-loaded)
		if len(entry.inlineScenarios) > 0 {
			// Convert config.InlineScenarioEntry to storage.ScenarioEntryV2
			storageScenarios := make(map[string]storage.ScenarioEntryV2, len(entry.inlineScenarios))
			orderedNames := make([]string, 0, len(entry.inlineScenarios))
			for name, e := range entry.inlineScenarios {
				responses := make([]storage.WeightedResponseV2, len(e.Responses))
				for i, r := range e.Responses {
					responses[i] = storage.WeightedResponseV2{
						Weight:     r.Weight,
						Set:        r.Set,
						Status:     r.Status,
						HTTPStatus: r.HTTPStatus,
						Body:       r.Body,
						Delay:      r.Delay,
					}
				}
				storageScenarios[name] = storage.ScenarioEntryV2{
					Method:     e.Method,
					Endpoint:   e.Endpoint,
					Status:     e.Status,
					HTTPStatus: e.HTTPStatus,
					Priority:   e.Priority,
					When:       e.When,
					Set:        e.Set,
					Body:       e.Body,
					BodyFile:   e.BodyFile,
					Delay:      e.Delay,
					Responses:  responses,
				}
				orderedNames = append(orderedNames, name)
			}
			file := &storage.ScenarioFileV2{
				Scenarios: storageScenarios,
			}
			inlineScenarios, convErr := storage.ConvertV2ToScenarios(file, orderedNames)
			if convErr != nil {
				log.Warn("failed to convert inline scenarios", "server", entry.name, "error", convErr)
			} else {
				// Give inline scenarios higher priority (2000+)
				for i, s := range inlineScenarios {
					s.Priority = 2000 - i
				}
				for _, s := range inlineScenarios {
					if addErr := registry.Add(s); addErr != nil {
						log.Warn("failed to add inline scenario", "server", entry.name, "scenario", s.Name, "error", addErr)
					}
				}
			}
		}

		// Create processor chain
		proc, procCleanup := createProcessorChain(cfg, registry, store, collector, log)
		defer procCleanup(context.Background())

		// Create router and register handlers
		rtr := router.NewRouter()
		previewRL, err := registerHandlers(rtr, proc, collector, limiter, storageMiddleware, cfg, log)
		if err != nil {
			return fmt.Errorf("registering handlers for %s: %w", entry.name, err)
		}
		defer func() {
			if previewRL != nil {
				previewRL.Shutdown()
			}
		}()

		// Create connection pool and server
		srvCfg := entry.serverCfg
		pool := server.NewConnectionPool()
		srv, err := server.NewServer(&srvCfg, pool, log.Logger)
		if err != nil {
			return fmt.Errorf("creating server %s: %w", entry.name, err)
		}
		srv.SetRouter(rtr)
		srv.SetMetrics(collector)

		log.Info("starting ICAP server",
			"name", entry.name,
			"host", srvCfg.Host,
			"port", srvCfg.Port,
			"scenarios_dir", entry.scenariosDir,
			"scenarios_count", len(registry.List()),
		)

		if err := srv.Start(ctx); err != nil {
			return fmt.Errorf("starting server %s: %w", entry.name, err)
		}
		allServers = append(allServers, srv)
	}

	// Start health server (after ICAP servers are up)
	if healthServer != nil {
		if firstRegistry != nil {
			healthServer.SetupAPI(firstRegistry)
			healthServer.Checker().SetScenariosCount(len(firstRegistry.List()))
		}
		healthServer.Checker().SetICAPReady(true)
		healthServer.Checker().SetStorageReady(true)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("panic in health server", "error", r)
				}
			}()
			if err := healthServer.Start(ctx); err != nil {
				log.Error("health server error", "error", err)
			}
		}()
	}

	// Wait for shutdown
	<-ctx.Done()

	// Stop all ICAP servers
	for _, srv := range allServers {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := srv.Stop(shutdownCtx); err != nil {
			log.Warn("error stopping server", "error", err)
		}
		cancel()
	}

	// Shutdown storage
	if store != nil {
		if err := store.Close(); err != nil {
			log.Warn("error closing storage", "error", err)
		}
	}

	log.Info("all servers stopped gracefully")
	return nil
}

// createMetricsCollector creates a new metrics collector.
func createMetricsCollector(cfg *config.Config) (*prometheus.Registry, *metrics.Collector, error) {
	// Create a Prometheus registry for metrics collection
	reg := prometheus.NewRegistry()

	collector, err := metrics.NewCollector(reg)
	if err != nil {
		return nil, nil, err
	}
	return reg, collector, nil
}

// createRateLimiter creates a rate limiter based on configuration.
func createRateLimiter(cfg *config.Config) ratelimit.Limiter {
	if !cfg.RateLimit.Enabled {
		return nil
	}

	switch cfg.RateLimit.Algorithm {
	case "sliding_window":
		return ratelimit.NewSlidingWindowLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	case "sharded_token_bucket":
		// Use key-based sharded limiter with global key
		return ratelimit.NewGlobalKeyBasedLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst, ratelimit.GlobalKey)
	default:
		return ratelimit.NewTokenBucketLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	}
}

// createStorageManager creates a storage manager based on configuration.
// P0 FIX: Added metrics collector parameter for rotation monitoring.
func createStorageManager(cfg *config.Config, m *metrics.Collector) (*storage.FileStorage, error) {
	if !cfg.Storage.Enabled {
		return nil, nil
	}

	return storage.NewFileStorage(cfg.Storage, m)
}

// loadScenarios loads scenarios from the configured directory.
func loadScenarios(cfg *config.Config, registry storage.ScenarioRegistry, log *logger.Logger) error {
	// Load scenarios from directory
	log.Info("loading scenarios", "directory", cfg.Mock.ScenariosDir)

	// Find all YAML files in the scenarios directory
	entries, err := os.ReadDir(cfg.Mock.ScenariosDir)
	if err != nil {
		return fmt.Errorf("reading scenarios directory: %w", err)
	}

	loadedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(cfg.Mock.ScenariosDir, name)
		if err := registry.Load(path); err != nil {
			log.Warn("failed to load scenario file", "path", path, "error", err)
			continue
		}
		loadedCount++
	}

	log.Info("loaded scenarios", "count", len(registry.List()))
	return nil
}

// loadPlugins loads plugins from the configured directory.
func loadPlugins(cfg *config.Config, log *logger.Logger) (*plugin.Loader, error) {
	if !cfg.Plugin.Enabled {
		return nil, nil
	}

	log.Info("loading plugins", "directory", cfg.Plugin.Dir)

	// Check if directory exists
	if _, err := os.Stat(cfg.Plugin.Dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("plugin directory does not exist: %s", cfg.Plugin.Dir)
	}

	// Create plugin loader
	loader := plugin.NewLoader()

	// Load all plugins from directory
	if err := loader.LoadDir(cfg.Plugin.Dir); err != nil {
		loader.Close()
		return nil, fmt.Errorf("loading plugins: %w", err)
	}

	log.Info("plugins loaded", "count", len(plugin.List()))
	return loader, nil
}

// createProcessorChain creates the processor chain for handling requests.
func createProcessorChain(
	cfg *config.Config,
	registry storage.ScenarioRegistry,
	store *storage.FileStorage,
	collector *metrics.Collector,
	log *logger.Logger,
) (processor.Processor, func(context.Context)) {
	// Create processors in order: mock -> plugins -> chaos (if enabled) -> echo
	var processors []processor.Processor
	var cleanups []func(context.Context)

	// Add script processor if script mode is configured
	if cfg.Mock.DefaultMode == "script" {
		scriptProc := processor.NewScriptProcessor(registry, log, cfg.Mock.DefaultTimeout)
		processors = append(processors, scriptProc)
		cleanups = append(cleanups, func(ctx context.Context) { _ = scriptProc.Shutdown(ctx) })
		log.Info("script processor added to chain")
	}

	// Create mock processor with scenario registry
	mockProc := processor.NewMockProcessor(registry, log)
	processors = append(processors, mockProc)

	// Add plugin processor if plugins are loaded
	if cfg.Plugin.Enabled && len(plugin.List()) > 0 {
		pluginProcessor := plugin.CreatePluginProcessor()
		processors = append(processors, pluginProcessor)
		log.Info("plugin processor added to chain", "count", len(plugin.List()))
	}

	// Add echo processor for fallback
	echoProc := processor.NewEchoProcessor()
	processors = append(processors, echoProc)

	// Wrap with chaos processor if enabled
	if cfg.Chaos.Enabled {
		chaosConfig := processor.ChaosConfig{
			Enabled:            cfg.Chaos.Enabled,
			ErrorRate:          cfg.Chaos.ErrorRate,
			TimeoutRate:        cfg.Chaos.TimeoutRate,
			MinLatencyMs:       cfg.Chaos.MinLatencyMs,
			MaxLatencyMs:       cfg.Chaos.MaxLatencyMs,
			LatencyRate:        cfg.Chaos.LatencyRate,
			ConnectionDropRate: cfg.Chaos.ConnectionDropRate,
		}
		// Create a chain of non-chaos processors
		baseChain := processor.Chain(processors...)
		chaosProc := processor.NewChaosProcessor(chaosConfig, baseChain, log)
		return chaosProc, func(ctx context.Context) {
			for _, fn := range cleanups {
				fn(ctx)
			}
		}
	}

	// Return the chain of processors
	return processor.Chain(processors...), func(ctx context.Context) {
		for _, fn := range cleanups {
			fn(ctx)
		}
	}
}

// registerHandlers registers all ICAP handlers with the router.
// Returns the preview rate limiter for proper shutdown.
func registerHandlers(
	rtr *router.Router,
	proc processor.Processor,
	collector *metrics.Collector,
	limiter ratelimit.Limiter,
	storageMiddleware *middleware.StorageMiddleware,
	cfg *config.Config,
	log *logger.Logger,
) (*handler.PreviewRateLimiter, error) {
	// PanicRecoveryMiddleware provides protection against panics in handlers.
	// It recovers from panics, logs the error with request context, and returns
	// a 500 Internal Server Error response to prevent server crashes.
	// This is the first middleware in the chain, ensuring all downstream panics are caught.
	panicRecovery := middleware.PanicRecoveryMiddleware(log.Logger)

	// RateLimiterMiddleware checks rate limit before processing requests.
	// If rate limit is exceeded, returns ICAP 429 (Too Many Requests).
	// Applied before the handler to prevent processing over limit requests.
	var rateLimitMiddleware handler.Middleware
	if limiter != nil && cfg.RateLimit.Enabled {
		rateLimitMiddleware = middleware.RateLimiterMiddleware(limiter)
	}

	// StorageMiddleware saves requests to storage asynchronously.
	// Applied after handler execution to capture response status.
	// Uses worker pool to prevent goroutine explosion.
	var storageMW handler.Middleware
	if storageMiddleware != nil {
		storageMW = storageMiddleware.Wrap
	}

	// Build middleware chain: Panic Recovery -> Rate Limiter -> Storage -> Handler
	// Order is important: Rate limiter should be before storage to prevent
	// saving rate-limited requests.
	applyMiddleware := func(h handler.Handler) handler.Handler {
		wrapped := panicRecovery(h)
		if rateLimitMiddleware != nil {
			wrapped = rateLimitMiddleware(wrapped)
		}
		if storageMW != nil {
			wrapped = storageMW(wrapped)
		}
		return wrapped
	}

	// Create preview rate limiter for preview mode requests
	// This prevents DoS attacks by limiting preview requests per client
	var previewRateLimiter *handler.PreviewRateLimiter
	if cfg.Preview.Enabled {
		previewRateLimiter = handler.NewPreviewRateLimiter(
			handler.PreviewRateLimiterConfig{
				Enabled:       cfg.Preview.Enabled,
				MaxRequests:   cfg.Preview.MaxRequests,
				WindowSeconds: cfg.Preview.WindowSeconds,
				MaxClients:    cfg.Preview.MaxClients,
			},
			collector,
			log.Logger,
		)
	}

	// Register REQMOD handler with middleware chain
	// Wraps handler with panic recovery, rate limiting, and storage
	reqmodHandler := handler.NewReqmodHandler(proc, collector, log.Logger, previewRateLimiter)
	if err := rtr.Handle("/reqmod", applyMiddleware(reqmodHandler)); err != nil {
		return nil, fmt.Errorf("registering REQMOD handler: %w", err)
	}

	// Register RESPMOD handler with middleware chain
	// Ensures response modification is protected and logged
	respmodHandler := handler.NewRespmodHandler(proc, collector, log.Logger, previewRateLimiter)
	if err := rtr.Handle("/respmod", applyMiddleware(respmodHandler)); err != nil {
		return nil, fmt.Errorf("registering RESPMOD handler: %w", err)
	}

	// Register OPTIONS handler with middleware chain
	// OPTIONS is lightweight but benefits from rate limiting
	optionsHandler := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		ServiceTag:     "\"icap-mock-dev\"",
		ServiceID:      cfg.Mock.ServiceID,
		Methods:        []string{"REQMOD", "RESPMOD"},
		MaxConnections: cfg.Server.MaxConnections,
		OptionsTTL:     3600 * time.Second,
	})
	if err := rtr.Handle("/options", applyMiddleware(optionsHandler)); err != nil {
		return nil, fmt.Errorf("registering OPTIONS handler: %w", err)
	}

	return previewRateLimiter, nil
}

// startMetricsServer starts the Prometheus metrics HTTP server.
func startMetricsServer(ctx context.Context, cfg *config.Config, log *logger.Logger, reg *prometheus.Registry, collector *metrics.Collector) error {
	// Log metrics availability
	log.Info("starting metrics server",
		"host", cfg.Metrics.Host,
		"port", cfg.Metrics.Port,
		"path", cfg.Metrics.Path,
	)

	// Create HTTP handler for metrics
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.Metrics.Path, func(w http.ResponseWriter, r *http.Request) {
		// Update goroutine count before serving metrics
		collector.SetGoroutines(runtime.NumGoroutine())
		handler.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Metrics.Host, cfg.Metrics.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		// Graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}
