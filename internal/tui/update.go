// Copyright 2026 ICAP Mock

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/icap-mock/icap-mock/internal/tui/components"
	"github.com/icap-mock/icap-mock/internal/tui/server"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

const keyEsc = "esc"

// Update handles incoming messages and updates the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Forward all keys to log viewer when it's in search mode
		if m.currentScreen == ScreenLogs && m.logViewer != nil && m.logViewer.IsSearching() {
			var cmd tea.Cmd
			m.logViewer, cmd = m.logViewer.Update(msg)
			return m, cmd
		}

		// Dismiss help overlay on ? or Esc
		if m.showHelp {
			if msg.String() == "?" || msg.String() == keyEsc {
				m.showHelp = false
				return m, nil
			}
			// Swallow all other keys while help is shown
			return m, nil
		}

		// Reset confirmExit on any key other than Esc
		if msg.String() != keyEsc {
			m.confirmExit = false
		}

		// Handle global key bindings
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Sequence(
				func() tea.Msg { return ShutdownSignal{} },
				tea.Quit,
			)

		case "q":
			// Don't quit when text input is active (config editor or log search)
			if m.currentScreen == ScreenConfig && m.configEditor != nil {
				break
			}
			return m, tea.Sequence(
				func() tea.Msg { return ShutdownSignal{} },
				tea.Quit,
			)

		case "s":
			// Start replay (only on replay screen)
			if m.currentScreen == ScreenReplay && m.replayPanel != nil {
				var cmd tea.Cmd
				m.replayPanel, cmd = m.replayPanel.Update(msg)
				return m, cmd
			}
			// Start server (only on dashboard)
			if m.currentScreen == ScreenDashboard && m.serviceControls != nil {
				m.serviceControls.SetLoading(true)
				cmds = append(cmds, m.serverStatus.Start())
			}

		case "t":
			// Stop replay (only on replay screen)
			if m.currentScreen == ScreenReplay && m.replayPanel != nil {
				var cmd tea.Cmd
				m.replayPanel, cmd = m.replayPanel.Update(msg)
				return m, cmd
			}
			// Stop server (only on dashboard)
			if m.currentScreen == ScreenDashboard && m.serviceControls != nil {
				m.serviceControls.SetLoading(true)
				cmds = append(cmds, m.serverStatus.Stop())
			}

		case "ctrl+s":
			// Save config (only on config screen)
			if m.currentScreen == ScreenConfig && m.configEditor != nil {
				if m.configEditor.IsModified() {
					m.configEditor.SetLoading(true)
					cmds = append(cmds, m.saveConfigCmd())
					m.lastMessage = "Saving configuration..."
				} else {
					m.lastMessage = "No changes to save"
				}
			}

		case "r":
			// Restart server (only on dashboard)
			if m.currentScreen == ScreenDashboard && m.serviceControls != nil {
				m.serviceControls.SetLoading(true)
				cmds = append(cmds, m.serverStatus.Restart())
			}

		case "f":
			// Cycle log filter level (only on logs screen)
			if m.currentScreen == ScreenLogs && m.logViewer != nil {
				levels := []string{"", "DEBUG", "INFO", "WARN", "ERROR"}
				current := m.logViewer.GetFilter().Level
				nextIdx := 0
				for i, l := range levels {
					if l == current {
						nextIdx = (i + 1) % len(levels)
						break
					}
				}
				m.logViewer.SetFilter(&components.LogFilter{Level: levels[nextIdx]})
				displayLevel := levels[nextIdx]
				if displayLevel == "" {
					displayLevel = "ALL"
				}
				m.lastMessage = fmt.Sprintf("Filter: %s", displayLevel)
			}

		case "/":
			// Toggle search mode (only on logs screen)
			if m.currentScreen == ScreenLogs && m.logViewer != nil {
				m.lastMessage = "Search mode activated"
				var cmd tea.Cmd
				m.logViewer, cmd = m.logViewer.Update(msg)
				return m, cmd
			}

		case "a":
			// Toggle auto-scroll (only on logs screen)
			if m.currentScreen == ScreenLogs && m.logViewer != nil {
				enabled := !m.logViewer.IsAutoScrollEnabled()
				m.logViewer.SetAutoScroll(enabled)
				m.lastMessage = fmt.Sprintf("Auto-scroll: %t", enabled)
			}

		case "?":
			m.showHelp = true
			return m, nil

		case "1":
			newModel, _ := m.changeScreen(ScreenDashboard)
			return newModel, nil
		case "2":
			newModel, _ := m.changeScreen(ScreenConfig)
			return newModel, nil
		case "3":
			newModel, _ := m.changeScreen(ScreenScenarios)
			return newModel, nil
		case "4":
			newModel, _ := m.changeScreen(ScreenLogs)
			return newModel, nil
		case "5":
			newModel, _ := m.changeScreen(ScreenReplay)
			return newModel, nil
		case "6":
			newModel, _ := m.changeScreen(ScreenHealth)
			return newModel, nil

		case "tab":
			// Cycle to next screen
			next := m.currentScreen + 1
			if next > ScreenHealth {
				next = ScreenDashboard
			}
			newModel, _ := m.changeScreen(next)
			return newModel, nil

		case "shift+tab":
			// Cycle to previous screen
			prev := m.currentScreen - 1
			if prev < ScreenDashboard {
				prev = ScreenHealth
			}
			newModel, _ := m.changeScreen(prev)
			return newModel, nil

		case keyEsc:
			if m.currentScreen != ScreenDashboard {
				// Check for unsaved changes on config screen
				if m.currentScreen == ScreenConfig && m.configEditor != nil && m.configEditor.IsModified() {
					if !m.confirmExit {
						m.confirmExit = true
						m.lastMessage = "Unsaved changes! Press Esc again to discard, Ctrl+S to save"
						return m, nil
					}
					m.confirmExit = false
				}
				newModel, _ := m.changeScreen(m.previousScreen)
				return newModel, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Propagate resize to dashboard
		if m.dashboard != nil {
			var cmd tea.Cmd
			m.dashboard, cmd = m.dashboard.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Propagate resize to config editor
		if m.configEditor != nil {
			m.configEditor.SetWindowSize(m.width, m.height)
		}

		// Propagate resize to scenario manager
		if m.scenarioManager != nil {
			var cmd tea.Cmd
			m.scenarioManager, cmd = m.scenarioManager.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Propagate resize to log viewer
		if m.logViewer != nil {
			var cmd tea.Cmd
			m.logViewer, cmd = m.logViewer.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Propagate resize to replay panel
		if m.replayPanel != nil {
			var cmd tea.Cmd
			m.replayPanel, cmd = m.replayPanel.Update(msg)
			cmds = append(cmds, cmd)
		}

		return m, tea.Batch(cmds...)

	case ShutdownSignal:
		// Gracefully shutdown all components
		m.Shutdown()

	case TickMsg:
		// Periodic update - refresh metrics and status
		cmds = append(cmds,
			m.metricsState.Refresh(),
			m.serverStatus.Check(),
		)

		// Refresh logs every 2 seconds
		if msg.Time.Second()%2 == 0 {
			cmds = append(cmds, m.logsState.Refresh())
		}

		// Perform health checks every 5 seconds
		if msg.Time.Second()%5 == 0 {
			cmds = append(cmds, m.serverStatus.CheckHealth())
		}

	case state.MetricsUpdatedMsg:
		m.metricsState.Update(msg.Data)

		// Update dashboard with new metrics
		if m.dashboard != nil {
			var cmd tea.Cmd
			m.dashboard, cmd = m.dashboard.Update(msg)
			cmds = append(cmds, cmd)
		}

	case state.LogEntryMsg:
		m.logsState.AddEntry(msg.Entry)

		// Update dashboard with new log entry
		if m.dashboard != nil {
			m.dashboard.AddLogEntry(msg.Entry)
		}

		// Update log viewer with new log entry
		if m.logViewer != nil {
			logs := m.logsState.GetEntries(nil, 1000)
			m.logViewer.SetEntries(logs)
		}

	case state.LogsUpdatedMsg:
		m.logsState.UpdateEntries(msg.Entries)

		// Update log viewer with refreshed logs
		if m.logViewer != nil {
			logs := m.logsState.GetEntries(nil, 1000)
			m.logViewer.SetEntries(logs)
		}

	case state.ServerStatusMsg:
		m.serverStatus.Update(msg.Status)

	case state.HealthCheckMsg:
		// Update health monitor with new health check result
		if m.healthMonitor != nil {
			m.healthMonitor.UpdateHealthCheck(msg.Result)
		}

	case state.ServerControlMsg:
		// Handle server control operation result
		if m.serviceControls != nil {
			m.serviceControls.SetLoading(false)
		}

		if msg.Success {
			m.serverStatus.Update(msg.Status)
			m.lastMessage = fmt.Sprintf("Server %s successful", msg.Action)
		} else {
			m.lastMessage = fmt.Sprintf("Server %s failed: %s", msg.Action, msg.Error)
		}

	case state.ServerConfigMsg:
		// Handle server configuration retrieved
		m.lastMessage = "Server configuration loaded"

	case state.ErrorMessage:
		// Handle error from server operations
		if m.serviceControls != nil {
			m.serviceControls.SetLoading(false)
		}
		errText := msg.Err.Error()
		if strings.Contains(errText, "connection refused") {
			errText = fmt.Sprintf("%s (server: %s)", errText, m.serverStatusURL)
		}
		m.lastMessage = "Error: " + errText

	case ConfigChangedMsg:
		// Handle configuration change
		m.lastMessage = "Configuration changed"

	case ScreenChangeMsg:
		newModel, _ := m.changeScreen(msg.Screen)
		return newModel, nil

	case ErrorMessage:
		errText := msg.Err.Error()
		if strings.Contains(errText, "connection refused") {
			errText = fmt.Sprintf("%s (server: %s)", errText, m.serverStatusURL)
		}
		m.lastMessage = "Error: " + errText

	case SuccessMsg:
		m.lastMessage = msg.Message

	case components.ScenarioListMsg:
		// Handle scenario list received
		if m.scenarioManager != nil {
			m.scenarioManager.SetScenarios(msg.Scenarios)
		}

	case components.ScenarioDeleteMsg:
		// Handle scenario deletion
		m.lastMessage = fmt.Sprintf("Deleting scenario: %s", msg.ScenarioName)
		cmds = append(cmds, m.deleteScenarioCmd(msg.ScenarioName))

	case components.ScenarioUpdateMsg:
		// Handle scenario update
		m.lastMessage = fmt.Sprintf("Updating scenario: %s", msg.ScenarioName)
		cmds = append(cmds, m.updateScenarioCmd(msg.ScenarioName, msg.Name))

	case components.ScenarioCreateMsg:
		// Handle scenario creation
		m.lastMessage = fmt.Sprintf("Creating scenario: %s", msg.Name)
		cmds = append(cmds, m.createScenarioCmd(msg.Name))

	case components.ScenarioReloadMsg:
		// Handle scenario reload
		m.lastMessage = "Reloading scenarios..."
		cmds = append(cmds, m.reloadScenariosCmd())

	case components.ScenarioErrorMsg:
		// Handle scenario error
		m.lastMessage = "Scenario error: " + msg.Err.Error()

	case components.ReplayListMsg:
		// Handle replay list received
		if m.replayPanel != nil {
			m.replayPanel.SetRequests(msg.Requests)
		}

	case components.ReplayStartMsg:
		// Handle replay start
		m.lastMessage = fmt.Sprintf("Starting replay with %d requests at %.1fx speed",
			len(msg.Requests), msg.Speed)

	case components.ReplayProgressMsg:
		// Handle replay progress update
		if m.replayPanel != nil {
			m.replayPanel.UpdateProgress(msg.Current, msg.Total)
		}

	case components.ReplayCompleteMsg:
		// Handle replay completion
		if m.replayPanel != nil {
			m.replayPanel.SetReplayResults(msg.Results)
			m.lastMessage = fmt.Sprintf("Replay complete: %d succeeded, %d failed",
				msg.Results.SuccessCount, msg.Results.FailureCount)
		}

	case components.ReplayStopMsg:
		// Handle replay stop
		m.lastMessage = "Replay stopped"

	case components.ReplayExportMsg:
		// Handle replay export
		m.lastMessage = "Exporting replay report..."
		// Here you would make an HTTP call to export the report

	case components.ReplayErrorMsg:
		// Handle replay error
		m.lastMessage = "Replay error: " + msg.Err.Error()

	case ConfigSavedMsg:
		// Handle configuration save result
		if m.configEditor != nil {
			m.configEditor.SetLoading(false)
		}

		if msg.Success {
			m.lastMessage = "Configuration saved successfully to " + msg.FilePath
			if m.configEditor != nil {
				_ = m.configEditor.SetContent(m.configEditor.GetContent(), msg.FilePath)
			}
		} else {
			m.lastMessage = "Failed to save configuration: " + msg.Error
		}
	}

	return m, tea.Batch(cmds...)
}

// changeScreen switches to a different screen.
func (m *Model) changeScreen(screen Screen) (tea.Model, tea.Cmd) {
	if m.currentScreen == screen {
		return m, nil
	}

	m.previousScreen = m.currentScreen
	m.currentScreen = screen

	// Load replay requests when switching to replay screen
	if screen == ScreenReplay {
		cmd := m.loadReplayRequestsCmd()
		return m, cmd
	}

	return m, nil
}

// loadReplayRequestsCmd loads replay requests from the server.
func (m *Model) loadReplayRequestsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.replayClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			reqs, err := m.replayClient.ListRequests(ctx)
			if err != nil {
				return components.ReplayErrorMsg{Err: fmt.Errorf("failed to load replay requests: %w", err)}
			}

			items := make([]components.ReplayRequestItem, len(reqs))
			for i, r := range reqs {
				items[i] = components.ReplayRequestItem{
					ID:         r.ID,
					Timestamp:  r.Timestamp,
					Method:     r.Method,
					Path:       r.Path,
					StatusCode: r.StatusCode,
					Response:   r.Response,
					Duration:   r.Duration,
				}
			}

			return components.ReplayListMsg{Requests: items}
		}

		// No replay client available — return empty state
		return components.ReplayListMsg{Requests: nil}
	}
}

