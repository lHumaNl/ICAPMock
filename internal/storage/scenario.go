// Copyright 2026 ICAP Mock

package storage

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Error definitions for scenario operations.
// These sentinel errors can be used with errors.Is() for type checking.
var (
	// ErrNoMatch is returned when no scenario matches the request.
	ErrNoMatch = errors.New("no matching scenario")

	// ErrScenarioLoadFailed is returned when scenario file cannot be loaded.
	ErrScenarioLoadFailed = errors.New("failed to load scenarios")

	// ErrInvalidScenario is returned when a scenario definition is invalid.
	ErrInvalidScenario = errors.New("invalid scenario definition")

	// ErrInvalidRegex is returned when a regex pattern is invalid.
	ErrInvalidRegex = errors.New("invalid regex pattern")

	// ErrBodyFileNotFound is returned when a body file cannot be read.
	ErrBodyFileNotFound = errors.New("body file not found")

	// ErrInvalidCIDR is returned when a CIDR range is invalid.
	ErrInvalidCIDR = errors.New("invalid CIDR range")
)

// ScenarioRegistry manages mock scenarios for request matching.
// It supports loading scenarios from YAML files and hot-reloading.
type ScenarioRegistry interface {
	// Load loads scenarios from a YAML file.
	// Returns an error if the file cannot be read or parsed.
	Load(path string) error

	// Match finds the first scenario that matches the given request.
	// Scenarios are checked in priority order (highest first).
	// Returns ErrNoMatch if no scenario matches.
	Match(req *icap.Request) (*Scenario, error)

	// Reload reloads all scenarios from the last loaded file.
	// Returns an error if reload fails.
	Reload() error

	// List returns all registered scenarios sorted by priority.
	List() []*Scenario

	// Add adds a scenario to the registry.
	Add(scenario *Scenario) error

	// Remove removes a scenario by name.
	Remove(name string) error
}

// Scenario defines a mock response scenario.
type Scenario struct {
	compiledPath      *regexp.Regexp
	compiledBody      *regexp.Regexp
	compiledHeaders   map[string]*regexp.Regexp
	Response          ResponseTemplate   `yaml:"response" json:"response"`
	Name              string             `yaml:"name" json:"name"`
	Match             MatchRule          `yaml:"match" json:"match"`
	WeightedResponses []WeightedResponse `yaml:"-" json:"-"`
	compiledCIDRs     []*net.IPNet
	Priority          int `yaml:"priority" json:"priority"`
}

// WeightedResponse is a single weighted response variant used for random selection.
type WeightedResponse struct {
	Headers    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body       string            `yaml:"body,omitempty" json:"body,omitempty"`
	Delay      DelayConfig       `yaml:"-" json:"-"`
	Weight     int               `yaml:"weight" json:"weight"`
	ICAPStatus int               `yaml:"icap_status,omitempty" json:"icap_status,omitempty"`
	HTTPStatus int               `yaml:"http_status,omitempty" json:"http_status,omitempty"`
}

// CompiledPath returns the compiled path regex, or nil if not set.
func (s *Scenario) CompiledPath() *regexp.Regexp { return s.compiledPath }

// CompiledBody returns the compiled body regex, or nil if not set.
func (s *Scenario) CompiledBody() *regexp.Regexp { return s.compiledBody }

// MatchRule defines criteria for matching ICAP requests.
type MatchRule struct {
	// Path is a regex pattern to match the ICAP URI path.
	// Empty string matches all paths.
	Path string `yaml:"path_pattern,omitempty" json:"path_pattern,omitempty"`

	// Method is the ICAP method to match (REQMOD, RESPMOD, OPTIONS).
	// Empty string matches all methods.
	Method string `yaml:"icap_method,omitempty" json:"icap_method,omitempty"`

	// HTTPMethod is the HTTP method to match in embedded requests.
	// Empty string matches all HTTP methods.
	HTTPMethod string `yaml:"http_method,omitempty" json:"http_method,omitempty"`

	// Headers contains exact match criteria for ICAP headers.
	// All specified headers must match for the scenario to match.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// BodyPattern is a regex pattern to match the HTTP body.
	// Empty string matches any body (including no body).
	BodyPattern string `yaml:"body_pattern,omitempty" json:"body_pattern,omitempty"`

	// ClientIP is a CIDR or exact IP to match.
	// Empty string matches all clients.
	ClientIP string `yaml:"client_ip,omitempty" json:"client_ip,omitempty"`

	// CIDRRanges is a list of CIDR ranges to match client IPs against.
	// Empty list matches all clients.
	// Example: ["192.168.1.0/24", "10.0.0.0/8"]
	CIDRRanges []string `yaml:"cidr_ranges,omitempty" json:"cidr_ranges,omitempty"`
}

