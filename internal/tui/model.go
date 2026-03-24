package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/icap-mock/icap-mock/internal/tui/components"
	"github.com/icap-mock/icap-mock/internal/tui/server"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

// Screen represents different views in application
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenConfig
	ScreenScenarios
	ScreenLogs
	ScreenReplay
	ScreenHealth
)

func (s Screen) String() string {
	switch s {
	case ScreenDashboard:
		return "Dashboard"
	case ScreenConfig:
		return "Config Editor"
	case ScreenScenarios:
		return "Scenarios"
	case ScreenLogs:
		return "Logs"
	case ScreenReplay:
		return "Replay"
	case ScreenHealth:
		return "Health Monitor"
	default:
		return "Unknown"
	}
}

// Model represents complete application state
type Model struct {
	// Navigation state
	currentScreen  Screen
	previousScreen Screen

	// Navigation components
	header *components.HeaderModel
	footer *components.FooterModel
	tabs   *components.TabsModel
	layout *components.LayoutModel

	// Dashboard component
	dashboard *components.DashboardModel

	// Health monitor component
	healthMonitor *components.HealthMonitorModel

	// Service controls component
	serviceControls *components.ServiceControlsModel

	// Config editor component
	configEditor *components.ConfigEditorModel

	// Log viewer component
	logViewer *components.LogViewerModel

	// Scenario manager component
	scenarioManager *components.ScenarioManagerModel

	// Replay panel component
	replayPanel *components.ReplayPanelModel

	// HTTP clients
	scenarioClient *server.ScenarioClient
	replayClient   *server.ReplayClient
	configClient   *server.ConfigClient

	// Shared state
	metricsState *state.MetricsState
	logsState    *state.LogsState
	serverStatus *state.ServerStatus

	// UI dimensions
	width  int
	height int

	// Ready flag
	ready bool

	// Last message
	lastMessage string

	// confirmExit is set when the user presses Esc on a screen with unsaved changes
	confirmExit bool

	// showHelp toggles the full help overlay
	showHelp bool

	// App info
	appName string
	version string

	// Server address for error context
	serverStatusURL string

	// Cleanup
	tickerCancel   context.CancelFunc
	shutdownCancel context.CancelFunc
	shutdownDone   chan struct{}
	shutdownMu     sync.Mutex
}

// InitialModel creates initial application state with configuration
func InitialModel(appName, version string, cfg *state.ClientConfig) *Model {
	model := &Model{
		currentScreen:  ScreenDashboard,
		previousScreen: ScreenDashboard,
		appName:        appName,
		version:        version,
	}

	// Initialize navigation components
	model.header = components.NewHeaderModel(appName, version)
	model.footer = components.NewFooterModel()
	model.tabs = components.NewTabsModel()
	model.layout = components.NewLayoutModel(lipgloss.Top)

	// Initialize dashboard component
	model.dashboard = components.NewDashboardModel()

	// Initialize health monitor component
	model.healthMonitor = components.NewHealthMonitorModel()

	// Initialize service controls component
	model.serviceControls = components.NewServiceControlsModel()

	// Initialize config editor component
	model.configEditor = components.NewConfigEditorModel()

	// Initialize log viewer component
	model.logViewer = components.NewLogViewerModel()

	// Initialize scenario manager component
	model.scenarioManager = components.NewScenarioManagerModel()

	// Initialize replay panel component
	model.replayPanel = components.NewReplayPanelModel()

	// Use configured server address or defaults
	serverHost := "localhost"
	serverPort := 1344
	if cfg != nil {
		if cfg.ServerHost != "" {
			serverHost = cfg.ServerHost
		}
		if cfg.ServerPort != 0 {
			serverPort = cfg.ServerPort
		}
	}

	model.replayPanel.SetTargetURL(fmt.Sprintf("http://%s:%d", serverHost, serverPort))

	// Initialize HTTP clients
	model.scenarioClient = server.NewScenarioClient(serverHost, serverPort)
	model.replayClient = server.NewReplayClient(serverHost, serverPort)
	model.configClient = server.NewConfigClient(serverHost, serverPort)

	// Setup tabs
	model.tabs.SetTabs([]components.Tab{
		{ID: "dashboard", Title: "Dashboard", Shortcut: "1"},
		{ID: "config", Title: "Config", Shortcut: "2"},
		{ID: "scenarios", Title: "Scenarios", Shortcut: "3"},
		{ID: "logs", Title: "Logs", Shortcut: "4"},
		{ID: "replay", Title: "Replay", Shortcut: "5"},
		{ID: "health", Title: "Health", Shortcut: "6"},
	})

	// Setup footer key bindings
	model.footer.SetKeyBindings(components.DefaultKeyBindings())

	// Initialize shared state with configuration
	model.metricsState = state.NewMetricsState(cfg)
	model.logsState = state.NewLogsState(cfg)
	model.serverStatus = state.NewServerStatus(cfg)

	// Store server status URL for error context messages
	if cfg != nil && cfg.StatusURL != "" {
		model.serverStatusURL = cfg.StatusURL
	} else {
		model.serverStatusURL = "http://localhost:8080"
	}

	return model
}

// Init initializes model and returns any initial commands
func (m *Model) Init() tea.Cmd {
	// Create context for ticker cancellation
	ctx, cancel := context.WithCancel(context.Background())
	m.tickerCancel = cancel

	// Initialize shutdown signaling
	m.shutdownDone = make(chan struct{})

	return tea.Batch(
		// Start ticker for periodic updates
		m.tickCmd(ctx),
		// Start metrics streaming
		m.metricsState.StartStreaming(),
		// Start log streaming
		m.logsState.StartStreaming(),
		// Check server status
		m.serverStatus.Check(),
	)
}

// tickCmd returns a command that sends TickMsg every second
func (m *Model) tickCmd(ctx context.Context) tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil
		default:
			return TickMsg{Time: t}
		}
	})
}

// Cleanup releases resources and stops tickers
func (m *Model) Cleanup() {
	if m.tickerCancel != nil {
		m.tickerCancel()
	}
}

// Shutdown gracefully stops all components
func (m *Model) Shutdown() {
	// Cancel ticker
	if m.tickerCancel != nil {
		m.tickerCancel()
		m.tickerCancel = nil
	}

	// Stop all streaming operations
	if m.metricsState != nil {
		m.metricsState.Shutdown()
	}

	if m.logsState != nil {
		m.logsState.Shutdown()
	}

	if m.serverStatus != nil {
		m.serverStatus.Shutdown()
	}

	// Notify shutdown is complete (with mutex to prevent race condition)
	m.shutdownMu.Lock()
	defer m.shutdownMu.Unlock()
	if m.shutdownDone != nil {
		close(m.shutdownDone)
		m.shutdownDone = nil
	}
}
