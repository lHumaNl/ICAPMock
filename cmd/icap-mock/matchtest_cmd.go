// Copyright 2026 ICAP Mock

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/icap-mock/icap-mock/internal/management"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// MatchTestCommand handles the match-test subcommand.
type MatchTestCommand struct {
	fs           *flag.FlagSet
	scenariosDir string
	method       string
	uri          string
	path         string
	body         string
	clientIP     string
	httpMethod   string
	headers      stringSlice
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
	uri := c.resolveURI()
	if uri == "" {
		return fmt.Errorf("either --uri or --path is required")
	}

	registry, err := c.loadScenarios()
	if err != nil {
		return err
	}
	scenarios := registry.List()

	req, err := c.buildRequest(uri)
	if err != nil {
		return err
	}
	winner, err := registry.Match(ctx, req)
	if err != nil {
		return fmt.Errorf("matching request with runtime registry: %w", err)
	}

	c.printRequestSummary(uri)

	fmt.Fprintf(os.Stdout, "Scenarios (%d loaded):\n\n", len(scenarios)) //nolint:errcheck

	for _, s := range scenarios {
		result := explainMatch(s, req)
		if s == winner {
			result.matched = true
			printMatchResult(s, result, c.verbose)
			return nil
		} else if c.verbose {
			printSkipResult(s, result)
		}
	}

	if winner != nil {
		return fmt.Errorf("runtime registry returned scenario %q that is absent from its scenario list", winner.Name)
	}
	return fmt.Errorf("no matching scenario found")
}

func (c *MatchTestCommand) resolveURI() string {
	if c.uri != "" {
		return c.uri
	}
	if c.path != "" {
		return "icap://localhost" + c.path
	}
	return ""
}

func (c *MatchTestCommand) loadScenarios() (storage.ScenarioRegistry, error) {
	registry, err := management.LoadScenarioDirectory(c.scenariosDir, storage.NewScenarioRegistry)
	if err != nil {
		return nil, fmt.Errorf("loading scenarios directory %s: %w", c.scenariosDir, err)
	}
	if len(registry.List()) == 0 {
		return nil, fmt.Errorf("no scenarios found in %s", c.scenariosDir)
	}
	return registry, nil
}

func (c *MatchTestCommand) buildRequest(uri string) (*icap.Request, error) {
	req, err := icap.NewRequest(c.method, uri)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for _, h := range c.headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header format %q (expected Key:Value)", h)
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
	return req, nil
}

func (c *MatchTestCommand) printRequestSummary(uri string) {
	fmt.Fprintf(os.Stdout, "Request: %s %s\n", c.method, uri) //nolint:errcheck
	if c.httpMethod != "" {
		fmt.Fprintf(os.Stdout, "  HTTP method: %s\n", c.httpMethod) //nolint:errcheck
	}
	if c.body != "" {
		bodyPreview := c.body
		if len(bodyPreview) > 80 {
			bodyPreview = bodyPreview[:80] + "..."
		}
		fmt.Fprintf(os.Stdout, "  Body: %s\n", bodyPreview) //nolint:errcheck
	}
	if c.clientIP != "" {
		fmt.Fprintf(os.Stdout, "  Client IP: %s\n", c.clientIP) //nolint:errcheck
	}
	for _, h := range c.headers {
		fmt.Fprintf(os.Stdout, "  Header: %s\n", h) //nolint:errcheck
	}
	fmt.Fprintln(os.Stdout) //nolint:errcheck
}

func printMatchResult(s *storage.Scenario, result matchResult, verbose bool) {
	fmt.Fprintf(os.Stdout, "  >>> MATCH: %s (priority: %d)\n", s.Name, s.Priority) //nolint:errcheck
	fmt.Fprintf(os.Stdout, "      Response: ICAP %d", s.Response.ICAPStatus)       //nolint:errcheck
	if s.Response.HTTPStatus != 0 {
		fmt.Fprintf(os.Stdout, ", HTTP %d", s.Response.HTTPStatus) //nolint:errcheck
	}
	if s.Response.Delay > 0 {
		fmt.Fprintf(os.Stdout, ", delay %s", s.Response.Delay) //nolint:errcheck
	}
	fmt.Fprintln(os.Stdout) //nolint:errcheck
	if verbose {
		for _, check := range result.checks {
			if strings.HasPrefix(check, "FAIL ") {
				fmt.Fprintf(os.Stdout, "      [FAIL] %s\n", strings.TrimPrefix(check, "FAIL ")) //nolint:errcheck
				continue
			}
			fmt.Fprintf(os.Stdout, "      [PASS] %s\n", check) //nolint:errcheck
		}
	}
	fmt.Fprintln(os.Stdout) //nolint:errcheck
}

func printSkipResult(s *storage.Scenario, result matchResult) {
	fmt.Fprintf(os.Stdout, "  --- SKIP: %s (priority: %d)\n", s.Name, s.Priority) //nolint:errcheck
	for _, check := range result.checks {
		if strings.HasPrefix(check, "FAIL") {
			fmt.Fprintf(os.Stdout, "      [FAIL] %s\n", check[5:]) //nolint:errcheck
		} else {
			fmt.Fprintf(os.Stdout, "      [PASS] %s\n", check) //nolint:errcheck
		}
	}
	fmt.Fprintln(os.Stdout) //nolint:errcheck
}

type matchResult struct {
	checks  []string
	matched bool
}

func explainMatch(s *storage.Scenario, req *icap.Request) matchResult {
	e := &matchExplainer{scenario: s, req: req, path: extractPathFromURI(req.URI)}
	return e.run()
}

