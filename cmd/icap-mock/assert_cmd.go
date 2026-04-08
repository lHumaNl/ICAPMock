// Copyright 2026 ICAP Mock

// Package main implements the ICAP mock server CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// AssertCommand handles the assert subcommand for CI/CD integration.
type AssertCommand struct {
	fs           *flag.FlagSet
	metricsURL   string
	scenarioHit  string
	minRequests  int64
	maxErrorRate float64
	maxP95Ms     float64
	timeout      time.Duration
}

// NewAssertCommand creates a new assert command.
func NewAssertCommand() *AssertCommand {
	cmd := &AssertCommand{
		fs: flag.NewFlagSet("assert", flag.ContinueOnError),
	}

	cmd.fs.StringVar(&cmd.metricsURL, "metrics-url", "http://localhost:8080/metrics", "Prometheus metrics endpoint URL")
	cmd.fs.Int64Var(&cmd.minRequests, "min-requests", 0, "Minimum total requests expected (0=skip)")
	cmd.fs.Float64Var(&cmd.maxErrorRate, "max-error-rate", -1, "Maximum error rate 0.0-1.0 (-1=skip)")
	cmd.fs.StringVar(&cmd.scenarioHit, "scenario-hit", "", "Scenario name that must have been matched at least once")
	cmd.fs.Float64Var(&cmd.maxP95Ms, "max-p95-ms", 0, "Maximum P95 latency in milliseconds (0=skip)")
	cmd.fs.DurationVar(&cmd.timeout, "timeout", 10*time.Second, "HTTP request timeout")

	return cmd
}

func (c *AssertCommand) Name() string { return "assert" }
func (c *AssertCommand) Description() string {
	return "Assert mock server state for CI/CD (exit 0=pass, 1=fail)"
}
func (c *AssertCommand) Parse(args []string) error { return c.fs.Parse(args) }
func (c *AssertCommand) Usage()                    { c.fs.Usage() }

