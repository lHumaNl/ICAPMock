// Copyright 2026 ICAP Mock

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLogsState(t *testing.T) {
	cfg := DefaultClientConfig()

	state := NewLogsState(cfg)

	assert.NotNil(t, state)
	assert.NotNil(t, state.client)
	assert.NotNil(t, state.entries)
	assert.NotNil(t, state.filter)
	assert.Equal(t, 100, state.maxLines)
	assert.True(t, state.autoScroll)
	assert.Equal(t, 0, state.entries.Size())
}

func TestLogsState_AddEntry(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Test message",
		Fields:    map[string]interface{}{"key": "value"},
	}

	msg := state.AddEntry(entry)

	assert.IsType(t, LogEntryMsg{}, msg)
	entryMsg := msg.(LogEntryMsg)
	assert.Equal(t, entry, entryMsg.Entry)
	assert.Equal(t, 1, state.entries.Size())
}

func TestLogsState_AddEntry_MaxLines(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	for i := 0; i < 105; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		state.AddEntry(entry)
	}

	assert.Equal(t, 100, state.entries.Size())
}

func TestLogsState_GetEntries_NoFilter(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entries := []*LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Message 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error 1"},
		{Timestamp: time.Now(), Level: "WARN", Message: "Warning 1"},
	}

	for _, entry := range entries {
		state.AddEntry(entry)
	}

	result := state.GetEntries(nil, 0)
	assert.Len(t, result, 3)
}

func TestLogsState_GetEntries_WithFilter(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entries := []*LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "Message 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error 1"},
		{Timestamp: time.Now(), Level: "WARN", Message: "Warning 1"},
		{Timestamp: time.Now(), Level: "ERROR", Message: "Error 2"},
	}

	for _, entry := range entries {
		state.AddEntry(entry)
	}

	filter := &LogFilter{Level: "ERROR"}
	result := state.GetEntries(filter, 0)

	assert.Len(t, result, 2)
	assert.Equal(t, "ERROR", result[0].Level)
	assert.Equal(t, "ERROR", result[1].Level)
}

func TestLogsState_GetEntries_WithLimit(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	for i := 0; i < 20; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		state.AddEntry(entry)
	}

	result := state.GetEntries(nil, 5)
	assert.Len(t, result, 5)
}

func TestLogsState_GetEntries_WithSearch(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entries := []*LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "User logged in"},
		{Timestamp: time.Now(), Level: "INFO", Message: "User logged out"},
		{Timestamp: time.Now(), Level: "INFO", Message: "Database connection established"},
	}

	for _, entry := range entries {
		state.AddEntry(entry)
	}

	filter := &LogFilter{Search: "logged"}
	result := state.GetEntries(filter, 0)

	assert.Len(t, result, 2)
	for _, r := range result {
		assert.Contains(t, r.Message, "logged")
	}
}

func TestLogsState_matchesFilter_Level(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{Level: "ERROR", Message: "Test error"}

	filter := &LogFilter{Level: "ERROR"}
	assert.True(t, state.matchesFilter(entry, filter))

	filter.Level = "INFO"
	assert.False(t, state.matchesFilter(entry, filter))
}

func TestLogsState_matchesFilter_Search(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	entry := &LogEntry{Level: "INFO", Message: "User logged in successfully"}

	filter := &LogFilter{Search: "logged"}
	assert.True(t, state.matchesFilter(entry, filter))

	filter.Search = "database"
	assert.False(t, state.matchesFilter(entry, filter))
}

func TestLogsState_SetFilter(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	cmd := state.SetFilter("ERROR")
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, LogFilterMsg{}, msg)

	filterMsg := msg.(LogFilterMsg)
	assert.Equal(t, "ERROR", filterMsg.Level)
	assert.Equal(t, "ERROR", state.filter.Level)
}

func TestLogsState_SetSearch(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	cmd := state.SetSearch("error")
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, LogSearchMsg{}, msg)

	searchMsg := msg.(LogSearchMsg)
	assert.Equal(t, "error", searchMsg.Query)
	assert.Equal(t, "error", state.filter.Search)
}

func TestLogsState_SetAutoScroll(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	cmd := state.SetAutoScroll(false)
	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, LogAutoScrollMsg{}, msg)

	autoScrollMsg := msg.(LogAutoScrollMsg)
	assert.False(t, autoScrollMsg.Enabled)
	assert.False(t, state.autoScroll)
}

func TestLogsState_IsAutoScrollEnabled(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	assert.True(t, state.IsAutoScrollEnabled())

	state.SetAutoScroll(false)
	assert.False(t, state.IsAutoScrollEnabled())
}

func TestLogsState_GetFilter(t *testing.T) {
	state := NewLogsState(&ClientConfig{})
	state.SetFilter("WARN")

	filter := state.GetFilter()
	assert.NotNil(t, filter)
	assert.Equal(t, "WARN", filter.Level)
}

func TestLogsState_GetSearch(t *testing.T) {
	state := NewLogsState(&ClientConfig{})
	state.SetSearch("test")

	search := state.GetSearch()
	assert.Equal(t, "test", search)
}

func TestLogsState_StartStreaming(t *testing.T) {
	cfg := &ClientConfig{LogsURL: "http://localhost:8080/logs"}
	state := NewLogsState(cfg)

	cmd := state.StartStreaming()
	assert.NotNil(t, cmd)
	assert.True(t, state.streaming)
}

func TestLogsState_Refresh(t *testing.T) {
	cfg := &ClientConfig{LogsURL: "http://localhost:8080/logs"}
	state := NewLogsState(cfg)

	cmd := state.Refresh()
	assert.NotNil(t, cmd)
}

func TestLogsState_UpdateEntries_Merge(t *testing.T) {
	state := NewLogsState(&ClientConfig{})

	existingEntry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   "Existing message",
	}
	state.AddEntry(existingEntry)

	newEntries := []*LogEntry{
		{
			Timestamp: existingEntry.Timestamp,
			Level:     "INFO",
			Message:   "Existing message",
		},
		{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "New message",
		},
	}

	state.UpdateEntries(newEntries)

	assert.Equal(t, 2, state.entries.Size())
}

func TestLogsState_UpdateEntries_MaxLines(t *testing.T) {
	logsState := NewLogsState(&ClientConfig{})

	for i := 0; i < 100; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Test message",
		}
		logsState.AddEntry(entry)
	}

	newEntries := []*LogEntry{
		{Timestamp: time.Now(), Level: "INFO", Message: "New 1"},
		{Timestamp: time.Now(), Level: "INFO", Message: "New 2"},
		{Timestamp: time.Now(), Level: "INFO", Message: "New 3"},
	}

	logsState.UpdateEntries(newEntries)

	assert.Equal(t, 100, logsState.entries.Size())
}

func TestLogsState_StopStreaming(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewLogsState(cfg)

	// Start streaming
	state.StartStreaming()
	assert.True(t, state.streaming)

	// Stop streaming
	state.StopStreaming()
	assert.False(t, state.streaming)
}

func TestLogsState_StopStreaming_Multiple(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewLogsState(cfg)

	// Multiple stop calls should not panic
	assert.NotPanics(t, func() {
		state.StopStreaming()
		state.StopStreaming()
		state.StopStreaming()
	})
}

func TestLogsState_Shutdown(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewLogsState(cfg)

	// Start streaming
	state.StartStreaming()

	// Shutdown
	state.Shutdown()
	assert.False(t, state.streaming)
}

func TestLogsState_Shutdown_Nil(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewLogsState(cfg)

	// Shutdown without start should not panic
	assert.NotPanics(t, func() {
		state.Shutdown()
	})
}
