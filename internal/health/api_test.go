// Copyright 2026 ICAP Mock

package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/management"
	"github.com/icap-mock/icap-mock/internal/storage"
)

func TestAPIHandler_ListScenarios(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	mux := newScenarioAPIHandler(registry, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"] == nil {
		t.Fatal("expected count in response")
	}
}

func TestAPIHandler_AddAndGetScenario(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	mux := newScenarioAPIHandler(registry, "")

	// Add scenario
	scenario := `{"name":"test-scenario","match":{"path_pattern":"/test"},"response":{"icap_status":200},"priority":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios", bytes.NewBufferString(scenario))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get scenario
	req = httptest.NewRequest(http.MethodGet, "/api/v1/scenarios/test-scenario", http.NoBody)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}

	var s storage.Scenario
	json.NewDecoder(w.Body).Decode(&s)
	if s.Name != "test-scenario" {
		t.Fatalf("expected name test-scenario, got %s", s.Name)
	}
}

func TestAPIHandler_AddScenarioJSONNormalizesStreamSelectors(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	mux := newScenarioAPIHandler(registry, "")
	body := `{
		"name":"json-stream",
		"match":{"path_pattern":"^/upload","icap_method":["REQMOD"]},
		"response":{"icap_status":200,"stream":{
			"from":"request_http_body",
			"multipart":{"files":true},
			"fallback":{"raw_file":{"filename":".*\\.bin$"}}
		}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	assertJSONStreamSelectorFlags(t, registry)
}

func TestAPIHandler_DeleteScenario(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	mux := newScenarioAPIHandler(registry, "")

	// Add then delete
	scenario := `{"name":"to-delete","match":{},"response":{"icap_status":200}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios", bytes.NewBufferString(scenario))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/scenarios/to-delete", http.NoBody)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	req = httptest.NewRequest(http.MethodGet, "/api/v1/scenarios/to-delete", http.NoBody)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

func TestAPIHandler_Auth(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	mux := newScenarioAPIHandler(registry, "secret-token")

	// Without token — should fail
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: expected 401, got %d", w.Code)
	}

	// With wrong token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", w.Code)
	}

	// With correct token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody)
	req.Header.Set("Authorization", "Bearer secret-token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("correct token: expected 200, got %d", w.Code)
	}
}

func TestAPIHandler_DefaultManagementDisabled(t *testing.T) {
	mux := http.NewServeMux()
	NewAPIHandler(storage.NewScenarioRegistry(), "").RegisterRoutes(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 while disabled, got %d", w.Code)
	}
}

func TestAPIHandler_EnabledWithoutTokenAllowsUnauthenticated(t *testing.T) {
	mux := newScenarioAPIHandler(storage.NewScenarioRegistry(), "")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without token, got %d", w.Code)
	}
}

func TestAPIHandler_RejectsFileBackedScenarioResponse(t *testing.T) {
	cases := []string{
		`{"name":"body-file","response":{"body_file":"/tmp/body"}}`,
		`{"name":"http-body-file","response":{"http_body_file":"/tmp/body"}}`,
		`{"name":"stream-file","response":{"stream":{"source":{"body_file":"/tmp/body"}}}}`,
		`{"name":"stream-part-file","response":{"stream":{"parts":[{"body_file":"/tmp/body"}]}}}`,
		`{"name":"stream-fallback-file","response":{"stream":{"source":{"from":"request_http_body"},` +
			`"fallback":{"body_file":"/tmp/body"}}}}`,
	}
	mux := newScenarioAPIHandler(storage.NewScenarioRegistry(), "")
	for _, body := range cases {
		assertScenarioRejected(t, mux, body)
	}
}

func TestRejectFileBackedScenarioTraversesNestedResponses(t *testing.T) {
	for _, scenario := range nestedFileBackedScenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if !rejectFileBackedScenario(w, &scenario) {
				t.Fatal("expected nested file-backed scenario to be rejected")
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func nestedFileBackedScenarios() []storage.Scenario {
	return []storage.Scenario{
		{Name: "weighted-body-file", WeightedResponses: []storage.WeightedResponse{{BodyFile: "/tmp/body"}}},
		{
			Name:              "weighted-http-body-file",
			WeightedResponses: []storage.WeightedResponse{{HTTPBodyFile: "/tmp/body"}},
		},
		{Name: "weighted-stream-file", WeightedResponses: []storage.WeightedResponse{{Stream: streamWithBodyFile()}}},
		{
			Name:     "branch-response-file",
			Branches: []storage.Branch{{Response: storage.ResponseTemplate{BodyFile: "/tmp/body"}}},
		},
		{
			Name: "branch-weighted-file",
			Branches: []storage.Branch{{WeightedResponses: []storage.WeightedResponse{{
				HTTPBodyFile: "/tmp/body",
			}}}},
		},
	}
}

func streamWithBodyFile() *storage.StreamConfig {
	return &storage.StreamConfig{Parts: []storage.StreamPartConfig{{BodyFile: "/tmp/body"}}}
}

func TestAPIHandler_RejectsFileBackedScenarioUpdate(t *testing.T) {
	mux := newScenarioAPIHandler(storage.NewScenarioRegistry(), "")
	body := `{"response":{"body_file":"/tmp/body"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scenarios/file", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIHandler_ScenarioCreateUpdateRejectTrailingJSON(t *testing.T) {
	mux := newScenarioAPIHandler(storage.NewScenarioRegistry(), "")
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/scenarios", `{"name":"bad"} {}`},
		{http.MethodPut, "/api/v1/scenarios/bad", `{"response":{}} {}`},
	}
	for _, tc := range cases {
		assertScenarioMutationRejected(t, mux, tc.method, tc.path, tc.body)
	}
}

func TestAPIHandler_ScenarioCreateUpdateRejectOversizedBody(t *testing.T) {
	mux := newScenarioAPIHandler(storage.NewScenarioRegistry(), "")
	oversizedName := strings.Repeat("a", int(maxScenarioBodyBytes))
	body := `{"name":"` + oversizedName + `"}`
	assertScenarioMutationRejected(t, mux, http.MethodPost, "/api/v1/scenarios", body)
	assertScenarioMutationRejected(t, mux, http.MethodPut, "/api/v1/scenarios/large", body)
}

func TestAPIHandler_ConfigLoadInvalidJSON(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/load", bytes.NewBufferString("{"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPIHandler_ConfigLoadRejectsTrailingJSON(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	body := bytes.NewBufferString(`{"path":"config.yaml"} {}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/load", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPIHandler_ConfigLoadInvalidConfigRollback(t *testing.T) {
	handler, manager := newManagementTestHandler(t, "")
	oldPath := manager.CurrentConfigPath()
	badPath := writeTempFile(t, t.TempDir(), "bad.yaml", "server:\n  port: 70000\n")
	payload, err := json.Marshal(map[string]string{"path": badPath})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	body := bytes.NewBuffer(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/load", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "config validation failed") {
		t.Fatalf("expected validation failure response, got %s", w.Body.String())
	}
	if manager.CurrentConfigPath() != oldPath {
		t.Fatalf("config path changed after failed load")
	}
}

func TestAPIHandler_ConfigLoadMissingFileReturnsBadRequest(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, missingPath))

	resp := assertConfigOperationResponse(t, w, http.StatusBadRequest)
	if resp.Error != "config load failed" {
		t.Fatalf("expected config load failure, got %q", resp.Error)
	}
	assertResponseOmitsPath(t, w.Body.String(), missingPath)
}

func TestAPIHandler_ConfigLoadRejectsDirectory(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	dir := t.TempDir()
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, dir))

	resp := assertConfigOperationResponse(t, w, http.StatusBadRequest)
	if resp.Reason != "config path must be a regular file" {
		t.Fatalf("expected regular-file reason, got %+v", resp)
	}
	assertResponseOmitsPath(t, w.Body.String(), dir)
}

func TestAPIHandler_ConfigLoadRejectsOversizedFile(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	path := writeOversizedConfigFile(t)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, path))

	resp := assertConfigOperationResponse(t, w, http.StatusBadRequest)
	if resp.Reason != "config file is too large" {
		t.Fatalf("expected oversized-file reason, got %+v", resp)
	}
	assertResponseOmitsPath(t, w.Body.String(), path)
}

