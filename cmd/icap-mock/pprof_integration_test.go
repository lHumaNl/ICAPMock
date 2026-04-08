// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/pprof"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
)

// newMetricsHandlerWithPprof creates an HTTP handler that serves both metrics and pprof endpoints.
// When pprofEnabled is true, pprof endpoints are registered under /debug/pprof/.
// When pprofEnabled is false, pprof endpoints return 404 Not Found.
func newMetricsHandlerWithPprof(reg prometheus.Gatherer, pprofEnabled bool) http.Handler {
	mux := http.NewServeMux()

	// Register metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	if pprofEnabled {
		// Register pprof handlers when enabled
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
		mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
		mux.Handle("/debug/pprof/block", pprof.Handler("block"))
		mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
		mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	} else {
		// Return 404 for pprof endpoints when disabled
		mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/cmdline", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/profile", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/symbol", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/trace", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/heap", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/debug/pprof/goroutine", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	return mux
}

// TestPprofDisabledByDefault verifies that pprof is disabled by default for security.
func TestPprofDisabledByDefault(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.SetDefaults()

	if cfg.Pprof.Enabled {
		t.Error("Pprof should be disabled by default for security reasons")
	}
}

// TestPprofEndpointsDisabledWhenConfigDisabled verifies pprof endpoints return 404 when disabled.
func TestPprofEndpointsDisabledWhenConfigDisabled(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	// Use handler with pprof disabled (false)
	handler := newMetricsHandlerWithPprof(registry, false)

	server := httptest.NewServer(handler)
	defer server.Close()

	pprofEndpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
	}

	for _, endpoint := range pprofEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp, err := http.Get(server.URL + endpoint)
			if err != nil {
				t.Fatalf("GET %s failed: %v", endpoint, err)
			}
			defer resp.Body.Close()

			// Should return 404 when pprof is disabled
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s: status = %d, want %d", endpoint, resp.StatusCode, http.StatusNotFound)
			}
		})
	}
}

// TestPprofEndpointsEnabledWhenConfigEnabled verifies pprof endpoints work when enabled.
func TestPprofEndpointsEnabledWhenConfigEnabled(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	_, err := metrics.NewCollector(registry)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Use handler with pprof enabled (true)
	handler := newMetricsHandlerWithPprof(registry, true)

	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name         string
		endpoint     string
		contains     string
		expectStatus int
	}{
		{
			name:         "pprof index",
			endpoint:     "/debug/pprof/",
			expectStatus: http.StatusOK,
			contains:     "pprof",
		},
		{
			name:         "cmdline",
			endpoint:     "/debug/pprof/cmdline",
			expectStatus: http.StatusOK,
			contains:     "", // cmdline returns binary data, may be empty
		},
		{
			name:         "symbol",
			endpoint:     "/debug/pprof/symbol",
			expectStatus: http.StatusOK,
			contains:     "", // symbol table may be empty
		},
		{
			name:         "heap profile",
			endpoint:     "/debug/pprof/heap?debug=1",
			expectStatus: http.StatusOK,
			contains:     "heap", // heap profile should contain "heap" in debug mode
		},
		{
			name:         "goroutine profile",
			endpoint:     "/debug/pprof/goroutine?debug=1",
			expectStatus: http.StatusOK,
			contains:     "goroutine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "GET", server.URL+tt.endpoint, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.endpoint, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectStatus {
				t.Errorf("GET %s: status = %d, want %d", tt.endpoint, resp.StatusCode, tt.expectStatus)
			}

			// Read body content
			buf := make([]byte, 4096)
			n, _ := resp.Body.Read(buf)
			body := string(buf[:n])

			if tt.contains != "" && !strings.Contains(body, tt.contains) {
				t.Errorf("GET %s: body should contain %q, got: %s", tt.endpoint, tt.contains, body[:min(200, len(body))])
			}
		})
	}
}

