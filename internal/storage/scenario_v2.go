// Copyright 2026 ICAP Mock

package storage

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

// DelayConfig represents a static or range-based delay.
type DelayConfig struct {
	Min     time.Duration
	Max     time.Duration
	IsRange bool
}

// Duration returns the delay duration. For ranges, returns a random value in [Min, Max].
func (d DelayConfig) Duration() time.Duration {
	if !d.IsRange || d.Max <= d.Min {
		return d.Min
	}
	delta := d.Max - d.Min
	return d.Min + time.Duration(rand.Int63n(int64(delta))) //nolint:gosec // crypto not needed here
}

// MatchValue represents an exact or regex match condition.
type MatchValue struct {
	compiled *regexp.Regexp
	Raw      string
	Pattern  string
	IsRegex  bool
}

// Matches checks if the given value matches this condition.
func (m *MatchValue) Matches(value string) bool {
	if m.IsRegex {
		if m.compiled == nil {
			return false
		}
		return m.compiled.MatchString(value)
	}
	return m.Pattern == value
}

// --- v2 YAML structs ---

// ScenarioFileV2 is the top-level structure of a v2 scenario file.
type ScenarioFileV2 struct {
	Scenarios map[string]ScenarioEntryV2 `yaml:"scenarios"`
	Defaults  ScenarioDefaultsV2         `yaml:"defaults"`
}

// ScenarioDefaultsV2 contains default values inherited by all scenarios.
type ScenarioDefaultsV2 struct {
	Headers    map[string]string `yaml:"headers,omitempty"`
	Method     string            `yaml:"method,omitempty"`
	Endpoint   string            `yaml:"endpoint,omitempty"`
	Status     int               `yaml:"status,omitempty"`
	HTTPStatus int               `yaml:"http_status,omitempty"`
}

// ScenarioEntryV2 defines a single scenario in v2 format.
type ScenarioEntryV2 struct {
	When       map[string]string    `yaml:"when,omitempty"`
	Set        map[string]string    `yaml:"set,omitempty"`
	Method     string               `yaml:"method,omitempty"`
	Endpoint   string               `yaml:"endpoint,omitempty"`
	Body       string               `yaml:"body,omitempty"`
	BodyFile   string               `yaml:"body_file,omitempty"`
	Delay      string               `yaml:"delay,omitempty"`
	Responses  []WeightedResponseV2 `yaml:"responses,omitempty"`
	Status     int                  `yaml:"status,omitempty"`
	HTTPStatus int                  `yaml:"http_status,omitempty"`
	Priority   int                  `yaml:"priority,omitempty"`
}

// WeightedResponseV2 defines one variant in a weighted response set.
type WeightedResponseV2 struct {
	Set        map[string]string `yaml:"set,omitempty"`
	Body       string            `yaml:"body,omitempty"`
	Delay      string            `yaml:"delay,omitempty"`
	Weight     int               `yaml:"weight,omitempty"`
	Status     int               `yaml:"status,omitempty"`
	HTTPStatus int               `yaml:"http_status,omitempty"`
}

// rangePattern matches patterns like "300ms-1500ms", "1s-5s", "1m-2m".
var rangePattern = regexp.MustCompile(`^(\d+(?:ms|s|m))-(\d+(?:ms|s|m))$`)

// ParseDelay parses a delay string: "500ms" (static) or "300ms-1500ms" (range).
func ParseDelay(s string) (DelayConfig, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DelayConfig{}, fmt.Errorf("empty delay string")
	}

	// Try range first.
	if matches := rangePattern.FindStringSubmatch(s); matches != nil {
		minDur, err := time.ParseDuration(matches[1])
		if err != nil {
			return DelayConfig{}, fmt.Errorf("invalid range min %q: %w", matches[1], err)
		}
		maxDur, err := time.ParseDuration(matches[2])
		if err != nil {
			return DelayConfig{}, fmt.Errorf("invalid range max %q: %w", matches[2], err)
		}
		if minDur > maxDur {
			return DelayConfig{}, fmt.Errorf("range min %v is greater than max %v", minDur, maxDur)
		}
		return DelayConfig{Min: minDur, Max: maxDur, IsRange: true}, nil
	}

	// Try static duration.
	d, err := time.ParseDuration(s)
	if err != nil {
		return DelayConfig{}, fmt.Errorf("invalid delay %q: %w", s, err)
	}
	if d < 0 {
		return DelayConfig{}, fmt.Errorf("negative delay %q is not allowed", s)
	}
	return DelayConfig{Min: d, Max: d, IsRange: false}, nil
}

