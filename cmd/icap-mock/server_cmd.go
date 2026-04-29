// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// ServerCommand handles the server subcommand.
type ServerCommand struct {
	fs                      *flag.FlagSet
	TUIRunner               func(cfg interface{}) error
	writeTimeout            string
	shutdownTimeout         string
	readyPath               string
	healthPath              string
	rateLimitAlgo           string
	host                    string
	storageDir              string
	readTimeout             string
	metricsHost             string
	pluginDir               string
	mockTimeout             string
	configFile              string
	scenariosDir            string
	mockMode                string
	tlsCert                 string
	tlsKey                  string
	logLevel                string
	logFormat               string
	logOutput               string
	metricsPath             string
	maxBodySize             int64
	rateLimitRPS            float64
	replaySpeed             float64
	logMaxBackups           int
	metricsPort             int
	logMaxSize              int
	healthPort              int
	rateLimitBurst          int
	maxConns                int
	storageRotate           int
	chaosErrorRate          float64
	chaosTimeoutRate        float64
	chaosMinLatencyMs       int
	chaosMaxLatencyMs       int
	chaosConnectionDropRate float64
	logMaxAge               int
	port                    int
	storageMaxSize          int64
	storageEnabled          bool
	rateLimitEnabled        bool
	chaosEnabled            bool
	streaming               bool
	debugFlag               bool
	healthEnabled           bool
	tlsEnable               bool
	tuiFlag                 bool
	versionFlag             bool
	replayEnabled           bool
	metricsEnabled          bool
	pluginEnabled           bool
	validateFlag            bool
	pprofEnabled            bool
}

