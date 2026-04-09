// Copyright 2026 ICAP Mock

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

var (
	// errStorageDisabled is returned by createStorageManager when storage is not enabled.
	errStorageDisabled = errors.New("storage is disabled")
	// errPluginsDisabled is returned by loadPlugins when plugins are not enabled.
	errPluginsDisabled = errors.New("plugins are disabled")
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
	fmt.Fprintln(w, "Validating configuration...") //nolint:errcheck
	fmt.Fprintln(w)                                //nolint:errcheck

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
		fmt.Fprintln(w, "Configuration validation: PASSED") //nolint:errcheck
		return nil
	}

	return errors.New("configuration validation failed")
}

// printServerConfig prints server configuration for validation.
func printServerConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Server Configuration:\n")                             //nolint:errcheck
	fmt.Fprintf(w, "  host: %s\n", cfg.Server.Host)                       //nolint:errcheck
	fmt.Fprintf(w, "  port: %d\n", cfg.Server.Port)                       //nolint:errcheck
	fmt.Fprintf(w, "  read_timeout: %s\n", cfg.Server.ReadTimeout)        //nolint:errcheck
	fmt.Fprintf(w, "  write_timeout: %s\n", cfg.Server.WriteTimeout)      //nolint:errcheck
	fmt.Fprintf(w, "  max_connections: %d\n", cfg.Server.MaxConnections)  //nolint:errcheck
	fmt.Fprintf(w, "  max_body_size: %d bytes\n", cfg.Server.MaxBodySize) //nolint:errcheck
	fmt.Fprintf(w, "  streaming: %v\n", cfg.Server.Streaming)             //nolint:errcheck
	if cfg.Server.TLS.Enabled {
		fmt.Fprintf(w, "  tls: enabled (cert=%s)\n", cfg.Server.TLS.CertFile) //nolint:errcheck
	} else {
		fmt.Fprintf(w, "  tls: disabled\n") //nolint:errcheck
	}
	fmt.Fprintln(w) //nolint:errcheck
}

// printLoggingConfig prints logging configuration for validation.
func printLoggingConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Logging Configuration:\n")                    //nolint:errcheck
	fmt.Fprintf(w, "  level: %s\n", cfg.Logging.Level)            //nolint:errcheck
	fmt.Fprintf(w, "  format: %s\n", cfg.Logging.Format)          //nolint:errcheck
	fmt.Fprintf(w, "  output: %s\n", cfg.Logging.Output)          //nolint:errcheck
	fmt.Fprintf(w, "  max_size: %d MB\n", cfg.Logging.MaxSize)    //nolint:errcheck
	fmt.Fprintf(w, "  max_backups: %d\n", cfg.Logging.MaxBackups) //nolint:errcheck
	fmt.Fprintf(w, "  max_age: %d days\n", cfg.Logging.MaxAge)    //nolint:errcheck
	fmt.Fprintln(w)                                               //nolint:errcheck
}

// printMetricsConfig prints metrics configuration for validation.
func printMetricsConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Metrics Configuration:\n")             //nolint:errcheck
	fmt.Fprintf(w, "  enabled: %v\n", cfg.Metrics.Enabled) //nolint:errcheck
	if cfg.Metrics.Enabled {
		fmt.Fprintf(w, "  host: %s\n", cfg.Metrics.Host) //nolint:errcheck
		fmt.Fprintf(w, "  port: %d\n", cfg.Metrics.Port) //nolint:errcheck
		fmt.Fprintf(w, "  path: %s\n", cfg.Metrics.Path) //nolint:errcheck
	}
	fmt.Fprintln(w) //nolint:errcheck
}

// printMockConfig prints mock configuration for validation.
func printMockConfig(w io.Writer, cfg *config.Config, allPassed *bool) {
	fmt.Fprintf(w, "Mock Configuration:\n")                            //nolint:errcheck
	fmt.Fprintf(w, "  default_mode: %s\n", cfg.Mock.DefaultMode)       //nolint:errcheck
	fmt.Fprintf(w, "  scenarios_dir: %s\n", cfg.Mock.ScenariosDir)     //nolint:errcheck
	fmt.Fprintf(w, "  default_timeout: %s\n", cfg.Mock.DefaultTimeout) //nolint:errcheck

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
				fmt.Fprintf(w, "  scenarios loaded: %d files found\n", scenarioCount) //nolint:errcheck
			}
		} else {
			fmt.Fprintf(w, "  WARNING: scenarios directory not found: %s\n", cfg.Mock.ScenariosDir) //nolint:errcheck
			*allPassed = false
		}
	}
	fmt.Fprintln(w) //nolint:errcheck
}

