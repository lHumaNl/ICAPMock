package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
)

// Dashboard edge cases

func TestDashboardModel_AddLogEntry_ExtremeValues(t *testing.T) {
	model := NewDashboardModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   strings.Repeat("a", 100000),
		Fields: map[string]interface{}{
			"large_number": 9223372036854775807,
			"negative":     -999999999,
			"float":        123.456789012345,
		},
	}

	assert.NotPanics(t, func() {
		model.AddLogEntry(entry)
	})

	assert.Equal(t, 1, len(model.logPreview.entries))
}

func TestDashboardModel_Update_ZeroWindowSize(t *testing.T) {
	model := NewDashboardModel()

	msg := tea.WindowSizeMsg{Width: 0, Height: 0}
	assert.NotPanics(t, func() {
		newModel, _ := model.Update(msg)
		assert.NotNil(t, newModel)
	})
}

func TestDashboardModel_Update_NegativeWindowSize(t *testing.T) {
	model := NewDashboardModel()

	msg := tea.WindowSizeMsg{Width: -100, Height: -50}
	assert.NotPanics(t, func() {
		newModel, _ := model.Update(msg)
		assert.NotNil(t, newModel)
	})
}

func TestDashboardModel_Update_ExtremelyLargeWindowSize(t *testing.T) {
	model := NewDashboardModel()

	msg := tea.WindowSizeMsg{Width: 100000, Height: 100000}
	assert.NotPanics(t, func() {
		newModel, _ := model.Update(msg)
		assert.NotNil(t, newModel)
	})
}

