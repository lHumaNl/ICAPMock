// Copyright 2026 ICAP Mock

// Package health provides health check and monitoring endpoints.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
)

// Response represents the JSON payload returned by the /health endpoint.
type Response struct {
	Time   time.Time `json:"time"`
	Status string    `json:"status"`
}

// ReadyResponse represents the response for the /ready endpoint.
type ReadyResponse struct {
	Checks map[string]interface{} `json:"checks"`
	Status string                 `json:"status"`
}

// Status represents the current health status of all components.
type Status struct {
	ICAPError      string
	StorageError   string
	ScenariosCount int
	ICAPReady      bool
	StorageReady   bool
}

// Checker tracks the readiness status of server components.
// It is safe for concurrent use.
type Checker struct {
	icapError      string
	storageError   string
	scenariosCount int
	mu             sync.RWMutex
	icapReady      bool
	storageReady   bool
}

// NewChecker creates a new Checker with default values.
// All components start as not ready.
func NewChecker() *Checker {
	return &Checker{}
}

// SetICAPReady sets the ICAP server readiness status.
// When set to true, any existing ICAP error is cleared.
//
// This method is safe for concurrent use.
func (c *Checker) SetICAPReady(ready bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.icapReady = ready
	if ready {
		c.icapError = ""
	}
}

// IsICAPReady returns whether the ICAP server is ready.
//
// This method is safe for concurrent use.
func (c *Checker) IsICAPReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.icapReady
}

// SetStorageReady sets the storage backend readiness status.
// When set to true, any existing storage error is cleared.
//
// This method is safe for concurrent use.
func (c *Checker) SetStorageReady(ready bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storageReady = ready
	if ready {
		c.storageError = ""
	}
}

// IsStorageReady returns whether the storage backend is ready.
//
// This method is safe for concurrent use.
func (c *Checker) IsStorageReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.storageReady
}

// SetScenariosCount sets the number of loaded mock scenarios.
//
// This method is safe for concurrent use.
func (c *Checker) SetScenariosCount(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scenariosCount = count
}

// SetStorageError sets an error message for the storage backend.
// Setting an error automatically marks storage as not ready.
// Clearing the error (setting to empty string) restores storage to ready.
//
// This method is safe for concurrent use.
func (c *Checker) SetStorageError(err string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storageError = err
	if err != "" {
		c.storageReady = false
	} else {
		c.storageReady = true
	}
}

// SetICAPError sets an error message for the ICAP server.
// Setting an error automatically marks ICAP as not ready.
// Clearing the error (setting to empty string) restores ICAP to ready.
//
// This method is safe for concurrent use.
func (c *Checker) SetICAPError(err string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.icapError = err
	if err != "" {
		c.icapReady = false
	} else {
		c.icapReady = true
	}
}

// IsReady returns whether the server is overall ready to accept traffic.
// This requires both ICAP and Storage to be ready with no errors.
//
// This method is safe for concurrent use.
func (c *Checker) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.icapReady && c.storageReady && c.icapError == "" && c.storageError == ""
}

// GetStatus returns a snapshot of the current health status.
//
// This method is safe for concurrent use.
func (c *Checker) GetStatus() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Status{
		ICAPReady:      c.icapReady,
		StorageReady:   c.storageReady,
		ScenariosCount: c.scenariosCount,
		ICAPError:      c.icapError,
		StorageError:   c.storageError,
	}
}

// Server provides HTTP endpoints for health and readiness checks.
// It is designed to work with Kubernetes liveness and readiness probes.
type Server struct {
	server     *http.Server
	checker    *Checker
	apiHandler *APIHandler
	config     *config.HealthConfig
	metrics    *metrics.Collector
	mgmtConfig config.ManagementConfig
}

// NewServer creates a new Server with the given configuration.
//
// Parameters:
//   - cfg: Health check configuration. Must not be nil.
//
// Returns an error if cfg is nil.
func NewServer(cfg *config.HealthConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("health config cannot be nil")
	}

	return &Server{
		config:     cfg,
		checker:    NewChecker(),
		mgmtConfig: config.ManagementConfig{},
	}, nil
}

// Checker returns the Checker for updating component status.
// Use this to mark components as ready or set error states.
func (s *Server) Checker() *Checker {
	return s.checker
}