func TestAPIHandler_ConfigValidationOmitsRawPath(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "")
	tlsPath := filepath.Join(t.TempDir(), "missing-cert.pem")
	configPath := writeTLSConfigWithMissingFiles(t, tlsPath)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, configPath))

	resp := assertConfigOperationResponse(t, w, http.StatusBadRequest)
	if resp.Error != "config validation failed" {
		t.Fatalf("expected validation failure, got %+v", resp)
	}
	assertResponseOmitsPath(t, w.Body.String(), tlsPath)
}

func TestAPIHandler_ConfigLoadRuntimeFailureReturnsServerError(t *testing.T) {
	errRuntime := errors.New("runtime manager failure")
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	handler := newAPIHandlerWithRuntimeManager(registry, failingRuntimeManager{err: errRuntime}, "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, "config.yaml"))

	resp := assertConfigOperationResponse(t, w, http.StatusInternalServerError)
	if resp.Error != "runtime apply failed" || resp.Reason != "" {
		t.Fatalf("expected sanitized runtime failure, got %+v", resp)
	}
}

func TestAPIHandler_ConfigReloadCurrentUnsupportedChangeReturnsConflict(t *testing.T) {
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	errUnsupported := management.UnsupportedRuntimeChangeError{Reason: "static field changed; restart required"}
	handler := newAPIHandlerWithRuntimeManager(registry, failingRuntimeManager{err: errUnsupported}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload-current", http.NoBody)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assertConfigConflictResponse(t, w)
}

func TestAPIHandler_ConfigLoadAppliesScenarioDir(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeTempFile(t, oldDir, "old.yaml", scenarioYAML("old", "/old"))
	writeTempFile(t, newDir, "new.yaml", scenarioYAML("new", "/new"))
	oldPath := writeConfigWithScenarioDir(t, oldDir)
	newPath := writeConfigWithScenarioDir(t, newDir)
	cfg := loadConfigForAPITest(t, oldPath)
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(cfg, oldPath)
	manager.RegisterScenarioSet(management.ScenarioSet{Dir: oldDir, Registry: registry, Name: "default", Server: cfg.Server})
	if err := manager.ReloadScenarios(t.Context()); err != nil {
		t.Fatalf("initial reload error = %v", err)
	}
	handler := newAPIHandlerWithManager(registry, manager, "")
	req := newConfigLoadRequest(t, newPath)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertScenarioNames(t, registry.List(), "new")
	assertScenarioMissing(t, registry.List(), "old")
}

func TestAPIHandler_ConfigLoadUnsupportedStaticChange(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeTempFile(t, oldDir, "old.yaml", scenarioYAML("old", "/old"))
	writeTempFile(t, newDir, "new.yaml", scenarioYAML("new", "/new"))
	oldPath := writeConfigWithPortAndScenarioDir(t, oldDir, 1344)
	newPath := writeConfigWithPortAndScenarioDir(t, newDir, 1345)
	registry, manager := newLoadedAPIManager(t, oldDir, oldPath)
	handler := newAPIHandlerWithManager(registry, manager, "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, newConfigLoadRequest(t, newPath))

	assertConfigConflictResponse(t, w)
	if manager.CurrentConfigPath() != managementPath(oldPath) {
		t.Fatalf("config path changed after unsupported load")
	}
	assertScenarioNames(t, registry.List(), "old")
	assertScenarioMissing(t, registry.List(), "new")
}

func TestAPIHandler_ManagementAuth(t *testing.T) {
	handler, _ := newManagementTestHandler(t, "secret-token")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload-current", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/config/reload-current", http.NoBody)
	req.Header.Set("Authorization", "Bearer secret-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", w.Code)
	}
}

func TestAPIHandler_ScenariosReloadMultiFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one.yaml", scenarioYAML("one", "/one"))
	writeTempFile(t, dir, "two.yaml", scenarioYAML("two", "/two"))
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(management.ScenarioSet{Name: "default", Dir: dir, Registry: registry})
	handler := newAPIHandlerWithManager(registry, manager, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/reload", http.NoBody)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertScenarioNames(t, registry.List(), "one", "two")
}

func TestAPIHandler_ScenariosReloadReportsAggregateCount(t *testing.T) {
	avDir := t.TempDir()
	proxyDir := t.TempDir()
	writeTempFile(t, avDir, "av.yaml", scenarioYAML("av", "/av"))
	writeTempFile(t, proxyDir, "one.yaml", scenarioYAML("proxy-one", "/one"))
	writeTempFile(t, proxyDir, "two.yaml", scenarioYAML("proxy-two", "/two"))
	avRegistry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	proxyRegistry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(management.ScenarioSet{Name: "proxy", Dir: proxyDir, Registry: proxyRegistry})
	manager.RegisterScenarioSet(management.ScenarioSet{Name: "av", Dir: avDir, Registry: avRegistry})
	handler := newAPIHandlerWithManager(avRegistry, manager, "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/reload", http.NoBody))

	resp := assertScenarioReloadResponse(t, w, http.StatusOK)
	if resp.Scenarios != 5 || len(resp.ScenarioSets) != 2 {
		t.Fatalf("unexpected reload response: %+v", resp)
	}
	if resp.ScenarioSets[0].Name != "av" || resp.ScenarioSets[1].Name != "proxy" {
		t.Fatalf("scenario_sets order = %+v, want av then proxy", resp.ScenarioSets)
	}
	if resp.ScenarioSets[0].Count != 2 || resp.ScenarioSets[1].Count != 3 {
		t.Fatalf("scenario_sets counts = %+v, want 2 and 3", resp.ScenarioSets)
	}
}

