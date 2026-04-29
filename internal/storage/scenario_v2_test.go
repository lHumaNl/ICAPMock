// Copyright 2026 ICAP Mock

package storage

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// --- ParseDelay tests ---

func TestParseDelay_Static(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"500ms", 500 * time.Millisecond},
		{"1s", 1 * time.Second},
		{"2m", 2 * time.Minute},
		{"0s", 0},
		{"100ms", 100 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseDelay(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.IsRange {
				t.Errorf("expected IsRange=false, got true")
			}
			if got.Min != tc.want {
				t.Errorf("Min: got %v, want %v", got.Min, tc.want)
			}
			if got.Max != tc.want {
				t.Errorf("Max: got %v, want %v", got.Max, tc.want)
			}
		})
	}
}

func TestParseDelay_Range(t *testing.T) {
	cases := []struct {
		input string
		min   time.Duration
		max   time.Duration
	}{
		{"300ms-1500ms", 300 * time.Millisecond, 1500 * time.Millisecond},
		{"1s-5s", 1 * time.Second, 5 * time.Second},
		{"100ms-200ms", 100 * time.Millisecond, 200 * time.Millisecond},
		{"500ms-1s", 500 * time.Millisecond, 1 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseDelay(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.IsRange {
				t.Errorf("expected IsRange=true, got false")
			}
			if got.Min != tc.min {
				t.Errorf("Min: got %v, want %v", got.Min, tc.min)
			}
			if got.Max != tc.max {
				t.Errorf("Max: got %v, want %v", got.Max, tc.max)
			}
		})
	}
}

func TestParseDelay_Invalid(t *testing.T) {
	cases := []string{
		"abc",
		"",
		"  ",
		"-500ms",
		"ms",
		"1x",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDelay(input)
			if err == nil {
				t.Errorf("expected error for input %q, got nil", input)
			}
		})
	}
}

func TestParseDelay_RangeMinGreaterThanMax(t *testing.T) {
	_, err := ParseDelay("5s-1s")
	if err == nil {
		t.Error("expected error when min > max, got nil")
	}
}

// --- DelayConfig.Duration tests ---

func TestDelayConfig_Duration_Static(t *testing.T) {
	dc := DelayConfig{Min: 500 * time.Millisecond, Max: 500 * time.Millisecond, IsRange: false}
	for i := 0; i < 10; i++ {
		got := dc.Duration()
		if got != 500*time.Millisecond {
			t.Errorf("expected 500ms, got %v", got)
		}
	}
}

func TestDelayConfig_Duration_Range(t *testing.T) {
	minDelay := 100 * time.Millisecond
	maxDelay := 500 * time.Millisecond
	dc := DelayConfig{Min: minDelay, Max: maxDelay, IsRange: true}

	for i := 0; i < 100; i++ {
		got := dc.Duration()
		if got < minDelay || got > maxDelay {
			t.Errorf("Duration %v out of range [%v, %v]", got, minDelay, maxDelay)
		}
	}
}

func TestDelayConfig_Duration_RangeEqual(t *testing.T) {
	// When Min == Max for a "range", should return Min.
	dc := DelayConfig{Min: 200 * time.Millisecond, Max: 200 * time.Millisecond, IsRange: true}
	got := dc.Duration()
	if got != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", got)
	}
}

// --- ParseMatch tests ---

func TestParseMatch_Exact(t *testing.T) {
	cases := []string{"hello", "exact value", "synthetic-test-token", ""}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			mv, err := ParseMatch(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mv.IsRegex {
				t.Errorf("expected IsRegex=false")
			}
			if mv.Pattern != input {
				t.Errorf("Pattern: got %q, want %q", mv.Pattern, input)
			}
			if mv.Raw != input {
				t.Errorf("Raw: got %q, want %q", mv.Raw, input)
			}
		})
	}
}

