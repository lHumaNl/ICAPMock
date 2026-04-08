// Copyright 2026 ICAP Mock

package health

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/icap-mock/icap-mock/internal/storage"
)

func TestAPIHandler_ListScenarios(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	handler := NewAPIHandler(registry, "")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

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
	handler := NewAPIHandler(registry, "")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

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

func TestAPIHandler_DeleteScenario(t *testing.T) {
	registry := storage.NewScenarioRegistry()
	handler := NewAPIHandler(registry, "")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

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
	handler := NewAPIHandler(registry, "secret-token")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

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
