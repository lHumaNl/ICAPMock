// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
)

// Command represents a CLI subcommand.
type Command interface {
	// Name returns the command name.
	Name() string

	// Description returns a short description of the command.
	Description() string

	// Parse parses the command arguments.
	Parse(args []string) error

	// Run executes the command.
	Run(ctx context.Context) error

	// Usage prints the command usage.
	Usage()
}

// CommandRegistry manages available subcommands.
type CommandRegistry struct {
	commands   map[string]Command
	defaultCmd string
}

// NewCommandRegistry creates a new command registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]Command),
	}
}

// Register registers a command with the registry.
func (r *CommandRegistry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
}

// SetDefault sets the default command when no subcommand is specified.
func (r *CommandRegistry) SetDefault(name string) {
	r.defaultCmd = name
}

// Get returns a command by name.
func (r *CommandRegistry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// GetDefault returns the default command.
func (r *CommandRegistry) GetDefault() (Command, bool) {
	if r.defaultCmd == "" {
		return nil, false
	}
	return r.Get(r.defaultCmd)
}

// List returns all registered command names.
func (r *CommandRegistry) List() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	return names
}

// PrintUsage prints global usage including all subcommands.
func (r *CommandRegistry) PrintUsage() {
	fmt.Fprintf(os.Stderr, "Usage: icap-mock <command> [command-options]\n\n")
	fmt.Fprintf(os.Stderr, "Available commands:\n")
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cmd := r.commands[name]
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", name, cmd.Description())
	}
	fmt.Fprintf(os.Stderr, "\nUse 'icap-mock <command> --help' for command-specific help.\n")
	fmt.Fprintf(os.Stderr, "\nCommon options:\n")
	fmt.Fprintf(os.Stderr, "  --config, -c   Path to configuration file (YAML or JSON)\n")
	fmt.Fprintf(os.Stderr, "  --debug, -d    Enable debug logging\n")
	fmt.Fprintf(os.Stderr, "  --help, -h     Show this help message\n")
	fmt.Fprintf(os.Stderr, "  --version       Print version information\n")
}
