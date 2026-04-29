// Copyright 2026 ICAP Mock

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadManagementConfig(t *testing.T) {
	path := writeManagementConfig(t, `management:
  enabled: true
  scenario_reload_enabled: true
  config_reload_enabled: true
  token: "local-token"
  token_env: "IGNORED_TOKEN_ENV"
`)
	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Management.Enabled || !cfg.Management.ConfigReloadEnabled {
		t.Fatalf("management flags were not loaded")
	}
	if cfg.Management.ResolvedToken() != "local-token" {
		t.Fatalf("ResolvedToken() did not prefer explicit token")
	}
}

func TestManagementConfig_ResolvedTokenFromEnv(t *testing.T) {
	t.Setenv("MANAGEMENT_TEST_TOKEN", "env-token")
	cfg := ManagementConfig{TokenEnv: "MANAGEMENT_TEST_TOKEN"}
	if cfg.ResolvedToken() != "env-token" {
		t.Fatalf("ResolvedToken() did not read token_env")
	}
}

func TestLoader_ManagementDefaultsDisabled(t *testing.T) {
	cfg, err := NewLoader().Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Management.Enabled || cfg.Management.ScenarioReloadEnabled || cfg.Management.ConfigReloadEnabled {
		t.Fatalf("management defaults should be disabled")
	}
}

func TestLoader_LoadManagementTokenKeepsDefaultDisabled(t *testing.T) {
	path := writeManagementConfig(t, `management:
  token: "local-token"
`)
	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Management.Enabled || cfg.Management.ScenarioReloadEnabled {
		t.Fatalf("management token should not enable API or reload by default")
	}
}

func TestLoader_LoadManagementExplicitFalseDisablesDefaults(t *testing.T) {
	path := writeManagementConfig(t, `management:
  enabled: false
  scenario_reload_enabled: false
  config_reload_enabled: false
`)
	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Management.Enabled || cfg.Management.ScenarioReloadEnabled {
		t.Fatalf("explicit management false values should override defaults")
	}
	if cfg.Management.ConfigReloadEnabled {
		t.Fatalf("config reload should remain disabled")
	}
}

func TestLoader_LoadManagementEnvOverrides(t *testing.T) {
	t.Setenv("ICAP_MANAGEMENT_ENABLED", "true")
	t.Setenv("ICAP_MANAGEMENT_CONFIG_RELOAD_ENABLED", "true")
	t.Setenv("ICAP_MANAGEMENT_TOKEN_ENV", "MANAGEMENT_ENV_TOKEN")
	t.Setenv("MANAGEMENT_ENV_TOKEN", "resolved-token")
	cfg, err := NewLoader().Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Management.Enabled || !cfg.Management.ConfigReloadEnabled {
		t.Fatalf("management env flags were not loaded")
	}
	if cfg.Management.ResolvedToken() != "resolved-token" {
		t.Fatalf("ResolvedToken() did not resolve env configured token")
	}
}

func writeManagementConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
