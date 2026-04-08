// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/tui"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

func main() {
	// Create command registry
	registry := NewCommandRegistry()

	// Register commands with TUI runner injected
	serverCmd := NewServerCommand()
	serverCmd.TUIRunner = func(cfg interface{}) error {
		var clientCfg *state.ClientConfig
		if cfg != nil {
			clientCfg = cfg.(*state.ClientConfig) //nolint:errcheck
		}
		return tui.RunTUIWithVersion(clientCfg, version)
	}
	registry.Register(serverCmd)
	replayCmd := NewReplayCommand()
	registry.Register(replayCmd)
	registry.Register(NewValidateCommand())
	registry.Register(NewMatchTestCommand())
	registry.Register(NewAssertCommand())
	registry.Register(NewGenerateCommand())

	// Set server as default for backward compatibility
	registry.SetDefault("server")

	// Parse arguments
	args := os.Args[1:]

	// Check for global flags that work without a subcommand
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h": //nolint:goconst
			registry.PrintUsage()
			os.Exit(0)
		case "--version":
			PrintVersion()
			os.Exit(0)
		}
	}

	cmd, cmdArgs := resolveCommand(registry, args)

	// Parse command arguments
	if err := cmd.Parse(cmdArgs); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
		cmd.Usage()
		os.Exit(1)
	}

	// Setup signal handling and run
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	if err := cmd.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1) //nolint:gocritic // exitAfterDefer: cancel is best-effort cleanup
	}
}

// resolveCommand determines which command to run and returns it along with its arguments.
func resolveCommand(registry *CommandRegistry, args []string) (cmd Command, remaining []string) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		defaultCmd, ok := registry.GetDefault()
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: no default command\n")
			os.Exit(1)
		}
		return defaultCmd, args
	}

	cmdName := args[0]
	cmd, ok := registry.Get(cmdName)
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmdName)
		if suggestion := findClosestCommand(cmdName, registry.List()); suggestion != "" {
			fmt.Fprintf(os.Stderr, "Did you mean: %s?\n\n", suggestion)
		}
		registry.PrintUsage()
		os.Exit(1)
	}

	// Check for command-specific help
	if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
		cmd.Usage()
		os.Exit(0)
	}

	return cmd, args[1:]
}

// RunWithContext starts the ICAP server with the given context.
// This function is separated from Run to allow signal handling in main.
func RunWithContext(ctx context.Context, cfg interface{}) error {
	return Run(ctx, cfg.(*config.Config)) //nolint:errcheck
}

// findClosestCommand returns the closest matching command name using Levenshtein distance.
// Returns empty string if no command is close enough (distance > 3).
func findClosestCommand(input string, commands []string) string {
	best := ""
	bestDist := 4 // max distance threshold
	for _, cmd := range commands {
		d := levenshtein(input, cmd)
		if d < bestDist {
			bestDist = d
			best = cmd
		}
	}
	return best
}

// levenshtein computes the Levenshtein edit distance between two strings.
func levenshtein(a, b string) int {
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
