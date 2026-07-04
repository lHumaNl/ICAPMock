// Copyright 2026 ICAP Mock

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const serversConfigKey = "servers"

var removedTopLevelConfigKeys = map[string]struct{}{
	"rate_limit":            {},
	"chaos":                 {},
	"per_client_rate_limit": {},
	"per_method_rate_limit": {},
	"preview":               {},
	"plugin":                {},
	"replay":                {},
	"circuit_breaker":       {},
}

var removedNestedConfigKeys = map[string]map[string]struct{}{
	"mock": {
		"default_mode": {},
	},
	"server": {
		"trust_client_ip_header": {},
		"trusted_proxies":        {},
	},
	"storage": {
		"circuit_breaker": {},
	},
}

var removedServerEntryConfigKeys = map[string]struct{}{
	"trust_client_ip_header": {},
	"trusted_proxies":        {},
}

var removedEnvVarNames = []string{
	"SERVER_TRUST_CLIENT_IP_HEADER",
	"SERVER_TRUSTED_PROXIES",
	"MOCK_DEFAULT_MODE",
	"STORAGE_CIRCUIT_BREAKER_ENABLED",
}

var removedEnvVarPrefixes = []string{
	"CHAOS",
	"CIRCUIT_BREAKER",
	"PER_CLIENT_RATE_LIMIT",
	"PER_METHOD_RATE_LIMIT",
	"PLUGIN",
	"PREVIEW",
	"RATE_LIMIT",
	"REPLAY",
}

func loadYAMLConfig(data []byte, cfg *Config) error {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return err
	}
	if err := rejectRemovedYAMLKeys(&node); err != nil {
		return err
	}
	return node.Decode(cfg)
}

func rejectRemovedYAMLKeys(node *yaml.Node) error {
	root := yamlDocumentRoot(node)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if _, removed := removedTopLevelConfigKeys[key]; removed {
			return removedConfigKeyError(key)
		}
		if err := rejectRemovedNestedYAMLKeys(key, root.Content[i+1]); err != nil {
			return err
		}
		if err := rejectRemovedServerEntryYAMLKeys(key, root.Content[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedNestedYAMLKeys(parent string, value *yaml.Node) error {
	removedChildren := removedNestedConfigKeys[parent]
	if len(removedChildren) == 0 || value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		child := value.Content[i].Value
		if _, removed := removedChildren[child]; removed {
			return removedConfigKeyError(parent + "." + child)
		}
	}
	return nil
}

func rejectRemovedServerEntryYAMLKeys(parent string, value *yaml.Node) error {
	if parent != serversConfigKey || value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		name := value.Content[i].Value
		if err := rejectRemovedServerEntryYAMLFields(name, value.Content[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedServerEntryYAMLFields(name string, value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		child := value.Content[i].Value
		if _, removed := removedServerEntryConfigKeys[child]; removed {
			return removedConfigKeyError(serverEntryConfigKey(name, child))
		}
	}
	return nil
}

func yamlDocumentRoot(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func loadJSONConfig(data []byte, cfg *Config) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := rejectRemovedJSONKeys(raw); err != nil {
		return err
	}
	return json.Unmarshal(data, cfg)
}

func rejectRemovedJSONKeys(raw map[string]json.RawMessage) error {
	for key := range raw {
		if _, removed := removedTopLevelConfigKeys[key]; removed {
			return removedConfigKeyError(key)
		}
		if err := rejectRemovedNestedJSONKeys(key, raw[key]); err != nil {
			return err
		}
		if err := rejectRemovedServerEntryJSONKeys(key, raw[key]); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedNestedJSONKeys(parent string, data json.RawMessage) error {
	removedChildren := removedNestedConfigKeys[parent]
	if len(removedChildren) == 0 {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for child := range raw {
		if _, removed := removedChildren[child]; removed {
			return removedConfigKeyError(parent + "." + child)
		}
	}
	return nil
}

func rejectRemovedServerEntryJSONKeys(parent string, data json.RawMessage) error {
	if parent != serversConfigKey {
		return nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(data, &servers); err != nil {
		return err
	}
	for name, raw := range servers {
		if err := rejectRemovedServerEntryJSONFields(name, raw); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedServerEntryJSONFields(name string, data json.RawMessage) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for child := range raw {
		if _, removed := removedServerEntryConfigKeys[child]; removed {
			return removedConfigKeyError(serverEntryConfigKey(name, child))
		}
	}
	return nil
}

func (l *Loader) rejectRemovedEnvVars() error {
	if err := l.rejectRemovedNamedEnvVars(); err != nil {
		return err
	}
	return l.rejectRemovedPrefixedEnvVars()
}

func (l *Loader) rejectRemovedNamedEnvVars() error {
	for _, key := range removedEnvVarNames {
		name := l.envPrefix + key
		if _, ok := os.LookupEnv(name); ok {
			return removedEnvVarError(name)
		}
	}
	return nil
}

func (l *Loader) rejectRemovedPrefixedEnvVars() error {
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if l.isRemovedPrefixedEnvVar(name) {
			return removedEnvVarError(name)
		}
	}
	return nil
}

func (l *Loader) isRemovedPrefixedEnvVar(name string) bool {
	for _, key := range removedEnvVarPrefixes {
		if name == l.envPrefix+key || strings.HasPrefix(name, l.envPrefix+key+"_") {
			return true
		}
	}
	return false
}

func serverEntryConfigKey(name, child string) string {
	return serversConfigKey + "." + name + "." + child
}

func removedConfigKeyError(key string) error {
	return fmt.Errorf("unsupported removed config section %q", key)
}

func removedEnvVarError(name string) error {
	return fmt.Errorf("unsupported removed environment variable %q", name)
}
