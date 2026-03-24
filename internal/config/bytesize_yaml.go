package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML implements custom YAML unmarshaling for ServerConfig.
// It handles MaxBodySize as either a number or a human-readable string like "10MB".
func (c *ServerConfig) UnmarshalYAML(value *yaml.Node) error {
	// Use an alias to avoid infinite recursion
	type Alias ServerConfig

	// First, try normal unmarshaling (handles numeric max_body_size)
	alias := (*Alias)(c)
	if err := value.Decode(alias); err != nil {
		// If it failed, it might be because max_body_size is a string
		// Try with a raw map to extract and convert the string value
		var raw map[string]yaml.Node
		if mapErr := value.Decode(&raw); mapErr != nil {
			return err // return original error
		}

		if node, ok := raw["max_body_size"]; ok && node.Tag == "!!str" {
			size, parseErr := ParseByteSize(node.Value)
			if parseErr != nil {
				return fmt.Errorf("invalid max_body_size: %w", parseErr)
			}
			// Temporarily replace the node value with the parsed int
			node.Tag = "!!int"
			node.Value = fmt.Sprintf("%d", size)
			raw["max_body_size"] = node

			// Re-encode the map to YAML and decode into alias
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

// UnmarshalYAML implements custom YAML unmarshaling for StorageConfig.
// It handles MaxFileSize as either a number or a human-readable string like "100MB".
func (c *StorageConfig) UnmarshalYAML(value *yaml.Node) error {
	type Alias StorageConfig

	alias := (*Alias)(c)
	if err := value.Decode(alias); err != nil {
		var raw map[string]yaml.Node
		if mapErr := value.Decode(&raw); mapErr != nil {
			return err
		}

		if node, ok := raw["max_file_size"]; ok && node.Tag == "!!str" {
			size, parseErr := ParseByteSize(node.Value)
			if parseErr != nil {
				return fmt.Errorf("invalid max_file_size: %w", parseErr)
			}
			node.Tag = "!!int"
			node.Value = fmt.Sprintf("%d", size)
			raw["max_file_size"] = node

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