// ResponseTemplate defines the mock response to return.
type ResponseTemplate struct {
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	HTTPHeaders map[string]string `yaml:"http_headers,omitempty" json:"http_headers,omitempty"`
	DelayRange  *DelayConfig      `yaml:"-" json:"-"`
	Body        string            `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile    string            `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	Error       string            `yaml:"error,omitempty" json:"error,omitempty"`
	Script      string            `yaml:"script,omitempty" json:"script,omitempty"`
	ICAPStatus  int               `yaml:"icap_status" json:"icap_status"`
	HTTPStatus  int               `yaml:"http_status,omitempty" json:"http_status,omitempty"`
	Delay       time.Duration     `yaml:"delay,omitempty" json:"delay,omitempty"`
}

// ScenarioFile represents the YAML structure for scenario files.
type ScenarioFile struct {
	Scenarios []Scenario `yaml:"scenarios"`
}

// DefaultScenario returns a default scenario that returns 204 No Content.
func DefaultScenario() *Scenario {
	return &Scenario{
		Name: defaultScenarioName,
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: -1, // Lowest priority
	}
}

// scenarioRegistry implements the ScenarioRegistry interface.
type scenarioRegistry struct {
	filePath  string
	scenarios []*Scenario
	mu        sync.RWMutex
}

// NewScenarioRegistry creates a new scenario registry.
func NewScenarioRegistry() ScenarioRegistry {
	return &scenarioRegistry{
		scenarios: []*Scenario{DefaultScenario()},
	}
}

// Load loads scenarios from a YAML file.
// It provides detailed error messages with file path, scenario name,
// field name, and suggestions for fixing issues.
func (r *scenarioRegistry) Load(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is validated
	if err != nil {
		return NewScenarioLoadError(path, err)
	}

	// Detect format: v2 (scenarios is a map) vs v1 (scenarios is an array)
	isV2, orderedNames, err := detectV2Format(data)
	if err != nil {
		return NewScenarioParseError(path, err)
	}

	var scenarios []Scenario

	if isV2 {
		var sf ScenarioFileV2
		if err := yaml.Unmarshal(data, &sf); err != nil {
			return NewScenarioParseError(path, err)
		}
		converted, err := ConvertV2ToScenarios(&sf, orderedNames)
		if err != nil {
			return &ScenarioError{
				Operation:  "convert_v2",
				FilePath:   path,
				Message:    err.Error(),
				Suggestion: "check v2 scenario format",
			}
		}
		for _, s := range converted {
			scenarios = append(scenarios, *s)
		}
	} else {
		var sf ScenarioFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			return NewScenarioParseError(path, err)
		}
		scenarios = sf.Scenarios
	}

	// Validate and compile regex patterns
	for i := range scenarios {
		if err := r.validateAndCompile(&scenarios[i]); err != nil {
			var se *ScenarioError
			if AsScenarioError(err, &se) {
				se.FilePath = path
				return se
			}
			return &ScenarioError{
				Operation:    "validate",
				FilePath:     path,
				ScenarioName: scenarios[i].Name,
				Message:      err.Error(),
				Suggestion:   "check the scenario configuration for errors",
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Add default scenario if not present
	hasDefault := false
	for _, s := range scenarios {
		if s.Name == defaultScenarioName {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		scenarios = append(scenarios, *DefaultScenario())
	}

	// Convert to pointers for storage
	r.scenarios = make([]*Scenario, len(scenarios))
	for i := range scenarios {
		r.scenarios[i] = &scenarios[i]
	}
	r.filePath = path

	// Sort by priority (descending)
	sort.Slice(r.scenarios, func(i, j int) bool {
		return r.scenarios[i].Priority > r.scenarios[j].Priority
	})

	return nil
}

// detectV2Format checks if the YAML data uses v2 format (scenarios is a mapping).
// Returns true for v2, false for v1, and the ordered scenario names for v2 files.
func detectV2Format(data []byte) (bool, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false, nil, err
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false, nil, nil
	}

	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return false, nil, nil
	}

	// Find "scenarios" key
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		key := mapping.Content[i]
		value := mapping.Content[i+1]
		if key.Value == "scenarios" {
			if value.Kind == yaml.MappingNode {
				// v2: extract ordered names from mapping keys
				var names []string
				for j := 0; j < len(value.Content)-1; j += 2 {
					names = append(names, value.Content[j].Value)
				}
				return true, names, nil
			}
			// v1: scenarios is a sequence
			return false, nil, nil
		}
	}

	// No "scenarios" key found — treat as v1
	return false, nil, nil
}

