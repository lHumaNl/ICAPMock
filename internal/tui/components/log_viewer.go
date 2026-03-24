// Package components provides UI components for the TUI.
package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icap-mock/icap-mock/internal/tui/state"
)

// LogViewerModel represents the log viewer component
type LogViewerModel struct {
	viewport    viewport.Model
	entries     []*state.LogEntry
	filter      *LogFilter
	searchQuery string
	searching   bool
	searchInput textinput.Model
	selectedIdx int
	autoScroll  bool
	showDetails bool
	ready       bool
	width       int
	height      int
}

// LogFilter defines filters for log entries
type LogFilter struct {
	Level string
}

// LogFilterMsg is sent when filter changes
type LogFilterMsg struct {
	Filter *LogFilter
}

// LogSearchMsg is sent when search query changes
type LogSearchMsg struct {
	Query string
}

// LogAutoScrollMsg is sent when auto-scroll toggle changes
type LogAutoScrollMsg struct {
	Enabled bool
}

// LogSelectMsg is sent when a log entry is selected
type LogSelectMsg struct {
	Entry *state.LogEntry
}

// NewLogViewerModel creates a new log viewer model
func NewLogViewerModel() *LogViewerModel {
	si := textinput.New()
	si.Placeholder = "Search logs..."
	si.CharLimit = 128
	si.Width = 40

	return &LogViewerModel{
		filter:      &LogFilter{Level: ""},
		searchQuery: "",
		searchInput: si,
		selectedIdx: -1,
		autoScroll:  true,
		showDetails: false,
		entries:     make([]*state.LogEntry, 0),
	}
}

