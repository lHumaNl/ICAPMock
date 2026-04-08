// Copyright 2026 ICAP Mock

package components

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewReplayPanelModel(t *testing.T) {
	model := NewReplayPanelModel()

	assert.NotNil(t, model)
	assert.NotNil(t, model.requestList)
	assert.NotNil(t, model.replayResults)
	assert.Equal(t, ReplayViewList, model.mode)
	assert.Equal(t, 1.0, model.replaySpeed)
	assert.Equal(t, "http://localhost:1344", model.targetURL)
	assert.False(t, model.replayInProgress)
	assert.Zero(t, model.progress)
}

func TestReplayPanelModel_Init(t *testing.T) {
	model := NewReplayPanelModel()

	cmd := model.Init()
	assert.Nil(t, cmd)
}

func TestReplayPanelModel_SetRequests(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Method: "REQMOD", Path: "/api/v1/users"},
		{ID: "req-002", Method: "RESPMOD", Path: "/api/v1/products"},
	}

	model.SetRequests(requests)

	items := model.requestList.Items()
	assert.Len(t, items, 2)
}

func TestReplayPanelModel_SetReplayResults(t *testing.T) {
	model := NewReplayPanelModel()

	results := &ReplayResults{
		TotalRequests: 10,
		SuccessCount:  8,
		FailureCount:  2,
		StartTime:     time.Now(),
		EndTime:       time.Now(),
	}

	model.SetReplayResults(results)

	assert.Equal(t, results, model.replayResults)
}

func TestReplayPanelModel_UpdateProgress(t *testing.T) {
	model := NewReplayPanelModel()

	model.UpdateProgress(5, 10)

	assert.Equal(t, 0.5, model.progress)
}

func TestReplayPanelModel_UpdateProgress_Zero(t *testing.T) {
	model := NewReplayPanelModel()

	model.UpdateProgress(0, 10)

	assert.Equal(t, 0.0, model.progress)
}

func TestReplayPanelModel_startReplay(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList

	model.startReplay()

	assert.True(t, model.replayInProgress)
	assert.Equal(t, ReplayViewResults, model.mode)
	assert.NotNil(t, model.replayResults)
}

func TestReplayPanelModel_stopReplay(t *testing.T) {
	model := NewReplayPanelModel()
	model.replayInProgress = true
	startTime := time.Now().Add(-1 * time.Second)
	model.replayResults = &ReplayResults{StartTime: startTime}

	model.stopReplay()

	assert.False(t, model.replayInProgress)
	assert.True(t, model.replayResults.EndTime.After(startTime))
	assert.True(t, model.replayResults.TotalDuration >= 1*time.Second)
}

func TestReplayPanelModel_switchToListView(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults

	model.switchToListView()

	assert.Equal(t, ReplayViewList, model.mode)
	assert.Nil(t, model.selectedRequest)
}

func TestReplayPanelModel_switchToResultsView(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList

	model.switchToResultsView()

	assert.Equal(t, ReplayViewResults, model.mode)
}

func TestReplayPanelModel_switchToDetailView(t *testing.T) {
	model := NewReplayPanelModel()
	model.selectedRequest = &ReplayRequestItem{
		ID:         "req-001",
		Timestamp:  time.Now(),
		Method:     "REQMOD",
		Path:       "/api/v1/users",
		StatusCode: 200,
		Response:   "ICAP/1.0 204 No Content\n",
		Duration:   125 * time.Millisecond,
	}

	model.switchToDetailView()

	assert.Equal(t, ReplayViewDetail, model.mode)
	assert.NotEmpty(t, model.requestDetail)
}

func TestReplayPanelModel_switchToDetailView_Nil(t *testing.T) {
	model := NewReplayPanelModel()
	model.selectedRequest = nil

	model.switchToDetailView()

	assert.Equal(t, ReplayViewList, model.mode)
}

func TestReplayPanelModel_hasSelectedRequests(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: true},
		{ID: "req-002", Selected: false},
	}
	model.SetRequests(requests)

	assert.True(t, model.hasSelectedRequests())
}

