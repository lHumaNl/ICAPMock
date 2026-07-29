// Copyright 2026 ICAP Mock

package storage

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/weight"
)

// stringList decodes a YAML field that accepts either a single scalar string
// or a sequence of strings. It underlies MethodList and EndpointList so the
// two types share decoding without duplicating logic.
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

// MethodList is a list of ICAP methods (REQMOD, RESPMOD, OPTIONS) a scenario
// applies to. In YAML it accepts either a single string ("REQMOD") or a
// sequence ("[REQMOD, RESPMOD]"). An empty list means "any method".
type MethodList stringList

// UnmarshalYAML delegates to stringList.
func (m *MethodList) UnmarshalYAML(node *yaml.Node) error {
	return (*stringList)(m).UnmarshalYAML(node)
}

// MarshalYAML delegates to stringList.
func (m MethodList) MarshalYAML() (interface{}, error) {
	return stringList(m).marshalYAML()
}

// EndpointList is a list of ICAP endpoint paths a scenario serves. Entries
// may contain "{name}" captures (compiled to regex-named capture groups).
// In YAML it accepts either a single path or a sequence of paths.
type EndpointList stringList

// UnmarshalYAML delegates to stringList.
func (e *EndpointList) UnmarshalYAML(node *yaml.Node) error {
	return (*stringList)(e).UnmarshalYAML(node)
}

// MarshalYAML delegates to stringList.
func (e EndpointList) MarshalYAML() (interface{}, error) {
	return stringList(e).marshalYAML()
}

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

// HeaderMatchesV2 maps header names to either a scalar exact/regex matcher or
// a sequence. Generic-header sequences use case-insensitive contains-any;
// Content-Type sequences use media-type-aware matching.
type HeaderMatchesV2 map[string]HeaderMatchValueV2

// HeaderMatchValueV2 preserves whether a header matcher was declared as a
// scalar or a sequence in YAML/JSON.
type HeaderMatchValueV2 struct {
	Scalar   string
	Contains []string
	IsList   bool
}

// NewExactHeaderMatchesV2 converts legacy scalar header maps used by inline
// configuration and programmatic callers.
func NewExactHeaderMatchesV2(headers map[string]string) HeaderMatchesV2 {
	if len(headers) == 0 {
		return nil
	}
	matches := make(HeaderMatchesV2, len(headers))
	for key, value := range headers {
		matches[key] = HeaderMatchValueV2{Scalar: value}
	}
	return matches
}

// UnmarshalYAML accepts either one scalar string or a non-empty sequence of
// strings. Sequence values are normalized later during scenario validation.
func (m *HeaderMatchValueV2) UnmarshalYAML(node *yaml.Node) error {
	return m.decodeYAML(node)
}

func (m *HeaderMatchValueV2) decodeYAML(node *yaml.Node) error {
	switch node.Kind { //nolint:exhaustive // only scalar and sequence are valid matcher shapes.
	case yaml.ScalarNode:
		m.Scalar = node.Value
		m.Contains = nil
		m.IsList = false
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("header match list must not be empty")
		}
		values := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return fmt.Errorf("header match list item must be a string, got %v", item.Kind)
			}
			if strings.TrimSpace(item.Value) == "" {
				return fmt.Errorf("header match list item must not be empty")
			}
			values = append(values, item.Value)
		}
		m.Scalar = ""
		m.Contains = values
		m.IsList = true
		return nil
	default:
		return fmt.Errorf("header match must be a string or a list of strings, got %v", node.Kind)
	}
}

// UnmarshalJSON mirrors the YAML scalar-or-sequence contract.
func (m *HeaderMatchValueV2) UnmarshalJSON(data []byte) error {
	var scalar string
	if err := json.Unmarshal(data, &scalar); err == nil {
		*m = HeaderMatchValueV2{Scalar: scalar}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("header match must be a string or a list of strings: %w", err)
	}
	if len(values) == 0 {
		return fmt.Errorf("header match list must not be empty")
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("header match list item must not be empty")
		}
	}
	*m = HeaderMatchValueV2{Contains: values, IsList: true}
	return nil
}

