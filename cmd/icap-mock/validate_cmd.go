// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/storage"
)

// ValidateCommand handles the validate-scenarios subcommand.
type ValidateCommand struct {
	fs  *flag.FlagSet
	dir string
}

// NewValidateCommand creates a new validate-scenarios command.
func NewValidateCommand() *ValidateCommand {
	cmd := &ValidateCommand{
		fs: flag.NewFlagSet("validate-scenarios", flag.ContinueOnError),
	}
	cmd.fs.StringVar(&cmd.dir, "dir", "./configs/scenarios", "Directory containing scenario YAML files")
	return cmd
}

// Name returns the command name.
func (c *ValidateCommand) Name() string {
	return "validate-scenarios"
}

// Description returns a short description of the command.
func (c *ValidateCommand) Description() string {
	return "Validate scenario YAML files for correctness"
}

// Parse parses the command arguments.
func (c *ValidateCommand) Parse(args []string) error {
	return c.fs.Parse(args)
}

// Usage prints the command usage.
func (c *ValidateCommand) Usage() {
	fmt.Fprintf(os.Stderr, "Usage: icap-mock validate-scenarios [options]\n\n")
	fmt.Fprintf(os.Stderr, "Validate scenario YAML files in a directory.\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	c.fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  # Validate scenarios in default directory\n")
	fmt.Fprintf(os.Stderr, "  icap-mock validate-scenarios\n\n")
	fmt.Fprintf(os.Stderr, "  # Validate scenarios in a custom directory\n")
	fmt.Fprintf(os.Stderr, "  icap-mock validate-scenarios --dir ./my-scenarios\n")
}

// Run executes the validate-scenarios command.
func (c *ValidateCommand) Run(_ context.Context) error {
	// Check that the directory exists.
	if _, err := os.Stat(c.dir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", c.dir)
	}

	// Walk directory for .yaml/.yml files.
	var yamlFiles []string
	err := filepath.WalkDir(c.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".yaml" || ext == ".yml" {
			yamlFiles = append(yamlFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking directory %s: %w", c.dir, err)
	}

	if len(yamlFiles) == 0 {
		fmt.Printf("No YAML files found in %s\n", c.dir)
		return nil
	}

	// Track all scenario names across files for uniqueness check.
	seenNames := make(map[string]string) // name -> first file that defined it

	allPassed := true

	for _, filePath := range yamlFiles {
		if !validateFile(filePath, seenNames) {
			allPassed = false
		}
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All scenarios passed validation.")
		return nil
	}
	return fmt.Errorf("one or more scenarios failed validation")
}

// validateFile validates all scenarios in a single YAML file. Returns true if all passed.
func validateFile(filePath string, seenNames map[string]string) bool {
	fmt.Printf("\nFile: %s\n", filePath)

	data, err := os.ReadFile(filePath) //nolint:gosec // path is validated
	if err != nil {
		fmt.Printf("  [FAIL] cannot read file: %v\n", err)
		return false
	}

	scenarios, err := loadScenariosForValidation(filePath, data)
	if err != nil {
		fmt.Printf("  [FAIL] cannot parse file: %v\n", err)
		return false
	}

	if len(scenarios) == 0 {
		fmt.Printf("  (no scenarios defined)\n")
		return true
	}

	allPassed := true
	for i := range scenarios {
		s := &scenarios[i]
		scenarioLabel := s.Name
		if scenarioLabel == "" {
			scenarioLabel = fmt.Sprintf("<unnamed #%d>", i+1)
		}

		errs := validateScenario(s, filePath, seenNames)
		if len(errs) == 0 {
			fmt.Printf("  [OK]   %s\n", scenarioLabel)
		} else {
			allPassed = false
			fmt.Printf("  [FAIL] %s\n", scenarioLabel)
			for _, e := range errs {
				fmt.Printf("           - %s\n", e)
			}
		}
	}
	return allPassed
}

// loadScenarios parses a scenario YAML file in either v1 or v2 format and
// returns a flat list of runtime scenarios.
func loadScenariosForValidation(filePath string, data []byte) ([]storage.Scenario, error) {
	var sf storage.ScenarioFile
	if err := yaml.Unmarshal(data, &sf); err == nil {
		return sf.Scenarios, nil
	}

	var sfV2 storage.ScenarioFileV2
	if err := yaml.Unmarshal(data, &sfV2); err != nil {
		return nil, fmt.Errorf("%w (v1/v2 decode)", err)
	}

	names, err := v2ScenarioNames(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse v2 scenario order: %w", err)
	}

	converted, err := storage.ConvertV2ToScenarios(&sfV2, names)
	if err != nil {
		return nil, fmt.Errorf("failed to convert v2 scenarios: %w", err)
	}

	out := make([]storage.Scenario, 0, len(converted))
	baseDir := filepath.Dir(filePath)
	for _, s := range converted {
		normalizeScenarioBodyFiles(s, baseDir)
		out = append(out, *s)
	}

	return out, nil
}

func normalizeScenarioBodyFiles(s *storage.Scenario, baseDir string) {
	normalizeResponseBodyFiles(&s.Response, baseDir)
	for idx := range s.Branches {
		b := &s.Branches[idx]
		normalizeResponseBodyFiles(&b.Response, baseDir)
		for widx := range b.WeightedResponses {
			normalizeWeightedResponseBodyFiles(&b.WeightedResponses[widx], baseDir)
		}
	}

	for idx := range s.WeightedResponses {
		normalizeWeightedResponseBodyFiles(&s.WeightedResponses[idx], baseDir)
	}
}

func normalizeResponseBodyFiles(r *storage.ResponseTemplate, baseDir string) {
	r.BodyFile = normalizeBodyFilePath(r.BodyFile, baseDir)
	r.HTTPBodyFile = normalizeBodyFilePath(r.HTTPBodyFile, baseDir)
}

func normalizeWeightedResponseBodyFiles(w *storage.WeightedResponse, baseDir string) {
	w.BodyFile = normalizeBodyFilePath(w.BodyFile, baseDir)
	w.HTTPBodyFile = normalizeBodyFilePath(w.HTTPBodyFile, baseDir)
}

func normalizeBodyFilePath(v, baseDir string) string {
	if v == "" || filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(baseDir, v)
}

// v2ScenarioNames returns scenario keys in YAML order for v2 files.
func v2ScenarioNames(data []byte) ([]string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, nil
	}

	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, nil
	}

	for i := 0; i < len(mapping.Content)-1; i += 2 {
		key := mapping.Content[i]
		value := mapping.Content[i+1]
		if key.Value == "scenarios" && value.Kind == yaml.MappingNode {
			names := make([]string, 0, len(value.Content)/2)
			for j := 0; j < len(value.Content)-1; j += 2 {
				names = append(names, value.Content[j].Value)
			}
			return names, nil
		}
	}

	return nil, nil
}

