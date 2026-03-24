package components

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServiceControlsModel(t *testing.T) {
	model := NewServiceControlsModel()

	assert.NotNil(t, model)
	assert.Equal(t, "unknown", model.serverStatus)
	assert.Equal(t, "N/A", model.serverPort)
	assert.Equal(t, "N/A", model.serverUptime)
	assert.False(t, model.loading)
}

func TestServiceControlsModel_SetStatus(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "5m")

	assert.Equal(t, "running", model.serverStatus)
	assert.Equal(t, "1344", model.serverPort)
	assert.Equal(t, "5m", model.serverUptime)
}

func TestServiceControlsModel_SetLoading(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetLoading(true)
	assert.True(t, model.loading)

	model.SetLoading(false)
	assert.False(t, model.loading)
}

func TestServiceControlsModel_View(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "running"
	model.serverPort = "1344"
	model.serverUptime = "5m"

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "RUNNING")
	assert.Contains(t, view, "1344")
	assert.Contains(t, view, "5m")
}

func TestServiceControlsModel_View_Loading(t *testing.T) {
	model := NewServiceControlsModel()
	model.loading = true

	view := model.View()
	assert.Contains(t, view, "Processing...")
}

func TestServiceControlsModel_View_Stopped(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "stopped"

	view := model.View()
	assert.Contains(t, view, "STOPPED")
}

func TestServiceControlsModel_View_Error(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "error"

	view := model.View()
	assert.Contains(t, view, "ERROR")
}

func TestServiceControlsModel_View_Unknown(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "unknown"

	view := model.View()
	assert.Contains(t, view, "UNKNOWN")
}

func TestServiceControlsModel_renderStatus_AllStatuses(t *testing.T) {
	tests := []struct {
		status   string
		contains string
	}{
		{"running", "RUNNING"},
		{"stopped", "STOPPED"},
		{"error", "ERROR"},
		{"unknown", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.serverStatus = tt.status

			indicator := model.renderStatusIndicator()

			assert.NotEmpty(t, indicator)
			assert.Contains(t, indicator, tt.contains)
		})
	}
}

func TestServiceControlsModel_renderServerInfo(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverPort = "1344"
	model.serverUptime = "5m 30s"

	info := model.renderServerInfo()

	assert.NotEmpty(t, info)
	assert.Contains(t, info, "1344")
	assert.Contains(t, info, "5m 30s")
}

func TestServiceControlsModel_renderControls_Loading(t *testing.T) {
	model := NewServiceControlsModel()
	model.loading = true

	controls := model.renderControls()

	assert.NotEmpty(t, controls)
	assert.Contains(t, controls, "Processing...")
}

func TestServiceControlsModel_renderControls_NotLoading(t *testing.T) {
	model := NewServiceControlsModel()
	model.loading = false

	controls := model.renderControls()

	assert.NotEmpty(t, controls)
	assert.Contains(t, controls, "Start")
	assert.Contains(t, controls, "Stop")
	assert.Contains(t, controls, "Restart")
}

func TestServiceControlsModel_renderShortcuts(t *testing.T) {
	model := NewServiceControlsModel()

	shortcuts := model.renderShortcuts()

	assert.NotEmpty(t, shortcuts)
	assert.Contains(t, shortcuts, "start")
	assert.Contains(t, shortcuts, "stop")
	assert.Contains(t, shortcuts, "restart")
}

func TestServiceControlsModel_View_WithAllInfo(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "running"
	model.serverPort = "1344"
	model.serverUptime = "5m 30s"

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "RUNNING")
	assert.Contains(t, view, "1344")
	assert.Contains(t, view, "5m 30s")
	assert.Contains(t, view, "Start")
	assert.Contains(t, view, "Stop")
	assert.Contains(t, view, "Restart")
}

func TestServiceControlsModel_View_WithoutLoading(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "stopped"
	model.loading = false

	view := model.View()

	assert.NotEmpty(t, view)
	assert.NotContains(t, view, "Processing...")
}

func TestServiceControlsModel_AllStatusValues(t *testing.T) {
	model := NewServiceControlsModel()

	statuses := []string{"running", "stopped", "error", "unknown"}

	for _, status := range statuses {
		model.SetStatus(status, "1344", "5m")
		model.serverStatus = status

		view := model.View()

		assert.NotEmpty(t, view)
	}
}

func TestServiceControlsModel_View_KeyboardShortcuts(t *testing.T) {
	model := NewServiceControlsModel()

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Keyboard")
}