// MarshalYAML preserves the concise scalar/list scenario syntax.
func (m HeaderMatchValueV2) MarshalYAML() (interface{}, error) {
	if m.IsList {
		return m.Contains, nil
	}
	return m.Scalar, nil
}

// MarshalJSON preserves the scalar/list JSON shape.
func (m HeaderMatchValueV2) MarshalJSON() ([]byte, error) {
	if m.IsList {
		return json.Marshal(m.Contains)
	}
	return json.Marshal(m.Scalar)
}

func splitHeaderMatches(matches HeaderMatchesV2) (scalar map[string]string, contains map[string][]string) {
	for key, match := range matches {
		if match.IsList {
			if contains == nil {
				contains = make(map[string][]string)
			}
			contains[key] = append([]string(nil), match.Contains...)
			continue
		}
		if scalar == nil {
			scalar = make(map[string]string)
		}
		scalar[key] = match.Scalar
	}
	return scalar, contains
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
	Headers           map[string]string             `yaml:"headers,omitempty"`
	ResponseTemplates map[string]ResponseTemplateV2 `yaml:"response_templates,omitempty"`
	Use               string                        `yaml:"use,omitempty"`
	Method            MethodList                    `yaml:"method,omitempty"`
	Endpoint          EndpointList                  `yaml:"endpoint,omitempty"`
	Status            int                           `yaml:"status,omitempty"`
	HTTPStatus        int                           `yaml:"http_status,omitempty"`
}

// ScenarioEntryV2 defines a single scenario in v2 format.
//
// Response-shaping fields split into two tiers:
//
//   - ICAP-envelope: "set" (ICAP headers), "body" (ICAP body). Used by
//     scenarios that only modify the ICAP response wrapper.
//   - Wrapped HTTP (typical for block pages returned to the origin client):
//     "http_set" (HTTP headers on the wrapped response), "http_body" /
//     "http_body_file" (HTTP body on the wrapped response). Applied when
//     "http_status" is non-zero (the mock then returns a synthesized HTTP
//     response instead of letting the original request/response pass).
type ScenarioEntryV2 struct {
	When         HeaderMatchesV2        `yaml:"when,omitempty"`
	WhenHTTP     *WhenHTTPV2            `yaml:"when_http,omitempty"`
	Set          map[string]string      `yaml:"set,omitempty"`
	HTTPSet      map[string]string      `yaml:"http_set,omitempty"`
	Stream       *StreamConfig          `yaml:"stream,omitempty"`
	Block        *bool                  `yaml:"block,omitempty"`
	Method       MethodList             `yaml:"method,omitempty"`
	Endpoint     EndpointList           `yaml:"endpoint,omitempty"`
	Use          string                 `yaml:"use,omitempty"`
	Body         string                 `yaml:"body,omitempty"`
	BodyFile     string                 `yaml:"body_file,omitempty"`
	HTTPBody     string                 `yaml:"http_body,omitempty"`
	HTTPBodyFile string                 `yaml:"http_body_file,omitempty"`
	Error        string                 `yaml:"error,omitempty"`
	Delay        string                 `yaml:"delay,omitempty"`
	Responses    WeightedResponseListV2 `yaml:"responses,omitempty" json:"responses,omitempty"`
	Branches     []BranchV2             `yaml:"branches,omitempty"`
	Status       int                    `yaml:"status,omitempty"`
	HTTPStatus   int                    `yaml:"http_status,omitempty"`
	Priority     int                    `yaml:"priority,omitempty"`
}

// UnmarshalYAML preserves an explicitly null responses field as a present empty list.
func (s *ScenarioEntryV2) UnmarshalYAML(node *yaml.Node) error {
	type plain ScenarioEntryV2
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*s = ScenarioEntryV2(decoded)
	if mappingContainsKey(node, "responses") && s.Responses == nil {
		s.Responses = make(WeightedResponseListV2, 0)
	}
	return nil
}

