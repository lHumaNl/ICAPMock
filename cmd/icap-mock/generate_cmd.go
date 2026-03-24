// Package main provides the entry point for the ICAP Mock Server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

// GenerateCommand handles the generate subcommand.
type GenerateCommand struct {
	fs *flag.FlagSet

	fromDir    string
	output     string
	method     string
	minCount   int
	namePrefix string
}

// NewGenerateCommand creates a new generate command.
func NewGenerateCommand() *GenerateCommand {
	cmd := &GenerateCommand{
		fs: flag.NewFlagSet("generate", flag.ContinueOnError),
	}

	cmd.fs.StringVar(&cmd.fromDir, "from", "./data/requests", "Directory containing recorded requests (NDJSON)")
	cmd.fs.StringVar(&cmd.output, "output", "", "Output YAML file (default: stdout)")
	cmd.fs.StringVar(&cmd.method, "method", "", "Filter by ICAP method (REQMOD, RESPMOD)")
	cmd.fs.IntVar(&cmd.minCount, "min-count", 1, "Minimum requests in a group to generate a scenario")
	cmd.fs.StringVar(&cmd.namePrefix, "prefix", "generated", "Prefix for generated scenario names")

	return cmd
}

func (c *GenerateCommand) Name() string              { return "generate" }
func (c *GenerateCommand) Description() string       { return "Generate scenarios from recorded traffic" }
func (c *GenerateCommand) Parse(args []string) error { return c.fs.Parse(args) }
func (c *GenerateCommand) Usage()                    { c.fs.Usage() }

func (c *GenerateCommand) Run(ctx context.Context) error {
	// Create a FileStorage to read requests
	storageCfg := config.StorageConfig{
		Enabled:     true,
		RequestsDir: c.fromDir,
	}
	fs, err := storage.NewFileStorage(storageCfg, nil)
	if err != nil {
		return fmt.Errorf("opening storage at %s: %w", c.fromDir, err)
	}

	// List all recorded requests
	filter := storage.RequestFilter{}
	if c.method != "" {
		filter.Method = c.method
	}

	requests, err := fs.ListRequests(ctx, filter)
	if err != nil {
		return fmt.Errorf("listing requests: %w", err)
	}

	if len(requests) == 0 {
		return fmt.Errorf("no recorded requests found in %s", c.fromDir)
	}

	fmt.Fprintf(os.Stderr, "Loaded %d recorded requests\n", len(requests))

	// Group requests by method + path
	groups := groupRequests(requests)

	// Generate scenarios
	var scenarios []scenarioYAML
	priority := len(groups) * 10

	// Sort groups by count (most common first)
	type groupEntry struct {
		key  string
		reqs []*storage.StoredRequest
	}
	var sorted []groupEntry
	for k, v := range groups {
		sorted = append(sorted, groupEntry{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].reqs) > len(sorted[j].reqs)
	})

	for _, g := range sorted {
		if len(g.reqs) < c.minCount {
			continue
		}

		sample := g.reqs[0]
		name := generateName(c.namePrefix, sample.Method, sample.URI)
		path := extractPathFromStoredURI(sample.URI)

		s := scenarioYAML{
			Name:     name,
			Priority: priority,
			Match: matchYAML{
				Method:      sample.Method,
				PathPattern: escapeRegex(path),
			},
			Response: responseYAML{
				ICAPStatus: mostCommonStatus(g.reqs),
			},
		}

		// If all requests had the same HTTP method, include it
		if httpMethod := commonHTTPMethod(g.reqs); httpMethod != "" {
			s.Match.HTTPMethod = httpMethod
		}

		// Add common headers from the most common response
		if sample.ResponseStatus >= 200 && sample.ResponseStatus < 300 {
			s.Response.HTTPStatus = sample.ResponseStatus
		}

		scenarios = append(scenarios, s)
		priority -= 10
	}

	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios generated (try lowering --min-count)")
	}

	// Marshal to YAML
	sf := scenarioFileYAML{Scenarios: scenarios}
	data, err := yaml.Marshal(sf)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}

	// Write output
	if c.output != "" {
		if err := os.WriteFile(c.output, data, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", c.output, err)
		}
		fmt.Fprintf(os.Stderr, "Generated %d scenarios -> %s\n", len(scenarios), c.output)
	} else {
		fmt.Fprint(os.Stdout, string(data))
		fmt.Fprintf(os.Stderr, "Generated %d scenarios\n", len(scenarios))
	}

	return nil
}

// YAML output structs (separate from internal types to control output format)
type scenarioFileYAML struct {
	Scenarios []scenarioYAML `yaml:"scenarios"`
}

type scenarioYAML struct {
	Name     string       `yaml:"name"`
	Priority int          `yaml:"priority"`
	Match    matchYAML    `yaml:"match"`
	Response responseYAML `yaml:"response"`
}

type matchYAML struct {
	Method      string `yaml:"icap_method,omitempty"`
	PathPattern string `yaml:"path_pattern,omitempty"`
	HTTPMethod  string `yaml:"http_method,omitempty"`
}

type responseYAML struct {
	ICAPStatus int `yaml:"icap_status"`
	HTTPStatus int `yaml:"http_status,omitempty"`
}

func groupRequests(reqs []*storage.StoredRequest) map[string][]*storage.StoredRequest {
	groups := make(map[string][]*storage.StoredRequest)
	for _, r := range reqs {
		path := extractPathFromStoredURI(r.URI)
		key := r.Method + " " + path
		groups[key] = append(groups[key], r)
	}
	return groups
}

func extractPathFromStoredURI(uri string) string {
	uri = strings.TrimPrefix(uri, "icap://")
	uri = strings.TrimPrefix(uri, "icaps://")
	idx := strings.Index(uri, "/")
	if idx == -1 {
		return "/"
	}
	// Remove query string
	path := uri[idx:]
	if qIdx := strings.IndexByte(path, '?'); qIdx >= 0 {
		path = path[:qIdx]
	}
	return path
}

func generateName(prefix, method, uri string) string {
	path := extractPathFromStoredURI(uri)
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", "-")
	if path == "" {
		path = "root"
	}
	return fmt.Sprintf("%s-%s-%s", prefix, strings.ToLower(method), path)
}

func escapeRegex(path string) string {
	// Escape dots and keep the path as a literal match
	path = strings.ReplaceAll(path, ".", "\\.")
	return "^" + path + "$"
}

func mostCommonStatus(reqs []*storage.StoredRequest) int {
	counts := make(map[int]int)
	for _, r := range reqs {
		counts[r.ResponseStatus]++
	}
	maxCount := 0
	maxStatus := 204
	for status, count := range counts {
		if count > maxCount {
			maxCount = count
			maxStatus = status
		}
	}
	return maxStatus
}

func commonHTTPMethod(reqs []*storage.StoredRequest) string {
	if len(reqs) == 0 {
		return ""
	}
	methods := make(map[string]int)
	for _, r := range reqs {
		if r.HTTPRequest != nil {
			methods[r.HTTPRequest.Method]++
		}
	}
	if len(methods) == 1 {
		for m := range methods {
			return m
		}
	}
	return ""
}