// NewServerCommand creates a new server command.
func NewServerCommand() *ServerCommand {
	cmd := &ServerCommand{
		fs: flag.NewFlagSet("server", flag.ContinueOnError),
	}

	// Register global flags
	cmd.fs.BoolVar(&cmd.versionFlag, "version", false, "Print version information and exit")
	cmd.fs.BoolVar(&cmd.validateFlag, "validate", false, "Validate configuration and exit (dry-run mode)")
	cmd.fs.BoolVar(&cmd.tuiFlag, "tui", false, "Launch Terminal User Interface (TUI)")
	cmd.fs.BoolVar(&cmd.debugFlag, "debug", false, "Enable debug logging (sets log level to debug)")
	cmd.fs.BoolVar(&cmd.debugFlag, "d", false, "Enable debug logging (shorthand)")
	cmd.fs.StringVar(&cmd.configFile, "config", "", "Path to configuration file (YAML or JSON)")
	cmd.fs.StringVar(&cmd.configFile, "c", "", "Path to configuration file (shorthand)")

	// Register server flags
	cmd.fs.StringVar(&cmd.host, "server.host", "0.0.0.0", "Server host address")
	cmd.fs.IntVar(&cmd.port, "server.port", 1344, "Server port")
	cmd.fs.IntVar(&cmd.port, "p", 1344, "Server port (shorthand)")
	cmd.fs.StringVar(&cmd.readTimeout, "server.read-timeout", "", "Read timeout (from config, e.g. 30s)")
	cmd.fs.StringVar(&cmd.writeTimeout, "server.write-timeout", "", "Write timeout (from config, e.g. 30s)")
	cmd.fs.StringVar(&cmd.shutdownTimeout, "server.shutdown-timeout", "", "Graceful shutdown timeout (from config, e.g. 30s)")
	cmd.fs.IntVar(&cmd.maxConns, "server.max-connections", 0, "Maximum concurrent connections (from config)")
	cmd.fs.Int64Var(&cmd.maxBodySize, "server.max-body-size", 0, "Maximum request body size (e.g., 10485760 = 10MB), 0=unlimited (from config)")
	cmd.fs.BoolVar(&cmd.streaming, "server.streaming", false, "Enable streaming mode")

	// Register TLS flags
	cmd.fs.BoolVar(&cmd.tlsEnable, "server.tls.enabled", false, "Enable TLS")
	cmd.fs.StringVar(&cmd.tlsCert, "server.tls.cert", "", "TLS certificate file path")
	cmd.fs.StringVar(&cmd.tlsKey, "server.tls.key", "", "TLS private key file path")

	// Register logging flags
	cmd.fs.StringVar(&cmd.logLevel, "logging.level", "", "Log level: debug, info, warn, error (from config)")
	cmd.fs.StringVar(&cmd.logLevel, "l", "", "Log level (shorthand)")
	cmd.fs.StringVar(&cmd.logFormat, "logging.format", "", "Log format: json, text (from config)")
	cmd.fs.StringVar(&cmd.logOutput, "logging.output", "", "Log output: stdout, stderr, or file path (from config)")
	cmd.fs.IntVar(&cmd.logMaxSize, "logging.max-size", 0, "Maximum log file size in MB (from config)")
	cmd.fs.IntVar(&cmd.logMaxBackups, "logging.max-backups", 0, "Maximum number of old log files (from config)")
	cmd.fs.IntVar(&cmd.logMaxAge, "logging.max-age", 0, "Maximum days to retain old log files (from config)")

	// Register metrics flags
	cmd.fs.BoolVar(&cmd.metricsEnabled, "metrics.enabled", false, "Enable Prometheus metrics endpoint")
	cmd.fs.StringVar(&cmd.metricsHost, "metrics.host", "", "Metrics server host (from config)")
	cmd.fs.IntVar(&cmd.metricsPort, "metrics.port", 0, "Metrics server port (from config)")
	cmd.fs.StringVar(&cmd.metricsPath, "metrics.path", "", "Metrics endpoint path (from config)")

	// Register mock flags
	cmd.fs.StringVar(&cmd.mockMode, "mock.mode", "", "Processing mode: echo, mock, script (from config)")
	cmd.fs.StringVar(&cmd.scenariosDir, "mock.scenarios-dir", "", "Directory containing scenario files (from config)")
	cmd.fs.StringVar(&cmd.mockTimeout, "mock.timeout", "", "Default request timeout (from config)")

	// Register chaos flags
	cmd.fs.BoolVar(&cmd.chaosEnabled, "chaos.enabled", false, "Enable chaos engineering")
	cmd.fs.Float64Var(&cmd.chaosErrorRate, "chaos.error-rate", 0, "Error injection rate (from config, 0.0-1.0)")
	cmd.fs.Float64Var(&cmd.chaosTimeoutRate, "chaos.timeout-rate", 0, "Timeout injection rate (from config, 0.0-1.0)")
	cmd.fs.IntVar(&cmd.chaosMinLatencyMs, "chaos.min-latency-ms", 0, "Minimum latency in milliseconds (from config)")
	cmd.fs.IntVar(&cmd.chaosMaxLatencyMs, "chaos.max-latency-ms", 0, "Maximum latency in milliseconds (from config)")
	cmd.fs.Float64Var(&cmd.chaosConnectionDropRate, "chaos.connection-drop-rate", 0, "Connection drop rate (from config, 0.0-1.0)")

	// Register storage flags
	cmd.fs.BoolVar(&cmd.storageEnabled, "storage.enabled", false, "Enable request storage")
	cmd.fs.StringVar(&cmd.storageDir, "storage.dir", "", "Directory for stored requests (from config)")
	cmd.fs.Int64Var(&cmd.storageMaxSize, "storage.max-size", 0, "Maximum storage file size in bytes (from config)")
	cmd.fs.IntVar(&cmd.storageRotate, "storage.rotate", 0, "Rotate after N requests (from config)")

	// Register rate limit flags
	cmd.fs.BoolVar(&cmd.rateLimitEnabled, "rate-limit.enabled", false, "Enable rate limiting")
	cmd.fs.Float64Var(&cmd.rateLimitRPS, "rate-limit.rps", 0, "Requests per second (from config)")
	cmd.fs.IntVar(&cmd.rateLimitBurst, "rate-limit.burst", 0, "Burst capacity (from config)")
	cmd.fs.StringVar(&cmd.rateLimitAlgo, "rate-limit.algorithm", "", "Algorithm: token_bucket, sliding_window, sharded_token_bucket (from config)")

	// Register health flags
	cmd.fs.BoolVar(&cmd.healthEnabled, "health.enabled", false, "Enable health check endpoints")
	cmd.fs.IntVar(&cmd.healthPort, "health.port", 0, "Health check server port (from config)")
	cmd.fs.StringVar(&cmd.healthPath, "health.path", "", "Health endpoint path (from config)")
	cmd.fs.StringVar(&cmd.readyPath, "health.ready-path", "", "Readiness endpoint path (from config)")

	// Register replay flags
	cmd.fs.BoolVar(&cmd.replayEnabled, "replay.enabled", false, "Enable replay mode")
	cmd.fs.Float64Var(&cmd.replaySpeed, "replay.speed", 0, "Replay speed multiplier (from config)")

	// Register plugin flags
	cmd.fs.BoolVar(&cmd.pluginEnabled, "plugin.enabled", false, "Enable plugin system")
	cmd.fs.StringVar(&cmd.pluginDir, "plugin.dir", "", "Directory containing plugin .so files (from config)")

	// Register pprof flags
	cmd.fs.BoolVar(&cmd.pprofEnabled, "pprof.enabled", false, "Enable pprof profiling endpoints")

	// Register kebab-case aliases for dot-notation flags (backward-compatible)
	cmd.fs.StringVar(&cmd.host, "server-host", "", "Alias for --server.host")
	cmd.fs.IntVar(&cmd.port, "server-port", 0, "Alias for --server.port")
	cmd.fs.StringVar(&cmd.readTimeout, "server-read-timeout", "", "Alias for --server.read-timeout")
	cmd.fs.StringVar(&cmd.writeTimeout, "server-write-timeout", "", "Alias for --server.write-timeout")
	cmd.fs.StringVar(&cmd.shutdownTimeout, "server-shutdown-timeout", "", "Alias for --server.shutdown-timeout")
	cmd.fs.IntVar(&cmd.maxConns, "server-max-connections", 0, "Alias for --server.max-connections")
	cmd.fs.Int64Var(&cmd.maxBodySize, "server-max-body-size", 0, "Alias for --server.max-body-size")
	cmd.fs.BoolVar(&cmd.streaming, "server-streaming", false, "Alias for --server.streaming")
	cmd.fs.BoolVar(&cmd.tlsEnable, "server-tls-enabled", false, "Alias for --server.tls.enabled")
	cmd.fs.StringVar(&cmd.tlsCert, "server-tls-cert", "", "Alias for --server.tls.cert")
	cmd.fs.StringVar(&cmd.tlsKey, "server-tls-key", "", "Alias for --server.tls.key")
	cmd.fs.StringVar(&cmd.logLevel, "logging-level", "", "Alias for --logging.level")
	cmd.fs.StringVar(&cmd.logFormat, "logging-format", "", "Alias for --logging.format")
	cmd.fs.StringVar(&cmd.logOutput, "logging-output", "", "Alias for --logging.output")
	cmd.fs.IntVar(&cmd.logMaxSize, "logging-max-size", 0, "Alias for --logging.max-size")
	cmd.fs.IntVar(&cmd.logMaxBackups, "logging-max-backups", 0, "Alias for --logging.max-backups")
	cmd.fs.IntVar(&cmd.logMaxAge, "logging-max-age", 0, "Alias for --logging.max-age")
	cmd.fs.BoolVar(&cmd.metricsEnabled, "metrics-enabled", false, "Alias for --metrics.enabled")
	cmd.fs.StringVar(&cmd.metricsHost, "metrics-host", "", "Alias for --metrics.host")
	cmd.fs.IntVar(&cmd.metricsPort, "metrics-port", 0, "Alias for --metrics.port")
	cmd.fs.StringVar(&cmd.metricsPath, "metrics-path", "", "Alias for --metrics.path")
	cmd.fs.StringVar(&cmd.mockMode, "mock-mode", "", "Alias for --mock.mode")
	cmd.fs.StringVar(&cmd.scenariosDir, "mock-scenarios-dir", "", "Alias for --mock.scenarios-dir")
	cmd.fs.StringVar(&cmd.mockTimeout, "mock-timeout", "", "Alias for --mock.timeout")
	cmd.fs.BoolVar(&cmd.chaosEnabled, "chaos-enabled", false, "Alias for --chaos.enabled")
	cmd.fs.Float64Var(&cmd.chaosErrorRate, "chaos-error-rate", 0, "Alias for --chaos.error-rate")
	cmd.fs.Float64Var(&cmd.chaosTimeoutRate, "chaos-timeout-rate", 0, "Alias for --chaos.timeout-rate")
	cmd.fs.IntVar(&cmd.chaosMinLatencyMs, "chaos-min-latency-ms", 0, "Alias for --chaos.min-latency-ms")
	cmd.fs.IntVar(&cmd.chaosMaxLatencyMs, "chaos-max-latency-ms", 0, "Alias for --chaos.max-latency-ms")
	cmd.fs.Float64Var(&cmd.chaosConnectionDropRate, "chaos-connection-drop-rate", 0, "Alias for --chaos.connection-drop-rate")
	cmd.fs.BoolVar(&cmd.storageEnabled, "storage-enabled", false, "Alias for --storage.enabled")
	cmd.fs.StringVar(&cmd.storageDir, "storage-dir", "", "Alias for --storage.dir")
	cmd.fs.Int64Var(&cmd.storageMaxSize, "storage-max-size", 0, "Alias for --storage.max-size")
	cmd.fs.IntVar(&cmd.storageRotate, "storage-rotate", 0, "Alias for --storage.rotate")
	cmd.fs.BoolVar(&cmd.rateLimitEnabled, "rate-limit-enabled", false, "Alias for --rate-limit.enabled")
	cmd.fs.Float64Var(&cmd.rateLimitRPS, "rate-limit-rps", 0, "Alias for --rate-limit.rps")
	cmd.fs.IntVar(&cmd.rateLimitBurst, "rate-limit-burst", 0, "Alias for --rate-limit.burst")
	cmd.fs.StringVar(&cmd.rateLimitAlgo, "rate-limit-algorithm", "", "Alias for --rate-limit.algorithm")
	cmd.fs.BoolVar(&cmd.healthEnabled, "health-enabled", false, "Alias for --health.enabled")
	cmd.fs.IntVar(&cmd.healthPort, "health-port", 0, "Alias for --health.port")
	cmd.fs.StringVar(&cmd.healthPath, "health-path", "", "Alias for --health.path")
	cmd.fs.StringVar(&cmd.readyPath, "health-ready-path", "", "Alias for --health.ready-path")
	cmd.fs.BoolVar(&cmd.replayEnabled, "replay-enabled", false, "Alias for --replay.enabled")
	cmd.fs.Float64Var(&cmd.replaySpeed, "replay-speed", 0, "Alias for --replay.speed")
	cmd.fs.BoolVar(&cmd.pluginEnabled, "plugin-enabled", false, "Alias for --plugin.enabled")
	cmd.fs.StringVar(&cmd.pluginDir, "plugin-dir", "", "Alias for --plugin.dir")
	cmd.fs.BoolVar(&cmd.pprofEnabled, "pprof-enabled", false, "Alias for --pprof.enabled")

	return cmd
}

