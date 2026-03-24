// Package main provides the entry point for the ICAP Mock Server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// MatchTestCommand handles the match-test subcommand.
type MatchTestCommand struct {
	fs *flag.FlagSet

	scenariosDir string
	method       string
	uri          string
	path         string
	body         string
	headers      stringSlice
	clientIP     string
	httpMethod   string
	verbose      bool
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// NewMatchTestCommand creates a new match-test command.
func NewMatchTestCommand() *MatchTestCommand {
	cmd := &MatchTestCommand{
		fs: flag.NewFlagSet("match-test", flag.ContinueOnError),
	}

	cmd.fs.StringVar(&cmd.scenariosDir, "scenarios", "./configs/scenarios", "Scenarios directory")
	cmd.fs.StringVar(&cmd.scenariosDir, "s", "./configs/scenarios", "Scenarios directory (shorthand)")
	cmd.fs.StringVar(&cmd.method, "method", "REQMOD", "ICAP method (REQMOD, RESPMOD, OPTIONS)")
	cmd.fs.StringVar(&cmd.uri, "uri", "", "Full ICAP URI (e.g., icap://localhost/scan)")
	cmd.fs.StringVar(&cmd.path, "path", "", "Request path (e.g., /scan) — builds URI automatically")
	cmd.fs.StringVar(&cmd.body, "body", "", "HTTP request body content")
	cmd.fs.Var(&cmd.headers, "header", "ICAP header Key:Value (repeatable)")
	cmd.fs.StringVar(&cmd.clientIP, "client-ip", "", "Client IP address")
	cmd.fs.StringVar(&cmd.httpMethod, "http-method", "", "HTTP method (GET, POST, etc.)")
	cmd.fs.BoolVar(&cmd.verbose, "verbose", false, "Show detailed match explanation for each scenario")
	cmd.fs.BoolVar(&cmd.verbose, "v", false, "Show detailed match explanation (shorthand)")

	return cmd
}

func (c *MatchTestCommand) Name() string              { return "match-test" }
func (c *MatchTestCommand) Description() string       { return "Test which scenario matches a given request" }
func (c *MatchTestCommand) Parse(args []string) error { return c.fs.Parse(args) }
func (c *MatchTestCommand) Usage()                    { c.fs.Usage() }

func (c *MatchTestCommand) Run(ctx context.Context) error {
	// Build URI
	uri := c.uri
	if uri == "" && c.path != "" {
		uri = "icap://localhost" + c.path
	}
	if uri == "" {
		return fmt.Errorf("either --uri or --path is required")
	}

	// Load scenarios from directory
	registry := storage.NewScenarioRegistry()
	entries, err := os.ReadDir(c.scenariosDir)
	if err != nil {
		return fmt.Errorf("reading scenarios directory %s: %w", c.scenariosDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml")) {
			continue
		}
		if err := registry.Load(filepath.Join(c.scenariosDir, name)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load %s: %v\n", name, err)
		}
	}

	scenarios := registry.List()
	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios found in %s", c.scenariosDir)
	}

	// Build request
	req, err := icap.NewRequest(c.method, uri)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	for _, h := range c.headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format %q (expected Key:Value)", h)
		}
		req.SetHeader(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	if c.clientIP != "" {
		req.ClientIP = c.clientIP
	}

	if c.httpMethod != "" || c.body != "" {
		httpMethod := c.httpMethod
		if httpMethod == "" {
			httpMethod = "GET"
		}
		req.HTTPRequest = &icap.HTTPMessage{
			Method: httpMethod,
			URI:    "/",
			Proto:  "HTTP/1.1",
		}
		if c.body != "" {
			req.HTTPRequest.Body = []byte(c.body)
		}
	}

	// Print request summary
	fmt.Fprintf(os.Stdout, "Request: %s %s\n", c.method, uri)
	if c.httpMethod != "" {
		fmt.Fprintf(os.Stdout, "  HTTP method: %s\n", c.httpMethod)
	}
	if c.body != "" {
		bodyPreview := c.body
		if len(bodyPreview) > 80 {
			bodyPreview = bodyPreview[:80] + "..."
		}
		fmt.Fprintf(os.Stdout, "  Body: %s\n", bodyPreview)
	}
	if c.clientIP != "" {
		fmt.Fprintf(os.Stdout, "  Client IP: %s\n", c.clientIP)
	}
	for _, h := range c.headers {
		fmt.Fprintf(os.Stdout, "  Header: %s\n", h)
	}
	fmt.Fprintln(os.Stdout)

	// Test each scenario
	fmt.Fprintf(os.Stdout, "Scenarios (%d loaded):\n\n", len(scenarios))

	matched := false
	for _, s := range scenarios {
		result := explainMatch(s, req)
		if result.matched {
			fmt.Fprintf(os.Stdout, "  >>> MATCH: %s (priority: %d)\n", s.Name, s.Priority)
			fmt.Fprintf(os.Stdout, "      Response: ICAP %d", s.Response.ICAPStatus)
			if s.Response.HTTPStatus != 0 {
				fmt.Fprintf(os.Stdout, ", HTTP %d", s.Response.HTTPStatus)
			}
			if s.Response.Delay > 0 {
				fmt.Fprintf(os.Stdout, ", delay %s", s.Response.Delay)
			}
			fmt.Fprintln(os.Stdout)
			if c.verbose {
				for _, check := range result.checks {
					fmt.Fprintf(os.Stdout, "      [PASS] %s\n", check)
				}
			}
			fmt.Fprintln(os.Stdout)
			matched = true
			break // first match wins (sorted by priority)
		} else if c.verbose {
			fmt.Fprintf(os.Stdout, "  --- SKIP: %s (priority: %d)\n", s.Name, s.Priority)
			for _, check := range result.checks {
				if strings.HasPrefix(check, "FAIL") {
					fmt.Fprintf(os.Stdout, "      [FAIL] %s\n", check[5:])
				} else {
					fmt.Fprintf(os.Stdout, "      [PASS] %s\n", check)
				}
			}
			fmt.Fprintln(os.Stdout)
		}
	}

	if !matched {
		fmt.Fprintln(os.Stdout, "  No scenario matched.")
		if !c.verbose {
			fmt.Fprintln(os.Stdout, "  Tip: use --verbose to see why each scenario was skipped.")
		}
		return fmt.Errorf("no matching scenario found")
	}

	return nil
}

