// Copyright 2026 ICAP Mock

package components

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"

	"github.com/icap-mock/icap-mock/internal/tui/state"
)

func TestNewDashboardModel(t *testing.T) {
	model := NewDashboardModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.metricsCards)
	assert.NotNil(t, model.metricsGraph)
	assert.NotNil(t, model.logPreview)
	assert.NotNil(t, model.connections)
	assert.False(t, model.ready)
}

func TestDashboardModel_Init(t *testing.T) {
	model := NewDashboardModel()

	cmd := model.Init()
	assert.Nil(t, cmd)
}

func TestDashboardModel_Update_WindowSize(t *testing.T) {
	model := NewDashboardModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 100, newModel.width)
	assert.Equal(t, 50, newModel.height)
	assert.True(t, newModel.ready)
}

func TestDashboardModel_SetMetrics(t *testing.T) {
	model := NewDashboardModel()

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

	model.SetMetrics(snapshot)

	assert.Equal(t, snapshot, model.metricsCards.metrics)
	assert.Equal(t, snapshot, model.connections.metrics)
}

func TestDashboardModel_AddLogEntry(t *testing.T) {
	model := NewDashboardModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
	}

	model.AddLogEntry(entry)

	assert.Equal(t, 1, len(model.logPreview.entries))
	assert.Equal(t, entry, model.logPreview.entries[0])
}

func TestDashboardModel_AddLogEntry_MaxLines(t *testing.T) {
	model := NewDashboardModel()

	for i := 0; i < 10; i++ {
		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		model.AddLogEntry(entry)
	}

	assert.Equal(t, 5, len(model.logPreview.entries))
}

func TestDashboardModel_View_NotReady(t *testing.T) {
	model := NewDashboardModel()

	view := model.View()
	assert.Equal(t, "Loading dashboard...", view)
}

func TestDashboardModel_View_Ready(t *testing.T) {
	model := NewDashboardModel()
	model.ready = true
	model.width = 100
	model.height = 50

	view := model.View()
	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading dashboard...", view)
}

func TestNewMetricsCardsModel(t *testing.T) {
	model := NewMetricsCardsModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.metrics)
}

func TestMetricsCardsModel_SetMetrics(t *testing.T) {
	model := NewMetricsCardsModel()

	snapshot := &state.MetricsSnapshot{
		RPS:         100.5,
		LatencyP50:  10.0,
		LatencyP95:  25.0,
		LatencyP99:  50.0,
		Connections: 10,
		Errors:      1,
	}

	model.SetMetrics(snapshot)

	assert.Equal(t, snapshot, model.metrics)
}

func TestMetricsCardsModel_getErrorColor(t *testing.T) {
	model := NewMetricsCardsModel()

	model.metrics.Errors = 0
	color := model.getErrorColor()
	assert.Equal(t, lipgloss.AdaptiveColor{Light: "28", Dark: "46"}, color)

	model.metrics.Errors = 5
	color = model.getErrorColor()
	assert.Equal(t, lipgloss.AdaptiveColor{Light: "124", Dark: "196"}, color)
}

func TestNewMetricsGraphModel(t *testing.T) {
	model := NewMetricsGraphModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.history)
	assert.Equal(t, 0, len(model.history))
	assert.Equal(t, 60, model.maxHistory)
}

func TestMetricsGraphModel_Update_WindowSize(t *testing.T) {
	model := NewMetricsGraphModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 96, newModel.width)
}

func TestMetricsGraphModel_Update_SmallWindow(t *testing.T) {
	model := NewMetricsGraphModel()

	msg := tea.WindowSizeMsg{Width: 10, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 20, newModel.width)
}

func TestMetricsGraphModel_SetMetrics(t *testing.T) {
	model := NewMetricsGraphModel()

	snapshot := &state.MetricsSnapshot{RPS: 100.5}
	model.SetMetrics(snapshot)

	assert.Equal(t, 1, len(model.history))
	assert.Equal(t, snapshot, model.history[0])
}

func TestMetricsGraphModel_SetMetrics_MaxHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	for i := 0; i < 65; i++ {
		snapshot := &state.MetricsSnapshot{RPS: float64(i)}
		model.SetMetrics(snapshot)
	}

	assert.Equal(t, 60, len(model.history))
}

