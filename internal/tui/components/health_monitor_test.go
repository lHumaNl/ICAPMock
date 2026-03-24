package components

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
)

func TestNewHealthMonitorModel(t *testing.T) {
	model := NewHealthMonitorModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.healthChecks)
	assert.NotNil(t, model.alerts)
	assert.Equal(t, 0, len(model.healthChecks))
	assert.Equal(t, 0, len(model.alerts))
}

func TestHealthMonitorModel_UpdateHealthCheck(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     5,
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 0, len(model.alerts))
	assert.Equal(t, result, model.healthChecks[0])
}

func TestHealthMonitorModel_UpdateHealthCheck_Unhealthy(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     5,
		Error:         "health check failed",
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "Health check failed")
}

func TestHealthMonitorModel_UpdateHealthCheck_NotReady(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         false,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     5,
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "not ready to accept traffic")
}

func TestHealthMonitorModel_UpdateHealthCheck_ICAPIssue(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "error",
		StorageStatus: "ok",
		Scenarios:     5,
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "ICAP server issue")
}

func TestHealthMonitorModel_UpdateHealthCheck_StorageIssue(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "error",
		Scenarios:     5,
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "Storage issue")
}

func TestHealthMonitorModel_UpdateHealthCheck_MultipleIssues(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         false,
		ICAPStatus:    "error",
		StorageStatus: "error",
		Scenarios:     5,
		Error:         "multiple failures",
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, 4, len(model.alerts))
}

func TestHealthMonitorModel_AddAlert(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Test alert message")

	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "Test alert message")
}

func TestHealthMonitorModel_AddAlert_MaxSize(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 7; i++ {
		model.addAlert("Alert message")
	}

	assert.Equal(t, 5, len(model.alerts))
}

func TestHealthMonitorModel_ClearAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Alert 1")
	model.addAlert("Alert 2")
	assert.Equal(t, 2, len(model.alerts))

	model.ClearAlerts()
	assert.Equal(t, 0, len(model.alerts))
}

func TestHealthMonitorModel_View(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     5,
	}

	model.UpdateHealthCheck(result)

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestHealthMonitorModel_getStatusStyle(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		name     string
		healthy  bool
		ready    bool
		contains string
	}{
		{"healthy and ready", true, true, "running"},
		{"unhealthy", false, true, "stopped"},
		{"not ready", true, false, "warning"},
		{"unhealthy and not ready", false, false, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := model.getStatusStyle(tt.healthy, tt.ready)
			result := style.Render("test")
			assert.NotEmpty(t, result)
		})
	}
}

func TestHealthMonitorModel_getComponentStyle(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		status   string
		contains string
	}{
		{"ok", "running"},
		{"starting", "warning"},
		{"error", "stopped"},
		{"unknown", "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			style := model.getComponentStyle(tt.status)
			result := style.Render("test")
			assert.NotEmpty(t, result)
		})
	}
}

func TestHealthMonitorModel_renderContent_Empty(t *testing.T) {
	model := NewHealthMonitorModel()

	content := model.renderContent()

	assert.NotEmpty(t, content)
	assert.Contains(t, content, "Health Monitor")
}

func TestHealthMonitorModel_renderCurrentHealth(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		name       string
		healthy    bool
		ready      bool
		statusText string
	}{
		{"healthy and ready", true, true, "OK"},
		{"unhealthy", false, true, "Unhealthy"},
		{"not ready", true, false, "Not Ready"},
		{"unhealthy and not ready", false, false, "Unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := state.HealthCheckResult{
				Timestamp:     time.Now(),
				Healthy:       tt.healthy,
				Ready:         tt.ready,
				ICAPStatus:    "ok",
				StorageStatus: "ok",
				Scenarios:     5,
			}

			rendered := model.renderCurrentHealth(result)

			assert.NotEmpty(t, rendered)
			assert.Contains(t, rendered, tt.statusText)
		})
	}
}