// Name returns the command name.
func (c *ServerCommand) Name() string {
	return "server" //nolint:goconst
}

// Description returns a short description of the command.
func (c *ServerCommand) Description() string {
	return "Start the ICAP mock server"
}

// Parse parses the command arguments.
func (c *ServerCommand) Parse(args []string) error {
	return c.fs.Parse(args)
}

// Run executes the server command.
func (c *ServerCommand) Run(ctx context.Context) error {
	// Handle version flag
	if c.versionFlag {
		PrintVersion()
		return nil
	}

	// Handle TUI flag
	if c.tuiFlag {
		if c.TUIRunner != nil {
			return c.TUIRunner(nil)
		}
		return fmt.Errorf("TUI is not available: rebuild with TUI support or run without --tui flag")
	}

	// Load configuration
	cfg, err := c.loadConfiguration()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// Apply CLI overrides
	c.applyOverrides(cfg)
	cfg.SourcePath = c.configFile

	// Validate configuration
	validator := config.NewValidator()
	validationErrors := validator.Validate(cfg)
	if len(validationErrors) > 0 {
		if c.validateFlag {
			PrintValidationErrors(os.Stdout, validationErrors)
		}
		msgs := ""
		for _, e := range validationErrors {
			msgs += "\n  - " + e.Error()
		}
		return fmt.Errorf("configuration validation failed:%s", msgs)
	}

	// Handle validate-only mode
	if c.validateFlag {
		return RunValidateMode(os.Stdout, cfg)
	}

	// Run the server
	return RunWithContext(ctx, cfg)
}