// saveConfigCmd saves the current configuration.
func (m *Model) saveConfigCmd() tea.Cmd {
	return func() tea.Msg {
		if m.configEditor == nil {
			return ConfigSavedMsg{
				Success: false,
				Error:   "Config editor not initialized",
			}
		}

		filePath := m.configEditor.GetFilePath()
		content := m.configEditor.GetContent()

		if filePath == "" {
			return ConfigSavedMsg{
				Success: false,
				Error:   "No file path specified",
			}
		}

		// Try to save to server first
		if m.configClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			savedPath, err := m.configClient.SaveConfig(ctx, content, filePath)
			if err != nil {
				return ConfigSavedMsg{
					Success: false,
					Error:   fmt.Sprintf("Failed to save config: %v", err),
				}
			}

			// Use returned path or fallback to original
			if savedPath == "" {
				savedPath = filePath
			}

			return ConfigSavedMsg{
				FilePath: savedPath,
				Success:  true,
			}
		}

		// No client available, return error
		return ConfigSavedMsg{
			Success: false,
			Error:   "Config client not available",
		}
	}
}

// deleteScenarioCmd deletes a scenario.
func (m *Model) deleteScenarioCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.scenarioClient == nil {
			return components.ScenarioErrorMsg{Err: fmt.Errorf("scenario client not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := m.scenarioClient.DeleteScenario(ctx, name)
		if err != nil {
			return components.ScenarioErrorMsg{Err: err}
		}

		return components.ScenarioReloadMsg{}
	}
}

// updateScenarioCmd updates a scenario.
func (m *Model) updateScenarioCmd(oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		if m.scenarioClient == nil {
			return components.ScenarioErrorMsg{Err: fmt.Errorf("scenario client not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		scenario := server.APIScenario{
			Name:   newName,
			Config: map[string]interface{}{},
		}

		err := m.scenarioClient.UpdateScenario(ctx, oldName, scenario)
		if err != nil {
			return components.ScenarioErrorMsg{Err: err}
		}

		return components.ScenarioReloadMsg{}
	}
}

// createScenarioCmd creates a new scenario.
func (m *Model) createScenarioCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.scenarioClient == nil {
			return components.ScenarioErrorMsg{Err: fmt.Errorf("scenario client not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		scenario := server.APIScenario{
			Name:   name,
			Config: map[string]interface{}{},
		}

		err := m.scenarioClient.CreateScenario(ctx, scenario)
		if err != nil {
			return components.ScenarioErrorMsg{Err: err}
		}

		return components.ScenarioReloadMsg{}
	}
}

// reloadScenariosCmd reloads scenarios from server.
func (m *Model) reloadScenariosCmd() tea.Cmd {
	return func() tea.Msg {
		if m.scenarioClient == nil {
			return components.ScenarioErrorMsg{Err: fmt.Errorf("scenario client not available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := m.scenarioClient.ReloadScenarios(ctx)
		if err != nil {
			return components.ScenarioErrorMsg{Err: err}
		}

		scenarios, err := m.scenarioClient.ListScenarios(ctx)
		if err != nil {
			return components.ScenarioErrorMsg{Err: err}
		}

		scenarioItems := make([]components.ScenarioItem, len(scenarios))
		for i, s := range scenarios {
			scenarioItems[i] = components.ScenarioItem{
				Name:     s.Name,
				Priority: s.Priority,
				Method:   s.Method,
				Path:     "",
			}
		}

		return components.ScenarioListMsg{Scenarios: scenarioItems}
	}
}
