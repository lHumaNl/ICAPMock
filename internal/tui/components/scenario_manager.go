// Copyright 2026 ICAP Mock

package components

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// ScenarioItem represents a scenario item in the list.
type ScenarioItem struct {
	Name     string
	Method   string
	Path     string
	Priority int
}

// FilterValue implements list.Item.
func (s ScenarioItem) FilterValue() string {
	return s.Name
}

// Title implements list.Item.
func (s ScenarioItem) Title() string {
	return fmt.Sprintf("%s (priority: %d)", s.Name, s.Priority)
}

// Description implements list.Item.
func (s ScenarioItem) Description() string {
	var parts []string
	if s.Method != "" {
		parts = append(parts, s.Method)
	}
	if s.Path != "" {
		parts = append(parts, s.Path)
	}
	return strings.Join(parts, " ")
}

// ScenarioViewMode represents the current view mode.
type ScenarioViewMode int

const (
	ScenarioViewList ScenarioViewMode = iota
	ScenarioViewDetail
	ScenarioViewEdit
	ScenarioViewCreate
)

// ScenarioManagerModel represents the scenario manager component.
type ScenarioManagerModel struct {
	priorityInput    *textinput.Model
	selectedScenario *ScenarioItem
	yamlEditor       *textarea.Model
	bodyPatternInput *textinput.Model
	pathInput        *textinput.Model
	scenarioList     *list.Model
	nameInput        *textinput.Model
	methodInput      *textinput.Model
	scenarioDetail   string
	errorMessage     string
	mode             ScenarioViewMode
	height           int
	width            int
	mu               sync.RWMutex
	ready            bool
}

// NewScenarioManagerModel creates a new scenario manager model.
func NewScenarioManagerModel() *ScenarioManagerModel {
	// Initialize list
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
	l.SetShowHelp(true)

	// Initialize text inputs
	nameInput := textinput.New()
	nameInput.Placeholder = "Scenario name"
	nameInput.CharLimit = 50

	priorityInput := textinput.New()
	priorityInput.Placeholder = "Priority (number)"
	priorityInput.CharLimit = 5

	methodInput := textinput.New()
	methodInput.Placeholder = "ICAP method (REQMOD, RESPMOD, OPTIONS)"

	pathInput := textinput.New()
	pathInput.Placeholder = "Path pattern (regex)"

	bodyPatternInput := textinput.New()
	bodyPatternInput.Placeholder = "Body pattern (regex)"

	// Initialize YAML editor
	yamlEditor := textarea.New()
	yamlEditor.Placeholder = "Enter YAML configuration..."
	yamlEditor.ShowLineNumbers = true
	yamlEditor.SetHeight(15)

	return &ScenarioManagerModel{
		mode:             ScenarioViewList,
		scenarioList:     &l,
		nameInput:        &nameInput,
		priorityInput:    &priorityInput,
		methodInput:      &methodInput,
		pathInput:        &pathInput,
		bodyPatternInput: &bodyPatternInput,
		yamlEditor:       &yamlEditor,
	}
}

// Init initializes the scenario manager.
func (m *ScenarioManagerModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the scenario manager.
func (m *ScenarioManagerModel) Update(msg tea.Msg) (*ScenarioManagerModel, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Update list size
		m.scenarioList.SetWidth(m.width - 4)
		m.scenarioList.SetHeight(m.height - 10)

	case tea.KeyMsg:
		switch msg.String() {
		case "n":
			if m.mode == ScenarioViewList {
				return m.switchToCreateMode()
			}
		case "e":
			if m.mode == ScenarioViewList && m.selectedScenario != nil {
				return m.switchToEditMode()
			}
		case "d":
			if m.mode == ScenarioViewList && m.selectedScenario != nil {
				// Delete selected scenario
				return m, func() tea.Msg {
					return ScenarioDeleteMsg{ScenarioName: m.selectedScenario.Name}
				}
			}
		case "r":
			if m.mode == ScenarioViewList {
				// Reload scenarios
				return m, func() tea.Msg {
					return ScenarioReloadMsg{}
				}
			}
		case keyEsc:
			if m.mode == ScenarioViewDetail || m.mode == ScenarioViewEdit || m.mode == ScenarioViewCreate {
				return m.switchToListMode()
			}
		case keyEnter:
			if m.mode == ScenarioViewList {
				selected := m.scenarioList.SelectedItem()
				if selected != nil {
					item, ok := selected.(ScenarioItem)
					if ok {
						m.selectedScenario = &item
						return m.switchToDetailMode()
					}
				}
			}
		}
	}

	// Update based on mode
	switch m.mode {
	case ScenarioViewList:
		listCmd := m.updateList(msg)
		cmd = tea.Batch(cmd, listCmd)
	case ScenarioViewDetail:
		// Detail view is read-only, just handle esc
	case ScenarioViewEdit:
		editCmd := m.updateEdit(msg)
		cmd = tea.Batch(cmd, editCmd)
	case ScenarioViewCreate:
		createCmd := m.updateCreate(msg)
		cmd = tea.Batch(cmd, createCmd)
	}

	return m, cmd
}

