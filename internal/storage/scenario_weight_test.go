// Copyright 2026 ICAP Mock

package storage

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/weight"
)

func TestWeightedResponsesRequireExactHundredPercent(t *testing.T) {
	tests := []struct {
		name    string
		weights string
		wantErr string
	}{
		{name: "valid decimals", weights: "80.125, 19.875"},
		{name: "valid after rounding", weights: "33.3334, 66.6666"},
		{name: "under total", weights: "80, 19.999", wantErr: "99.999"},
		{name: "over total", weights: "80.001, 20", wantErr: "100.001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := strings.Split(tt.weights, ", ")
			variants := make(WeightedResponseListV2, 0, len(parts))
			for _, part := range parts {
				variants = append(variants, WeightedResponseV2{Weight: weight.MustParse(part), Status: 204})
			}
			_, err := buildWeightedList("scenario test", variants, InlineResponseV2{}, &ScenarioFileV2{}, "")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("buildWeightedList() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("buildWeightedList() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestWeightedResponseYAMLRejectsInvalidWeight(t *testing.T) {
	for _, document := range []string{
		"weight: 0\nstatus: 204\n",
		"weight: 100.001\nstatus: 204\n",
	} {
		var response WeightedResponseV2
		if err := yaml.Unmarshal([]byte(document), &response); err == nil {
			t.Fatalf("yaml.Unmarshal(%q) error = nil, want validation error", document)
		}
	}

	var unweighted WeightedResponseV2
	if err := yaml.Unmarshal([]byte("status: 204\n"), &unweighted); err != nil {
		t.Fatalf("yaml.Unmarshal() unexpected error = %v", err)
	}
	if _, err := buildWeightedList(
		"scenario uniform",
		WeightedResponseListV2{unweighted},
		InlineResponseV2{},
		&ScenarioFileV2{},
		"",
	); err != nil {
		t.Fatalf("buildWeightedList() error = %v, want uniform list to be valid", err)
	}
}

func TestWeightedResponsesRejectMixedWeightedAndUniformVariants(t *testing.T) {
	err := validateV2WeightTotal("scenario mixed", WeightedResponseListV2{
		{Weight: weight.MustParse("50")},
		{},
	})
	if err == nil || !strings.Contains(err.Error(), "all response variants") {
		t.Fatalf("validateV2WeightTotal() error = %v, want mixed-mode error", err)
	}
}

func TestRuntimeWeightedResponsesRequireExactHundredPercent(t *testing.T) {
	err := validateRuntimeWeightTotal("programmatic", []WeightedResponse{
		{Weight: weight.MustParse("99.999")},
	})
	if err == nil || !strings.Contains(err.Error(), "99.999") {
		t.Fatalf("validateRuntimeWeightTotal() error = %v, want exact total error", err)
	}
}

func TestRuntimeWeightedResponsesRejectExplicitEmptyList(t *testing.T) {
	err := validateRuntimeWeightTotal("programmatic", make([]WeightedResponse, 0))
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("validateRuntimeWeightTotal() error = %v, want empty list error", err)
	}
}

func TestV2RejectsExplicitEmptyWeightedLists(t *testing.T) {
	tests := map[string]string{
		"scenario empty": `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  test:
    responses: []
`,
		"scenario null": `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  test:
    responses: null
`,
		"branch empty": `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  test:
    branches:
      - responses: []
`,
		"branches with scenario responses empty": `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  test:
    responses: []
    branches:
      - status: 204
`,
		"branches with scenario responses null": `
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  test:
    responses: null
    branches:
      - status: 204
`,
		"unused template empty": `
defaults:
  method: REQMOD
  endpoint: /scan
  response_templates:
    invalid: []
scenarios:
  test:
    status: 204
`,
		"defaults use empty template": `
defaults:
  method: REQMOD
  endpoint: /scan
  use: invalid
  response_templates:
    invalid: []
scenarios:
  test: {}
`,
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			var file ScenarioFileV2
			if err := yaml.Unmarshal([]byte(document), &file); err != nil {
				t.Fatalf("yaml.Unmarshal() unexpected error = %v", err)
			}
			if _, err := ConvertV2ToScenarios(&file, []string{"test"}); err == nil {
				t.Fatal("ConvertV2ToScenarios() error = nil, want empty weighted list error")
			}
		})
	}
}

func TestV2WeightValidationRejectsProgrammaticOutOfRangeValue(t *testing.T) {
	err := validateV2WeightTotal("programmatic", WeightedResponseListV2{
		{Weight: weight.Percentage(-50000)},
		{Weight: weight.MustParse("100")},
		{Weight: weight.MustParse("50")},
	})
	if err == nil || !strings.Contains(err.Error(), "greater than 0.000") {
		t.Fatalf("validateV2WeightTotal() error = %v, want individual bounds error", err)
	}
}