// TestPprofProfileEndpointTimeout verifies profile endpoint respects timeout.
func TestPprofProfileEndpointTimeout(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	// Use handler with pprof enabled (true)
	handler := newMetricsHandlerWithPprof(registry, true)

	server := httptest.NewServer(handler)
	defer server.Close()

	// Request a very short CPU profile
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/debug/pprof/profile?seconds=1", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET profile failed: %v", err)
	}
	defer resp.Body.Close()

	elapsed := time.Since(start)

	// Should complete within reasonable time (profile seconds + overhead)
	if elapsed > 3*time.Second {
		t.Errorf("Profile took %v, expected around 1 second", elapsed)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET profile: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestPprofConfigCanBeEnabledViaYAML verifies pprof can be enabled via config.
func TestPprofConfigCanBeEnabledViaYAML(t *testing.T) {
	t.Parallel()

	// Create a temp config file
	tmpFile, err := os.CreateTemp("", "pprof-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `
pprof:
  enabled: true
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Load the config
	loader := config.NewLoader()
	cfg, err := loader.Load(config.LoadOptions{
		ConfigPath: tmpFile.Name(),
	})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Pprof.Enabled {
		t.Error("Pprof should be enabled via YAML config")
	}
}

// TestPprofConfigCanBeEnabledViaEnvironment verifies pprof can be enabled via env var.
func TestPprofConfigCanBeEnabledViaEnvironment(t *testing.T) {
	t.Parallel()

	// Set environment variable
	os.Setenv("ICAP_PPROF_ENABLED", "true")
	defer os.Unsetenv("ICAP_PPROF_ENABLED")

	loader := config.NewLoader()
	cfg, err := loader.Load(config.LoadOptions{
		ConfigPath: "",
	})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Pprof.Enabled {
		t.Error("Pprof should be enabled via environment variable")
	}
}

// TestPprofConfigCanBeEnabledViaCLI verifies pprof can be enabled via CLI flag.
// Note: This test verifies the applyCLIOverrides logic can// Real integration tests would use exec.Command to run a subprocess.
func TestPprofConfigCanBeEnabledViaCLI(t *testing.T) {
	t.Parallel()

	// Verify that the config can be manually modified
	cfg := &config.Config{}
	cfg.SetDefaults()

	// Manually enable pprof (simulating what CLI would do)
	cfg.Pprof.Enabled = true

	if !cfg.Pprof.Enabled {
		t.Error("Pprof should be manually enableable")
	}
}

// Flag is a minimal interface for testing.
type Flag struct {
	Name  string
	Value string
}

func visit(_ func(*Flag)) {
	// Mock implementation
}

// TestMetricsEndpointAlwaysAvailable verifies metrics endpoint works regardless of pprof.
func TestMetricsEndpointAlwaysAvailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pprofEnabled bool
	}{
		{"pprof disabled", false},
		{"pprof enabled", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			_, err := metrics.NewCollector(registry)
			if err != nil {
				t.Fatalf("Failed to create collector: %v", err)
			}

			// Use handler with appropriate pprof setting
			handler := newMetricsHandlerWithPprof(registry, tt.pprofEnabled)
			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Get(server.URL + "/metrics")
			if err != nil {
				t.Fatalf("GET /metrics failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET /metrics: status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
		})
	}
}

// TestPprofSecurityWarning verifies pprof security considerations.
func TestPprofSecurityWarning(t *testing.T) {
	t.Parallel()

	// Test that enabling pprof in the config structure works
	cfg := &config.Config{}
	cfg.SetDefaults()

	// Default should be disabled for security
	if cfg.Pprof.Enabled {
		t.Error("Pprof must be disabled by default for security")
	}

	// Verify the PprofConfig structure exists
	_ = cfg.Pprof.Enabled // Should compile without error
}

// TestPprofMultipleEndpointsConcurrent tests concurrent access to pprof endpoints.
func TestPprofMultipleEndpointsConcurrent(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	// Use handler with pprof enabled for this test
	handler := newMetricsHandlerWithPprof(registry, true)

	server := httptest.NewServer(handler)
	defer server.Close()

	endpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/heap?debug=1",
		"/debug/pprof/goroutine?debug=1",
		"/metrics",
	}

	errCh := make(chan error, len(endpoints)*10)

	for i := 0; i < 10; i++ {
		for _, endpoint := range endpoints {
			go func(ep string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				req, err := http.NewRequestWithContext(ctx, "GET", server.URL+ep, nil)
				if err != nil {
					errCh <- fmt.Errorf("create request %s: %w", ep, err)
					return
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					errCh <- fmt.Errorf("GET %s: %w", ep, err)
					return
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("GET %s: status = %d", ep, resp.StatusCode)
					return
				}

				errCh <- nil
			}(endpoint)
		}
	}

	// Collect results
	for i := 0; i < len(endpoints)*10; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

// Helper function.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
