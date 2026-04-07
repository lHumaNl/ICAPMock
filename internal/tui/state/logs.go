// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

// LogsState manages log entries for the TUI.
type LogsState struct {
	entries    *utils.RingBuffer[*LogEntry]
	filter     *LogFilter
	client     *LogsClient
	cancel     context.CancelFunc
	search     string
	maxLines   int
	mu         sync.RWMutex
	autoScroll bool
	streaming  bool
}

// LogEntry represents a single log entry.
type LogEntry struct {
	Timestamp time.Time
	Fields    map[string]interface{}
	Level     string
	Message   string
}

// LogFilter defines filters for log entries.
type LogFilter struct {
	Level  string
	Search string
}

// NewLogsState creates a new logs state with provided configuration.
func NewLogsState(cfg *ClientConfig) *LogsState {
	// Validate configuration
	if cfg == nil {
		cfg = DefaultClientConfig()
	} else {
		// Merge with defaults to fill in missing fields
		defaultCfg := DefaultClientConfig()
		if cfg.MetricsURL == "" {
			cfg.MetricsURL = defaultCfg.MetricsURL
		}
		if cfg.LogsURL == "" {
			cfg.LogsURL = defaultCfg.LogsURL
		}
		if cfg.StatusURL == "" {
			cfg.StatusURL = defaultCfg.StatusURL
		}
		if cfg.Timeout <= 0 {
			cfg.Timeout = defaultCfg.Timeout
		}
		if cfg.MaxConcurrentRequests <= 0 {
			cfg.MaxConcurrentRequests = defaultCfg.MaxConcurrentRequests
		}
		if cfg.RequestInterval == 0 {
			cfg.RequestInterval = defaultCfg.RequestInterval
		}
		if cfg.RetryMax < 0 {
			cfg.RetryMax = defaultCfg.RetryMax
		}
		if cfg.MaxLogs <= 0 {
			cfg.MaxLogs = defaultCfg.MaxLogs
		}
	}

	return &LogsState{
		entries:    utils.NewRingBuffer[*LogEntry](cfg.MaxLogs),
		maxLines:   cfg.MaxLogs,
		filter:     &LogFilter{},
		search:     "",
		autoScroll: true,
		client:     NewLogsClient(cfg),
	}
}

// StartStreaming begins streaming log updates.
func (s *LogsState) StartStreaming() tea.Cmd {
	s.mu.Lock()
	s.streaming = true
	s.mu.Unlock()

	// Initial load of logs
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		entries, err := s.client.GetLogs(ctx, 50)
		if err != nil {
			fmt.Printf("Error fetching logs: %v\n", err)
			return LogStreamErrorMsg{Error: err}
		}

		for _, entry := range entries {
			s.AddEntry(entry)
		}

		return nil
	}
}

// StreamLogs periodically fetches new logs.
func (s *LogsState) StreamLogs() tea.Cmd {
	return func() tea.Msg {
		s.mu.RLock()
		streaming := s.streaming
		s.mu.RUnlock()

		if !streaming {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Fetch latest logs (small batch)
		entries, err := s.client.GetLogs(ctx, 10)
		if err != nil {
			// Log error but don't interrupt streaming
			fmt.Printf("Error streaming logs: %v\n", err)
			return LogStreamErrorMsg{Error: err}
		}

		// Add only new entries
		s.mu.Lock()
		latestTimestamp := time.Time{}
		allEntries := s.entries.GetAll()
		if len(allEntries) > 0 {
			latestTimestamp = allEntries[len(allEntries)-1].Timestamp
		}
		s.mu.Unlock()

		for _, entry := range entries {
			if entry.Timestamp.After(latestTimestamp) {
				s.AddEntry(entry)
			}
		}

		return nil
	}
}

// LogStreamErrorMsg is sent when there's an error during log streaming.
type LogStreamErrorMsg struct {
	Error error
}

// AddEntry adds a new log entry.
func (s *LogsState) AddEntry(entry *LogEntry) tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries.Add(entry)

	return LogEntryMsg{Entry: entry}
}

