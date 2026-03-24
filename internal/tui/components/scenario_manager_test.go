package components

import (
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewScenarioManagerModel(t *testing.T) {
	model := NewScenarioManagerModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.scenarioList)
	assert.NotNil(t, model.nameInput)
	assert.NotNil(t, model.priorityInput)
	assert.NotNil(t, model.methodInput)
	assert.NotNil(t, model.pathInput)
	assert.NotNil(t, model.yamlEditor)
	assert.False(t, model.ready)
	assert.Equal(t, ScenarioViewList, model.mode)
}

func TestScenarioManagerModel_Init(t *testing.T) {
	model := NewScenarioManagerModel()

	cmd := model.Init()
	assert.Nil(t, cmd)
}

func TestScenarioManagerModel_SetScenarios(t *testing.T) {
	model := NewScenarioManagerModel()

	scenarios := []ScenarioItem{
		{Name: "scenario1", Priority: 10, Method: "REQMOD", Path: "/api/v1/users"},
		{Name: "scenario2", Priority: 20, Method: "RESPMOD", Path: "/api/v1/products"},
	}

	model.SetScenarios(scenarios)

	items := model.scenarioList.Items()
	assert.Len(t, items, 2)
}

func TestScenarioManagerModel_Update_CreateMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	assert.Equal(t, ScenarioViewCreate, newModel.mode)
	assert.Empty(t, newModel.nameInput.Value())
}

func TestScenarioManagerModel_Update_EditMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList
	model.selectedScenario = &ScenarioItem{Name: "test"}

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})

	assert.Equal(t, ScenarioViewEdit, newModel.mode)
}

func TestScenarioManagerModel_Update_Delete(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList
	model.selectedScenario = &ScenarioItem{Name: "test"}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	assert.NotNil(t, cmd)
}

func TestScenarioManagerModel_Update_Reload(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

	assert.NotNil(t, cmd)
}

func TestScenarioManagerModel_Update_Escape_ListMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, ScenarioViewList, newModel.mode)
}

func TestScenarioManagerModel_Update_Escape_DetailMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewDetail

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, ScenarioViewList, newModel.mode)
	assert.Nil(t, newModel.selectedScenario)
}

func TestScenarioManagerModel_Update_Enter(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, ScenarioViewList, newModel.mode)
}

func TestScenarioManagerModel_switchToListMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewDetail
	model.selectedScenario = &ScenarioItem{Name: "test"}

	newModel, _ := model.switchToListMode()

	assert.Equal(t, ScenarioViewList, newModel.mode)
	assert.Nil(t, newModel.selectedScenario)
	assert.Empty(t, newModel.errorMessage)
}

func TestScenarioManagerModel_switchToDetailMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.selectedScenario = &ScenarioItem{
		Name:     "test",
		Priority: 10,
		Method:   "REQMOD",
		Path:     "/api/v1/test",
	}

	newModel, _ := model.switchToDetailMode()

	assert.Equal(t, ScenarioViewDetail, newModel.mode)
	assert.NotEmpty(t, newModel.scenarioDetail)
}

func TestScenarioManagerModel_switchToDetailMode_Nil(t *testing.T) {
	model := NewScenarioManagerModel()
	model.selectedScenario = nil

	newModel, _ := model.switchToDetailMode()

	assert.Equal(t, ScenarioViewList, newModel.mode)
}

func TestScenarioManagerModel_switchToEditMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.selectedScenario = &ScenarioItem{Name: "test"}

	newModel, _ := model.switchToEditMode()

	assert.Equal(t, ScenarioViewEdit, newModel.mode)
	assert.Empty(t, newModel.errorMessage)
}

func TestScenarioManagerModel_switchToCreateMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList

	newModel, _ := model.switchToCreateMode()

	assert.Equal(t, ScenarioViewCreate, newModel.mode)
	assert.Empty(t, newModel.nameInput.Value())
	assert.Empty(t, newModel.errorMessage)
	assert.NotEmpty(t, newModel.yamlEditor.Value())
}

