// Copyright 2026 ICAP Mock

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMockMatchingDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()

	if cfg.Mock.Matching.BodyPatternLimit.Bytes != DefaultBodyPatternLimitBytes {
		t.Fatalf("BodyPatternLimit = %d, want %d", cfg.Mock.Matching.BodyPatternLimit.Bytes, DefaultBodyPatternLimitBytes)
	}
	if cfg.Mock.Matching.BodyPatternLimit.Unlimited {
		t.Fatal("BodyPatternLimit.Unlimited = true, want false")
	}
	if cfg.Mock.Matching.BodyPatternLimitAction != BodyPatternLimitActionNoMatch {
		t.Fatalf("BodyPatternLimitAction = %s, want no_match", cfg.Mock.Matching.BodyPatternLimitAction)
	}
}

func TestMockMatchingYAMLFiniteAndUnlimited(t *testing.T) {
	tests := []struct {
		name      string
		limitYAML string
		wantBytes int64
		wantUnlim bool
	}{
		{name: "finite size", limitYAML: "5mb", wantBytes: 5 * 1024 * 1024},
		{name: "unlimited", limitYAML: "unlimited", wantUnlim: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadMockMatchingYAML(t, tt.limitYAML, BodyPatternLimitActionError)
			if cfg.Mock.Matching.BodyPatternLimit.Bytes != tt.wantBytes {
				t.Fatalf("Bytes = %d, want %d", cfg.Mock.Matching.BodyPatternLimit.Bytes, tt.wantBytes)
			}
			if cfg.Mock.Matching.BodyPatternLimit.Unlimited != tt.wantUnlim {
				t.Fatalf("Unlimited = %v, want %v", cfg.Mock.Matching.BodyPatternLimit.Unlimited, tt.wantUnlim)
			}
		})
	}
}

func TestMockMatchingActionNormalizesMixedCase(t *testing.T) {
	cfg := loadMockMatchingYAML(t, "5mb", "No_Match")
	if cfg.Mock.Matching.BodyPatternLimitAction != BodyPatternLimitActionNoMatch {
		t.Fatalf("BodyPatternLimitAction = %s, want no_match", cfg.Mock.Matching.BodyPatternLimitAction)
	}
}

func TestServerMaxBodySizeExplicitZeroOverridesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  max_body_size: 0\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.MaxBodySize != 0 {
		t.Fatalf("Server.MaxBodySize = %d, want 0", cfg.Server.MaxBodySize)
	}
}

func TestMultiServerDefaultsMaxBodySizeExplicitZero(t *testing.T) {
	cfg := loadConfigYAML(t, "defaults:\n  max_body_size: 0\n"+multiServerYAML("av", ""))
	serverCfg := multiServerConfig(cfg, "av")
	if serverCfg.MaxBodySize != 0 {
		t.Fatalf("MaxBodySize = %d, want explicit unlimited zero", serverCfg.MaxBodySize)
	}
}

func TestMultiServerEntryMaxBodySizeExplicitZero(t *testing.T) {
	cfg := loadConfigYAML(t, "defaults:\n  max_body_size: 1024\n"+multiServerYAML("av", "    max_body_size: 0\n"))
	serverCfg := multiServerConfig(cfg, "av")
	if serverCfg.MaxBodySize != 0 {
		t.Fatalf("MaxBodySize = %d, want server override zero", serverCfg.MaxBodySize)
	}
}

func TestMultiServerEntryMaxBodySizeOverridesDefaults(t *testing.T) {
	cfg := loadConfigYAML(t, "defaults:\n  max_body_size: 1024\n"+multiServerYAML("av", "    max_body_size: 2048\n"))
	serverCfg := multiServerConfig(cfg, "av")
	if serverCfg.MaxBodySize != 2048 {
		t.Fatalf("MaxBodySize = %d, want 2048", serverCfg.MaxBodySize)
	}
}

func TestMockMatchingValidation(t *testing.T) {
	tests := []struct {
		name      string
		limit     BodySizeLimit
		action    string
		wantField string
	}{
		{name: "zero limit", limit: NewBodySizeLimit(0), action: BodyPatternLimitActionNoMatch, wantField: "mock.matching.body_pattern_limit"},
		{name: "negative limit", limit: NewBodySizeLimit(-1), action: BodyPatternLimitActionNoMatch, wantField: "mock.matching.body_pattern_limit"},
		{name: "invalid action", limit: NewBodySizeLimit(1024), action: "drop", wantField: "mock.matching.body_pattern_limit_action"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.SetDefaults()
			cfg.Mock.Matching.BodyPatternLimit = tt.limit
			cfg.Mock.Matching.BodyPatternLimitAction = tt.action
			assertValidationField(t, NewValidator().Validate(cfg), tt.wantField)
		})
	}
}

func TestMockMatchingInvalidSizeString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "mock:\n  matching:\n    body_pattern_limit: nope\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewLoader().Load(LoadOptions{ConfigPath: path}); err == nil {
		t.Fatal("Load() error = nil, want invalid body_pattern_limit error")
	}
}

func TestEffectiveBodyPatternLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     BodySizeLimit
		serverMax int64
		wantBytes int64
		wantUnlim bool
	}{
		{name: "finite min server", limit: NewBodySizeLimit(10), serverMax: 4, wantBytes: 4},
		{name: "finite keeps lower limit", limit: NewBodySizeLimit(4), serverMax: 10, wantBytes: 4},
		{name: "finite ignores non-positive server", limit: NewBodySizeLimit(4), wantBytes: 4},
		{name: "unlimited capped by server", limit: NewUnlimitedBodySizeLimit(), serverMax: 8, wantBytes: 8},
		{name: "unlimited remains unlimited", limit: NewUnlimitedBodySizeLimit(), wantUnlim: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveBodyPatternLimit(tt.limit, tt.serverMax)
			if got.Bytes != tt.wantBytes || got.Unlimited != tt.wantUnlim {
				t.Fatalf("EffectiveBodyPatternLimit() = {%d %v}, want {%d %v}", got.Bytes, got.Unlimited, tt.wantBytes, tt.wantUnlim)
			}
		})
	}
}

func loadMockMatchingYAML(t *testing.T, limit, action string) *Config {
	t.Helper()
	return loadConfigYAML(t, "mock:\n  matching:\n    body_pattern_limit: "+limit+"\n    body_pattern_limit_action: "+action+"\n")
}

func loadConfigYAML(t *testing.T, content string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := NewLoader().Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func multiServerYAML(name, extra string) string {
	return "servers:\n  " + name + ":\n    port: 1345\n    scenarios_dir: ./configs/scenarios\n" + extra
}

func multiServerConfig(cfg *Config, name string) ServerConfig {
	entry := cfg.Servers[name]
	return entry.ToServerConfig(cfg.Defaults)
}

func assertValidationField(t *testing.T, errors []ValidationError, field string) {
	t.Helper()
	for _, err := range errors {
		if err.Field == field {
			return
		}
	}
	t.Fatalf("validation errors %v do not include %s", errors, field)
}