// printChaosConfig prints chaos configuration for validation.
func printChaosConfig(w io.Writer, cfg *config.Config) {
	if cfg.Chaos.Enabled {
		fmt.Fprintf(w, "Chaos Configuration:\n")                                                //nolint:errcheck
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Chaos.Enabled)                                    //nolint:errcheck
		fmt.Fprintf(w, "  error_rate: %.2f\n", cfg.Chaos.ErrorRate)                             //nolint:errcheck
		fmt.Fprintf(w, "  timeout_rate: %.2f\n", cfg.Chaos.TimeoutRate)                         //nolint:errcheck
		fmt.Fprintf(w, "  latency: %d-%d ms\n", cfg.Chaos.MinLatencyMs, cfg.Chaos.MaxLatencyMs) //nolint:errcheck
		fmt.Fprintf(w, "  connection_drop_rate: %.2f\n", cfg.Chaos.ConnectionDropRate)          //nolint:errcheck
		fmt.Fprintln(w)                                                                         //nolint:errcheck
	}
}

// printStorageConfig prints storage configuration for validation.
func printStorageConfig(w io.Writer, cfg *config.Config) {
	if cfg.Storage.Enabled {
		fmt.Fprintf(w, "Storage Configuration:\n")                               //nolint:errcheck
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Storage.Enabled)                   //nolint:errcheck
		fmt.Fprintf(w, "  requests_dir: %s\n", cfg.Storage.RequestsDir)          //nolint:errcheck
		fmt.Fprintf(w, "  max_file_size: %d bytes\n", cfg.Storage.MaxFileSize)   //nolint:errcheck
		fmt.Fprintf(w, "  rotate_after: %d requests\n", cfg.Storage.RotateAfter) //nolint:errcheck
		fmt.Fprintln(w)                                                          //nolint:errcheck
	}
}

// printRateLimitConfig prints rate limit configuration for validation.
func printRateLimitConfig(w io.Writer, cfg *config.Config) {
	if cfg.RateLimit.Enabled {
		fmt.Fprintf(w, "Rate Limit Configuration:\n")                                    //nolint:errcheck
		fmt.Fprintf(w, "  enabled: %v\n", cfg.RateLimit.Enabled)                         //nolint:errcheck
		fmt.Fprintf(w, "  requests_per_second: %.0f\n", cfg.RateLimit.RequestsPerSecond) //nolint:errcheck
		fmt.Fprintf(w, "  burst: %d\n", cfg.RateLimit.Burst)                             //nolint:errcheck
		fmt.Fprintf(w, "  algorithm: %s\n", cfg.RateLimit.Algorithm)                     //nolint:errcheck
		fmt.Fprintln(w)                                                                  //nolint:errcheck
	}
}

// printHealthConfig prints health configuration for validation.
func printHealthConfig(w io.Writer, cfg *config.Config) {
	fmt.Fprintf(w, "Health Configuration:\n")             //nolint:errcheck
	fmt.Fprintf(w, "  enabled: %v\n", cfg.Health.Enabled) //nolint:errcheck
	if cfg.Health.Enabled {
		fmt.Fprintf(w, "  port: %d\n", cfg.Health.Port)              //nolint:errcheck
		fmt.Fprintf(w, "  health_path: %s\n", cfg.Health.HealthPath) //nolint:errcheck
		fmt.Fprintf(w, "  ready_path: %s\n", cfg.Health.ReadyPath)   //nolint:errcheck
	}
	fmt.Fprintln(w) //nolint:errcheck
}

// printPluginConfig prints plugin configuration for validation.
func printPluginConfig(w io.Writer, cfg *config.Config) {
	if cfg.Plugin.Enabled {
		fmt.Fprintf(w, "Plugin Configuration:\n")             //nolint:errcheck
		fmt.Fprintf(w, "  enabled: %v\n", cfg.Plugin.Enabled) //nolint:errcheck
		fmt.Fprintf(w, "  dir: %s\n", cfg.Plugin.Dir)         //nolint:errcheck
		fmt.Fprintln(w)                                       //nolint:errcheck
	}
}

