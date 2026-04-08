// Copyright 2026 ICAP Mock

package components

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReplayRequestItem represents a recorded request in the list.
type ReplayRequestItem struct {
	Timestamp  time.Time
	ID         string
	Method     string
	Path       string
	Response   string
	StatusCode int
	Duration   time.Duration
	Selected   bool
}

// FilterValue implements list.Item.
func (r ReplayRequestItem) FilterValue() string {
	return r.ID
}

// Title implements list.Item.
func (r ReplayRequestItem) Title() string {
	timestamp := r.Timestamp.Format("15:04:05")
	if r.Selected {
		return fmt.Sprintf("[✓] %s - %s %s", timestamp, r.Method, r.Path)
	}
	return fmt.Sprintf("[ ] %s - %s %s", timestamp, r.Method, r.Path)
}

// Description implements list.Item.
func (r ReplayRequestItem) Description() string {
	return fmt.Sprintf("Status: %d | Duration: %v", r.StatusCode, r.Duration)
}

// ReplayViewMode represents the current view mode.
type ReplayViewMode int

const (
	ReplayViewList ReplayViewMode = iota
	ReplayViewResults
	ReplayViewDetail
)

// ReplayPanelModel represents the replay panel component.
type ReplayPanelModel struct {
	selectedRequest  *ReplayRequestItem
	replayResults    *ReplayResults
	requestList      *list.Model
	targetURL        string
	errorMessage     string
	requestDetail    string
	replaySpeed      float64
	progress         float64
	height           int
	width            int
	mode             ReplayViewMode
	mu               sync.RWMutex
	replayInProgress bool
	ready            bool
}

// ReplayResults contains replay execution results.
type ReplayResults struct {
	StartTime      time.Time
	EndTime        time.Time
	RequestResults []RequestResult
	TotalRequests  int
	SuccessCount   int
	FailureCount   int
	TotalDuration  time.Duration
	AverageLatency time.Duration
}

// RequestResult represents the result of a single replayed request.
type RequestResult struct {
	ID         string
	Error      string
	StatusCode int
	Duration   time.Duration
	Success    bool
}

// NewReplayPanelModel creates a new replay panel model.
func NewReplayPanelModel() *ReplayPanelModel {
	// Initialize list with custom delegate
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "15"}).
		Background(lipgloss.AdaptiveColor{Light: "54", Dark: "57"}).
		Bold(true)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "15"}).
		Background(lipgloss.AdaptiveColor{Light: "54", Dark: "57"})
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "254"})
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "241"})

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowPagination(true)
	l.SetShowHelp(false)

	return &ReplayPanelModel{
		mode:             ReplayViewList,
		requestList:      &l,
		replaySpeed:      1.0,
		targetURL:        "http://localhost:1344",
		replayInProgress: false,
		progress:         0.0,
		replayResults:    &ReplayResults{},
	}
}

// SetTargetURL sets the target URL for replay requests.
func (m *ReplayPanelModel) SetTargetURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if url != "" {
		m.targetURL = url
	}
}

// Init initializes the replay panel.
func (m *ReplayPanelModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the replay panel.
func (m *ReplayPanelModel) Update(msg tea.Msg) (*ReplayPanelModel, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Update list size
		m.requestList.SetWidth(m.width - 4)
		m.requestList.SetHeight(m.height - 12)

	case tea.KeyMsg:
		switch m.mode {
		case ReplayViewList:
			return m.handleListKeys(msg)
		case ReplayViewResults:
			return m.handleResultsKeys(msg)
		case ReplayViewDetail:
			// Detail view handled below
		}
	}

	// Update based on mode
	switch m.mode {
	case ReplayViewList:
		listCmd := m.updateList(msg)
		cmd = tea.Batch(cmd, listCmd)
	case ReplayViewResults:
		resultsCmd := m.updateResults(msg)
		cmd = tea.Batch(cmd, resultsCmd)
	case ReplayViewDetail:
		// No specific update for detail mode
	}

	return m, cmd
}