func TestParseMatch_Regex(t *testing.T) {
	cases := []struct {
		input   string
		pattern string
	}{
		{"re:^hello.*", "^hello.*"},
		{"re:(?i)^synthetic\\.[a-z0-9.-]+$", "(?i)^synthetic\\.[a-z0-9.-]+$"},
		{"re:^[a-f0-9]{64}$", "^[a-f0-9]{64}$"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			mv, err := ParseMatch(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !mv.IsRegex {
				t.Errorf("expected IsRegex=true")
			}
			if mv.Pattern != tc.pattern {
				t.Errorf("Pattern: got %q, want %q", mv.Pattern, tc.pattern)
			}
			if mv.Raw != tc.input {
				t.Errorf("Raw: got %q, want %q", mv.Raw, tc.input)
			}
			if mv.compiled == nil {
				t.Errorf("expected compiled regex, got nil")
			}
		})
	}
}

func TestParseMatch_InvalidRegex(t *testing.T) {
	cases := []string{
		"re:[invalid",
		"re:(?P<bad",
		"re:*noquantifier",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseMatch(input)
			if err == nil {
				t.Errorf("expected error for invalid regex %q, got nil", input)
			}
		})
	}
}

// --- MatchValue.Matches tests ---

func TestMatchValue_Matches_Exact(t *testing.T) {
	mv, _ := ParseMatch("hello world")

	if !mv.Matches("hello world") {
		t.Error("expected exact match to succeed")
	}
	if mv.Matches("hello") {
		t.Error("expected partial match to fail")
	}
	if mv.Matches("Hello World") {
		t.Error("exact match should be case-sensitive")
	}
	if mv.Matches("") {
		t.Error("expected empty string not to match non-empty pattern")
	}
}

func TestMatchValue_Matches_EmptyPattern(t *testing.T) {
	mv, _ := ParseMatch("")
	if !mv.Matches("") {
		t.Error("empty pattern should match empty string")
	}
	if mv.Matches("anything") {
		t.Error("empty pattern should not match non-empty string")
	}
}

func TestMatchValue_Matches_Regex(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"re:^hello.*", "hello world", true},
		{"re:^hello.*", "world hello", false},
		{"re:(?i)^synthetic\\.[a-z0-9.-]+$", "Synthetic.sample-token", true},
		{"re:(?i)^synthetic\\.[a-z0-9.-]+$", "synthetic.test-case", true},
		{"re:(?i)^synthetic\\.[a-z0-9.-]+$", "safe.file.exe", false},
		{"re:^[a-f0-9]{64}$", strings.Repeat("0", 64), true},
		{"re:^[a-f0-9]{64}$", "short", false},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+"_"+tc.value, func(t *testing.T) {
			mv, err := ParseMatch(tc.pattern)
			if err != nil {
				t.Fatalf("ParseMatch error: %v", err)
			}
			got := mv.Matches(tc.value)
			if got != tc.want {
				t.Errorf("Matches(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestMatchValue_Matches_RegexCaseSensitive(t *testing.T) {
	mv, _ := ParseMatch("re:^Hello")
	if mv.Matches("hello") {
		t.Error("regex without (?i) should be case-sensitive")
	}
	if !mv.Matches("Hello world") {
		t.Error("regex should match when case matches")
	}
}

// --- ConvertV2ToScenarios tests ---

func TestConvertV2ToScenarios_Basic(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"RESPMOD"},
			Endpoint: EndpointList{"/scan-file"},
			Status:   204,
			Headers: map[string]string{
				"service": "Mock ICAP Server",
				"istag":   `"492710"`,
			},
		},
		Scenarios: map[string]ScenarioEntryV2{
			"scenario-a": {
				When: map[string]string{"X-Header": "value"},
				Set:  map[string]string{"x-custom": "custom-value"},
			},
		},
	}

	orderedNames := []string{"scenario-a"}
	scenarios, err := ConvertV2ToScenarios(file, orderedNames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(scenarios))
	}

	s := scenarios[0]
	if s.Name != "scenario-a" {
		t.Errorf("Name: got %q, want %q", s.Name, "scenario-a")
	}
	if len(s.Match.Methods) != 1 || s.Match.Methods[0] != "RESPMOD" {
		t.Errorf("Methods: got %v, want [RESPMOD]", s.Match.Methods)
	}
	if len(s.Match.Paths) == 0 || s.Match.Paths[0] != "/scan-file" {
		t.Errorf("Paths: got %v, want [/scan-file]", s.Match.Paths)
	}
	if s.Response.ICAPStatus != 204 {
		t.Errorf("ICAPStatus: got %d, want 204", s.Response.ICAPStatus)
	}
}

