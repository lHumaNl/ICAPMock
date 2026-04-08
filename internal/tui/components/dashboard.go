// Copyright 2026 ICAP Mock

package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/icap-mock/icap-mock/internal/tui/state"
)

// Shared styles for components (exported for other components to use).
var (
	// Title style - bold, primary color with padding.
	TitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "125", Dark: "205"}).
			Bold(true).
			Padding(0, 1)

	// Subtitle style - muted color with padding.
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"}).
			Padding(0, 1)

	// Panel style - bordered with padding.
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "250", Dark: "245"}).
			Padding(1)

	// Status styles.
	StatusRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"}).
				Bold(true)

	StatusStoppedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "196"}).
				Bold(true)

	StatusWarningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "208"}).
				Bold(true)

	// Error style.
	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "196"}).
			Bold(true)
)

// DashboardModel represents the dashboard screen model.
type DashboardModel struct {
	metricsCards *MetricsCardsModel
	metricsGraph *MetricsGraphModel
	logPreview   *LogPreviewModel
	connections  *ConnectionsModel
	width        int
	height       int
	ready        bool
}

// NewDashboardModel creates a new dashboard model.
func NewDashboardModel() *DashboardModel {
	return &DashboardModel{
		metricsCards: NewMetricsCardsModel(),
		metricsGraph: NewMetricsGraphModel(),
		logPreview:   NewLogPreviewModel(),
		connections:  NewConnectionsModel(),
	}
}

// Init initializes the dashboard model.
func (m *DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.metricsCards.Init(),
		m.metricsGraph.Init(),
		m.logPreview.Init(),
		m.connections.Init(),
	)
}

// Update handles messages and updates the dashboard model.
func (m *DashboardModel) Update(msg tea.Msg) (*DashboardModel, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)

	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
	}

	// Update sub-components
	var cmd tea.Cmd
	m.metricsCards, cmd = m.metricsCards.Update(msg)
	cmds = append(cmds, cmd)

	m.metricsGraph, cmd = m.metricsGraph.Update(msg)
	cmds = append(cmds, cmd)

	m.logPreview, cmd = m.logPreview.Update(msg)
	cmds = append(cmds, cmd)

	m.connections, cmd = m.connections.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// SetMetrics updates metrics in dashboard components.
func (m *DashboardModel) SetMetrics(snapshot *state.MetricsSnapshot) {
	m.metricsCards.SetMetrics(snapshot)
	m.metricsGraph.SetMetrics(snapshot)
	m.connections.SetMetrics(snapshot)
}

// AddLogEntry adds a log entry to the preview.
func (m *DashboardModel) AddLogEntry(entry *state.LogEntry) {
	m.logPreview.AddEntry(entry)
}

// View renders the dashboard.
func (m *DashboardModel) View() string {
	if !m.ready {
		return "Loading dashboard..."
	}

	// Calculate layout proportions
	headerHeight := 2
	footerHeight := 3
	metricsCardsHeight := 8
	graphHeight := 12
	logConnHeight := m.height - headerHeight - metricsCardsHeight - graphHeight - footerHeight
	if logConnHeight < 0 {
		logConnHeight = 0
	}

	// Render top section: metrics cards
	topSection := m.metricsCards.View()

	// Render middle section: metrics graph
	middleSection := m.metricsGraph.View()

	// Render bottom section: log preview + connections
	logSection := m.logPreview.View()
	connSection := m.connections.View()

	// Style log and connection sections
	logPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "250", Dark: "245"}).
		Height(logConnHeight).
		Width(m.width/2 - 1)

	connPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "250", Dark: "245"}).
		Height(logConnHeight).
		Width(m.width/2 - 1)

	logPanel := logPanelStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			TitleStyle.Render("Recent Logs"),
			"",
			logSection,
		),
	)

	connPanel := connPanelStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			TitleStyle.Render("Connections"),
			"",
			connSection,
		),
	)

	bottomSection := lipgloss.JoinHorizontal(
		lipgloss.Top,
		logPanel,
		connPanel,
	)

	// Combine all sections
	return lipgloss.JoinVertical(
		lipgloss.Top,
		topSection,
		"",
		middleSection,
		"",
		bottomSection,
	)
}

