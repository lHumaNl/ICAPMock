// Copyright 2026 ICAP Mock

package components

import (
	"sync"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SidebarModel represents a sidebar menu component.
type SidebarModel struct {
	list          list.Model
	width         int
	height        int
	mu            sync.RWMutex
}

// NewSidebarModel creates a new sidebar menu.
func NewSidebarModel() *SidebarModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("57")).
		Bold(true)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("57"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Navigation"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)

	return &SidebarModel{
		list: l,
	}
}

// Init initializes the sidebar.
func (m *SidebarModel) Init() tea.Cmd {
	return nil
}

// Update updates the sidebar model.
func (m *SidebarModel) Update(msg tea.Msg) (*SidebarModel, tea.Cmd) {
	var cmd tea.Cmd

	m.mu.Lock()
	m.list, cmd = m.list.Update(msg)
	m.mu.Unlock()
	return m, cmd
}

// SetItems sets the menu items.
func (m *SidebarModel) SetItems(items []list.Item) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list.SetItems(items)
}

// SetSize sets the sidebar dimensions with validation.
func (m *SidebarModel) SetSize(width, height int) {
	// Validate inputs
	if width < 10 {
		width = 10
	}
	if height < 10 {
		height = 10
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.width = width
	m.height = height
	m.list.SetWidth(width)
	m.list.SetHeight(height - 4) // Reserve space for header and help text
}

// SetSelected sets the selected item with bounds checking.
func (m *SidebarModel) SetSelected(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get the current number of items
	items := m.list.Items()

	// Validate index bounds
	if index < 0 {
		return
	}
	if index >= len(items) {
		return
	}

	m.list.Select(index)
}

// GetSelected returns the currently selected item.
func (m *SidebarModel) GetSelected() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.list.SelectedItem() == nil {
		return ""
	}
	return m.list.SelectedItem().FilterValue()
}

// View renders the sidebar.
func (m *SidebarModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Render the list
	content := m.list.View()

	// Render help text at the bottom
	helpText := m.renderHelpText()

	// Combine list and help text
	return lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		helpText,
	)
}

// renderHelpText renders keyboard shortcut help.
func (m *SidebarModel) renderHelpText() string {
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(m.width).
		Align(lipgloss.Center)

	helpLines := []string{
		"",
		"↑/k: Up  ↓/j: Down",
		"1-9: Quick select",
		"Enter: Select",
	}

	var rendered string
	for _, line := range helpLines {
		rendered += help.Render(line) + "\n"
	}

	return rendered
}

// ScreenItem represents a menu item for a screen.
type ScreenItem struct {
	title       string
	description string
	screen      string
	shortcut    string
}

// NewScreenItem creates a new screen item.
func NewScreenItem(title, description, screen, shortcut string) ScreenItem {
	return ScreenItem{
		title:       title,
		description: description,
		screen:      screen,
		shortcut:    shortcut,
	}
}

// FilterValue implements list.Item.
func (i ScreenItem) FilterValue() string {
	return i.screen
}

// Title implements list.Item.
func (i ScreenItem) Title() string {
	title := i.title
	if i.shortcut != "" {
		title = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Render("["+i.shortcut+"]") + " " + title
	}
	return title
}

// Description implements list.Item.
func (i ScreenItem) Description() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(i.description)
}