// validateAndCompile validates a scenario and compiles regex patterns.
// It returns detailed ScenarioError instances with suggestions for fixes.
func (r *scenarioRegistry) validateAndCompile(s *Scenario) error {
	// Validate scenario name
	if s.Name == "" {
		return NewScenarioValidationError(
			"", // file path will be set by caller
			"",
			"name",
			s.Name,
			"scenario name is required",
			"add a 'name' field to your scenario, e.g., 'name: my-scenario'",
		)
	}

	// Compile path pattern
	if s.Match.Path != "" {
		pattern := s.Match.Path
		if strings.HasPrefix(pattern, "re:") {
			pattern = strings.TrimPrefix(pattern, "re:")
		}
		// Always compile as regex for backward compatibility
		// (v1 paths are always regex, v2 paths with re: prefix are regex,
		//  v2 paths without re: are exact but we compile them as ^exact$ for prefix match)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return NewScenarioRegexError("", s.Name, "match.path_pattern", s.Match.Path, err)
		}
		s.compiledPath = re
	}

	// Compile body regex
	if s.Match.BodyPattern != "" {
		re, err := regexp.Compile(s.Match.BodyPattern)
		if err != nil {
			return NewScenarioRegexError(
				"", // file path will be set by caller
				s.Name,
				"match.body_pattern",
				s.Match.BodyPattern,
				err,
			)
		}
		s.compiledBody = re
	}

	// Validate ICAP status
	if s.Response.ICAPStatus == 0 {
		s.Response.ICAPStatus = 204 // Default to No Content
	}

	// Validate response body file path
	if s.Response.BodyFile != "" {
		if _, err := os.Stat(s.Response.BodyFile); err != nil {
			return NewScenarioBodyFileError(
				"", // file path will be set by caller
				s.Name,
				s.Response.BodyFile,
				err,
			)
		}
	}

	// Compile header patterns with re: prefix
	for key, value := range s.Match.Headers {
		if strings.HasPrefix(value, "re:") {
			pattern := strings.TrimPrefix(value, "re:")
			re, err := regexp.Compile(pattern)
			if err != nil {
				return NewScenarioRegexError("", s.Name, "match.headers."+key, value, err)
			}
			if s.compiledHeaders == nil {
				s.compiledHeaders = make(map[string]*regexp.Regexp)
			}
			s.compiledHeaders[key] = re
		}
	}

	// Validate and compile CIDR ranges
	if len(s.Match.CIDRRanges) > 0 {
		// Parse and cache CIDR ranges for performance
		s.compiledCIDRs = make([]*net.IPNet, 0, len(s.Match.CIDRRanges))
		for _, cidr := range s.Match.CIDRRanges {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return NewScenarioCIDRError(
					"", // file path will be set by caller
					s.Name,
					cidr,
					err,
				)
			}
			s.compiledCIDRs = append(s.compiledCIDRs, ipNet)
		}
	}

	return nil
}