// UnmarshalJSON preserves an explicitly null responses field as a present empty list.
func (s *ScenarioEntryV2) UnmarshalJSON(data []byte) error {
	type plain ScenarioEntryV2
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = ScenarioEntryV2(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, present := fields["responses"]; present && s.Responses == nil {
		s.Responses = make(WeightedResponseListV2, 0)
	}
	return nil
}

// BranchV2 is one conditional branch inside a scenario. Branches are matched
// top-to-bottom; the first one whose conditions pass produces the response.
// A branch without any "when"/"when_http" conditions acts as a catch-all.
type BranchV2 struct {
	When         HeaderMatchesV2        `yaml:"when,omitempty"`
	WhenHTTP     *WhenHTTPV2            `yaml:"when_http,omitempty"`
	Set          map[string]string      `yaml:"set,omitempty"`
	HTTPSet      map[string]string      `yaml:"http_set,omitempty"`
	Stream       *StreamConfig          `yaml:"stream,omitempty"`
	Block        *bool                  `yaml:"block,omitempty"`
	Use          string                 `yaml:"use,omitempty"`
	Body         string                 `yaml:"body,omitempty"`
	BodyFile     string                 `yaml:"body_file,omitempty"`
	HTTPBody     string                 `yaml:"http_body,omitempty"`
	HTTPBodyFile string                 `yaml:"http_body_file,omitempty"`
	Error        string                 `yaml:"error,omitempty"`
	Delay        string                 `yaml:"delay,omitempty"`
	Responses    WeightedResponseListV2 `yaml:"responses,omitempty" json:"responses,omitempty"`
	Status       int                    `yaml:"status,omitempty"`
	HTTPStatus   int                    `yaml:"http_status,omitempty"`
}

// UnmarshalYAML preserves an explicitly null responses field as a present empty list.
func (b *BranchV2) UnmarshalYAML(node *yaml.Node) error {
	type plain BranchV2
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*b = BranchV2(decoded)
	if mappingContainsKey(node, "responses") && b.Responses == nil {
		b.Responses = make(WeightedResponseListV2, 0)
	}
	return nil
}

// UnmarshalJSON preserves an explicitly null responses field as a present empty list.
func (b *BranchV2) UnmarshalJSON(data []byte) error {
	type plain BranchV2
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*b = BranchV2(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, present := fields["responses"]; present && b.Responses == nil {
		b.Responses = make(WeightedResponseListV2, 0)
	}
	return nil
}

func mappingContainsKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// ResponseTemplateV2 is a named entry in defaults.response_templates. It can be
// either a plain inline response or a list of weighted variants. The custom
// unmarshaler distinguishes the two by YAML shape (mapping vs. sequence).
type ResponseTemplateV2 struct {
	Inline   *InlineResponseV2
	Weighted WeightedResponseListV2
}

// WeightedResponseListV2 preserves the distinction between an absent list and
// an explicitly empty/null list so invalid policies cannot fail open.
type WeightedResponseListV2 []WeightedResponseV2

// UnmarshalYAML decodes a weighted list while preserving explicit presence.
func (w *WeightedResponseListV2) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		*w = make(WeightedResponseListV2, 0)
		return nil
	}
	var decoded []WeightedResponseV2
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*w = WeightedResponseListV2(decoded)
	return nil
}

// UnmarshalJSON decodes a weighted list while preserving explicit presence.
func (w *WeightedResponseListV2) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*w = make(WeightedResponseListV2, 0)
		return nil
	}
	var decoded []WeightedResponseV2
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*w = WeightedResponseListV2(decoded)
	return nil
}

// InlineResponseV2 is a single non-weighted response definition.
type InlineResponseV2 struct {
	Set          map[string]string `yaml:"set,omitempty"`
	HTTPSet      map[string]string `yaml:"http_set,omitempty"`
	Stream       *StreamConfig     `yaml:"stream,omitempty"`
	Block        *bool             `yaml:"block,omitempty"`
	Use          string            `yaml:"use,omitempty"`
	Body         string            `yaml:"body,omitempty"`
	BodyFile     string            `yaml:"body_file,omitempty"`
	HTTPBody     string            `yaml:"http_body,omitempty"`
	HTTPBodyFile string            `yaml:"http_body_file,omitempty"`
	Error        string            `yaml:"error,omitempty"`
	Delay        string            `yaml:"delay,omitempty"`
	Status       int               `yaml:"status,omitempty"`
	HTTPStatus   int               `yaml:"http_status,omitempty"`
}

