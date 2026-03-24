// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScenarioRegistry_Load tests loading scenarios from YAML.
func TestScenarioRegistry_Load(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "block-malware"
    priority: 100
    match:
      path_pattern: "^/scan.*"
      http_method: "POST"
      body_pattern: "(?i)(malware|virus)"
    response:
      icap_status: 200
      http_status: 403
      headers:
        X-Block-Reason: "malware-detected"
      body: "Access Denied"

  - name: "allow-images"
    priority: 50
    match:
      path_pattern: "^/scan/images"
      http_method: "GET"
    response:
      icap_status: 204

  - name: "delay-response"
    priority: 10
    match:
      path_pattern: "^/slow"
    response:
      icap_status: 200
      delay: "500ms"
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	scenarios := registry.List()
	if len(scenarios) != 4 { // 3 + default
		t.Errorf("List() got %d scenarios, want 4", len(scenarios))
	}

	// Verify priority order (highest first)
	if scenarios[0].Priority < scenarios[1].Priority {
		t.Error("Scenarios should be sorted by priority (descending)")
	}
}

// TestScenarioRegistry_Load_InvalidYAML tests loading invalid YAML.
func TestScenarioRegistry_Load_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "invalid.yaml")

	if err := os.WriteFile(scenarioFile, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	err := registry.Load(scenarioFile)
	if err == nil {
		t.Error("Load() should return error for invalid YAML")
	}
}

// TestScenarioRegistry_Load_MissingFile tests loading non-existent file.
func TestScenarioRegistry_Load_MissingFile(t *testing.T) {
	registry := NewScenarioRegistry()
	err := registry.Load("/nonexistent/scenarios.yaml")
	if err == nil {
		t.Error("Load() should return error for missing file")
	}
}

