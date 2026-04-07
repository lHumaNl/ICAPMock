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
		return fmt.Errorf("TUI is not available. Please rebuild with TUI support or run without --tui flag.")
	}

	// Load configuration
	cfg, err := c.loadConfiguration()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// Apply CLI overrides
	c.applyOverrides(cfg)

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
	if c.flagWasSet("server.host", "server-host") {
		cfg.Server.Host = c.host
	}
	if c.flagWasSet("server.port", "server-port", "p") {
		cfg.Server.Port = c.port
	}
	if c.maxConns != 0 {
		cfg.Server.MaxConnections = c.maxConns
	}
	if c.maxBodySize != 0 {
		cfg.Server.MaxBodySize = c.maxBodySize
	}
	if c.flagWasSet("server.streaming", "server-streaming") {
		cfg.Server.Streaming = c.streaming
	}
	if c.readTimeout != "" {
		if duration, err := time.ParseDuration(c.readTimeout); err == nil {
			cfg.Server.ReadTimeout = duration
		} else {
			fmt.Fprintf(os.Stderr, "WARNING: invalid --server.read-timeout value %q: %v\n", c.readTimeout, err)
		}
	}
	if c.writeTimeout != "" {
		if duration, err := time.ParseDuration(c.writeTimeout); err == nil {
			cfg.Server.WriteTimeout = duration
		} else {
			fmt.Fprintf(os.Stderr, "WARNING: invalid --server.write-timeout value %q: %v\n", c.writeTimeout, err)
		}
	}
	if c.shutdownTimeout != "" {
		if duration, err := time.ParseDuration(c.shutdownTimeout); err == nil {
			cfg.Server.ShutdownTimeout = duration
		} else {
			fmt.Fprintf(os.Stderr, "WARNING: invalid --server.shutdown-timeout value %q: %v\n", c.shutdownTimeout, err)
		}
	}

	if c.flagWasSet("server.tls.enabled", "server-tls-enabled") {
		cfg.Server.TLS.Enabled = c.tlsEnable
	}
	if c.tlsCert != "" {
		cfg.Server.TLS.CertFile = c.tlsCert
	}
	if c.tlsKey != "" {
		cfg.Server.TLS.KeyFile = c.tlsKey
	}

	if c.debugFlag {
		cfg.Logging.Level = "debug" //nolint:goconst
	} else if c.logLevel != "" {
		cfg.Logging.Level = c.logLevel
	}
	if c.logFormat != "" {
		cfg.Logging.Format = c.logFormat
	}
	if c.logOutput != "" {
		cfg.Logging.Output = c.logOutput
	}

	if c.flagWasSet("metrics.enabled", "metrics-enabled") {
		cfg.Metrics.Enabled = c.metricsEnabled
	}
	if c.metricsHost != "" {
		cfg.Metrics.Host = c.metricsHost
	}
	if c.metricsPort != 0 {
		cfg.Metrics.Port = c.metricsPort
	}
	if c.metricsPath != "" {
		cfg.Metrics.Path = c.metricsPath
	}

	if c.mockMode != "" {
		cfg.Mock.DefaultMode = c.mockMode
	}
	if c.scenariosDir != "" {
		cfg.Mock.ScenariosDir = c.scenariosDir
	}

	if c.flagWasSet("chaos.enabled", "chaos-enabled") {
		cfg.Chaos.Enabled = c.chaosEnabled
	}
	if c.chaosErrorRate != 0 {
		cfg.Chaos.ErrorRate = c.chaosErrorRate
	}

	if c.flagWasSet("storage.enabled", "storage-enabled") {
		cfg.Storage.Enabled = c.storageEnabled
	}
	if c.storageDir != "" {
		cfg.Storage.RequestsDir = c.storageDir
	}

	if c.flagWasSet("rate-limit.enabled", "rate-limit-enabled") {
		cfg.RateLimit.Enabled = c.rateLimitEnabled
	}
	if c.rateLimitRPS != 0 {
		cfg.RateLimit.RequestsPerSecond = c.rateLimitRPS
	}

	if c.flagWasSet("health.enabled", "health-enabled") {
		cfg.Health.Enabled = c.healthEnabled
	}
	if c.healthPort != 0 {
		cfg.Health.Port = c.healthPort
	}

	if c.flagWasSet("plugin.enabled", "plugin-enabled") {
		cfg.Plugin.Enabled = c.pluginEnabled
	}
	if c.pluginDir != "" {
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