func TestScenarioManagerModel_createScenario_EmptyName(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "required")
}

func TestScenarioManagerModel_createScenario(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.createScenario()

	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, ScenarioCreateMsg{}, msg)

	createMsg := msg.(ScenarioCreateMsg)
	assert.Equal(t, "test scenario", createMsg.Name)
}

func TestScenarioManagerModel_saveScenario(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.NotNil(t, cmd)

	msg := cmd()
	assert.IsType(t, ScenarioUpdateMsg{}, msg)

	updateMsg := msg.(ScenarioUpdateMsg)
	assert.Equal(t, "old name", updateMsg.ScenarioName)
	assert.Equal(t, "new name", updateMsg.Name)
}

func TestScenarioManagerModel_saveScenario_Nil(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = nil

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
}

func TestScenarioItem_FilterValue(t *testing.T) {
	item := ScenarioItem{
		Name:     "test scenario",
		Priority: 10,
		Method:   "REQMOD",
		Path:     "/api/v1/test",
	}

	filterValue := item.FilterValue()
	assert.Equal(t, "test scenario", filterValue)
}

func TestScenarioItem_Title(t *testing.T) {
	item := ScenarioItem{
		Name:     "test scenario",
		Priority: 10,
		Method:   "REQMOD",
		Path:     "/api/v1/test",
	}

	title := item.Title()
	assert.Equal(t, "test scenario (priority: 10)", title)
}

func TestScenarioItem_Description(t *testing.T) {
	tests := []struct {
		name     string
		item     ScenarioItem
		expected string
	}{
		{
			name: "with method and path",
			item: ScenarioItem{
				Name:     "test",
				Priority: 10,
				Method:   "REQMOD",
				Path:     "/api/v1/test",
			},
			expected: "REQMOD /api/v1/test",
		},
		{
			name: "with only method",
			item: ScenarioItem{
				Name:     "test",
				Priority: 10,
				Method:   "REQMOD",
				Path:     "",
			},
			expected: "REQMOD",
		},
		{
			name: "with only path",
			item: ScenarioItem{
				Name:     "test",
				Priority: 10,
				Method:   "",
				Path:     "/api/v1/test",
			},
			expected: "/api/v1/test",
		},
		{
			name: "empty",
			item: ScenarioItem{
				Name:     "test",
				Priority: 10,
				Method:   "",
				Path:     "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description := tt.item.Description()
			assert.Equal(t, tt.expected, description)
		})
	}
}

func TestScenarioManagerModel_View_NotReady(t *testing.T) {
	model := NewScenarioManagerModel()

	view := model.View()
	assert.Equal(t, "Loading scenario manager...", view)
}

func TestScenarioManagerModel_View_ListMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true
	model.mode = ScenarioViewList

	view := model.View()
	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading scenario manager...", view)
}

func TestScenarioManagerModel_View_DetailMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true
	model.mode = ScenarioViewDetail
	model.selectedScenario = &ScenarioItem{Name: "test"}
	model.scenarioDetail = "Test details"

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestScenarioManagerModel_View_EditMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true
	model.mode = ScenarioViewEdit
	model.nameInput.SetValue("test name")

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestScenarioManagerModel_View_CreateMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test name")

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestScenarioManagerModel_View_UnknownMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true
	model.mode = ScenarioViewMode(99)

	view := model.View()
	assert.Equal(t, "Unknown view mode", view)
}

func TestScenarioManagerModel_createScenario_InvalidPriority_Negative(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("-5")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be between 0 and 100")
}

func TestScenarioManagerModel_createScenario_InvalidPriority_TooHigh(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("150")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be between 0 and 100")
}

func TestScenarioManagerModel_createScenario_InvalidPriority_NotNumber(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("abc")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be a valid number")
}

func TestScenarioManagerModel_createScenario_EmptyPriority(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority is required")
}

func TestScenarioManagerModel_createScenario_InvalidYAML(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("invalid: yaml: [")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Invalid YAML")
}