func TestReplayPanelModel_hasSelectedRequests_None(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: false},
		{ID: "req-002", Selected: false},
	}
	model.SetRequests(requests)

	assert.False(t, model.hasSelectedRequests())
}

func TestReplayPanelModel_getSelectedRequests(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: true},
		{ID: "req-002", Selected: false},
		{ID: "req-003", Selected: true},
	}
	model.SetRequests(requests)

	selected := model.getSelectedRequests()
	assert.Len(t, selected, 2)
	for _, req := range selected {
		assert.True(t, req.Selected)
	}
}

func TestReplayPanelModel_Update_WindowSize(t *testing.T) {
	model := NewReplayPanelModel()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, _ := model.Update(msg)

	assert.Equal(t, 100, newModel.width)
	assert.Equal(t, 50, newModel.height)
	assert.True(t, newModel.ready)
}

func TestReplayPanelModel_Update_ToggleSelection(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.SetRequests([]ReplayRequestItem{
		{ID: "req-001", Selected: false},
	})

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})

	items := newModel.requestList.Items()
	assert.Len(t, items, 1)
	assert.True(t, items[0].(ReplayRequestItem).Selected)
}

func TestReplayPanelModel_Update_ChangeSpeed(t *testing.T) {
	tests := []struct {
		key           string
		expectedSpeed float64
	}{
		{"1", 0.5},
		{"2", 1.0},
		{"3", 2.0},
		{"4", 5.0},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			model := NewReplayPanelModel()
			model.mode = ReplayViewList

			newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})

			assert.Equal(t, tt.expectedSpeed, newModel.replaySpeed)
		})
	}
}

func TestReplayPanelModel_Update_Start(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.SetRequests([]ReplayRequestItem{
		{ID: "req-001", Selected: true},
	})

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})

	assert.NotNil(t, newModel)
	assert.True(t, newModel.replayInProgress)
}

func TestReplayPanelModel_Update_Stop(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.replayInProgress = true

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})

	assert.NotNil(t, newModel)
	assert.False(t, newModel.replayInProgress)
}

func TestReplayPanelModel_Update_Enter(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.SetRequests([]ReplayRequestItem{
		{ID: "req-001", Selected: true},
	})

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotNil(t, newModel)
	assert.Equal(t, ReplayViewDetail, newModel.mode)
}

func TestReplayPanelModel_Update_Escape(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.NotNil(t, newModel)
	assert.Equal(t, ReplayViewList, newModel.mode)
}

func TestReplayPanelModel_View_NotReady(t *testing.T) {
	model := NewReplayPanelModel()

	view := model.View()
	assert.Equal(t, "Loading replay panel...", view)
}

func TestReplayPanelModel_View_ListMode(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewList
	model.width = 100
	model.height = 50

	view := model.View()
	assert.NotEmpty(t, view)
	assert.NotEqual(t, "Loading replay panel...", view)
}

func TestReplayPanelModel_View_ResultsMode(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewResults
	model.replayInProgress = false
	model.replayResults = &ReplayResults{
		TotalRequests: 10,
		SuccessCount:  8,
		FailureCount:  2,
	}

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestReplayPanelModel_View_DetailMode(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewDetail
	model.selectedRequest = &ReplayRequestItem{ID: "req-001"}
	model.requestDetail = "Test details"

	view := model.View()
	assert.NotEmpty(t, view)
}

func TestReplayPanelModel_getStatusIcon(t *testing.T) {
	model := NewReplayPanelModel()

	tests := []struct {
		name    string
		want    string
		success bool
	}{
		{"success", "✓", true},
		{"failure", "✗", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			icon := model.getStatusIcon(tt.success)
			assert.Equal(t, tt.want, icon)
		})
	}
}

func TestReplayRequestItem_FilterValue(t *testing.T) {
	item := ReplayRequestItem{
		ID:         "req-001",
		Timestamp:  time.Now(),
		Method:     "REQMOD",
		Path:       "/api/v1/users",
		StatusCode: 200,
		Response:   "ICAP/1.0 204 No Content\n",
		Duration:   125 * time.Millisecond,
		Selected:   false,
	}

	filterValue := item.FilterValue()
	assert.Equal(t, "req-001", filterValue)
}