// Usage prints the command usage.
func (c *ServerCommand) Usage() {
	fmt.Fprintf(os.Stderr, `Usage: icap-mock server [options]

Start the ICAP mock server.

Examples:
  icap-mock server                                    Start with defaults
  icap-mock server -c config.yaml                     Start with config file
  icap-mock server -p 1345 --mock.mode echo           Custom port, echo mode
  icap-mock server --metrics.enabled --health.enabled  Enable metrics & health
  icap-mock server --validate -c config.yaml          Validate config only

`)
	c.printGroupedFlags()
}

// flagGroup defines a named group of flags.
type flagGroup struct {
	name     string
	prefixes []string
}

// printGroupedFlags prints flags organized by category.
func (c *ServerCommand) printGroupedFlags() {
	groups := []flagGroup{
		{"Global", []string{"version", "validate", "tui", "debug", "d", "config", "c"}},
		{"Server", []string{"server.", "p"}},
		{"Logging", []string{"logging.", "l"}},
		{"Metrics", []string{"metrics."}},
		{"Mock", []string{"mock."}},
		{"Chaos", []string{"chaos."}},
		{"Storage", []string{"storage."}},
		{"Rate Limit", []string{"rate-limit."}},
		{"Health", []string{"health."}},
		{"Replay", []string{"replay."}},
		{"Plugin", []string{"plugin."}},
		{"Profiling", []string{"pprof."}},
	}

	assigned := make(map[string]bool)

	for _, g := range groups {
		var flags []*flag.Flag
		c.fs.VisitAll(func(f *flag.Flag) {
			for _, prefix := range g.prefixes {
				if f.Name == prefix || (len(prefix) > 1 && len(f.Name) > len(prefix) && f.Name[:len(prefix)] == prefix) {
					flags = append(flags, f)
					assigned[f.Name] = true
					return
				}
			}
		})
		if len(flags) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s:\n", g.name)
		for _, f := range flags {
			defVal := f.DefValue
			if defVal == "" {
				defVal = `""`
			}
			fmt.Fprintf(os.Stderr, "  -%-30s %s (default %s)\n", f.Name, f.Usage, defVal)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Print any unassigned flags under "Other"
	var other []*flag.Flag
	c.fs.VisitAll(func(f *flag.Flag) {
		if !assigned[f.Name] {
			other = append(other, f)
		}
	})
	if len(other) > 0 {
		fmt.Fprintf(os.Stderr, "Other:\n")
		for _, f := range other {
			defVal := f.DefValue
			if defVal == "" {
				defVal = `""`
			}
			fmt.Fprintf(os.Stderr, "  -%-30s %s (default %s)\n", f.Name, f.Usage, defVal)
		}
		fmt.Fprintln(os.Stderr)
	}
}

// loadConfiguration loads the configuration.
func (c *ServerCommand) loadConfiguration() (*config.Config, error) {
	loader := config.NewLoader()
	opts := config.LoadOptions{
		ConfigPath: c.configFile,
	}
	return loader.Load(opts)
}

// flagWasSet returns true if the named flag (or any of the given names) was explicitly provided on the command line.
func (c *ServerCommand) flagWasSet(names ...string) bool {
	found := false
	c.fs.Visit(func(f *flag.Flag) {
		for _, name := range names {
			if f.Name == name {
				found = true
			}
		}
	})
	return found
}

// applyOverrides applies CLI flag values to the configuration.
func (c *ServerCommand) applyOverrides(cfg *config.Config) {
	c.applyServerOverrides(cfg)
	c.applyLoggingOverrides(cfg)
	c.applyFeatureOverrides(cfg)
}

// parseDurationFlag parses a duration string and warns on error.
func parseDurationFlag(name, value string) (time.Duration, bool) {
	d, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: invalid --%s value %q: %v\n", name, value, err)
		return 0, false
	}
	return d, true
}

func (c *ServerCommand) applyServerOverrides(cfg *config.Config) {
	c.applyServerAddressOverrides(cfg)
	c.applyServerLimitOverrides(cfg)
	c.applyServerStreamingOverride(cfg)
	c.applyServerTimeoutOverrides(cfg)
	c.applyServerTLSOverrides(cfg)
}

func (c *ServerCommand) applyServerAddressOverrides(cfg *config.Config) {
	if c.flagWasSet("server.host", "server-host") {
		cfg.Server.Host = c.host
		cfg.Defaults.Host = c.host
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.Host = c.host })
	}
	if c.flagWasSet("server.port", "server-port", "p") {
		cfg.Server.Port = c.port
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.Port = c.port })
	}
}

