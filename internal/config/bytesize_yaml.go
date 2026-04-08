// Copyright 2026 ICAP Mock

package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// unmarshalYAMLWithByteSize handles YAML unmarshaling for structs that contain a byte-size
// field (e.g. "10MB") which may be encoded as a string instead of an integer.
// It tries normal decoding first, then falls back to parsing the named field as a byte size.
func unmarshalYAMLWithByteSize(value *yaml.Node, alias interface{}, fieldName string) error {
	if err := value.Decode(alias); err != nil {
		var raw map[string]yaml.Node
		if mapErr := value.Decode(&raw); mapErr != nil {
			return err
		}

		if node, ok := raw[fieldName]; ok && node.Tag == "!!str" {
			size, parseErr := ParseByteSize(node.Value)
			if parseErr != nil {
				return fmt.Errorf("invalid %s: %w", fieldName, parseErr)
			}
			node.Tag = "!!int"
			node.Value = fmt.Sprintf("%d", size)
			raw[fieldName] = node

			fixedBytes, _ := yaml.Marshal(raw)
			var fixedNode yaml.Node
			if yamlErr := yaml.Unmarshal(fixedBytes, &fixedNode); yamlErr != nil {
				return err
			}
			if yamlErr := fixedNode.Decode(alias); yamlErr != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

// UnmarshalYAML implements custom YAML unmarshaling for ServerConfig.
// It handles MaxBodySize as either a number or a human-readable string like "10MB".
func (c *ServerConfig) UnmarshalYAML(value *yaml.Node) error {
	type Alias ServerConfig
	return unmarshalYAMLWithByteSize(value, (*Alias)(c), "max_body_size")
}

// UnmarshalYAML implements custom YAML unmarshaling for StorageConfig.
// It handles MaxFileSize as either a number or a human-readable string like "100MB".
func (c *StorageConfig) UnmarshalYAML(value *yaml.Node) error {
	type Alias StorageConfig
	return unmarshalYAMLWithByteSize(value, (*Alias)(c), "max_file_size")
}
