// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestRoutesYAMLAcceptsScalarAndListEndpoints(t *testing.T) {
	var file ScenarioFileV2
	err := yaml.Unmarshal([]byte(`
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD: [/av/respmod, /av/scanfile]
scenarios:
  clean: {}
`), &file)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	want := RouteMap{
		icap.MethodREQMOD:  {"/av/reqmod"},
		icap.MethodRESPMOD: {"/av/respmod", "/av/scanfile"},
	}
	if !reflect.DeepEqual(file.Defaults.Routes, want) {
		t.Fatalf("defaults.routes = %#v, want %#v", file.Defaults.Routes, want)
	}
}

func TestRoutesYAMLRejectsInvalidEndpointTypesAndMembers(t *testing.T) {
	tests := []string{
		"defaults:\n  routes: null\n",
		"defaults:\n  routes: {REQMOD: 123}\n",
		"defaults:\n  routes: {REQMOD: true}\n",
		"defaults:\n  routes: {REQMOD: [/ok, null]}\n",
		"defaults:\n  routes: {REQMOD: []}\n",
		"defaults:\n  routes: {REQMOD: ''}\n",
	}
	for _, input := range tests {
		var file ScenarioFileV2
		err := yaml.Unmarshal([]byte(input), &file)
		if err == nil {
			_, err = ConvertV2ToScenarios(&file, nil)
		}
		if err == nil {
			t.Errorf("invalid routes %q were accepted", input)
		}
	}
}

func TestConvertV2RoutesResolution(t *testing.T) {
	defaultRoutes := RouteMap{
		icap.MethodREQMOD:  {"/av/reqmod"},
		icap.MethodRESPMOD: {"/av/respmod", "/av/scanfile"},
	}
	tests := []struct {
		name        string
		entry       ScenarioEntryV2
		wantRoutes  RouteMap
		wantMethods MethodList
		wantPaths   EndpointList
	}{
		{
			name:       "inherit all default routes",
			wantRoutes: defaultRoutes,
		},
		{
			name: "scenario routes replace defaults",
			entry: ScenarioEntryV2{Routes: RouteMap{
				icap.MethodRESPMOD: {"/av/special"},
			}},
			wantRoutes: RouteMap{icap.MethodRESPMOD: {"/av/special"}},
		},
		{
			name:       "method only selects inherited endpoints",
			entry:      ScenarioEntryV2{Method: MethodList{icap.MethodREQMOD}},
			wantRoutes: RouteMap{icap.MethodREQMOD: {"/av/reqmod"}},
		},
		{
			name:  "endpoint only replaces endpoints for all methods",
			entry: ScenarioEntryV2{Endpoint: EndpointList{"/av/custom"}},
			wantRoutes: RouteMap{
				icap.MethodREQMOD:  {"/av/custom"},
				icap.MethodRESPMOD: {"/av/custom"},
			},
		},
		{
			name: "explicit legacy pair replaces routes",
			entry: ScenarioEntryV2{
				Method:   MethodList{icap.MethodREQMOD, icap.MethodRESPMOD},
				Endpoint: EndpointList{"/av/custom"},
			},
			wantMethods: MethodList{icap.MethodREQMOD, icap.MethodRESPMOD},
			wantPaths:   EndpointList{"/av/custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &ScenarioFileV2{
				Defaults:  ScenarioDefaultsV2{Routes: cloneRouteMap(defaultRoutes)},
				Scenarios: map[string]ScenarioEntryV2{"test": tt.entry},
			}
			scenarios, err := ConvertV2ToScenarios(file, []string{"test"})
			if err != nil {
				t.Fatalf("ConvertV2ToScenarios() error = %v", err)
			}
			if len(scenarios) != 1 {
				t.Fatalf("scenario count = %d, want 1", len(scenarios))
			}
			match := scenarios[0].Match
			if !reflect.DeepEqual(match.Routes, tt.wantRoutes) {
				t.Errorf("Routes = %#v, want %#v", match.Routes, tt.wantRoutes)
			}
			if tt.wantRoutes != nil {
				return
			}
			if !reflect.DeepEqual(match.Methods, tt.wantMethods) {
				t.Errorf("Methods = %#v, want %#v", match.Methods, tt.wantMethods)
			}
			if !reflect.DeepEqual(match.Paths, []string(tt.wantPaths)) {
				t.Errorf("Paths = %#v, want %#v", match.Paths, tt.wantPaths)
			}
		})
	}
}