// UnmarshalYAML routes mapping nodes to InlineResponseV2 and sequence nodes to
// a list of WeightedResponseV2.
func (r *ResponseTemplateV2) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind { //nolint:exhaustive // only Mapping and Sequence are valid at this position.
	case yaml.MappingNode:
		var inline InlineResponseV2
		if err := node.Decode(&inline); err != nil {
			return err
		}
		r.Inline = &inline
		return nil
	case yaml.SequenceNode:
		var ws WeightedResponseListV2
		if err := node.Decode(&ws); err != nil {
			return err
		}
		r.Weighted = ws
		return nil
	default:
		return fmt.Errorf("response template must be a mapping (inline) or a sequence (weighted), got %v", node.Kind)
	}
}

// WhenHTTPV2 matches against the encapsulated HTTP request/response of an ICAP
// message. All specified fields must match (logical AND). Combine with the
// top-level "when" block to also match ICAP headers.
type WhenHTTPV2 struct {
	// Headers matches encapsulated HTTP headers. Scalar values may be exact or
	// "re:"-prefixed regex patterns; lists are case-insensitive contains-any.
	Headers HeaderMatchesV2 `yaml:"headers,omitempty"`
	// URL matches the URI of the encapsulated HTTP request. Exact string or
	// "re:"-prefixed regex. Useful for matching filenames from the request URL.
	URL string `yaml:"url,omitempty"`
	// Method matches the HTTP method (GET/POST/...) of the encapsulated request.
	Method string `yaml:"method,omitempty"`
}