func (c *ServerCommand) applyServerLimitOverrides(cfg *config.Config) {
	if c.flagWasSet("server.max-connections", "server-max-connections") {
		cfg.Server.MaxConnections = c.maxConns
		cfg.Defaults.MaxConnections = c.maxConns
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.MaxConnections = c.maxConns })
	}
	if c.flagWasSet("server.max-body-size", "server-max-body-size") {
		cfg.Server.MaxBodySize = c.maxBodySize
		cfg.Defaults.SetMaxBodySize(c.maxBodySize)
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.SetMaxBodySize(c.maxBodySize) })
	}
}

func (c *ServerCommand) applyServerStreamingOverride(cfg *config.Config) {
	if c.flagWasSet("server.streaming", "server-streaming") {
		cfg.Server.Streaming = c.streaming
		cfg.Defaults.SetStreaming(c.streaming)
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.SetStreaming(c.streaming) })
	}
}

func (c *ServerCommand) applyServerTimeoutOverrides(cfg *config.Config) {
	c.applyDurationOverride("server.read-timeout", "server-read-timeout", c.readTimeout, func(d time.Duration) {
		cfg.Server.ReadTimeout = d
		cfg.Defaults.ReadTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.ReadTimeout = d })
	})
	c.applyDurationOverride("server.write-timeout", "server-write-timeout", c.writeTimeout, func(d time.Duration) {
		cfg.Server.WriteTimeout = d
		cfg.Defaults.WriteTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.WriteTimeout = d })
	})
	c.applyDurationOverride("server.shutdown-timeout", "server-shutdown-timeout", c.shutdownTimeout, func(d time.Duration) {
		cfg.Server.ShutdownTimeout = d
		cfg.Defaults.ShutdownTimeout = d
		updateServerEntries(cfg, func(entry *config.ServerEntryConfig) { entry.ShutdownTimeout = d })
	})
}