func TestHealthMonitorModel_renderAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Alert 1: Test error")
	model.addAlert("Alert 2: Another error")

	rendered := model.renderAlerts()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Alerts")
	assert.Contains(t, rendered, "Alert 1")
	assert.Contains(t, rendered, "Alert 2")
}

func TestHealthMonitorModel_renderAlerts_Empty(t *testing.T) {
	model := NewHealthMonitorModel()

	rendered := model.renderAlerts()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Alerts")
}

func TestHealthMonitorModel_renderHistory(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 3; i++ {
		result := state.HealthCheckResult{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Second),
			Healthy:   true,
			Ready:     true,
			Scenarios: i,
		}
		model.UpdateHealthCheck(result)
	}

	rendered := model.renderHistory()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Recent Health Checks")
}

func TestHealthMonitorModel_renderHistory_Empty(t *testing.T) {
	model := NewHealthMonitorModel()

	rendered := model.renderHistory()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Recent Health Checks")
}

func TestHealthMonitorModel_renderHistory_Single(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp: time.Now(),
		Healthy:   true,
		Ready:     true,
		Scenarios: 5,
	}
	model.UpdateHealthCheck(result)

	rendered := model.renderHistory()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Recent Health Checks")
}

func TestHealthMonitorModel_renderCurrentHealth_WithFields(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     10,
		Error:         "",
	}

	rendered := model.renderCurrentHealth(result)

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "10")
	assert.Contains(t, rendered, "ICAP Server")
	assert.Contains(t, rendered, "Storage")
}

func TestHealthMonitorModel_renderCurrentHealth_WithError(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         true,
		ICAPStatus:    "error",
		StorageStatus: "ok",
		Scenarios:     5,
		Error:         "Connection failed",
	}

	rendered := model.renderCurrentHealth(result)

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Error")
	assert.Contains(t, rendered, "Connection failed")
}

func TestHealthMonitorModel_addAlert_Timestamp(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Test alert")

	assert.Equal(t, 1, len(model.alerts))
	alert := model.alerts[0]

	assert.Contains(t, alert, "Test alert")

	prefix := alert[:1]
	assert.Equal(t, "[", prefix)
}

func TestHealthMonitorModel_addAlert_MaxAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 10; i++ {
		model.addAlert(fmt.Sprintf("Alert %d", i))
	}

	assert.Equal(t, 5, len(model.alerts))
	assert.Contains(t, model.alerts[0], "Alert 5")
}

func TestHealthMonitorModel_renderCurrentHealth_StatusStyles(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		healthy       bool
		ready         bool
		shouldContain []string
	}{
		{true, true, []string{"OK"}},
		{false, true, []string{"Unhealthy"}},
		{true, false, []string{"Not Ready"}},
		{false, false, []string{"Unhealthy"}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("healthy=%v_ready=%v", tt.healthy, tt.ready), func(t *testing.T) {
			result := state.HealthCheckResult{
				Timestamp:     time.Now(),
				Healthy:       tt.healthy,
				Ready:         tt.ready,
				ICAPStatus:    "ok",
				StorageStatus: "ok",
				Scenarios:     5,
			}

			rendered := model.renderCurrentHealth(result)

			for _, shouldContain := range tt.shouldContain {
				assert.Contains(t, rendered, shouldContain)
			}
		})
	}
}

func TestHealthMonitorModel_addAlert_EmptyMessage(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("")

	assert.Equal(t, 1, len(model.alerts))
}

func TestHealthMonitorModel_AddAlert_Unicode(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("⚠️ Warning: Service degraded")

	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "⚠️")
}

func TestHealthMonitorModel_renderHistory_LastFive(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 10; i++ {
		result := state.HealthCheckResult{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Second),
			Healthy:   true,
			Ready:     true,
			Scenarios: i,
		}
		model.UpdateHealthCheck(result)
	}

	rendered := model.renderHistory()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Recent Health Checks")
}