func TestAPIHandler_ScenariosReloadUpdatesReadinessCount(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "one.yaml", scenarioYAML("one", "/one"))
	writeTempFile(t, dir, "two.yaml", scenarioYAML("two", "/two"))
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(management.ScenarioSet{Name: "default", Dir: dir, Registry: registry})
	server := newManagementHealthServer(t, registry, manager)
	w := httptest.NewRecorder()

	server.apiHandler.handleScenariosReload(w, httptest.NewRequest(http.MethodPost, "/api/v1/scenarios/reload", http.NoBody))

	if server.Checker().GetStatus().ScenariosCount != 3 {
		t.Fatalf("ScenariosCount = %d, want 3", server.Checker().GetStatus().ScenariosCount)
	}
}

func TestAPIHandler_ConfigureManagementConcurrentRequests(t *testing.T) {
	t.Helper()
	handler := NewAPIHandler(storage.NewScenarioRegistry(), "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	var stop atomic.Bool
	var wg sync.WaitGroup

	for range 4 {
		wg.Add(1)
		go serveScenarioRequests(&wg, &stop, mux)
	}
	for i := range 100 {
		handler.ConfigureManagement(managementConfigForIteration(i), "")
	}
	stop.Store(true)
	wg.Wait()
}

func TestAPIHandler_ConcurrentManagerAndConfigUpdates(t *testing.T) {
	handler := NewAPIHandler(storage.NewScenarioRegistry(), "")
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); handler.SetManager(failingRuntimeManager{}) }()
		go func() { defer wg.Done(); handler.ConfigureManagement(config.ManagementConfig{Enabled: true}, "") }()
	}
	wg.Wait()
	handler.SetManager(failingRuntimeManager{})
	handler.ConfigureManagement(config.ManagementConfig{Enabled: true, ConfigReloadEnabled: true}, "")
	assertReloadCurrentAllowed(t, handler)
}

