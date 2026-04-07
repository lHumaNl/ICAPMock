// Copyright 2026 ICAP Mock

package config_test

import (
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
)

func TestPprofConfig_Defaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()

	// Pprof should be disabled by default for security
	if cfg.Pprof.Enabled {
		t.Error("Pprof should be disabled by default for security")
	}
}

func TestPprofConfig_Structure(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetDefaults()

	// Verify PprofConfig structure exists
	if cfg.Pprof.Enabled != false {
		t.Errorf("Expected Pprof.Enabled to be false, got %v", cfg.Pprof.Enabled)
	}
}

func TestPprofConfig_YAML(t *testing.T) {
	loader := config.NewLoader()

	cfg, err := loader.Load(config.LoadOptions{
		ConfigPath: "",
	})
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Pprof should be disabled by default
	if cfg.Pprof.Enabled {
		t.Error("Pprof should be disabled by default")
	}
}