func TestHealthMonitorModel_UpdateHealthCheck_MultipleResults(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 5; i++ {
		result := state.HealthCheckResult{
			Timestamp: time.Now(),
			Healthy:   true,
			Ready:     true,
			Scenarios: i,
		}
		model.UpdateHealthCheck(result)
	}

	assert.Equal(t, 5, len(model.healthChecks))
}

func TestHealthMonitorModel_getStatus_AllCombinations(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		healthy bool
		ready   bool
	}{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("healthy=%v_ready=%v", tt.healthy, tt.ready), func(t *testing.T) {
			style := model.getStatusStyle(tt.healthy, tt.ready)
			assert.NotEmpty(t, style.Render("test"))
		})
	}
}

func TestHealthMonitorModel_UpdateHealthCheck_Concurrent(t *testing.T) {
	model := NewHealthMonitorModel()
	var wg sync.WaitGroup
	numGoroutines := 100
	checksPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < checksPerGoroutine; j++ {
				result := state.HealthCheckResult{
					Timestamp:     time.Now(),
					Healthy:       j%2 == 0,
					Ready:         true,
					ICAPStatus:    "ok",
					StorageStatus: "ok",
					Scenarios:     id,
				}
				model.UpdateHealthCheck(result)
			}
		}(i)
	}

	wg.Wait()

	assert.Greater(t, len(model.healthChecks), 0)
	assert.GreaterOrEqual(t, len(model.alerts), 0)
}

func TestHealthMonitorModel_UpdateHealthCheck_LimitTo10(t *testing.T) {
	model := NewHealthMonitorModel()
	now := time.Now()

	for i := 0; i < 15; i++ {
		result := state.HealthCheckResult{
			Timestamp:     now.Add(-time.Duration(i) * time.Second),
			Healthy:       true,
			Ready:         true,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
			Scenarios:     i,
		}
		model.UpdateHealthCheck(result)
	}

	assert.Equal(t, 10, len(model.healthChecks))
}

func TestHealthMonitorModel_addAlert_TimestampFormat(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Test alert")

	assert.Equal(t, 1, len(model.alerts))
	alert := model.alerts[0]

	assert.Contains(t, alert, "[")
	assert.Contains(t, alert, "]")
	assert.Contains(t, alert, "Test alert")
}

func TestHealthMonitorModel_renderCurrentHealth_ComponentStatuses(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		name          string
		icapStatus    string
		storageStatus string
	}{
		{"Both OK", "ok", "ok"},
		{"ICAP starting", "starting", "ok"},
		{"Storage starting", "ok", "starting"},
		{"Both starting", "starting", "starting"},
		{"ICAP error", "error", "ok"},
		{"Storage error", "ok", "error"},
		{"Both error", "error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := state.HealthCheckResult{
				Timestamp:     time.Now(),
				Healthy:       true,
				Ready:         true,
				ICAPStatus:    tt.icapStatus,
				StorageStatus: tt.storageStatus,
			}

			rendered := model.renderCurrentHealth(result)
			assert.Contains(t, rendered, "ICAP Server:")
			assert.Contains(t, rendered, "Storage:")
			assert.Contains(t, rendered, tt.icapStatus)
			assert.Contains(t, rendered, tt.storageStatus)
		})
	}
}

func TestHealthMonitorModel_renderHistory_StatusIndicators(t *testing.T) {
	model := NewHealthMonitorModel()

	checks := []struct {
		healthy bool
		ready   bool
		status  string
	}{
		{true, true, "OK"},
		{false, true, "FAIL"},
		{true, false, "NREADY"},
		{false, false, "FAIL"},
	}

	for _, check := range checks {
		result := state.HealthCheckResult{
			Timestamp:     time.Now().Add(-time.Duration(len(checks)) * time.Second),
			Healthy:       check.healthy,
			Ready:         check.ready,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
		}
		model.UpdateHealthCheck(result)
	}

	rendered := model.renderHistory()

	for _, check := range checks {
		assert.Contains(t, rendered, check.status)
	}
}

