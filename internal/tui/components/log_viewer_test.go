package components

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icap-mock/icap-mock/internal/tui/state"
	"github.com/stretchr/testify/assert"
)

func TestNewLogViewerModel(t *testing.T) {
	model := NewLogViewerModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.viewport)
	assert.NotNil(t, model.entries)
	assert.NotNil(t, model.filter)
	assert.True(t, model.autoScroll)
	assert.False(t, model.showDetails)
	assert.Equal(t, -1, model.selectedIdx)
}

func TestLogViewerModel_Init(t *testing.T) {
	model := NewLogViewerModel()

	cmd := model.Init()
	assert.Nil(t, cmd)
}

func TestLogViewerModel_SetEntries(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Message 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error 1"},
	}

	model.SetEntries(entries)

	assert.Equal(t, 2, len(model.entries))
}

func TestLogViewerModel_SetEntries_AutoScroll(t *testing.T) {
	model := NewLogViewerModel()
	model.autoScroll = true

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Message 1"},
	}

	model.SetEntries(entries)

	assert.Equal(t, 1, len(model.entries))
}

func TestLogViewerModel_SetFilter(t *testing.T) {
	model := NewLogViewerModel()

	filter := &LogFilter{Level: "ERROR"}
	model.SetFilter(filter)

	assert.Equal(t, "ERROR", model.filter.Level)
}

func TestLogViewerModel_SetSearch(t *testing.T) {
	model := NewLogViewerModel()

	model.SetSearch("error")

	assert.Equal(t, "error", model.searchQuery)
}

func TestLogViewerModel_SetAutoScroll(t *testing.T) {
	model := NewLogViewerModel()

	model.SetAutoScroll(false)

	assert.False(t, model.autoScroll)
}

func TestLogViewerModel_GetFilter(t *testing.T) {
	model := NewLogViewerModel()
	model.filter = &LogFilter{Level: "ERROR"}

	filter := model.GetFilter()
	assert.Equal(t, "ERROR", filter.Level)
}

func TestLogViewerModel_GetSearch(t *testing.T) {
	model := NewLogViewerModel()
	model.searchQuery = "test"

	search := model.GetSearch()
	assert.Equal(t, "test", search)
}

func TestLogViewerModel_IsAutoScrollEnabled(t *testing.T) {
	model := NewLogViewerModel()
	model.autoScroll = true

	assert.True(t, model.IsAutoScrollEnabled())

	model.autoScroll = false
	assert.False(t, model.IsAutoScrollEnabled())
}

func TestLogViewerModel_cycleFilter(t *testing.T) {
	model := NewLogViewerModel()

	model.cycleFilter()
	assert.Equal(t, "DEBUG", model.filter.Level)

	model.cycleFilter()
	assert.Equal(t, "INFO", model.filter.Level)

	model.cycleFilter()
	assert.Equal(t, "WARN", model.filter.Level)

	model.cycleFilter()
	assert.Equal(t, "ERROR", model.filter.Level)

	model.cycleFilter()
	assert.Equal(t, "", model.filter.Level)
}

func TestLogViewerModel_filterEntries_Level(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Info message"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error message"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Another error"},
	}

	model.entries = entries
	model.filter = &LogFilter{Level: "ERROR"}

	filtered := model.filterEntries()

	assert.Len(t, filtered, 2)
	for _, entry := range filtered {
		assert.Equal(t, "ERROR", entry.Level)
	}
}

func TestLogViewerModel_filterEntries_Search(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "User logged in"},
		{Timestamp: time.Now(), Level: "INFO", Message: "User logged out"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Database error"},
	}

	model.entries = entries
	model.searchQuery = "logged"

	filtered := model.filterEntries()

	assert.Len(t, filtered, 2)
	for _, entry := range filtered {
		assert.Contains(t, entry.Message, "logged")
	}
}

func TestLogViewerModel_containsSearch_Message(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "User logged in successfully",
	}

	model.searchQuery = "logged"
	assert.True(t, model.containsSearch(entry))
}

