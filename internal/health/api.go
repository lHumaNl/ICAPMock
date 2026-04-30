// Copyright 2026 ICAP Mock

package health

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/management"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
)

const (
	maxConfigLoadBodyBytes int64 = 4096
)

var errTrailingJSON = errors.New("unexpected trailing JSON")

type configOperationErrorResponse struct {
	Error           string `json:"error"`
	Reason          string `json:"reason,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

// RuntimeManager defines runtime operations exposed by management endpoints.
type RuntimeManager interface {
	ReloadScenarios(ctx context.Context) error
	ReloadCurrentConfig(ctx context.Context) error
	LoadConfigFromPath(ctx context.Context, path string) error
}

type scenarioCounter interface {
	ScenarioCount() int
	ScenarioCounts() []management.ScenarioSetCount
}

// APIHandler provides REST API endpoints for scenario and configuration reloads.
type APIHandler struct {
	registry storage.ScenarioRegistry
	logger   *slog.Logger
	metrics  *metrics.Collector
	state    atomic.Pointer[managementState]
	stateMu  sync.Mutex
}

type managementState struct {
	countUpdater func(int)
	manager      RuntimeManager
	token        string
	cfg          config.ManagementConfig
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
	handler := &APIHandler{
		registry: registry,
		logger:   l,
	}
	handler.state.Store(&managementState{cfg: defaultManagementConfig(), token: apiToken})
	return handler
}

// SetManager configures the runtime manager used by management endpoints.
func (h *APIHandler) SetManager(manager RuntimeManager) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	state := h.currentState()
	h.state.Store(&managementState{cfg: state.cfg, token: state.token, manager: manager, countUpdater: state.countUpdater})
}

// SetMetrics configures management API metrics collection.
func (h *APIHandler) SetMetrics(collector *metrics.Collector) {
	h.metrics = collector
}

// SetScenarioCountUpdater registers a callback for successful scenario reload counts.
func (h *APIHandler) SetScenarioCountUpdater(fn func(int)) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	state := h.currentState()
	h.state.Store(&managementState{cfg: state.cfg, token: state.token, manager: state.manager, countUpdater: fn})
}

// ConfigureManagement updates management endpoint controls and authentication.
func (h *APIHandler) ConfigureManagement(cfg config.ManagementConfig, fallbackToken string) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	state := h.currentState()
	token := resolvedManagementToken(cfg, fallbackToken)
	h.state.Store(&managementState{cfg: cfg, token: token, manager: state.manager, countUpdater: state.countUpdater})
}

// RegisterRoutes registers all API routes on the given mux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/scenarios/reload", h.withAuth(h.handleScenariosReload))
	mux.HandleFunc("/api/v1/config/reload-current", h.withAuth(h.handleConfigReloadCurrent))
	mux.HandleFunc("/api/v1/config/load", h.withAuth(h.handleConfigLoad))
}

// withAuth wraps a handler with bearer token authentication if a token is configured.
func (h *APIHandler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := h.currentState()
		if !state.cfg.Enabled {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "disabled"})
			return
		}
		if state.token != "" {
			if !validBearerToken(r.Header.Get("Authorization"), state.token) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func (h *APIHandler) handleScenariosReload(w http.ResponseWriter, r *http.Request) {
	state := h.currentState()
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !state.cfg.ScenarioReloadEnabled || state.manager == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "disabled"})
		return
	}
	if err := state.manager.ReloadScenarios(r.Context()); err != nil {
		h.logger.Warn("scenario reload failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reload failed"})
		return
	}
	count, setCounts := h.scenarioReloadCounts(state.manager)
	h.recordScenarioSetMetrics(setCounts, count)
	if state.countUpdater != nil {
		state.countUpdater(count)
	}
	h.audit("scenarios_reloaded", r, "scenarios_count", count)
	writeJSON(w, http.StatusOK, scenarioReloadResponse(count, setCounts))
}

func (h *APIHandler) recordScenarioSetMetrics(setCounts []management.ScenarioSetCount, total int) {
	if h.metrics == nil {
		return
	}
	if len(setCounts) == 0 {
		h.metrics.SetScenariosLoadedSnapshot(map[string]int{"default": total})
		return
	}
	snapshot := make(map[string]int, len(setCounts))
	for _, setCount := range setCounts {
		snapshot[setCount.Name] = setCount.Count
	}
	h.metrics.SetScenariosLoadedSnapshot(snapshot)
}

func (h *APIHandler) scenarioReloadCounts(manager RuntimeManager) (int, []management.ScenarioSetCount) {
	counter, ok := manager.(scenarioCounter)
	if !ok {
		return len(h.registry.List()), nil
	}
	return counter.ScenarioCount(), counter.ScenarioCounts()
}

func scenarioReloadResponse(count int, setCounts []management.ScenarioSetCount) map[string]interface{} {
	resp := map[string]interface{}{"status": "ok", "scenarios": count}
	if len(setCounts) > 0 {
		resp["scenario_sets"] = setCounts
	}
	return resp
}

func (h *APIHandler) handleConfigReloadCurrent(w http.ResponseWriter, r *http.Request) {
	state, ok := h.allowConfigOperation(w, r)
	if !ok {
		return
	}
	if err := state.manager.ReloadCurrentConfig(r.Context()); err != nil {
		h.logger.Warn("config reload failed", "error", err)
		writeConfigOperationError(w, err)
		return
	}
	h.audit("config_reloaded", r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *APIHandler) handleConfigLoad(w http.ResponseWriter, r *http.Request) {
	state, ok := h.allowConfigOperation(w, r)
	if !ok {
		return
	}
	path, ok := decodeConfigLoadPath(w, r)
	if !ok {
		return
	}
	if err := state.manager.LoadConfigFromPath(r.Context(), path); err != nil {
		h.logger.Warn("config load failed", "error", err)
		writeConfigOperationError(w, err)
		return
	}
	h.audit("config_loaded", r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *APIHandler) allowConfigOperation(w http.ResponseWriter, r *http.Request) (*managementState, bool) {
	state := h.currentState()
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return state, false
	}
	if !state.cfg.ConfigReloadEnabled || state.manager == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "disabled"})
		return state, false
	}
	return state, true
}

func decodeConfigLoadPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Path string `json:"path"`
	}
	body := http.MaxBytesReader(w, r.Body, maxConfigLoadBodyBytes)
	if err := decodeSingleJSON(json.NewDecoder(body), &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return "", false
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return "", false
	}
	return req.Path, true
}

func decodeSingleJSON(dec *json.Decoder, dst interface{}) error {
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errTrailingJSON
		}
		return err
	}
	return nil
}

func writeConfigOperationError(w http.ResponseWriter, err error) {
	if errors.Is(err, management.ErrUnsupportedRuntimeChange) {
		writeJSON(w, http.StatusConflict, unsupportedConfigResponse(err))
		return
	}
	if isConfigClientError(err) {
		writeJSON(w, http.StatusBadRequest, configClientErrorResponse(err))
		return
	}
	writeJSON(w, http.StatusInternalServerError, configRuntimeErrorResponse())
}

func unsupportedConfigResponse(err error) configOperationErrorResponse {
	return configOperationErrorResponse{
		Error:           "unsupported live config change",
		Reason:          err.Error(),
		RestartRequired: true,
	}
}

func configClientErrorResponse(err error) configOperationErrorResponse {
	if validationErr := configValidationError(err); validationErr != nil {
		return configOperationErrorResponse{Error: "config validation failed", Reason: safeValidationReason(validationErr.Errors)}
	}
	if loadErr := configLoadError(err); loadErr != nil {
		return configOperationErrorResponse{Error: "config load failed", Reason: safeConfigLoadReason(loadErr.Err)}
	}
	return configOperationErrorResponse{Error: "invalid config path", Reason: "path is required"}
}

func configRuntimeErrorResponse() configOperationErrorResponse {
	return configOperationErrorResponse{Error: "runtime apply failed"}
}

func isConfigClientError(err error) bool {
	return isConfigLoadError(err) || isConfigValidationError(err) || isConfigPathError(err)
}

func isConfigLoadError(err error) bool { return configLoadError(err) != nil }

func configLoadError(err error) *management.ConfigLoadError {
	var loadErr *management.ConfigLoadError
	if errors.As(err, &loadErr) {
		return loadErr
	}
	return nil
}

func isConfigValidationError(err error) bool { return configValidationError(err) != nil }

func configValidationError(err error) *management.ConfigValidationError {
	var validationErr *management.ConfigValidationError
	if errors.As(err, &validationErr) {
		return validationErr
	}
	return nil
}

func isConfigPathError(err error) bool {
	return errors.Is(err, management.ErrConfigPathRequired) || errors.Is(err, management.ErrCurrentConfigPathRequired)
}

func defaultManagementConfig() config.ManagementConfig { return config.ManagementConfig{} }

func (h *APIHandler) currentState() *managementState {
	state := h.state.Load()
	if state != nil {
		return state
	}
	return &managementState{cfg: defaultManagementConfig()}
}

func resolvedManagementToken(cfg config.ManagementConfig, fallbackToken string) string {
	token := cfg.ResolvedToken()
	if token != "" {
		return token
	}
	return fallbackToken
}

func validBearerToken(auth, expected string) bool {
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return constantTimeTokenEqual(expected, auth[len("Bearer "):])
}

func constantTimeTokenEqual(expected, provided string) bool {
	expectedHash := sha256.Sum256([]byte(expected))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func safeConfigLoadReason(err error) string {
	switch {
	case errors.Is(err, management.ErrConfigFileTooLarge):
		return "config file is too large"
	case errors.Is(err, management.ErrConfigFileNotRegular):
		return "config path must be a regular file"
	case errors.Is(err, os.ErrNotExist):
		return "config file not found"
	case errors.Is(err, os.ErrPermission):
		return "config file is not readable"
	default:
		return "config file could not be loaded"
	}
}

func safeValidationReason(validationErrors []config.ValidationError) string {
	if len(validationErrors) == 0 {
		return "config did not pass validation"
	}
	fields := make([]string, 0, len(validationErrors))
	for _, err := range validationErrors {
		fields = append(fields, err.Field)
	}
	return "invalid fields: " + strings.Join(fields, ", ")
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
	_ = json.NewEncoder(w).Encode(v)
}
