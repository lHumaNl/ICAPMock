// Copyright 2026 ICAP Mock

package components

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// ConfigEditorModel represents configuration editor component.
type ConfigEditorModel struct {
	textarea   textarea.Model
	content    string
	filePath   string
	fileType   string
	validation ValidationStatus
	width      int
	height     int
	maxSize    int
	mu         sync.RWMutex
	showHelp   bool
	loading    bool
	modified   bool
}

// ValidationStatus represents the validation state of the config.
type ValidationStatus struct {
	Error   string
	Message string
	Valid   bool
}

// NewConfigEditorModel creates a new config editor model.
func NewConfigEditorModel() *ConfigEditorModel {
	ta := textarea.New()
	ta.ShowLineNumbers = true
	ta.Placeholder = "Enter YAML or JSON configuration here..."
	ta.Focus()

	return &ConfigEditorModel{
		textarea:   ta,
		content:    "",
		filePath:   "",
		fileType:   "yaml",
		validation: ValidationStatus{Valid: false, Message: "No content to validate"},
		showHelp:   true,
		loading:    false,
		modified:   false,
		maxSize:    1024 * 1024, // 1MB default max size
	}
}

// Init initializes the config editor model.
func (m *ConfigEditorModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the config editor model.
func (m *ConfigEditorModel) Update(msg tea.Msg) (*ConfigEditorModel, tea.Cmd) {
	var cmd tea.Cmd
	cmds := make([]tea.Cmd, 0, 1)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle special keys
		switch msg.String() {
		case keyCtrlS:
			// Save is handled by parent
			return m, nil
		case "ctrl+l":
			// Toggle help
			m.showHelp = !m.showHelp
			return m, nil
		case "ctrl+v":
			// Validate current content
			m.validateContent()
			return m, nil
		case "ctrl+r":
			// Reload from file
			return m, m.Reload()
		case "ctrl+f":
			// Format content
			m.formatContent()
			return m, nil
		case keyEsc:
			// Return to previous screen - handled by parent
			return m, nil
		}
	}

	// Update textarea and check for modifications
	m.mu.Lock()
	prevContent := m.textarea.Value()
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	// Check if content was modified
	newContent := m.textarea.Value()
	if prevContent != newContent {
		m.content = newContent
		m.modified = true
		// Auto-validate on significant changes
		m.validateContent()
	}
	m.mu.Unlock()

	return m, tea.Batch(cmds...)
}

// SetContent sets the editor content and file path.
func (m *ConfigEditorModel) SetContent(content, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate file path is not empty
	if filePath == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	// Validate content size
	if len(content) > m.maxSize {
		return fmt.Errorf("content size (%d bytes) exceeds maximum allowed size (%d bytes)", len(content), m.maxSize)
	}

	// Validate content is not empty
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content cannot be empty")
	}

	m.content = content
	m.textarea.SetValue(content)
	m.filePath = filePath

	// Detect file type
	if strings.HasSuffix(strings.ToLower(filePath), ".json") {
		m.fileType = "json" //nolint:goconst
	} else {
		m.fileType = "yaml" //nolint:goconst
	}

	m.validateContent()
	m.modified = false

	// Check if validation failed
	if !m.validation.Valid {
		return fmt.Errorf("invalid configuration: %s", m.validation.Error)
	}

	return nil
}

// GetContent returns the current editor content.
func (m *ConfigEditorModel) GetContent() string {
	return m.textarea.Value()
}

// GetFilePath returns the current file path.
func (m *ConfigEditorModel) GetFilePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filePath
}

// IsModified returns whether the content has been modified.
func (m *ConfigEditorModel) IsModified() bool {
	return m.modified
}

// Reload reloads the content from the file.
func (m *ConfigEditorModel) Reload() tea.Cmd {
	return func() tea.Msg {
		// This should be implemented by the parent component
		// For now, just return nil
		return nil
	}
}

// formatContent formats the content according to file type.
func (m *ConfigEditorModel) formatContent() {
	content := m.textarea.Value()
	if content == "" {
		return
	}

	var formatted interface{}

	if m.fileType == "json" {
		// Format JSON
		if err := json.Unmarshal([]byte(content), &formatted); err == nil {
			if data, err := json.MarshalIndent(formatted, "", "  "); err == nil {
				m.textarea.SetValue(string(data))
				m.modified = true
				m.validateContent()
			}
		}
	} else {
		// Format YAML
		if err := yaml.Unmarshal([]byte(content), &formatted); err == nil {
			if data, err := yaml.Marshal(formatted); err == nil {
				m.textarea.SetValue(string(data))
				m.modified = true
				m.validateContent()
			}
		}
	}
}