// WeightedResponseV2 defines one variant in a weighted response set. It may
// either carry inline response fields or reference a named template via "use:".
type WeightedResponseV2 struct {
	Set          map[string]string `yaml:"set,omitempty"`
	HTTPSet      map[string]string `yaml:"http_set,omitempty"`
	Stream       *StreamConfig     `yaml:"stream,omitempty"`
	Block        *bool             `yaml:"block,omitempty"`
	Body         string            `yaml:"body,omitempty"`
	HTTPBody     string            `yaml:"http_body,omitempty"`
	HTTPBodyFile string            `yaml:"http_body_file,omitempty"`
	Error        string            `yaml:"error,omitempty"`
	Delay        string            `yaml:"delay,omitempty"`
	Use          string            `yaml:"use,omitempty"`
	Weight       weight.Percentage `yaml:"weight" json:"weight"`
	Status       int               `yaml:"status,omitempty"`
	HTTPStatus   int               `yaml:"http_status,omitempty"`
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

// ConvertV2ToScenarios converts a v2 scenario file into the runtime Scenario
// representation. It resolves defaults, named response templates, and
// branches; validates method/endpoint presence; and assigns file-order
// priorities.
//
// Template resolution is single-depth: a "use:" reference points to an entry
// in defaults.response_templates, which itself must be a concrete response
// (inline fields or a weighted list). Templates cannot reference other
// templates — keeps the model predictable and rules out cycles by construction.
func ConvertV2ToScenarios(file *ScenarioFileV2, orderedNames []string) ([]*Scenario, error) { //nolint:gocyclo // v2-to-v1 conversion necessarily touches many fields
	if file == nil {
		return nil, fmt.Errorf("nil ScenarioFileV2")
	}

	// Pre-validate defaults.use target exists, if set.
	if file.Defaults.Use != "" {
		if _, ok := file.Defaults.ResponseTemplates[file.Defaults.Use]; !ok {
			return nil, fmt.Errorf("defaults.use: template %q is not defined in defaults.response_templates", file.Defaults.Use)
		}
	}
	for name, template := range file.Defaults.ResponseTemplates {
		if template.Weighted == nil {
			continue
		}
		if err := validateV2WeightTotal("response template "+name, template.Weighted); err != nil {
			return nil, err
		}
	}

	scenarios := make([]*Scenario, 0, len(orderedNames))
	basePriority := 1000

	for i, name := range orderedNames {
		entry, ok := file.Scenarios[name]
		if !ok {
			continue
		}

		// Resolve method(s): entry overrides default wholesale (not merged).
		methods := file.Defaults.Method
		if len(entry.Method) > 0 {
			methods = entry.Method
		}

		// Resolve endpoint list: entry overrides default wholesale.
		endpoints := file.Defaults.Endpoint
		if len(entry.Endpoint) > 0 {
			endpoints = entry.Endpoint
		}

		if len(methods) == 0 {
			return nil, fmt.Errorf("scenario %q: method is not set (provide defaults.method or scenario.method)", name)
		}
		for _, m := range methods {
			if !validICAPMethods[m] {
				return nil, fmt.Errorf("scenario %q: invalid ICAP method %q (allowed: REQMOD, RESPMOD, OPTIONS)", name, m)
			}
		}
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("scenario %q: endpoint is not set (provide defaults.endpoint or scenario.endpoint)", name)
		}

		// Validate mutual exclusion: branches vs scenario-level response.
		hasBranches := len(entry.Branches) > 0
		hasInline := entry.Status != 0 || entry.HTTPStatus != 0 || entry.Body != "" || entry.BodyFile != "" || entry.Error != "" || entry.Delay != "" || entry.Responses != nil || entry.Use != "" || entry.Stream != nil || entry.Block != nil
		if hasBranches && hasInline {
			return nil, fmt.Errorf("scenario %q: branches cannot be combined with scenario-level response fields (status/body/use/responses/...) on the same level — move the fallback into an explicit catch-all branch", name)
		}

		// Resolve priority.
		priority := entry.Priority
		if priority == 0 {
			priority = basePriority - i
		}

		// Build MatchRule.
		headers, headerContains := splitHeaderMatches(entry.When)
		matchRule := MatchRule{
			Methods:        []string(methods),
			Paths:          []string(endpoints),
			Headers:        headers,
			HeaderContains: headerContains,
		}
		if entry.WhenHTTP != nil {
			matchRule.HTTPHeaders, matchRule.HTTPHeaderContains = splitHeaderMatches(entry.WhenHTTP.Headers)
			matchRule.HTTPURL = entry.WhenHTTP.URL
			matchRule.HTTPMethod = entry.WhenHTTP.Method
		}

		s := &Scenario{
			Name:     name,
			Match:    matchRule,
			Priority: priority,
		}

		if hasBranches {
			branches, err := buildBranches(name, entry.Branches, file)
			if err != nil {
				return nil, err
			}
			s.Branches = branches
		} else {
			// Scenario-level response (use / inline / responses).
			resp, weighted, err := resolveScenarioResponse(name, entry, file)
			if err != nil {
				return nil, err
			}
			s.Response = resp
			s.WeightedResponses = weighted
		}

		scenarios = append(scenarios, s)
	}

	if file.Defaults.Use != "" && !containsScenarioNamed(scenarios, defaultScenarioName) {
		fallback, err := buildDefaultsUseFallbackScenario(file)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, fallback)
	}

	return scenarios, nil
}

func containsScenarioNamed(scenarios []*Scenario, name string) bool {
	for _, scenario := range scenarios {
		if scenario != nil && scenario.Name == name {
			return true
		}
	}
	return false
}

func buildDefaultsUseFallbackScenario(file *ScenarioFileV2) (*Scenario, error) {
	resp, weighted, err := resolveResponse(
		"defaults.use fallback",
		InlineResponseV2{Use: file.Defaults.Use},
		nil,
		file,
	)
	if err != nil {
		return nil, err
	}

	return &Scenario{
		Name: defaultScenarioName,
		// defaults.use is the file-wide fallback. It must be reachable when no
		// scenario matches, including requests outside defaults.endpoint; those
		// defaults are only for materializing ordinary scenarios.
		Match:             MatchRule{},
		Response:          resp,
		WeightedResponses: weighted,
		Priority:          -1,
	}, nil
}

// resolveScenarioResponse produces the non-branches response for a scenario:
// a single ResponseTemplate plus an optional weighted list.
func resolveScenarioResponse(name string, entry ScenarioEntryV2, file *ScenarioFileV2) (ResponseTemplate, []WeightedResponse, error) {
	inline := InlineResponseV2{
		Set:          entry.Set,
		HTTPSet:      entry.HTTPSet,
		Stream:       entry.Stream,
		Block:        entry.Block,
		Use:          entry.Use,
		Body:         entry.Body,
		BodyFile:     entry.BodyFile,
		HTTPBody:     entry.HTTPBody,
		HTTPBodyFile: entry.HTTPBodyFile,
		Error:        entry.Error,
		Delay:        entry.Delay,
		Status:       entry.Status,
		HTTPStatus:   entry.HTTPStatus,
	}
	return resolveResponse("scenario "+name, inline, entry.Responses, file)
}