// updateList updates the list view.
func (m *ScenarioManagerModel) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	*m.scenarioList, cmd = m.scenarioList.Update(msg)

	// Track selected scenario with type assertion safety
	selected := m.scenarioList.SelectedItem()
	if selected != nil {
		item, ok := selected.(ScenarioItem)
		if ok {
			m.selectedScenario = &item
		}
	}

	return cmd
}

// updateEdit updates the edit view.
func (m *ScenarioManagerModel) updateEdit(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case keyCtrlS:
			// Save scenario
			return m.saveScenario()
		case "tab":
			// Cycle through inputs
			cycleCmd := m.cycleInputs()
			cmd = tea.Batch(cmd, cycleCmd)
		}
	}

	// Update text inputs
	inputCmd := m.updateInputs(msg)
	cmd = tea.Batch(cmd, inputCmd)

	return cmd
}

// updateCreate updates the create view.
func (m *ScenarioManagerModel) updateCreate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case keyCtrlS:
			// Create scenario
			return m.createScenario()
		case "ctrl+c":
			// Cancel and return to list
			model, switchCmd := m.switchToListMode()
			_ = model
			cmd = tea.Batch(cmd, switchCmd)
		case "tab":
			// Cycle through inputs
			cycleCmd := m.cycleInputs()
			cmd = tea.Batch(cmd, cycleCmd)
		}
	}

	// Update text inputs
	inputCmd := m.updateInputs(msg)
	cmd = tea.Batch(cmd, inputCmd)

	return cmd
}

// updateInputs updates text inputs.
func (m *ScenarioManagerModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd //nolint:prealloc
	var cmd tea.Cmd

	// Update all input fields
	*m.nameInput, cmd = m.nameInput.Update(msg)
	cmds = append(cmds, cmd)

	*m.priorityInput, cmd = m.priorityInput.Update(msg)
	cmds = append(cmds, cmd)

	*m.methodInput, cmd = m.methodInput.Update(msg)
	cmds = append(cmds, cmd)

	*m.pathInput, cmd = m.pathInput.Update(msg)
	cmds = append(cmds, cmd)

	*m.bodyPatternInput, cmd = m.bodyPatternInput.Update(msg)
	cmds = append(cmds, cmd)

	*m.yamlEditor, cmd = m.yamlEditor.Update(msg)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// SetScenarios sets the list of scenarios.
func (m *ScenarioManagerModel) SetScenarios(scenarios []ScenarioItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]list.Item, len(scenarios))
	for i, s := range scenarios {
		items[i] = s
	}
	m.scenarioList.SetItems(items)
}

// switchToListMode switches to list view.
func (m *ScenarioManagerModel) switchToListMode() (*ScenarioManagerModel, tea.Cmd) {
	m.mode = ScenarioViewList
	m.selectedScenario = nil
	m.errorMessage = ""
	return m, nil
}

