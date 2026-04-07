// Copyright 2026 ICAP Mock

package components

import (
	"github.com/charmbracelet/lipgloss"
)

// TabsModel represents a tab navigation component.
type TabsModel struct {
	tabs      []Tab
	activeTab int
	width     int
	height    int
}

// Tab represents a single tab.
type Tab struct {
	ID       string
	Title    string
	Shortcut string
	Disabled bool
}

// NewTabsModel creates a new tabs model.
func NewTabsModel() *TabsModel {
	return &TabsModel{
		tabs:      make([]Tab, 0),
		activeTab: 0,
	}
}

// SetTabs sets the available tabs.
func (m *TabsModel) SetTabs(tabs []Tab) {
	m.tabs = tabs
}

// SetActiveTab sets the active tab by index.
func (m *TabsModel) SetActiveTab(index int) {
	if index >= 0 && index < len(m.tabs) {
		m.activeTab = index
	}
}

// SetActiveTabByID sets the active tab by ID.
func (m *TabsModel) SetActiveTabByID(id string) bool {
	for i, tab := range m.tabs {
		if tab.ID == id {
			m.activeTab = i
			return true
		}
	}
	return false
}

// GetActiveTab returns the active tab.
func (m *TabsModel) GetActiveTab() Tab {
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab]
	}
	return Tab{}
}

// GetActiveTabID returns the active tab ID.
func (m *TabsModel) GetActiveTabID() string {
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].ID
	}
	return ""
}

// SetSize sets the tabs dimensions.
func (m *TabsModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// NextTab moves to the next tab.
func (m *TabsModel) NextTab() {
	if len(m.tabs) == 0 {
		return
	}
	m.activeTab = (m.activeTab + 1) % len(m.tabs)
}

// PrevTab moves to the previous tab.
func (m *TabsModel) PrevTab() {
	if len(m.tabs) == 0 {
		return
	}
	m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
}

// View renders the tabs.
func (m *TabsModel) View() string {
	if len(m.tabs) == 0 {
		return ""
	}

	var renderedTabs []string

	for i, tab := range m.tabs {
		style := inactiveTabStyle

		if i == m.activeTab {
			style = activeTabStyle
		} else if tab.Disabled {
			style = disabledTabStyle
		}

		tabText := tab.Title
		if tab.Shortcut != "" {
			tabText = tab.Shortcut + " " + tabText
		}

		renderedTabs = append(renderedTabs, style.Render(tabText))
	}

	// Calculate total width needed
	totalWidth := 0
	for _, tab := range renderedTabs {
		totalWidth += lipgloss.Width(tab)
	}

	// Add spacing between tabs
	if len(renderedTabs) > 1 {
		totalWidth += (len(renderedTabs) - 1) * 2
	}

	// Center tabs if they're shorter than the available width
	var result string
	if totalWidth < m.width {
		result = lipgloss.NewStyle().
			Width(m.width).
			Align(lipgloss.Center).
			Render(lipgloss.JoinHorizontal(lipgloss.Left, renderedTabs...))
	} else {
		result = lipgloss.JoinHorizontal(lipgloss.Left, renderedTabs...)
	}

	return result
}

// Styles for tabs.
var (
	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57")).
			Bold(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Padding(0, 1)

	disabledTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 1)
)