func TestDashboardModel_View_WithNilMetrics(t *testing.T) {
	model := NewDashboardModel()
	model.ready = true
	model.width = 100
	model.height = 50

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestDashboardModel_SetMetrics_ExtremeValues(t *testing.T) {
	model := NewDashboardModel()

	snapshot := &state.MetricsSnapshot{
		Timestamp:     time.Now(),
		RPS:           9999999999.9999999999,
		LatencyP50:    999999.0,
		LatencyP95:    9999999.0,
		LatencyP99:    99999999.0,
		Connections:   999999999,
		Errors:        999999999,
		BytesSent:     9223372036854775807,
		BytesReceived: 9223372036854775807,
	}

	assert.NotPanics(t, func() {
		model.SetMetrics(snapshot)
	})

	assert.Equal(t, snapshot, model.metricsCards.metrics)
}

func TestDashboardModel_SetMetrics_NegativeValues(t *testing.T) {
	model := NewDashboardModel()

	snapshot := &state.MetricsSnapshot{
		Timestamp:     time.Now(),
		RPS:           -100.0,
		LatencyP50:    -50.0,
		LatencyP95:    -100.0,
		LatencyP99:    -150.0,
		Connections:   -10,
		Errors:        -5,
		BytesSent:     -1000,
		BytesReceived: -2000,
	}

	assert.NotPanics(t, func() {
		model.SetMetrics(snapshot)
	})

	assert.Equal(t, snapshot, model.metricsCards.metrics)
}

func TestDashboardModel_SetMetrics_ZeroTimestamp(t *testing.T) {
	model := NewDashboardModel()

	snapshot := &state.MetricsSnapshot{
		Timestamp: time.Time{},
		RPS:       100.0,
	}

	assert.NotPanics(t, func() {
		model.SetMetrics(snapshot)
	})
}

func TestMetricsCardsModel_View_ExtremeValues(t *testing.T) {
	model := NewMetricsCardsModel()

	snapshot := &state.MetricsSnapshot{
		RPS:           9999999999.9999999999,
		LatencyP50:    9999999999.9999999999,
		LatencyP95:    9999999999.9999999999,
		LatencyP99:    9999999999.9999999999,
		Connections:   999999999,
		Errors:        999999999,
		BytesSent:     9223372036854775807,
		BytesReceived: 9223372036854775807,
	}

	model.SetMetrics(snapshot)

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestMetricsGraphModel_SetMetrics_MaxHistory_Race(t *testing.T) {
	model := NewMetricsGraphModel()

	for i := 0; i < 200; i++ {
		snapshot := &state.MetricsSnapshot{
			Timestamp: time.Now(),
			RPS:       float64(i),
		}
		model.SetMetrics(snapshot)
	}

	assert.Equal(t, 60, len(model.history))
}

func TestMetricsGraphModel_createGraph_NegativeValues(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{-100, -50, -25, -10, -5}

	assert.NotPanics(t, func() {
		graph := model.createGraph(values, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.NotEmpty(t, graph)
	})
}

func TestMetricsGraphModel_createGraph_ZeroValues(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{0, 0, 0, 0, 0}

	assert.NotPanics(t, func() {
		graph := model.createGraph(values, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.NotEmpty(t, graph)
	})
}

func TestMetricsGraphModel_findMinMax_AllZeros(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{0, 0, 0, 0, 0}

	min, max := model.findMinMax(values)
	assert.Equal(t, 0.0, min)
	assert.Equal(t, 0.0, max)
}

func TestMetricsGraphModel_findMinMax_AllSame(t *testing.T) {
	model := NewMetricsGraphModel()

	values := []float64{50, 50, 50, 50, 50}

	min, max := model.findMinMax(values)
	assert.Equal(t, 50.0, min)
	assert.Equal(t, 50.0, max)
}

func TestLogPreviewModel_AddEntry_MaxLines_Race(t *testing.T) {
	model := NewLogPreviewModel()

	for i := 0; i < 20; i++ {
		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		model.AddEntry(entry)
	}

	assert.Equal(t, 5, len(model.entries))
}

func TestLogPreviewModel_AddEntry_Nil(t *testing.T) {
	model := NewLogPreviewModel()

	assert.NotPanics(t, func() {
		model.AddEntry(nil)
	})

	assert.Equal(t, 1, len(model.entries))
	assert.Nil(t, model.entries[0])
}

func TestFormatBytes_MaxInt64(t *testing.T) {
	result := formatBytes(9223372036854775807)
	assert.NotEmpty(t, result)
}

func TestFormatBytes_Zero(t *testing.T) {
	result := formatBytes(0)
	assert.Equal(t, "0 B", result)
}

func TestFormatBytes_Negative(t *testing.T) {
	result := formatBytes(-100)
	assert.NotEmpty(t, result)
}

func TestTruncate_EmptyString(t *testing.T) {
	result := truncate("", 10)
	assert.Equal(t, "", result)
}

func TestTruncate_ZeroMaxLen(t *testing.T) {
	result := truncate("test", 0)
	assert.Equal(t, "...", result)
}

func TestTruncate_NegativeMaxLen(t *testing.T) {
	result := truncate("test", -5)
	assert.Equal(t, "", result)
}

func TestTruncate_StringShorterThanMax(t *testing.T) {
	result := truncate("test", 100)
	assert.Equal(t, "test", result)
}

func TestGetLogLevelStyle_InvalidLevel(t *testing.T) {
	invalidLevels := []string{"CRITICAL", "TRACE", "VERBOSE", "UNKNOWN", ""}

	for _, level := range invalidLevels {
		t.Run(level, func(t *testing.T) {
			assert.NotPanics(t, func() {
				style := getLogLevelStyle(level)
				result := style.Render("test")
				assert.NotEmpty(t, result)
			})
		})
	}
}

func TestConnectionsModel_SetMetrics_Nil(t *testing.T) {
	model := NewConnectionsModel()

	assert.NotPanics(t, func() {
		model.SetMetrics(nil)
	})
}

func TestConnectionsModel_View_ExtremeValues(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   999999999,
		BytesSent:     9223372036854775807,
		BytesReceived: 9223372036854775807,
	}

	model.SetMetrics(snapshot)

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestConnectionsModel_View_NegativeValues(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   -100,
		BytesSent:     -1000,
		BytesReceived: -2000,
	}

	model.SetMetrics(snapshot)

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestDashboardModel_Update_WindowSize_Changing(t *testing.T) {
	model := NewDashboardModel()

	sizes := []struct{ width, height int }{
		{0, 0},
		{1, 1},
		{10, 5},
		{100, 50},
		{1000, 500},
		{100000, 100000},
	}

	for _, size := range sizes {
		msg := tea.WindowSizeMsg{Width: size.width, Height: size.height}
		assert.NotPanics(t, func() {
			newModel, _ := model.Update(msg)
			assert.NotNil(t, newModel)
		})
	}
}

func TestMetricsGraphModel_extractRPS_EmptyHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	rpsValues := model.extractRPS()
	assert.Len(t, rpsValues, 0)
}

func TestMetricsGraphModel_extractLatency_EmptyHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	latencyValues := model.extractLatency()
	assert.Len(t, latencyValues, 0)
}

func TestMetricsGraphModel_currentValue_NilSlice(t *testing.T) {
	model := NewMetricsGraphModel()

	current := model.currentValue(nil)
	assert.Equal(t, 0.0, current)
}

func TestMetricsGraphModel_currentValue_EmptySlice(t *testing.T) {
	model := NewMetricsGraphModel()

	current := model.currentValue([]float64{})
	assert.Equal(t, 0.0, current)
}

func TestDashboardModel_AddLogEntry_Unicode(t *testing.T) {
	model := NewDashboardModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Unicode test: 你好世界 🌍 مرحبا بالعالم",
		Fields: map[string]interface{}{
			"emoji":   "😀😁😂",
			"arabic":  "مرحبا",
			"chinese": "你好",
			"special": "\x00\x01\xff",
		},
	}

	assert.NotPanics(t, func() {
		model.AddLogEntry(entry)
	})

	assert.Equal(t, 1, len(model.logPreview.entries))
}

func TestDashboardModel_AddLogEntry_Whitespace(t *testing.T) {
	model := NewDashboardModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: strings.Repeat(" ", 1000)},
		{Timestamp: time.Now(), Level: "INFO", Message: "\t\t\t"},
		{Timestamp: time.Now(), Level: "INFO", Message: "\n\n\n"},
		{Timestamp: time.Now(), Level: "INFO", Message: " \t\n \t\n "},
	}

	for _, entry := range entries {
		assert.NotPanics(t, func() {
			model.AddLogEntry(entry)
		})
	}

	assert.Equal(t, 4, len(model.logPreview.entries))
}

