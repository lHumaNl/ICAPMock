// Package components provides reusable UI components for the TUI.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterModel represents the footer component.
type FooterModel struct {
	keyBindings []KeyBinding
	width       int
	height      int
	status      string
	errorMsg    string
}

// KeyBinding represents a keyboard shortcut and its description.
type KeyBinding struct {
	Key         string
	Description string
}

// NewFooterModel creates a new footer model.
func NewFooterModel() *FooterModel {
	return &FooterModel{
		keyBindings: make([]KeyBinding, 0),
	}
}

// SetSize sets the footer dimensions.
func (m *FooterModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetKeyBindings sets the key bindings to display.
func (m *FooterModel) SetKeyBindings(bindings []KeyBinding) {
	m.keyBindings = bindings
}

// SetStatus sets the status message.
func (m *FooterModel) SetStatus(status string) {
	m.status = status
}

// SetError sets an error message.
func (m *FooterModel) SetError(err string) {
	m.errorMsg = err
}

// View renders the footer.
func (m *FooterModel) View() string {
	if m.width == 0 {
		return footerStyle.Render("q:quit Tab:navigate")
	}

	// Render key bindings
	keys := m.renderKeyBindings()

	// Render status message if present
	var status string
	if m.status != "" {
		status = m.renderStatus()
	} else if m.errorMsg != "" {
		status = m.renderError()
	}

	// Combine sections
	var content string
	if status != "" {
		// Left side: key bindings, right side: status
		keysWidth := lipgloss.Width(keys)
		statusWidth := lipgloss.Width(status)

		if keysWidth+statusWidth < m.width {
			spacing := m.width - keysWidth - statusWidth
			content = keys + strings.Repeat(" ", spacing) + status
		} else {
			// Prioritize status message
			content = status
		}
	} else {
		// Center the key bindings
		content = lipgloss.NewStyle().
			Width(m.width).
			Align(lipgloss.Center).
			Render(keys)
	}

	// Wrap in footer style
	return footerStyle.Render(content)
}

// renderKeyBindings renders the key bindings.
func (m *FooterModel) renderKeyBindings() string {
	if len(m.keyBindings) == 0 {
		return ""
	}

	var rendered []string

	for _, binding := range m.keyBindings {
		keyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true)

		key := keyStyle.Render(binding.Key)
		desc := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(binding.Description)

		rendered = append(rendered, key+" "+desc)
	}

	return strings.Join(rendered, " | ")
}

// renderStatus renders the status message.
func (m *FooterModel) renderStatus() string {
	if m.status == "" {
		return ""
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Render("✓ " + m.status)
}

// renderError renders the error message.
func (m *FooterModel) renderError() string {
	if m.errorMsg == "" {
		return ""
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Render("✗ " + m.errorMsg)
}

// ClearStatus clears any status or error messages.
func (m *FooterModel) ClearStatus() {
	m.status = ""
	m.errorMsg = ""
}

// GetHeight returns the height of the footer (always 1 line).
func (m *FooterModel) GetHeight() int {
	return 1
}

// Footer styles
var (
	footerStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		Foreground(lipgloss.Color("244")).
		Padding(0, 1)
)

// DefaultKeyBindings returns default key bindings for the application.
func DefaultKeyBindings() []KeyBinding {
	return []KeyBinding{
		{"1-6", "Screens"},
		{"Tab", "Next"},
		{"Shift+Tab", "Prev"},
		{"Esc", "Back"},
		{"q", "Quit"},
	}
}
