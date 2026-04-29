// Copyright 2026 ICAP Mock

// Package management provides runtime management primitives for admin endpoints.
package management

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

const (
	defaultScenarioName    = "default"
	inlineScenarioBasePrio = 2000
)

var (
	// ErrCurrentConfigPathRequired is returned when no current config path is tracked.
	ErrCurrentConfigPathRequired = errors.New("current config path is not set")
	// ErrConfigPathRequired is returned when a requested config path is empty.
	ErrConfigPathRequired = errors.New("config path is required")
	// ErrUnsupportedRuntimeChange is returned when a live config change requires restart.
	ErrUnsupportedRuntimeChange = errors.New("unsupported live config change; restart required")
)

// ConfigLoadError preserves concrete config file load failures.
type ConfigLoadError struct {
	Err error
}

// Error implements the error interface.
func (e *ConfigLoadError) Error() string {
	return fmt.Sprintf("config load failed: %v", e.Err)
}

// Unwrap returns the underlying load error.
func (e *ConfigLoadError) Unwrap() error { return e.Err }

// ConfigValidationError preserves validator failures for API classification.
type ConfigValidationError struct {
	Errors []config.ValidationError
}

// Error implements the error interface.
func (e *ConfigValidationError) Error() string {
	return "config validation failed: " + validationSummary(e.Errors)
}

// UnsupportedRuntimeChangeError adds safe context to restart-required changes.
type UnsupportedRuntimeChangeError struct {
	Reason string
}

// Error implements the error interface.
func (e UnsupportedRuntimeChangeError) Error() string {
	if e.Reason == "" {
		return ErrUnsupportedRuntimeChange.Error()
	}
	return ErrUnsupportedRuntimeChange.Error() + ": " + e.Reason
}

// Is allows errors.Is(err, ErrUnsupportedRuntimeChange).
func (e UnsupportedRuntimeChangeError) Is(target error) bool {
	return target == ErrUnsupportedRuntimeChange
}

// ScenarioApplyFunc applies in-memory scenarios to a freshly loaded registry.
type ScenarioApplyFunc func(storage.ScenarioRegistry) error

// ScenarioSet describes one runtime scenario source.
type ScenarioSet struct {
	Apply       ScenarioApplyFunc
	NewRegistry func() storage.ScenarioRegistry
	Registry    *ManagedScenarioRegistry
	Name        string
	Dir         string
	Server      config.ServerConfig
}

// ScenarioSetCount reports the number of active scenarios for one scenario set.
type ScenarioSetCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RuntimeManager tracks mutable runtime state used by management endpoints.
type RuntimeManager struct {
	newRegistry func() storage.ScenarioRegistry
	validator   *config.Validator
	cfg         *config.Config
	configPath  string
	sets        []ScenarioSet
	onApply     []func(*config.Config)
	mu          sync.Mutex
}

// NewRuntimeManager creates a runtime manager for the current configuration.
func NewRuntimeManager(cfg *config.Config, configPath string) *RuntimeManager {
	return &RuntimeManager{
		cfg:         cfg,
		configPath:  normalizePath(configPath),
		validator:   config.NewValidator(),
		newRegistry: storage.NewShardedScenarioRegistry,
	}
}

// CurrentConfigPath returns the current tracked config path.
func (m *RuntimeManager) CurrentConfigPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.configPath
}

// RegisterScenarioSet registers a reloadable scenario source.
func (m *RuntimeManager) RegisterScenarioSet(set ScenarioSet) {
	if set.Registry == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets = append(m.sets, set)
}

// RegisterConfigApplyFunc registers a callback for live-applied config updates.
func (m *RuntimeManager) RegisterConfigApplyFunc(fn func(*config.Config)) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onApply = append(m.onApply, fn)
}

// ReloadScenarios reloads every registered scenario set and swaps them together.
func (m *RuntimeManager) ReloadScenarios(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	replacements, err := m.buildScenarioReplacements(ctx)
	if err != nil {
		return err
	}
	m.applyScenarioReplacements(m.sets, replacements)
	return nil
}

// ScenarioCount returns the total active scenario count across registered sets.
func (m *RuntimeManager) ScenarioCount() int {
	total := 0
	for _, setCount := range m.ScenarioCounts() {
		total += setCount.Count
	}
	return total
}

// ScenarioCounts returns per-set scenario counts sorted by scenario set name.
func (m *RuntimeManager) ScenarioCounts() []ScenarioSetCount {
	m.mu.Lock()
	sets := append([]ScenarioSet(nil), m.sets...)
	m.mu.Unlock()
	sortScenarioSets(sets)
	return scenarioSetCounts(sets)
}