// GetEntries returns log entries with optional filtering.
func (s *LogsState) GetEntries(filter *LogFilter, limit int) []*LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if filter == nil {
		filter = s.filter
	}

	// Apply filter and limit
	allEntries := s.entries.GetAll()
	var filtered []*LogEntry
	for i := len(allEntries) - 1; i >= 0; i-- {
		entry := allEntries[i]
		if s.matchesFilter(entry, filter) {
			filtered = append(filtered, entry)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}

	// Reverse to show newest first
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered
}

// matchesFilter checks if an entry matches the filter.
func (s *LogsState) matchesFilter(entry *LogEntry, filter *LogFilter) bool {
	if entry == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if filter.Level != "" && entry.Level != filter.Level {
		return false
	}
	if filter.Search != "" && !containsString(entry.Message, filter.Search) {
		return false
	}
	return true
}

// SetFilter sets the log level filter.
func (s *LogsState) SetFilter(level string) tea.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.filter.Level = level

	return func() tea.Msg {
		return LogFilterMsg{Level: level}
	}
}

// SetSearch sets the search query.
func (s *LogsState) SetSearch(query string) tea.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.filter.Search = query
	s.search = query

	return func() tea.Msg {
		return LogSearchMsg{Query: query}
	}
}

// SetAutoScroll sets the auto-scroll state.
func (s *LogsState) SetAutoScroll(enabled bool) tea.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.autoScroll = enabled

	return func() tea.Msg {
		return LogAutoScrollMsg{Enabled: enabled}
	}
}

// IsAutoScrollEnabled returns whether auto-scroll is enabled.
func (s *LogsState) IsAutoScrollEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.autoScroll
}

// GetFilter returns the current filter.
func (s *LogsState) GetFilter() *LogFilter {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.filter
}

// GetSearch returns the current search query.
func (s *LogsState) GetSearch() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.search
}

// LogFilterMsg is sent when filter changes.
type LogFilterMsg struct {
	Level string
}

// LogSearchMsg is sent when search query changes.
type LogSearchMsg struct {
	Query string
}

// LogAutoScrollMsg is sent when auto-scroll toggle changes.
type LogAutoScrollMsg struct {
	Enabled bool
}

// Refresh fetches the latest logs.
func (s *LogsState) Refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		entries, err := s.client.GetLogs(ctx, 100)
		if err != nil {
			// Return error message for proper error handling
			fmt.Printf("Error refreshing logs: %v\n", err)
			return LogRefreshErrorMsg{Error: err}
		}

		return LogsUpdatedMsg{Entries: entries}
	}
}

// LogRefreshErrorMsg is sent when there's an error during log refresh.
type LogRefreshErrorMsg struct {
	Error error
}

// LogsUpdatedMsg is sent when logs are refreshed.
type LogsUpdatedMsg struct {
	Entries []*LogEntry
}

// UpdateEntries updates the log entries with new data.
func (s *LogsState) UpdateEntries(entries []*LogEntry) {
	if entries == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Merge new entries, avoiding duplicates
	existingSet := make(map[string]bool)
	for _, entry := range s.entries.GetAll() {
		key := fmt.Sprintf("%d-%s", entry.Timestamp.UnixNano(), entry.Message)
		existingSet[key] = true
	}

	for _, entry := range entries {
		if entry == nil {
			continue
		}
		key := fmt.Sprintf("%d-%s", entry.Timestamp.UnixNano(), entry.Message)
		if !existingSet[key] {
			s.entries.Add(entry)
		}
	}

	// RingBuffer automatically enforces max limit
}

// containsString checks if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || indexOfString(s, substr) >= 0)
}

// indexOfString finds the index of a substring.
func indexOfString(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	for i := 0; i <= len(s)-n; i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}

// LogEntryMsg is sent when a new log entry arrives.
type LogEntryMsg struct {
	Entry *LogEntry
}

// StopStreaming stops the log streaming.
func (s *LogsState) StopStreaming() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.streaming = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Shutdown releases all resources.
func (s *LogsState) Shutdown() {
	s.StopStreaming()
}
