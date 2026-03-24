package tui

import (
	"time"
)

// TickMsg is sent periodically to refresh data
type TickMsg struct {
	Time time.Time
}

// ConfigChangedMsg is sent when configuration is modified
type ConfigChangedMsg struct {
	Config *ConfigSnapshot
}

// ScreenChangeMsg is sent to switch between screens
type ScreenChangeMsg struct {
	Screen Screen
}

// ErrorMessage is sent when an error occurs
type ErrorMessage struct {
	Err error
}

// SuccessMsg is sent when an operation succeeds
type SuccessMsg struct {
	Message string
}

// LogEntry represents a single log entry for view rendering
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]interface{}
}

// ConfigSnapshot represents configuration snapshot
type ConfigSnapshot struct {
	FilePath string
	Modified bool
	Valid    bool
	Error    string
}

// ConfigSavedMsg is sent when configuration is saved successfully
type ConfigSavedMsg struct {
	FilePath string
	Success  bool
	Error    string
}

// ShutdownSignal is sent when application should gracefully shutdown
type ShutdownSignal struct{}

// ShutdownCompleteMsg is sent when shutdown is complete
type ShutdownCompleteMsg struct{}