func TestMetricsCardsModel_getErrorColor_LargeValues(t *testing.T) {
	model := NewMetricsCardsModel()

	model.metrics.Errors = 999999999
	color := model.getErrorColor()

	assert.NotEmpty(t, color)
}

func TestMetricsCardsModel_getErrorColor_Negative(t *testing.T) {
	model := NewMetricsCardsModel()

	model.metrics.Errors = -100
	assert.NotPanics(t, func() {
		color := model.getErrorColor()
		assert.NotEmpty(t, color)
	})
}

func TestMetricsGraphModel_View_WithNoHistory(t *testing.T) {
	model := NewMetricsGraphModel()

	assert.NotPanics(t, func() {
		view := model.View()
		assert.Contains(t, view, "Waiting for metrics data")
	})
}

func TestLogPreviewModel_View_WithNilEntry(t *testing.T) {
	model := NewLogPreviewModel()
	model.entries = []*state.LogEntry{nil}

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestLogPreviewModel_View_WithEmptyMessage(t *testing.T) {
	model := NewLogPreviewModel()
	model.entries = []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: ""},
	}

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestDashboardModel_View_NotReady_WithMetrics(t *testing.T) {
	model := NewDashboardModel()
	model.ready = false

	snapshot := &state.MetricsSnapshot{
		RPS: 100.0,
	}
	model.SetMetrics(snapshot)

	view := model.View()
	assert.Equal(t, "Loading dashboard...", view)
}

