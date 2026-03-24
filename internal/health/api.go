package health

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
)

// APIHandler provides REST API endpoints for managing scenarios and configuration.
type APIHandler struct {
	registry storage.ScenarioRegistry
	apiToken string
	logger   *slog.Logger
}

// NewAPIHandler creates a new API handler with the given scenario registry.
// If logger is nil, a default no-op logger is used.
func NewAPIHandler(registry storage.ScenarioRegistry, apiToken string, logger ...*slog.Logger) *APIHandler {
	var l *slog.Logger
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	} else {
		l = slog.Default()
	}
	return &APIHandler{
		registry: registry,
		apiToken: apiToken,
		logger:   l,
	}
}

// RegisterRoutes registers all API routes on the given mux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/scenarios", h.withAuth(h.handleScenarios))
	mux.HandleFunc("/api/v1/scenarios/", h.withAuth(h.handleScenarioByName))
	mux.HandleFunc("/api/v1/config/reload", h.withAuth(h.handleConfigReload))
}

// withAuth wraps a handler with bearer token authentication if a token is configured.
func (h *APIHandler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.apiToken != "" {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != h.apiToken {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

// handleScenarios handles GET /api/v1/scenarios and POST /api/v1/scenarios.
func (h *APIHandler) handleScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listScenarios(w, r)
	case http.MethodPost:
		h.addScenario(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleScenarioByName handles GET/PUT/DELETE /api/v1/scenarios/{name}.
func (h *APIHandler) handleScenarioByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/scenarios/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scenario name required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getScenario(w, name)
	case http.MethodPut:
		h.updateScenario(w, r, name)
	case http.MethodDelete:
		h.deleteScenario(w, name)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *APIHandler) listScenarios(w http.ResponseWriter, _ *http.Request) {
	scenarios := h.registry.List()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":     len(scenarios),
		"scenarios": scenarios,
	})
}

func (h *APIHandler) getScenario(w http.ResponseWriter, name string) {
	for _, s := range h.registry.List() {
		if s.Name == name {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("scenario %q not found", name)})
}

func (h *APIHandler) addScenario(w http.ResponseWriter, r *http.Request) {
	var scenario storage.Scenario
	if err := json.NewDecoder(r.Body).Decode(&scenario); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}

	if scenario.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scenario name is required"})
		return
	}

	if err := h.registry.Add(&scenario); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to add scenario: %v", err)})
		return
	}

	h.audit("scenario_created", r, "name", scenario.Name, "priority", scenario.Priority)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": scenario.Name})
}

func (h *APIHandler) updateScenario(w http.ResponseWriter, r *http.Request, name string) {
	var scenario storage.Scenario
	if err := json.NewDecoder(r.Body).Decode(&scenario); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}

	scenario.Name = name

	// Remove old, add new (Add upserts by name)
	if err := h.registry.Add(&scenario); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to update scenario: %v", err)})
		return
	}

	h.audit("scenario_updated", r, "name", name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
}

func (h *APIHandler) deleteScenario(w http.ResponseWriter, name string) {
	if err := h.registry.Remove(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("scenario %q not found", name)})
		return
	}

	h.audit("scenario_deleted", nil, "name", name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

func (h *APIHandler) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if err := h.registry.Reload(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("reload failed: %v", err)})
		return
	}

	count := len(h.registry.List())
	h.audit("config_reloaded", r, "scenarios_count", count)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "reloaded",
		"scenarios": fmt.Sprintf("%d", count),
	})
}

// audit logs a management operation with structured fields.
func (h *APIHandler) audit(action string, r *http.Request, extra ...any) {
	args := []any{
		"action", action,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	}
	if r != nil {
		args = append(args, "remote_addr", r.RemoteAddr)
	}
	args = append(args, extra...)
	h.logger.Info("audit", args...)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
