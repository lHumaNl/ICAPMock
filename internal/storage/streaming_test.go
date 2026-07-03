// Copyright 2026 ICAP Mock

package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScenarioRegistry_Load_StreamUseTemplate(t *testing.T) {
	scenarioFile := writeScenarioFile(t, `
responses:
  slow_complete:
    status: 200
    stream:
      source:
        from: body
        body: "abcd"
      chunks:
        size: 2
        delay: 1ms-2ms
      finish:
        mode: complete
scenarios:
  - name: stream-template
    match:
      path_pattern: ^/scan
    response:
      use: slow_complete
`)

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	scenario := registry.List()[0]
	if scenario.Response.Stream == nil {
		t.Fatal("expected stream config")
	}
	if scenario.Response.ICAPStatus != 200 {
		t.Fatalf("ICAPStatus = %d, want 200", scenario.Response.ICAPStatus)
	}
}

func TestScenarioRegistry_Load_StreamInlineV2(t *testing.T) {
	scenarioFile := writeScenarioFile(t, `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  inline-stream:
    status: 200
    stream:
      source:
        from: request_body
      chunks:
        size: 1
      start_delay: 2ms-4ms
      duration: 1ms
      finish:
        mode: complete
`)

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stream := registry.List()[0].Response.Stream
	if stream == nil {
		t.Fatal("expected stream config")
	}
	if stream.StartDelay.Min != 2*time.Millisecond || stream.StartDelay.Max != 4*time.Millisecond {
		t.Fatalf("StartDelay = %v-%v, want 2ms-4ms", stream.StartDelay.Min, stream.StartDelay.Max)
	}
}