func TestMetricsGraphModel_extractRPS(t *testing.T) {
	model := NewMetricsGraphModel()

	snapshots := []*state.MetricsSnapshot{
		{RPS: 10.0},
		{RPS: 20.0},
		{RPS: 30.0},
	}

	for _, s := range snapshots {
		model.SetMetrics(s)
	}

	rpsValues := model.extractRPS()
	assert.Len(t, rpsValues, 3)
	assert.Equal(t, 10.0, rpsValues[0])
	assert.Equal(t, 20.0, rpsValues[1])
	assert.Equal(t, 30.0, rpsValues[2])
}

func TestMetricsGraphModel_extractLatency(t *testing.T) {
	model := NewMetricsGraphModel()

	snapshots := []*state.MetricsSnapshot{
		{LatencyP95: 10.0},
		{LatencyP95: 20.0},
		{LatencyP95: 30.0},
	}

	for _, s := range snapshots {
		model.SetMetrics(s)
	}

	latencyValues := model.extractLatency()
	assert.Len(t, latencyValues, 3)
	assert.Equal(t, 10.0, latencyValues[0])
	assert.Equal(t, 20.0, latencyValues[1])
	assert.Equal(t, 30.0, latencyValues[2])
}

func TestMetricsGraphModel_findMinMax(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{10.0, 25.0, 50.0, 5.0, 100.0}
	min, max := model.findMinMax(values)

	assert.Equal(t, 5.0, min)
	assert.Equal(t, 100.0, max)
}

func TestMetricsGraphModel_findMinMax_Empty(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{}
	min, max := model.findMinMax(values)

	assert.Equal(t, 0.0, min)
	assert.Equal(t, 0.0, max)
}

func TestMetricsGraphModel_findMinMax_Single(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{42.0}
	min, max := model.findMinMax(values)

	assert.Equal(t, 42.0, min)
	assert.Equal(t, 42.0, max)
}

func TestMetricsGraphModel_currentValue(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{10.0, 20.0, 30.0}
	current := model.currentValue(values)

	assert.Equal(t, 30.0, current)
}

func TestMetricsGraphModel_currentValue_Empty(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{}
	current := model.currentValue(values)

	assert.Equal(t, 0.0, current)
}

func TestNewLogPreviewModel(t *testing.T) {
	model := NewLogPreviewModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.entries)
	assert.Equal(t, 5, model.maxLines)
}

func TestLogPreviewModel_AddEntry(t *testing.T) {
	model := NewLogPreviewModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
	}

	model.AddEntry(entry)

	assert.Equal(t, 1, len(model.entries))
	assert.Equal(t, entry, model.entries[0])
}

func TestLogPreviewModel_AddEntry_MaxLines(t *testing.T) {
	model := NewLogPreviewModel()

	for i := 0; i < 10; i++ {
		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		model.AddEntry(entry)
	}

	assert.Equal(t, 5, len(model.entries))
}

func TestNewConnectionsModel(t *testing.T) {
	model := NewConnectionsModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.metrics)
}

func TestConnectionsModel_SetMetrics(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   10,
		BytesSent:     1024,
		BytesReceived: 2048,
	}

	model.SetMetrics(snapshot)

	assert.Equal(t, snapshot, model.metrics)
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		bytes int64
	}{
		{"bytes", 512, "512 B"},
		{"kilobytes", 1024, "1.0 KiB"},
		{"megabytes", 1048576, "1.0 MiB"},
		{"gigabytes", 1073741824, "1.0 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		want   string
		maxLen int
	}{
		{"no truncation", "hello world", 20, "hello world"},
		{"truncation", "this is a very long string that needs to be truncated", 20, "this is a very lo..."},
		{"exact length", "exactly", 7, "exactly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.s, tt.maxLen)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetLogLevelStyle(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"ERROR", "196"},
		{"WARN", "208"},
		{"INFO", "75"},
		{"DEBUG", "240"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			style := getLogLevelStyle(tt.level)
			result := style.Render("test")
			assert.NotEmpty(t, result)
		})
	}
}

func TestMetricsCardsModel_View_WithMetrics(t *testing.T) {
	model := NewMetricsCardsModel()

	snapshot := &state.MetricsSnapshot{
		RPS:         123.45,
		LatencyP50:  15.5,
		LatencyP95:  45.2,
		LatencyP99:  78.9,
		Connections: 25,
		Errors:      3,
	}

	model.SetMetrics(snapshot)
	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "123.45")
	assert.Contains(t, view, "15.50 ms")
	assert.Contains(t, view, "25")
}