func TestScenarioManagerModel_createScenario_EmptyYAML(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("   \n  ")

	cmd := model.createScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "YAML configuration cannot be empty")
}

func TestScenarioManagerModel_saveScenario_InvalidPriority_Negative(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("-5")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be between 0 and 100")
}

func TestScenarioManagerModel_saveScenario_InvalidPriority_TooHigh(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("150")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be between 0 and 100")
}

func TestScenarioManagerModel_saveScenario_InvalidYAML(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("invalid: yaml: [")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Invalid YAML")
}

func TestScenarioManagerModel_saveScenario_EmptyName(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("   ")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Scenario name is required")
}

func TestScenarioManagerModel_updateInputs_TextInput(t *testing.T) {
	model := NewScenarioManagerModel()

	cmd := model.updateInputs(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.Nil(t, cmd)
}

func TestScenarioManagerModel_cycleInputs_AllInputs(t *testing.T) {
	model := NewScenarioManagerModel()
	model.nameInput.Focus()

	cmd := model.cycleInputs()
	assert.Nil(t, cmd)
	assert.False(t, model.nameInput.Focused())
	assert.True(t, model.priorityInput.Focused())

	cmd = model.cycleInputs()
	assert.False(t, model.priorityInput.Focused())
	assert.True(t, model.methodInput.Focused())

	cmd = model.cycleInputs()
	assert.False(t, model.methodInput.Focused())
	assert.True(t, model.pathInput.Focused())

	cmd = model.cycleInputs()
	assert.False(t, model.pathInput.Focused())
	assert.True(t, model.bodyPatternInput.Focused())

	cmd = model.cycleInputs()
	assert.False(t, model.bodyPatternInput.Focused())
	assert.True(t, model.yamlEditor.Focused())

	cmd = model.cycleInputs()
	assert.False(t, model.yamlEditor.Focused())
	assert.True(t, model.nameInput.Focused())
}

func TestScenarioManagerModel_switchToEditMode_NoScenario(t *testing.T) {
	model := NewScenarioManagerModel()
	model.selectedScenario = nil

	newModel, _ := model.switchToEditMode()

	assert.Equal(t, ScenarioViewList, newModel.mode)
	assert.Contains(t, newModel.errorMessage, "No scenario selected")
}

func TestScenarioManagerModel_updateList_Selection(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true

	scenarios := []ScenarioItem{
		{Name: "scenario1", Priority: 10, Method: "REQMOD", Path: "/api/v1/users"},
		{Name: "scenario2", Priority: 20, Method: "RESPMOD", Path: "/api/v1/products"},
	}
	model.SetScenarios(scenarios)

	cmd := model.updateList(tea.KeyMsg{Type: tea.KeyDown})

	assert.Nil(t, cmd)
}

func TestScenarioManagerModel_Update_CreateMode_WithInputs(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.NotNil(t, newModel)
}

func TestScenarioManagerModel_Update_EditMode_WithInputs(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "test"}

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.NotNil(t, newModel)
}

func TestScenarioManagerModel_formatScenarioDetail(t *testing.T) {
	model := NewScenarioManagerModel()

	scenario := &ScenarioItem{
		Name:     "test scenario",
		Priority: 50,
		Method:   "REQMOD",
		Path:     "/api/v1/test",
	}

	detail := model.formatScenarioDetail(scenario)

	assert.Contains(t, detail, "test scenario")
	assert.Contains(t, detail, "50")
	assert.Contains(t, detail, "REQMOD")
	assert.Contains(t, detail, "/api/v1/test")
}

func TestScenarioManagerModel_renderListView(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true

	scenarios := []ScenarioItem{
		{Name: "scenario1", Priority: 10},
	}
	model.SetScenarios(scenarios)

	view := model.renderListView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Scenarios")
}

func TestScenarioManagerModel_renderDetailView(t *testing.T) {
	model := NewScenarioManagerModel()
	model.selectedScenario = &ScenarioItem{Name: "test", Priority: 10}
	model.scenarioDetail = model.formatScenarioDetail(model.selectedScenario)

	view := model.renderDetailView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Scenario Details")
}