// TestScenarioRegistry_Load_InvalidRegex tests loading scenario with invalid regex.
func TestScenarioRegistry_Load_InvalidRegex(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "invalid_regex.yaml")

	yamlContent := `
scenarios:
  - name: "invalid-regex"
    priority: 100
    match:
      path_pattern: "[invalid(regex"
    response:
      icap_status: 204
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	err := registry.Load(scenarioFile)
	if err == nil {
		t.Error("Load() should return error for invalid regex")
	}
}

// TestScenarioRegistry_Match tests scenario matching.
func TestScenarioRegistry_Match(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "block-malware"
    priority: 100
    match:
      path_pattern: "^/scan.*"
      http_method: "POST"
      body_pattern: "(?i)(malware|virus)"
    response:
      icap_status: 200
      http_status: 403

  - name: "allow-all-get"
    priority: 50
    match:
      http_method: "GET"
    response:
      icap_status: 204

  - name: "catch-all"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name         string
		req          *icap.Request
		wantScenario string
		wantErr      bool
	}{
		{
			name: "match malware pattern",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/scan",
				HTTPRequest: &icap.HTTPMessage{
					Method: "POST",
					URI:    "http://example.com/scan",
					Body:   []byte("this contains malware"),
				},
			},
			wantScenario: "block-malware",
		},
		{
			name: "match GET request",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
				HTTPRequest: &icap.HTTPMessage{
					Method: "GET",
					URI:    "http://example.com/page",
				},
			},
			wantScenario: "allow-all-get",
		},
		{
			name: "match catch-all",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
				HTTPRequest: &icap.HTTPMessage{
					Method: "PUT",
					URI:    "http://example.com/resource",
				},
			},
			wantScenario: "catch-all",
		},
		{
			name: "no HTTP request - default",
			req: &icap.Request{
				Method: icap.MethodOPTIONS,
				URI:    "icap://localhost/reqmod",
			},
			wantScenario: "catch-all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario, err := registry.Match(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Match() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if scenario.Name != tt.wantScenario {
				t.Errorf("Match() scenario = %v, want %v", scenario.Name, tt.wantScenario)
			}
		})
	}
}

// TestScenarioRegistry_Match_LazyBody tests body pattern matching with lazy loading.
func TestScenarioRegistry_Match_LazyBody(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "block-malware"
    priority: 100
    match:
      body_pattern: "(?i)(malware|virus)"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test with body already loaded
	httpMsg := &icap.HTTPMessage{}
	httpMsg.SetLoadedBody([]byte("this contains malware"))
	req1 := &icap.Request{
		Method:      icap.MethodREQMOD,
		URI:         "icap://localhost/scan",
		HTTPRequest: httpMsg,
	}
	scenario, err := registry.Match(req1)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "block-malware" {
		t.Errorf("Match() scenario = %v, want block-malware", scenario.Name)
	}
}

// TestScenarioRegistry_Match_ByMethod tests matching by ICAP method.
func TestScenarioRegistry_Match_ByMethod(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "reqmod-handler"
    priority: 100
    match:
      icap_method: "REQMOD"
    response:
      icap_status: 204

  - name: "respmod-handler"
    priority: 100
    match:
      icap_method: "RESPMOD"
    response:
      icap_status: 204
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test REQMOD match
	reqmodReq := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
	}
	scenario, err := registry.Match(reqmodReq)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "reqmod-handler" {
		t.Errorf("Match() scenario = %v, want reqmod-handler", scenario.Name)
	}

	// Test RESPMOD match
	respmodReq := &icap.Request{
		Method: icap.MethodRESPMOD,
		URI:    "icap://localhost/respmod",
	}
	scenario, err = registry.Match(respmodReq)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "respmod-handler" {
		t.Errorf("Match() scenario = %v, want respmod-handler", scenario.Name)
	}
}

// TestScenarioRegistry_Match_ByHeader tests matching by headers.
func TestScenarioRegistry_Match_ByHeader(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "special-client"
    priority: 100
    match:
      headers:
        X-Special-Client: "true"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test with matching header
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
		Header: icap.NewHeader(),
	}
	req.Header.Set("X-Special-Client", "true")

	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "special-client" {
		t.Errorf("Match() scenario = %v, want special-client", scenario.Name)
	}

	// Test without matching header
	req2 := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/reqmod",
		Header: icap.NewHeader(),
	}
	scenario, err = registry.Match(req2)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	// Should match default
	if scenario.Name != "default" {
		t.Errorf("Match() scenario = %v, want default", scenario.Name)
	}
}

// TestScenarioRegistry_Add tests adding scenarios programmatically.
func TestScenarioRegistry_Add(t *testing.T) {
	registry := NewScenarioRegistry()

	scenario := &Scenario{
		Name: "test-scenario",
		Match: MatchRule{
			Path: "^/test",
		},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	scenarios := registry.List()
	found := false
	for _, s := range scenarios {
		if s.Name == "test-scenario" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Scenario not found after Add()")
	}
}

// TestScenarioRegistry_Add_Duplicate tests adding duplicate scenario.
func TestScenarioRegistry_Add_Duplicate(t *testing.T) {
	registry := NewScenarioRegistry()

	scenario1 := &Scenario{
		Name:  "duplicate",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	scenario2 := &Scenario{
		Name:  "duplicate",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 200,
		},
		Priority: 100,
	}

	if err := registry.Add(scenario1); err != nil {
		t.Fatalf("Add() first error = %v", err)
	}

	if err := registry.Add(scenario2); err != nil {
		t.Fatalf("Add() second error = %v", err)
	}

	// Should have replaced the first one
	scenarios := registry.List()
	count := 0
	for _, s := range scenarios {
		if s.Name == "duplicate" {
			count++
			if s.Priority != 100 {
				t.Error("Duplicate scenario should have been replaced")
			}
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 scenario with name 'duplicate', got %d", count)
	}
}

// TestScenarioRegistry_Remove tests removing scenarios.
func TestScenarioRegistry_Remove(t *testing.T) {
	registry := NewScenarioRegistry()

	scenario := &Scenario{
		Name:  "removable",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := registry.Remove("removable"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	scenarios := registry.List()
	for _, s := range scenarios {
		if s.Name == "removable" {
			t.Error("Scenario should have been removed")
		}
	}
}

// TestScenarioRegistry_Remove_NotFound tests removing non-existent scenario.
func TestScenarioRegistry_Remove_NotFound(t *testing.T) {
	registry := NewScenarioRegistry()

	err := registry.Remove("nonexistent")
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("Remove() error = %v, want %v", err, ErrNoMatch)
	}
}

// TestScenarioRegistry_Reload tests reloading scenarios.
func TestScenarioRegistry_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Initial content
	yamlContent := `
