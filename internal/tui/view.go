// Copyright 2026 ICAP Mock

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/icap-mock/icap-mock/internal/tui/components"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

// View renders the UI.
func (m *Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Update navigation components with current state
	m.updateNavigationState()

	// Render header component
	header := m.header.View()

	// Render tabs component
	tabs := m.tabs.View()

	// Main content area
	content := m.renderContent()

	// Render context-sensitive status bar
	statusBar := m.renderStatusBar()

	// Render footer component
	footer := m.footer.View()

	// Combine all sections
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		tabs,
		content,
		statusBar,
		footer,
	)

	// Render help overlay on top if active
	if m.showHelp {
		return m.renderHelpOverlay(view)
	}

	return view
}

// updateNavigationState updates navigation components with current model state.
func (m *Model) updateNavigationState() {
	// Update header with server status
	if m.serverStatus != nil {
		status := m.serverStatus.Current()
		m.header.SetServerInfo(components.ServerInfo{
			Running: status.State == "running",
			Port:    status.Port,
			Uptime:  status.Uptime,
		})
	}

	// Update tabs with current screen
	screenMap := map[Screen]string{
		ScreenDashboard: "dashboard",
		ScreenConfig:    "config",
		ScreenScenarios: "scenarios",
		ScreenLogs:      "logs",
		ScreenReplay:    "replay",
		ScreenHealth:    "health",
	}

	if screenID, ok := screenMap[m.currentScreen]; ok {
		m.tabs.SetActiveTabByID(screenID)
	}

	// Update footer with last message
	if m.lastMessage != "" {
		m.footer.SetStatus(m.lastMessage)
	}
}

// renderContent displays the current screen.
func (m *Model) renderContent() string {
	switch m.currentScreen {
	case ScreenDashboard:
		return m.renderDashboard()
	case ScreenConfig:
		return m.renderConfig()
	case ScreenScenarios:
		return m.renderScenarios()
	case ScreenLogs:
		return m.renderLogs()
	case ScreenReplay:
		return m.renderReplay()
	case ScreenHealth:
		return m.renderHealth()
	default:
		return "Unknown screen"
	}
}

// renderDashboard renders dashboard screen.
func (m *Model) renderDashboard() string {
	if m.dashboard == nil {
		return PanelStyle.Render(
			TitleStyle.Render("Dashboard"),
			"",
			SubtitleStyle.Render("Dashboard component not initialized"),
		)
	}

	// Update service controls with current server status
	if m.serverStatus != nil && m.serviceControls != nil {
		status := m.serverStatus.Current()
		m.serviceControls.SetStatus(status.State, status.Port, status.Uptime)
	}

	// Update dashboard with current metrics
	metrics := m.metricsState.GetCurrent()
	m.dashboard.SetMetrics(metrics)

	// Render service controls
	serviceControls := m.serviceControls.View()

	// Render dashboard component
	dashboard := m.dashboard.View()

	// Combine service controls and dashboard
	return lipgloss.JoinVertical(lipgloss.Left, serviceControls, "", dashboard)
}

// renderConfig renders config editor screen.
func (m *Model) renderConfig() string {
	if m.configEditor == nil {
		return PanelStyle.Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				TitleStyle.Render("Config Editor"),
				"",
				SubtitleStyle.Render("Configuration editor not initialized"),
			),
		)
	}

	// Update config editor dimensions
	m.configEditor.SetWindowSize(m.width, m.height)

	// Render config editor
	return m.configEditor.View()
}

// renderScenarios renders scenarios screen.
func (m *Model) renderScenarios() string {
	if m.scenarioManager == nil {
		return PanelStyle.Render(
			SubtitleStyle.Render("Scenario manager not initialized"),
		)
	}

	return m.scenarioManager.View()
}

// renderLogs renders logs screen.
func (m *Model) renderLogs() string {
	// Update log viewer with latest entries
	logs := m.logsState.GetEntries(nil, 1000)
	m.logViewer.SetEntries(logs)

	return m.logViewer.View()
}