func TestHealthMonitorModel_View_WithNoHealthChecks(t *testing.T) {
	model := NewHealthMonitorModel()

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Health Monitor")
	assert.NotContains(t, view, "Status:")
}

func TestHealthMonitorModel_View_WithMultipleHealthChecks(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 5; i++ {
		result := state.HealthCheckResult{
			Timestamp:     time.Now().Add(-time.Duration(i) * time.Second),
			Healthy:       true,
			Ready:         true,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
			Scenarios:     i,
		}
		model.UpdateHealthCheck(result)
	}

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Health Monitor")
	assert.Contains(t, view, "Recent Health Checks")
	assert.Contains(t, view, "Status:")
}

func TestHealthMonitorModel_getComponentStyle_AllStatuses(t *testing.T) {
	model := NewHealthMonitorModel()

	tests := []struct {
		name   string
		status string
	}{
		{"OK status", "ok"},
		{"Starting status", "starting"},
		{"Error status", "error"},
		{"Unknown status", "unknown"},
		{"Empty status", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := model.getComponentStyle(tt.status)
			if tt.status == "" {
				rendered := style.Render("test")
				assert.NotEmpty(t, rendered)
			} else {
				rendered := style.Render(tt.status)
				assert.NotEmpty(t, rendered)
			}
		})
	}
}

func TestHealthMonitorModel_EdgeCase_ZeroScenarios(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     0,
	}

	model.UpdateHealthCheck(result)
	rendered := model.renderCurrentHealth(result)

	assert.Contains(t, rendered, "Scenarios Loaded: 0")
}

func TestHealthMonitorModel_EdgeCase_VeryLongError(t *testing.T) {
	model := NewHealthMonitorModel()

	longError := strings.Repeat("error ", 100)
	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Error:         longError,
	}

	model.UpdateHealthCheck(result)
	rendered := model.renderCurrentHealth(result)

	assert.Contains(t, rendered, "Error:")
	assert.Contains(t, rendered, longError)
}

func TestHealthMonitorModel_EdgeCase_UnicodeInAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	unicodeAlert := "⚠️ Critical Alert: 🚨 Service Unavailable 🔥"
	model.addAlert(unicodeAlert)

	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], unicodeAlert)
}

func TestHealthMonitorModel_EdgeCase_RapidUpdates(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 1000; i++ {
		result := state.HealthCheckResult{
			Timestamp:     time.Now(),
			Healthy:       i%2 == 0,
			Ready:         true,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
			Scenarios:     i,
		}
		model.UpdateHealthCheck(result)
	}

	assert.LessOrEqual(t, len(model.healthChecks), 10)
}

func TestHealthMonitorModel_EdgeCase_MultipleRapidAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 100; i++ {
		model.addAlert(fmt.Sprintf("Alert %d", i))
	}

	assert.LessOrEqual(t, len(model.alerts), 5)
}

func TestHealthMonitorModel_EdgeCase_EmptyAlertMessage(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("")

	assert.Equal(t, 1, len(model.alerts))
}

func TestHealthMonitorModel_EdgeCase_AlertWithSpecialChars(t *testing.T) {
	model := NewHealthMonitorModel()

	specialAlert := "Error: \t\n\r\x00"
	model.addAlert(specialAlert)

	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], specialAlert)
}

func TestHealthMonitorModel_EdgeCase_FutureTimestamp(t *testing.T) {
	model := NewHealthMonitorModel()

	futureTime := time.Now().Add(24 * time.Hour)
	result := state.HealthCheckResult{
		Timestamp:     futureTime,
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	assert.Equal(t, futureTime, model.healthChecks[0].Timestamp)
}

func TestHealthMonitorModel_EdgeCase_ZeroTimestamp(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Time{},
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
	}

	model.UpdateHealthCheck(result)

	assert.Equal(t, 1, len(model.healthChecks))
	rendered := model.renderCurrentHealth(result)
	assert.Contains(t, rendered, "Last Check:")
}