// Run starts the ICAP server with the given configuration.
// The provided context controls the server lifecycle — canceling it triggers graceful shutdown.
func Run(ctx context.Context, cfg *config.Config) error {
	// Initialize logger
	log, err := logger.New(cfg.Logging)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	defer log.Close() //nolint:errcheck

	// Create metrics collector
	metricsRegistry, collector, err := createMetricsCollector()
	if err != nil {
		return fmt.Errorf("creating metrics collector: %w", err)
	}

	// Create rate limiter (used for request throttling)
	limiter := createRateLimiter(cfg)

	// Create storage manager and middleware
	store, storageMiddleware, err := createStorageStack(cfg, collector, log)
	if err != nil {
		return err
	}
	if storageMiddleware != nil {
		defer storageMiddleware.Shutdown(context.Background()) //nolint:errcheck,contextcheck // shutdown uses fresh context because parent is canceled
	}

	// Determine server entries: use Servers map if present, otherwise fall back to legacy config
	serverEntries := buildServerEntries(cfg)

	// Load plugins if enabled
	pluginLoader := tryLoadPlugins(cfg, log)
	defer func() {
		if pluginLoader != nil {
			_ = pluginLoader.Close()
		}
	}()

	// Start health check server if enabled
	healthServer, err := createHealthServer(cfg)
	if err != nil {
		return err
	}

	// Start metrics server if enabled
	launchMetricsServer(ctx, cfg, log, metricsRegistry, collector)

	// Start all ICAP servers
	allServers, firstRegistry, err := startAllServers(ctx, cfg, serverEntries, collector, limiter, storageMiddleware, log)
	if err != nil {
		return err
	}

	// Start health server (after ICAP servers are up)
	startHealthServer(ctx, cfg, healthServer, firstRegistry, log)

	// Wait for shutdown
	<-ctx.Done()

	// Graceful shutdown
	shutdownTimeout := cfg.Server.ShutdownTimeout
	if cfg.Defaults.ShutdownTimeout != 0 {
		shutdownTimeout = cfg.Defaults.ShutdownTimeout
	}
	for _, srv := range allServers {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := srv.Stop(shutdownCtx); err != nil { //nolint:contextcheck // shutdown uses fresh context because parent is canceled
			log.Warn("error stopping server", "error", err)
		}
		cancel()
	}

	if store != nil {
		if err := store.Close(); err != nil {
			log.Warn("error closing storage", "error", err)
		}
	}

	log.Info("all servers stopped gracefully")
	return nil
}

// startAllServers creates and starts all ICAP servers from the server entries.
func startAllServers(
	ctx context.Context,
	cfg *config.Config,
	serverEntries []serverEntry,
	collector *metrics.Collector,
	limiter ratelimit.Limiter,
	storageMiddleware *middleware.StorageMiddleware,
	log *logger.Logger,
) ([]*server.ICAPServer, storage.ScenarioRegistry, error) {
	var allServers []*server.ICAPServer
	var firstRegistry storage.ScenarioRegistry

	for _, entry := range serverEntries {
		registry := storage.NewShardedScenarioRegistry()
		if firstRegistry == nil {
			firstRegistry = registry
		}

		if entry.scenariosDir != "" {
			entryCfg := &config.Config{
				Mock: config.MockConfig{ScenariosDir: entry.scenariosDir},
			}
			if err := loadScenarios(entryCfg, registry, log); err != nil {
				log.Warn("failed to load scenarios", "server", entry.name, "error", err)
			}
		}

		if len(entry.inlineScenarios) > 0 {
			loadInlineScenarios(entry, registry, log)
		}

		proc, _ := createProcessorChain(cfg, registry, log)

		rtr := router.NewRouter()
		rtr.SetLogger(log.Logger)
		if err := registerHandlers(rtr, proc, collector, limiter, storageMiddleware, cfg, log, registry); err != nil {
			return nil, nil, fmt.Errorf("registering handlers for %s: %w", entry.name, err)
		}

		srvCfg := entry.serverCfg
		pool := server.NewConnectionPool()
		srv, err := server.NewServer(&srvCfg, pool, log.Logger)
		if err != nil {
			return nil, nil, fmt.Errorf("creating server %s: %w", entry.name, err)
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
			return nil, nil, fmt.Errorf("starting server %s: %w", entry.name, err)
		}
		allServers = append(allServers, srv)
	}

	// Reset goroutine baselines after all servers have started,
	// so that goroutines from other servers are not counted as leaks.
	if len(allServers) > 1 {
		for _, srv := range allServers {
			srv.ResetGoroutineBaseline()
		}
	}

	return allServers, firstRegistry, nil
}

