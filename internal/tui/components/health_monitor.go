// Copyright 2026 ICAP Mock

package components

import (
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/icap-mock/icap-mock/internal/tui/state"
)

const statusStarting = "starting"

// HealthMonitorModel manages the health monitor component.
type HealthMonitorModel struct {
	healthChecks []state.HealthCheckResult
	alerts       []string
	mu           sync.RWMutex
}

// NewHealthMonitorModel creates a new health monitor model.
func NewHealthMonitorModel() *HealthMonitorModel {
	return &HealthMonitorModel{
		healthChecks: make([]state.HealthCheckResult, 0),
		alerts:       make([]string, 0),
	}
}

// UpdateHealthCheck updates the health monitor with a new check result.
func (m *HealthMonitorModel) UpdateHealthCheck(result state.HealthCheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Add to health checks (keep last 10 for display)
	m.healthChecks = append(m.healthChecks, result)
	if len(m.healthChecks) > 10 {
		m.healthChecks = m.healthChecks[1:]
	}

	// Generate alert for unhealthy states
	if !result.Healthy {
		m.addAlert(fmt.Sprintf("Health check failed: %s", result.Error))
	}
	if !result.Ready {
		m.addAlert("Server is not ready to accept traffic")
	}
	if result.ICAPStatus != "ok" && result.ICAPStatus != statusStarting {
		m.addAlert(fmt.Sprintf("ICAP server issue: %s", result.ICAPStatus))
	}
	if result.StorageStatus != "ok" && result.StorageStatus != statusStarting {
		m.addAlert(fmt.Sprintf("Storage issue: %s", result.StorageStatus))
	}
}

// addAlert adds an alert message.
func (m *HealthMonitorModel) addAlert(message string) {
	// Add alert with timestamp
	alert := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message)
	m.alerts = append(m.alerts, alert)

	// Keep only last 5 alerts
	if len(m.alerts) > 5 {
		m.alerts = m.alerts[1:]
	}
}

// ClearAlerts clears all alerts.
func (m *HealthMonitorModel) ClearAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = make([]string, 0)
}

// View renders the health monitor component.
func (m *HealthMonitorModel) View() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	content := m.renderContent()
	return PanelStyle.Render(content)
}

// renderContent renders the health monitor content.
func (m *HealthMonitorModel) renderContent() string {
	var sections []string

	// Title
	sections = append(sections, TitleStyle.Render("Health Monitor"))
	sections = append(sections, "")

	// Current health status
	if len(m.healthChecks) > 0 {
		latest := m.healthChecks[len(m.healthChecks)-1]
		sections = append(sections, m.renderCurrentHealth(latest))
		sections = append(sections, "")
	}

	// Alerts
	if len(m.alerts) > 0 {
		sections = append(sections, m.renderAlerts())
		sections = append(sections, "")
	}

	// Health check history
	if len(m.healthChecks) > 1 {
		sections = append(sections, m.renderHistory())
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderCurrentHealth renders the current health status.
func (m *HealthMonitorModel) renderCurrentHealth(result state.HealthCheckResult) string {
	var lines []string

	// Overall status
	statusStyle := m.getStatusStyle(result.Healthy, result.Ready)
	statusText := "OK"
	if !result.Healthy {
		statusText = "Unhealthy"
	} else if !result.Ready {
		statusText = "Not Ready"
	}
	lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("Status: %s", statusStyle.Render(statusText))))

	// ICAP server status
	icapStyle := m.getComponentStyle(result.ICAPStatus)
	lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("ICAP Server: %s", icapStyle.Render(result.ICAPStatus))))

	// Storage status
	storageStyle := m.getComponentStyle(result.StorageStatus)
	lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("Storage: %s", storageStyle.Render(result.StorageStatus))))

	// Scenarios loaded
	lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("Scenarios Loaded: %d", result.Scenarios)))

	// Last check time
	lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("Last Check: %s", result.Timestamp.Format("15:04:05"))))

	// Error if present
	if result.Error != "" {
		lines = append(lines, "")
		lines = append(lines, SubtitleStyle.Render(fmt.Sprintf("Error: %s", result.Error)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderAlerts renders the alerts section.
func (m *HealthMonitorModel) renderAlerts() string {
	lines := make([]string, 0, 1+len(m.alerts))
	lines = append(lines, TitleStyle.Render("Alerts"))

	for _, alert := range m.alerts {
		lines = append(lines, ErrorStyle.Render(alert))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderHistory renders the health check history.
func (m *HealthMonitorModel) renderHistory() string {
	var lines []string
	lines = append(lines, TitleStyle.Render("Recent Health Checks"))

	for i := len(m.healthChecks) - 1; i >= 0 && i >= len(m.healthChecks)-5; i-- {
		check := m.healthChecks[i]
		statusStyle := m.getStatusStyle(check.Healthy, check.Ready)
		status := "OK"
		if !check.Healthy {
			status = "FAIL"
		} else if !check.Ready {
			status = "NREADY"
		}

		line := fmt.Sprintf("%s - %s",
			check.Timestamp.Format("15:04:05"),
			statusStyle.Render(status))
		lines = append(lines, SubtitleStyle.Render(line))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// getStatusStyle returns the style for the overall status.
func (m *HealthMonitorModel) getStatusStyle(healthy, ready bool) lipgloss.Style {
	if !healthy {
		return StatusStoppedStyle
	}
	if !ready {
		return StatusWarningStyle
	}
	return StatusRunningStyle
}

// getComponentStyle returns the style for a component status.
func (m *HealthMonitorModel) getComponentStyle(status string) lipgloss.Style {
	switch status {
	case "ok":
		return StatusRunningStyle
	case statusStarting:
		return StatusWarningStyle
	default:
		return StatusStoppedStyle
	}
}
