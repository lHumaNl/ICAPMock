// Copyright 2026 ICAP Mock

// Package health provides health check and monitoring endpoints.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

// HealthResponse represents the response for the /health endpoint.
type HealthResponse struct {
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

// HealthChecker tracks the readiness status of server components.
// It is safe for concurrent use.
type HealthChecker struct {
	icapError      string
	storageError   string
	scenariosCount int
	mu             sync.RWMutex
	icapReady      bool
	storageReady   bool
}

// NewHealthChecker creates a new HealthChecker with default values.
// All components start as not ready.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{}
}

// SetICAPReady sets the ICAP server readiness status.
// When set to true, any existing ICAP error is cleared.
//
// This method is safe for concurrent use.
func (c *HealthChecker) SetICAPReady(ready bool) {
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
func (c *HealthChecker) IsICAPReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.icapReady
}

// SetStorageReady sets the storage backend readiness status.
// When set to true, any existing storage error is cleared.
//
// This method is safe for concurrent use.
func (c *HealthChecker) SetStorageReady(ready bool) {
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
func (c *HealthChecker) IsStorageReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.storageReady
}

// SetScenariosCount sets the number of loaded mock scenarios.
//
// This method is safe for concurrent use.
func (c *HealthChecker) SetScenariosCount(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scenariosCount = count
}

// SetStorageError sets an error message for the storage backend.
// Setting an error automatically marks storage as not ready.
// Clearing the error (setting to empty string) restores storage to ready.
//
// This method is safe for concurrent use.
func (c *HealthChecker) SetStorageError(err string) {
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
func (c *HealthChecker) SetICAPError(err string) {
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
func (c *HealthChecker) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.icapReady && c.storageReady && c.icapError == "" && c.storageError == ""
}

// GetStatus returns a snapshot of the current health status.
//
// This method is safe for concurrent use.
func (c *HealthChecker) GetStatus() Status {
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

// HealthServer provides HTTP endpoints for health and readiness checks.
// It is designed to work with Kubernetes liveness and readiness probes.
type HealthServer struct {
	config     *config.HealthConfig
	server     *http.Server
	checker    *HealthChecker
	apiHandler *APIHandler
}

// NewHealthServer creates a new HealthServer with the given configuration.
//
// Parameters:
//   - cfg: Health check configuration. Must not be nil.
//
// Returns an error if cfg is nil.
func NewHealthServer(cfg *config.HealthConfig) (*HealthServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("health config cannot be nil")
	}

	return &HealthServer{
		config:  cfg,
		checker: NewHealthChecker(),
	}, nil
}

// Checker returns the HealthChecker for updating component status.
// Use this to mark components as ready or set error states.
func (s *HealthServer) Checker() *HealthChecker {
	return s.checker
}

// SetupAPI configures the REST API for scenario management.
// Must be called before Start(). If registry is nil, API endpoints are not registered.
func (s *HealthServer) SetupAPI(registry storage.ScenarioRegistry) {
	if registry == nil {
		return
	}
	token := s.config.APIToken
	s.apiHandler = NewAPIHandler(registry, token)
}

// Start starts the health check HTTP server.
// This method blocks until the server is stopped or an error occurs.
// If the server is disabled in config, it returns immediately with nil.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused for disabled check)
//
// Returns an error if the server fails to start.
func (s *HealthServer) Start(ctx context.Context) error {
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
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Shutdown when context is canceled
	go func() { //nolint:gosec // background context intentional for shutdown
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully stops the health check HTTP server.
// It waits for existing connections to finish up to the context deadline.
//
// Parameters:
//   - ctx: Context with timeout for graceful shutdown
//
// Returns an error if shutdown fails.
func (s *HealthServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// handleHealth handles the /health endpoint.
// It always returns 200 OK with status "healthy" if the server is running.
// Only GET requests are allowed.
func (s *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := HealthResponse{
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
func (s *HealthServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.checker.GetStatus()
	checks := make(map[string]interface{})

	// Check ICAP server status
	if status.ICAPError != "" {
		checks["icap_server"] = fmt.Sprintf("error: %s", status.ICAPError)
	} else if status.ICAPReady {
		checks["icap_server"] = "ok"
	} else {
		checks["icap_server"] = "starting"
	}

	// Check storage status
	if status.StorageError != "" {
		checks["storage"] = fmt.Sprintf("error: %s", status.StorageError)
	} else if status.StorageReady {
		checks["storage"] = "ok"
	} else {
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