// ReloadCurrentConfig validates and applies reloadable current config contents.
func (m *RuntimeManager) ReloadCurrentConfig(ctx context.Context) error {
	m.mu.Lock()
	path := m.configPath
	m.mu.Unlock()
	if path == "" {
		return ErrCurrentConfigPathRequired
	}
	return m.LoadConfigFromPath(ctx, path)
}

// LoadConfigFromPath validates a config file and applies reloadable changes atomically.
func (m *RuntimeManager) LoadConfigFromPath(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return ErrConfigPathRequired
	}
	cfg, absPath, err := m.loadAndValidate(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	sets, replacements, err := m.planConfigApply(ctx, cfg)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.applyScenarioReplacements(sets, replacements)
	m.cfg = cfg
	m.configPath = absPath
	callbacks := append([]func(*config.Config){}, m.onApply...)
	m.mu.Unlock()
	notifyConfigApplied(callbacks, cfg)
	return nil
}

func (m *RuntimeManager) planConfigApply(ctx context.Context, cfg *config.Config) ([]ScenarioSet, []storage.ScenarioRegistry, error) {
	if err := rejectUnsupportedRuntimeChange(m.cfg, cfg, len(m.onApply) > 0); err != nil {
		return nil, nil, err
	}
	sets, err := m.updatedScenarioSets(cfg)
	if err != nil {
		return nil, nil, err
	}
	replacements, err := buildScenarioReplacements(ctx, sets, m.newRegistry)
	if err != nil {
		return nil, nil, err
	}
	return sets, replacements, nil
}

func (m *RuntimeManager) applyScenarioReplacements(sets []ScenarioSet, replacements []storage.ScenarioRegistry) {
	replaceScenarioSets(sets, replacements)
	m.sets = sets
}

func replaceScenarioSets(sets []ScenarioSet, replacements []storage.ScenarioRegistry) {
	locked := lockScenarioRegistries(sets)
	defer unlockScenarioRegistries(locked)
	for i, replacement := range replacements {
		if sets[i].Registry != nil && replacement != nil {
			sets[i].Registry.active = replacement
		}
	}
}

func lockScenarioRegistries(sets []ScenarioSet) []*ManagedScenarioRegistry {
	locked := make([]*ManagedScenarioRegistry, 0, len(sets))
	for _, set := range sets {
		if set.Registry == nil {
			continue
		}
		set.Registry.mu.Lock()
		locked = append(locked, set.Registry)
	}
	return locked
}

func unlockScenarioRegistries(registries []*ManagedScenarioRegistry) {
	for i := len(registries) - 1; i >= 0; i-- {
		registries[i].mu.Unlock()
	}
}

func notifyConfigApplied(callbacks []func(*config.Config), cfg *config.Config) {
	for _, fn := range callbacks {
		fn(cfg)
	}
}

func (m *RuntimeManager) buildScenarioReplacements(ctx context.Context) ([]storage.ScenarioRegistry, error) {
	return buildScenarioReplacements(ctx, m.sets, m.newRegistry)
}

func buildScenarioReplacements(
	ctx context.Context,
	sets []ScenarioSet,
	newRegistry func() storage.ScenarioRegistry,
) ([]storage.ScenarioRegistry, error) {
	replacements := make([]storage.ScenarioRegistry, len(sets))
	for i, set := range sets {
		registry, err := buildScenarioReplacement(ctx, set, newRegistry)
		if err != nil {
			return nil, err
		}
		replacements[i] = registry
	}
	return replacements, nil
}

func buildScenarioReplacement(
	ctx context.Context,
	set ScenarioSet,
	newRegistry func() storage.ScenarioRegistry,
) (storage.ScenarioRegistry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registry, err := LoadScenarioDirectory(set.Dir, set.registryFactory(newRegistry))
	if err != nil {
		return nil, fmt.Errorf("loading scenario set %q: %w", set.Name, err)
	}
	if err := applyScenarioSet(set, registry); err != nil {
		return nil, fmt.Errorf("applying scenario set %q: %w", set.Name, err)
	}
	return registry, nil
}