// buildBranches compiles each branch declaration into a runtime Branch.
func buildBranches(scenarioName string, in []BranchV2, file *ScenarioFileV2) ([]Branch, error) {
	out := make([]Branch, 0, len(in))
	for i, b := range in {
		where := fmt.Sprintf("scenario %q, branch #%d", scenarioName, i+1)
		headers, headerContains := splitHeaderMatches(b.When)
		match := MatchRule{Headers: headers, HeaderContains: headerContains}
		if b.WhenHTTP != nil {
			match.HTTPHeaders, match.HTTPHeaderContains = splitHeaderMatches(b.WhenHTTP.Headers)
			match.HTTPURL = b.WhenHTTP.URL
			match.HTTPMethod = b.WhenHTTP.Method
		}
		inline := InlineResponseV2{
			Set:          b.Set,
			HTTPSet:      b.HTTPSet,
			Stream:       b.Stream,
			Block:        b.Block,
			Use:          b.Use,
			Body:         b.Body,
			BodyFile:     b.BodyFile,
			HTTPBody:     b.HTTPBody,
			HTTPBodyFile: b.HTTPBodyFile,
			Error:        b.Error,
			Delay:        b.Delay,
			Status:       b.Status,
			HTTPStatus:   b.HTTPStatus,
		}
		resp, weighted, err := resolveResponse(where, inline, b.Responses, file)
		if err != nil {
			return nil, err
		}
		out = append(out, Branch{
			Match:             match,
			Response:          resp,
			WeightedResponses: weighted,
		})
	}
	return out, nil
}

// resolveResponse takes an inline block (with optional "use:" ref) and an
// optional weighted list, resolves templates, and returns a (ResponseTemplate,
// []WeightedResponse) pair. Exactly one of the two outputs is "set" per call:
// if the result is weighted, ResponseTemplate contains only the default ICAP
// status (204) and the weighted slice carries the variants; otherwise
// weighted is nil and ResponseTemplate carries the full response.
//
// where is a human-readable location used in error messages.
func resolveResponse(where string, inline InlineResponseV2, weighted WeightedResponseListV2, file *ScenarioFileV2) (ResponseTemplate, []WeightedResponse, error) { //nolint:gocyclo // resolution covers several orthogonal shapes
	tpls := file.Defaults.ResponseTemplates

	// If a weighted list is provided, we're building a weighted response.
	// Scenario/branch-level inline fields (status, body, set, delay, …) act as
	// defaults that each variant inherits; the variant can override any field.
	// "use:" on the same level is ambiguous with "responses:" and is rejected.
	if weighted != nil {
		if len(weighted) == 0 {
			return ResponseTemplate{}, nil, fmt.Errorf("%s: responses must contain at least one weighted variant", where)
		}
		if inline.Use != "" {
			return ResponseTemplate{}, nil, fmt.Errorf("%s: responses cannot be combined with use: on the same level", where)
		}
		out, err := buildWeightedList(where, weighted, inline, file, "")
		if err != nil {
			return ResponseTemplate{}, nil, err
		}
		return ResponseTemplate{ICAPStatus: 204}, out, nil
	}

	// Single response. Start from the referenced template if any, then overlay
	// inline fields. Also inherit defaults headers and file-wide fallback.
	var (
		base   InlineResponseV2
		baseWt WeightedResponseListV2
	)
	useRef := inline.Use
	if useRef == "" && file.Defaults.Use != "" && isEmptyInline(inline) {
		// Scenario/branch has no inline content at all → use file-wide fallback.
		useRef = file.Defaults.Use
	}
	if useRef != "" {
		t, ok := tpls[useRef]
		if !ok {
			return ResponseTemplate{}, nil, fmt.Errorf("%s: use %q: template is not defined in defaults.response_templates", where, useRef)
		}
		switch {
		case t.Inline != nil:
			base = *t.Inline
			if base.Use != "" {
				return ResponseTemplate{}, nil, fmt.Errorf("%s: template %q itself contains use: — templates cannot reference other templates", where, useRef)
			}
		case t.Weighted != nil:
			// Template is a weighted set — only allowed when no inline overlays.
			if !isEmptyInlineExceptUse(inline) {
				return ResponseTemplate{}, nil, fmt.Errorf("%s: use %q points to a weighted template; inline overrides on the same level are not allowed", where, useRef)
			}
			baseWt = t.Weighted
		}
	}

	if baseWt != nil {
		out, err := buildWeightedList(where, baseWt, InlineResponseV2{}, file, useRef)
		if err != nil {
			return ResponseTemplate{}, nil, err
		}
		return ResponseTemplate{ICAPStatus: 204}, out, nil
	}

	// Inline merge: base < inline overlay.
	merged := mergeInline(base, inline)
	resolved, err := inlineToTemplate(where, merged, file.Defaults.Headers, file.Defaults.Status, file.Defaults.HTTPStatus)
	resolved.ResponseName = useRef
	return resolved, nil, err
}

