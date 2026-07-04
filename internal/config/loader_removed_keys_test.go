// Copyright 2026 ICAP Mock

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoader_LoadFromFileRejectsRemovedTopLevelConfigKeys(t *testing.T) {
	for _, key := range removedConfigKeysForTest() {
		for _, format := range removedKeyFormatsForTest(key) {
			t.Run(format.name, func(t *testing.T) {
				path := writeConfigFile(t, format.pathExt, format.content)
				_, err := NewLoader().LoadFromFile(path)
				if err == nil {
					t.Fatal("LoadFromFile() error = nil, want removed section error")
				}
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("LoadFromFile() error = %v, want key %q", err, key)
				}
			})
		}
	}
}

func removedConfigKeysForTest() []string {
	return []string{
		"chaos",
		"rate_limit",
		"per_client_rate_limit",
		"per_method_rate_limit",
		"preview",
		"plugin",
		"replay",
		"circuit_breaker",
	}
}

func TestLoader_LoadFromFileRejectsRemovedNestedConfigKeys(t *testing.T) {
	for _, format := range removedNestedKeyFormatsForTest() {
		t.Run(format.name, func(t *testing.T) {
			path := writeConfigFile(t, format.pathExt, format.content)
			_, err := NewLoader().LoadFromFile(path)
			if err == nil {
				t.Fatal("LoadFromFile() error = nil, want removed nested section error")
			}
			if !strings.Contains(err.Error(), format.key) {
				t.Fatalf("LoadFromFile() error = %v, want %s", err, format.key)
			}
		})
	}
}

type removedKeyFormat struct {
	name    string
	pathExt string
	content string
	key     string
}

func removedKeyFormatsForTest(key string) []removedKeyFormat {
	return []removedKeyFormat{
		{name: "yaml " + key, pathExt: ".yaml", content: fmt.Sprintf("%s:\n  enabled: true\n", key), key: key},
		{name: "json " + key, pathExt: ".json", content: fmt.Sprintf(`{%q:{"enabled":true}}`, key), key: key},
	}
}

func removedNestedKeyFormatsForTest() []removedKeyFormat {
	return []removedKeyFormat{
		{name: "yaml storage.circuit_breaker", pathExt: ".yaml", content: "storage:\n  circuit_breaker:\n    enabled: true\n", key: "storage.circuit_breaker"},
		{name: "json storage.circuit_breaker", pathExt: ".json", content: `{"storage":{"circuit_breaker":{"enabled":true}}}`, key: "storage.circuit_breaker"},
		{name: "yaml server.trust_client_ip_header", pathExt: ".yaml", content: "server:\n  trust_client_ip_header: true\n", key: "server.trust_client_ip_header"},
		{name: "json server.trust_client_ip_header", pathExt: ".json", content: `{"server":{"trust_client_ip_header":true}}`, key: "server.trust_client_ip_header"},
		{name: "yaml server.trusted_proxies", pathExt: ".yaml", content: "server:\n  trusted_proxies: [\"127.0.0.1\"]\n", key: "server.trusted_proxies"},
		{name: "json server.trusted_proxies", pathExt: ".json", content: `{"server":{"trusted_proxies":["127.0.0.1"]}}`, key: "server.trusted_proxies"},
		{name: "yaml mock.default_mode", pathExt: ".yaml", content: "mock:\n  default_mode: mock\n", key: "mock.default_mode"},
		{name: "json mock.default_mode", pathExt: ".json", content: `{"mock":{"default_mode":"mock"}}`, key: "mock.default_mode"},
		{name: "yaml servers.main.trust_client_ip_header", pathExt: ".yaml", content: "servers:\n  main:\n    trust_client_ip_header: true\n", key: "servers.main.trust_client_ip_header"},
		{name: "json servers.main.trust_client_ip_header", pathExt: ".json", content: `{"servers":{"main":{"trust_client_ip_header":true}}}`, key: "servers.main.trust_client_ip_header"},
		{name: "yaml servers.main.trusted_proxies", pathExt: ".yaml", content: "servers:\n  main:\n    trusted_proxies: [\"127.0.0.1\"]\n", key: "servers.main.trusted_proxies"},
		{name: "json servers.main.trusted_proxies", pathExt: ".json", content: `{"servers":{"main":{"trusted_proxies":["127.0.0.1"]}}}`, key: "servers.main.trusted_proxies"},
	}
}

func TestLoader_LoadFromEnvRejectsRemovedEnvVars(t *testing.T) {
	for _, name := range removedEnvVarsForTest() {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "true")
			cfg := &Config{}
			cfg.SetDefaults()
			err := NewLoader().LoadFromEnv(cfg)
			if err == nil {
				t.Fatal("LoadFromEnv() error = nil, want removed environment variable error")
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("LoadFromEnv() error = %v, want %s", err, name)
			}
		})
	}
}

func removedEnvVarsForTest() []string {
	return []string{
		"ICAP_SERVER_TRUST_CLIENT_IP_HEADER",
		"ICAP_SERVER_TRUSTED_PROXIES",
		"ICAP_MOCK_DEFAULT_MODE",
		"ICAP_REPLAY_ENABLED",
		"ICAP_CHAOS_ENABLED",
		"ICAP_PLUGIN_PATH",
		"ICAP_RATE_LIMIT_ENABLED",
	}
}

func writeConfigFile(t *testing.T, pathExt, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config"+pathExt)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}