scenarios:
  - name: "initial"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Modify file
	yamlContent = `
scenarios:
  - name: "updated"
    priority: 100
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Reload
	if err := registry.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Verify updated scenario exists
	scenarios := registry.List()
	found := false
	for _, s := range scenarios {
		if s.Name == "updated" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Updated scenario not found after reload")
	}
}

// TestScenarioRegistry_DefaultScenario tests default scenario behavior.
func TestScenarioRegistry_DefaultScenario(t *testing.T) {
	registry := NewScenarioRegistry()

	// Should always have default scenario
	scenarios := registry.List()
	found := false
	for _, s := range scenarios {
		if s.Name == "default" {
			found = true
			if s.Response.ICAPStatus != 204 {
				t.Error("Default scenario should return 204")
			}
			break
		}
	}
	if !found {
		t.Error("Default scenario not found")
	}

	// Should match any request
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/any",
	}
	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "default" {
		t.Errorf("Match() scenario = %v, want default", scenario.Name)
	}
}

// TestResponseTemplate_GetBody tests getting response body.
func TestResponseTemplate_GetBody(t *testing.T) {
	t.Run("inline body", func(t *testing.T) {
		rt := ResponseTemplate{
			Body: "inline content",
		}
		body, err := rt.GetBody()
		if err != nil {
			t.Fatalf("GetBody() error = %v", err)
		}
		if body != "inline content" {
			t.Errorf("GetBody() = %v, want 'inline content'", body)
		}
	})

	t.Run("file body", func(t *testing.T) {
		tmpDir := t.TempDir()
		bodyFile := filepath.Join(tmpDir, "body.txt")
		if err := os.WriteFile(bodyFile, []byte("file content"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		rt := ResponseTemplate{
			BodyFile: bodyFile,
		}
		body, err := rt.GetBody()
		if err != nil {
			t.Fatalf("GetBody() error = %v", err)
		}
		if body != "file content" {
			t.Errorf("GetBody() = %v, want 'file content'", body)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		rt := ResponseTemplate{
			BodyFile: "/nonexistent/body.txt",
		}
		_, err := rt.GetBody()
		if err == nil {
			t.Error("GetBody() should return error for missing file")
		}
	})
}

// TestScenarioRegistry_ThreadSafety tests concurrent access.
func TestScenarioRegistry_ThreadSafety(t *testing.T) {
	registry := NewScenarioRegistry()

	done := make(chan bool)

	// Concurrent adds
	for i := 0; i < 10; i++ {
		go func(n int) {
			scenario := &Scenario{
				Name:     "concurrent-" + string(rune('0'+n)),
				Priority: n,
				Response: ResponseTemplate{ICAPStatus: 204},
			}
			_ = registry.Add(scenario)
			done <- true
		}(i)
	}

	// Concurrent matches
	for i := 0; i < 10; i++ {
		go func() {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/test",
			}
			_, _ = registry.Match(req)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestExtractPath tests the extractPath helper function.
func TestExtractPath(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"icap://localhost:1344/reqmod", "/reqmod"},
		{"icap://localhost/scan", "/scan"},
		{"icap://example.com/api/v1/check", "/api/v1/check"},
		{"icap://localhost", "/"},
		{"icap://localhost/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			got := extractPath(tt.uri)
			if got != tt.want {
				t.Errorf("extractPath(%v) = %v, want %v", tt.uri, got, tt.want)
			}
		})
	}
}

// TestGenerateRequestID tests request ID generation.
func TestGenerateRequestID(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	id1 := GenerateRequestID(t1)

	if id1 == "" {
		t.Error("GenerateRequestID() returned empty string")
	}

	// IDs should be unique
	t2 := t1.Add(time.Millisecond)
	id2 := GenerateRequestID(t2)

	if id1 == id2 {
		t.Error("GenerateRequestID() should generate unique IDs")
	}
}

// TestDefaultScenario tests the DefaultScenario function.
func TestDefaultScenario(t *testing.T) {
	s := DefaultScenario()

	if s.Name != "default" {
		t.Errorf("DefaultScenario().Name = %v, want 'default'", s.Name)
	}
	if s.Response.ICAPStatus != 204 {
		t.Errorf("DefaultScenario().Response.ICAPStatus = %v, want 204", s.Response.ICAPStatus)
	}
	if s.Priority != -1 {
		t.Errorf("DefaultScenario().Priority = %v, want -1", s.Priority)
	}
}
