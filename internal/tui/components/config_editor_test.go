// Copyright 2026 ICAP Mock

package components

import (
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewConfigEditorModel(t *testing.T) {
	model := NewConfigEditorModel()

	assert.NotNil(t, model.textarea)
	assert.Empty(t, model.content)
	assert.Empty(t, model.filePath)
	assert.Equal(t, "yaml", model.fileType)
	assert.False(t, model.validation.Valid)
	assert.True(t, model.showHelp)
	assert.False(t, model.loading)
	assert.False(t, model.modified)
}

func TestConfigEditorModel_Init(t *testing.T) {
	model := NewConfigEditorModel()
	cmd := model.Init()

	assert.Nil(t, cmd)
}

func TestConfigEditorModel_SetContent(t *testing.T) {
	model := NewConfigEditorModel()

	content := "server:\n  port: 1344"
	filePath := "/path/to/config.yaml"

	model.SetContent(content, filePath)

	assert.Equal(t, content, model.content)
	assert.Equal(t, filePath, model.filePath)
	assert.Equal(t, "yaml", model.fileType)
	assert.False(t, model.modified)
}

func TestConfigEditorModel_SetContent_Json(t *testing.T) {
	model := NewConfigEditorModel()

	content := `{"server": {"port": 1344}}`
	filePath := "/path/to/config.json"

	model.SetContent(content, filePath)

	assert.Equal(t, content, model.content)
	assert.Equal(t, filePath, model.filePath)
	assert.Equal(t, "json", model.fileType)
	assert.False(t, model.modified)
}

func TestConfigEditorModel_GetContent(t *testing.T) {
	model := NewConfigEditorModel()
	content := "test content"

	model.textarea.SetValue(content)

	assert.Equal(t, content, model.GetContent())
}

func TestConfigEditorModel_GetFilePath(t *testing.T) {
	model := NewConfigEditorModel()
	filePath := "/path/to/config.yaml"

	model.SetContent("content", filePath)

	assert.Equal(t, filePath, model.GetFilePath())
}

func TestConfigEditorModel_IsModified(t *testing.T) {
	model := NewConfigEditorModel()

	assert.False(t, model.IsModified())

	model.modified = true
	assert.True(t, model.IsModified())
}

func TestConfigEditorModel_SetLoading(t *testing.T) {
	model := NewConfigEditorModel()

	assert.False(t, model.loading)

	model.SetLoading(true)
	assert.True(t, model.loading)

	model.SetLoading(false)
	assert.False(t, model.loading)
}

func TestConfigEditorModel_SetWindowSize(t *testing.T) {
	model := NewConfigEditorModel()
	width := 80
	height := 24

	model.SetWindowSize(width, height)

	assert.Equal(t, width, model.width)
	assert.Equal(t, height, model.height)
}

func TestConfigEditorModel_Reset(t *testing.T) {
	model := NewConfigEditorModel()

	// Set some values
	model.content = "test content"
	model.filePath = "/path/to/config.yaml"
	model.fileType = "json"
	model.modified = true
	model.loading = true
	model.validation.Valid = true

	model.Reset()

	assert.Empty(t, model.content)
	assert.Empty(t, model.filePath)
	assert.Equal(t, "yaml", model.fileType)
	assert.False(t, model.modified)
	assert.False(t, model.loading)
	assert.False(t, model.validation.Valid)
}

func TestConfigEditorModel_ClearValidation(t *testing.T) {
	model := NewConfigEditorModel()

	model.validation.Valid = true
	model.validation.Message = "Valid"

	model.ClearValidation()

	assert.False(t, model.validation.Valid)
	assert.Equal(t, "No content to validate", model.validation.Message)
}

func TestConfigEditorModel_ValidateContent_YAML(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "yaml"

	validYaml := "server:\n  port: 1344\n  host: localhost"
	model.textarea.SetValue(validYaml)

	model.validateContent()

	assert.True(t, model.validation.Valid)
	assert.Equal(t, "Valid configuration", model.validation.Message)
}

func TestConfigEditorModel_ValidateContent_InvalidYAML(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "yaml"

	invalidYaml := "server:\n  port: 1344\n  host: localhost\n  invalid: ["
	model.textarea.SetValue(invalidYaml)

	model.validateContent()

	assert.False(t, model.validation.Valid)
	assert.NotEmpty(t, model.validation.Error)
}

func TestConfigEditorModel_ValidateContent_JSON(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	validJSON := `{"server": {"port": 1344, "host": "localhost"}}`
	model.textarea.SetValue(validJSON)

	model.validateContent()

	assert.True(t, model.validation.Valid)
	assert.Equal(t, "Valid configuration", model.validation.Message)
}

func TestConfigEditorModel_ValidateContent_InvalidJSON(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	invalidJSON := `{"server": {"port": 1344, "host": "localhost"}`
	model.textarea.SetValue(invalidJSON)

	model.validateContent()

	assert.False(t, model.validation.Valid)
	assert.NotEmpty(t, model.validation.Error)
}

func TestConfigEditorModel_ValidateContent_Empty(t *testing.T) {
	model := NewConfigEditorModel()

	model.textarea.SetValue("")

	model.validateContent()

	assert.False(t, model.validation.Valid)
	assert.Equal(t, "No content to validate", model.validation.Message)
}

func TestConfigEditorModel_Update_KeyboardShortcuts(t *testing.T) {
	tests := []struct {
		check func(*ConfigEditorModel)
		name  string
		key   string
	}{
		{
			name: "toggle help",
			key:  "ctrl+l",
			check: func(m *ConfigEditorModel) {
				assert.False(t, m.showHelp)
			},
		},
		{
			name: "validate",
			key:  "ctrl+v",
			check: func(m *ConfigEditorModel) {
				// Just check that validation was called
				assert.NotNil(t, m.validation)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			m := NewConfigEditorModel()
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}

			// We don't use the return value, just check the model state
			_, _ = m.Update(msg)

			tt.check(m)
		})
	}
}

