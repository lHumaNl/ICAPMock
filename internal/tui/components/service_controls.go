package components

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// ServiceControlsModel represents the service controls component
type ServiceControlsModel struct {
	mu           sync.RWMutex
	serverStatus string
	serverPort   string
	serverUptime string
	loading      bool
}

// NewServiceControlsModel creates a new service controls model
func NewServiceControlsModel() *ServiceControlsModel {
	return &ServiceControlsModel{
		serverStatus: "unknown",
		serverPort:   "N/A",
		serverUptime: "N/A",
		loading:      false,
	}
}

// SetStatus updates the server status information
func (m *ServiceControlsModel) SetStatus(status, port, uptime string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serverStatus = status
	m.serverPort = port
	m.serverUptime = uptime
}

// SetLoading sets the loading state
func (m *ServiceControlsModel) SetLoading(loading bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loading = loading
}

// View renders the service controls component
func (m *ServiceControlsModel) View() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Render status indicator
	statusIndicator := m.renderStatusIndicator()

	// Render server info
	serverInfo := m.renderServerInfo()

	// Render control buttons
	controls := m.renderControls()

	// Render keyboard shortcuts
	shortcuts := m.renderShortcuts()

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Service Controls"),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, statusIndicator, " ", serverInfo),
		"",
		controls,
		"",
		shortcuts,
	)

	return PanelStyle.Render(content)
}

// renderStatusIndicator renders the server status indicator
func (m *ServiceControlsModel) renderStatusIndicator() string {
	var style lipgloss.Style
	var text string

	switch m.serverStatus {
	case "running":
		style = StatusRunningStyle
		text = "● RUNNING"
	case "stopped":
		style = StatusStoppedStyle
		text = "● STOPPED"
	case "error":
		style = ErrorStyle
		text = "● ERROR"
	default:
		style = StatusWarningStyle
		text = "● UNKNOWN"
	}

	return style.Render(text)
}

// renderServerInfo renders the server information
func (m *ServiceControlsModel) renderServerInfo() string {
	return SubtitleStyle.Render(
		"Port: " + m.serverPort + " | Uptime: " + m.serverUptime,
	)
}

// renderControls renders the control buttons
func (m *ServiceControlsModel) renderControls() string {
	if m.loading {
		return SubtitleStyle.Render("Processing...")
	}

	startBtn := ButtonStyle.Render("[s] Start")
	stopBtn := ButtonStyle.Render("[t] Stop")
	restartBtn := ButtonStyle.Render("[r] Restart")

	return lipgloss.JoinHorizontal(lipgloss.Left, startBtn, " ", stopBtn, " ", restartBtn)
}

// renderShortcuts renders the keyboard shortcuts
func (m *ServiceControlsModel) renderShortcuts() string {
	return SubtitleStyle.Render(
		"Keyboard: s=start | t=stop | r=restart | esc=back",
	)
}

// ButtonStyle for control buttons
var ButtonStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("250")).
	Background(lipgloss.Color("240")).
	Padding(0, 2)