// renderReplay renders replay screen.
func (m *Model) renderReplay() string {
	if m.replayPanel == nil {
		return PanelStyle.Render(
			SubtitleStyle.Render("Replay panel not initialized"),
		)
	}

	return m.replayPanel.View()
}

// renderHealth renders health monitor screen.
func (m *Model) renderHealth() string {
	if m.healthMonitor == nil {
		status := m.serverStatus.Current()

		return PanelStyle.Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				TitleStyle.Render("Health Monitor"),
				"",
				SubtitleStyle.Render("Server Status: "+status.State),
				SubtitleStyle.Render("Port: "+status.Port),
				SubtitleStyle.Render("Uptime: "+status.Uptime),
			),
		)
	}

	return m.healthMonitor.View()
}

// renderStatusBar renders context-sensitive keybindings at the bottom.
func (m *Model) renderStatusBar() string {
	bindings := m.ShortHelp()
	var parts []string
	for _, b := range bindings {
		keys := b.Help().Key
		desc := b.Help().Desc
		if keys == "" {
			continue
		}
		keyPart := FooterKeyStyle.Render(keys)
		descPart := FooterDescStyle.Render(desc)
		parts = append(parts, keyPart+":"+descPart)
	}

	bar := strings.Join(parts, "  ")
	style := lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorMuted).
		Padding(0, 1)

	if m.width > 0 {
		style = style.Width(m.width)
	}

	return style.Render(bar)
}

// renderHelpOverlay renders the full help as a centered overlay on top of the view.
func (m *Model) renderHelpOverlay(baseView string) string {
	groups := m.FullHelp()

	var lines []string //nolint:prealloc
	lines = append(lines, HelpKeyStyle.Render("Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, group := range groups {
		var groupLines []string
		for _, b := range group {
			keys := b.Help().Key
			desc := b.Help().Desc
			if keys == "" {
				continue
			}
			entry := fmt.Sprintf("  %s  %s",
				HelpKeyStyle.Render(fmt.Sprintf("%-12s", keys)),
				HelpDescStyle.Render(desc),
			)
			groupLines = append(groupLines, entry)
		}
		lines = append(lines, groupLines...)
		lines = append(lines, "")
	}

	lines = append(lines, SubtitleStyle.Render("Press ? or Esc to close"))

	helpContent := strings.Join(lines, "\n")

	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 3).
		Background(ColorBackground)

	overlay := overlayStyle.Render(helpContent)

	// Center the overlay on the base view
	overlayW := lipgloss.Width(overlay)
	overlayH := lipgloss.Height(overlay)
	baseW := m.width
	baseH := m.height
	if baseW == 0 {
		baseW = lipgloss.Width(baseView)
	}
	if baseH == 0 {
		baseH = lipgloss.Height(baseView)
	}

	xPad := 0
	if baseW > overlayW {
		xPad = (baseW - overlayW) / 2
	}
	yPad := 0
	if baseH > overlayH {
		yPad = (baseH - overlayH) / 3
	}

	positioned := lipgloss.NewStyle().
		PaddingLeft(xPad).
		PaddingTop(yPad).
		Render(overlay)

	return positioned
}

// renderLogEntries renders log entries.
func (m *Model) renderLogEntries(entries []*state.LogEntry) string {
	if len(entries) == 0 {
		return SubtitleStyle.Render("No logs available")
	}

	var rendered []string
	for _, entry := range entries {
		style := m.getLogLevelStyle(entry.Level)
		rendered = append(rendered,
			style.Render(
				"["+entry.Timestamp.Format("15:04:05")+"] "+
					entry.Level+": "+
					entry.Message,
			),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

// getLogLevelStyle returns style for a log level.
func (m *Model) getLogLevelStyle(level string) lipgloss.Style {
	return GetLogLevelStyle(level)
}

// formatFloat formats a float64 with 2 decimal places.
func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// formatInt formats an int64 as a string.
func formatInt(i int64) string {
	return fmt.Sprintf("%d", i)
}
