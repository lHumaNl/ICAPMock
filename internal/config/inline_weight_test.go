// Copyright 2026 ICAP Mock

package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInlineWeightedResponsesPreserveExplicitEmptyLists(t *testing.T) {
	for name, decode := range map[string]func(*InlineScenarioEntry) error{
		"yaml empty": func(entry *InlineScenarioEntry) error {
			return yaml.Unmarshal([]byte("responses: []\n"), entry)
		},
		"yaml null": func(entry *InlineScenarioEntry) error {
			return yaml.Unmarshal([]byte("responses: null\n"), entry)
		},
		"json empty": func(entry *InlineScenarioEntry) error {
			return json.Unmarshal([]byte(`{"responses":[]}`), entry)
		},
		"json null": func(entry *InlineScenarioEntry) error {
			return json.Unmarshal([]byte(`{"responses":null}`), entry)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var entry InlineScenarioEntry
			if err := decode(&entry); err != nil {
				t.Fatalf("decode() error = %v", err)
			}
			if entry.Responses == nil || len(entry.Responses) != 0 {
				t.Fatalf("Responses = %#v, want present empty list", entry.Responses)
			}
		})
	}
}
