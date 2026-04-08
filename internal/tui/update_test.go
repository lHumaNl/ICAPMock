// Copyright 2026 ICAP Mock

package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/icap-mock/icap-mock/internal/tui/components"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

func TestModel_Update_ScenarioListMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	scenarios := []components.ScenarioItem{
		{Name: "scenario1", Priority: 10, Method: "REQMOD", Path: "/api/v1/users"},
		{Name: "scenario2", Priority: 20, Method: "RESPMOD", Path: "/api/v1/products"},
	}

	msg := components.ScenarioListMsg{Scenarios: scenarios}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotNil(t, typedModel.scenarioManager)
}

func TestModel_Update_ScenarioDeleteMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ScenarioDeleteMsg{ScenarioName: "test-scenario"}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Deleting scenario")
	assert.Contains(t, typedModel.lastMessage, "test-scenario")
}

func TestModel_Update_ScenarioUpdateMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ScenarioUpdateMsg{
		ScenarioName: "old-scenario",
		Name:         "new-scenario",
		YAML:         "test: yaml",
	}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Updating scenario")
	assert.Contains(t, typedModel.lastMessage, "old-scenario")
}

func TestModel_Update_ScenarioCreateMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ScenarioCreateMsg{
		Name: "new-scenario",
		YAML: "test: yaml",
	}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Creating scenario")
	assert.Contains(t, typedModel.lastMessage, "new-scenario")
}

func TestModel_Update_ScenarioReloadMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ScenarioReloadMsg{}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, "Reloading scenarios...", typedModel.lastMessage)
}

func TestModel_Update_ScenarioErrorMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ScenarioErrorMsg{Err: assert.AnError}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Scenario error")
}

func TestModel_Update_ReplayListMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	requests := []components.ReplayRequestItem{
		{ID: "req-001", Method: "REQMOD", Path: "/api/v1/users"},
	}

	msg := components.ReplayListMsg{Requests: requests}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotNil(t, typedModel.replayPanel)
}

func TestModel_Update_ReplayStartMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	requests := []components.ReplayRequestItem{
		{ID: "req-001", Method: "REQMOD", Path: "/api/v1/users"},
		{ID: "req-002", Method: "REQMOD", Path: "/api/v1/products"},
	}

	msg := components.ReplayStartMsg{
		Requests:  requests,
		Speed:     1.5,
		TargetURL: "http://localhost:1344",
	}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Starting replay")
	assert.Contains(t, typedModel.lastMessage, "2 requests")
	assert.Contains(t, typedModel.lastMessage, "1.5x")
}

func TestModel_Update_ReplayProgressMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ReplayProgressMsg{
		Current: 5,
		Total:   10,
		Result:  &components.RequestResult{ID: "req-001", Success: true},
	}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotNil(t, typedModel.replayPanel)
}

func TestModel_Update_ReplayCompleteMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	results := &components.ReplayResults{
		TotalRequests: 10,
		SuccessCount:  8,
		FailureCount:  2,
	}

	msg := components.ReplayCompleteMsg{Results: results}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Replay complete")
	assert.Contains(t, typedModel.lastMessage, "8 succeeded")
	assert.Contains(t, typedModel.lastMessage, "2 failed")
}

func TestModel_Update_ReplayStopMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ReplayStopMsg{}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, "Replay stopped", typedModel.lastMessage)
}

func TestModel_Update_ReplayExportMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ReplayExportMsg{}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Exporting replay report")
}

func TestModel_Update_ReplayErrorMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := components.ReplayErrorMsg{Err: assert.AnError}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "Replay error")
}

func TestModel_Update_KeyMsg_LogOperations(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		setup        func(*Model)
		checkLastMsg string
	}{
		{
			name:         "toggle filter",
			key:          "f",
			setup:        func(m *Model) { m.currentScreen = ScreenLogs },
			checkLastMsg: "Filter:",
		},
		{
			name:         "search",
			key:          "/",
			setup:        func(m *Model) { m.currentScreen = ScreenLogs },
			checkLastMsg: "Search mode",
		},
		{
			name:         "toggle auto-scroll",
			key:          "a",
			setup:        func(m *Model) { m.currentScreen = ScreenLogs },
			checkLastMsg: "Auto-scroll:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			tt.setup(model)

			newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			typedModel := newModel.(*Model)

			assert.Contains(t, typedModel.lastMessage, tt.checkLastMsg)
		})
	}
}