// MetricsCardsModel displays metric cards.
type MetricsCardsModel struct {
	metrics *state.MetricsSnapshot
}

// NewMetricsCardsModel creates a new metrics cards model.
func NewMetricsCardsModel() *MetricsCardsModel {
	return &MetricsCardsModel{
		metrics: &state.MetricsSnapshot{},
	}
}

// Init initializes metrics cards model.
func (m *MetricsCardsModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates metrics cards model.
func (m *MetricsCardsModel) Update(_ tea.Msg) (*MetricsCardsModel, tea.Cmd) {
	return m, nil
}

// SetMetrics updates the current metrics.
func (m *MetricsCardsModel) SetMetrics(snapshot *state.MetricsSnapshot) {
	m.metrics = snapshot
}

// View renders the metrics cards.
func (m *MetricsCardsModel) View() string {
	// Define metric cards
	cards := []struct {
		label  string
		value  string
		color  lipgloss.AdaptiveColor
		format string
	}{
		{"RPS", fmt.Sprintf("%.2f", m.metrics.RPS), lipgloss.AdaptiveColor{Light: "28", Dark: "46"}, "req/s"},
		{"P50 Latency", fmt.Sprintf("%.2f ms", m.metrics.LatencyP50), lipgloss.AdaptiveColor{Light: "26", Dark: "75"}, ""},
		{"P95 Latency", fmt.Sprintf("%.2f ms", m.metrics.LatencyP95), lipgloss.AdaptiveColor{Light: "130", Dark: "208"}, ""},
		{"P99 Latency", fmt.Sprintf("%.2f ms", m.metrics.LatencyP99), lipgloss.AdaptiveColor{Light: "124", Dark: "196"}, ""},
		{"Connections", fmt.Sprintf("%d", m.metrics.Connections), lipgloss.AdaptiveColor{Light: "178", Dark: "229"}, ""},
		{"Errors", fmt.Sprintf("%d", m.metrics.Errors), m.getErrorColor(), ""},
	}

	// Render each card
	renderedCards := make([]string, 0, len(cards))
	for _, card := range cards {
		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(card.color).
			Padding(0, 2).
			Width(20).
			Height(5)

		valueStyle := lipgloss.NewStyle().
			Foreground(card.color).
			Bold(true)

		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"})

		cardContent := lipgloss.JoinVertical(
			lipgloss.Left,
			labelStyle.Render(card.label),
			"",
			valueStyle.Render(card.value),
		)

		renderedCards = append(renderedCards, cardStyle.Render(cardContent))
	}

	// Join cards horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, renderedCards...)
}

// getErrorColor returns color based on error count.
func (m *MetricsCardsModel) getErrorColor() lipgloss.AdaptiveColor {
	if m.metrics.Errors > 0 {
		return lipgloss.AdaptiveColor{Light: "124", Dark: "196"} // Red
	}
	return lipgloss.AdaptiveColor{Light: "28", Dark: "46"} // Green
}

// MetricsGraphModel displays ASCII charts for metrics trends.
type MetricsGraphModel struct {
	history    []*state.MetricsSnapshot
	maxHistory int
	width      int
	height     int
}

// NewMetricsGraphModel creates a new metrics graph model.
func NewMetricsGraphModel() *MetricsGraphModel {
	return &MetricsGraphModel{
		history:    make([]*state.MetricsSnapshot, 0),
		maxHistory: 60, // Keep 60 seconds of history
		width:      80,
		height:     10,
	}
}

// Init initializes metrics graph model.
func (m *MetricsGraphModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates metrics graph model.
func (m *MetricsGraphModel) Update(msg tea.Msg) (*MetricsGraphModel, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width - 4 // Account for borders
		if m.width < 20 {
			m.width = 20
		}
	}

	return m, nil
}

// SetMetrics updates the history with new snapshot.
func (m *MetricsGraphModel) SetMetrics(snapshot *state.MetricsSnapshot) {
	m.history = append(m.history, snapshot)
	if len(m.history) > m.maxHistory {
		m.history = m.history[1:]
	}
}

// View renders the metrics graph.
func (m *MetricsGraphModel) View() string {
	if len(m.history) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"}).Render("Waiting for metrics data...")
	}

	// Create RPS graph
	rpsGraph := m.createGraph(m.extractRPS(), "Requests Per Second", lipgloss.AdaptiveColor{Light: "28", Dark: "46"})

	// Create latency graph
	latencyGraph := m.createGraph(m.extractLatency(), "Latency (P50/P95/P99)", lipgloss.AdaptiveColor{Light: "130", Dark: "208"})

	// Combine graphs vertically
	graphPanel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "250", Dark: "245"}).
		Padding(1)

	return graphPanel.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			TitleStyle.Render("Metrics History (Last 60s)"),
			"",
			rpsGraph,
			"",
			latencyGraph,
		),
	)
}