func TestServiceControlsModel_multipleStatusChanges(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "5m")
	assert.Equal(t, "running", model.serverStatus)

	model.SetStatus("stopped", "1344", "10m")
	assert.Equal(t, "stopped", model.serverStatus)

	model.SetStatus("running", "1344", "15m")
	assert.Equal(t, "running", model.serverStatus)
}

func TestServiceControlsModel_SetStatus_Concurrent(t *testing.T) {
	model := NewServiceControlsModel()
	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			status := "running"
			if id%2 == 0 {
				status = "stopped"
			}
			model.SetStatus(status, "1344", "5m")
		}(i)
	}

	wg.Wait()

	assert.NotEmpty(t, model.serverStatus)
}

func TestServiceControlsModel_SetLoading_Concurrent(t *testing.T) {
	model := NewServiceControlsModel()
	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			model.SetLoading(id%2 == 0)
		}(i)
	}

	wg.Wait()

	assert.NotNil(t, model.loading)
}

func TestServiceControlsModel_Lifecycle_StartStop(t *testing.T) {
	model := NewServiceControlsModel()

	assert.Equal(t, "unknown", model.serverStatus)
	assert.Equal(t, "N/A", model.serverPort)
	assert.Equal(t, "N/A", model.serverUptime)

	model.SetStatus("running", "1344", "5m")
	assert.Equal(t, "running", model.serverStatus)
	assert.Equal(t, "1344", model.serverPort)
	assert.Equal(t, "5m", model.serverUptime)

	model.SetStatus("stopped", "1344", "0m")
	assert.Equal(t, "stopped", model.serverStatus)
	assert.Equal(t, "1344", model.serverPort)
	assert.Equal(t, "0m", model.serverUptime)
}

func TestServiceControlsModel_Lifecycle_StartErrorStop(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "5m")
	assert.Equal(t, "running", model.serverStatus)

	model.SetStatus("error", "N/A", "N/A")
	assert.Equal(t, "error", model.serverStatus)

	model.SetStatus("stopped", "N/A", "N/A")
	assert.Equal(t, "stopped", model.serverStatus)
}

func TestServiceControlsModel_Lifecycle_Restart(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "10m")
	assert.Equal(t, "running", model.serverStatus)

	model.SetStatus("running", "1344", "0m")
	assert.Equal(t, "running", model.serverStatus)

	model.SetStatus("running", "1344", "5m")
	assert.Equal(t, "running", model.serverStatus)
}

func TestServiceControlsModel_Lifecycle_UnknownToRunning(t *testing.T) {
	model := NewServiceControlsModel()

	assert.Equal(t, "unknown", model.serverStatus)

	model.SetStatus("running", "1344", "1m")
	assert.Equal(t, "running", model.serverStatus)

	model.SetStatus("stopped", "N/A", "N/A")
	assert.Equal(t, "stopped", model.serverStatus)

	model.SetStatus("unknown", "N/A", "N/A")
	assert.Equal(t, "unknown", model.serverStatus)
}

func TestServiceControlsModel_View_AllStatuses_Render(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{"Running status", "running", "RUNNING"},
		{"Stopped status", "stopped", "STOPPED"},
		{"Error status", "error", "ERROR"},
		{"Unknown status", "unknown", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.SetStatus(tt.status, "1344", "5m")

			view := model.View()

			assert.NotEmpty(t, view)
			assert.Contains(t, view, "Service Controls")
			assert.Contains(t, view, tt.expected)
			assert.Contains(t, view, "1344")
			assert.Contains(t, view, "5m")
		})
	}
}

func TestServiceControlsModel_View_LoadingState(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		loading     bool
		contains    string
		notContains string
	}{
		{"Loading with running status", "running", true, "Processing...", ""},
		{"Loading with stopped status", "stopped", true, "Processing...", ""},
		{"Not loading with running status", "running", false, "Start", "Processing..."},
		{"Not loading with stopped status", "stopped", false, "Start", "Processing..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.SetStatus(tt.status, "1344", "5m")
			model.SetLoading(tt.loading)

			view := model.View()

			assert.NotEmpty(t, view)
			assert.Contains(t, view, tt.contains)
			if tt.notContains != "" {
				assert.NotContains(t, view, tt.notContains)
			}
		})
	}
}

func TestServiceControlsModel_renderControls_ButtonStates(t *testing.T) {
	model := NewServiceControlsModel()
	model.loading = false

	controls := model.renderControls()

	assert.NotEmpty(t, controls)
	assert.Contains(t, controls, "[s]")
	assert.Contains(t, controls, "Start")
	assert.Contains(t, controls, "[t]")
	assert.Contains(t, controls, "Stop")
	assert.Contains(t, controls, "[r]")
	assert.Contains(t, controls, "Restart")
}

