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
		fmt.Printf("\nFile: %s\n", filePath)

		data, err := os.ReadFile(filePath) //nolint:gosec // path is validated
		if err != nil {
			fmt.Printf("  [FAIL] cannot read file: %v\n", err)
			allPassed = false
			continue
		}

		var sf storage.ScenarioFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			fmt.Printf("  [FAIL] YAML parse error: %v\n", err)
			allPassed = false
			continue
		}

		if len(sf.Scenarios) == 0 {
			fmt.Printf("  (no scenarios defined)\n")
			continue
		}

		for i := range sf.Scenarios {
			s := &sf.Scenarios[i]
			scenarioLabel := s.Name
			if scenarioLabel == "" {
				scenarioLabel = fmt.Sprintf("<unnamed #%d>", i+1)
			}

			var errs []string

			// Check name is non-empty.
			if s.Name == "" {
				errs = append(errs, "name is empty")
			} else {
				// Check name uniqueness across all files.
				if prev, exists := seenNames[s.Name]; exists {
					errs = append(errs, fmt.Sprintf("duplicate name %q (first seen in %s)", s.Name, prev))
				} else {
					seenNames[s.Name] = filePath
				}
			}

			// Compile path_pattern regex.
			if s.Match.Path != "" {
				if _, err := regexp.Compile(s.Match.Path); err != nil {
					errs = append(errs, fmt.Sprintf("invalid path_pattern regex %q: %v", s.Match.Path, err))
				}
			}

			// Compile body_pattern regex.
			if s.Match.BodyPattern != "" {
				if _, err := regexp.Compile(s.Match.BodyPattern); err != nil {
					errs = append(errs, fmt.Sprintf("invalid body_pattern regex %q: %v", s.Match.BodyPattern, err))
				}
			}

			// Check BodyFile exists (resolved relative to the scenario file's directory).
			if s.Response.BodyFile != "" {
				bodyFilePath := s.Response.BodyFile
				if !filepath.IsAbs(bodyFilePath) {
					bodyFilePath = filepath.Join(filepath.Dir(filePath), bodyFilePath)
				}
				if _, err := os.Stat(bodyFilePath); err != nil {
					errs = append(errs, fmt.Sprintf("body_file %q not found: %v", s.Response.BodyFile, err))
				}
			}

			// Check priority is non-negative.
			if s.Priority < 0 {
				errs = append(errs, fmt.Sprintf("priority %d is negative", s.Priority))
			}

			// Check ICAPStatus is valid (100-599).
			// A zero value is treated as 204 by the runtime, but we flag it as
			// unset so operators are aware.
			status := s.Response.ICAPStatus
			if status == 0 {
				// Warn but don't fail: runtime defaults to 204.
				errs = append(errs, "icap_status is 0 (will default to 204 at runtime; consider setting it explicitly)")
			} else if status < 100 || status > 599 {
				errs = append(errs, fmt.Sprintf("icap_status %d is out of valid range 100-599", status))
			}

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
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All scenarios passed validation.")
		return nil
	}
	return fmt.Errorf("one or more scenarios failed validation")
}