func TestHealthMonitorModel_EdgeCase_NegativeScenarios(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     -1,
	}

	model.UpdateHealthCheck(result)
	rendered := model.renderCurrentHealth(result)

	assert.Contains(t, rendered, "Scenarios Loaded:")
}

func TestHealthMonitorModel_EdgeCase_VeryLargeScenarios(t *testing.T) {
	model := NewHealthMonitorModel()

	result := state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       true,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Scenarios:     999999,
	}

	model.UpdateHealthCheck(result)
	rendered := model.renderCurrentHealth(result)

	assert.Contains(t, rendered, "999999")
}

func TestHealthMonitorModel_getComponentStyle_UnknownStatuses(t *testing.T) {
	model := NewHealthMonitorModel()

	unknownStatuses := []string{
		"pending",
		"degraded",
		"maintenance",
		"invalid",
		"123",
		"OK",
		"STARTING",
	}

	for _, status := range unknownStatuses {
		style := model.getComponentStyle(status)
		rendered := style.Render(status)
		assert.NotEmpty(t, rendered, "Should handle unknown status: %s", status)
	}
}

func TestHealthMonitorModel_renderHistory_Order(t *testing.T) {
	model := NewHealthMonitorModel()

	timestamps := []time.Time{}
	for i := 0; i < 3; i++ {
		ts := time.Now().Add(-time.Duration(i) * time.Second)
		timestamps = append(timestamps, ts)
		result := state.HealthCheckResult{
			Timestamp:     ts,
			Healthy:       true,
			Ready:         true,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
		}
		model.UpdateHealthCheck(result)
	}

	assert.Equal(t, 3, len(model.healthChecks))

	for i := 0; i < 3; i++ {
		assert.Equal(t, timestamps[i], model.healthChecks[i].Timestamp)
	}
}

func TestHealthMonitorModel_renderHistory_Limit(t *testing.T) {
	model := NewHealthMonitorModel()

	for i := 0; i < 10; i++ {
		result := state.HealthCheckResult{
			Timestamp:     time.Now().Add(-time.Duration(i) * time.Second),
			Healthy:       true,
			Ready:         true,
			ICAPStatus:    "ok",
			StorageStatus: "ok",
		}
		model.UpdateHealthCheck(result)
	}

	rendered := model.renderHistory()

	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "Recent Health Checks")
}

func TestHealthMonitorModel_ClearAlerts_MultipleTimes(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Alert 1")
	model.addAlert("Alert 2")
	model.ClearAlerts()
	model.ClearAlerts()
	model.ClearAlerts()

	assert.Equal(t, 0, len(model.alerts))
}

func TestHealthMonitorModel_ClearAlerts_ThenAddNew(t *testing.T) {
	model := NewHealthMonitorModel()

	model.addAlert("Old alert")
	model.ClearAlerts()
	assert.Equal(t, 0, len(model.alerts))

	model.addAlert("New alert")
	assert.Equal(t, 1, len(model.alerts))
	assert.Contains(t, model.alerts[0], "New alert")
}

func TestHealthMonitorModel_UpdateHealthCheck_AfterClearAlerts(t *testing.T) {
	model := NewHealthMonitorModel()

	model.UpdateHealthCheck(state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Error:         "Error 1",
	})

	assert.Greater(t, len(model.alerts), 0)

	model.ClearAlerts()
	assert.Equal(t, 0, len(model.alerts))

	model.UpdateHealthCheck(state.HealthCheckResult{
		Timestamp:     time.Now(),
		Healthy:       false,
		Ready:         true,
		ICAPStatus:    "ok",
		StorageStatus: "ok",
		Error:         "Error 2",
	})

	assert.Greater(t, len(model.alerts), 0)
	assert.Contains(t, model.alerts[0], "Error 2")
}
