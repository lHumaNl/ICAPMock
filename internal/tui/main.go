package tui

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/icap-mock/icap-mock/internal/tui/state"
)

// RunTUI starts the TUI application with optional configuration
func RunTUI(cfg *state.ClientConfig) error {
	return RunTUIWithVersion(cfg, "dev")
}

// RunTUIWithVersion starts the TUI application with optional configuration and version.
func RunTUIWithVersion(cfg *state.ClientConfig, version string) error {
	if cfg == nil {
		cfg = state.DefaultClientConfig()
	}

	model := InitialModel("ICAP Mock Server", version, cfg)

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	p := tea.NewProgram(model, tea.WithAltScreen())

	// Run the program in a goroutine so we can handle signals
	programDone := make(chan error, 1)
	go func() {
		_, err := p.Run()
		programDone <- err
	}()

	// Wait for either signal or program completion
	select {
	case <-sigCh:
		// Graceful shutdown requested by signal
		p.Send(ShutdownSignal{})

		// Give the program time to shutdown gracefully
		select {
		case err := <-programDone:
			return err
		case <-time.After(5 * time.Second):
			// Timeout waiting for graceful shutdown
			// The program should be killed by OS
			return nil
		}

	case err := <-programDone:
		return err
	}
}