// matchExplainer accumulates match checks and short-circuits on first failure.
type matchExplainer struct {
	scenario *storage.Scenario
	req      *icap.Request
	path     string
	checks   []string
	failed   bool
}

func (e *matchExplainer) pass(msg string) { e.checks = append(e.checks, msg) }
func (e *matchExplainer) fail(msg string) {
	e.checks = append(e.checks, "FAIL "+msg)
	e.failed = true
}

func (e *matchExplainer) run() matchResult {
	e.checkMethod()
	if !e.failed {
		e.checkPath()
	}
	if !e.failed {
		e.checkHeaders()
	}
	if !e.failed {
		e.checkHTTPMethod()
	}
	if !e.failed {
		e.checkBodyPattern()
	}
	if !e.failed {
		e.checkClientIP()
	}
	return matchResult{matched: !e.failed, checks: e.checks}
}

func (e *matchExplainer) checkMethod() {
	if len(e.scenario.Match.Routes) > 0 {
		endpoints, ok := e.scenario.Match.Routes[e.req.Method]
		if !ok {
			e.fail(fmt.Sprintf("exact routes do not declare method %s", e.req.Method))
			return
		}
		e.pass(fmt.Sprintf("exact route method=%s selects endpoints %v", e.req.Method, endpoints))
		return
	}
	methods := e.scenario.Match.Methods
	if len(methods) == 0 {
		return
	}
	for _, m := range methods {
		if m == e.req.Method {
			e.pass(fmt.Sprintf("icap_method=%s matches (scenario accepts %v)", m, methods))
			return
		}
	}
	e.fail(fmt.Sprintf("icap_method: want one of %v, got %s", methods, e.req.Method))
}

func (e *matchExplainer) checkPath() {
	if len(e.scenario.Match.Routes) > 0 {
		e.checkEndpointList(e.scenario.Match.Routes[e.req.Method], "exact route")
		return
	}
	if len(e.scenario.Match.Paths) > 0 {
		e.checkEndpointList(storage.EndpointList(e.scenario.Match.Paths), "endpoint")
		return
	}
	if e.scenario.Match.Path == "" {
		return
	}
	if e.scenario.CompiledPath() != nil && e.scenario.CompiledPath().MatchString(e.path) {
		e.pass(fmt.Sprintf("path_pattern=%s matches %q", e.scenario.Match.Path, e.path))
	} else {
		e.fail(fmt.Sprintf("path_pattern: %s does not match %q", e.scenario.Match.Path, e.path))
	}
}

func (e *matchExplainer) checkEndpointList(endpoints storage.EndpointList, label string) {
	for _, endpoint := range endpoints {
		re, err := storage.CompileEndpointRegex(endpoint)
		if err == nil && re.MatchString(e.path) {
			e.pass(fmt.Sprintf("%s %s matches %q", label, endpoint, e.path))
			return
		}
	}
	e.fail(fmt.Sprintf("%s: want one of %v, got %q", label, endpoints, e.path))
}

func (e *matchExplainer) checkHeaders() {
	for key, value := range e.scenario.Match.Headers {
		h, ok := e.req.Header.Get(key)
		if ok && h == value {
			e.pass(fmt.Sprintf("header %s=%s matches", key, value))
		} else {
			got := "<not set>"
			if ok {
				got = h
			}
			e.fail(fmt.Sprintf("header %s: want %q, got %q", key, value, got))
			return
		}
	}
}

func (e *matchExplainer) checkHTTPMethod() {
	if e.scenario.Match.HTTPMethod == "" {
		return
	}
	if e.req.HTTPRequest != nil && e.req.HTTPRequest.Method == e.scenario.Match.HTTPMethod {
		e.pass(fmt.Sprintf("http_method=%s matches", e.scenario.Match.HTTPMethod))
	} else {
		got := "<no HTTP request>"
		if e.req.HTTPRequest != nil {
			got = e.req.HTTPRequest.Method
		}
		e.fail(fmt.Sprintf("http_method: want %s, got %s", e.scenario.Match.HTTPMethod, got))
	}
}

func (e *matchExplainer) checkBodyPattern() {
	if e.scenario.Match.BodyPattern == "" {
		return
	}
	if e.req.HTTPRequest == nil || e.scenario.CompiledBody() == nil {
		e.fail("body_pattern: no HTTP body to match against")
		return
	}
	body, _ := e.req.HTTPRequest.GetBody()
	if e.scenario.CompiledBody().MatchString(string(body)) {
		e.pass(fmt.Sprintf("body_pattern=%s matches", e.scenario.Match.BodyPattern))
	} else {
		preview := string(body)
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		e.fail(fmt.Sprintf("body_pattern: %s does not match %q", e.scenario.Match.BodyPattern, preview))
	}
}

func (e *matchExplainer) checkClientIP() {
	if e.scenario.Match.ClientIP == "" {
		return
	}
	if e.scenario.Match.ClientIP == e.req.ClientIP {
		e.pass(fmt.Sprintf("client_ip=%s matches", e.scenario.Match.ClientIP))
	} else {
		e.fail(fmt.Sprintf("client_ip: want %s, got %s", e.scenario.Match.ClientIP, e.req.ClientIP))
	}
}

func extractPathFromURI(uri string) string {
	uri = strings.TrimPrefix(uri, "icap://")
	uri = strings.TrimPrefix(uri, "icaps://")
	idx := strings.Index(uri, "/")
	if idx == -1 {
		return "/"
	}
	path := uri[idx:]
	if end := strings.IndexAny(path, "?#"); end >= 0 {
		path = path[:end]
	}
	return path
}