// SetMetrics configures metrics for management API requests.
func (s *Server) SetMetrics(collector *metrics.Collector) {
	s.metrics = collector
	if s.apiHandler != nil {
		s.apiHandler.SetMetrics(collector)
	}
}

// SetupAPI configures the REST API for scenario and configuration reloads.
// Must be called before Start(). If registry is nil, API endpoints are not registered.
func (s *Server) SetupAPI(registry storage.ScenarioRegistry, managers ...RuntimeManager) {
	if registry == nil {
		return
	}
	token := s.config.APIToken
	s.apiHandler = NewAPIHandler(registry, token)
	s.apiHandler.SetMetrics(s.metrics)
	s.apiHandler.SetScenarioCountUpdater(s.checker.SetScenariosCount)
	s.apiHandler.ConfigureManagement(s.mgmtConfig, token)
	if len(managers) > 0 {
		s.apiHandler.SetManager(managers[0])
	}
}

// ConfigureManagement sets management controls and authentication.
func (s *Server) ConfigureManagement(cfg config.ManagementConfig, fallbackToken string) {
	s.mgmtConfig = cfg
	if s.apiHandler != nil {
		s.apiHandler.ConfigureManagement(cfg, fallbackToken)
	}
}

// Start starts the health check HTTP server.
// This method blocks until the server is stopped or an error occurs.
// If the server is disabled in config, it returns immediately with nil.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused for disabled check)
//
// Returns an error if the server fails to start.
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc(s.config.HealthPath, s.handleHealth)
	mux.HandleFunc(s.config.ReadyPath, s.handleReady)

	// Register REST API routes if configured
	if s.apiHandler != nil {
		s.apiHandler.RegisterRoutes(mux)
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      s.apiMetricsMiddleware(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Shutdown when context is canceled
	go func() { //nolint:gosec // background context intentional for shutdown
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown uses fresh context because parent is canceled
	}()

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) apiMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.recordAPIMetrics(r, recorder.status)
	})
}

func (s *Server) recordAPIMetrics(r *http.Request, status int) {
	if s.metrics == nil {
		return
	}
	route := apiRoutePattern(r.URL.Path)
	if route == "" {
		return
	}
	s.metrics.RecordAPIRequest("management", route, r.Method, status)
	if status >= http.StatusBadRequest {
		s.metrics.RecordAPIError("management", route, r.Method, status, apiErrorType(status))
	}
}

func apiRoutePattern(path string) string {
	switch path {
	case "/api/v1/scenarios/reload":
		return "/api/v1/scenarios/reload"
	case "/api/v1/config/reload-current":
		return "/api/v1/config/reload-current"
	case "/api/v1/config/load":
		return "/api/v1/config/load"
	default:
		return ""
	}
}

func apiErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	default:
		return strconv.Itoa(status/100) + "xx"
	}
}

// Stop gracefully stops the health check HTTP server.
// It waits for existing connections to finish up to the context deadline.
//
// Parameters:
//   - ctx: Context with timeout for graceful shutdown
//
// Returns an error if shutdown fails.
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// handleHealth handles the /health endpoint.
// It always returns 200 OK with status "healthy" if the server is running.
// Only GET requests are allowed.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := Response{
		Status: "healthy",
		Time:   time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleReady handles the /ready endpoint.
// It returns 200 OK if all components are ready, 503 Service Unavailable otherwise.
// Only GET requests are allowed.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.checker.GetStatus()
	checks := make(map[string]interface{})

	// Check ICAP server status
	switch {
	case status.ICAPError != "":
		checks["icap_server"] = fmt.Sprintf("error: %s", status.ICAPError)
	case status.ICAPReady:
		checks["icap_server"] = "ok"
	default:
		checks["icap_server"] = "starting"
	}

	// Check storage status
	switch {
	case status.StorageError != "":
		checks["storage"] = fmt.Sprintf("error: %s", status.StorageError)
	case status.StorageReady:
		checks["storage"] = "ok"
	default:
		checks["storage"] = "starting"
	}

	// Add scenarios count
	checks["scenarios_loaded"] = status.ScenariosCount

	resp := ReadyResponse{
		Checks: checks,
	}

	isReady := s.checker.IsReady()
	w.Header().Set("Content-Type", "application/json")
	if isReady {
		resp.Status = "ready"
		w.WriteHeader(http.StatusOK)
	} else {
		resp.Status = "not_ready"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_ = json.NewEncoder(w).Encode(resp)
}