func TestDashboardModel_View_Ready_WithNoMetrics(t *testing.T) {
	model := NewDashboardModel()
	model.ready = true
	model.width = 100
	model.height = 50

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestDashboardModel_Update_UnknownMessageType(t *testing.T) {
	model := NewDashboardModel()

	assert.NotPanics(t, func() {
		newModel, cmd := model.Update("unknown message type")
		assert.NotNil(t, newModel)
		assert.Nil(t, cmd)
	})
}

func TestMetricsGraphModel_Update_ZeroWidth(t *testing.T) {
	model := NewMetricsGraphModel()

	msg := tea.WindowSizeMsg{Width: 0, Height: 50}

	assert.NotPanics(t, func() {
		newModel, _ := model.Update(msg)
		assert.NotNil(t, newModel)
	})
}

func TestMetricsGraphModel_Update_SmallWidth(t *testing.T) {
	model := NewMetricsGraphModel()

	msg := tea.WindowSizeMsg{Width: 5, Height: 50}

	assert.NotPanics(t, func() {
		newModel, _ := model.Update(msg)
		assert.NotNil(t, newModel)
	})
}

func TestDashboardModel_SetMetrics_Consistency(t *testing.T) {
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

func TestDashboardModel_AddLogEntry_TimestampOrder(t *testing.T) {
	model := NewDashboardModel()

	now := time.Now()

	entry1 := &state.LogEntry{
		Timestamp: now.Add(-2 * time.Hour),
		Level:     "INFO",
		Message:   "Old message",
	}

	entry2 := &state.LogEntry{
		Timestamp: now.Add(-1 * time.Hour),
		Level:     "INFO",
		Message:   "Newer message",
	}

	entry3 := &state.LogEntry{
		Timestamp: now,
		Level:     "INFO",
		Message:   "Newest message",
	}

	model.AddLogEntry(entry1)
	model.AddLogEntry(entry2)
	model.AddLogEntry(entry3)

	assert.Equal(t, 3, len(model.logPreview.entries))
}

func TestDashboardModel_AddLogEntry_AllLogLevels(t *testing.T) {
	model := NewDashboardModel()

	levels := []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

	for _, level := range levels {
		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Message:   fmt.Sprintf("Message at %s level", level),
		}
		model.AddLogEntry(entry)
	}

	assert.Equal(t, 5, len(model.logPreview.entries))
}

func TestMetricsGraphModel_extractRPS_SingleValue(t *testing.T) {
	model := NewMetricsGraphModel()

	snapshot := &state.MetricsSnapshot{RPS: 100.0}
	model.SetMetrics(snapshot)

	rpsValues := model.extractRPS()
	assert.Len(t, rpsValues, 1)
	assert.Equal(t, 100.0, rpsValues[0])
}

func TestMetricsGraphModel_extractLatency_SingleValue(t *testing.T) {
	model := NewMetricsGraphModel()

	snapshot := &state.MetricsSnapshot{LatencyP95: 50.0}
	model.SetMetrics(snapshot)

	latencyValues := model.extractLatency()
	assert.Len(t, latencyValues, 1)
	assert.Equal(t, 50.0, latencyValues[0])
}

func TestMetricsGraphModel_createGraph_SingleValue(t *testing.T) {
	model := NewMetricsGraphModel()

	graph := model.createGraph([]float64{50.0}, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
	assert.NotEmpty(t, graph)
}

func TestMetricsGraphModel_createGraph_TwoValues(t *testing.T) {
	model := NewMetricsGraphModel()

	graph := model.createGraph([]float64{10.0, 20.0}, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
	assert.NotEmpty(t, graph)
}

func TestMetricsGraphModel_createGraph_LargeValues(t *testing.T) {
	model := NewMetricsGraphModel()

	largeValues := []float64{9223372036854775807.0, 9223372036854775806.0, 9223372036854775805.0}

	assert.NotPanics(t, func() {
		graph := model.createGraph(largeValues, "Test", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
		assert.NotEmpty(t, graph)
	})
}

func TestLogPreviewModel_View_WithMultipleEntries(t *testing.T) {
	model := NewLogPreviewModel()

	for i := 0; i < 10; i++ {
		entry := &state.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   fmt.Sprintf("Message %d", i),
		}
		model.AddEntry(entry)
	}

	assert.NotPanics(t, func() {
		view := model.View()
		assert.NotEmpty(t, view)
	})
}

func TestConnectionsModel_View_WithZeroConnections(t *testing.T) {
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

func TestConnectionsModel_View_WithLargeBytes(t *testing.T) {
	model := NewConnectionsModel()

	snapshot := &state.MetricsSnapshot{
		Connections:   100,
		BytesSent:     1099511627776,
		BytesReceived: 1099511627776,
	}

	model.SetMetrics(snapshot)

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "TiB")
}