func TestLogViewerModel_containsSearch_Level(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
	}

	model.searchQuery = "info"
	assert.True(t, model.containsSearch(entry))
}

func TestLogViewerModel_containsSearch_Fields(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
		Fields:    map[string]interface{}{"user_id": 123, "action": "login"},
	}

	model.searchQuery = "user_id"
	assert.True(t, model.containsSearch(entry))
}

func TestLogViewerModel_highlightSearch(t *testing.T) {
	model := NewLogViewerModel()
	model.searchQuery = "error"

	text := "This is an error message"

	highlighted := model.highlightSearch(text)
	assert.Contains(t, highlighted, "error")
}

func TestLogViewerModel_Update_Scroll(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.NotNil(t, newModel)

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.NotNil(t, newModel)

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.NotNil(t, newModel)

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.NotNil(t, newModel)

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	assert.NotNil(t, newModel)

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	assert.NotNil(t, newModel)
}

func TestLogViewerModel_Update_ToggleFilter(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})

	assert.NotNil(t, newModel)
	assert.NotEmpty(t, newModel.filter.Level)
}

func TestLogViewerModel_Update_ToggleAutoScroll(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.autoScroll = true

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.NotNil(t, cmd)
	assert.False(t, newModel.autoScroll)

	cmdMsg := cmd()
	assert.IsType(t, LogAutoScrollMsg{}, cmdMsg)

	autoScrollMsg := cmdMsg.(LogAutoScrollMsg)
	assert.False(t, autoScrollMsg.Enabled)
}

func TestLogViewerModel_Update_ShowDetails(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.entries = []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Test message"},
	}
	model.selectedIdx = 0

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, newModel.showDetails)
}

func TestLogViewerModel_Update_CloseDetails(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.showDetails = true
	model.selectedIdx = 0

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, newModel.showDetails)
}

func TestLogViewerModel_View_NotReady(t *testing.T) {
	model := NewLogViewerModel()

	view := model.View()
	assert.Equal(t, "Loading log viewer...", view)
}

func TestLogViewerModel_View_Ready(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.width = 100
	model.height = 50

	view := model.View()
	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading log viewer...", view)
}

func TestLogViewerModel_View_ShowDetails(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.showDetails = true
	model.selectedIdx = 0
	model.entries = []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Test message"},
	}

	view := model.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Log Entry Details")
}

func TestLogViewerModel_updateViewportContent(t *testing.T) {
	model := NewLogViewerModel()
	model.width = 100
	model.height = 50

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Message 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error 1"},
	}

	newModel.entries = entries
	newModel.updateViewportContent()

	viewContent := newModel.viewport.View()
	assert.NotEmpty(t, viewContent)
}

func TestLogViewerModel_updateViewportContent_Empty(t *testing.T) {
	model := NewLogViewerModel()
	model.width = 100
	model.height = 50

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	newModel.entries = []*state.LogEntry{}
	newModel.updateViewportContent()

	viewContent := newModel.viewport.View()
	assert.NotEmpty(t, viewContent)
}

func TestLogViewerModel_renderLogLine(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:     "ERROR",
		Message:   "Test error message",
	}

	line := model.renderLogLine(0, entry)

	assert.NotEmpty(t, line)
	assert.Contains(t, line, "2024-01-01")
	assert.Contains(t, line, "ERROR")
	assert.Contains(t, line, "Test error message")
}

func TestLogViewerModel_renderLogLine_Selected(t *testing.T) {
	model := NewLogViewerModel()
	model.selectedIdx = 0

	entry := &state.LogEntry{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:     "INFO",
		Message:   "Test message",
	}

	line := model.renderLogLine(0, entry)

	assert.NotEmpty(t, line)
}

func TestLogViewerModel_filterEntries_Combined(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "User logged in"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Database error"},
		{Timestamp: time.Now(), Level: "WARN", Message: "Warning message"},
	}

	model.entries = entries
	model.filter = &LogFilter{Level: "ERROR"}
	model.searchQuery = "database"

	filtered := model.filterEntries()

	assert.Len(t, filtered, 1)
	assert.Contains(t, filtered[0].Message, "Database")
}

