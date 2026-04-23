// Copyright 2026 ICAP Mock

package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// stringList decodes a YAML field that accepts either a single scalar string
// or a sequence of strings. It's used as the underlying representation for
// MethodList and EndpointList (which mirror the storage-package equivalents
// locally to avoid circular imports). Empty value decodes to nil.
type stringList []string

func (s *stringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind { //nolint:exhaustive // DocumentNode/MappingNode/AliasNode never appear here; other shapes fall to default.
	case yaml.ScalarNode:
		if node.Value == "" {
			*s = nil
			return nil
		}
		*s = stringList{node.Value}
		return nil
	case yaml.SequenceNode:
		list := make(stringList, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("list item must be a string, got %v", item.Kind)
			}
			if item.Value != "" {
				list = append(list, item.Value)
			}
		}
		*s = list
		return nil
	default:
		return fmt.Errorf("value must be a string or a list of strings, got %v", node.Kind)
	}
}

func (s stringList) marshalYAML() (interface{}, error) {
	switch len(s) {
	case 0:
		return nil, nil //nolint:nilnil // YAML marshaler uses (nil, nil) to omit the field
	case 1:
		return s[0], nil
	default:
		return []string(s), nil
	}
}

// MethodList accepts a single ICAP method ("REQMOD") or a list
// (["REQMOD", "RESPMOD"]) in YAML. Empty list means "any method".
type MethodList stringList

// UnmarshalYAML delegates to stringList.
func (m *MethodList) UnmarshalYAML(node *yaml.Node) error {
	return (*stringList)(m).UnmarshalYAML(node)
}

// MarshalYAML delegates to stringList.
func (m MethodList) MarshalYAML() (interface{}, error) {
	return stringList(m).marshalYAML()
}

// EndpointList accepts a single ICAP endpoint path ("/scan") or a list in
// YAML (e.g. ["/a", "/b/{id}"]). Empty list means "no endpoint set".
type EndpointList stringList

// UnmarshalYAML delegates to stringList.
func (e *EndpointList) UnmarshalYAML(node *yaml.Node) error {
	return (*stringList)(e).UnmarshalYAML(node)
}

// MarshalYAML delegates to stringList.
func (e EndpointList) MarshalYAML() (interface{}, error) {
	return stringList(e).marshalYAML()
}