type matchResult struct {
	matched bool
	checks  []string
}

func explainMatch(s *storage.Scenario, req *icap.Request) matchResult {
	var checks []string
	path := extractPathFromURI(req.URI)

	// Check ICAP method
	if s.Match.Method != "" {
		if s.Match.Method == req.Method {
			checks = append(checks, fmt.Sprintf("icap_method=%s matches", s.Match.Method))
		} else {
			checks = append(checks, fmt.Sprintf("FAIL icap_method: want %s, got %s", s.Match.Method, req.Method))
			return matchResult{matched: false, checks: checks}
		}
	}

	// Check path pattern
	if s.Match.Path != "" {
		if s.CompiledPath() != nil && s.CompiledPath().MatchString(path) {
			checks = append(checks, fmt.Sprintf("path_pattern=%s matches %q", s.Match.Path, path))
		} else {
			checks = append(checks, fmt.Sprintf("FAIL path_pattern: %s does not match %q", s.Match.Path, path))
			return matchResult{matched: false, checks: checks}
		}
	}

	// Check headers
	for key, value := range s.Match.Headers {
		h, ok := req.Header.Get(key)
		if ok && h == value {
			checks = append(checks, fmt.Sprintf("header %s=%s matches", key, value))
		} else {
			got := "<not set>"
			if ok {
				got = h
			}
			checks = append(checks, fmt.Sprintf("FAIL header %s: want %q, got %q", key, value, got))
			return matchResult{matched: false, checks: checks}
		}
	}

	// Check HTTP method
	if s.Match.HTTPMethod != "" {
		if req.HTTPRequest != nil && req.HTTPRequest.Method == s.Match.HTTPMethod {
			checks = append(checks, fmt.Sprintf("http_method=%s matches", s.Match.HTTPMethod))
		} else {
			got := "<no HTTP request>"
			if req.HTTPRequest != nil {
				got = req.HTTPRequest.Method
			}
			checks = append(checks, fmt.Sprintf("FAIL http_method: want %s, got %s", s.Match.HTTPMethod, got))
			return matchResult{matched: false, checks: checks}
		}
	}

	// Check body pattern
	if s.Match.BodyPattern != "" {
		if req.HTTPRequest != nil && s.CompiledBody() != nil {
			body, _ := req.HTTPRequest.GetBody()
			if s.CompiledBody().MatchString(string(body)) {
				checks = append(checks, fmt.Sprintf("body_pattern=%s matches", s.Match.BodyPattern))
			} else {
				preview := string(body)
				if len(preview) > 40 {
					preview = preview[:40] + "..."
				}
				checks = append(checks, fmt.Sprintf("FAIL body_pattern: %s does not match %q", s.Match.BodyPattern, preview))
				return matchResult{matched: false, checks: checks}
			}
		} else {
			checks = append(checks, fmt.Sprintf("FAIL body_pattern: no HTTP body to match against"))
			return matchResult{matched: false, checks: checks}
		}
	}

	// Check client IP
	if s.Match.ClientIP != "" {
		if s.Match.ClientIP == req.ClientIP {
			checks = append(checks, fmt.Sprintf("client_ip=%s matches", s.Match.ClientIP))
		} else {
			checks = append(checks, fmt.Sprintf("FAIL client_ip: want %s, got %s", s.Match.ClientIP, req.ClientIP))
			return matchResult{matched: false, checks: checks}
		}
	}

	return matchResult{matched: true, checks: checks}
}

func extractPathFromURI(uri string) string {
	uri = strings.TrimPrefix(uri, "icap://")
	uri = strings.TrimPrefix(uri, "icaps://")
	idx := strings.Index(uri, "/")
	if idx == -1 {
		return "/"
	}
	return uri[idx:]
}