// buildWeightedList resolves each WeightedResponseV2 variant to a concrete
// WeightedResponse, expanding any "use:" refs. The base InlineResponseV2
// carries scenario/branch-level inline defaults (delay, status, set, …) that
// variants inherit; each variant can override any field.
func buildWeightedList(
	where string,
	variants WeightedResponseListV2,
	base InlineResponseV2,
	file *ScenarioFileV2,
	baseName string,
) ([]WeightedResponse, error) {
	if err := validateV2WeightTotal(where, variants); err != nil {
		return nil, err
	}
	out := make([]WeightedResponse, 0, len(variants))
	for i, wr := range variants {
		inl := InlineResponseV2{
			Set:          wr.Set,
			HTTPSet:      wr.HTTPSet,
			Stream:       wr.Stream,
			Block:        wr.Block,
			Use:          wr.Use,
			Body:         wr.Body,
			HTTPBody:     wr.HTTPBody,
			HTTPBodyFile: wr.HTTPBodyFile,
			Error:        wr.Error,
			Delay:        wr.Delay,
			Status:       wr.Status,
			HTTPStatus:   wr.HTTPStatus,
		}
		// Scenario/branch-level defaults under the variant's overrides. If
		// the variant uses a template ("use:"), the template is the deepest
		// base and the scenario-level defaults wrap around it.
		if inl.Use == "" {
			inl = mergeInline(base, inl)
		}
		// A variant cannot itself nest responses (no recursive weighted).
		tpl, nested, err := resolveResponse(fmt.Sprintf("%s variant #%d", where, i+1), inl, nil, file)
		if err != nil {
			return nil, err
		}
		if len(nested) > 0 {
			return nil, fmt.Errorf("%s variant #%d: a weighted variant cannot itself resolve to a weighted set", where, i+1)
		}
		w := WeightedResponse{
			Weight:       wr.Weight,
			Headers:      tpl.Headers,
			HTTPHeaders:  tpl.HTTPHeaders,
			Stream:       tpl.Stream,
			Block:        tpl.Block,
			ResponseName: selectedResponseName(wr.Use, baseName),
			ICAPStatus:   tpl.ICAPStatus,
			HTTPStatus:   tpl.HTTPStatus,
			Body:         tpl.Body,
			BodyFile:     tpl.BodyFile,
			HTTPBody:     tpl.HTTPBody,
			HTTPBodyFile: tpl.HTTPBodyFile,
			Error:        tpl.Error,
		}
		if tpl.DelayRange != nil {
			w.Delay = *tpl.DelayRange
		}
		out = append(out, w)
	}
	return out, nil
}