// switchToDetailMode switches to detail view.
func (m *ScenarioManagerModel) switchToDetailMode() (*ScenarioManagerModel, tea.Cmd) {
	if m.selectedScenario == nil {
		return m, nil
	}

	m.mode = ScenarioViewDetail
	m.scenarioDetail = m.formatScenarioDetail(m.selectedScenario)

	// Populate edit fields
	m.nameInput.SetValue(m.selectedScenario.Name)
	m.priorityInput.SetValue(fmt.Sprintf("%d", m.selectedScenario.Priority))
	m.methodInput.SetValue(m.selectedScenario.Method)
	m.pathInput.SetValue(m.selectedScenario.Path)

	return m, nil
}

// switchToEditMode switches to edit mode.
func (m *ScenarioManagerModel) switchToEditMode() (*ScenarioManagerModel, tea.Cmd) {
	if m.selectedScenario == nil {
		m.errorMessage = "No scenario selected for editing"
		return m, nil
	}

	m.mode = ScenarioViewEdit
	m.nameInput.Focus()
	m.errorMessage = ""

	return m, nil
}

// switchToCreateMode switches to create mode.
func (m *ScenarioManagerModel) switchToCreateMode() (*ScenarioManagerModel, tea.Cmd) {
	m.mode = ScenarioViewCreate
	m.nameInput.SetValue("")
	m.nameInput.Placeholder = "Scenario name"
	m.nameInput.Focus()
	m.errorMessage = ""

	// Generate default YAML template
	yamlTemplate := `name: 
priority: 50
match:
  path_pattern: ""
  icap_method: ""
  http_method: ""
  body_pattern: ""
response:
  icap_status: 204
  http_status: 200
  headers: {}
  body: ""
`
	m.yamlEditor.SetValue(yamlTemplate)

	return m, nil
}

// cycleInputs cycles through input fields.
func (m *ScenarioManagerModel) cycleInputs() tea.Cmd {
	if m.nameInput.Focused() {
		m.nameInput.Blur()
		m.priorityInput.Focus()
	} else if m.priorityInput.Focused() {
		m.priorityInput.Blur()
		m.methodInput.Focus()
	} else if m.methodInput.Focused() {
		m.methodInput.Blur()
		m.pathInput.Focus()
	} else if m.pathInput.Focused() {
		m.pathInput.Blur()
		m.bodyPatternInput.Focus()
	} else if m.bodyPatternInput.Focused() {
		m.bodyPatternInput.Blur()
		m.yamlEditor.Focus()
	} else if m.yamlEditor.Focused() {
		m.yamlEditor.Blur()
		m.nameInput.Focus()
	}
	return nil
}

// saveScenario saves the edited scenario.
func (m *ScenarioManagerModel) saveScenario() tea.Cmd {
	if m.selectedScenario == nil {
		m.errorMessage = "No scenario selected"
		return nil
	}

	// Validate name
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		m.errorMessage = "Scenario name is required"
		return nil
	}

	// Validate priority
	priorityStr := strings.TrimSpace(m.priorityInput.Value())
	if priorityStr == "" {
		m.errorMessage = "Priority is required"
		return nil
	}
	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		m.errorMessage = "Priority must be a valid number"
		return nil
	}
	if priority < 0 || priority > 100 {
		m.errorMessage = "Priority must be between 0 and 100"
		return nil
	}

	// Validate YAML syntax
	yamlContent := m.yamlEditor.Value()
	var data interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &data); err != nil {
		m.errorMessage = fmt.Sprintf("Invalid YAML: %v", err)
		return nil
	}

	return func() tea.Msg {
		return ScenarioUpdateMsg{
			ScenarioName: m.selectedScenario.Name,
			Name:         name,
			YAML:         yamlContent,
		}
	}
}

// createScenario creates a new scenario.
func (m *ScenarioManagerModel) createScenario() tea.Cmd {
	// Validate name
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		m.errorMessage = "Scenario name is required"
		return nil
	}

	// Validate priority
	priorityStr := strings.TrimSpace(m.priorityInput.Value())
	if priorityStr == "" {
		m.errorMessage = "Priority is required"
		return nil
	}
	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		m.errorMessage = "Priority must be a valid number"
		return nil
	}
	if priority < 0 || priority > 100 {
		m.errorMessage = "Priority must be between 0 and 100"
		return nil
	}

	// Validate YAML syntax
	yamlContent := m.yamlEditor.Value()
	if strings.TrimSpace(yamlContent) == "" {
		m.errorMessage = "YAML configuration cannot be empty"
		return nil
	}

	var data interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &data); err != nil {
		m.errorMessage = fmt.Sprintf("Invalid YAML: %v", err)
		return nil
	}

	return func() tea.Msg {
		return ScenarioCreateMsg{
			Name: name,
			YAML: yamlContent,
		}
	}
}