// handleListKeys handles key messages in list view.
func (m *ReplayPanelModel) handleListKeys(msg tea.KeyMsg) (*ReplayPanelModel, tea.Cmd) {
	switch msg.String() {
	case " ":
		// Toggle selection with type assertion safety
		selected := m.requestList.SelectedItem()
		if selected != nil {
			item, ok := selected.(ReplayRequestItem)
			if !ok {
				log.Printf("[ERROR] Failed type assertion for selected item: expected ReplayRequestItem, got %T", selected)
				m.errorMessage = "Failed to toggle selection: invalid item type"
				return m, nil
			}
			item.Selected = !item.Selected
			m.updateItem(item)
		}

	case "s":
		// Start replay if requests are selected
		if m.hasSelectedRequests() {
			m.startReplay()
			return m, nil
		}

	case "t":
		// Stop replay
		if m.replayInProgress {
			m.stopReplay()
			return m, nil
		}

	case "e":
		// Export results if available
		if m.replayResults != nil && len(m.replayResults.RequestResults) > 0 {
			return m, func() tea.Msg {
				return ReplayExportMsg{}
			}
		}

	case "1", "2", "3", "4":
		// Change speed
		speeds := map[string]float64{
			"1": 0.5,
			"2": 1.0,
			"3": 2.0,
			"4": 5.0,
		}
		if speed, ok := speeds[msg.String()]; ok {
			m.replaySpeed = speed
		}

	case keyEnter:
		// View details with type assertion safety
		selected := m.requestList.SelectedItem()
		if selected != nil {
			item, ok := selected.(ReplayRequestItem)
			if !ok {
				log.Printf("[ERROR] Failed type assertion for selected item: expected ReplayRequestItem, got %T", selected)
				m.errorMessage = "Failed to view details: invalid item type"
				return m, nil
			}
			m.selectedRequest = &item
			if err := m.switchToDetailView(); err != nil {
				m.errorMessage = err.Error()
			}
			return m, nil
		}

	case keyEsc:
		if m.mode == ReplayViewDetail {
			m.switchToListView()
			return m, nil
		}
	}

	return m, nil
}

// handleResultsKeys handles key messages in results view.
func (m *ReplayPanelModel) handleResultsKeys(msg tea.KeyMsg) (*ReplayPanelModel, tea.Cmd) {
	switch msg.String() {
	case "e":
		// Export results
		if m.replayResults != nil && len(m.replayResults.RequestResults) > 0 {
			return m, func() tea.Msg {
				return ReplayExportMsg{}
			}
		}

	case keyEsc:
		// Return to list
		m.switchToListView()
		return m, nil
	}

	return m, nil
}

// updateList updates the list view.
func (m *ReplayPanelModel) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	*m.requestList, cmd = m.requestList.Update(msg)
	return cmd
}

// updateResults updates the results view.
func (m *ReplayPanelModel) updateResults(_ tea.Msg) tea.Cmd {
	return nil
}

// updateItem updates an item in the list.
func (m *ReplayPanelModel) updateItem(item ReplayRequestItem) {
	items := m.requestList.Items()
	for i, listItem := range items {
		req, ok := listItem.(ReplayRequestItem)
		if !ok {
			log.Printf("[ERROR] Failed type assertion for list item at index %d: expected ReplayRequestItem, got %T", i, listItem)
			continue
		}
		if req.ID == item.ID {
			items[i] = item
			break
		}
	}
	m.requestList.SetItems(items)
}

// hasSelectedRequests returns true if at least one request is selected.
func (m *ReplayPanelModel) hasSelectedRequests() bool {
	items := m.requestList.Items()
	for _, item := range items {
		req, ok := item.(ReplayRequestItem)
		if !ok {
			log.Printf("[ERROR] Failed type assertion in hasSelectedRequests: expected ReplayRequestItem, got %T", item)
			continue
		}
		if req.Selected {
			return true
		}
	}
	return false
}

// getSelectedRequests returns all selected requests.
func (m *ReplayPanelModel) getSelectedRequests() []ReplayRequestItem {
	var selected []ReplayRequestItem
	items := m.requestList.Items()
	for _, item := range items {
		req, ok := item.(ReplayRequestItem)
		if !ok {
			log.Printf("[ERROR] Failed type assertion in getSelectedRequests: expected ReplayRequestItem, got %T", item)
			continue
		}
		if req.Selected {
			selected = append(selected, req)
		}
	}
	return selected
}

