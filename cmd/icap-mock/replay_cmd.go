// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/replay"
)

// ReplayCommand handles the replay subcommand.
type ReplayCommand struct {
	fs         *flag.FlagSet
	dir        string
	from       string
	to         string
	method     string
	target     string
	configFile string
	speed      float64
	parallel   int
	loop       bool
}

// NewReplayCommand creates a new replay command.
func NewReplayCommand() *ReplayCommand {
	cmd := &ReplayCommand{
		fs: flag.NewFlagSet("replay", flag.ExitOnError),
	}

	cmd.fs.StringVar(&cmd.dir, "dir", "./data/requests", "Directory containing recorded requests")
	cmd.fs.StringVar(&cmd.dir, "D", "./data/requests", "Directory containing recorded requests (shorthand)")
	cmd.fs.StringVar(&cmd.from, "from", "", "Start time filter (YYYY-MM-DD or RFC3339)")
	cmd.fs.StringVar(&cmd.to, "to", "", "End time filter (YYYY-MM-DD or RFC3339)")
	cmd.fs.StringVar(&cmd.method, "method", "", "ICAP method filter (REQMOD, RESPMOD)")
	cmd.fs.Float64Var(&cmd.speed, "speed", 1.0, "Replay speed multiplier (1.0=original, 2.0=2x faster, 0=max speed)")
	cmd.fs.StringVar(&cmd.target, "target", "", "Target ICAP server URL (e.g., icap://localhost:1344)")
	cmd.fs.BoolVar(&cmd.loop, "loop", false, "Enable continuous replay (loop mode)")
	cmd.fs.IntVar(&cmd.parallel, "parallel", 1, "Number of parallel replay workers")
	cmd.fs.StringVar(&cmd.configFile, "config", "", "Path to config file for logging settings")

	return cmd
}

// Name returns the command name.
func (c *ReplayCommand) Name() string {
	return "replay" //nolint:goconst
}

// Description returns a short description of the command.
func (c *ReplayCommand) Description() string {
	return "Replay recorded ICAP requests"
}

// Parse parses the command arguments.
func (c *ReplayCommand) Parse(args []string) error {
	return c.fs.Parse(args)
}

// Run executes the replay command.
func (c *ReplayCommand) Run(ctx context.Context) error {
	// Load config for logging settings
	var log *logger.Logger
	if c.configFile != "" {
		loader := config.NewLoader()
		cfg, err := loader.Load(config.LoadOptions{ConfigPath: c.configFile})
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		log, err = logger.New(cfg.Logging)
		if err != nil {
			return fmt.Errorf("initializing logger: %w", err)
		}
		defer log.Close() //nolint:errcheck
	} else {
		// Use default text logger
		log = logger.MustNew(config.LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		})
	}

	// Validate directory
	if c.dir == "" {
		return fmt.Errorf("--dir is required")
	}

	// Check if directory exists
	if _, err := os.Stat(c.dir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", c.dir)
	}

	// Parse filter from flags
	filter, err := replay.ParseFilterFromFlags(c.from, c.to, c.method)
	if err != nil {
		return fmt.Errorf("parsing filter: %w", err)
	}

	// Create file storage adapter for the replay directory
	store := replay.NewFileStorageAdapter(c.dir)

	// Create metrics collector (optional, for tracking)
	registry := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(registry)
	if err != nil {
		return fmt.Errorf("creating metrics collector: %w", err)
	}

	// Create replay config
	cfg := &config.ReplayConfig{
		Enabled:     true,
		RequestsDir: c.dir,
		Speed:       c.speed,
	}

	// Create replayer
	replayer, err := replay.NewReplayer(cfg, store, log, collector)
	if err != nil {
		return fmt.Errorf("creating replayer: %w", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("received shutdown signal, stopping replay")
		replayer.Stop()
	}()

	// Prepare replay options
	opts := replay.ReplayOptions{
		Filter:    filter,
		Speed:     c.speed,
		Loop:      c.loop,
		Parallel:  c.parallel,
		TargetURL: c.target,
		OnProgress: func(current, total int) {
			if total > 0 {
				percent := float64(current) / float64(total) * 100
				fmt.Printf("\rReplaying: %d/%d (%.1f%%)", current, total, percent)
				if current == total {
					fmt.Println()
				}
			} else {
				fmt.Printf("\rReplaying: %d requests", current)
			}
		},
	}

	// If no target specified, show a warning
	if c.target == "" {
		log.Warn("no target server specified, requests will fail unless original URLs are reachable")
		log.Info("use --target icap://host:port to specify the target ICAP server")
	}

	log.Info("starting replay",
		"dir", c.dir,
		"speed", c.speed,
		"loop", c.loop,
		"parallel", c.parallel,
		"target", c.target,
		"filter", filter,
	)

	// Run the replay
	startTime := time.Now()
	err = replayer.Start(ctx, opts)

	// Print final stats
	stats := replayer.Stats()
	log.Info("replay completed",
		"total_requests", stats.TotalRequests,
		"successful", stats.SuccessfulRequests,
		"failed", stats.FailedRequests,
		"duration", time.Since(startTime).Round(time.Millisecond),
	)

	return err
}

// Usage prints the command usage.
func (c *ReplayCommand) Usage() {
	fmt.Fprintf(os.Stderr, "Usage: icap-mock replay [options]\n\n")
	fmt.Fprintf(os.Stderr, "Replay recorded ICAP requests to an ICAP server.\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	c.fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  # Replay all recorded requests\n")
	fmt.Fprintf(os.Stderr, "  icap-mock replay --dir ./data/requests\n\n")
	fmt.Fprintf(os.Stderr, "  # Replay with date filter\n")
	fmt.Fprintf(os.Stderr, "  icap-mock replay --dir ./data/requests --from YYYY-MM-DD --to YYYY-MM-DD\n\n")
	fmt.Fprintf(os.Stderr, "  # Replay at 2x speed to a different server\n")
	fmt.Fprintf(os.Stderr, "  icap-mock replay --dir ./data/requests --speed 2.0 --target icap://localhost:1344\n\n")
	fmt.Fprintf(os.Stderr, "  # Continuous replay (loop mode)\n")
	fmt.Fprintf(os.Stderr, "  icap-mock replay --dir ./data/requests --loop --target icap://icap-server:1344\n")
}
