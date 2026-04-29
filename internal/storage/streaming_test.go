// Copyright 2026 ICAP Mock

package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
      duration: 1ms
      finish:
        mode: complete
`)

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if registry.List()[0].Response.Stream == nil {
		t.Fatal("expected stream config")
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