// Init initializes the log viewer model
func (m *LogViewerModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the log viewer model
func (m *LogViewerModel) Update(msg tea.Msg) (*LogViewerModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height - 8 // Account for header and footer
		if m.height < 10 {
			m.height = 10
		}
		m.ready = true

		// Initialize viewport if needed
		if m.viewport.Width == 0 {
			m.viewport = viewport.New(msg.Width, m.height)
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = m.height
		}

		m.updateViewportContent()

	case tea.KeyMsg:
		if m.searching {
			switch msg.Type {
			case tea.KeyEnter:
				// Apply search
				m.searchQuery = m.searchInput.Value()
				m.searching = false
				m.searchInput.Blur()
				m.updateViewportContent()
				return m, nil
			case tea.KeyEsc:
				// Cancel search
				m.searching = false
				m.searchInput.Blur()
				m.searchInput.SetValue(m.searchQuery)
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		if !m.showDetails {
			// Handle main view keys
			switch msg.String() {
			case "f":
				// Cycle through filter levels
				m.cycleFilter()
				m.updateViewportContent()
				return m, nil

			case "/":
				// Enable search mode
				m.searching = true
				m.searchInput.Focus()
				return m, textinput.Blink

			case "a":
				// Toggle auto-scroll
				m.autoScroll = !m.autoScroll
				return m, func() tea.Msg {
					return LogAutoScrollMsg{Enabled: m.autoScroll}
				}

			case "up", "k":
				// Scroll up
				m.viewport.LineUp(1)

			case "down", "j":
				// Scroll down
				m.viewport.LineDown(1)

			case "pgup":
				// Page up
				m.viewport.HalfViewUp()

			case "pgdown":
				// Page down
				m.viewport.HalfViewDown()

			case "home", "g":
				// Go to top
				m.viewport.GotoTop()

			case "end", "G":
				// Go to bottom
				m.viewport.GotoBottom()

			case "enter":
				// Show log details
				if m.selectedIdx >= 0 && m.selectedIdx < len(m.entries) {
					m.showDetails = true
				}
			}
		} else {
			// Handle detail view keys
			switch msg.String() {
			case "q", "esc":
				// Close detail view
				m.showDetails = false
			}
		}
	}

	// Update viewport
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// SetEntries updates the log entries
func (m *LogViewerModel) SetEntries(entries []*state.LogEntry) {
	m.entries = entries
	m.updateViewportContent()

	// Auto-scroll to bottom if enabled
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// SetFilter sets the log level filter
func (m *LogViewerModel) SetFilter(filter *LogFilter) {
	m.filter = filter
	m.updateViewportContent()
}

// SetSearch sets the search query
func (m *LogViewerModel) SetSearch(query string) {
	m.searchQuery = query
	m.updateViewportContent()
}

// SetAutoScroll sets the auto-scroll state
func (m *LogViewerModel) SetAutoScroll(enabled bool) {
	m.autoScroll = enabled
}

// cycleFilter cycles through available filter levels
func (m *LogViewerModel) cycleFilter() {
	levels := []string{"", "DEBUG", "INFO", "WARN", "ERROR"}
	for i, level := range levels {
		if level == m.filter.Level {
			m.filter.Level = levels[(i+1)%len(levels)]
			return
		}
	}
	m.filter.Level = levels[0]
}

// updateViewportContent updates the viewport content
func (m *LogViewerModel) updateViewportContent() {
	filtered := m.filterEntries()

	if len(filtered) == 0 {
		m.viewport.SetContent(SubtitleStyle.Render("No log entries available"))
		return
	}

	var lines []string
	for i, entry := range filtered {
		lines = append(lines, m.renderLogLine(i, entry))
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
}

// filterEntries filters log entries based on current filter and search
func (m *LogViewerModel) filterEntries() []*state.LogEntry {
	var filtered []*state.LogEntry

	for _, entry := range m.entries {
		// Apply level filter
		if m.filter.Level != "" && entry.Level != m.filter.Level {
			continue
		}

		// Apply search filter
		if m.searchQuery != "" && !m.containsSearch(entry) {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

// containsSearch checks if entry contains the search query
func (m *LogViewerModel) containsSearch(entry *state.LogEntry) bool {
	if m.searchQuery == "" {
		return true
	}

	query := strings.ToLower(m.searchQuery)

	// Search in message
	if strings.Contains(strings.ToLower(entry.Message), query) {
		return true
	}

	// Search in level
	if strings.Contains(strings.ToLower(entry.Level), query) {
		return true
	}

	// Search in fields
	for key, value := range entry.Fields {
		if strings.Contains(strings.ToLower(key), query) {
			return true
		}
		if strVal, ok := value.(string); ok {
			if strings.Contains(strings.ToLower(strVal), query) {
				return true
			}
		}
	}

	return false
}

// renderLogLine renders a single log line
func (m *LogViewerModel) renderLogLine(idx int, entry *state.LogEntry) string {
	style := getLogLevelStyle(entry.Level)
	timestamp := entry.Timestamp.Format("2006-01-02 15:04:05")

	// Highlight search matches if searching
	message := entry.Message
	if m.searchQuery != "" {
		message = m.highlightSearch(message)
	}

	// Truncate message if too long
	if len(message) > 80 {
		message = message[:77] + "..."
	}

	line := fmt.Sprintf("[%s] %s: %s",
		timestamp,
		style.Render(entry.Level),
		message)

	// Mark selected entry
	if idx == m.selectedIdx {
		selectedStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("244")).
			Foreground(lipgloss.Color("255"))
		return selectedStyle.Render(line)
	}

	return line
}

// highlightSearch highlights search matches in text
func (m *LogViewerModel) highlightSearch(text string) string {
	if m.searchQuery == "" {
		return text
	}

	query := strings.ToLower(m.searchQuery)
	lowerText := strings.ToLower(text)

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("208"))

	var result strings.Builder
	lastIdx := 0

	for {
		idx := strings.Index(lowerText[lastIdx:], query)
		if idx == -1 {
			result.WriteString(text[lastIdx:])
			break
		}

		actualIdx := lastIdx + idx
		result.WriteString(text[lastIdx:actualIdx])
		result.WriteString(highlightStyle.Render(text[actualIdx : actualIdx+len(m.searchQuery)]))
		lastIdx = actualIdx + len(m.searchQuery)
	}

	return result.String()
}

// View renders the log viewer
func (m *LogViewerModel) View() string {
	if !m.ready {
		return "Loading log viewer..."
	}

	if m.showDetails && m.selectedIdx >= 0 && m.selectedIdx < len(m.entries) {
		return m.renderDetails()
	}

	// Render toolbar
	toolbar := m.renderToolbar()

	// Render viewport content
	var content string
	content = m.viewport.View()

	// Render status bar
	statusBar := m.renderStatusBar()

	// Combine all sections
	return lipgloss.JoinVertical(
		lipgloss.Left,
		toolbar,
		"",
		content,
		"",
		statusBar,
	)
}

// renderToolbar renders the toolbar
func (m *LogViewerModel) renderToolbar() string {
	if m.searching {
		return "Search: " + m.searchInput.View() + "  (Enter to apply, Esc to cancel)"
	}

	items := []string{
		fmt.Sprintf("Filter: %s", m.renderFilter()),
		fmt.Sprintf("Auto-scroll: %s", m.renderAutoScroll()),
		fmt.Sprintf("Entries: %d", len(m.filterEntries())),
	}

	if m.searchQuery != "" {
		items = append(items, fmt.Sprintf("Search: %q", m.searchQuery))
	}

	toolbarStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("250")).
		Padding(0, 1)

	itemStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)

	var itemsStyled []string
	for _, item := range items {
		parts := strings.SplitN(item, ": ", 2)
		if len(parts) == 2 {
			itemsStyled = append(itemsStyled,
				itemStyle.Render(parts[0])+": "+parts[1])
		} else {
			itemsStyled = append(itemsStyled, item)
		}
	}

	return toolbarStyle.Render(strings.Join(itemsStyled, "   "))
}

// renderFilter renders the filter status
func (m *LogViewerModel) renderFilter() string {
	if m.filter.Level == "" {
		return "All"
	}
	style := getLogLevelStyle(m.filter.Level)
	return style.Render(m.filter.Level)
}

// renderAutoScroll renders the auto-scroll status
func (m *LogViewerModel) renderAutoScroll() string {
	if m.autoScroll {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true).
			Render("On")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("Off")
}

// renderStatusBar renders the status bar with key hints
func (m *LogViewerModel) renderStatusBar() string {
	hints := []string{
		"f: filter",
		"/: search",
		"a: auto-scroll",
		"↑↓: scroll",
		"Enter: details",
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("75"))

	var hintsStyled []string
	for _, hint := range hints {
		parts := strings.SplitN(hint, ": ", 2)
		if len(parts) == 2 {
			hintsStyled = append(hintsStyled,
				hintStyle.Render(parts[0])+": "+parts[1])
		} else {
			hintsStyled = append(hintsStyled, hint)
		}
	}

	return statusStyle.Render(strings.Join(hintsStyled, "   "))
}

// renderDetails renders the log entry details
func (m *LogViewerModel) renderDetails() string {
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.entries) {
		return "No entry selected"
	}

	entry := m.entries[m.selectedIdx]

	details := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Log Entry Details"),
		"",
		SubtitleStyle.Render("Press 'q' or 'Esc' to close"),
		"",
		m.renderDetailField("Timestamp", entry.Timestamp.Format("2006-01-02 15:04:05.000")),
		m.renderDetailField("Level", entry.Level),
		m.renderDetailField("Message", entry.Message),
		"",
		TitleStyle.Render("Fields"),
		"",
	)

	if len(entry.Fields) == 0 {
		details += SubtitleStyle.Render("No fields")
	} else {
		for key, value := range entry.Fields {
			details += m.renderDetailField(key, fmt.Sprintf("%v", value)) + "\n"
		}
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("245")).
		Padding(1).
		Width(m.width - 4)

	return panelStyle.Render(details)
}

// renderDetailField renders a detail field
func (m *LogViewerModel) renderDetailField(key, value string) string {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(15)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	return labelStyle.Render(key+": ") + valueStyle.Render(value)
}

// GetFilter returns the current filter
func (m *LogViewerModel) GetFilter() *LogFilter {
	return m.filter
}

// GetSearch returns the current search query
func (m *LogViewerModel) GetSearch() string {
	return m.searchQuery
}

// IsSearching returns whether the log viewer is in search mode
func (m *LogViewerModel) IsSearching() bool {
	return m.searching
}

// IsAutoScrollEnabled returns whether auto-scroll is enabled
func (m *LogViewerModel) IsAutoScrollEnabled() bool {
	return m.autoScroll
}
