// Copyright 2026 ICAP Mock

package tui

import (
	"time"
)

// TickMsg is sent periodically to refresh data.
type TickMsg struct {
	Time time.Time
}

// ConfigChangedMsg is sent when configuration is modified.
type ConfigChangedMsg struct {
	Config *ConfigSnapshot
}

// ScreenChangeMsg is sent to switch between screens.
type ScreenChangeMsg struct {
	Screen Screen
}

// ErrorMessage is sent when an error occurs.
type ErrorMessage struct {
	Err error
}

// SuccessMsg is sent when an operation succeeds.
type SuccessMsg struct {
	Message string
}

// LogEntry represents a single log entry for view rendering.
type LogEntry struct {
	Timestamp time.Time
	Fields    map[string]interface{}
	Level     string
	Message   string
}

// ConfigSnapshot represents configuration snapshot.
type ConfigSnapshot struct {
	FilePath string
	Error    string
	Modified bool
	Valid    bool
}

// ConfigSavedMsg is sent when configuration is saved successfully.
type ConfigSavedMsg struct {
	FilePath string
	Error    string
	Success  bool
}

// ShutdownSignal is sent when application should gracefully shutdown.
type ShutdownSignal struct{}

// ShutdownCompleteMsg is sent when shutdown is complete.
type ShutdownCompleteMsg struct{}