func (c *AssertCommand) Run(_ context.Context) error {
	// Fetch metrics
	client := &http.Client{Timeout: c.timeout}
	resp, err := client.Get(c.metricsURL)
	if err != nil {
		return fmt.Errorf("fetching metrics from %s: %w", c.metricsURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading metrics response: %w", err)
	}

	metrics := parsePrometheusText(string(body))

	// Run assertions
	var failures []string

	// Check minimum requests
	if c.minRequests > 0 {
		total := sumMetricValues(metrics, "icap_requests_total")
		if total < float64(c.minRequests) {
			failures = append(failures, fmt.Sprintf("min-requests: got %.0f, want >= %d", total, c.minRequests))
		} else {
			fmt.Fprintf(os.Stdout, "PASS: requests total = %.0f (>= %d)\n", total, c.minRequests) //nolint:errcheck
		}
	}

	// Check error rate
	if c.maxErrorRate >= 0 {
		total := sumMetricValues(metrics, "icap_requests_total")
		errors := sumMetricValuesFiltered(metrics, "icap_requests_total", "status", "5")
		if total > 0 {
			rate := errors / total
			if rate > c.maxErrorRate {
				failures = append(failures, fmt.Sprintf("max-error-rate: got %.4f, want <= %.4f", rate, c.maxErrorRate))
			} else {
				fmt.Fprintf(os.Stdout, "PASS: error rate = %.4f (<= %.4f)\n", rate, c.maxErrorRate) //nolint:errcheck
			}
		} else {
			fmt.Fprintf(os.Stdout, "PASS: error rate = 0 (no requests)\n") //nolint:errcheck
		}
	}

	// Check scenario was hit
	if c.scenarioHit != "" {
		hits := sumMetricValuesFiltered(metrics, "icap_scenario_matches_total", "scenario", c.scenarioHit)
		if hits == 0 {
			failures = append(failures, fmt.Sprintf("scenario-hit: scenario %q was never matched", c.scenarioHit))
		} else {
			fmt.Fprintf(os.Stdout, "PASS: scenario %q matched %.0f times\n", c.scenarioHit, hits) //nolint:errcheck
		}
	}

	// Check P95 latency
	if c.maxP95Ms > 0 {
		p95 := getQuantileValue(metrics, "icap_request_duration_seconds", "0.95")
		if p95 >= 0 {
			p95Ms := p95 * 1000
			if p95Ms > c.maxP95Ms {
				failures = append(failures, fmt.Sprintf("max-p95-ms: got %.1fms, want <= %.1fms", p95Ms, c.maxP95Ms))
			} else {
				fmt.Fprintf(os.Stdout, "PASS: P95 latency = %.1fms (<= %.1fms)\n", p95Ms, c.maxP95Ms) //nolint:errcheck
			}
		} else {
			fmt.Fprintf(os.Stdout, "SKIP: P95 latency metric not found\n") //nolint:errcheck
		}
	}

	if len(failures) > 0 {
		fmt.Fprintln(os.Stdout) //nolint:errcheck
		for _, f := range failures {
			fmt.Fprintf(os.Stdout, "FAIL: %s\n", f) //nolint:errcheck
		}
		return fmt.Errorf("%d assertion(s) failed", len(failures))
	}

	fmt.Fprintln(os.Stdout, "\nAll assertions passed.") //nolint:errcheck
	return nil
}

// prometheusMetric represents a single metric line from Prometheus text format.
type prometheusMetric struct {
	labels map[string]string
	name   string
	value  float64
}

// parsePrometheusText parses Prometheus text exposition format.
func parsePrometheusText(text string) []prometheusMetric {
	var metrics []prometheusMetric
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		m := prometheusMetric{labels: make(map[string]string)}

		// Parse name and labels
		labelStart := strings.IndexByte(line, '{')
		if labelStart >= 0 {
			m.name = line[:labelStart]
			labelEnd := strings.IndexByte(line[labelStart:], '}')
			if labelEnd < 0 {
				continue
			}
			labelEnd += labelStart
			labelStr := line[labelStart+1 : labelEnd]
			for _, pair := range splitLabels(labelStr) {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) == 2 {
					m.labels[kv[0]] = strings.Trim(kv[1], "\"")
				}
			}
			line = line[labelEnd+1:]
		} else {
			spaceIdx := strings.IndexByte(line, ' ')
			if spaceIdx < 0 {
				continue
			}
			m.name = line[:spaceIdx]
			line = line[spaceIdx:]
		}

		// Parse value
		valStr := strings.TrimSpace(line)
		// Handle timestamp suffix
		if spaceIdx := strings.IndexByte(valStr, ' '); spaceIdx >= 0 {
			valStr = valStr[:spaceIdx]
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		m.value = val
		metrics = append(metrics, m)
	}
	return metrics
}

// splitLabels splits label pairs, respecting quoted values.
func splitLabels(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
			current.WriteByte(ch)
		case ch == ',' && !inQuote:
			result = append(result, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}
	return result
}

// sumMetricValues sums all values for a metric name.
func sumMetricValues(metrics []prometheusMetric, name string) float64 {
	var sum float64
	for _, m := range metrics {
		if m.name == name {
			sum += m.value
		}
	}
	return sum
}

// sumMetricValuesFiltered sums values where the given label starts with the prefix.
func sumMetricValuesFiltered(metrics []prometheusMetric, name, labelKey, labelPrefix string) float64 {
	var sum float64
	for _, m := range metrics {
		if m.name == name {
			if v, ok := m.labels[labelKey]; ok && strings.HasPrefix(v, labelPrefix) {
				sum += m.value
			}
		}
	}
	return sum
}

// getQuantileValue returns the value for a specific quantile from a summary/histogram metric.
func getQuantileValue(metrics []prometheusMetric, name, quantile string) float64 {
	for _, m := range metrics {
		if m.name == name {
			if q, ok := m.labels["quantile"]; ok && q == quantile {
				return m.value
			}
		}
	}
	return -1
}