func newManagementTestHandler(t *testing.T, token string) (*http.ServeMux, *management.RuntimeManager) {
	t.Helper()
	validPath := writeTempFile(t, t.TempDir(), "valid.yaml", "server:\n  port: 1344\n")
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(loadConfigForAPITest(t, validPath), validPath)
	return newAPIHandlerWithManager(registry, manager, token), manager
}

func newAPIHandlerWithManager(registry storage.ScenarioRegistry, manager *management.RuntimeManager, token string) *http.ServeMux {
	return newAPIHandlerWithRuntimeManager(registry, manager, token)
}

func newAPIHandlerWithRuntimeManager(registry storage.ScenarioRegistry, manager RuntimeManager, token string) *http.ServeMux {
	handler := NewAPIHandler(registry, "")
	handler.SetManager(manager)
	handler.ConfigureManagement(config.ManagementConfig{
		Token:                 token,
		Enabled:               true,
		ScenarioReloadEnabled: true,
		ConfigReloadEnabled:   true,
	}, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func newManagementHealthServer(
	t *testing.T,
	registry storage.ScenarioRegistry,
	manager *management.RuntimeManager,
) *Server {
	t.Helper()
	server, err := NewServer(&config.HealthConfig{Enabled: true})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	server.ConfigureManagement(config.ManagementConfig{Enabled: true, ScenarioReloadEnabled: true}, "")
	server.SetupAPI(registry, manager)
	return server
}

func newScenarioAPIHandler(registry storage.ScenarioRegistry, token string) *http.ServeMux {
	handler := NewAPIHandler(registry, "")
	handler.ConfigureManagement(config.ManagementConfig{Enabled: true, Token: token}, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func assertScenarioRejected(t *testing.T, mux *http.ServeMux, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scenarios", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for %s, got %d: %s", body, w.Code, w.Body.String())
	}
}

func assertJSONStreamSelectorFlags(t *testing.T, registry storage.ScenarioRegistry) {
	t.Helper()
	for _, scenario := range registry.List() {
		if scenario.Name == "json-stream" {
			assertJSONStreamSelectorFlagValues(t, scenario.Response.Stream)
			return
		}
	}
	t.Fatal("json-stream scenario was not registered")
}

func assertJSONStreamSelectorFlagValues(t *testing.T, stream *storage.StreamConfig) {
	t.Helper()
	if stream == nil || stream.Source.From != "request_http_body" {
		t.Fatalf("stream source was not normalized: %+v", stream)
	}
	if !stream.Multipart.IsSet || !stream.Multipart.Files.IsSet || !stream.Multipart.Files.Enabled {
		t.Fatalf("multipart flags were not set: %+v", stream.Multipart)
	}
	if !stream.Fallback.RawFile.IsSet || !stream.Fallback.RawFile.Enabled {
		t.Fatalf("raw_file flags were not set: %+v", stream.Fallback.RawFile)
	}
	if got := stream.Fallback.RawFile.Filename; len(got) != 1 || got[0] != `.*\.bin$` {
		t.Fatalf("raw_file filename = %v, want single .bin regex", got)
	}
}

func assertScenarioMutationRejected(t *testing.T, mux *http.ServeMux, method, path, body string) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for %s %s, got %d: %s", method, path, w.Code, w.Body.String())
	}
}