func TestScenarioRegistry_Load_NewStreamControls(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"complete", streamYAML(newCompleteStreamControls())},
		{"fin-percent", streamYAML(newFINStreamControls("40%"))},
		{"fin-percent-range", streamYAML(newFINStreamControls("30%-60%"))},
		{"term-percent", streamYAML(newTermStreamControls("40%"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewScenarioRegistry()
			if err := registry.Load(writeScenarioFile(t, tt.yaml)); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestScenarioRegistry_Add_NormalizedNewStreamControls(t *testing.T) {
	loaded := NewScenarioRegistry()
	if err := loaded.Load(writeScenarioFile(t, streamYAML(newFINStreamControls("40%")))); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	dst := NewScenarioRegistry()
	if err := dst.Add(firstNonDefaultScenario(t, loaded.List())); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestScenarioRegistry_Add_NormalizedStreamParts(t *testing.T) {
	loaded := NewScenarioRegistry()
	if err := loaded.Load(writeScenarioFile(t, streamTopLevelPartsYAML(t))); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	dst := NewScenarioRegistry()
	if err := dst.Add(firstNonDefaultScenario(t, loaded.List())); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestScenarioRegistry_Load_InvalidNewStreamControls(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"fin-no-percent", streamYAML("end:\n        mode: fin\n      throttle:\n        every: 1ms")},
		{"term-no-percent", streamYAML("end:\n        mode: term\n      throttle:\n        every: 1ms")},
		{"fin-no-pacing", streamYAML("send:\n        percent: 40\n      end:\n        mode: fin")},
		{"term-no-pacing", streamYAML("send:\n        percent: 40\n      end:\n        mode: term")},
		{"fin-zero-percent", streamYAML("send:\n        percent: 0\n        duration: 1ms\n      end:\n        mode: fin")},
		{"term-full-percent", streamYAML(newTermStreamControls("100%"))},
		{"fin-full-percent", streamYAML(newFINStreamControls("100%"))},
		{"complete-percent", streamYAML("send:\n        percent: 40\n      end:\n        mode: complete")},
		{"end-finish-conflict", streamYAML("end:\n        mode: fin\n      finish:\n        mode: fin")},
		{"send-duration-conflict", streamYAML("send:\n        duration: 1ms\n      duration: 1ms")},
		{"send-finish-conflict", streamYAML("send:\n        duration: 1ms\n      finish:\n        mode: fin")},
		{"throttle-finish-conflict", streamYAML("throttle:\n        every: 1ms\n      finish:\n        mode: fin")},
		{"throttle-size-conflict", streamYAML("throttle:\n        chunk_size: 4\n      chunks:\n        size: 2")},
		{"throttle-every-conflict", streamYAML("throttle:\n        every: 1ms\n      chunks:\n        delay: 1ms")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewScenarioRegistry()
			if err := registry.Load(writeScenarioFile(t, tt.yaml)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestScenarioRegistry_Load_StreamCanonicalHTTPBodySources(t *testing.T) {
	tests := []struct {
		name   string
		method string
		source string
	}{
		{"request", "REQMOD", "request_http_body"},
		{"response", "RESPMOD", "response_http_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewScenarioRegistry()
			err := registry.Load(writeScenarioFile(t, streamYAMLTopLevelForMethod(tt.method, "from: "+tt.source)))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestScenarioRegistry_Load_StreamTopLevelAndParts(t *testing.T) {
	registry := NewScenarioRegistry()
	if err := registry.Load(writeScenarioFile(t, streamTopLevelPartsYAML(t))); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stream := registry.List()[0].Response.Stream
	if got := len(stream.Parts); got != 4 {
		t.Fatalf("parts count = %d, want 4", got)
	}
	if stream.Parts[1].From != "body" || stream.Parts[2].From != "body_file" {
		t.Fatalf("parts were not normalized: %+v", stream.Parts)
	}
}

func TestScenarioRegistry_Load_InvalidStreamConfigs(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"delay-with-duration", streamYAML("chunks:\n        delay: 1ms\n      duration: 1ms")},
		{"zero-start-delay", streamYAML("start_delay: 0s")},
		{"zero-chunk", streamYAML("chunks:\n        size: 0")},
		{"bad-finish", streamYAML("finish:\n        mode: reset")},
		{"bad-weight", streamYAML("finish:\n        mode: weighted\n        complete_percent: 70\n        fin_percent: 20")},
		{"missing-weight", streamYAML("finish:\n        mode: weighted")},
		{"weighted-fin-without-config", streamYAML("finish:\n        mode: weighted\n        complete_percent: 50\n        fin_percent: 50")},
		{"bad-percent", streamYAML("finish:\n        mode: complete\n        complete_percent: 101")},
		{"bad-source", streamYAMLWithBody("source:\n        from: sequence")},
		{"request-body-no-method", streamYAMLWithoutMethod("from: request_body")},
		{"response-body-no-method", streamYAMLWithoutMethod("from: response_body")},
		{
			"request-body-wildcard-icap-method",
			streamYAMLWithICAPMethod(`"*"`, "from: request_body"),
		},
		{
			"response-body-wildcard-icap-method",
			streamYAMLWithICAPMethod(`"*"`, "from: response_body"),
		},
		{"bad-fin-close", streamYAML("finish:\n        mode: fin\n        fin:\n          close: reset")},
		{"response-body-reqmod", streamYAMLForMethod("REQMOD", "from: response_body", "")},
		{"request-body-respmod", streamYAMLForMethod("RESPMOD", "from: request_body", "")},
		{"request-body-mixed-methods", streamYAMLForMethod("[REQMOD, RESPMOD]", "from: request_body", "")},
		{"response-body-mixed-methods", streamYAMLForMethod("[REQMOD, RESPMOD]", "from: response_body", "")},
		{"request-body-options", streamYAMLForMethod("OPTIONS", "from: request_body", "")},
		{"response-body-options", streamYAMLForMethod("OPTIONS", "from: response_body", "")},
		{"body-missing", streamYAMLForMethod("REQMOD", "from: body", "")},
		{"body-with-body-file", streamYAMLForMethod("REQMOD", "from: body\nbody: data\nbody_file: /unused", "")},
		{"body-file-missing", streamYAMLForMethod("REQMOD", "from: body_file", "")},
		{"body-file-with-body", streamYAMLForMethod("REQMOD", "from: body_file\nbody: data\nbody_file: /unused", "")},
		{"request-body-with-body", streamYAMLForMethod("REQMOD", "from: request_body\nbody: data", "")},
		{"response-body-with-body-file", streamYAMLForMethod("RESPMOD", "from: response_body\nbody_file: /unused", "")},
		{"complete-with-fin", streamYAML("finish:\n        mode: complete\n        fin:\n          close: clean")},
		{"complete-with-fin-after", streamYAML("finish:\n        mode: complete\n        fin:\n          after:\n            bytes: 1")},
		{"complete-with-percent", streamYAML("finish:\n        mode: complete\n        complete_percent: 100")},
		{"stream-http-body", streamYAMLForMethod("REQMOD", "from: body\nbody: data", "http_body: blocked")},
		{"stream-http-body-file", streamYAMLForMethod("REQMOD", "from: body\nbody: data", "http_body_file: /unused")},
		{"multipart-non-http", streamYAMLTopLevelForMethod("REQMOD", "body: data\nmultipart:\n  files: true")},
		{"multipart-legacy-alias", streamYAMLTopLevelForMethod("REQMOD", "from: request_body\nmultipart:\n  files: true")},
		{"multipart-bad-regex", streamYAMLTopLevelForMethod("REQMOD", "from: request_http_body\nmultipart:\n  files:\n    filename: '['")},
		{
			"fallback-raw-bad-regex",
			streamYAMLTopLevelForMethod("REQMOD", "from: request_http_body\nmultipart:\n  files: true\nfallback:\n  raw_file:\n    filename: '['"),
		},
		{
			"fallback-body-file-missing",
			streamYAMLTopLevelForMethod("REQMOD", "from: request_http_body\nmultipart:\n  files: true\nfallback:\n  body_file: /missing/fallback/body"),
		},
		{
			"fallback-bad-from",
			streamYAMLTopLevelForMethod("REQMOD", "from: request_http_body\nmultipart:\n  files: true\nfallback:\n  from: body"),
		},
		{"from-with-parts", streamYAMLTopLevelForMethod("REQMOD", "from: request_http_body\nparts:\n  - body: data")},
		{"empty-parts", streamYAMLTopLevelForMethod("REQMOD", "parts: []")},
	}
	wantErrs := map[string]string{
		"request-body-wildcard-icap-method":  "source.request_body requires an explicit REQMOD scenario method",
		"response-body-wildcard-icap-method": "source.response_body requires an explicit RESPMOD scenario method",
		"fallback-raw-bad-regex":             "fallback.raw_file.filename",
		"fallback-body-file-missing":         "/missing/fallback/body",
		"fallback-bad-from":                  "unsupported fallback.from",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewScenarioRegistry()
			err := registry.Load(writeScenarioFile(t, tt.yaml))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if wantErr := wantErrs[tt.name]; wantErr != "" && !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("Load() error = %v, want fragment %q", err, wantErr)
			}
		})
	}
}

func streamTopLevelPartsYAML(t *testing.T) string {
	t.Helper()
	bodyFile := filepath.Join(t.TempDir(), "footer.bin")
	if err := os.WriteFile(bodyFile, []byte("file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return "defaults:\n  method: REQMOD\n  endpoint: /scan\nscenarios:\n  s:\n    status: 200\n    stream:\n      parts:\n        - from: request_body\n        - body: marker\n        - body_file: " + bodyFile + "\n        - from: request_http_body\n"
}

func streamYAML(fragment string) string {
	return "defaults:\n  method: REQMOD\n  endpoint: /scan\nscenarios:\n  s:\n    status: 200\n    stream:\n      source:\n        from: body\n        body: data\n      " + fragment + "\n"
}

func newCompleteStreamControls() string {
	return "throttle:\n        chunk_size: 2\n        every: 1ms\n      send:\n        duration: 5ms\n      end:\n        mode: complete"
}

func newFINStreamControls(percent string) string {
	return "send:\n        percent: \"" + percent + "\"\n        duration: 5ms\n      throttle:\n        chunk_size: 2\n      end:\n        mode: fin"
}

func newTermStreamControls(percent string) string {
	return "send:\n        percent: \"" + percent + "\"\n        duration: 5ms\n      throttle:\n        chunk_size: 2\n      end:\n        mode: term"
}

func streamYAMLWithBody(body string) string {
	return "defaults:\n  method: REQMOD\n  endpoint: /scan\nscenarios:\n  s:\n    status: 200\n    stream:\n      " + body + "\n"
}

func streamYAMLWithoutMethod(sourceFields string) string {
	source := strings.ReplaceAll(strings.TrimSpace(sourceFields), "\n", "\n          ")
	return "scenarios:\n  - name: s\n    match:\n      path_pattern: ^/scan\n    response:\n      status: 200\n      stream:\n        source:\n          " + source + "\n"
}

func streamYAMLWithICAPMethod(method, sourceFields string) string {
	source := strings.ReplaceAll(strings.TrimSpace(sourceFields), "\n", "\n          ")
	return "scenarios:\n  - name: s\n    match:\n      path_pattern: ^/scan\n      icap_method: " + method + "\n    response:\n      status: 200\n      stream:\n        source:\n          " + source + "\n"
}

func streamYAMLForMethod(method, sourceFields, responseFields string) string {
	source := strings.ReplaceAll(strings.TrimSpace(sourceFields), "\n", "\n        ")
	response := indentOptional(responseFields, "\n    ")
	return "defaults:\n  method: " + method + "\n  endpoint: /scan\nscenarios:\n  s:\n    status: 200" + response + "\n    stream:\n      source:\n        " + source + "\n"
}

func streamYAMLTopLevelForMethod(method, streamFields string) string {
	fields := strings.ReplaceAll(strings.TrimSpace(streamFields), "\n", "\n      ")
	return "defaults:\n  method: " + method + "\n  endpoint: /scan\nscenarios:\n  s:\n    status: 200\n    stream:\n      " + fields + "\n"
}

func indentOptional(raw, prefix string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return prefix + strings.ReplaceAll(trimmed, "\n", prefix)
}

func writeScenarioFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenarios.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func firstNonDefaultScenario(t *testing.T, scenarios []*Scenario) *Scenario {
	t.Helper()
	for _, scenario := range scenarios {
		if scenario.Name != defaultScenarioName {
			return scenario
		}
	}
	t.Fatal("no non-default scenarios")
	return nil
}