// SetRequests sets the list of requests.
func (m *ReplayPanelModel) SetRequests(requests []ReplayRequestItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]list.Item, len(requests))
	for i, r := range requests {
		items[i] = r
		log.Printf("[DEBUG] SetRequests: adding request %s at index %d", r.ID, i)
	}
	m.requestList.SetItems(items)
	log.Printf("[DEBUG] SetRequests: set %d items in list", len(items))
}

// SetReplayResults sets the replay results.
func (m *ReplayPanelModel) SetReplayResults(results *ReplayResults) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replayResults = results
}

// UpdateProgress updates the replay progress.
func (m *ReplayPanelModel) UpdateProgress(current, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if total > 0 {
		m.progress = float64(current) / float64(total)
	}
}

// startReplay starts the replay process.
func (m *ReplayPanelModel) startReplay() {
	m.replayInProgress = true
	m.replayResults = &ReplayResults{
		StartTime: time.Now(),
	}
	m.switchToResultsView()
}

// stopReplay stops the replay process.
func (m *ReplayPanelModel) stopReplay() {
	m.replayInProgress = false
	if m.replayResults != nil {
		m.replayResults.EndTime = time.Now()
		m.replayResults.TotalDuration = m.replayResults.EndTime.Sub(m.replayResults.StartTime)
	}
}

// switchToListView switches to list view.
func (m *ReplayPanelModel) switchToListView() {
	m.mode = ReplayViewList
	m.selectedRequest = nil
}

// switchToResultsView switches to results view.
func (m *ReplayPanelModel) switchToResultsView() {
	m.mode = ReplayViewResults
}

// switchToDetailView switches to detail view.
func (m *ReplayPanelModel) switchToDetailView() error {
	if m.selectedRequest == nil {
		return fmt.Errorf("no request selected for viewing details")
	}
	m.mode = ReplayViewDetail
	m.requestDetail = m.formatRequestDetail(m.selectedRequest)
	return nil
}

// formatRequestDetail formats request details for display.
func (m *ReplayPanelModel) formatRequestDetail(request *ReplayRequestItem) string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Request Details"),
		"",
		SubtitleStyle.Render(fmt.Sprintf("ID: %s", request.ID)),
		SubtitleStyle.Render(fmt.Sprintf("Timestamp: %s", request.Timestamp.Format("2006-01-02 15:04:05"))),
		SubtitleStyle.Render(fmt.Sprintf("Method: %s", request.Method)),
		SubtitleStyle.Render(fmt.Sprintf("Path: %s", request.Path)),
		SubtitleStyle.Render(fmt.Sprintf("Status: %d", request.StatusCode)),
		SubtitleStyle.Render(fmt.Sprintf("Duration: %v", request.Duration)),
		"",
		TitleStyle.Render("Response"),
		SubtitleStyle.Render(request.Response),
	)
}

// View renders the replay panel.
func (m *ReplayPanelModel) View() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.ready {
		return "Loading replay panel..."
	}

	switch m.mode {
	case ReplayViewList:
		return m.renderListView()
	case ReplayViewResults:
		return m.renderResultsView()
	case ReplayViewDetail:
		return m.renderDetailView()
	default:
		return "Unknown view mode"
	}
}

// renderListView renders the request list.
func (m *ReplayPanelModel) renderListView() string {
	// Render controls info
	controls := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Replay Controls"),
		"",
		SubtitleStyle.Render(fmt.Sprintf("Speed: %.1fx", m.replaySpeed)),
		SubtitleStyle.Render(fmt.Sprintf("Target: %s", m.targetURL)),
		SubtitleStyle.Render(fmt.Sprintf("Selected: %d requests", len(m.getSelectedRequests()))),
	)

	// Render help
	help := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  Space - Toggle selection"),
		SubtitleStyle.Render("  1-4 - Speed (0.5x, 1x, 2x, 5x)"),
		SubtitleStyle.Render("  s - Start replay"),
		SubtitleStyle.Render("  t - Stop replay"),
		SubtitleStyle.Render("  e - Export results"),
		SubtitleStyle.Render("  Enter - View details"),
	)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		controls,
		"",
		TitleStyle.Render("Recorded Requests"),
		"",
		m.requestList.View(),
		help,
	)

	if m.errorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			"",
			ErrorStyle.Render(m.errorMessage),
		)
	}

	return PanelStyle.Render(content)
}