// validateContent validates the current content.
func (m *ConfigEditorModel) validateContent() {
	content := m.textarea.Value()

	if strings.TrimSpace(content) == "" {
		m.validation = ValidationStatus{
			Valid:   false,
			Message: "No content to validate",
		}
		return
	}

	var data interface{}
	var err error

	if m.fileType == "json" {
		err = json.Unmarshal([]byte(content), &data)
	} else {
		err = yaml.Unmarshal([]byte(content), &data)
	}

	if err != nil {
		m.validation = ValidationStatus{
			Valid:   false,
			Error:   err.Error(),
			Message: "Invalid configuration",
		}
		return
	}

	// Additional validation can be added here
	// For now, just check if it's valid YAML/JSON
	m.validation = ValidationStatus{
		Valid:   true,
		Message: "Valid configuration",
	}
}

// SetLoading sets the loading state.
func (m *ConfigEditorModel) SetLoading(loading bool) {
	m.loading = loading
}

// SetWindowSize sets the window size.
func (m *ConfigEditorModel) SetWindowSize(width, height int) {
	m.width = width
	m.height = height

	// Calculate editor height
	editorHeight := height - 10 // Reserve space for header, validation, footer
	if editorHeight < 5 {
		editorHeight = 5
	}

	m.textarea.SetWidth(width - 4)
	m.textarea.SetHeight(editorHeight)
}

// View renders the config editor component.
func (m *ConfigEditorModel) View() string {
	// Render header
	header := m.renderHeader()

	// Render validation status
	validation := m.renderValidation()

	// Render editor
	editor := m.renderEditor()

	// Render help if enabled
	var help string
	if m.showHelp {
		help = m.renderHelp()
	}

	// Combine all sections
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		validation,
		"",
		editor,
	)

	if m.showHelp {
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", help)
	}

	return PanelStyle.Render(content)
}

// renderHeader renders the editor header.
func (m *ConfigEditorModel) renderHeader() string {
	path := m.filePath
	if path == "" {
		path = "New file"
	}

	modified := ""
	if m.modified {
		modified = " *"
	}

	fileType := strings.ToUpper(m.fileType)

	headerLine := lipgloss.JoinHorizontal(
		lipgloss.Left,
		TitleStyle.Render("Configuration Editor"),
		SubtitleStyle.Render(" - "+path+modified+" ("+fileType+")"),
	)

	return headerLine
}

// renderValidation renders the validation status.
func (m *ConfigEditorModel) renderValidation() string {
	if m.validation.Valid {
		return StatusRunningStyle.Render("✓ " + m.validation.Message)
	}

	if m.validation.Error != "" {
		errorMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Width(m.width - 6).
			Render("✗ " + m.validation.Error)

		return errorMsg
	}

	return SubtitleStyle.Render("○ " + m.validation.Message)
}

// renderEditor renders the text editor.
func (m *ConfigEditorModel) renderEditor() string {
	if m.loading {
		return SubtitleStyle.Render("Loading configuration...")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.textarea.View()
}

// renderHelp renders the keyboard shortcuts help.
func (m *ConfigEditorModel) renderHelp() string {
	shortcuts := []struct {
		key   string
		desc  string
		color string
	}{
		{"Ctrl+S", "Save", "46"},
		{"Ctrl+V", "Validate", "75"},
		{"Ctrl+R", "Reload", "208"},
		{"Ctrl+F", "Format", "229"},
		{"Ctrl+L", "Toggle Help", "240"},
		{"Esc", "Back", "196"},
	}

	rendered := make([]string, 0, len(shortcuts))
	for _, s := range shortcuts {
		keyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.color)).
			Bold(true).
			Padding(0, 1)

		descStyle := SubtitleStyle

		line := lipgloss.JoinHorizontal(
			lipgloss.Left,
			keyStyle.Render(s.key),
			descStyle.Render(s.desc),
		)

		rendered = append(rendered, line)
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, rendered...)
}

// Reset resets the editor to initial state.
func (m *ConfigEditorModel) Reset() {
	m.content = ""
	m.textarea.SetValue("")
	m.filePath = ""
	m.fileType = "yaml"
	m.validation = ValidationStatus{Valid: false, Message: "No content to validate"}
	m.modified = false
	m.loading = false
}

// ClearValidation clears the validation status.
func (m *ConfigEditorModel) ClearValidation() {
	m.validation = ValidationStatus{Valid: false, Message: "No content to validate"}
}