func TestConvertV2ToScenarios_HeaderMerge(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"REQMOD"},
			Endpoint: EndpointList{"/x"},
			Headers: map[string]string{
				"default-header": "default-value",
				"override-me":    "original",
			},
		},
		Scenarios: map[string]ScenarioEntryV2{
			"s1": {
				Set: map[string]string{
					"override-me": "overridden",
					"extra":       "extra-value",
				},
			},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"s1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := scenarios[0].Response.Headers
	if h["default-header"] != "default-value" {
		t.Errorf("default-header: got %q, want default-value", h["default-header"])
	}
	if h["override-me"] != "overridden" {
		t.Errorf("override-me: got %q, want overridden", h["override-me"])
	}
	if h["extra"] != "extra-value" {
		t.Errorf("extra: got %q, want extra-value", h["extra"])
	}
}

func TestConvertV2ToScenarios_PriorityAssignment(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"first":  {},
			"second": {},
			"third":  {},
		},
	}

	orderedNames := []string{"first", "second", "third"}
	scenarios, err := ConvertV2ToScenarios(file, orderedNames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]*Scenario)
	for _, s := range scenarios {
		byName[s.Name] = s
	}

	if byName["first"].Priority != 1000 {
		t.Errorf("first priority: got %d, want 1000", byName["first"].Priority)
	}
	if byName["second"].Priority != 999 {
		t.Errorf("second priority: got %d, want 999", byName["second"].Priority)
	}
	if byName["third"].Priority != 998 {
		t.Errorf("third priority: got %d, want 998", byName["third"].Priority)
	}
}

func TestConvertV2ToScenarios_ExplicitPriority(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"high": {Priority: 9999},
			"low":  {Priority: 1},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"high", "low"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]*Scenario)
	for _, s := range scenarios {
		byName[s.Name] = s
	}

	if byName["high"].Priority != 9999 {
		t.Errorf("high priority: got %d, want 9999", byName["high"].Priority)
	}
	if byName["low"].Priority != 1 {
		t.Errorf("low priority: got %d, want 1", byName["low"].Priority)
	}
}

func TestConvertV2ToScenarios_EntryOverridesDefaults(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"RESPMOD"},
			Endpoint: EndpointList{"/default-endpoint"},
			Status:   204,
		},
		Scenarios: map[string]ScenarioEntryV2{
			"override": {
				Method:   MethodList{"REQMOD"},
				Endpoint: EndpointList{"/custom-endpoint"},
				Status:   200,
			},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"override"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := scenarios[0]
	if len(s.Match.Methods) != 1 || s.Match.Methods[0] != "REQMOD" {
		t.Errorf("Methods: got %v, want [REQMOD]", s.Match.Methods)
	}
	if len(s.Match.Paths) == 0 || s.Match.Paths[0] != "/custom-endpoint" {
		t.Errorf("Paths: got %v, want [/custom-endpoint]", s.Match.Paths)
	}
	if s.Response.ICAPStatus != 200 {
		t.Errorf("ICAPStatus: got %d, want 200", s.Response.ICAPStatus)
	}
}