func TestReplayRequestItem_Title(t *testing.T) {
	tests := []struct {
		name     string
		want     string
		selected bool
	}{
		{"selected", "[✓]", true},
		{"not selected", "[ ]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := ReplayRequestItem{
				ID:        "req-001",
				Timestamp: time.Now(),
				Method:    "REQMOD",
				Path:      "/api/v1/users",
				Selected:  tt.selected,
			}

			title := item.Title()
			assert.Contains(t, title, tt.want)
			assert.Contains(t, title, "REQMOD")
			assert.Contains(t, title, "/api/v1/users")
		})
	}
}

func TestReplayRequestItem_Description(t *testing.T) {
	item := ReplayRequestItem{
		ID:         "req-001",
		Timestamp:  time.Now(),
		Method:     "REQMOD",
		Path:       "/api/v1/users",
		StatusCode: 200,
		Response:   "ICAP/1.0 204 No Content\n",
		Duration:   125 * time.Millisecond,
		Selected:   false,
	}

	description := item.Description()
	assert.Contains(t, description, "Status: 200")
	assert.Contains(t, description, "Duration:")
}

func TestReplayResults(t *testing.T) {
	results := ReplayResults{
		TotalRequests:  10,
		SuccessCount:   8,
		FailureCount:   2,
		TotalDuration:  5 * time.Second,
		AverageLatency: 50 * time.Millisecond,
		StartTime:      time.Now(),
		EndTime:        time.Now(),
		RequestResults: []RequestResult{
			{ID: "req-001", Success: true, StatusCode: 200, Duration: 50 * time.Millisecond},
			{ID: "req-002", Success: false, StatusCode: 500, Duration: 100 * time.Millisecond, Error: "timeout"},
		},
	}

	assert.Equal(t, 10, results.TotalRequests)
	assert.Equal(t, 8, results.SuccessCount)
	assert.Equal(t, 2, results.FailureCount)
	assert.Len(t, results.RequestResults, 2)
}

func TestRequestResult(t *testing.T) {
	result := RequestResult{
		ID:         "req-001",
		Success:    true,
		StatusCode: 200,
		Duration:   125 * time.Millisecond,
		Error:      "",
	}

	assert.Equal(t, "req-001", result.ID)
	assert.True(t, result.Success)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, 125*time.Millisecond, result.Duration)
	assert.Empty(t, result.Error)
}

func TestReplayListMsg(t *testing.T) {
	requests := []ReplayRequestItem{
		{ID: "req-001"},
		{ID: "req-002"},
	}

	msg := ReplayListMsg{Requests: requests}

	assert.Equal(t, requests, msg.Requests)
}

func TestReplayStartMsg(t *testing.T) {
	requests := []ReplayRequestItem{
		{ID: "req-001"},
	}

	msg := ReplayStartMsg{
		Requests:  requests,
		Speed:     1.5,
		TargetURL: "http://localhost:1344",
	}

	assert.Equal(t, requests, msg.Requests)
	assert.Equal(t, 1.5, msg.Speed)
	assert.Equal(t, "http://localhost:1344", msg.TargetURL)
}

func TestReplayProgressMsg(t *testing.T) {
	result := &RequestResult{
		ID:         "req-001",
		Success:    true,
		StatusCode: 200,
		Duration:   125 * time.Millisecond,
	}

	msg := ReplayProgressMsg{
		Current: 5,
		Total:   10,
		Result:  result,
	}

	assert.Equal(t, 5, msg.Current)
	assert.Equal(t, 10, msg.Total)
	assert.Equal(t, result, msg.Result)
}

func TestReplayCompleteMsg(t *testing.T) {
	results := &ReplayResults{
		TotalRequests: 10,
		SuccessCount:  8,
		FailureCount:  2,
	}

	msg := ReplayCompleteMsg{Results: results}

	assert.Equal(t, results, msg.Results)
}

func TestReplayErrorMsg(t *testing.T) {
	err := assert.AnError

	msg := ReplayErrorMsg{Err: err}

	assert.Equal(t, err, msg.Err)
}