func (c *ServerCommand) applyDurationOverride(name, alias, value string, apply func(time.Duration)) {
	if !c.flagWasSet(name, alias) {
		return
	}
	if d, ok := parseDurationFlag(name, value); ok {
		apply(d)
	}
}

func (c *ServerCommand) applyServerTLSOverrides(cfg *config.Config) {
	if c.flagWasSet("server.tls.enabled", "server-tls-enabled") {
		cfg.Server.TLS.Enabled = c.tlsEnable
	}
	if c.flagWasSet("server.tls.cert", "server-tls-cert") {
		cfg.Server.TLS.CertFile = c.tlsCert
	}
	if c.flagWasSet("server.tls.key", "server-tls-key") {
		cfg.Server.TLS.KeyFile = c.tlsKey
	}
}

func updateServerEntries(cfg *config.Config, update func(*config.ServerEntryConfig)) {
	for name, entry := range cfg.Servers {
		update(&entry)
		cfg.Servers[name] = entry
	}
}

func (c *ServerCommand) applyLoggingOverrides(cfg *config.Config) {
	if c.debugFlag {
		cfg.Logging.Level = "debug" //nolint:goconst
	} else if c.flagWasSet("logging.level", "logging-level", "l") {
		cfg.Logging.Level = c.logLevel
	}
	if c.flagWasSet("logging.format", "logging-format") {
		cfg.Logging.Format = c.logFormat
	}
	if c.flagWasSet("logging.output", "logging-output") {
		cfg.Logging.Output = c.logOutput
	}
	if c.flagWasSet("logging.max-size", "logging-max-size") {
		cfg.Logging.MaxSize = c.logMaxSize
	}
	if c.flagWasSet("logging.max-backups", "logging-max-backups") {
		cfg.Logging.MaxBackups = c.logMaxBackups
	}
	if c.flagWasSet("logging.max-age", "logging-max-age") {
		cfg.Logging.MaxAge = c.logMaxAge
	}
}