func TestScenarioManagerModel_renderEditView(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.nameInput.SetValue("test scenario")

	view := model.renderEditView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Edit Scenario")
}

func TestScenarioManagerModel_renderCreateView(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test scenario")

	view := model.renderCreateView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Create New Scenario")
}

func TestScenarioManagerModel_renderEditView_WithError(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.nameInput.SetValue("test")
	model.errorMessage = "Invalid YAML"

	view := model.renderEditView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Invalid YAML")
}

func TestScenarioManagerModel_renderCreateView_WithError(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.SetValue("test")
	model.errorMessage = "Priority must be a number"

	view := model.renderCreateView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Priority must be a number")
}

func TestScenarioManagerModel_Update_TabInEditMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.nameInput.Focus()

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})

	assert.NotNil(t, newModel)
}

func TestScenarioManagerModel_Update_TabInCreateMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate
	model.nameInput.Focus()

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})

	assert.NotNil(t, newModel)
}

func TestScenarioManagerModel_Update_CtrlCInCreateMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewCreate

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	assert.Equal(t, ScenarioViewList, newModel.mode)
}

func TestScenarioManagerModel_Update_CtrlSInEditMode(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "test"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("10")
	model.yamlEditor.SetValue("test: yaml")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	assert.NotNil(t, cmd)
}

func TestScenarioManagerModel_ConcurrentUpdate(t *testing.T) {
	model := NewScenarioManagerModel()
	model.ready = true

	scenarios := []ScenarioItem{
		{Name: "scenario1", Priority: 10, Method: "REQMOD", Path: "/api/v1/users"},
		{Name: "scenario2", Priority: 20, Method: "RESPMOD", Path: "/api/v1/products"},
	}
	model.SetScenarios(scenarios)

	var wg sync.WaitGroup
	iterations := 50

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		}()
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestScenarioManagerModel_ConcurrentSetScenarios(t *testing.T) {
	model := NewScenarioManagerModel()

	var wg sync.WaitGroup
	iterations := 10

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			scenarios := []ScenarioItem{
				{Name: "scenario1", Priority: index, Method: "REQMOD", Path: "/api/v1/test"},
			}
			model.SetScenarios(scenarios)
		}(i)
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestScenarioManagerModel_Update_WindowSize(t *testing.T) {
	model := NewScenarioManagerModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 100, newModel.width)
	assert.Equal(t, 50, newModel.height)
	assert.True(t, newModel.ready)
}

func TestScenarioManagerModel_Update_EnterWithSelection(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList
	model.ready = true

	scenarios := []ScenarioItem{
		{Name: "scenario1", Priority: 10, Method: "REQMOD", Path: "/api/v1/users"},
	}
	model.SetScenarios(scenarios)

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotNil(t, newModel)
}

func TestScenarioManagerModel_Update_EditWithoutSelection(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList
	model.selectedScenario = nil

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})

	assert.NotNil(t, newModel)
	assert.Equal(t, ScenarioViewList, newModel.mode)
}

func TestScenarioManagerModel_Update_DeleteWithoutSelection(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewList
	model.selectedScenario = nil

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	assert.NotNil(t, newModel)
	assert.Nil(t, cmd)
}

func TestScenarioManagerModel_saveScenario_EmptyPriority(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority is required")
}

func TestScenarioManagerModel_saveScenario_InvalidPriority_NotNumber(t *testing.T) {
	model := NewScenarioManagerModel()
	model.mode = ScenarioViewEdit
	model.selectedScenario = &ScenarioItem{Name: "old name"}
	model.nameInput.SetValue("new name")
	model.priorityInput.SetValue("abc")
	model.yamlEditor.SetValue("test: yaml")

	cmd := model.saveScenario()

	assert.Nil(t, cmd)
	assert.Contains(t, model.errorMessage, "Priority must be a valid number")
}