func TestLogViewerModel_filterEntries_EmptyResults(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Info message"},
		{Timestamp: time.Now(), Level: "WARN", Message: "Warning message"},
	}

	model.entries = entries
	model.filter = &LogFilter{Level: "ERROR"}
	model.searchQuery = "nonexistent"

	filtered := model.filterEntries()

	assert.Len(t, filtered, 0)
}

func TestLogViewerModel_filterEntries_SearchInFields(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
			Fields:    map[string]interface{}{"user_id": "12345", "action": "login"},
		},
		{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Another message",
			Fields:    map[string]interface{}{"user_id": "67890", "action": "logout"},
		},
	}

	model.entries = entries
	model.searchQuery = "12345"

	filtered := model.filterEntries()

	assert.Len(t, filtered, 1)
	assert.Equal(t, "12345", filtered[0].Fields["user_id"])
}

func TestLogViewerModel_containsSearch_NoMatch(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "User logged in successfully",
	}

	model.searchQuery = "nonexistent"
	assert.False(t, model.containsSearch(entry))
}

func TestLogViewerModel_containsSearch_CaseInsensitive(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "User Logged In Successfully",
	}

	model.searchQuery = "logged"
	assert.True(t, model.containsSearch(entry))
}

func TestLogViewerModel_highlightSearch_MultipleMatches(t *testing.T) {
	model := NewLogViewerModel()
	model.searchQuery = "error"

	text := "error 1 and error 2 and error 3"

	highlighted := model.highlightSearch(text)

	assert.NotEmpty(t, highlighted)
}

func TestLogViewerModel_highlightSearch_EmptyQuery(t *testing.T) {
	model := NewLogViewerModel()
	model.searchQuery = ""

	text := "This is a test message"

	highlighted := model.highlightSearch(text)

	assert.Equal(t, text, highlighted)
}

func TestLogViewerModel_renderLogLine_LongMessage(t *testing.T) {
	model := NewLogViewerModel()

	entry := &state.LogEntry{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:     "INFO",
		Message:   "This is a very long message that should be truncated because it exceeds the maximum length allowed for display in the log viewer",
	}

	line := model.renderLogLine(0, entry)

	assert.NotEmpty(t, line)
	assert.Contains(t, line, "...")
}

func TestLogViewerModel_renderLogLine_WithSearch(t *testing.T) {
	model := NewLogViewerModel()
	model.searchQuery = "error"

	entry := &state.LogEntry{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:     "ERROR",
		Message:   "This is an error message",
	}

	line := model.renderLogLine(0, entry)

	assert.NotEmpty(t, line)
	assert.Contains(t, line, "ERROR")
}

func TestLogViewerModel_Update_ShowDetails_InvalidIndex(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.entries = []*state.LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Test message"},
	}
	model.selectedIdx = 5

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotNil(t, newModel)
	assert.False(t, newModel.showDetails)
}

func TestLogViewerModel_Update_Search(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	assert.NotNil(t, newModel)
}

func TestLogViewerModel_renderToolbar(t *testing.T) {
	model := NewLogViewerModel()
	model.filter = &LogFilter{Level: "ERROR"}
	model.autoScroll = true
	model.entries = []*state.LogEntry{
		{Timestamp: time.Now(), Level: "ERROR", Message: "Test"},
	}

	toolbar := model.renderToolbar()

	assert.NotEmpty(t, toolbar)
	assert.Contains(t, toolbar, "Filter")
	assert.Contains(t, toolbar, "Auto-scroll")
	assert.Contains(t, toolbar, "Entries")
}

func TestLogViewerModel_renderFilter(t *testing.T) {
	model := NewLogViewerModel()

	model.filter = &LogFilter{Level: ""}
	result := model.renderFilter()
	assert.Contains(t, result, "All")

	model.filter = &LogFilter{Level: "ERROR"}
	result = model.renderFilter()
	assert.Contains(t, result, "ERROR")
}