// extractRPS extracts RPS values from history.
func (m *MetricsGraphModel) extractRPS() []float64 {
	values := make([]float64, len(m.history))
	for i, snapshot := range m.history {
		values[i] = snapshot.RPS
	}
	return values
}

// extractLatency extracts latency values from history.
func (m *MetricsGraphModel) extractLatency() []float64 {
	values := make([]float64, len(m.history))
	for i, snapshot := range m.history {
		values[i] = snapshot.LatencyP95
	}
	return values
}

// createGraph creates an ASCII chart from values.
func (m *MetricsGraphModel) createGraph(values []float64, title string, color lipgloss.AdaptiveColor) string {
	if len(values) == 0 {
		return SubtitleStyle.Render("No data available")
	}

	// Find minVal and maxVal for scaling
	minVal, maxVal := m.findMinMax(values)
	if maxVal == minVal {
		maxVal = minVal + 1.0
	}

	// Create chart
	graphWidth := m.width - len(title) - 10
	if graphWidth < 10 {
		graphWidth = 10
	}

	if len(values) > graphWidth {
		values = values[len(values)-graphWidth:]
	}

	// Generate ASCII chart
	lines := make([]string, 0, 3)
	lines = append(lines, fmt.Sprintf("%s: %s", title, colorValue(m.currentValue(values), color)))

	chartLine := m.generateChartLine(values, minVal, maxVal, graphWidth, color)
	lines = append(lines, chartLine,
		// Add scale
		fmt.Sprintf("%s%s",
			SubtitleStyle.Render("0"),
			SubtitleStyle.Render(fmt.Sprintf(" %.2f", maxVal)),
		),
	)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// findMinMax finds minimum and maximum values.
func (m *MetricsGraphModel) findMinMax(values []float64) (minVal, maxVal float64) {
	if len(values) == 0 {
		return 0, 0
	}

	minVal = values[0]
	maxVal = values[0]

	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	return minVal, maxVal
}

// currentValue returns the most recent value.
func (m *MetricsGraphModel) currentValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

// generateChartLine generates a single line of ASCII chart.
func (m *MetricsGraphModel) generateChartLine(values []float64, minVal, maxVal float64, _ int, color lipgloss.AdaptiveColor) string {
	if len(values) == 0 {
		return ""
	}

	// Define chart characters
	low := "▁"
	midLow := "▂"
	mid := "▃"
	midHigh := "▄"
	highMid := "▅"
	high := "▆"
	veryHigh := "▇"
	maxChar := "█"

	// Map each value to a character
	var chars []string
	step := (maxVal - minVal) / 8.0

	for _, v := range values {
		var char string
		switch {
		case maxVal == minVal:
			char = mid
		case v < minVal+step:
			char = low
		case v < minVal+2.0*step:
			char = midLow
		case v < minVal+3.0*step:
			char = mid
		case v < minVal+4.0*step:
			char = midHigh
		case v < minVal+5.0*step:
			char = highMid
		case v < minVal+6.0*step:
			char = high
		case v < minVal+7.0*step:
			char = veryHigh
		default:
			char = maxChar
		}
		chars = append(chars, char)
	}

	// Join characters
	line := strings.Join(chars, "")

	// Colorize
	colorStyle := lipgloss.NewStyle().Foreground(color)
	return colorStyle.Render(line)
}

// colorValue formats a value with color.
func colorValue(value float64, color lipgloss.AdaptiveColor) string {
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	return style.Render(fmt.Sprintf("%.2f", value))
}

// LogPreviewModel displays recent log entries.
type LogPreviewModel struct {
	entries  []*state.LogEntry
	maxLines int
}

// NewLogPreviewModel creates a new log preview model.
func NewLogPreviewModel() *LogPreviewModel {
	return &LogPreviewModel{
		entries:  make([]*state.LogEntry, 0),
		maxLines: 5,
	}
}

// Init initializes log preview model.
func (m *LogPreviewModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates log preview model.
func (m *LogPreviewModel) Update(_ tea.Msg) (*LogPreviewModel, tea.Cmd) {
	return m, nil
}

// AddEntry adds a log entry.
func (m *LogPreviewModel) AddEntry(entry *state.LogEntry) {
	m.entries = append(m.entries, entry)
	if len(m.entries) > m.maxLines {
		m.entries = m.entries[1:]
	}
}

// View renders the log preview.
func (m *LogPreviewModel) View() string {
	if len(m.entries) == 0 {
		return SubtitleStyle.Render("No recent logs")
	}

	var rendered []string
	for _, entry := range m.entries {
		if entry == nil {
			continue
		}
		style := getLogLevelStyle(entry.Level)
		rendered = append(rendered,
			style.Render(
				fmt.Sprintf("[%s] %s",
					entry.Timestamp.Format("15:04:05"),
					truncate(entry.Message, 50),
				),
			),
		)
	}

	if len(rendered) == 0 {
		return SubtitleStyle.Render("No recent logs")
	}

	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

// truncate truncates a string to max length.
func truncate(s string, maxLen int) string {
	if maxLen < 0 || s == "" {
		return ""
	}
	if maxLen == 0 {
		return "..."
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ConnectionsModel displays connection information.
type ConnectionsModel struct {
	metrics *state.MetricsSnapshot
}

// NewConnectionsModel creates a new connections model.
func NewConnectionsModel() *ConnectionsModel {
	return &ConnectionsModel{
		metrics: &state.MetricsSnapshot{},
	}
}

// Init initializes connections model.
func (m *ConnectionsModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates connections model.
func (m *ConnectionsModel) Update(_ tea.Msg) (*ConnectionsModel, tea.Cmd) {
	return m, nil
}

// SetMetrics updates the current metrics.
func (m *ConnectionsModel) SetMetrics(snapshot *state.MetricsSnapshot) {
	m.metrics = snapshot
}

// View renders the connections panel.
func (m *ConnectionsModel) View() string {
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "46"}).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"})

	return lipgloss.JoinVertical(
		lipgloss.Left,
		activeStyle.Render(fmt.Sprintf("Active: %d", m.metrics.Connections)),
		"",
		inactiveStyle.Render("Bytes Sent:"),
		activeStyle.Render(formatBytes(m.metrics.BytesSent)),
		"",
		inactiveStyle.Render("Bytes Received:"),
		activeStyle.Render(formatBytes(m.metrics.BytesReceived)),
	)
}

// formatBytes formats bytes into human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// getLogLevelStyle returns style for log level.
func getLogLevelStyle(level string) lipgloss.Style {
	switch level {
	case "ERROR":
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "196"}).Bold(true)
	case "WARN":
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "208"})
	case "INFO":
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "26", Dark: "75"})
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "240"})
	}
}
