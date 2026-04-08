// Copyright 2026 ICAP Mock

package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
)

// TestNewHealthChecker tests creating a new health checker.
func TestNewHealthChecker(t *testing.T) {
	checker := NewHealthChecker()
	if checker == nil {
		t.Fatal("NewHealthChecker() returned nil")
	}

	// Initially, both should be not ready
	status := checker.GetStatus()
	if status.ICAPReady {
		t.Error("ICAP should not be ready initially")
	}
	if status.StorageReady {
		t.Error("Storage should not be ready initially")
	}
}

// TestHealthChecker_SetICAPReady tests setting ICAP ready status.
func TestHealthChecker_SetICAPReady(t *testing.T) {
	checker := NewHealthChecker()

	// Set ready
	checker.SetICAPReady(true)
	if !checker.IsICAPReady() {
		t.Error("ICAP should be ready after SetICAPReady(true)")
	}

	// Set not ready
	checker.SetICAPReady(false)
	if checker.IsICAPReady() {
		t.Error("ICAP should not be ready after SetICAPReady(false)")
	}
}

// TestHealthChecker_SetStorageReady tests setting storage ready status.
func TestHealthChecker_SetStorageReady(t *testing.T) {
	checker := NewHealthChecker()

	// Set ready
	checker.SetStorageReady(true)
	if !checker.IsStorageReady() {
		t.Error("Storage should be ready after SetStorageReady(true)")
	}

	// Set not ready
	checker.SetStorageReady(false)
	if checker.IsStorageReady() {
		t.Error("Storage should not be ready after SetStorageReady(false)")
	}
}

// TestHealthChecker_SetScenariosCount tests setting scenarios count.
func TestHealthChecker_SetScenariosCount(t *testing.T) {
	checker := NewHealthChecker()

	checker.SetScenariosCount(15)
	status := checker.GetStatus()
	if status.ScenariosCount != 15 {
		t.Errorf("ScenariosCount = %d, want 15", status.ScenariosCount)
	}

	checker.SetScenariosCount(0)
	status = checker.GetStatus()
	if status.ScenariosCount != 0 {
		t.Errorf("ScenariosCount = %d, want 0", status.ScenariosCount)
	}
}

// TestHealthChecker_SetStorageError tests setting storage error.
func TestHealthChecker_SetStorageError(t *testing.T) {
	checker := NewHealthChecker()

	// Set error
	checker.SetStorageError("disk full")
	status := checker.GetStatus()
	if status.StorageError != "disk full" {
		t.Errorf("StorageError = %q, want %q", status.StorageError, "disk full")
	}

	// Clear error
	checker.SetStorageError("")
	status = checker.GetStatus()
	if status.StorageError != "" {
		t.Errorf("StorageError = %q, want empty", status.StorageError)
	}
}

// TestHealthChecker_SetICAPError tests setting ICAP error.
func TestHealthChecker_SetICAPError(t *testing.T) {
	checker := NewHealthChecker()

	// Set error
	checker.SetICAPError("binding failed")
	status := checker.GetStatus()
	if status.ICAPError != "binding failed" {
		t.Errorf("ICAPError = %q, want %q", status.ICAPError, "binding failed")
	}

	// Clear error
	checker.SetICAPError("")
	status = checker.GetStatus()
	if status.ICAPError != "" {
		t.Errorf("ICAPError = %q, want empty", status.ICAPError)
	}
}

// TestHealthChecker_IsReady tests overall readiness.
func TestHealthChecker_IsReady(t *testing.T) {
	checker := NewHealthChecker()

	// Initially not ready
	if checker.IsReady() {
		t.Error("Should not be ready initially")
	}

	// Only ICAP ready
	checker.SetICAPReady(true)
	if checker.IsReady() {
		t.Error("Should not be ready with only ICAP ready")
	}

	// Both ready
	checker.SetStorageReady(true)
	if !checker.IsReady() {
		t.Error("Should be ready when both ICAP and Storage are ready")
	}

	// Storage has error
	checker.SetStorageError("some error")
	if checker.IsReady() {
		t.Error("Should not be ready when Storage has error")
	}

	// Clear error
	checker.SetStorageError("")
	if !checker.IsReady() {
		t.Error("Should be ready when error is cleared")
	}
}

// TestHealthChecker_ConcurrentAccess tests thread safety.
func TestHealthChecker_ConcurrentAccess(_ *testing.T) {
	checker := NewHealthChecker()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(val bool) {
			defer wg.Done()
			checker.SetICAPReady(val)
		}(i%2 == 0)
		go func(val bool) {
			defer wg.Done()
			checker.SetStorageReady(val)
		}(i%2 == 0)
		go func(val int) {
			defer wg.Done()
			checker.SetScenariosCount(val)
		}(i)
	}

	wg.Wait()
	// If we get here without race condition, test passes
}

// TestNewHealthServer tests creating a new health server.
func TestNewHealthServer(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("NewHealthServer() returned nil")
	}
}

// TestNewHealthServer_NilConfig tests that nil config returns error.
func TestNewHealthServer_NilConfig(t *testing.T) {
	_, err := NewHealthServer(nil)
	if err == nil {
		t.Error("NewHealthServer(nil) should return error")
	}
}

// TestHealthServer_GetChecker tests getting the checker.
func TestHealthServer_GetChecker(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	checker := server.Checker()
	if checker == nil {
		t.Error("Checker() returned nil")
	}
}

// TestHealthServer_HealthEndpoint tests the /health endpoint.
func TestHealthServer_HealthEndpoint(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Check content type
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Parse response
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("Status = %q, want %q", resp.Status, "healthy")
	}

	if resp.Time.IsZero() {
		t.Error("Time should not be zero")
	}
}