func TestConvertV2ToScenarios_WhenHeaders(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"with-when": {
				When: map[string]string{
					"X-Filename": "synthetic-block.exe",
					"X-Other":    "re:^value.*",
				},
			},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"with-when"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := scenarios[0]
	if s.Match.Headers["X-Filename"] != "synthetic-block.exe" {
		t.Errorf("X-Filename header: got %q, want synthetic-block.exe", s.Match.Headers["X-Filename"])
	}
	if s.Match.Headers["X-Other"] != "re:^value.*" {
		t.Errorf("X-Other header: got %q, want re:^value.*", s.Match.Headers["X-Other"])
	}
}

func TestConvertV2ToScenarios_Delay(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"with-delay": {Delay: "500ms"},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"with-delay"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scenarios[0].Response.Delay != 500*time.Millisecond {
		t.Errorf("Delay: got %v, want 500ms", scenarios[0].Response.Delay)
	}
}

func TestConvertV2ToScenarios_InvalidDelay(t *testing.T) {
	file := &ScenarioFileV2{
		Scenarios: map[string]ScenarioEntryV2{
			"bad-delay": {Delay: "notavalidduration"},
		},
	}

	_, err := ConvertV2ToScenarios(file, []string{"bad-delay"})
	if err == nil {
		t.Error("expected error for invalid delay, got nil")
	}
}

func TestConvertV2ToScenarios_NilFile(t *testing.T) {
	_, err := ConvertV2ToScenarios(nil, []string{"any"})
	if err == nil {
		t.Error("expected error for nil file, got nil")
	}
}

func TestConvertV2ToScenarios_EmptySet_NoDefaultHeaders(t *testing.T) {
	// When both defaults.headers and set are empty, Headers should be nil.
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"no-headers": {},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"no-headers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scenarios[0].Response.Headers != nil {
		t.Errorf("expected nil headers, got %v", scenarios[0].Response.Headers)
	}
}

func TestConvertV2ToScenarios_BodyAndBodyFile(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"with-body": {
				Body:     "response body text",
				BodyFile: "./some/file.html",
			},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"with-body"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := scenarios[0]
	if s.Response.Body != "response body text" {
		t.Errorf("Body: got %q", s.Response.Body)
	}
	if s.Response.BodyFile != "./some/file.html" {
		t.Errorf("BodyFile: got %q", s.Response.BodyFile)
	}
}

func TestConvertV2ToScenarios_DefaultStatusFallback(t *testing.T) {
	// No status in defaults or entry — should default to 204.
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"no-status": {},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"no-status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scenarios[0].Response.ICAPStatus != 204 {
		t.Errorf("ICAPStatus: got %d, want 204", scenarios[0].Response.ICAPStatus)
	}
}

func TestConvertV2ToScenarios_UnknownNameSkipped(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"real": {},
		},
	}

	// orderedNames includes a name not in map — should be silently skipped.
	scenarios, err := ConvertV2ToScenarios(file, []string{"real", "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scenarios) != 1 {
		t.Errorf("expected 1 scenario, got %d", len(scenarios))
	}
}

// --- when_http / HTTP match tests ---