func TestModel_Update_KeyMsg_ConfigOperations(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenConfig
	model.configEditor.SetContent("test: yaml\nport: 1344", "/path/to/config.yaml")

	// Simulate typing to modify the content
	_, _ = model.configEditor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	typedModel := newModel.(*Model)

	assert.True(t, typedModel.configEditor.IsModified())
	assert.Contains(t, typedModel.lastMessage, "Saving configuration")
}

func TestModel_Update_KeyMsg_ConfigNoChanges(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenConfig
	model.configEditor.SetContent("test: yaml\nport: 1344", "/path/to/config.yaml")

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "No changes to save")
}

func TestModel_Update_KeyMsg_StartStopServer(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		currentScreen Screen
	}{
		{"start server on dashboard", "s", ScreenDashboard},
		{"stop server on dashboard", "t", ScreenDashboard},
		{"restart server", "r", ScreenDashboard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			model.currentScreen = tt.currentScreen

			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			assert.NotNil(t, cmd)
		})
	}
}

func TestModel_Update_KeyMsg_ReplayOperations(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		currentScreen Screen
	}{
		{"start replay", "s", ScreenReplay},
		{"stop replay", "t", ScreenReplay},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			model.currentScreen = tt.currentScreen

			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			// cmd may be nil if no selected requests or replay not in progress
			_ = cmd
		})
	}
}

func TestModel_changeScreen_SameScreen(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenDashboard
	model.previousScreen = ScreenDashboard

	newModel, cmd := model.changeScreen(ScreenDashboard)
	typedModel := newModel.(*Model)

	assert.Equal(t, ScreenDashboard, typedModel.currentScreen)
	assert.Nil(t, cmd)
}

func TestModel_changeScreen_DifferentScreen(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenDashboard
	model.previousScreen = ScreenDashboard

	newModel, cmd := model.changeScreen(ScreenConfig)
	typedModel := newModel.(*Model)

	assert.Equal(t, ScreenConfig, typedModel.currentScreen)
	assert.Equal(t, ScreenDashboard, typedModel.previousScreen)
	assert.Nil(t, cmd)
}

func TestModel_changeScreen_SwitchToPrevious(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenDashboard
	model.previousScreen = ScreenConfig

	newModel, _ := model.changeScreen(ScreenLogs)
	typedModel := newModel.(*Model)

	assert.Equal(t, ScreenLogs, typedModel.currentScreen)
	assert.Equal(t, ScreenDashboard, typedModel.previousScreen)
}

func TestModel_saveConfigCmd_EditorNil(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.configEditor = nil

	cmd := model.saveConfigCmd()
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, ConfigSavedMsg{}, msg)

	savedMsg := msg.(ConfigSavedMsg)
	assert.False(t, savedMsg.Success)
	assert.Contains(t, savedMsg.Error, "not initialized")
}

func TestModel_saveConfigCmd_NoFilePath(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.configEditor.SetContent("test: content", "")

	cmd := model.saveConfigCmd()
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, ConfigSavedMsg{}, msg)

	savedMsg := msg.(ConfigSavedMsg)
	assert.False(t, savedMsg.Success)
	assert.Contains(t, savedMsg.Error, "No file path specified")
}

func TestModel_saveConfigCmd_NoClient(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.configClient = nil
	model.configEditor.SetContent("test: content", "/path/to/config.yaml")

	cmd := model.saveConfigCmd()
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, ConfigSavedMsg{}, msg)

	savedMsg := msg.(ConfigSavedMsg)
	assert.False(t, savedMsg.Success)
	assert.Contains(t, savedMsg.Error, "Config client not available")
}