func assertReloadCurrentAllowed(t *testing.T, handler *APIHandler) {
	t.Helper()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload-current", http.NoBody)
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after concurrent updates, got %d: %s", w.Code, w.Body.String())
	}
}

func serveScenarioRequests(wg *sync.WaitGroup, stop *atomic.Bool, mux *http.ServeMux) {
	defer wg.Done()
	for !stop.Load() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/scenarios", http.NoBody)
		mux.ServeHTTP(w, req)
	}
}

func managementConfigForIteration(i int) config.ManagementConfig {
	if i%2 == 0 {
		return config.ManagementConfig{Enabled: true}
	}
	return config.ManagementConfig{Enabled: true, Token: "secret-token"}
}

type failingRuntimeManager struct {
	err error
}

func (m failingRuntimeManager) ReloadScenarios(context.Context) error {
	return m.err
}

func (m failingRuntimeManager) ReloadCurrentConfig(context.Context) error {
	return m.err
}

func (m failingRuntimeManager) LoadConfigFromPath(context.Context, string) error {
	return m.err
}

func writeTempFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func writeOversizedConfigFile(t *testing.T) string {
	t.Helper()
	body := strings.Repeat("#", int(management.MaxConfigFileBytes)+1)
	return writeTempFile(t, t.TempDir(), "large.yaml", body)
}

