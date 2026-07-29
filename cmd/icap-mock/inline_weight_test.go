// Copyright 2026 ICAP Mock

package main

import (
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
)

func TestConvertInlineScenariosRejectsExplicitEmptyResponses(t *testing.T) {
	entry := serverEntry{inlineScenarios: map[string]config.InlineScenarioEntry{
		"invalid": {
			Method:    config.MethodList{"REQMOD"},
			Endpoint:  config.EndpointList{"/scan"},
			Responses: make(config.InlineWeightedResponses, 0),
		},
	}}

	_, err := convertInlineScenarios(entry)
	if err == nil || !strings.Contains(err.Error(), "responses must contain") {
		t.Fatalf("convertInlineScenarios() error = %v, want empty responses error", err)
	}
}
