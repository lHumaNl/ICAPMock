package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icap-mock/icap-mock/internal/tui/components"
	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
)

func TestInitialModel(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		version string
		cfg     *state.ClientConfig
		check   func(*testing.T, *Model)
	}{
		{
			name:    "default initialization",
			appName: "test-app",
			version: "1.0.0",
			cfg:     &state.ClientConfig{},
			check: func(t *testing.T, m *Model) {
				assert.NotNil(t, m.header)
				assert.NotNil(t, m.footer)
				assert.NotNil(t, m.tabs)
				assert.NotNil(t, m.layout)
				assert.NotNil(t, m.dashboard)
				assert.NotNil(t, m.healthMonitor)
				assert.NotNil(t, m.serviceControls)
				assert.NotNil(t, m.configEditor)
				assert.NotNil(t, m.logViewer)
				assert.NotNil(t, m.scenarioManager)
				assert.NotNil(t, m.replayPanel)
				assert.NotNil(t, m.metricsState)
				assert.NotNil(t, m.logsState)
				assert.NotNil(t, m.serverStatus)
			},
		},
		{
			name:    "with configuration",
			appName: "my-app",
			version: "2.0.0",
			cfg: &state.ClientConfig{
				MetricsURL: "http://localhost:8080/metrics",
				LogsURL:    "http://localhost:8080/logs",
				StatusURL:  "http://localhost:8080/status",
			},
			check: func(t *testing.T, m *Model) {
				assert.Equal(t, ScreenDashboard, m.currentScreen)
				assert.Equal(t, ScreenDashboard, m.previousScreen)
				assert.Equal(t, "my-app", m.appName)
				assert.Equal(t, "2.0.0", m.version)
				assert.False(t, m.ready)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel(tt.appName, tt.version, tt.cfg)
			tt.check(t, model)
		})
	}
}

func TestScreen_String(t *testing.T) {
	tests := []struct {
		screen Screen
		want   string
	}{
		{ScreenDashboard, "Dashboard"},
		{ScreenConfig, "Config Editor"},
		{ScreenScenarios, "Scenarios"},
		{ScreenLogs, "Logs"},
		{ScreenReplay, "Replay"},
		{ScreenHealth, "Health Monitor"},
		{Screen(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.screen.String())
		})
	}
}

func TestModel_Init(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	cmd := model.Init()
	assert.NotNil(t, cmd)

	assert.NotNil(t, model.tickerCancel)
	assert.NotNil(t, model.shutdownDone)
}

func TestModel_Init_Ticker(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.Init()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tickerCmd := model.tickCmd(ctx)
	assert.NotNil(t, tickerCmd)
}

func TestModel_Cleanup(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.Init()

	assert.NotNil(t, model.tickerCancel)

	model.Cleanup()

	assert.NotPanics(t, func() {
		model.Cleanup()
	})
}

func TestModel_Cleanup_NilCancel(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.tickerCancel = nil

	assert.NotPanics(t, func() {
		model.Cleanup()
	})
}

func TestModel_Update_KeyMsg_Quit(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"ctrl+c", "ctrl+c"},
		{"q", "q"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})

			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})

			assert.NotNil(t, cmd)
		})
	}
}

func TestModel_Update_KeyMsg_ScreenNavigation(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		expectedScreen Screen
	}{
		{"navigate to dashboard", "1", ScreenDashboard},
		{"navigate to config", "2", ScreenConfig},
		{"navigate to scenarios", "3", ScreenScenarios},
		{"navigate to logs", "4", ScreenLogs},
		{"navigate to replay", "5", ScreenReplay},
		{"navigate to health", "6", ScreenHealth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})

			newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			typedModel := newModel.(*Model)

			assert.Equal(t, tt.expectedScreen, typedModel.currentScreen)
		})
	}
}

func TestModel_Update_KeyMsg_Escape(t *testing.T) {
	tests := []struct {
		name           string
		initialScreen  Screen
		previousScreen Screen
		expectedScreen Screen
	}{
		{"escape from config to dashboard", ScreenConfig, ScreenDashboard, ScreenDashboard},
		{"escape from logs to dashboard", ScreenLogs, ScreenDashboard, ScreenDashboard},
		{"escape from replay to dashboard", ScreenReplay, ScreenDashboard, ScreenDashboard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			model.currentScreen = tt.initialScreen
			model.previousScreen = tt.previousScreen

			newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			typedModel := newModel.(*Model)

			assert.Equal(t, tt.expectedScreen, typedModel.currentScreen)
		})
	}
}

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, cmd := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, 100, typedModel.width)
	assert.Equal(t, 50, typedModel.height)
	assert.True(t, typedModel.ready)
	// cmd may be nil if no component returns a command
	_ = cmd
}

func TestModel_Update_TickMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := TickMsg{Time: time.Now()}
	newModel, cmd := model.Update(msg)

	assert.NotNil(t, cmd)
	_ = newModel
}

func TestModel_Update_MetricsUpdatedMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	snapshot := &state.MetricsSnapshot{
		Timestamp:     time.Now(),
		RPS:           100.5,
		LatencyP50:    10.0,
		LatencyP95:    25.0,
		LatencyP99:    50.0,
		Connections:   10,
		Errors:        1,
		BytesSent:     1024,
		BytesReceived: 2048,
	}

	msg := state.MetricsUpdatedMsg{Data: snapshot}
	newModel, cmd := model.Update(msg)
	typedModel := newModel.(*Model)

	// cmd may be nil if dashboard doesn't return a command
	_ = cmd
	assert.Equal(t, snapshot, typedModel.metricsState.GetCurrent())
}