// Match finds the first scenario that matches the given request.
// Scenarios are checked in priority order (highest first).
// Returns a detailed error if no scenario matches, including what was checked.
func (r *scenarioRegistry) Match(req *icap.Request) (*Scenario, error) {
	if req == nil {
		return nil, NewScenarioMatchError(
			"cannot match against nil request",
			nil,
		)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	checkedCount := 0
	for _, s := range r.scenarios {
		checkedCount++
		if r.matches(s, req) {
			return s, nil
		}
	}

	// Provide detailed error message about what was attempted
	return nil, &ScenarioError{
		Operation:  "match",
		Message:    fmt.Sprintf("no scenario matched the request (checked %d scenarios)", checkedCount),
		Suggestion: fmt.Sprintf("add a scenario that matches method=%s, uri=%s", req.Method, req.URI),
	}
}

// matches checks if a scenario matches the given request.
func (r *scenarioRegistry) matches(s *Scenario, req *icap.Request) bool {
	// Check ICAP method
	if s.Match.Method != "" && s.Match.Method != req.Method {
		return false
	}

	// Check path pattern
	if s.compiledPath != nil {
		// Extract path from ICAP URI
		path := extractPath(req.URI)
		if !s.compiledPath.MatchString(path) {
			return false
		}
	}

	// Check headers (all must match — exact or regex)
	for key, value := range s.Match.Headers {
		h, ok := req.Header.Get(key)
		if !ok {
			return false
		}
		if compiled, hasRegex := s.compiledHeaders[key]; hasRegex {
			if !compiled.MatchString(h) {
				return false
			}
		} else {
			if h != value {
				return false
			}
		}
	}

	// Check HTTP method - if scenario specifies HTTP method, request must have HTTP request
	if s.Match.HTTPMethod != "" {
		if req.HTTPRequest == nil {
			return false
		}
		if req.HTTPRequest.Method != s.Match.HTTPMethod {
			return false
		}
	}

	// Check body pattern - load body only if pattern exists (lazy loading)
	if s.compiledBody != nil && req.HTTPRequest != nil {
		body, err := req.HTTPRequest.GetBody()
		if err != nil {
			// Log error but don't fail the match
			return false
		}
		if !s.compiledBody.MatchString(string(body)) {
			return false
		}
	}

	// Check client IP (exact match)
	if s.Match.ClientIP != "" {
		if !matchClientIP(s.Match.ClientIP, req.ClientIP) {
			return false
		}
	}

	// Check CIDR ranges
	if len(s.compiledCIDRs) > 0 {
		if !matchByCIDR(s.compiledCIDRs, req.ClientIP) {
			return false
		}
	}

	return true
}

// extractPath extracts the path from an ICAP URI.
func extractPath(uri string) string {
	// Remove icap:// prefix
	uri = strings.TrimPrefix(uri, "icap://")
	uri = strings.TrimPrefix(uri, "icaps://")

	// Find the first slash
	idx := strings.Index(uri, "/")
	if idx == -1 {
		return "/"
	}
	return uri[idx:]
}

// matchClientIP checks if the client IP matches the pattern.
func matchClientIP(pattern, clientIP string) bool {
	// Simple exact match for now
	return pattern == clientIP
}

// matchByCIDR checks if the client IP matches any of the CIDR ranges.
func matchByCIDR(cidrRanges []*net.IPNet, clientIP string) bool {
	if len(cidrRanges) == 0 {
		return true
	}

	// Parse client IP
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// Check if IP matches any CIDR range
	for _, ipNet := range cidrRanges {
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}

// Reload reloads all scenarios from the last loaded file.
func (r *scenarioRegistry) Reload() error {
	r.mu.RLock()
	path := r.filePath
	r.mu.RUnlock()

	if path == "" {
		return nil
	}

	return r.Load(path)
}

// List returns all registered scenarios sorted by priority.
func (r *scenarioRegistry) List() []*Scenario {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Scenario, len(r.scenarios))
	copy(result, r.scenarios)
	return result
}

// Add adds a scenario to the registry.
// Returns a detailed ScenarioError if validation fails.
func (r *scenarioRegistry) Add(scenario *Scenario) error {
	if scenario == nil {
		return &ScenarioError{
			Operation:  operationAdd,
			Message:    "cannot add nil scenario",
			Suggestion: "provide a valid scenario with at least a name field",
		}
	}

	if err := r.validateAndCompile(scenario); err != nil {
		// Wrap the error with additional context
		var se *ScenarioError
		if AsScenarioError(err, &se) {
			se.Operation = operationAdd
			return se
		}
		return &ScenarioError{
			Operation:    operationAdd,
			ScenarioName: scenario.Name,
			Message:      err.Error(),
			Suggestion:   "fix the validation error before adding the scenario",
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove existing scenario with same name
	for i, s := range r.scenarios {
		if s.Name == scenario.Name {
			r.scenarios = append(r.scenarios[:i], r.scenarios[i+1:]...)
			break
		}
	}

	r.scenarios = append(r.scenarios, scenario)

	// Re-sort by priority
	sort.Slice(r.scenarios, func(i, j int) bool {
		return r.scenarios[i].Priority > r.scenarios[j].Priority
	})

	return nil
}

// Remove removes a scenario by name.
func (r *scenarioRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, s := range r.scenarios {
		if s.Name == name {
			r.scenarios = append(r.scenarios[:i], r.scenarios[i+1:]...)
			return nil
		}
	}

	return ErrNoMatch
}

// GetBody returns the response body, loading from file if specified.
// Returns a detailed ScenarioError if the body file cannot be read.
func (rt *ResponseTemplate) GetBody() (string, error) {
	if rt.BodyFile != "" {
		data, err := os.ReadFile(rt.BodyFile)
		if err != nil {
			return "", NewScenarioBodyFileError(
				"", // file path context not available here
				"", // scenario name not available here
				rt.BodyFile,
				err,
			)
		}
		return string(data), nil
	}
	return rt.Body, nil
}