func TestMetricsCardsModel_View_WithZeroMetrics(t *testing.T) {
	model := NewMetricsCardsModel()

	snapshot := &state.MetricsSnapshot{
		RPS:         0,
		LatencyP50:  0,
		LatencyP95:  0,
		LatencyP99:  0,
		Connections: 0,
		Errors:      0,
	}

	model.SetMetrics(snapshot)
	view := model.View()

	assert.NotEmpty(t, view)
}

func TestMetricsGraphModel_View_NoHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	view := model.View()

	assert.Contains(t, view, "Waiting for metrics data")
}

func TestMetricsGraphModel_View_WithHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	for i := 0; i < 5; i++ {
		snapshot := &state.MetricsSnapshot{
			RPS:        float64(10 + i*5),
			LatencyP95: float64(20 + i*10),
		}
		model.SetMetrics(snapshot)
	}

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Metrics History")
}

func TestMetricsGraphModel_createGraph_EdgeCases(t *testing.T) {
	model := NewMetricsGraphModel()

	t.Run("empty values", func(t *testing.T) {
		graph := model.createGraph([]float64{}, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.Contains(t, graph, "No data available")
	})

	t.Run("single value", func(t *testing.T) {
		graph := model.createGraph([]float64{50.0}, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.NotEmpty(t, graph)
	})

	t.Run("all same values", func(t *testing.T) {
		graph := model.createGraph([]float64{50.0, 50.0, 50.0}, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.NotEmpty(t, graph)
	})
}

func TestLogPreviewModel_View_Empty(t *testing.T) {
	model := NewLogPreviewModel()

	view := model.View()

	assert.Contains(t, view, "No recent logs")
}

func TestLogPreviewModel_View_WithEntries(t *testing.T) {
	model := NewLogPreviewModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Test message 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Test message 2"},
	}

	for _, entry := range entries {
		model.AddEntry(entry)
	}

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Test message 1")
	assert.Contains(t, view, "Test message 2")
}

func TestConnectionsModel_View_EmptyMetrics(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   0,
		BytesSent:     0,
		BytesReceived: 0,
	}

	model.SetMetrics(snapshot)
	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Active: 0")
}

func TestConnectionsModel_View_WithMetrics(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   42,
		BytesSent:     1024 * 1024,
		BytesReceived: 2 * 1024 * 1024,
	}

	model.SetMetrics(snapshot)
	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Active: 42")
	assert.Contains(t, view, "MiB")
}

func TestDashboardModel_View_WithMetrics(t *testing.T) {
	model := NewDashboardModel()
	model.ready = true
	model.width = 100
	model.height = 50

	snapshot := &state.MetricsSnapshot{
		RPS:           100.5,
		LatencyP50:    10.0,
		LatencyP95:    25.0,
		LatencyP99:    50.0,
		Connections:   10,
		Errors:        1,
		BytesSent:     1024,
		BytesReceived: 2048,
	}

	model.SetMetrics(snapshot)

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
	}

	model.AddLogEntry(entry)

	view := model.View()

	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading dashboard...", view)
}

func TestDashboardModel_Update_EmptyMessages(t *testing.T) {
	model := NewDashboardModel()

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyUp})

	assert.NotNil(t, newModel)
	assert.Nil(t, cmd)
}

func TestFormatBytes_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		bytes int64
	}{
		{"zero bytes", 0, "0 B"},
		{"exactly 1KB", 1024, "1.0 KiB"},
		{"exactly 1MB", 1048576, "1.0 MiB"},
		{"terabytes", 1099511627776, "1.0 TiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestDashboardModel_AddLogEntry_Nil(t *testing.T) {
	model := NewDashboardModel()

	model.AddLogEntry(nil)

	assert.Equal(t, 1, len(model.logPreview.entries))
	assert.Nil(t, model.logPreview.entries[0])
}

func TestDashboardModel_SetNilMetrics(t *testing.T) {
	model := NewDashboardModel()

	snapshot := &state.MetricsSnapshot{
		RPS: 100.5,
	}
	model.SetMetrics(snapshot)

	assert.Equal(t, snapshot, model.metricsCards.metrics)
	assert.Equal(t, snapshot, model.connections.metrics)
}