func TestLogViewerModel_renderAutoScroll(t *testing.T) {
	model := NewLogViewerModel()

	model.autoScroll = true
	result := model.renderAutoScroll()
	assert.Contains(t, result, "On")

	model.autoScroll = false
	result = model.renderAutoScroll()
	assert.Contains(t, result, "Off")
}

func TestLogViewerModel_renderStatusBar(t *testing.T) {
	model := NewLogViewerModel()

	statusBar := model.renderStatusBar()

	assert.NotEmpty(t, statusBar)
	assert.Contains(t, statusBar, "filter")
	assert.Contains(t, statusBar, "search")
	assert.Contains(t, statusBar, "scroll")
}

func TestLogViewerModel_renderDetails(t *testing.T) {
	model := NewLogViewerModel()
	model.width = 100
	model.selectedIdx = 0
	model.entries = []*state.LogEntry{
		{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Level:     "ERROR",
			Message:   "Database connection failed",
			Fields:    map[string]interface{}{"database": "mysql", "host": "localhost"},
		},
	}

	details := model.renderDetails()

	assert.NotEmpty(t, details)
	assert.Contains(t, details, "Log Entry Details")
	assert.Contains(t, details, "ERROR")
	assert.Contains(t, details, "Database connection failed")
	assert.Contains(t, details, "Fields")
}

func TestLogViewerModel_renderDetails_NoFields(t *testing.T) {
	model := NewLogViewerModel()
	model.width = 100
	model.selectedIdx = 0
	model.entries = []*state.LogEntry{
		{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Level:     "INFO",
			Message:   "Simple message",
		},
	}

	details := model.renderDetails()

	assert.NotEmpty(t, details)
	assert.Contains(t, details, "No fields")
}

func TestLogViewerModel_renderDetailField(t *testing.T) {
	model := NewLogViewerModel()

	field := model.renderDetailField("Key", "Value")

	assert.NotEmpty(t, field)
	assert.Contains(t, field, "Key")
	assert.Contains(t, field, "Value")
}

func TestLogViewerModel_updateViewportContent_EmptyEntries(t *testing.T) {
	model := NewLogViewerModel()
	model.width = 100
	model.height = 50

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	model, _ = model.Update(msg)

	model.entries = []*state.LogEntry{}
	model.updateViewportContent()

	content := model.viewport.View()
	assert.NotEmpty(t, content)
}

func TestLogViewerModel_Update_WindowSize(t *testing.T) {
	model := NewLogViewerModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 100, newModel.width)
	assert.True(t, newModel.ready)
}

func TestLogViewerModel_Update_WindowSize_Small(t *testing.T) {
	model := NewLogViewerModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 5}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 100, newModel.width)
	assert.Equal(t, 10, newModel.height)
}

func TestLogViewerModel_renderLogLine_AllLogLevels(t *testing.T) {
	model := NewLogViewerModel()

	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}

	for _, level := range levels {
		entry := &state.LogEntry{
			Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			Level:     level,
			Message:   "Test message",
		}

		line := model.renderLogLine(0, entry)

		assert.NotEmpty(t, line)
		assert.Contains(t, line, level)
	}
}

func TestLogViewerModel_filterEntries_AllLevels(t *testing.T) {
	model := NewLogViewerModel()

	entries := []*state.LogEntry{
		{Timestamp: time.Now(), Level: "DEBUG", Message: "Debug message"},
		{Timestamp: time.Now(), Level: "INFO", Message: "Info message"},
		{Timestamp: time.Now(), Level: "WARN", Message: "Warn message"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error message"},
	}

	model.entries = entries
	model.filter = &LogFilter{Level: ""}

	filtered := model.filterEntries()

	assert.Len(t, filtered, 4)
}

func TestLogViewerModel_View_DetailsMode_NoEntry(t *testing.T) {
	model := NewLogViewerModel()
	model.ready = true
	model.showDetails = true
	model.selectedIdx = -1

	view := model.View()

	assert.NotEmpty(t, view)
}