func TestReplayPanelModel_Update_Export(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults
	model.replayResults = &ReplayResults{
		TotalRequests: 10,
		RequestResults: []RequestResult{
			{ID: "req-001", Success: true},
		},
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})

	assert.NotNil(t, cmd)

	cmdMsg := cmd()
	assert.IsType(t, ReplayExportMsg{}, cmdMsg)
}

func TestReplayPanelModel_Update_Export_NoResults(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults
	model.replayResults = &ReplayResults{
		TotalRequests:  0,
		RequestResults: []RequestResult{},
	}

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})

	assert.NotNil(t, newModel)
	assert.Nil(t, cmd)
}

func TestReplayPanelModel_Update_ProgressUpdate(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults

	model.UpdateProgress(5, 10)

	assert.Equal(t, 0.5, model.progress)
}

func TestReplayPanelModel_updateItem_Found(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: false},
		{ID: "req-002", Selected: false},
	}
	model.SetRequests(requests)

	updatedItem := ReplayRequestItem{ID: "req-001", Selected: true}
	model.updateItem(updatedItem)

	items := model.requestList.Items()
	found := false
	for _, item := range items {
		req, ok := item.(ReplayRequestItem)
		if ok && req.ID == "req-001" {
			assert.True(t, req.Selected)
			found = true
		}
	}
	assert.True(t, found)
}

func TestReplayPanelModel_updateItem_NotFound(t *testing.T) {
	model := NewReplayPanelModel()

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: false},
	}
	model.SetRequests(requests)

	updatedItem := ReplayRequestItem{ID: "req-999", Selected: true}
	model.updateItem(updatedItem)

	items := model.requestList.Items()
	assert.Len(t, items, 1)
	req := items[0].(ReplayRequestItem)
	assert.False(t, req.Selected)
}

func TestReplayPanelModel_renderListView_WithError(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewList
	model.errorMessage = "An error occurred"

	view := model.renderListView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "An error occurred")
}

func TestReplayPanelModel_renderDetailView_WithError(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewDetail
	model.requestDetail = "Test details"
	model.errorMessage = "Failed to load details"

	view := model.renderDetailView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Failed to load details")
}

func TestReplayPanelModel_renderResultsView_InProgress(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewResults
	model.replayInProgress = true
	model.progress = 0.5
	model.replayResults = &ReplayResults{
		TotalRequests: 10,
		SuccessCount:  5,
		FailureCount:  0,
		RequestResults: []RequestResult{
			{ID: "req-001", Success: true},
		},
	}

	view := model.renderResultsView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Replay Progress")
}

func TestReplayPanelModel_renderResultsView_WithErrors(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewResults
	model.replayInProgress = false
	model.replayResults = &ReplayResults{
		TotalRequests: 10,
		SuccessCount:  7,
		FailureCount:  3,
		RequestResults: []RequestResult{
			{ID: "req-001", Success: true, Duration: 100 * time.Millisecond},
			{ID: "req-002", Success: false, Duration: 200 * time.Millisecond, Error: "Connection timeout"},
		},
	}

	view := model.renderResultsView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Success: 7")
	assert.Contains(t, view, "Failed: 3")
	assert.Contains(t, view, "Connection timeout")
}

func TestReplayPanelModel_formatRequestDetail(t *testing.T) {
	model := NewReplayPanelModel()

	request := &ReplayRequestItem{
		ID:         "req-001",
		Timestamp:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Method:     "REQMOD",
		Path:       "/api/v1/users",
		StatusCode: 200,
		Response:   "ICAP/1.0 204 No Content\n",
		Duration:   125 * time.Millisecond,
	}

	detail := model.formatRequestDetail(request)

	assert.NotEmpty(t, detail)
	assert.Contains(t, detail, "req-001")
	assert.Contains(t, detail, "REQMOD")
	assert.Contains(t, detail, "/api/v1/users")
	assert.Contains(t, detail, "200")
}

func TestReplayPanelModel_hasSelectedRequests_Empty(t *testing.T) {
	model := NewReplayPanelModel()

	model.SetRequests([]ReplayRequestItem{})

	assert.False(t, model.hasSelectedRequests())
}