func TestConvertV2ToScenarios_WhenHTTP(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/av/reqmod"}},
		Scenarios: map[string]ScenarioEntryV2{
			"http-match": {
				WhenHTTP: &WhenHTTPV2{
					Headers: map[string]string{
						"Content-Type": "re:(?i)application/x-dosexec",
					},
					URL:    "re:(?i)\\.(exe|dll)$",
					Method: "GET",
				},
			},
		},
	}

	scenarios, err := ConvertV2ToScenarios(file, []string{"http-match"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := scenarios[0]
	if s.Match.HTTPHeaders["Content-Type"] != "re:(?i)application/x-dosexec" {
		t.Errorf("HTTPHeaders: got %q", s.Match.HTTPHeaders["Content-Type"])
	}
	if s.Match.HTTPURL != "re:(?i)\\.(exe|dll)$" {
		t.Errorf("HTTPURL: got %q", s.Match.HTTPURL)
	}
	if s.Match.HTTPMethod != "GET" {
		t.Errorf("HTTPMethod: got %q", s.Match.HTTPMethod)
	}
}

func TestConvertV2ToScenarios_MissingMethodOrEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		defaults ScenarioDefaultsV2
		entry    ScenarioEntryV2
		want     string
	}{
		{
			name:  "no method",
			entry: ScenarioEntryV2{Endpoint: EndpointList{"/x"}},
			want:  "method is not set",
		},
		{
			name:  "no endpoint",
			entry: ScenarioEntryV2{Method: MethodList{"REQMOD"}},
			want:  "endpoint is not set",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := &ScenarioFileV2{
				Defaults:  tc.defaults,
				Scenarios: map[string]ScenarioEntryV2{"s": tc.entry},
			}
			_, err := ConvertV2ToScenarios(file, []string{"s"})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !containsString(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- MethodList tests ---

func TestMethodList_UnmarshalYAML_Scalar(t *testing.T) {
	var out struct {
		M MethodList `yaml:"m"`
	}
	if err := yaml.Unmarshal([]byte("m: REQMOD\n"), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.M) != 1 || out.M[0] != "REQMOD" {
		t.Errorf("got %v, want [REQMOD]", out.M)
	}
}

func TestMethodList_UnmarshalYAML_Sequence(t *testing.T) {
	var out struct {
		M MethodList `yaml:"m"`
	}
	if err := yaml.Unmarshal([]byte("m: [REQMOD, RESPMOD]\n"), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.M) != 2 || out.M[0] != "REQMOD" || out.M[1] != "RESPMOD" {
		t.Errorf("got %v, want [REQMOD RESPMOD]", out.M)
	}
}

func TestMethodList_UnmarshalYAML_Empty(t *testing.T) {
	var out struct {
		M MethodList `yaml:"m"`
	}
	if err := yaml.Unmarshal([]byte("m:\n"), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.M != nil && len(out.M) != 0 {
		t.Errorf("got %v, want empty", out.M)
	}
}

func TestConvertV2_MultipleMethods(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"REQMOD", "RESPMOD"},
			Endpoint: EndpointList{"/scan"},
		},
		Scenarios: map[string]ScenarioEntryV2{"s": {}},
	}
	scenarios, err := ConvertV2ToScenarios(file, []string{"s"})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(scenarios[0].Match.Methods) != 2 {
		t.Errorf("Methods: got %v, want 2 items", scenarios[0].Match.Methods)
	}
}

func TestConvertV2_InvalidMethod(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"WRONG"},
			Endpoint: EndpointList{"/x"},
		},
		Scenarios: map[string]ScenarioEntryV2{"s": {}},
	}
	_, err := ConvertV2ToScenarios(file, []string{"s"})
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
	if !containsString(err.Error(), "invalid ICAP method") {
		t.Errorf("error %q does not mention invalid method", err.Error())
	}
}

// --- Endpoint list + captures ---

func TestEndpointList_UnmarshalYAML(t *testing.T) {
	cases := []struct {
		in   string
		want EndpointList
	}{
		{"e: /scan\n", EndpointList{"/scan"}},
		{"e: [/a, \"/b/{id}\"]\n", EndpointList{"/a", "/b/{id}"}},
		{"e:\n", nil},
	}
	for _, tc := range cases {
		var out struct {
			E EndpointList `yaml:"e"`
		}
		if err := yaml.Unmarshal([]byte(tc.in), &out); err != nil {
			t.Fatalf("unmarshal %q: %v", tc.in, err)
		}
		if len(out.E) != len(tc.want) {
			t.Errorf("%q: got %v, want %v", tc.in, out.E, tc.want)
			continue
		}
		for i := range tc.want {
			if out.E[i] != tc.want[i] {
				t.Errorf("%q[%d]: got %q, want %q", tc.in, i, out.E[i], tc.want[i])
			}
		}
	}
}