// renderResultsView renders the replay results.
func (m *ReplayPanelModel) renderResultsView() string {
	// Render progress bar if replay is in progress
	var progressSection string
	if m.replayInProgress {
		barWidth := 40
		filled := int(m.progress * float64(barWidth))
		bar := "[" + strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled) + "]"
		progressSection = lipgloss.JoinVertical(
			lipgloss.Left,
			TitleStyle.Render("Replay Progress"),
			"",
			SubtitleStyle.Render(bar+fmt.Sprintf(" %.1f%%", m.progress*100)),
		)
	}

	// Define success and failure styles
	successStyle := SubtitleStyle.
		Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"}).
		Bold(true)
	failureStyle := SubtitleStyle.
		Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "196"}).
		Bold(true)

	// Render results
	results := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Replay Results"),
		"",
		SubtitleStyle.Render(fmt.Sprintf("Total: %d requests", m.replayResults.TotalRequests)),
		successStyle.Render(fmt.Sprintf("Success: %d", m.replayResults.SuccessCount)),
		failureStyle.Render(fmt.Sprintf("Failed: %d", m.replayResults.FailureCount)),
		"",
		SubtitleStyle.Render(fmt.Sprintf("Duration: %v", m.replayResults.TotalDuration)),
		SubtitleStyle.Render(fmt.Sprintf("Avg Latency: %v", m.replayResults.AverageLatency)),
	)

	// Render request results list
	var requestList string
	if len(m.replayResults.RequestResults) > 0 {
		var lines []string
		lines = append(lines, TitleStyle.Render("Request Results"), "")

		for i, result := range m.replayResults.RequestResults {
			statusStyle := lipgloss.NewStyle()
			if result.Success {
				statusStyle = statusStyle.Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"})
			} else {
				statusStyle = statusStyle.Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "196"})
			}

			lines = append(lines,
				statusStyle.Render(
					fmt.Sprintf("%d. %s - %s (%v)",
						i+1,
						result.ID,
						m.getStatusIcon(result.Success),
						result.Duration,
					),
				),
			)

			if result.Error != "" {
				lines = append(lines,
					SubtitleStyle.Render("   Error: "+result.Error),
				)
			}
		}

		requestList = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	// Render help
	help := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  e - Export results"),
		SubtitleStyle.Render("  esc - Back to list"),
	)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		progressSection,
		results,
		"",
		requestList,
		help,
	)

	return PanelStyle.Render(content)
}

// renderDetailView renders the request detail view.
func (m *ReplayPanelModel) renderDetailView() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.requestDetail,
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  esc - Back to list"),
	)

	if m.errorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			"",
			ErrorStyle.Render(m.errorMessage),
		)
	}

	return PanelStyle.Render(content)
}

// getStatusIcon returns status icon.
func (m *ReplayPanelModel) getStatusIcon(success bool) string {
	if success {
		return "✓"
	}
	return "✗"
}

// ReplayListMsg is sent when request list is received.
type ReplayListMsg struct {
	Requests []ReplayRequestItem
}

// ReplayStartMsg is sent to start replay.
type ReplayStartMsg struct {
	TargetURL string
	Requests  []ReplayRequestItem
	Speed     float64
}

// ReplayProgressMsg is sent with replay progress update.
type ReplayProgressMsg struct {
	Result  *RequestResult
	Current int
	Total   int
}

// ReplayCompleteMsg is sent when replay completes.
type ReplayCompleteMsg struct {
	Results *ReplayResults
}

// ReplayStopMsg is sent to stop replay.
type ReplayStopMsg struct{}

// ReplayExportMsg is sent to export replay results.
type ReplayExportMsg struct{}

// ReplayErrorMsg is sent when a replay operation fails.
type ReplayErrorMsg struct {
	Err error
}
