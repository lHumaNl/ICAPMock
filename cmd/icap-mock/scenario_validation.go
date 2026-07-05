// Copyright 2026 ICAP Mock

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

func validateMultiServerScenarioDirectories(w io.Writer, cfg *config.Config, allPassed *bool) {
	fmt.Fprintln(w, "  scenario_sets:") //nolint:errcheck
	for _, entry := range buildServerEntries(cfg) {
		fmt.Fprintf(w, "    %s:\n", entry.name)                         //nolint:errcheck
		fmt.Fprintf(w, "      scenarios_dir: %s\n", entry.scenariosDir) //nolint:errcheck
		factory := newScenarioRegistryFactory(cfg.Mock.Matching, entry.serverCfg.MaxBodySize, cfg.Sharding)
		validateScenarioDirectoryPath(w, entry.scenariosDir, factory, allPassed, "      ")
	}
}

func validateScenarioDirectoryPath(
	w io.Writer,
	dir string,
	newRegistry func() storage.ScenarioRegistry,
	allPassed *bool,
	indent string,
) {
	paths, err := scenarioYAMLFilePaths(dir)
	if err != nil {
		fmt.Fprintf(w, "%sWARNING: scenarios directory not found: %s\n", indent, dir) //nolint:errcheck
		*allPassed = false
		return
	}
	validScenarios, err := validateScenarioFiles(paths, newRegistry)
	if err != nil {
		fmt.Fprintf(w, "%sERROR: scenarios validation failed: %v\n", indent, err) //nolint:errcheck
		*allPassed = false
		return
	}
	fmt.Fprintf(w, "%sscenarios loaded: %d files found, %d scenarios valid\n", indent, len(paths), validScenarios) //nolint:errcheck
}

func scenarioYAMLFilePaths(dir string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("scenarios directory is required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && isScenarioYAMLFile(entry.Name()) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths, nil
}

func validateScenarioFiles(paths []string, newRegistry func() storage.ScenarioRegistry) (int, error) {
	seenNames := make(map[string]string)
	validScenarios := 0
	for _, path := range paths {
		if err := newRegistry().Load(path); err != nil {
			return 0, fmt.Errorf("%s: %w", path, err)
		}
		count, err := validateScenarioNamesInFile(path, seenNames)
		if err != nil {
			return 0, err
		}
		validScenarios += count
	}
	return validScenarios, nil
}

func validateScenarioNamesInFile(filePath string, seenNames map[string]string) (int, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // path comes from the configured scenario directory
	if err != nil {
		return 0, fmt.Errorf("%s: cannot read file: %w", filePath, err)
	}
	scenarios, err := loadScenariosForValidation(filePath, data)
	if err != nil {
		return 0, fmt.Errorf("%s: cannot parse file: %w", filePath, err)
	}
	for i := range scenarios {
		if errs := validateScenarioName(scenarios[i].Name, filePath, seenNames); len(errs) > 0 {
			return 0, fmt.Errorf("%s: %s", filePath, strings.Join(errs, "; "))
		}
	}
	return len(scenarios), nil
}