// validateScenario validates a single scenario and returns a list of errors.
func validateScenario(s *storage.Scenario, filePath string, seenNames map[string]string) []string {
	err := make([]string, 0, 7)
	err = append(err, validateScenarioName(s.Name, filePath, seenNames)...)
	err = append(err, validateScenarioRegex("path_pattern", s.Match.Path)...)
	err = append(err, validateScenarioRegex("body_pattern", s.Match.BodyPattern)...)
	err = append(err, validateScenarioBodyFile(s.Response.BodyFile, filePath)...)

	if s.Priority < 0 {
		err = append(err, fmt.Sprintf("priority %d is negative", s.Priority))
	}

	err = append(err, validateScenarioStatuses(s)...)

	return err
}

func validateScenarioName(name, filePath string, seenNames map[string]string) []string {
	if name == "" {
		return []string{"name is empty"}
	}

	if prev, exists := seenNames[name]; exists {
		return []string{fmt.Sprintf("duplicate name %q (first seen in %s)", name, prev)}
	}

	seenNames[name] = filePath

	return nil
}

func validateScenarioRegex(fieldName, pattern string) []string {
	if pattern == "" {
		return nil
	}

	if _, err := regexp.Compile(pattern); err != nil {
		return []string{fmt.Sprintf("invalid %s regex %q: %v", fieldName, pattern, err)}
	}

	return nil
}

func validateScenarioBodyFile(bodyFile, filePath string) []string {
	if bodyFile == "" {
		return nil
	}

	bodyFilePath := bodyFile
	if !filepath.IsAbs(bodyFilePath) {
		bodyFilePath = filepath.Join(filepath.Dir(filePath), bodyFilePath)
	}

	if _, err := os.Stat(bodyFilePath); err != nil {
		return []string{fmt.Sprintf("body_file %q not found: %v", bodyFile, err)}
	}

	return nil
}

func validateScenarioStatuses(s *storage.Scenario) []string {
	var errs []string

	if len(s.Branches) == 0 {
		status := s.Response.ICAPStatus
		if status == 0 {
			errs = append(errs, "icap_status is 0 (will default to 204 at runtime; consider setting it explicitly)")
		} else if status < 100 || status > 599 {
			errs = append(errs, fmt.Sprintf("icap_status %d is out of valid range 100-599", status))
		}
	}

	for i := range s.Branches {
		status := s.Branches[i].Response.ICAPStatus
		if status != 0 && (status < 100 || status > 599) {
			errs = append(errs, fmt.Sprintf("branch #%d icap_status %d is out of valid range 100-599", i+1, status))
		}
	}

	return errs
}