func (c *ServerCommand) applyFeatureOverrides(cfg *config.Config) {
	c.applyMetricsAndMockOverrides(cfg)
	c.applyInfraOverrides(cfg)
}

func (c *ServerCommand) applyMetricsAndMockOverrides(cfg *config.Config) {
	if c.flagWasSet("metrics.enabled", "metrics-enabled") {
		cfg.Metrics.Enabled = c.metricsEnabled
	}
	if c.flagWasSet("metrics.host", "metrics-host") {
		cfg.Metrics.Host = c.metricsHost
	}
	if c.flagWasSet("metrics.port", "metrics-port") {
		cfg.Metrics.Port = c.metricsPort
	}
	if c.flagWasSet("metrics.path", "metrics-path") {
		cfg.Metrics.Path = c.metricsPath
	}
	if c.flagWasSet("mock.mode", "mock-mode") {
		cfg.Mock.DefaultMode = c.mockMode
	}
	if c.flagWasSet("mock.scenarios-dir", "mock-scenarios-dir") {
		cfg.Mock.ScenariosDir = c.scenariosDir
	}
	if c.flagWasSet("mock.timeout", "mock-timeout") {
		if d, ok := parseDurationFlag("mock.timeout", c.mockTimeout); ok {
			cfg.Mock.DefaultTimeout = d
		}
	}
	if c.flagWasSet("chaos.enabled", "chaos-enabled") {
		cfg.Chaos.Enabled = c.chaosEnabled
	}
	if c.flagWasSet("chaos.error-rate", "chaos-error-rate") {
		cfg.Chaos.ErrorRate = c.chaosErrorRate
	}
	if c.flagWasSet("chaos.timeout-rate", "chaos-timeout-rate") {
		cfg.Chaos.TimeoutRate = c.chaosTimeoutRate
	}
	if c.flagWasSet("chaos.min-latency-ms", "chaos-min-latency-ms") {
		cfg.Chaos.MinLatencyMs = c.chaosMinLatencyMs
	}
	if c.flagWasSet("chaos.max-latency-ms", "chaos-max-latency-ms") {
		cfg.Chaos.MaxLatencyMs = c.chaosMaxLatencyMs
	}
	if c.flagWasSet("chaos.connection-drop-rate", "chaos-connection-drop-rate") {
		cfg.Chaos.ConnectionDropRate = c.chaosConnectionDropRate
	}
}