// formatScenarioDetail formats scenario details for display.
func (m *ScenarioManagerModel) formatScenarioDetail(scenario *ScenarioItem) string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Scenario Details"),
		"",
		SubtitleStyle.Render(fmt.Sprintf("Name: %s", scenario.Name)),
		SubtitleStyle.Render(fmt.Sprintf("Priority: %d", scenario.Priority)),
		"",
		TitleStyle.Render("Match Rules"),
		SubtitleStyle.Render(fmt.Sprintf("Method: %s", scenario.Method)),
		SubtitleStyle.Render(fmt.Sprintf("Path: %s", scenario.Path)),
	)
}

// View renders the scenario manager.
func (m *ScenarioManagerModel) View() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.ready {
		return "Loading scenario manager..."
	}

	switch m.mode {
	case ScenarioViewList:
		return m.renderListView()
	case ScenarioViewDetail:
		return m.renderDetailView()
	case ScenarioViewEdit:
		return m.renderEditView()
	case ScenarioViewCreate:
		return m.renderCreateView()
	default:
		return "Unknown view mode"
	}
}

// renderListView renders the scenario list.
func (m *ScenarioManagerModel) renderListView() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Scenarios"),
		"",
		m.scenarioList.View(),
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  n - New scenario"),
		SubtitleStyle.Render("  e - Edit scenario"),
		SubtitleStyle.Render("  d - Delete scenario"),
		SubtitleStyle.Render("  r - Reload scenarios"),
		SubtitleStyle.Render("  Enter - View details"),
	)

	return PanelStyle.Render(content)
}

// renderDetailView renders the scenario detail view.
func (m *ScenarioManagerModel) renderDetailView() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.scenarioDetail,
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  esc - Back to list"),
		SubtitleStyle.Render("  e - Edit scenario"),
		SubtitleStyle.Render("  d - Delete scenario"),
	)

	return PanelStyle.Render(content)
}

// renderEditView renders the edit view.
func (m *ScenarioManagerModel) renderEditView() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Edit Scenario"),
		"",
		SubtitleStyle.Render("Name:"),
		m.nameInput.View(),
		"",
		SubtitleStyle.Render("Priority:"),
		m.priorityInput.View(),
		"",
		TitleStyle.Render("YAML Configuration"),
		m.yamlEditor.View(),
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  Ctrl+S - Save"),
		SubtitleStyle.Render("  Esc - Cancel"),
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

// renderCreateView renders the create view.
func (m *ScenarioManagerModel) renderCreateView() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		TitleStyle.Render("Create New Scenario"),
		"",
		SubtitleStyle.Render("Name:"),
		m.nameInput.View(),
		"",
		TitleStyle.Render("YAML Configuration"),
		m.yamlEditor.View(),
		"",
		SubtitleStyle.Render("Key bindings:"),
		SubtitleStyle.Render("  Ctrl+S - Save"),
		SubtitleStyle.Render("  Ctrl+C - Cancel"),
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

// ScenarioListMsg is sent when scenario list is received.
type ScenarioListMsg struct {
	Scenarios []ScenarioItem
}

// ScenarioDeleteMsg is sent to delete a scenario.
type ScenarioDeleteMsg struct {
	ScenarioName string
}

// ScenarioUpdateMsg is sent to update a scenario.
type ScenarioUpdateMsg struct {
	ScenarioName string
	Name         string
	YAML         string
}

// ScenarioCreateMsg is sent to create a scenario.
type ScenarioCreateMsg struct {
	Name string
	YAML string
}

// ScenarioReloadMsg is sent to reload scenarios.
type ScenarioReloadMsg struct{}

// ScenarioErrorMsg is sent when a scenario operation fails.
type ScenarioErrorMsg struct {
	Err error
}