func TestModel_Update_ServerStatusMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	status := state.ServerStatusInfo{
		State:  "running",
		Port:   "1344",
		Uptime: "5m",
		Error:  "",
	}

	msg := state.ServerStatusMsg{Status: status}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, "running", typedModel.serverStatus.Current().State)
}

func TestModel_Update_HealthCheckMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     5,
	}

	msg := state.HealthCheckMsg{Result: result}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotNil(t, typedModel.healthMonitor)
}

func TestModel_Update_ServerControlMsg_Success(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	status := state.ServerStatusInfo{
		State:  "running",
		Port:   "1344",
		Uptime: "1m",
	}

	msg := state.ServerControlMsg{
		Action:  "start",
		Success: true,
		Status:  status,
	}

	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotEmpty(t, typedModel.lastMessage)
	assert.Contains(t, typedModel.lastMessage, "successful")
}

func TestModel_Update_ServerControlMsg_Error(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := state.ServerControlMsg{
		Action:  "start",
		Success: false,
		Error:   "connection refused",
	}

	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Contains(t, typedModel.lastMessage, "failed")
	assert.Contains(t, typedModel.lastMessage, "connection refused")
}

func TestModel_Update_ErrorMessage(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := ErrorMessage{Err: assert.AnError}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.NotEmpty(t, typedModel.lastMessage)
	assert.Contains(t, typedModel.lastMessage, "Error:")
}

func TestModel_Update_SuccessMessage(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := SuccessMsg{Message: "Operation completed successfully"}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, "Operation completed successfully", typedModel.lastMessage)
}

func TestModel_Update_ConfigChangedMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := ConfigChangedMsg{Config: &ConfigSnapshot{}}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, "Configuration changed", typedModel.lastMessage)
}

func TestModel_Update_ScreenChangeMsg(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	msg := ScreenChangeMsg{Screen: ScreenLogs}
	newModel, _ := model.Update(msg)
	typedModel := newModel.(*Model)

	assert.Equal(t, ScreenLogs, typedModel.currentScreen)
}

func TestModel_Update_ConfigSavedMsg(t *testing.T) {
	tests := []struct {
		name     string
		msg      ConfigSavedMsg
		expected string
	}{
		{
			name: "save success",
			msg: ConfigSavedMsg{
				FilePath: "/path/to/config.yaml",
				Success:  true,
			},
			expected: "Configuration saved successfully to /path/to/config.yaml",
		},
		{
			name: "save failure",
			msg: ConfigSavedMsg{
				FilePath: "/path/to/config.yaml",
				Success:  false,
				Error:    "permission denied",
			},
			expected: "Failed to save configuration: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			model.configEditor.SetLoading(true)

			newModel, _ := model.Update(tt.msg)
			typedModel := newModel.(*Model)

			assert.False(t, typedModel.configEditor.IsModified())
			assert.Equal(t, tt.expected, typedModel.lastMessage)
		})
	}
}

func TestModel_changeScreen(t *testing.T) {
	tests := []struct {
		name           string
		currentScreen  Screen
		targetScreen   Screen
		expectedScreen Screen
	}{
		{
			name:           "change to different screen",
			currentScreen:  ScreenDashboard,
			targetScreen:   ScreenConfig,
			expectedScreen: ScreenConfig,
		},
		{
			name:           "change to same screen",
			currentScreen:  ScreenDashboard,
			targetScreen:   ScreenDashboard,
			expectedScreen: ScreenDashboard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("test", "1.0", &state.ClientConfig{})
			model.currentScreen = tt.currentScreen

			newModel, _ := model.changeScreen(tt.targetScreen)
			typedModel := newModel.(*Model)

			assert.Equal(t, tt.expectedScreen, typedModel.currentScreen)
			assert.Equal(t, tt.currentScreen, typedModel.previousScreen)
		})
	}
}

func TestModel_changeScreen_Replay(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.currentScreen = ScreenDashboard

	newModel, cmd := model.changeScreen(ScreenReplay)
	typedModel := newModel.(*Model)

	assert.Equal(t, ScreenReplay, typedModel.currentScreen)
	assert.NotNil(t, cmd)
}

func TestModel_loadReplayRequestsCmd(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	cmd := model.loadReplayRequestsCmd()
	assert.NotNil(t, cmd)

	msg := cmd()
	// Without a configured replay client, we expect either an error or empty list
	switch msg.(type) {
	case components.ReplayListMsg:
		// OK - got a list (possibly empty)
	case components.ReplayErrorMsg:
		// OK - no replay client configured
	default:
		t.Errorf("unexpected message type: %T", msg)
	}
}

func TestModel_Shutdown(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.Init()

	assert.NotNil(t, model.tickerCancel)
	assert.NotNil(t, model.shutdownDone)

	model.Shutdown()

	// Verify shutdown cleaned up resources
	assert.Nil(t, model.tickerCancel)
	assert.Nil(t, model.shutdownDone)
}

func TestModel_Shutdown_Nil(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})

	// Shutdown without init should not panic
	assert.NotPanics(t, func() {
		model.Shutdown()
	})
}

func TestModel_Update_ShutdownSignal(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.Init()

	assert.NotNil(t, model.tickerCancel)

	// Send shutdown signal
	newModel, _ := model.Update(ShutdownSignal{})
	typedModel := newModel.(*Model)

	// Verify shutdown was called
	assert.Nil(t, typedModel.tickerCancel)
}

func TestModel_Shutdown_Multiple(t *testing.T) {
	model := InitialModel("test", "1.0", &state.ClientConfig{})
	model.Init()

	// Multiple shutdowns should not panic
	assert.NotPanics(t, func() {
		model.Shutdown()
		model.Shutdown()
		model.Shutdown()
	})
}
