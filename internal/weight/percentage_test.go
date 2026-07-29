// Copyright 2026 ICAP Mock

package weight

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRoundsToThreeDecimals(t *testing.T) {
	tests := map[string]Percentage{
		"80.125":  80125,
		"0.001":   1,
		"1.2344":  1234,
		"1.2345":  1235,
		"99.9999": 100000,
		"100":     100000,
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != want {
				t.Fatalf("Parse() = %d, want %d", got, want)
			}
		})
	}
}

func TestParseRejectsInvalidWeights(t *testing.T) {
	for _, input := range []string{"", "0", "0.0004", "-1", "+1", "+-50", "100.001", "101", "1e2", "abc"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("Parse(%q) error = nil, want validation error", input)
			}
		})
	}
}

func TestPercentageYAMLAndJSON(t *testing.T) {
	type document struct {
		Weight Percentage `yaml:"weight" json:"weight"`
	}
	var yamlDoc document
	if err := yaml.Unmarshal([]byte("weight: 12.3456\n"), &yamlDoc); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if yamlDoc.Weight != 12346 {
		t.Fatalf("YAML weight = %d, want 12346", yamlDoc.Weight)
	}

	var jsonDoc document
	if err := json.Unmarshal([]byte(`{"weight":0.125}`), &jsonDoc); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if jsonDoc.Weight != 125 {
		t.Fatalf("JSON weight = %d, want 125", jsonDoc.Weight)
	}
}