func TestServiceControlsModel_renderControls_LoadingState(t *testing.T) {
	model := NewServiceControlsModel()
	model.loading = true

	controls := model.renderControls()

	assert.NotEmpty(t, controls)
	assert.Contains(t, controls, "Processing...")
	assert.NotContains(t, controls, "Start")
	assert.NotContains(t, controls, "Stop")
	assert.NotContains(t, controls, "Restart")
}

func TestServiceControlsModel_renderShortcuts_KeyboardBindings(t *testing.T) {
	model := NewServiceControlsModel()

	shortcuts := model.renderShortcuts()

	assert.NotEmpty(t, shortcuts)
	assert.Contains(t, shortcuts, "Keyboard:")
	assert.Contains(t, shortcuts, "s=start")
	assert.Contains(t, shortcuts, "t=stop")
	assert.Contains(t, shortcuts, "r=restart")
	assert.Contains(t, shortcuts, "esc=back")
}

func TestServiceControlsModel_renderServerInfo_Formatting(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		uptime   string
		contains []string
	}{
		{
			name:     "Basic info",
			port:     "1344",
			uptime:   "5m",
			contains: []string{"Port:", "1344", "Uptime:", "5m"},
		},
		{
			name:     "Detailed uptime",
			port:     "8080",
			uptime:   "1h 30m 45s",
			contains: []string{"Port:", "8080", "Uptime:", "1h 30m 45s"},
		},
		{
			name:     "N/A values",
			port:     "N/A",
			uptime:   "N/A",
			contains: []string{"Port:", "N/A", "Uptime:", "N/A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.SetStatus("running", tt.port, tt.uptime)

			info := model.renderServerInfo()

			for _, want := range tt.contains {
				assert.Contains(t, info, want)
			}
		})
	}
}

func TestServiceControlsModel_renderStatusIndicator_Styles(t *testing.T) {
	tests := []struct {
		name   string
		status string
		symbol string
	}{
		{"Running status", "running", "●"},
		{"Stopped status", "stopped", "●"},
		{"Error status", "error", "●"},
		{"Unknown status", "unknown", "●"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.SetStatus(tt.status, "1344", "5m")

			indicator := model.renderStatusIndicator()

			assert.NotEmpty(t, indicator)
			assert.Contains(t, indicator, tt.symbol)
		})
	}
}

func TestServiceControlsModel_EdgeCase_EmptyPort(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "", "5m")

	assert.Equal(t, "", model.serverPort)

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestServiceControlsModel_EdgeCase_EmptyUptime(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "")

	assert.Equal(t, "", model.serverUptime)

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestServiceControlsModel_EdgeCase_VeryLongPort(t *testing.T) {
	model := NewServiceControlsModel()
	longPort := "999999999999999"

	model.SetStatus("running", longPort, "5m")

	assert.Equal(t, longPort, model.serverPort)

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, longPort)
}

func TestServiceControlsModel_EdgeCase_VeryLongUptime(t *testing.T) {
	model := NewServiceControlsModel()
	longUptime := "999999h 999999m 999999s"

	model.SetStatus("running", "1344", longUptime)

	assert.Equal(t, longUptime, model.serverUptime)

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, longUptime)
}

func TestServiceControlsModel_EdgeCase_InvalidStatus(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("invalid_status", "1344", "5m")

	assert.Equal(t, "invalid_status", model.serverStatus)

	indicator := model.renderStatusIndicator()
	assert.NotEmpty(t, indicator)
}

func TestServiceControlsModel_EdgeCase_SpecialCharactersInPort(t *testing.T) {
	model := NewServiceControlsModel()

	specialPort := "1344:8080"
	model.SetStatus("running", specialPort, "5m")

	assert.Equal(t, specialPort, model.serverPort)
}

func TestServiceControlsModel_EdgeCase_SpecialCharactersInUptime(t *testing.T) {
	model := NewServiceControlsModel()

	specialUptime := "5m (30s pending)"
	model.SetStatus("running", "1344", specialUptime)

	assert.Equal(t, specialUptime, model.serverUptime)
}

func TestServiceControlsModel_EdgeCase_NilFields(t *testing.T) {
	model := NewServiceControlsModel()

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Service Controls")
}