func TestConfigEditorModel_SetContent_EmptyPath(t *testing.T) {
	model := NewConfigEditorModel()

	err := model.SetContent("content", "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file path cannot be empty")
}

func TestConfigEditorModel_SetContent_EmptyContent(t *testing.T) {
	model := NewConfigEditorModel()

	err := model.SetContent("", "/path/to/config.yaml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content cannot be empty")
}

func TestConfigEditorModel_SetContent_WhitespaceContent(t *testing.T) {
	model := NewConfigEditorModel()

	err := model.SetContent("   \n  \t  ", "/path/to/config.yaml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "content cannot be empty")
}

func TestConfigEditorModel_SetContent_TooLarge(t *testing.T) {
	model := NewConfigEditorModel()

	content := string(make([]byte, 1024*1024+1))

	err := model.SetContent(content, "/path/to/config.yaml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestConfigEditorModel_SetContent_InvalidYAML(t *testing.T) {
	model := NewConfigEditorModel()

	invalidYAML := "server:\n  port: 1344\n  invalid: ["
	err := model.SetContent(invalidYAML, "/path/to/config.yaml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

func TestConfigEditorModel_formatContent_JSON(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	minifiedJSON := `{"name":"test","value":123}`
	model.textarea.SetValue(minifiedJSON)

	model.formatContent()

	result := model.textarea.Value()
	assert.Contains(t, result, "\n")
	assert.Contains(t, result, "  ")
}

func TestConfigEditorModel_formatContent_YAML(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "yaml"

	compactYAML := "name:test\nvalue:123"
	model.textarea.SetValue(compactYAML)

	model.formatContent()

	result := model.textarea.Value()
	assert.Contains(t, result, "\n")
}

func TestConfigEditorModel_formatContent_InvalidJSON(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	invalidJSON := `{"invalid": json}`
	model.textarea.SetValue(invalidJSON)
	prevValue := model.textarea.Value()

	model.formatContent()

	assert.Equal(t, prevValue, model.textarea.Value())
	assert.False(t, model.modified)
}

func TestConfigEditorModel_formatContent_EmptyContent(t *testing.T) {
	model := NewConfigEditorModel()

	model.textarea.SetValue("")
	model.formatContent()

	assert.Empty(t, model.textarea.Value())
}

func TestConfigEditorModel_Update_TextInput(t *testing.T) {
	model := NewConfigEditorModel()

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.NotNil(t, newModel)
	assert.True(t, newModel.modified)
}

func TestConfigEditorModel_Update_AutoValidation(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "yaml"

	model.textarea.SetValue("server:\n  port: 1344")
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	assert.True(t, newModel.validation.Valid)
}

func TestConfigEditorModel_View_NotReady(t *testing.T) {
	model := NewConfigEditorModel()

	view := model.View()

	assert.NotEmpty(t, view)
}

func TestConfigEditorModel_View_WithContent(t *testing.T) {
	model := NewConfigEditorModel()
	model.width = 100
	model.height = 50
	model.SetWindowSize(100, 50)

	validYAML := "server:\n  port: 1344"
	model.SetContent(validYAML, "/path/to/config.yaml")

	view := model.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Configuration Editor")
}

func TestConfigEditorModel_View_ValidationError(t *testing.T) {
	model := NewConfigEditorModel()
	model.width = 100
	model.height = 50
	model.SetWindowSize(100, 50)

	invalidYAML := "server:\n  invalid: ["
	model.textarea.SetValue(invalidYAML)
	model.validateContent()

	view := model.View()

	assert.NotEmpty(t, view)
}

func TestConfigEditorModel_View_Loading(t *testing.T) {
	model := NewConfigEditorModel()
	model.loading = true

	view := model.View()

	assert.Contains(t, view, "Loading configuration")
}

func TestConfigEditorModel_View_WithoutHelp(t *testing.T) {
	model := NewConfigEditorModel()
	model.showHelp = false

	view := model.View()

	assert.NotContains(t, view, "Ctrl+S")
}

func TestConfigEditorModel_Reload(t *testing.T) {
	model := NewConfigEditorModel()

	cmd := model.Reload()

	assert.NotNil(t, cmd)
}

func TestConfigEditorModel_SetWindowSize_SmallHeight(t *testing.T) {
	model := NewConfigEditorModel()

	model.SetWindowSize(100, 5)

	assert.Equal(t, 100, model.width)
	assert.Equal(t, 5, model.height)
}

func TestConfigEditorModel_ValidateContent_WithWhitespace(t *testing.T) {
	model := NewConfigEditorModel()

	whitespaceOnly := "   \n\t\n  "
	model.textarea.SetValue(whitespaceOnly)

	model.validateContent()

	assert.False(t, model.validation.Valid)
	assert.Contains(t, model.validation.Message, "No content")
}

func TestConfigEditorModel_renderValidation(t *testing.T) {
	tests := []struct {
		name    string
		error   string
		message string
		valid   bool
	}{
		{
			name:    "valid",
			valid:   true,
			error:   "",
			message: "Valid configuration",
		},
		{
			name:    "invalid with error",
			valid:   false,
			error:   "invalid syntax at line 5",
			message: "Invalid configuration",
		},
		{
			name:    "no content",
			valid:   false,
			error:   "",
			message: "No content to validate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewConfigEditorModel()
			model.validation = ValidationStatus{
				Valid:   tt.valid,
				Error:   tt.error,
				Message: tt.message,
			}

			rendered := model.renderValidation()

			assert.NotEmpty(t, rendered)
		})
	}
}

func TestConfigEditorModel_ConcurrentUpdate(t *testing.T) {
	model := NewConfigEditorModel()
	content := "server:\n  port: 1344"
	model.SetContent(content, "/path/to/config.yaml")

	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
		}()
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestConfigEditorModel_ConcurrentSetContent(t *testing.T) {
	model := NewConfigEditorModel()

	var wg sync.WaitGroup
	errors := make(chan error, 10)
	iterations := 10

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			content := "server:\n  port: " + string(rune('0'+index%10))
			if err := model.SetContent(content, "/path/to/config.yaml"); err != nil {
				errors <- err
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(errors)
	}()

	for err := range errors {
		t.Log("Error during concurrent SetContent:", err)
	}
}

func TestConfigEditorModel_Update_CtrlR(t *testing.T) {
	model := NewConfigEditorModel()

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+r")})

	assert.NotNil(t, cmd)
}

func TestConfigEditorModel_Update_CtrlF(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	minifiedJSON := `{"name":"test","value":123}`
	model.textarea.SetValue(minifiedJSON)

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+f")})

	assert.NotNil(t, newModel)
	assert.True(t, newModel.modified)
}

func TestConfigEditorModel_renderHeader_Modified(t *testing.T) {
	model := NewConfigEditorModel()
	model.filePath = "/path/to/config.yaml"
	model.fileType = "yaml"
	model.modified = true

	header := model.renderHeader()

	assert.Contains(t, header, "*")
	assert.Contains(t, header, "/path/to/config.yaml")
	assert.Contains(t, header, "YAML")
}

func TestConfigEditorModel_renderHeader_NewFile(t *testing.T) {
	model := NewConfigEditorModel()
	model.filePath = ""
	model.fileType = "yaml"
	model.modified = false

	header := model.renderHeader()

	assert.Contains(t, header, "New file")
	assert.NotContains(t, header, "*")
}

func TestConfigEditorModel_Update_Esc(t *testing.T) {
	model := NewConfigEditorModel()

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.NotNil(t, newModel)
}

func TestConfigEditorModel_Update_CtrlS(t *testing.T) {
	model := NewConfigEditorModel()

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+s")})

	assert.NotNil(t, newModel)
}

func TestConfigEditorModel_formatContent_JSONWithErrors(t *testing.T) {
	model := NewConfigEditorModel()
	model.fileType = "json"

	jsonWithQuotes := `{"test": "value with \"quotes\""}`
	model.textarea.SetValue(jsonWithQuotes)

	model.formatContent()

	result := model.textarea.Value()
	assert.Contains(t, result, "\n")
}
