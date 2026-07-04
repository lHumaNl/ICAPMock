// Copyright 2026 ICAP Mock

package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioRegistry_LoadRejectsRemovedScriptFields(t *testing.T) {
	for _, tc := range removedScriptScenarioCases() {
		t.Run(tc.name, func(t *testing.T) {
			path := writeScenarioYAMLForRemovedKeyTest(t, tc.content)
			err := NewScenarioRegistry().Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want removed script field error")
			}
			assertRemovedScriptError(t, err, tc.wantPath)
		})
	}
}

type removedScriptScenarioCase struct {
	name     string
	content  string
	wantPath string
}

func removedScriptScenarioCases() []removedScriptScenarioCase {
	return []removedScriptScenarioCase{
		{name: "v1 scenario", content: v1ScenarioScriptYAML(), wantPath: "scenarios[0].script"},
		{name: "v1 response", content: v1ResponseScriptYAML(), wantPath: "scenarios[0].response.script"},
		{name: "v1 template", content: v1TemplateScriptYAML(), wantPath: "responses.block.script"},
		{name: "v2 scenario", content: v2ScenarioScriptYAML(), wantPath: "scenarios.block.script"},
		{name: "v2 weighted", content: v2WeightedScriptYAML(), wantPath: "scenarios.block.responses[0].script"},
		{name: "v2 branch", content: v2BranchScriptYAML(), wantPath: "scenarios.block.branches[0].script"},
		{name: "v2 branch weighted", content: v2BranchWeightedScriptYAML(), wantPath: "scenarios.block.branches[0].responses[0].script"},
		{name: "v2 inline template", content: v2InlineTemplateScriptYAML(), wantPath: "defaults.response_templates.block.script"},
		{name: "v2 weighted template", content: v2WeightedTemplateScriptYAML(), wantPath: "defaults.response_templates.block[0].script"},
	}
}

func assertRemovedScriptError(t *testing.T, err error, wantPath string) {
	t.Helper()
	message := err.Error()
	for _, want := range []string{removedScenarioScriptKey, wantPath} {
		if !strings.Contains(message, want) {
			t.Fatalf("Load() error = %v, want %q", err, want)
		}
	}
}

func writeScenarioYAMLForRemovedKeyTest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenarios.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write scenario file: %v", err)
	}
	return path
}

func v1ScenarioScriptYAML() string {
	return `scenarios:
  - name: block
    script: reject.js
    response:
      icap_status: 204
`
}

func v1ResponseScriptYAML() string {
	return `scenarios:
  - name: block
    response:
      script: reject.js
      icap_status: 204
`
}

func v1TemplateScriptYAML() string {
	return `responses:
  block:
    script: reject.js
    icap_status: 204
scenarios:
  - name: block
    response:
      use: block
`
}

func v2ScenarioScriptYAML() string {
	return v2ScriptYAML("script: reject.js")
}

func v2WeightedScriptYAML() string {
	return v2ScriptYAML("responses:\n      - weight: 1\n        script: reject.js")
}

func v2BranchScriptYAML() string {
	return v2ScriptYAML("branches:\n      - script: reject.js\n        status: 204")
}

func v2BranchWeightedScriptYAML() string {
	return v2ScriptYAML("branches:\n      - responses:\n          - weight: 1\n            script: reject.js")
}

func v2InlineTemplateScriptYAML() string {
	return `defaults:
  method: REQMOD
  endpoint: /scan
  response_templates:
    block:
      script: reject.js
      status: 204
scenarios:
  block:
    use: block
`
}

func v2WeightedTemplateScriptYAML() string {
	return `defaults:
  method: REQMOD
  endpoint: /scan
  response_templates:
    block:
      - weight: 1
        script: reject.js
scenarios:
  block:
    use: block
`
}

func v2ScriptYAML(scriptBlock string) string {
	return "defaults:\n  method: REQMOD\n  endpoint: /scan\nscenarios:\n  block:\n    " + scriptBlock + "\n"
}
