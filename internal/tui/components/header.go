// Package components provides reusable UI components for the TUI.
package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeaderModel represents the header component.
type HeaderModel struct {
	appName    string
	version    string
	width      int
	height     int
	serverInfo ServerInfo
}

// ServerInfo contains server status information.
type ServerInfo struct {
	Running bool
	Port    string
	Uptime  string
	Errors  int
}

// NewHeaderModel creates a new header model.
func NewHeaderModel(appName, version string) *HeaderModel {
	return &HeaderModel{
		appName:    appName,
		version:    version,
		serverInfo: ServerInfo{Running: false},
	}
}

// SetSize sets the header dimensions.
func (m *HeaderModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetServerInfo updates the server information.
func (m *HeaderModel) SetServerInfo(info ServerInfo) {
	m.serverInfo = info
}

// View renders the header.
func (m *HeaderModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Render title section
	title := m.renderTitle()

	// Render status section
	status := m.renderStatus()

	// Calculate spacing
	spacing := m.width - lipgloss.Width(title) - lipgloss.Width(status)
	if spacing < 0 {
		spacing = 2
	}

	// Combine title and status
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		title,
		strings.Repeat(" ", spacing),
		status,
	)
}

// renderTitle renders the application title and version.
func (m *HeaderModel) renderTitle() string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	title := titleStyle.Render(m.appName)
	version := versionStyle.Render(fmt.Sprintf("v%s", m.version))

	return title + " " + version
}

// renderStatus renders the server status.
func (m *HeaderModel) renderStatus() string {
	var statusColor lipgloss.Color
	var statusText string

	if m.serverInfo.Running {
		statusColor = lipgloss.Color("42")
		statusText = "● RUNNING"
	} else {
		statusColor = lipgloss.Color("196")
		statusText = "● STOPPED"
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(statusColor)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	status := statusStyle.Render(statusText)
	info := infoStyle.Render(fmt.Sprintf(
		"| Port: %s | Uptime: %s",
		m.serverInfo.Port,
		m.serverInfo.Uptime,
	))

	return status + " " + info
}

// GetHeight returns the height of the header (always 1 line).
func (m *HeaderModel) GetHeight() int {
	return 1
}

// Header styles
var (
	headerBackgroundStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Padding(0, 1)

	borderBottomStyle = lipgloss.NewStyle().
				Border(lipgloss.Border{Left: " ", Right: " ", Top: " ", Bottom: "─"})
)