// TestHealthServer_ReadyEndpoint_Ready tests the /ready endpoint when ready.
func TestHealthServer_ReadyEndpoint_Ready(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	// Mark as ready
	server.Checker().SetICAPReady(true)
	server.Checker().SetStorageReady(true)
	server.Checker().SetScenariosCount(15)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	server.handleReady(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("handleReady() status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse response
	var resp ReadyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "ready" {
		t.Errorf("Status = %q, want %q", resp.Status, "ready")
	}

	if resp.Checks["icap_server"] != "ok" {
		t.Errorf("icap_server = %q, want %q", resp.Checks["icap_server"], "ok")
	}

	if resp.Checks["storage"] != "ok" {
		t.Errorf("storage = %q, want %q", resp.Checks["storage"], "ok")
	}

	if resp.Checks["scenarios_loaded"].(float64) != 15 {
		t.Errorf("scenarios_loaded = %v, want 15", resp.Checks["scenarios_loaded"])
	}
}

// TestHealthServer_ReadyEndpoint_NotReady tests the /ready endpoint when not ready.
func TestHealthServer_ReadyEndpoint_NotReady(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	// Don't mark as ready - test default state

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	server.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("handleReady() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// Parse response
	var resp ReadyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Errorf("Status = %q, want %q", resp.Status, "not_ready")
	}

	if resp.Checks["icap_server"] != "starting" {
		t.Errorf("icap_server = %q, want %q", resp.Checks["icap_server"], "starting")
	}

	if resp.Checks["storage"] != "starting" {
		t.Errorf("storage = %q, want %q", resp.Checks["storage"], "starting")
	}
}

// TestHealthServer_ReadyEndpoint_WithErrors tests the /ready endpoint with errors.
func TestHealthServer_ReadyEndpoint_WithErrors(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	// Mark with errors
	server.Checker().SetICAPReady(false)
	server.Checker().SetICAPError("binding failed")
	server.Checker().SetStorageReady(false)
	server.Checker().SetStorageError("disk full")

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	server.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("handleReady() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// Parse response
	var resp ReadyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Errorf("Status = %q, want %q", resp.Status, "not_ready")
	}

	// Check error format
	icapStatus, ok := resp.Checks["icap_server"].(string)
	if !ok || !strings.Contains(icapStatus, "error:") {
		t.Errorf("icap_server should contain 'error:', got %v", resp.Checks["icap_server"])
	}

	storageStatus, ok := resp.Checks["storage"].(string)
	if !ok || !strings.Contains(storageStatus, "error:") {
		t.Errorf("storage should contain 'error:', got %v", resp.Checks["storage"])
	}
}

// TestHealthServer_StartStop tests starting and stopping the server.
func TestHealthServer_StartStop(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       18080, // Use non-standard port to avoid conflicts
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	ctx := context.Background()

	// Start server
	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Make a request to verify server is running
	resp, err := http.Get("http://localhost:18080/health")
	if err != nil {
		t.Fatalf("Failed to connect to health server: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Stop server
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify Start returned
	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start() did not return after Stop()")
	}
}

// TestHealthServer_StopWithoutStart tests stopping without starting.
func TestHealthServer_StopWithoutStart(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should not error when stopping without starting
	if err := server.Stop(ctx); err != nil {
		t.Errorf("Stop() without Start() error = %v", err)
	}
}

// TestHealthServer_Disabled tests that disabled server doesn't start.
func TestHealthServer_Disabled(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    false,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	ctx := context.Background()

	// Start should return immediately for disabled server
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() for disabled server error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Start() should return immediately for disabled server")
	}
}

// TestHealthServer_CustomPaths tests custom health and ready paths.
func TestHealthServer_CustomPaths(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       18081,
		HealthPath: "/healthz",
		ReadyPath:  "/readyz",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	ctx := context.Background()

	// Start server
	go func() {
		_ = server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	defer server.Stop(context.Background())

	// Test custom health path
	resp, err := http.Get("http://localhost:18081/healthz")
	if err != nil {
		t.Fatalf("Failed to connect to health server: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Test custom ready path
	resp, err = http.Get("http://localhost:18081/readyz")
	if err != nil {
		t.Fatalf("Failed to connect to ready server: %v", err)
	}
	resp.Body.Close()

	// Should be 503 since not ready
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Ready endpoint status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestHealthChecker_GetStatus tests getting full status.
func TestHealthChecker_GetStatus(t *testing.T) {
	checker := NewHealthChecker()

	checker.SetICAPReady(true)
	checker.SetStorageReady(true)
	checker.SetScenariosCount(42)
	checker.SetStorageError("")
	checker.SetICAPError("")

	status := checker.GetStatus()

	if !status.ICAPReady {
		t.Error("ICAPReady should be true")
	}
	if !status.StorageReady {
		t.Error("StorageReady should be true")
	}
	if status.ScenariosCount != 42 {
		t.Errorf("ScenariosCount = %d, want 42", status.ScenariosCount)
	}
}

// TestHealthServer_MethodNotAllowed tests that non-GET methods are rejected.
func TestHealthServer_MethodNotAllowed(t *testing.T) {
	cfg := &config.HealthConfig{
		Enabled:    true,
		Port:       8080,
		HealthPath: "/health",
		ReadyPath:  "/ready",
	}

	server, err := NewHealthServer(cfg)
	if err != nil {
		t.Fatalf("NewHealthServer() error = %v", err)
	}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()

		server.handleHealth(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("handleHealth() with %s status = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}
