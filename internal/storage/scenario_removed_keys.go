// Copyright 2026 ICAP Mock

package storage

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	removedScenarioScriptKey = "script"
	scenarioKeyDefaults      = "defaults"
	scenarioKeyScenarios     = "scenarios"
	scenarioKeyResponses     = "responses"
	scenarioKeyResponse      = "response"
	scenarioKeyTemplates     = "response_templates"
	scenarioKeyBranches      = "branches"
)

func rejectRemovedScenarioYAMLKeys(data []byte, path string) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return NewScenarioParseError(path, err)
	}
	if err := rejectRemovedScenarioKeysInRoot(scenarioYAMLRoot(&doc)); err != nil {
		return removedScenarioKeyError(path, err)
	}
	return nil
}

func scenarioYAMLRoot(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func rejectRemovedScenarioKeysInRoot(root *yaml.Node) error {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	return walkMapping(root, "", rejectTopLevelRemovedScenarioKey)
}

func rejectTopLevelRemovedScenarioKey(key string, value *yaml.Node, path string) error {
	switch key {
	case scenarioKeyScenarios:
		return rejectScenarioEntries(value, joinScenarioPath(path, key))
	case scenarioKeyResponses:
		return rejectResponseTemplates(value, joinScenarioPath(path, key))
	case scenarioKeyDefaults:
		return rejectScenarioDefaults(value, joinScenarioPath(path, key))
	default:
		return nil
	}
}

func rejectScenarioDefaults(node *yaml.Node, path string) error {
	return walkMapping(node, path, func(key string, value *yaml.Node, keyPath string) error {
		switch key {
		case removedScenarioScriptKey:
			return removedScenarioScriptFieldError(keyPath)
		case scenarioKeyTemplates:
			return rejectResponseTemplates(value, keyPath)
		default:
			return nil
		}
	})
}

func rejectScenarioEntries(node *yaml.Node, path string) error {
	switch node.Kind { //nolint:exhaustive // only v1 sequence and v2 mapping carry scenarios.
	case yaml.SequenceNode:
		return walkSequence(node, path, rejectScenarioEntry)
	case yaml.MappingNode:
		return walkMappingValues(node, path, rejectScenarioEntry)
	default:
		return nil
	}
}

func rejectScenarioEntry(node *yaml.Node, path string) error {
	return walkMapping(node, path, func(key string, value *yaml.Node, keyPath string) error {
		switch key {
		case removedScenarioScriptKey:
			return removedScenarioScriptFieldError(keyPath)
		case scenarioKeyResponse:
			return rejectResponseNode(value, keyPath)
		case scenarioKeyResponses:
			return rejectWeightedResponses(value, keyPath)
		case scenarioKeyBranches:
			return walkSequence(value, keyPath, rejectBranchNode)
		default:
			return nil
		}
	})
}

func rejectBranchNode(node *yaml.Node, path string) error {
	return walkMapping(node, path, func(key string, value *yaml.Node, keyPath string) error {
		switch key {
		case removedScenarioScriptKey:
			return removedScenarioScriptFieldError(keyPath)
		case scenarioKeyResponse:
			return rejectResponseNode(value, keyPath)
		case scenarioKeyResponses:
			return rejectWeightedResponses(value, keyPath)
		default:
			return nil
		}
	})
}

func rejectResponseTemplates(node *yaml.Node, path string) error {
	return walkMappingValues(node, path, rejectResponseNode)
}

func rejectResponseNode(node *yaml.Node, path string) error {
	switch node.Kind { //nolint:exhaustive // templates are mapping or sequence nodes.
	case yaml.MappingNode:
		return rejectResponseMapping(node, path)
	case yaml.SequenceNode:
		return rejectWeightedResponses(node, path)
	default:
		return nil
	}
}

func rejectResponseMapping(node *yaml.Node, path string) error {
	return walkMapping(node, path, func(key string, _ *yaml.Node, keyPath string) error {
		if key == removedScenarioScriptKey {
			return removedScenarioScriptFieldError(keyPath)
		}
		return nil
	})
}

func rejectWeightedResponses(node *yaml.Node, path string) error {
	return walkSequence(node, path, rejectResponseNode)
}

func walkMapping(
	node *yaml.Node,
	path string,
	visit func(key string, value *yaml.Node, keyPath string) error,
) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if err := visit(key, node.Content[i+1], joinScenarioPath(path, key)); err != nil {
			return err
		}
	}
	return nil
}

func walkMappingValues(node *yaml.Node, path string, visit func(*yaml.Node, string) error) error {
	return walkMapping(node, path, func(key string, value *yaml.Node, _ string) error {
		return visit(value, joinScenarioPath(path, key))
	})
}

func walkSequence(node *yaml.Node, path string, visit func(*yaml.Node, string) error) error {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	for i, item := range node.Content {
		if err := visit(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func joinScenarioPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func removedScenarioScriptFieldError(path string) error {
	return fmt.Errorf("unsupported removed scenario field %q at %s", removedScenarioScriptKey, path)
}

func removedScenarioKeyError(path string, cause error) *ScenarioError {
	return &ScenarioError{
		Operation:  "parse",
		FilePath:   path,
		Field:      removedScenarioScriptKey,
		Message:    cause.Error(),
		Cause:      cause,
		Suggestion: "remove script fields from scenario files; script processing is no longer supported",
	}
}