func TestServiceControlsModel_EdgeCase_DefaultValues(t *testing.T) {
	model := NewServiceControlsModel()

	assert.Equal(t, "unknown", model.serverStatus)
	assert.Equal(t, "N/A", model.serverPort)
	assert.Equal(t, "N/A", model.serverUptime)
	assert.False(t, model.loading)

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "UNKNOWN")
	assert.Contains(t, view, "N/A")
}

func TestServiceControlsModel_StateTransitions_Valid(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		{"Unknown to Running", "unknown", "running", true},
		{"Running to Stopped", "running", "stopped", true},
		{"Stopped to Running", "stopped", "running", true},
		{"Any to Error", "running", "error", true},
		{"Error to Stopped", "error", "stopped", true},
		{"Stopped to Unknown", "stopped", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewServiceControlsModel()
			model.SetStatus(tt.from, "1344", "5m")
			model.SetStatus(tt.to, "1344", "5m")

			assert.Equal(t, tt.to, model.serverStatus)
		})
	}
}

func TestServiceControlsModel_View_Components(t *testing.T) {
	model := NewServiceControlsModel()
	model.SetStatus("running", "1344", "5m")

	view := model.View()

	assert.Contains(t, view, "Service Controls")
	assert.Contains(t, view, "Port:")
	assert.Contains(t, view, "Uptime:")
	assert.Contains(t, view, "[s] Start")
	assert.Contains(t, view, "[t] Stop")
	assert.Contains(t, view, "[r] Restart")
	assert.Contains(t, view, "Keyboard:")
	assert.Contains(t, view, "s=start")
	assert.Contains(t, view, "t=stop")
	assert.Contains(t, view, "r=restart")
}

func TestServiceControlsModel_View_LoadingWithoutControls(t *testing.T) {
	model := NewServiceControlsModel()
	model.SetStatus("running", "1344", "5m")
	model.SetLoading(true)

	view := model.View()

	assert.Contains(t, view, "Processing...")
	assert.NotContains(t, view, "[s] Start")
	assert.NotContains(t, view, "[t] Stop")
	assert.NotContains(t, view, "[r] Restart")
}

func TestServiceControlsModel_MultipleSetStatusCalls(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetStatus("running", "1344", "5m")
	assert.Equal(t, "running", model.serverStatus)
	assert.Equal(t, "1344", model.serverPort)
	assert.Equal(t, "5m", model.serverUptime)

	model.SetStatus("stopped", "8080", "0m")
	assert.Equal(t, "stopped", model.serverStatus)
	assert.Equal(t, "8080", model.serverPort)
	assert.Equal(t, "0m", model.serverUptime)

	model.SetStatus("error", "N/A", "N/A")
	assert.Equal(t, "error", model.serverStatus)
	assert.Equal(t, "N/A", model.serverPort)
	assert.Equal(t, "N/A", model.serverUptime)
}

func TestServiceControlsModel_SetLoading_MultipleCalls(t *testing.T) {
	model := NewServiceControlsModel()

	model.SetLoading(true)
	assert.True(t, model.loading)

	model.SetLoading(false)
	assert.False(t, model.loading)

	model.SetLoading(true)
	assert.True(t, model.loading)

	model.SetLoading(false)
	assert.False(t, model.loading)
}

func TestServiceControlsModel_View_Reloading(t *testing.T) {
	model := NewServiceControlsModel()
	model.SetStatus("running", "1344", "5m")

	view1 := model.View()
	assert.NotEmpty(t, view1)

	model.SetStatus("stopped", "1344", "0m")
	view2 := model.View()
	assert.NotEmpty(t, view2)

	model.SetLoading(true)
	view3 := model.View()
	assert.NotEmpty(t, view3)

	model.SetLoading(false)
	view4 := model.View()
	assert.NotEmpty(t, view4)
}

func TestServiceControlsModel_renderStatusIndicator_UnknownStatus(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverStatus = "invalid_status"

	indicator := model.renderStatusIndicator()

	assert.NotEmpty(t, indicator)
	assert.Contains(t, indicator, "UNKNOWN")
}

func TestServiceControlsModel_renderServerInfo_EmptyValues(t *testing.T) {
	model := NewServiceControlsModel()
	model.serverPort = ""
	model.serverUptime = ""

	info := model.renderServerInfo()

	assert.NotEmpty(t, info)
	assert.Contains(t, info, "Port:")
	assert.Contains(t, info, "Uptime:")
}

func TestServiceControlsModel_ButtonStyle_Constant(t *testing.T) {
	style := ButtonStyle

	assert.NotNil(t, style)
	rendered := style.Render("Test")
	assert.NotEmpty(t, rendered)
}