func (c *ServerCommand) applyInfraOverrides(cfg *config.Config) {
	c.applyStorageOverrides(cfg)
	c.applyRateLimitOverrides(cfg)
	c.applyHealthReplayPluginOverrides(cfg)
}

func (c *ServerCommand) applyStorageOverrides(cfg *config.Config) {
	if c.flagWasSet("storage.enabled", "storage-enabled") {
		cfg.Storage.Enabled = c.storageEnabled
	}
	if c.flagWasSet("storage.dir", "storage-dir") {
		cfg.Storage.RequestsDir = c.storageDir
	}
	if c.flagWasSet("storage.max-size", "storage-max-size") {
		cfg.Storage.MaxFileSize = c.storageMaxSize
	}
	if c.flagWasSet("storage.rotate", "storage-rotate") {
		cfg.Storage.RotateAfter = c.storageRotate
	}
}

func (c *ServerCommand) applyRateLimitOverrides(cfg *config.Config) {
	if c.flagWasSet("rate-limit.enabled", "rate-limit-enabled") {
		cfg.RateLimit.Enabled = c.rateLimitEnabled
	}
	if c.flagWasSet("rate-limit.rps", "rate-limit-rps") {
		cfg.RateLimit.RequestsPerSecond = c.rateLimitRPS
	}
	if c.flagWasSet("rate-limit.burst", "rate-limit-burst") {
		cfg.RateLimit.Burst = c.rateLimitBurst
	}
	if c.flagWasSet("rate-limit.algorithm", "rate-limit-algorithm") {
		cfg.RateLimit.Algorithm = c.rateLimitAlgo
	}
}

func (c *ServerCommand) applyHealthReplayPluginOverrides(cfg *config.Config) {
	if c.flagWasSet("health.enabled", "health-enabled") {
		cfg.Health.Enabled = c.healthEnabled
	}
	if c.flagWasSet("health.port", "health-port") {
		cfg.Health.Port = c.healthPort
	}
	if c.flagWasSet("health.path", "health-path") {
		cfg.Health.HealthPath = c.healthPath
	}
	if c.flagWasSet("health.ready-path", "health-ready-path") {
		cfg.Health.ReadyPath = c.readyPath
	}
	if c.flagWasSet("replay.enabled", "replay-enabled") {
		cfg.Replay.Enabled = c.replayEnabled
	}
	if c.flagWasSet("replay.speed", "replay-speed") {
		cfg.Replay.Speed = c.replaySpeed
	}
	if c.flagWasSet("plugin.enabled", "plugin-enabled") {
		cfg.Plugin.Enabled = c.pluginEnabled
	}
	if c.flagWasSet("plugin.dir", "plugin-dir") {
		cfg.Plugin.Dir = c.pluginDir
	}
	if c.flagWasSet("pprof.enabled", "pprof-enabled") {
		cfg.Pprof.Enabled = c.pprofEnabled
	}
}

// PrintValidationErrors prints validation errors to the given writer.
func PrintValidationErrors(w io.Writer, errors []config.ValidationError) {
	fmt.Fprintln(w, "Configuration validation failed:") //nolint:errcheck
	for _, e := range errors {
		fmt.Fprintf(w, "  - %s\n", e.Error()) //nolint:errcheck
	}
}
