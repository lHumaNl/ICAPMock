// Copyright 2026 ICAP Mock

package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	keyEnter = "enter"
	keyEsc   = "esc"
	keyCtrlS = "ctrl+s"
)

// MenuItem represents an item in a dropdown menu.
type MenuItem struct {
	id        string
	title     string
	shortcut  string
	disabled  bool
	separator bool
}

// NewMenuItem creates a new menu item.
func NewMenuItem(id, title, shortcut string) MenuItem {
	return MenuItem{
		id:       id,
		title:    title,
		shortcut: shortcut,
	}
}

// NewMenuSeparator creates a separator menu item.
func NewMenuSeparator() MenuItem {
	return MenuItem{
		separator: true,
	}
}

// Disabled marks the item as disabled.
func (m MenuItem) Disabled() MenuItem {
	m.disabled = true
	return m
}

// FilterValue implements list.Item.
func (m MenuItem) FilterValue() string {
	return m.id
}

// Title implements list.Item.
func (m MenuItem) Title() string {
	if m.separator {
		return strings.Repeat("─", 20)
	}

	title := m.title
	if m.shortcut != "" {
		title = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("["+m.shortcut+"]") + " " + title
	}

	if m.disabled {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render(title)
	}

	return title
}

// Description implements list.Item.
func (m MenuItem) Description() string {
	return ""
}

// MenuModel represents a dropdown menu component.
type MenuModel struct {
	list     list.Model
	onSelect func(id string)
	width    int
	height   int
	visible  bool
}

// NewMenuModel creates a new menu model.
func NewMenuModel() *MenuModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("57")).
		Bold(true)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("57"))
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("254"))
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)

	return &MenuModel{
		list:    l,
		visible: false,
	}
}

// Init initializes the menu.
func (m *MenuModel) Init() tea.Cmd {
	return nil
}

// Update updates the menu model.
func (m *MenuModel) Update(msg tea.Msg) (*MenuModel, tea.Cmd) {
	var cmd tea.Cmd

	m.list, cmd = m.list.Update(msg)

	// Check for selection
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case keyEnter:
			if m.visible && m.onSelect != nil {
				selected := m.list.SelectedItem()
				if selected != nil {
					if item, ok := selected.(MenuItem); ok && !item.disabled && !item.separator {
						m.visible = false
						return m, func() tea.Msg {
							return MenuItemSelectedMsg{
								ID: item.id,
							}
						}
					}
				}
			}
		case keyEsc:
			if m.visible {
				m.visible = false
				return m, nil
			}
		}
	}

	return m, cmd
}

// SetItems sets the menu items.
func (m *MenuModel) SetItems(items []MenuItem) {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}
	m.list.SetItems(listItems)
}

// SetSize sets the menu dimensions.
func (m *MenuModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

// Show shows the menu.
func (m *MenuModel) Show() {
	m.visible = true
}

// Hide hides the menu.
func (m *MenuModel) Hide() {
	m.visible = false
}

// IsVisible returns whether the menu is visible.
func (m *MenuModel) IsVisible() bool {
	return m.visible
}

// SetOnSelect sets the callback for item selection.
func (m *MenuModel) SetOnSelect(fn func(id string)) {
	m.onSelect = fn
}

// View renders the menu.
func (m *MenuModel) View() string {
	if !m.visible {
		return ""
	}

	// Render menu with border
	return menuBorderStyle.Render(m.list.View())
}

// GetSelected returns the currently selected item.
func (m *MenuModel) GetSelected() *MenuItem {
	if m.list.SelectedItem() == nil {
		return nil
	}
	item := m.list.SelectedItem().(MenuItem) //nolint:errcheck
	return &item
}

// Next moves to the next menu item.
func (m *MenuModel) Next() {
	m.list.CursorDown()
}

// Prev moves to the previous menu item.
func (m *MenuModel) Prev() {
	m.list.CursorUp()
}

// MenuItemSelectedMsg is sent when a menu item is selected.
type MenuItemSelectedMsg struct {
	ID string
}

// Menu styles.
var (
	menuBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("57")).
		Padding(0, 1)
)

// MenuBuilder provides a fluent API for building menus.
type MenuBuilder struct {
	items []MenuItem
}

// NewMenuBuilder creates a new menu builder.
func NewMenuBuilder() *MenuBuilder {
	return &MenuBuilder{
		items: make([]MenuItem, 0),
	}
}

// AddItem adds a menu item.
func (b *MenuBuilder) AddItem(id, title, shortcut string) *MenuBuilder {
	b.items = append(b.items, NewMenuItem(id, title, shortcut))
	return b
}

// AddDisabledItem adds a disabled menu item.
func (b *MenuBuilder) AddDisabledItem(id, title, shortcut string) *MenuBuilder {
	b.items = append(b.items, NewMenuItem(id, title, shortcut).Disabled())
	return b
}

// AddSeparator adds a separator.
func (b *MenuBuilder) AddSeparator() *MenuBuilder {
	b.items = append(b.items, NewMenuSeparator())
	return b
}

// Build builds the menu model.
func (b *MenuBuilder) Build() *MenuModel {
	menu := NewMenuModel()
	menu.SetItems(b.items)
	return menu
}