// ParseMatch parses a match string: plain = exact, "re:pattern" = regex.
func ParseMatch(s string) (*MatchValue, error) {
	mv := &MatchValue{Raw: s}

	if strings.HasPrefix(s, "re:") {
		pattern := s[3:]
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidRegex, err.Error())
		}
		mv.IsRegex = true
		mv.Pattern = pattern
		mv.compiled = compiled
	} else {
		mv.IsRegex = false
		mv.Pattern = s
	}

	return mv, nil
}

// ConvertV2ToScenarios converts a v2 scenario file to v1 Scenario slice.
// It applies defaults, merges headers, assigns priorities.
// The orderedNames parameter provides scenario names in file order for priority assignment.
func ConvertV2ToScenarios(file *ScenarioFileV2, orderedNames []string) ([]*Scenario, error) { //nolint:gocyclo // v2-to-v1 conversion resolves defaults, merges headers, builds match rules
	if file == nil {
		return nil, fmt.Errorf("nil ScenarioFileV2")
	}

	scenarios := make([]*Scenario, 0, len(orderedNames))

	// Base priority for file-order assignment: first scenario = 1000, decrementing.
	basePriority := 1000

	for i, name := range orderedNames {
		entry, ok := file.Scenarios[name]
		if !ok {
			continue
		}

		// Resolve method: entry overrides default.
		method := file.Defaults.Method
		if entry.Method != "" {
			method = entry.Method
		}

		// Resolve endpoint: entry overrides default.
		endpoint := file.Defaults.Endpoint
		if entry.Endpoint != "" {
			endpoint = entry.Endpoint
		}

		// Resolve ICAP status.
		status := file.Defaults.Status
		if entry.Status != 0 {
			status = entry.Status
		}
		if status == 0 {
			status = 204 // sensible default
		}

		// Resolve HTTP status.
		httpStatus := file.Defaults.HTTPStatus
		if entry.HTTPStatus != 0 {
			httpStatus = entry.HTTPStatus
		}

		// Merge headers: start with defaults, then overlay set.
		mergedHeaders := make(map[string]string)
		for k, v := range file.Defaults.Headers {
			mergedHeaders[k] = v
		}
		for k, v := range entry.Set {
			mergedHeaders[k] = v
		}
		if len(mergedHeaders) == 0 {
			mergedHeaders = nil
		}

		// Resolve priority.
		priority := entry.Priority
		if priority == 0 {
			priority = basePriority - i
		}

		// Build MatchRule.
		matchRule := MatchRule{
			Method:  method,
			Path:    endpoint,
			Headers: entry.When,
		}

		// Build ResponseTemplate.
		response := ResponseTemplate{
			ICAPStatus: status,
			HTTPStatus: httpStatus,
			Headers:    mergedHeaders,
			Body:       entry.Body,
			BodyFile:   entry.BodyFile,
		}

		if entry.Delay != "" {
			dc, err := ParseDelay(entry.Delay)
			if err != nil {
				return nil, fmt.Errorf("scenario %q delay: %w", name, err)
			}
			response.Delay = dc.Min
			response.DelayRange = &dc
		}

		s := &Scenario{
			Name:     name,
			Match:    matchRule,
			Response: response,
			Priority: priority,
		}

		// Convert weighted responses
		if len(entry.Responses) > 0 {
			fileDefaults := file.Defaults
			for _, wr := range entry.Responses {
				w := WeightedResponse{
					Weight:     wr.Weight,
					Headers:    mergeHeaders(fileDefaults.Headers, wr.Set),
					ICAPStatus: wr.Status,
					HTTPStatus: wr.HTTPStatus,
					Body:       wr.Body,
				}
				if w.Weight == 0 {
					w.Weight = 1
				}
				if w.ICAPStatus == 0 {
					w.ICAPStatus = s.Response.ICAPStatus
				}
				if wr.Delay != "" {
					dc, err := ParseDelay(wr.Delay)
					if err != nil {
						return nil, fmt.Errorf("scenario %q response delay: %w", name, err)
					}
					w.Delay = dc
				}
				s.WeightedResponses = append(s.WeightedResponses, w)
			}
		}

		scenarios = append(scenarios, s)
	}

	return scenarios, nil
}

// mergeHeaders merges base headers with overlay, returning nil if the result is empty.
func mergeHeaders(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}