// startHealthServer starts the health check server if enabled and configured.
func startHealthServer(ctx context.Context, _ *config.Config, healthServer *health.Server, firstRegistry storage.ScenarioRegistry, log *logger.Logger) {
	if healthServer == nil {
		return
	}
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

// serverEntry represents a single ICAP server configuration.
type serverEntry struct {
	inlineScenarios map[string]config.InlineScenarioEntry
	name            string
	scenariosDir    string
	serviceID       string
	serverCfg       config.ServerConfig
}

// buildServerEntries determines server entries from config.
func buildServerEntries(cfg *config.Config) []serverEntry {
	if len(cfg.Servers) > 0 {
		entries := make([]serverEntry, 0, len(cfg.Servers))
		for name, entry := range cfg.Servers {
			srvCfg := entry.ToServerConfig(cfg.Defaults)
			sid := entry.ServiceID
			if sid == "" {
				sid = cfg.Mock.ServiceID
			}
			entries = append(entries, serverEntry{
				name:            name,
				serverCfg:       srvCfg,
				scenariosDir:    entry.ScenariosDir,
				serviceID:       sid,
				inlineScenarios: entry.Scenarios,
			})
		}
		return entries
	}
	return []serverEntry{{
		name:         "default",
		serverCfg:    cfg.Server,
		scenariosDir: cfg.Mock.ScenariosDir,
		serviceID:    cfg.Mock.ServiceID,
	}}
}

// loadInlineScenarios converts inline scenario config entries and adds them to the registry.
func loadInlineScenarios(entry serverEntry, registry storage.ScenarioRegistry, log *logger.Logger) {
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
		return
	}
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

// tryLoadPlugins loads plugins if enabled, logging any errors.
func tryLoadPlugins(cfg *config.Config, log *logger.Logger) *plugin.DynamicLoader {
	if !cfg.Plugin.Enabled {
		return nil
	}
	pluginLoader, pluginErr := loadPlugins(cfg, log)
	if pluginErr != nil {
		log.Warn("failed to load plugins", "error", pluginErr)
	}
	return pluginLoader
}

// createHealthServer creates a health server if health checks are enabled.
func createHealthServer(cfg *config.Config) (*health.Server, error) {
	if !cfg.Health.Enabled {
		return nil, nil //nolint:nilnil // nil server signals health is disabled; caller checks for nil
	}
	srv, err := health.NewServer(&cfg.Health)
	if err != nil {
		return nil, fmt.Errorf("creating health server: %w", err)
	}
	return srv, nil
}

// launchMetricsServer starts the metrics server in a goroutine if enabled.
func launchMetricsServer(ctx context.Context, cfg *config.Config, log *logger.Logger, metricsRegistry *prometheus.Registry, collector *metrics.Collector) {
	if !cfg.Metrics.Enabled {
		return
	}
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

// createStorageStack creates the storage manager and middleware.
func createStorageStack(cfg *config.Config, collector *metrics.Collector, log *logger.Logger) (*storage.FileStorage, *middleware.StorageMiddleware, error) {
	store, err := createStorageManager(cfg, collector)
	if err != nil && !errors.Is(err, errStorageDisabled) {
		return nil, nil, fmt.Errorf("creating storage manager: %w", err)
	}
	if store == nil || !cfg.Storage.Enabled {
		return store, nil, nil
	}
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
	sm, err := middleware.NewStorageMiddlewareWithPool(store, log.Logger, storageCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("creating storage middleware: %w", err)
	}
	return store, sm, nil
}

// createMetricsCollector creates a new metrics collector.
func createMetricsCollector() (*prometheus.Registry, *metrics.Collector, error) {
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
		return nil, errStorageDisabled
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
func loadPlugins(cfg *config.Config, log *logger.Logger) (*plugin.DynamicLoader, error) {
	if !cfg.Plugin.Enabled {
		return nil, errPluginsDisabled
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
		_ = loader.Close()
		return nil, fmt.Errorf("loading plugins: %w", err)
	}

	log.Info("plugins loaded", "count", len(plugin.List()))
	return loader, nil
}

// createProcessorChain creates the processor chain for handling requests.
func createProcessorChain(
	cfg *config.Config,
	registry storage.ScenarioRegistry,
	log *logger.Logger,
) (proc processor.Processor, cleanup func(context.Context)) {
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

// buildMiddlewareChain creates the middleware chain function.
// Order: Panic Recovery -> Rate Limiter -> Storage -> Handler.
func buildMiddlewareChain(
	log *logger.Logger,
	limiter ratelimit.Limiter,
	storageMiddleware *middleware.StorageMiddleware,
	cfg *config.Config,
) func(handler.Handler) handler.Handler {
	panicRecovery := middleware.PanicRecoveryMiddleware(log.Logger)

	var rateLimitMiddleware handler.Middleware
	if limiter != nil && cfg.RateLimit.Enabled {
		rateLimitMiddleware = middleware.RateLimiterMiddleware(limiter)
	}

	var storageMW handler.Middleware
	if storageMiddleware != nil {
		storageMW = storageMiddleware.Wrap
	}

	return func(h handler.Handler) handler.Handler {
		wrapped := panicRecovery(h)
		if rateLimitMiddleware != nil {
			wrapped = rateLimitMiddleware(wrapped)
		}
		if storageMW != nil {
			wrapped = storageMW(wrapped)
		}
		return wrapped
	}
}

// createHandlers creates REQMOD, RESPMOD, and OPTIONS handlers with middleware applied.
func createHandlers(
	proc processor.Processor,
	collector *metrics.Collector,
	cfg *config.Config,
	log *logger.Logger,
	applyMiddleware func(handler.Handler) handler.Handler,
) (wrappedReqmod, wrappedRespmod, wrappedOptions handler.Handler) {
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

	reqmodHandler := handler.NewReqmodHandler(proc, collector, log.Logger, previewRateLimiter)
	respmodHandler := handler.NewRespmodHandler(proc, collector, log.Logger, previewRateLimiter)
	optionsHandler := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
		ServiceTag:     "\"icap-mock-dev\"",
		ServiceID:      cfg.Mock.ServiceID,
		Methods:        []string{"REQMOD", "RESPMOD"},
		MaxConnections: cfg.Server.MaxConnections,
		OptionsTTL:     3600 * time.Second,
	})

	return applyMiddleware(reqmodHandler), applyMiddleware(respmodHandler), applyMiddleware(optionsHandler)
}

// registerHandlers registers all ICAP handlers with the router.
func registerHandlers(
	rtr *router.Router,
	proc processor.Processor,
	collector *metrics.Collector,
	limiter ratelimit.Limiter,
	storageMiddleware *middleware.StorageMiddleware,
	cfg *config.Config,
	log *logger.Logger,
	registry storage.ScenarioRegistry,
) error {
	applyMiddleware := buildMiddlewareChain(log, limiter, storageMiddleware, cfg)
	wrappedReqmod, wrappedRespmod, wrappedOptions := createHandlers(proc, collector, cfg, log, applyMiddleware)

	// Register default endpoints
	defaultEndpoints := []string{"/reqmod", "/respmod", "/options"}
	handlers := map[string]handler.Handler{
		"/reqmod":  wrappedReqmod,
		"/respmod": wrappedRespmod,
		"/options": wrappedOptions,
	}
	for _, ep := range defaultEndpoints {
		if err := rtr.Handle(ep, handlers[ep]); err != nil {
			return fmt.Errorf("registering handler for %s: %w", ep, err)
		}
	}

	// Register custom endpoints from scenarios
	if registry != nil {
		registered := make(map[string]bool)
		for _, ep := range defaultEndpoints {
			registered[ep] = true
		}
		for _, s := range registry.List() {
			ep := s.Match.Path
			if ep == "" || registered[ep] {
				continue
			}
			registered[ep] = true
			h := wrappedRespmod
			if s.Match.Method == "REQMOD" {
				h = wrappedReqmod
			}
			if err := rtr.Handle(ep, h); err != nil {
				return fmt.Errorf("registering handler for custom endpoint %s: %w", ep, err)
			}
			log.Info("registered custom endpoint from scenario", "endpoint", ep, "method", s.Match.Method)
		}
	}

	return nil
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
	metricsHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.Metrics.Path, func(w http.ResponseWriter, r *http.Request) {
		// Update goroutine count before serving metrics
		collector.SetGoroutines(runtime.NumGoroutine())
		metricsHandler.ServeHTTP(w, r)
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
		return srv.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown uses fresh context because parent is canceled
	case err := <-errChan:
		return err
	}
}