func TestCompileEndpoint_Captures(t *testing.T) {
	ce, err := compileEndpoint("/env/{id}/ok")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(ce.captures) != 1 || ce.captures[0] != "id" {
		t.Errorf("captures: got %v, want [id]", ce.captures)
	}
	m := ce.re.FindStringSubmatch("/env/abc-123/ok")
	if m == nil {
		t.Fatal("expected match for /env/abc-123/ok")
	}
	if ce.re.FindStringSubmatch("/env/abc/def/ok") != nil {
		t.Error("should not match when extra path segment is present")
	}
}

func TestMatchEndpoint_WithCaptures(t *testing.T) {
	p, err := compileEndpoint("/env/{env}/queue/{qid}")
	if err != nil {
		t.Fatal(err)
	}
	caps, ok := matchEndpoint([]compiledEndpoint{p}, "/env/prod/queue/42?x=1")
	if !ok {
		t.Fatal("expected match with stripped query")
	}
	if caps["env"] != "prod" || caps["qid"] != "42" {
		t.Errorf("captures: %v", caps)
	}
}

// --- Branches + templates + use ---

func TestConvertV2_BranchesWithTemplates(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"REQMOD"},
			Endpoint: EndpointList{"/scan"},
			ResponseTemplates: map[string]ResponseTemplateV2{
				"clean":   {Inline: &InlineResponseV2{Status: 204}},
				"blocked": {Inline: &InlineResponseV2{Status: 200, HTTPStatus: 403, Body: "<html>blocked</html>"}},
			},
			Use: "clean",
		},
		Scenarios: map[string]ScenarioEntryV2{
			"s": {
				Branches: []BranchV2{
					{When: map[string]string{"X-Tag": "bad"}, Use: "blocked"},
					{Use: "clean"},
				},
			},
		},
	}
	scenarios, err := ConvertV2ToScenarios(file, []string{"s"})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	s := scenarios[0]
	if len(s.Branches) != 2 {
		t.Fatalf("branches: got %d, want 2", len(s.Branches))
	}
	if s.Branches[0].Response.ICAPStatus != 200 || s.Branches[0].Response.HTTPStatus != 403 {
		t.Errorf("branch[0] resp: %+v", s.Branches[0].Response)
	}
	if s.Branches[1].Response.ICAPStatus != 204 {
		t.Errorf("branch[1] resp: %+v", s.Branches[1].Response)
	}
}

func TestConvertV2_BranchesRejectInlineOnSameLevel(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"bad": {
				Status:   200,
				Branches: []BranchV2{{Use: "does-not-matter"}},
			},
		},
	}
	_, err := ConvertV2ToScenarios(file, []string{"bad"})
	if err == nil {
		t.Fatal("expected error for mixing inline response with branches")
	}
	if !containsString(err.Error(), "branches cannot be combined") {
		t.Errorf("error: %v", err)
	}
}

func TestConvertV2_UnknownTemplateRef(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Method: MethodList{"REQMOD"}, Endpoint: EndpointList{"/x"}},
		Scenarios: map[string]ScenarioEntryV2{
			"s": {Use: "missing"},
		},
	}
	_, err := ConvertV2ToScenarios(file, []string{"s"})
	if err == nil {
		t.Fatal("expected error for unknown template reference")
	}
	if !containsString(err.Error(), "missing") {
		t.Errorf("error should name the missing template: %v", err)
	}
}

func TestConvertV2_DefaultsUseUnknown(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{
			Method:   MethodList{"REQMOD"},
			Endpoint: EndpointList{"/x"},
			Use:      "nope",
		},
		Scenarios: map[string]ScenarioEntryV2{"s": {}},
	}
	_, err := ConvertV2ToScenarios(file, []string{"s"})
	if err == nil {
		t.Fatal("expected error for unknown defaults.use")
	}
}