func (m *RuntimeManager) updatedScenarioSets(cfg *config.Config) ([]ScenarioSet, error) {
	if len(m.sets) == 0 {
		return nil, nil
	}
	specs := scenarioSpecsByName(cfg)
	if len(specs) != len(m.sets) {
		return nil, unsupportedRuntimeChange("scenario set topology changed")
	}
	updated := make([]ScenarioSet, len(m.sets))
	for i, set := range m.sets {
		spec, ok := specs[set.Name]
		if !ok || serverChanged(set.Server, spec.Server) {
			return nil, unsupportedRuntimeChange("server configuration changed")
		}
		updated[i] = set.withSpec(spec)
	}
	return updated, nil
}

func (s ScenarioSet) withSpec(spec ScenarioSet) ScenarioSet {
	s.Dir = spec.Dir
	s.Apply = spec.Apply
	s.Server = spec.Server
	return s
}

func (s ScenarioSet) registryFactory(fallback func() storage.ScenarioRegistry) func() storage.ScenarioRegistry {
	if s.NewRegistry != nil {
		return s.NewRegistry
	}
	return fallback
}

func scenarioSpecsByName(cfg *config.Config) map[string]ScenarioSet {
	entries := scenarioSpecs(cfg)
	specs := make(map[string]ScenarioSet, len(entries))
	for _, entry := range entries {
		specs[entry.Name] = entry
	}
	return specs
}

func scenarioSpecs(cfg *config.Config) []ScenarioSet {
	if len(cfg.Servers) == 0 {
		return []ScenarioSet{legacyScenarioSpec(cfg)}
	}
	sets := make([]ScenarioSet, 0, len(cfg.Servers))
	for _, name := range sortedServerNames(cfg.Servers) {
		entry := cfg.Servers[name]
		sets = append(sets, serverScenarioSpec(name, entry, cfg.Defaults))
	}
	return sets
}

func sortedServerNames(servers map[string]config.ServerEntryConfig) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortScenarioSets(sets []ScenarioSet) {
	sort.Slice(sets, func(i, j int) bool { return sets[i].Name < sets[j].Name })
}

func scenarioSetCounts(sets []ScenarioSet) []ScenarioSetCount {
	counts := make([]ScenarioSetCount, 0, len(sets))
	for _, set := range sets {
		counts = append(counts, ScenarioSetCount{Name: set.Name, Count: scenarioSetCount(set)})
	}
	return counts
}

func scenarioSetCount(set ScenarioSet) int {
	if set.Registry == nil {
		return 0
	}
	return len(set.Registry.List())
}

func legacyScenarioSpec(cfg *config.Config) ScenarioSet {
	return ScenarioSet{Name: defaultScenarioName, Dir: cfg.Mock.ScenariosDir, Server: cfg.Server}
}

func serverScenarioSpec(name string, entry config.ServerEntryConfig, defaults config.DefaultsConfig) ScenarioSet {
	return ScenarioSet{
		Name:   name,
		Dir:    entry.ScenariosDir,
		Server: entry.ToServerConfig(defaults),
		Apply:  inlineScenarioApplier(entry.Scenarios),
	}
}

func serverChanged(current, next config.ServerConfig) bool {
	return !reflect.DeepEqual(current, config.ServerConfig{}) && !reflect.DeepEqual(current, next)
}

func rejectUnsupportedRuntimeChange(current, next *config.Config, managementLive bool) error {
	if current == nil || reflect.DeepEqual(*current, config.Config{}) {
		return nil
	}
	if reflect.DeepEqual(staticConfigSnapshot(current, managementLive), staticConfigSnapshot(next, managementLive)) {
		return nil
	}
	return unsupportedRuntimeChange("static configuration changed")
}

func unsupportedRuntimeChange(reason string) error {
	return UnsupportedRuntimeChangeError{Reason: reason + "; restart required"}
}

func staticConfigSnapshot(cfg *config.Config, managementLive bool) config.Config {
	snapshot := *cfg
	snapshot.SourcePath = ""
	snapshot.Mock.ScenariosDir = ""
	snapshot.Servers = staticServerEntries(cfg.Servers)
	if managementLive {
		snapshot.Management = config.ManagementConfig{}
	}
	return snapshot
}

func staticServerEntries(servers map[string]config.ServerEntryConfig) map[string]config.ServerEntryConfig {
	if len(servers) == 0 {
		return nil
	}
	entries := make(map[string]config.ServerEntryConfig, len(servers))
	for name, entry := range servers {
		entry.ScenariosDir = ""
		entry.Scenarios = nil
		entries[name] = entry
	}
	return entries
}