func writeTLSConfigWithMissingFiles(t *testing.T, tlsPath string) string {
	t.Helper()
	body := "server:\n  tls:\n    enabled: true\n    cert_file: \"" + tlsPath + "\"\n    key_file: \"" + tlsPath + "\"\n"
	return writeTempFile(t, t.TempDir(), "tls.yaml", body)
}

func writeConfigWithScenarioDir(t *testing.T, scenariosDir string) string {
	t.Helper()
	return writeConfigWithPortAndScenarioDir(t, scenariosDir, 1344)
}

func writeConfigWithPortAndScenarioDir(t *testing.T, scenariosDir string, port int) string {
	t.Helper()
	body := "server:\n  port: " + strconv.Itoa(port) + "\nmock:\n  scenarios_dir: \"" + scenariosDir + "\"\n"
	return writeTempFile(t, t.TempDir(), "config.yaml", body)
}

func loadConfigForAPITest(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.NewLoader().Load(config.LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func newConfigLoadRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/v1/config/load", bytes.NewBuffer(payload))
}

func scenarioYAML(name, path string) string {
	return "scenarios:\n  - name: " + name + "\n    match:\n      method: REQMOD\n      path_pattern: \"" + path + "\"\n    response:\n      icap_status: 204\n"
}

type scenarioReloadTestResponse struct {
	Status       string                        `json:"status"`
	ScenarioSets []management.ScenarioSetCount `json:"scenario_sets"`
	Scenarios    int                           `json:"scenarios"`
}

func assertScenarioReloadResponse(t *testing.T, w *httptest.ResponseRecorder, status int) scenarioReloadTestResponse {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected %d, got %d: %s", status, w.Code, w.Body.String())
	}
	var resp scenarioReloadTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode reload response: %v", err)
	}
	return resp
}

func assertScenarioNames(t *testing.T, scenarios []*storage.Scenario, names ...string) {
	t.Helper()
	found := make(map[string]bool, len(scenarios))
	for _, scenario := range scenarios {
		found[scenario.Name] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Fatalf("scenario %q was not loaded", name)
		}
	}
}

func assertScenarioMissing(t *testing.T, scenarios []*storage.Scenario, names ...string) {
	t.Helper()
	found := make(map[string]bool, len(scenarios))
	for _, scenario := range scenarios {
		found[scenario.Name] = true
	}
	for _, name := range names {
		if found[name] {
			t.Fatalf("scenario %q should not be loaded", name)
		}
	}
}

func newLoadedAPIManager(
	t *testing.T,
	dir string,
	path string,
) (*management.ManagedScenarioRegistry, *management.RuntimeManager) {
	t.Helper()
	cfg := loadConfigForAPITest(t, path)
	registry := management.NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := management.NewRuntimeManager(cfg, path)
	manager.RegisterScenarioSet(management.ScenarioSet{Dir: dir, Registry: registry, Name: "default", Server: cfg.Server})
	if err := manager.ReloadScenarios(t.Context()); err != nil {
		t.Fatalf("initial reload error = %v", err)
	}
	return registry, manager
}

func assertConfigConflictResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	resp := assertConfigOperationResponse(t, w, http.StatusConflict)
	if resp.Error != "unsupported live config change" {
		t.Fatalf("expected unsupported live config change, got %q", resp.Error)
	}
	if !resp.RestartRequired {
		t.Fatalf("expected restart_required true, got %+v", resp)
	}
	if resp.Reason == "" {
		t.Fatalf("expected restart-required reason, got %+v", resp)
	}
}

func assertConfigOperationResponse(
	t *testing.T,
	w *httptest.ResponseRecorder,
	status int,
) configOperationErrorResponse {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected %d, got %d: %s", status, w.Code, w.Body.String())
	}
	var resp configOperationErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func assertResponseOmitsPath(t *testing.T, body, path string) {
	t.Helper()
	if strings.Contains(body, path) {
		t.Fatalf("response includes raw path %q: %s", path, body)
	}
}

func managementPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}
