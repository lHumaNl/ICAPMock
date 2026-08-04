// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/weight"
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

	// ErrEndpointTopologyChange indicates that live reload would require router
	// endpoint registration changes and therefore needs a process restart.
	ErrEndpointTopologyChange = errors.New("scenario endpoint topology changed; restart required")

	// ErrInvalidRegex is returned when a regex pattern is invalid.
	ErrInvalidRegex = errors.New("invalid regex pattern")

	// ErrBodyFileNotFound is returned when a body file cannot be read.
	ErrBodyFileNotFound = errors.New("body file not found")

	// ErrInvalidCIDR is returned when a CIDR range is invalid.
	ErrInvalidCIDR = errors.New("invalid CIDR range")

	// ErrBodyPatternLimitExceeded is returned when body_pattern matching hits its configured limit.
	ErrBodyPatternLimitExceeded = errors.New("body_pattern limit exceeded")
)

// HasExactRoutes reports whether any scenario uses route-aware method/path pairs.
func HasExactRoutes(scenarios []*Scenario) bool {
	for _, scenario := range scenarios {
		if scenario != nil && len(scenario.Match.Routes) > 0 {
			return true
		}
	}
	return false
}

// ScenarioEndpointTopology returns the sorted declaration-level endpoint set.
func ScenarioEndpointTopology(scenarios []*Scenario) []string {
	unique := make(map[string]struct{})
	for _, scenario := range scenarios {
		if scenario == nil {
			continue
		}
		for _, path := range scenario.Match.Paths {
			if path != "" {
				unique[path] = struct{}{}
			}
		}
		if scenario.Match.Path != "" {
			unique[scenario.Match.Path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// ValidateExactRouteTopology rejects live endpoint-set changes when either
// registry uses exact routes. Method remapping on the same endpoint set is safe.
func ValidateExactRouteTopology(current, next []*Scenario) error {
	if !HasExactRoutes(current) && !HasExactRoutes(next) {
		return nil
	}
	currentPaths := ScenarioEndpointTopology(current)
	nextPaths := ScenarioEndpointTopology(next)
	if stringSlicesEqual(currentPaths, nextPaths) {
		return nil
	}
	return fmt.Errorf("%w: from %v to %v", ErrEndpointTopologyChange, currentPaths, nextPaths)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

const defaultBodyPatternLimit int64 = 10 * 1024 * 1024

// BodyPatternLimitAction controls behavior when body_pattern input exceeds the configured limit.
type BodyPatternLimitAction string

const (
	// BodyPatternLimitActionNoMatch treats oversized bodies as a non-match.
	BodyPatternLimitActionNoMatch BodyPatternLimitAction = "no_match"
	// BodyPatternLimitActionError surfaces oversized bodies as controlled match errors.
	BodyPatternLimitActionError BodyPatternLimitAction = "error"
)

// BodyPatternOptions configures body_pattern matching reads. Limit < 0 means unlimited.
type BodyPatternOptions struct {
	LimitAction BodyPatternLimitAction
	Limit       int64
}

// DefaultBodyPatternOptions returns the legacy 10 MiB no-match behavior.
func DefaultBodyPatternOptions() BodyPatternOptions {
	return BodyPatternOptions{Limit: defaultBodyPatternLimit, LimitAction: BodyPatternLimitActionNoMatch}
}

func (o BodyPatternOptions) normalized() BodyPatternOptions {
	if o.Limit == 0 {
		o.Limit = defaultBodyPatternLimit
	}
	o.LimitAction = BodyPatternLimitAction(strings.ToLower(strings.TrimSpace(string(o.LimitAction))))
	if o.LimitAction == "" {
		o.LimitAction = BodyPatternLimitActionNoMatch
	}
	return o
}

// ScenarioRegistry manages mock scenarios for request matching.
// It supports loading scenarios from YAML files and hot-reloading.
type ScenarioRegistry interface {
	// Load loads scenarios from a YAML file.
	// Returns an error if the file cannot be read or parsed.
	Load(path string) error

	// Match finds the first scenario that matches the given request.
	// Scenarios are checked in priority order (highest first).
	// Returns ErrNoMatch if no scenario matches.
	Match(ctx context.Context, req *icap.Request) (*Scenario, error)

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
	compiledBody        *regexp.Regexp
	compiledHeaders     map[string]*regexp.Regexp
	compiledHeaderLists map[string]compiledHeaderListMatcher
	compiledHTTPHeaders map[string]*regexp.Regexp
	compiledHTTPLists   map[string]compiledHeaderListMatcher
	compiledHTTPURL     *regexp.Regexp
	compiledRoutes      map[string][]compiledEndpoint
	Name                string             `yaml:"name" json:"name"`
	Match               MatchRule          `yaml:"match" json:"match"`
	WeightedResponses   []WeightedResponse `yaml:"-" json:"-"`
	Branches            []Branch           `yaml:"-" json:"-"`
	compiledCIDRs       []*net.IPNet
	compiledPaths       []compiledEndpoint
	Response            ResponseTemplate `yaml:"response" json:"response"`
	Priority            int              `yaml:"priority" json:"priority"`
	order               int
}

// compiledEndpoint holds a compiled endpoint pattern plus the names of any
// path-parameter captures declared in it (e.g. "{id}" → "id").
type compiledEndpoint struct {
	re       *regexp.Regexp
	raw      string
	captures []string
}

type compiledHeaderListMatcher struct {
	mediaTypes    map[string]struct{}
	parameterized []contentTypeMatcher
	contains      []string
	isContentType bool
}

type contentTypeMatcher struct {
	parameters map[string]string
	mediaType  string
}

// Branch is one conditional response branch inside a scenario. Branches are
// evaluated in order; the first whose conditions match produces the response.
// A Branch with no conditions acts as a catch-all inside its scenario.
type Branch struct {
	compiledHeaders     map[string]*regexp.Regexp
	compiledHeaderLists map[string]compiledHeaderListMatcher
	compiledHTTPHeaders map[string]*regexp.Regexp
	compiledHTTPLists   map[string]compiledHeaderListMatcher
	compiledHTTPURL     *regexp.Regexp
	WeightedResponses   []WeightedResponse
	Match               MatchRule
	Response            ResponseTemplate
}

// WeightedResponse is a single weighted response variant used for random selection.
type WeightedResponse struct {
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	HTTPHeaders  map[string]string `yaml:"http_headers,omitempty" json:"http_headers,omitempty"`
	Stream       *StreamConfig     `yaml:"stream,omitempty" json:"stream,omitempty"`
	Block        *bool             `yaml:"block,omitempty" json:"block,omitempty"`
	ResponseName string            `yaml:"-" json:"-"`
	Body         string            `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile     string            `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	HTTPBody     string            `yaml:"http_body,omitempty" json:"http_body,omitempty"`
	HTTPBodyFile string            `yaml:"http_body_file,omitempty" json:"http_body_file,omitempty"`
	Error        string            `yaml:"error,omitempty" json:"error,omitempty"`
	Delay        DelayConfig       `yaml:"-" json:"-"`
	Weight       weight.Percentage `yaml:"weight" json:"weight"`
	ICAPStatus   int               `yaml:"icap_status,omitempty" json:"icap_status,omitempty"`
	HTTPStatus   int               `yaml:"http_status,omitempty" json:"http_status,omitempty"`
}

// SelectBranch returns the index of the first branch whose conditions match
// the request, or -1 if no branch matches. If the scenario has no branches,
// returns -1 as well. Branch matching ignores endpoint/method (they live at
// scenario level); it checks only ICAP headers, HTTP headers, HTTP URL, and
// HTTP method declared on the branch.
func (s *Scenario) SelectBranch(req *icap.Request) int {
	for i := range s.Branches {
		if branchMatches(&s.Branches[i], req) {
			return i
		}
	}
	return -1
}

// branchMatches mirrors the header/HTTP checks done in the main matcher but
// against a Branch's MatchRule and its pre-compiled regexps.
func branchMatches(b *Branch, req *icap.Request) bool { //nolint:gocyclo // mirrors scenario-level checks which are themselves a list of AND clauses
	if !headerRulesMatch(b.Match.Headers, b.compiledHeaders, b.compiledHeaderLists, req.Header.Get) {
		return false
	}
	if b.Match.HTTPMethod != "" {
		if req.HTTPRequest == nil || req.HTTPRequest.Method != b.Match.HTTPMethod {
			return false
		}
	}
	if len(b.Match.HTTPHeaders) > 0 || len(b.Match.HTTPHeaderContains) > 0 {
		if !hasEncapsulatedHTTP(req) {
			return false
		}
		if !headerRulesMatch(b.Match.HTTPHeaders, b.compiledHTTPHeaders, b.compiledHTTPLists, func(key string) (string, bool) {
			return httpHeaderLookup(req, key)
		}) {
			return false
		}
	}
	if b.Match.HTTPURL != "" {
		// URL lives on the original HTTP request even in RESPMOD (the "req-hdr"
		// part of the Encapsulated header).
		if req.HTTPRequest == nil {
			return false
		}
		if b.compiledHTTPURL != nil {
			if !b.compiledHTTPURL.MatchString(req.HTTPRequest.URI) {
				return false
			}
		} else if req.HTTPRequest.URI != b.Match.HTTPURL {
			return false
		}
	}
	return true
}

// CompiledPath returns the first compiled path regex, or nil if none. Kept for
// backward compatibility with callers (e.g. match-test CLI); for scenarios with
// multiple endpoints, use CompiledPaths.
func (s *Scenario) CompiledPath() *regexp.Regexp {
	if len(s.compiledPaths) == 0 {
		return nil
	}
	return s.compiledPaths[0].re
}

// CompiledPaths returns all compiled endpoint regexps for this scenario.
func (s *Scenario) CompiledPaths() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(s.compiledPaths))
	for _, c := range s.compiledPaths {
		out = append(out, c.re)
	}
	return out
}

// CompiledBody returns the compiled body regex, or nil if not set.
func (s *Scenario) CompiledBody() *regexp.Regexp { return s.compiledBody }

// MatchRule defines criteria for matching ICAP requests.
type MatchRule struct {
	// Routes binds methods to their exact endpoint sets for v2 route-aware
	// scenarios. When non-empty, Routes is the method/path matching source of
	// truth; Methods and Paths contain deterministic compatibility unions.
	Routes RouteMap `yaml:"routes,omitempty" json:"routes,omitempty"`

	// Path is a single regex pattern to match the ICAP URI path. Kept for v1
	// scenario files; v2 files use Paths (set from the "endpoint:" YAML key).
	// Empty string matches all paths.
	Path string `yaml:"path_pattern,omitempty" json:"path_pattern,omitempty"`

	// Paths is a list of endpoint patterns a scenario accepts. Each entry may
	// be a concrete path ("/scan") or a pattern with "{name}" captures
	// ("/env/{id}/scan"); "{name}" compiles to a regex-named capture group
	// "[^/]+" and the captured value is available as "${name}" in response
	// fields. If Paths is non-empty, Path is ignored.
	Paths []string `yaml:"paths,omitempty" json:"paths,omitempty"`

	// Methods is the set of ICAP methods (REQMOD, RESPMOD, OPTIONS) this
	// scenario applies to. An empty list matches any method. In YAML the field
	// accepts either a single string ("icap_method: REQMOD") or a sequence
	// ("icap_method: [REQMOD, RESPMOD]") — MethodList handles both shapes.
	// The YAML tag stays singular for backward compatibility with v1 files.
	Methods MethodList `yaml:"icap_method,omitempty" json:"icap_method,omitempty"`

	// HTTPMethod is the HTTP method to match in embedded requests.
	// Empty string matches all HTTP methods.
	HTTPMethod string `yaml:"http_method,omitempty" json:"http_method,omitempty"`

	// HTTPURL is an exact or "re:"-prefixed regex pattern applied to the URI of the
	// encapsulated HTTP request (e.g., "http://host/path/file.exe?q=1"). Useful for
	// matching by filename when no identifying ICAP header is present.
	// Empty string matches any URL.
	HTTPURL string `yaml:"http_url,omitempty" json:"http_url,omitempty"`

	// Headers contains scalar exact/regex criteria for ICAP headers.
	// All specified headers must match for the scenario to match.
	// Values may be exact strings or "re:"-prefixed regex patterns.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// HeaderContains contains list alternatives. Generic headers use
	// case-insensitive substrings; Content-Type uses media-type-aware matching.
	HeaderContains map[string][]string `yaml:"header_contains,omitempty" json:"header_contains,omitempty"`

	// HTTPHeaders contains scalar match criteria for headers of the encapsulated HTTP
	// request (REQMOD) or response (RESPMOD). All specified headers must match.
	// Values may be exact strings or "re:"-prefixed regex patterns.
	HTTPHeaders map[string]string `yaml:"http_headers,omitempty" json:"http_headers,omitempty"`

	// HTTPHeaderContains is the encapsulated-HTTP counterpart of HeaderContains.
	HTTPHeaderContains map[string][]string `yaml:"http_header_contains,omitempty" json:"http_header_contains,omitempty"`

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
//
// Two layers of headers and body:
//
//   - Headers / Body / BodyFile — on the ICAP-envelope response.
//   - HTTPHeaders / HTTPBody / HTTPBodyFile — on the encapsulated HTTP
//     response (used together with HTTPStatus != 0 to synthesize a block
//     page or similar).
type ResponseTemplate struct {
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	HTTPHeaders  map[string]string `yaml:"http_headers,omitempty" json:"http_headers,omitempty"`
	DelayRange   *DelayConfig      `yaml:"-" json:"-"`
	Stream       *StreamConfig     `yaml:"stream,omitempty" json:"stream,omitempty"`
	Block        *bool             `yaml:"block,omitempty" json:"block,omitempty"`
	ResponseName string            `yaml:"-" json:"-"`
	Use          string            `yaml:"use,omitempty" json:"use,omitempty"`
	Body         string            `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile     string            `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	HTTPBody     string            `yaml:"http_body,omitempty" json:"http_body,omitempty"`
	HTTPBodyFile string            `yaml:"http_body_file,omitempty" json:"http_body_file,omitempty"`
	Error        string            `yaml:"error,omitempty" json:"error,omitempty"`
	Status       int               `yaml:"status,omitempty" json:"status,omitempty"`
	ICAPStatus   int               `yaml:"icap_status" json:"icap_status"`
	HTTPStatus   int               `yaml:"http_status,omitempty" json:"http_status,omitempty"`
	Delay        time.Duration     `yaml:"delay,omitempty" json:"delay,omitempty"`
}

// ScenarioFile represents the YAML structure for scenario files.
type ScenarioFile struct {
	Responses map[string]ResponseTemplate `yaml:"responses,omitempty"`
	Scenarios []Scenario                  `yaml:"scenarios"`
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
	bodyPattern BodyPatternOptions
	filePath    string
	scenarios   []*Scenario
	mu          sync.RWMutex
}

// NewScenarioRegistry creates a new scenario registry.
func NewScenarioRegistry() ScenarioRegistry {
	return NewScenarioRegistryWithBodyPatternOptions(DefaultBodyPatternOptions())
}

// NewScenarioRegistryWithBodyPatternOptions creates a registry with body_pattern limits.
func NewScenarioRegistryWithBodyPatternOptions(options BodyPatternOptions) ScenarioRegistry {
	return &scenarioRegistry{
		bodyPattern: options.normalized(),
		scenarios:   []*Scenario{DefaultScenario()},
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

	scenarios, err := loadScenariosFromData(data, path)
	if err != nil {
		return err
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
	for i := range scenarios {
		scenarios[i].order = i
	}

	// Convert to pointers for storage
	r.scenarios = make([]*Scenario, len(scenarios))
	for i := range scenarios {
		r.scenarios[i] = &scenarios[i]
	}
	r.filePath = path

	// Sort by priority (descending)
	sortScenariosByPriority(r.scenarios)

	return nil
}

func loadScenariosFromData(data []byte, path string) ([]Scenario, error) {
	if removedKeyErr := rejectRemovedScenarioYAMLKeys(data, path); removedKeyErr != nil {
		return nil, removedKeyErr
	}

	isV2, orderedNames, detectErr := detectV2Format(data)
	if detectErr != nil {
		return nil, NewScenarioParseError(path, detectErr)
	}
	if !isV2 {
		return loadScenarioFileV1(data, path)
	}

	return loadScenarioFileV2(data, path, orderedNames)
}

func loadScenarioFileV2(data []byte, path string, orderedNames []string) ([]Scenario, error) {
	var sf ScenarioFileV2
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, NewScenarioParseError(path, err)
	}

	converted, err := ConvertV2ToScenarios(&sf, orderedNames)
	if err != nil {
		return nil, &ScenarioError{
			Operation:  "convert_v2",
			FilePath:   path,
			Message:    err.Error(),
			Suggestion: "check v2 scenario format",
		}
	}

	scenarios := make([]Scenario, 0, len(converted))
	for _, scenario := range converted {
		scenarios = append(scenarios, *scenario)
	}

	return scenarios, nil
}

// detectV2Format checks if the YAML data uses v2 format (scenarios is a mapping).
// Returns true for v2, false for v1, and the ordered scenario names for v2 files.
func detectV2Format(data []byte) (isV2 bool, names []string, err error) {
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

	// A v2 file may intentionally contain only defaults (for example,
	// defaults.use as a file-wide fallback). Treat that as v2 so defaults are not
	// silently ignored by the legacy v1 loader.
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		key := mapping.Content[i]
		value := mapping.Content[i+1]
		if key.Value == "defaults" && value.Kind == yaml.MappingNode {
			return true, nil, nil
		}
	}

	// No v2 keys found — treat as v1.
	return false, nil, nil
}

func loadScenarioFileV1(data []byte, path string) ([]Scenario, error) {
	var sf ScenarioFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, NewScenarioParseError(path, err)
	}
	if err := resolveScenarioResponseRefs(sf.Scenarios, sf.Responses); err != nil {
		return nil, &ScenarioError{
			Operation:  "resolve_responses",
			FilePath:   path,
			Message:    err.Error(),
			Suggestion: "check response use references",
		}
	}
	return sf.Scenarios, nil
}

// validateAndCompile validates a scenario and compiles regex patterns.
// It returns detailed ScenarioError instances with suggestions for fixes.
func (r *scenarioRegistry) validateAndCompile(s *Scenario) error { //nolint:gocyclo // validation requires checking each field independently
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

	// Scenarios can be revalidated after an in-memory update. Never retain
	// compiled exact routes after switching back to legacy method/path fields.
	s.compiledRoutes = nil
	if len(s.Match.Routes) > 0 {
		if err := validateRouteMap("scenario "+s.Name+" routes", s.Match.Routes); err != nil {
			return err
		}
		s.compiledRoutes = make(map[string][]compiledEndpoint, len(s.Match.Routes))
		for _, method := range orderedRouteMethods(s.Match.Routes) {
			for _, endpoint := range s.Match.Routes[method] {
				compiled, err := compileEndpoint(endpoint)
				if err != nil {
					return NewScenarioRegexError("", s.Name, "match.routes."+method, endpoint, err)
				}
				if err := validateReservedExactRoute(method, endpoint, compiled); err != nil {
					return fmt.Errorf("scenario %q: %w", s.Name, err)
				}
				s.compiledRoutes[method] = append(s.compiledRoutes[method], compiled)
			}
		}
	}

	// Compile endpoint list (v2 semantics, with "{name}" captures). If Paths is
	// set, use it; otherwise fall back to the legacy single Path field (raw
	// regex, no capture support).
	switch {
	case len(s.Match.Paths) > 0:
		s.compiledPaths = make([]compiledEndpoint, 0, len(s.Match.Paths))
		for _, p := range s.Match.Paths {
			ce, err := compileEndpoint(p)
			if err != nil {
				return NewScenarioRegexError("", s.Name, "match.endpoint", p, err)
			}
			s.compiledPaths = append(s.compiledPaths, ce)
		}
	case s.Match.Path != "":
		re, err := regexp.Compile(s.Match.Path)
		if err != nil {
			return NewScenarioRegexError("", s.Name, "match.path_pattern", s.Match.Path, err)
		}
		s.compiledPaths = []compiledEndpoint{{re: re, raw: s.Match.Path}}
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
	if s.Response.ICAPStatus == 0 && s.Response.Status != 0 {
		s.Response.ICAPStatus = s.Response.Status
	}
	if s.Response.ICAPStatus == 0 {
		s.Response.ICAPStatus = 204 // Default to No Content
	}

	// Validate response body file path
	if s.Response.BodyFile != "" {
		if _, err := os.Stat(s.Response.BodyFile); err != nil { //nolint:gosec // path comes from a loaded scenario file, not end-user input
			return NewScenarioBodyFileError(
				"", // file path will be set by caller
				s.Name,
				s.Response.BodyFile,
				err,
			)
		}
	}
	if err := validateResponseStreaming(s.Name+" response", &s.Response, s.Match.Methods); err != nil {
		return err
	}
	if err := validateRuntimeWeightTotal(s.Name, s.WeightedResponses); err != nil {
		return err
	}
	if err := validateWeightedStreaming(s.Name, s.WeightedResponses, s.Match.Methods); err != nil {
		return err
	}
	if err := normalizeHeaderContainsRules(s.Name+" match.headers", s.Match.Headers, s.Match.HeaderContains); err != nil {
		return err
	}
	if err := normalizeHeaderContainsRules(s.Name+" match.http_headers", s.Match.HTTPHeaders, s.Match.HTTPHeaderContains); err != nil {
		return err
	}
	compiledHeaderLists, compileErr := compileHeaderListMatchers(s.Name+" match.headers", s.Match.HeaderContains)
	if compileErr != nil {
		return compileErr
	}
	s.compiledHeaderLists = compiledHeaderLists
	compiledHTTPLists, compileErr := compileHeaderListMatchers(s.Name+" match.http_headers", s.Match.HTTPHeaderContains)
	if compileErr != nil {
		return compileErr
	}
	s.compiledHTTPLists = compiledHTTPLists

	// Compile header patterns with re: prefix
	for key, value := range s.Match.Headers {
		if !strings.HasPrefix(value, "re:") {
			continue
		}
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

	// Compile HTTP header patterns with re: prefix
	for key, value := range s.Match.HTTPHeaders {
		if !strings.HasPrefix(value, "re:") {
			continue
		}
		pattern := strings.TrimPrefix(value, "re:")
		re, err := regexp.Compile(pattern)
		if err != nil {
			return NewScenarioRegexError("", s.Name, "match.http_headers."+key, value, err)
		}
		if s.compiledHTTPHeaders == nil {
			s.compiledHTTPHeaders = make(map[string]*regexp.Regexp)
		}
		s.compiledHTTPHeaders[key] = re
	}

	// Compile HTTP URL pattern (regex with re: prefix)
	if strings.HasPrefix(s.Match.HTTPURL, "re:") {
		pattern := strings.TrimPrefix(s.Match.HTTPURL, "re:")
		re, err := regexp.Compile(pattern)
		if err != nil {
			return NewScenarioRegexError("", s.Name, "match.http_url", s.Match.HTTPURL, err)
		}
		s.compiledHTTPURL = re
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

	// Compile regex patterns inside branches (if any) — headers, HTTP headers
	// and the HTTP URL. Endpoints/methods are not per-branch.
	for idx := range s.Branches {
		b := &s.Branches[idx]
		where := fmt.Sprintf("%s branch #%d", s.Name, idx+1)
		if err := normalizeHeaderContainsRules(where+" when", b.Match.Headers, b.Match.HeaderContains); err != nil {
			return err
		}
		if err := normalizeHeaderContainsRules(where+" when_http.headers", b.Match.HTTPHeaders, b.Match.HTTPHeaderContains); err != nil {
			return err
		}
		compiledHeaderLists, compileErr := compileHeaderListMatchers(where+" when", b.Match.HeaderContains)
		if compileErr != nil {
			return compileErr
		}
		b.compiledHeaderLists = compiledHeaderLists
		compiledHTTPLists, compileErr := compileHeaderListMatchers(where+" when_http.headers", b.Match.HTTPHeaderContains)
		if compileErr != nil {
			return compileErr
		}
		b.compiledHTTPLists = compiledHTTPLists
		if b.Response.ICAPStatus == 0 && b.Response.Status != 0 {
			b.Response.ICAPStatus = b.Response.Status
		}
		if err := validateResponseStreaming(where, &b.Response, s.Match.Methods); err != nil {
			return err
		}
		if err := validateRuntimeWeightTotal(where, b.WeightedResponses); err != nil {
			return err
		}
		if err := validateWeightedStreaming(where, b.WeightedResponses, s.Match.Methods); err != nil {
			return err
		}
		for key, value := range b.Match.Headers {
			if !strings.HasPrefix(value, "re:") {
				continue
			}
			re, err := regexp.Compile(strings.TrimPrefix(value, "re:"))
			if err != nil {
				return NewScenarioRegexError("", s.Name, fmt.Sprintf("branches[%d].when.%s", idx, key), value, err)
			}
			if b.compiledHeaders == nil {
				b.compiledHeaders = make(map[string]*regexp.Regexp)
			}
			b.compiledHeaders[key] = re
		}
		for key, value := range b.Match.HTTPHeaders {
			if !strings.HasPrefix(value, "re:") {
				continue
			}
			re, err := regexp.Compile(strings.TrimPrefix(value, "re:"))
			if err != nil {
				return NewScenarioRegexError("", s.Name, fmt.Sprintf("branches[%d].when_http.headers.%s", idx, key), value, err)
			}
			if b.compiledHTTPHeaders == nil {
				b.compiledHTTPHeaders = make(map[string]*regexp.Regexp)
			}
			b.compiledHTTPHeaders[key] = re
		}
		if strings.HasPrefix(b.Match.HTTPURL, "re:") {
			re, err := regexp.Compile(strings.TrimPrefix(b.Match.HTTPURL, "re:"))
			if err != nil {
				return NewScenarioRegexError("", s.Name, fmt.Sprintf("branches[%d].when_http.url", idx), b.Match.HTTPURL, err)
			}
			b.compiledHTTPURL = re
		}
	}

	return nil
}

func validateRuntimeWeightTotal(where string, variants []WeightedResponse) error {
	if variants == nil {
		return nil
	}
	if len(variants) == 0 {
		return fmt.Errorf("%s: responses must contain at least one weighted variant", where)
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

// Match finds the first scenario that matches the given request.
// Scenarios are checked in priority order (highest first).
// Returns a detailed error if no scenario matches, including what was checked.
func (r *scenarioRegistry) Match(ctx context.Context, req *icap.Request) (*Scenario, error) {
	if req == nil {
		return nil, NewScenarioMatchError(
			"cannot match against nil request",
			nil,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	scenarios := append([]*Scenario(nil), r.scenarios...)
	r.mu.RUnlock()

	checkedCount := 0
	for _, s := range scenarios {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		checkedCount++
		matched, err := r.matches(ctx, s, req)
		if err != nil {
			return nil, err
		}
		if matched {
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
func (r *scenarioRegistry) matches(ctx context.Context, s *Scenario, req *icap.Request) (bool, error) {
	return matchesScenario(ctx, s, req, r.bodyPattern)
}

func matchesScenario(ctx context.Context, s *Scenario, req *icap.Request, bodyPattern BodyPatternOptions) (bool, error) { //nolint:gocyclo // scenario matching checks each rule field sequentially
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var routePaths []compiledEndpoint
	if s.compiledRoutes != nil {
		var ok bool
		routePaths, ok = s.compiledRoutes[req.Method]
		if !ok {
			return false, nil
		}
	} else {
		if !methodMatches(s.Match.Methods, req.Method) {
			return false, nil
		}
		routePaths = s.compiledPaths
	}

	// Check endpoint(s). Any one match is enough; capture names from the
	// matched endpoint are merged onto req.Captures for use in response
	// substitution.
	if len(routePaths) > 0 {
		caps, ok := matchEndpoint(routePaths, extractPath(req.URI))
		if !ok {
			return false, nil
		}
		if len(caps) > 0 {
			if req.Captures == nil {
				req.Captures = make(map[string]string, len(caps))
			}
			for k, v := range caps {
				req.Captures[k] = v
			}
		}
	}

	// Check headers (all must match — exact, regex, or contains-any list).
	if !headerRulesMatch(s.Match.Headers, s.compiledHeaders, s.compiledHeaderLists, req.Header.Get) {
		return false, nil
	}

	// Check HTTP method - if scenario specifies HTTP method, request must have HTTP request
	if s.Match.HTTPMethod != "" {
		if req.HTTPRequest == nil {
			return false, nil
		}
		if req.HTTPRequest.Method != s.Match.HTTPMethod {
			return false, nil
		}
	}

	// Check encapsulated HTTP headers. For RESPMOD the scanned headers live in
	// req.HTTPResponse (Content-Type, Content-Length, …); httpHeaderLookup picks
	// the right side by ICAP method and falls back to the other side.
	if len(s.Match.HTTPHeaders) > 0 || len(s.Match.HTTPHeaderContains) > 0 {
		if !hasEncapsulatedHTTP(req) {
			return false, nil
		}
		if !headerRulesMatch(s.Match.HTTPHeaders, s.compiledHTTPHeaders, s.compiledHTTPLists, func(key string) (string, bool) {
			return httpHeaderLookup(req, key)
		}) {
			return false, nil
		}
	}

	// Check encapsulated HTTP URL. The URL lives on the original HTTP request
	// even for RESPMOD (req-hdr part of Encapsulated).
	if s.Match.HTTPURL != "" {
		if req.HTTPRequest == nil {
			return false, nil
		}
		if s.compiledHTTPURL != nil {
			if !s.compiledHTTPURL.MatchString(req.HTTPRequest.URI) {
				return false, nil
			}
		} else if req.HTTPRequest.URI != s.Match.HTTPURL {
			return false, nil
		}
	}

	// Check body pattern - load body only if pattern exists (lazy loading)
	if s.compiledBody != nil {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		msg := bodyPatternMessage(req)
		if msg == nil {
			return false, nil
		}
		matched, err := bodyPatternMatches(ctx, s.compiledBody, msg, bodyPattern)
		if err != nil || !matched {
			return false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Check client IP (exact match)
	if s.Match.ClientIP != "" {
		if !matchClientIP(s.Match.ClientIP, req.ClientIP) {
			return false, nil
		}
	}

	// Check CIDR ranges
	if len(s.compiledCIDRs) > 0 {
		if !matchByCIDR(s.compiledCIDRs, req.ClientIP) {
			return false, nil
		}
	}

	// If the scenario has branches, require at least one to match; otherwise
	// treat the scenario as non-matching so the registry tries the next one.
	if len(s.Branches) > 0 && s.SelectBranch(req) < 0 {
		return false, nil
	}

	return true, nil
}

func bodyPatternMatches(
	ctx context.Context,
	pattern *regexp.Regexp,
	msg *icap.HTTPMessage,
	options BodyPatternOptions,
) (bool, error) {
	stopCancellation := func() bool { return false }
	if closer, ok := msg.BodyReader.(io.Closer); ok {
		stopCancellation = context.AfterFunc(ctx, func() { _ = closer.Close() })
	}
	defer stopCancellation()
	body, err := readBodyForPattern(msg, options.normalized())
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil {
		return false, handleBodyPatternReadError(err, options.normalized())
	}
	return pattern.Match(body), nil
}

func bodyPatternMessage(req *icap.Request) *icap.HTTPMessage {
	switch req.Method {
	case icap.MethodRESPMOD:
		return req.HTTPResponse
	case icap.MethodREQMOD:
		return req.HTTPRequest
	}
	if req.HTTPRequest != nil {
		return req.HTTPRequest
	}
	return req.HTTPResponse
}

func readBodyForPattern(msg *icap.HTTPMessage, options BodyPatternOptions) ([]byte, error) {
	if options.Limit < 0 {
		return msg.GetBody()
	}
	return msg.GetBodyLimited(options.Limit)
}

func handleBodyPatternReadError(err error, options BodyPatternOptions) error {
	if errors.Is(err, icap.ErrBodyTooLarge) {
		if options.LimitAction == BodyPatternLimitActionNoMatch {
			return nil
		}
		return fmt.Errorf("%w: max %d bytes", ErrBodyPatternLimitExceeded, options.Limit)
	}
	return fmt.Errorf("reading body for body_pattern: %w", err)
}

// endpointCapturePattern finds "{name}" placeholders in an endpoint string.
var endpointCapturePattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

var reservedICAPServicePaths = map[string]string{
	"/reqmod":  icap.MethodREQMOD,
	"/respmod": icap.MethodRESPMOD,
	"/options": icap.MethodOPTIONS,
}

func validateReservedExactRoute(method, raw string, compiled compiledEndpoint) error {
	isPattern := strings.HasPrefix(raw, "re:") || strings.Contains(raw, "{")
	for path, reservedMethod := range reservedICAPServicePaths {
		if isPattern {
			if compiled.re != nil && compiled.re.MatchString(path) {
				return fmt.Errorf("route pattern %q matches reserved service path %q", raw, path)
			}
			continue
		}
		if raw != path {
			continue
		}
		if reservedMethod == icap.MethodOPTIONS || method != reservedMethod {
			return fmt.Errorf("route %s -> %s conflicts with reserved service path", method, raw)
		}
	}
	return nil
}

// CompileEndpointRegex exposes the endpoint-pattern compiler for callers that
// need the raw regex (router pattern registration). The returned regex is
// anchored with ^…$; "{name}" placeholders become named captures.
func CompileEndpointRegex(raw string) (*regexp.Regexp, error) {
	ce, err := compileEndpoint(raw)
	if err != nil {
		return nil, err
	}
	return ce.re, nil
}

// compileEndpoint converts an endpoint declaration (v2 "endpoint:" value) into
// a compiled regex plus the list of capture names. A string prefixed with
// "re:" is treated as a raw regex; otherwise "{name}" placeholders become
// named capture groups "(?P<name>[^/]+)" and the rest of the string is
// regex-escaped, anchored with ^…$.
func compileEndpoint(raw string) (compiledEndpoint, error) {
	if raw == "" {
		return compiledEndpoint{}, nil
	}
	if strings.HasPrefix(raw, "re:") {
		re, err := regexp.Compile(strings.TrimPrefix(raw, "re:"))
		if err != nil {
			return compiledEndpoint{}, err
		}
		return compiledEndpoint{re: re, raw: raw}, nil
	}
	captures := make([]string, 0)
	var b strings.Builder
	b.WriteByte('^')
	last := 0
	for _, m := range endpointCapturePattern.FindAllStringSubmatchIndex(raw, -1) {
		b.WriteString(regexp.QuoteMeta(raw[last:m[0]]))
		name := raw[m[2]:m[3]]
		captures = append(captures, name)
		b.WriteString(`(?P<`)
		b.WriteString(name)
		b.WriteString(`>[^/]+)`)
		last = m[1]
	}
	b.WriteString(regexp.QuoteMeta(raw[last:]))
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return compiledEndpoint{}, err
	}
	return compiledEndpoint{re: re, captures: captures, raw: raw}, nil
}

// matchEndpoint tries each compiled endpoint in order; on success returns the
// captured values (may be empty if the matched endpoint has no "{name}"s).
// Returns (nil, false) if no endpoint matched; (empty map, true) if at least
// one endpoint matched but defined no captures. Query string and fragment are
// stripped from reqPath before matching so endpoints written without them
// still match real requests.
func matchEndpoint(paths []compiledEndpoint, reqPath string) (map[string]string, bool) {
	if q := strings.IndexByte(reqPath, '?'); q >= 0 {
		reqPath = reqPath[:q]
	}
	if h := strings.IndexByte(reqPath, '#'); h >= 0 {
		reqPath = reqPath[:h]
	}
	for _, p := range paths {
		if p.re == nil {
			continue
		}
		m := p.re.FindStringSubmatch(reqPath)
		if m == nil {
			continue
		}
		if len(p.captures) == 0 {
			return map[string]string{}, true
		}
		caps := make(map[string]string, len(p.captures))
		for i, name := range p.re.SubexpNames() {
			if i == 0 || name == "" {
				continue
			}
			caps[name] = m[i]
		}
		return caps, true
	}
	return nil, false
}

func normalizeHeaderContainsRules(where string, exact map[string]string, rules map[string][]string) error {
	for key, values := range rules {
		if _, conflict := exact[key]; conflict {
			return fmt.Errorf("%s.%s cannot use both scalar and list matchers", where, key)
		}
		if len(values) == 0 {
			return fmt.Errorf("%s.%s list must not be empty", where, key)
		}
		for i, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s.%s list item #%d must not be empty", where, key, i+1)
			}
			if isContentTypeHeader(key) {
				values[i] = strings.TrimSpace(value)
			} else {
				values[i] = strings.ToLower(value)
			}
		}
		rules[key] = values
	}
	return nil
}

func compileHeaderListMatchers(where string, rules map[string][]string) (map[string]compiledHeaderListMatcher, error) {
	if len(rules) == 0 {
		return nil, nil //nolint:nilnil // no compiled matchers is a valid result
	}
	compiled := make(map[string]compiledHeaderListMatcher, len(rules))
	for key, values := range rules {
		matcher := compiledHeaderListMatcher{contains: values}
		if isContentTypeHeader(key) {
			var err error
			matcher, err = compileContentTypeListMatcher(values)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", where, key, err)
			}
		}
		compiled[key] = matcher
	}
	return compiled, nil
}

func compileContentTypeListMatcher(values []string) (compiledHeaderListMatcher, error) {
	matcher := compiledHeaderListMatcher{isContentType: true}
	for i, value := range values {
		mediaType, parameters, err := mime.ParseMediaType(value)
		if err != nil {
			return compiledHeaderListMatcher{}, fmt.Errorf("list item #%d %q is not a valid media type: %w", i+1, value, err)
		}
		mediaTypeName, mediaSubtype, hasSubtype := strings.Cut(mediaType, "/")
		if !hasSubtype || mediaTypeName == "" || mediaSubtype == "" {
			return compiledHeaderListMatcher{}, fmt.Errorf("list item #%d %q must use type/subtype media type syntax", i+1, value)
		}
		if strings.Contains(value, ";") {
			matcher.parameterized = append(matcher.parameterized, contentTypeMatcher{
				mediaType:  mediaType,
				parameters: parameters,
			})
			continue
		}
		if matcher.mediaTypes == nil {
			matcher.mediaTypes = make(map[string]struct{})
		}
		matcher.mediaTypes[mediaType] = struct{}{}
	}
	return matcher, nil
}

func isContentTypeHeader(key string) bool {
	return strings.EqualFold(key, "Content-Type")
}

func headerRulesMatch(
	exact map[string]string,
	compiledRegex map[string]*regexp.Regexp,
	compiledLists map[string]compiledHeaderListMatcher,
	lookup func(string) (string, bool),
) bool {
	for key, value := range exact {
		header, ok := lookup(key)
		if !ok {
			return false
		}
		if pattern, isRegex := compiledRegex[key]; isRegex {
			if !pattern.MatchString(header) {
				return false
			}
		} else if header != value {
			return false
		}
	}
	for key, matcher := range compiledLists {
		header, ok := lookup(key)
		if !ok || !matcher.matches(header) {
			return false
		}
	}
	return true
}

func (m compiledHeaderListMatcher) matches(value string) bool {
	if !m.isContentType {
		return containsAnyLower(strings.ToLower(value), m.contains)
	}
	return m.matchesContentType(value)
}

func (m compiledHeaderListMatcher) matchesContentType(value string) bool {
	mediaTypePrefix, _, _ := strings.Cut(value, ";")
	mediaTypePrefix = strings.ToLower(strings.TrimSpace(mediaTypePrefix))
	if _, ok := m.mediaTypes[mediaTypePrefix]; ok {
		return true
	}
	if len(m.parameterized) == 0 {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	for _, expected := range m.parameterized {
		if mediaType == expected.mediaType && contentTypeParametersMatch(parameters, expected.parameters) {
			return true
		}
	}
	return false
}

func contentTypeParametersMatch(actual, expected map[string]string) bool {
	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok {
			return false
		}
		if key == "charset" {
			if !strings.EqualFold(actualValue, expectedValue) {
				return false
			}
		} else if actualValue != expectedValue {
			return false
		}
	}
	return true
}

func containsAnyLower(value string, alternatives []string) bool {
	for _, alternative := range alternatives {
		if strings.Contains(value, alternative) {
			return true
		}
	}
	return false
}

// httpHeaderLookup returns the value of the given header from the appropriate
// encapsulated HTTP message for the request.
//
// For RESPMOD the scanned payload is the response (req.HTTPResponse) — that's
// where headers like Content-Type / Content-Length live. The wrapped request
// (req.HTTPRequest) is present too but only carries the original client
// request context (URI, Host, …). For REQMOD the scanned payload is the
// request itself.
//
// We try the "primary" side for the ICAP method first, then fall back to the
// other side — so users can still match on Host/User-Agent/cookies of the
// client request even in a RESPMOD scenario.
func httpHeaderLookup(req *icap.Request, key string) (string, bool) {
	if req.Method == icap.MethodRESPMOD {
		if req.HTTPResponse != nil {
			if v, ok := req.HTTPResponse.Header.Get(key); ok {
				return v, true
			}
		}
		if req.HTTPRequest != nil {
			return req.HTTPRequest.Header.Get(key)
		}
		return "", false
	}
	if req.HTTPRequest != nil {
		return req.HTTPRequest.Header.Get(key)
	}
	return "", false
}

// hasEncapsulatedHTTP reports whether the request carries either an
// encapsulated HTTP request or response. Used to short-circuit when_http
// matching for requests without any HTTP context (e.g. OPTIONS).
func hasEncapsulatedHTTP(req *icap.Request) bool {
	return req.HTTPRequest != nil || req.HTTPResponse != nil
}

// methodMatches reports whether req belongs to the set of accepted methods.
// An empty list means "any method".
func methodMatches(methods []string, reqMethod string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		if m == reqMethod {
			return true
		}
	}
	return false
}

// validICAPMethods is the closed set of ICAP methods a scenario can declare.
var validICAPMethods = map[string]bool{
	"REQMOD":  true,
	"RESPMOD": true,
	"OPTIONS": true,
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

	next := &scenarioRegistry{bodyPattern: r.bodyPattern}
	if err := next.Load(path); err != nil {
		return err
	}
	if err := ValidateExactRouteTopology(r.List(), next.List()); err != nil {
		return err
	}
	r.mu.Lock()
	r.scenarios = next.List()
	r.filePath = path
	r.mu.Unlock()
	return nil
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
	scenario.order = nextScenarioOrder(r.scenarios)
	for i, s := range r.scenarios {
		if s.Name == scenario.Name {
			scenario.order = s.order
			r.scenarios = append(r.scenarios[:i], r.scenarios[i+1:]...)
			break
		}
	}

	r.scenarios = append(r.scenarios, scenario)

	// Re-sort by priority
	sortScenariosByPriority(r.scenarios)

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