func (m *RuntimeManager) loadAndValidate(path string) (*config.Config, string, error) {
	absPath := normalizePath(path)
	if err := validateConfigFile(absPath); err != nil {
		return nil, "", &ConfigLoadError{Err: err}
	}
	cfg, err := config.NewLoader().Load(config.LoadOptions{ConfigPath: absPath})
	if err != nil {
		return nil, "", &ConfigLoadError{Err: err}
	}
	if validationErrors := m.validator.Validate(cfg); len(validationErrors) > 0 {
		return nil, "", &ConfigValidationError{Errors: validationErrors}
	}
	cfg.SourcePath = absPath
	return cfg, absPath, nil
}

func validationSummary(validationErrors []config.ValidationError) string {
	messages := make([]string, 0, len(validationErrors))
	for _, err := range validationErrors {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

func applyScenarioSet(set ScenarioSet, registry storage.ScenarioRegistry) error {
	if set.Apply == nil {
		return nil
	}
	return set.Apply(registry)
}

func inlineScenarioApplier(inline map[string]config.InlineScenarioEntry) ScenarioApplyFunc {
	return func(registry storage.ScenarioRegistry) error {
		scenarios, err := convertInlineScenarios(inline)
		if err != nil {
			return err
		}
		for _, scenario := range scenarios {
			if err := registry.Add(scenario); err != nil {
				return err
			}
		}
		return nil
	}
}

func convertInlineScenarios(inline map[string]config.InlineScenarioEntry) ([]*storage.Scenario, error) {
	if len(inline) == 0 {
		return nil, nil
	}
	file, names := buildInlineScenarioFile(inline)
	scenarios, err := storage.ConvertV2ToScenarios(file, names)
	if err != nil {
		return nil, err
	}
	prioritizeInlineScenarios(scenarios)
	return scenarios, nil
}

func buildInlineScenarioFile(inline map[string]config.InlineScenarioEntry) (file *storage.ScenarioFileV2, names []string) {
	scenarios := make(map[string]storage.ScenarioEntryV2, len(inline))
	names = make([]string, 0, len(inline))
	for name, entry := range inline {
		scenarios[name] = convertInlineScenarioEntry(entry)
		names = append(names, name)
	}
	return &storage.ScenarioFileV2{Scenarios: scenarios}, names
}

func convertInlineScenarioEntry(entry config.InlineScenarioEntry) storage.ScenarioEntryV2 {
	return storage.ScenarioEntryV2{
		Method:     storage.MethodList(entry.Method),
		Endpoint:   storage.EndpointList(entry.Endpoint),
		Responses:  convertInlineResponses(entry.Responses),
		HTTPStatus: entry.HTTPStatus,
		BodyFile:   entry.BodyFile,
		Priority:   entry.Priority,
		Status:     entry.Status,
		Delay:      entry.Delay,
		When:       entry.When,
		Body:       entry.Body,
		Set:        entry.Set,
	}
}

func convertInlineResponses(entries []config.InlineWeightedResponse) []storage.WeightedResponseV2 {
	responses := make([]storage.WeightedResponseV2, len(entries))
	for i, response := range entries {
		responses[i] = storage.WeightedResponseV2{
			HTTPStatus: response.HTTPStatus,
			Weight:     response.Weight,
			Status:     response.Status,
			Delay:      response.Delay,
			Body:       response.Body,
			Set:        response.Set,
		}
	}
	return responses
}

func prioritizeInlineScenarios(scenarios []*storage.Scenario) {
	for i, scenario := range scenarios {
		scenario.Priority = inlineScenarioBasePrio - i
	}
}

func normalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}

// LoadScenarioDirectory loads all YAML scenario files into a single registry.
func LoadScenarioDirectory(dir string, newRegistry func() storage.ScenarioRegistry) (storage.ScenarioRegistry, error) {
	registry := newRegistry()
	if strings.TrimSpace(dir) == "" {
		return registry, nil
	}
	paths, err := scenarioFilePaths(dir)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err := mergeScenarioFile(registry, path, newRegistry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func scenarioFilePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading scenarios directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && isScenarioFile(entry.Name()) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths, nil
}

func isScenarioFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func mergeScenarioFile(dst storage.ScenarioRegistry, path string, newRegistry func() storage.ScenarioRegistry) error {
	tmp := newRegistry()
	if err := tmp.Load(path); err != nil {
		return err
	}
	for _, scenario := range tmp.List() {
		if scenario.Name == defaultScenarioName {
			continue
		}
		if err := dst.Add(scenario); err != nil {
			return err
		}
	}
	return nil
}