func validateV2WeightTotal(where string, variants WeightedResponseListV2) error {
	if len(variants) == 0 {
		return fmt.Errorf("%s: responses must contain at least one variant", where)
	}
	total := 0
	weightedCount := 0
	for _, variant := range variants {
		units := variant.Weight.Units()
		if units != 0 {
			weightedCount++
		}
		total += units
	}
	if weightedCount == 0 {
		return nil
	}
	if weightedCount != len(variants) {
		return fmt.Errorf("%s: either all response variants must define weight or none of them", where)
	}
	for i, variant := range variants {
		units := variant.Weight.Units()
		if units < 0 || units > weight.TotalUnits {
			return fmt.Errorf("%s variant #%d: weight must be greater than 0.000 and no greater than 100.000", where, i+1)
		}
	}
	if total != weight.TotalUnits {
		return fmt.Errorf(
			"%s: response weights total %d.%03d, want exactly 100.000",
			where,
			total/weight.Scale,
			total%weight.Scale,
		)
	}
	return nil
}

func selectedResponseName(variantName, baseName string) string {
	if variantName != "" {
		return variantName
	}
	return baseName
}

// inlineToTemplate turns a resolved InlineResponseV2 + file defaults into a
// concrete ResponseTemplate, parsing delay and merging headers with
// defaults.headers overlaid by the inline "set:" map.
func inlineToTemplate(
	where string,
	inline InlineResponseV2,
	defHeaders map[string]string,
	defStatus int,
	defHTTPStatus int,
) (ResponseTemplate, error) {
	status := inline.Status
	if status == 0 {
		status = defStatus
	}
	if status == 0 {
		status = 204
	}
	httpStatus := inline.HTTPStatus
	if httpStatus == 0 {
		httpStatus = defHTTPStatus
	}
	headers := mergeHeaders(defHeaders, inline.Set)
	resp := ResponseTemplate{
		ICAPStatus:   status,
		HTTPStatus:   httpStatus,
		Headers:      headers,
		HTTPHeaders:  inline.HTTPSet,
		Stream:       inline.Stream,
		Block:        inline.Block,
		Body:         inline.Body,
		BodyFile:     inline.BodyFile,
		HTTPBody:     inline.HTTPBody,
		HTTPBodyFile: inline.HTTPBodyFile,
		Error:        inline.Error,
	}
	if inline.Delay != "" {
		dc, err := ParseDelay(inline.Delay)
		if err != nil {
			return ResponseTemplate{}, fmt.Errorf("%s delay: %w", where, err)
		}
		resp.Delay = dc.Min
		resp.DelayRange = &dc
	}
	return resp, nil
}

// mergeInline overlays "over" onto "base". Scalars override when non-zero;
// "set" / "http_set" maps are deep-merged with overlay keys winning.
func mergeInline(base, over InlineResponseV2) InlineResponseV2 {
	out := base
	if over.Status != 0 {
		out.Status = over.Status
	}
	if over.HTTPStatus != 0 {
		out.HTTPStatus = over.HTTPStatus
	}
	if over.Body != "" {
		out.Body = over.Body
	}
	if over.BodyFile != "" {
		out.BodyFile = over.BodyFile
	}
	if over.HTTPBody != "" {
		out.HTTPBody = over.HTTPBody
	}
	if over.HTTPBodyFile != "" {
		out.HTTPBodyFile = over.HTTPBodyFile
	}
	if over.Error != "" {
		out.Error = over.Error
	}
	if over.Delay != "" {
		out.Delay = over.Delay
	}
	if over.Stream != nil {
		out.Stream = over.Stream
	}
	if over.Block != nil {
		out.Block = over.Block
	}
	if len(over.Set) > 0 {
		out.Set = mergeHeaders(base.Set, over.Set)
	}
	if len(over.HTTPSet) > 0 {
		out.HTTPSet = mergeHeaders(base.HTTPSet, over.HTTPSet)
	}
	return out
}

func isEmptyInline(i InlineResponseV2) bool {
	return i.Use == "" && isEmptyInlineExceptUse(i)
}

func isEmptyInlineExceptUse(i InlineResponseV2) bool {
	return i.Status == 0 && i.HTTPStatus == 0 &&
		i.Body == "" && i.BodyFile == "" &&
		i.HTTPBody == "" && i.HTTPBodyFile == "" && i.Error == "" &&
		i.Delay == "" && i.Block == nil &&
		i.Stream == nil && len(i.Set) == 0 && len(i.HTTPSet) == 0
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