func TestConvertV2RoutesRejectsConflictsAndInvalidSelections(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "defaults routes with method key",
			yaml: `
defaults:
  routes: {REQMOD: /req}
  method:
scenarios:
  test: {}
`,
			wantErr: "defaults.routes cannot be combined",
		},
		{
			name: "scenario routes with endpoint",
			yaml: `
defaults:
  routes: {REQMOD: /req}
scenarios:
  test:
    routes: {RESPMOD: /resp}
    endpoint: null
`,
			wantErr: `scenario "test": routes cannot be combined`,
		},
		{
			name: "empty routes",
			yaml: `
defaults:
  routes: {}
scenarios:
  test: {}
`,
			wantErr: "defaults.routes must not be empty",
		},
		{
			name: "options route",
			yaml: `
defaults:
  routes: {OPTIONS: /options}
scenarios:
  test: {}
`,
			wantErr: `invalid route method "OPTIONS"`,
		},
		{
			name: "case wrong method",
			yaml: `
defaults:
  routes: {reqmod: /req}
scenarios:
  test: {}
`,
			wantErr: `invalid route method "reqmod"`,
		},
		{
			name: "duplicate endpoint",
			yaml: `
defaults:
  routes: {REQMOD: [/req, /req]}
scenarios:
  test: {}
`,
			wantErr: `duplicate endpoint "/req"`,
		},
		{
			name: "method selection missing from defaults routes",
			yaml: `
defaults:
  routes: {REQMOD: /req}
scenarios:
  test:
    method: RESPMOD
`,
			wantErr: `method "RESPMOD" is not present in defaults.routes`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var file ScenarioFileV2
			if err := yaml.Unmarshal([]byte(tt.yaml), &file); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			_, err := ConvertV2ToScenarios(&file, []string{"test"})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ConvertV2ToScenarios() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRoutesAllowSameEndpointForDifferentMethods(t *testing.T) {
	file := &ScenarioFileV2{
		Defaults: ScenarioDefaultsV2{Routes: RouteMap{
			icap.MethodREQMOD:  {"/shared"},
			icap.MethodRESPMOD: {"/shared"},
		}},
		Scenarios: map[string]ScenarioEntryV2{"shared": {}},
	}
	scenarios, err := ConvertV2ToScenarios(file, []string{"shared"})
	if err != nil {
		t.Fatalf("ConvertV2ToScenarios() error = %v", err)
	}
	if got := scenarios[0].Match.Paths; !reflect.DeepEqual(got, []string{"/shared"}) {
		t.Fatalf("flattened Paths = %v, want [/shared]", got)
	}
}

func TestExactRoutesMatchOnlyDeclaredMethodEndpointPairs(t *testing.T) {
	scenario := &Scenario{
		Name: "exact",
		Match: MatchRule{Routes: RouteMap{
			icap.MethodREQMOD:  {"/req"},
			icap.MethodRESPMOD: {"/resp"},
		}},
	}
	registry := &scenarioRegistry{bodyPattern: DefaultBodyPatternOptions()}
	if err := registry.validateAndCompile(scenario); err != nil {
		t.Fatalf("validateAndCompile() error = %v", err)
	}

	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{icap.MethodREQMOD, "/req", true},
		{icap.MethodREQMOD, "/resp", false},
		{icap.MethodRESPMOD, "/req", false},
		{icap.MethodRESPMOD, "/resp", true},
	}
	for _, tt := range tests {
		req, err := icap.NewRequest(tt.method, "icap://localhost"+tt.path)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		got, err := matchesScenario(context.Background(), scenario, req, DefaultBodyPatternOptions())
		if err != nil {
			t.Fatalf("matchesScenario() error = %v", err)
		}
		if got != tt.want {
			t.Errorf("matchesScenario(%s, %s) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestExactRoutesCaptureOnlyFromCurrentMethodEndpoint(t *testing.T) {
	scenario := &Scenario{
		Name: "captures",
		Match: MatchRule{Routes: RouteMap{
			icap.MethodREQMOD:  {"/req/{request_id}"},
			icap.MethodRESPMOD: {"/resp/{response_id}"},
		}},
	}
	registry := &scenarioRegistry{bodyPattern: DefaultBodyPatternOptions()}
	if err := registry.validateAndCompile(scenario); err != nil {
		t.Fatalf("validateAndCompile() error = %v", err)
	}
	for _, tc := range []struct {
		method      string
		path        string
		want        bool
		wantCapture string
	}{
		{icap.MethodREQMOD, "/req/42", true, "request_id"},
		{icap.MethodREQMOD, "/resp/42", false, ""},
		{icap.MethodRESPMOD, "/req/42", false, ""},
		{icap.MethodRESPMOD, "/resp/42", true, "response_id"},
	} {
		req, err := icap.NewRequest(tc.method, "icap://localhost"+tc.path)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		matched, err := matchesScenario(context.Background(), scenario, req, DefaultBodyPatternOptions())
		if err != nil {
			t.Fatalf("matchesScenario() error = %v", err)
		}
		if matched != tc.want {
			t.Errorf("%s %s matched=%v, want %v", tc.method, tc.path, matched, tc.want)
		}
		if tc.wantCapture != "" && req.Captures[tc.wantCapture] != "42" {
			t.Errorf("%s %s captures=%v, want %s=42", tc.method, tc.path, req.Captures, tc.wantCapture)
		}
		foreign := "request_id"
		if tc.wantCapture == foreign {
			foreign = "response_id"
		}
		if _, exists := req.Captures[foreign]; exists {
			t.Errorf("%s %s captures unexpectedly contain %s: %v", tc.method, tc.path, foreign, req.Captures)
		}
	}
}

func TestExactRoutesRawRegexMatchMatrix(t *testing.T) {
	scenario := &Scenario{
		Name: "regex",
		Match: MatchRule{Routes: RouteMap{
			icap.MethodREQMOD:  {"re:^/req/[0-9]+$"},
			icap.MethodRESPMOD: {"re:^/resp/[0-9]+$"},
		}},
	}
	registry := &scenarioRegistry{bodyPattern: DefaultBodyPatternOptions()}
	if err := registry.validateAndCompile(scenario); err != nil {
		t.Fatalf("validateAndCompile() error = %v", err)
	}
	for _, tc := range []struct {
		method string
		path   string
		want   bool
	}{
		{icap.MethodREQMOD, "/req/42", true},
		{icap.MethodREQMOD, "/resp/42", false},
		{icap.MethodRESPMOD, "/req/42", false},
		{icap.MethodRESPMOD, "/resp/42", true},
	} {
		req, err := icap.NewRequest(tc.method, "icap://localhost"+tc.path)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		matched, err := matchesScenario(context.Background(), scenario, req, DefaultBodyPatternOptions())
		if err != nil {
			t.Fatalf("matchesScenario() error = %v", err)
		}
		if matched != tc.want {
			t.Errorf("%s %s matched=%v, want %v", tc.method, tc.path, matched, tc.want)
		}
	}
}

func TestExactRoutesRejectReservedServiceConflicts(t *testing.T) {
	tests := []RouteMap{
		{icap.MethodREQMOD: {"/respmod"}},
		{icap.MethodRESPMOD: {"/options"}},
		{icap.MethodREQMOD: {"re:^/(reqmod|custom)$"}},
	}
	for _, routes := range tests {
		scenario := &Scenario{Name: "reserved", Match: MatchRule{Routes: routes}}
		registry := &scenarioRegistry{bodyPattern: DefaultBodyPatternOptions()}
		err := registry.validateAndCompile(scenario)
		if err == nil || !strings.Contains(err.Error(), "reserved service path") {
			t.Errorf("validateAndCompile(%v) error = %v, want reserved service path error", routes, err)
		}
	}
}

func TestExactRoutesStandardAndShardedRegistryParity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yaml")
	data := []byte(`
defaults:
  routes:
    REQMOD: [/req-a, /req-b]
    RESPMOD: /resp
  status: 204
scenarios:
  exact: {}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	standard := NewScenarioRegistry()
	sharded := NewShardedScenarioRegistry()
	if err := standard.Load(path); err != nil {
		t.Fatalf("standard.Load() error = %v", err)
	}
	if err := sharded.Load(path); err != nil {
		t.Fatalf("sharded.Load() error = %v", err)
	}
	if got := len(sharded.List()); got != len(standard.List()) {
		t.Fatalf("sharded scenario count = %d, standard = %d", got, len(standard.List()))
	}
	shardedImpl, ok := sharded.(*ShardedScenarioRegistry)
	if !ok {
		t.Fatalf("sharded registry type = %T", sharded)
	}
	for _, scenario := range shardedImpl.globalScenarios {
		if scenario.Name == "exact" {
			t.Fatal("literal exact-routed scenario was degraded to globalScenarios")
		}
	}
	for _, pair := range []struct {
		method string
		path   string
	}{
		{icap.MethodREQMOD, "/req-a"},
		{icap.MethodREQMOD, "/req-b"},
		{icap.MethodRESPMOD, "/resp"},
	} {
		shard := shardedImpl.shards[shardedImpl.hashString(pair.path)]
		key := pair.method + ":" + pair.path
		found := false
		for _, scenario := range shard.index[key] {
			found = found || scenario.Name == "exact"
		}
		if !found {
			t.Errorf("pair-specific shard index %q does not contain exact scenario", key)
		}
	}

	for _, tc := range []struct {
		method string
		path   string
	}{
		{icap.MethodREQMOD, "/req-a"},
		{icap.MethodREQMOD, "/resp"},
		{icap.MethodRESPMOD, "/req-b"},
		{icap.MethodRESPMOD, "/resp"},
	} {
		req, err := icap.NewRequest(tc.method, "icap://localhost"+tc.path)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		standardMatch, err := standard.Match(context.Background(), req)
		if err != nil {
			t.Fatalf("standard.Match() error = %v", err)
		}
		shardedMatch, err := sharded.Match(context.Background(), req)
		if err != nil {
			t.Fatalf("sharded.Match() error = %v", err)
		}
		if standardMatch.Name != shardedMatch.Name {
			t.Errorf("%s %s: standard=%q sharded=%q", tc.method, tc.path, standardMatch.Name, shardedMatch.Name)
		}
	}
}

func TestExactRoutesEqualPriorityPatternAndLiteralPreserveFileOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priority.yaml")
	data := []byte(`
scenarios:
  first-pattern:
    routes: {REQMOD: "re:^/scan$"}
    priority: 10
    status: 204
  second-literal:
    routes: {REQMOD: /scan}
    priority: 10
    status: 204
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for name, registry := range map[string]ScenarioRegistry{
		"standard": NewScenarioRegistry(),
		"sharded":  NewShardedScenarioRegistry(),
	} {
		if err := registry.Load(path); err != nil {
			t.Fatalf("%s Load() error = %v", name, err)
		}
		req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/scan")
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		matched, err := registry.Match(context.Background(), req)
		if err != nil {
			t.Fatalf("%s Match() error = %v", name, err)
		}
		if matched.Name != "first-pattern" {
			t.Errorf("%s winner = %q, want first-pattern", name, matched.Name)
		}
	}
}

func TestExactRoutesCrossPairCanUseGlobalDefaultsFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.yaml")
	data := []byte(`
defaults:
  routes:
    REQMOD: /req
    RESPMOD: /resp
  response_templates:
    fallback:
      status: 204
      set: {X-Fallback: "yes"}
  use: fallback
scenarios:
  exact:
    status: 204
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	registry := NewScenarioRegistry()
	if err := registry.Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	req, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/resp")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	matched, err := registry.Match(context.Background(), req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if matched.Name != defaultScenarioName || matched.Response.Headers["X-Fallback"] != "yes" {
		t.Fatalf("cross-pair match = %q headers=%v, want configured default fallback", matched.Name, matched.Response.Headers)
	}
}

func TestExactRoutesReloadRejectsEndpointTopologyChange(t *testing.T) {
	for name, factory := range map[string]func() ScenarioRegistry{
		"standard": NewScenarioRegistry,
		"sharded":  NewShardedScenarioRegistry,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "routes.yaml")
			writeRoutesFile := func(endpoint string) {
				t.Helper()
				body := "defaults:\n  routes:\n    REQMOD: " + endpoint + "\nscenarios:\n  exact: {}\n"
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}
			writeRoutesFile("/old")
			registry := factory()
			if err := registry.Load(path); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			writeRoutesFile("/new")
			if err := registry.Reload(); err == nil || !strings.Contains(err.Error(), "restart required") {
				t.Fatalf("Reload() error = %v, want restart required", err)
			}
			if got := registry.List()[0].Match.Paths; len(got) == 0 || got[0] != "/old" {
				t.Fatalf("active endpoints = %v, want /old", got)
			}
		})
	}
}

func TestExactRoutesReloadAllowsResponseChangeAndRollsBackInvalidConfig(t *testing.T) {
	for name, factory := range map[string]func() ScenarioRegistry{
		"standard": NewScenarioRegistry,
		"sharded":  NewShardedScenarioRegistry,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "routes.yaml")
			write := func(method string, status int) {
				t.Helper()
				body := "defaults:\n  routes:\n    " + method + ": /same\n  status: " +
					fmt.Sprintf("%d", status) + "\nscenarios:\n  exact: {}\n"
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}
			write(icap.MethodREQMOD, 204)
			registry := factory()
			if err := registry.Load(path); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			write(icap.MethodREQMOD, 200)
			if err := registry.Reload(); err != nil {
				t.Fatalf("response-only Reload() error = %v", err)
			}
			if got := registry.List()[0].Response.ICAPStatus; got != 200 {
				t.Fatalf("status after reload = %d, want 200", got)
			}
			write(icap.MethodOPTIONS, 500)
			if err := registry.Reload(); err == nil {
				t.Fatal("invalid OPTIONS route Reload() error = nil")
			}
			if got := registry.List()[0].Response.ICAPStatus; got != 200 {
				t.Fatalf("status after rejected reload = %d, want previous 200", got)
			}
		})
	}
}

func TestUniqueScenarioPointersDeduplicatesCandidates(t *testing.T) {
	scenario := &Scenario{Name: "same"}
	got := uniqueScenarioPointers([]*Scenario{scenario, scenario, scenario})
	if len(got) != 1 || got[0] != scenario {
		t.Fatalf("uniqueScenarioPointers() = %v, want one original pointer", got)
	}
}

func TestScenarioRevalidationClearsCompiledExactRoutes(t *testing.T) {
	registry := NewScenarioRegistry()
	scenario := &Scenario{
		Name: "mutable",
		Match: MatchRule{Routes: RouteMap{
			icap.MethodREQMOD: {"/exact"},
		}},
		Response: ResponseTemplate{ICAPStatus: 204},
	}
	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add(exact) error = %v", err)
	}

	scenario.Match = MatchRule{
		Methods: []string{icap.MethodREQMOD},
		Paths:   []string{"/legacy"},
	}
	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add(legacy) error = %v", err)
	}

	legacyRequest, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/legacy")
	if err != nil {
		t.Fatalf("NewRequest(legacy) error = %v", err)
	}
	matched, err := registry.Match(context.Background(), legacyRequest)
	if err != nil {
		t.Fatalf("Match(legacy) error = %v", err)
	}
	if matched != scenario {
		t.Fatalf("legacy match = %q, want mutable", matched.Name)
	}

	exactRequest, err := icap.NewRequest(icap.MethodREQMOD, "icap://localhost/exact")
	if err != nil {
		t.Fatalf("NewRequest(exact) error = %v", err)
	}
	matched, err = registry.Match(context.Background(), exactRequest)
	if err != nil {
		t.Fatalf("Match(exact) error = %v", err)
	}
	if matched == scenario {
		t.Fatal("old exact route remained active after legacy revalidation")
	}
}