func TestReplayPanelModel_getSelectedRequests_Empty(t *testing.T) {
	model := NewReplayPanelModel()

	model.SetRequests([]ReplayRequestItem{})

	selected := model.getSelectedRequests()

	assert.Len(t, selected, 0)
}

func TestReplayPanelModel_stopReplay_WithoutResults(t *testing.T) {
	model := NewReplayPanelModel()
	model.replayInProgress = true
	model.replayResults = nil

	model.stopReplay()

	assert.False(t, model.replayInProgress)
}

func TestReplayPanelModel_Update_InvalidSpeedKey(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.replaySpeed = 1.0

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})

	assert.Equal(t, 1.0, newModel.replaySpeed)
}

func TestReplayPanelModel_Update_Enter_NoSelection(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotNil(t, newModel)
	assert.Nil(t, newModel.selectedRequest)
}

func TestReplayPanelModel_View_UnknownMode(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewMode(99)

	view := model.View()

	assert.Equal(t, "Unknown view mode", view)
}

func TestReplayPanelModel_Update_SkipReplay(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList
	model.SetRequests([]ReplayRequestItem{
		{ID: "req-001", Selected: false},
	})

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})

	assert.NotNil(t, newModel)
	assert.False(t, newModel.replayInProgress)
}

func TestReplayPanelModel_Update_Escape_InListMode(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewList

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.NotNil(t, newModel)
	assert.Equal(t, ReplayViewList, newModel.mode)
}

func TestReplayPanelModel_renderResultsView_NoResults(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewResults
	model.replayInProgress = false
	model.replayResults = &ReplayResults{
		TotalRequests: 0,
		SuccessCount:  0,
		FailureCount:  0,
	}

	view := model.renderResultsView()

	assert.NotEmpty(t, view)
}

func TestReplayPanelModel_renderResultsView_WithAverageLatency(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewResults
	model.replayInProgress = false
	model.replayResults = &ReplayResults{
		TotalRequests:  10,
		SuccessCount:   8,
		FailureCount:   2,
		TotalDuration:  5 * time.Second,
		AverageLatency: 50 * time.Millisecond,
	}

	view := model.renderResultsView()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Avg Latency")
}

func TestReplayPanelModel_ConcurrentUpdate(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: true},
		{ID: "req-002", Selected: false},
	}
	model.SetRequests(requests)

	var wg sync.WaitGroup
	iterations := 50

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index%2 == 0 {
				_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
			} else {
				_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
			}
		}(i)
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestReplayPanelModel_ConcurrentSetRequests(t *testing.T) {
	model := NewReplayPanelModel()

	var wg sync.WaitGroup
	iterations := 10

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			requests := []ReplayRequestItem{
				{ID: "req-001", Selected: index%2 == 0},
			}
			model.SetRequests(requests)
		}(i)
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestReplayPanelModel_ConcurrentToggleSelection(t *testing.T) {
	model := NewReplayPanelModel()
	model.ready = true
	model.mode = ReplayViewList

	requests := []ReplayRequestItem{
		{ID: "req-001", Selected: false},
		{ID: "req-002", Selected: false},
	}
	model.SetRequests(requests)

	var wg sync.WaitGroup
	iterations := 20

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
		}()
	}

	wg.Wait()

	assert.NotNil(t, model)
}

func TestReplayPanelModel_updateResults(t *testing.T) {
	model := NewReplayPanelModel()
	model.mode = ReplayViewResults

	cmd := model.updateResults(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	assert.Nil(t, cmd)
}

func TestReplayPanelModel_hasSelectedRequests_WithNilItems(t *testing.T) {
	model := NewReplayPanelModel()

	model.requestList.SetItems(nil)

	result := model.hasSelectedRequests()

	assert.False(t, result)
}

func TestReplayPanelModel_getSelectedRequests_WithNilItems(t *testing.T) {
	model := NewReplayPanelModel()

	model.requestList.SetItems(nil)

	selected := model.getSelectedRequests()

	assert.Len(t, selected, 0)
}
