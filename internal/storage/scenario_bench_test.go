// Copyright 2026 ICAP Mock

package storage

import (
	"fmt"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// buildRequest creates a minimal ICAP REQMOD request for benchmarking.
func buildRequest(path string) *icap.Request {
	req, _ := icap.NewRequest(icap.MethodREQMOD, "icap://icap-server.example.net:1344"+path)
	req.Header = make(icap.Header)
	req.Header.Set("Host", "icap-server.example.net")
	req.Header.Set("X-Client-IP", "192.168.1.100")
	req.ClientIP = "192.168.1.100"
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "POST",
		URI:    path,
		Proto:  "HTTP/1.1",
	}
	req.HTTPRequest.Header = make(icap.Header)
	req.HTTPRequest.Header.Set("Content-Type", "application/json")
	req.HTTPRequest.SetLoadedBody([]byte(`{"action":"scan","data":"safe-payload"}`))
	return req
}

// buildSimpleScenario creates a scenario that matches only on a path pattern.
func buildSimpleScenario(i int) *Scenario {
	return &Scenario{
		Name:     fmt.Sprintf("scenario-%d", i),
		Priority: i,
		Match: MatchRule{
			Path: fmt.Sprintf("^/api/v%d/scan", i),
		},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
	}
}

// buildComplexScenario creates a scenario that matches on path, HTTP method, and headers.
func buildComplexScenario(i int) *Scenario {
	return &Scenario{
		Name:     fmt.Sprintf("complex-scenario-%d", i),
		Priority: i,
		Match: MatchRule{
			Path:       fmt.Sprintf("^/api/v%d/", i),
			HTTPMethod: "POST",
			Headers: map[string]string{
				"X-Client-IP": fmt.Sprintf("10.0.%d.1", i%256),
			},
			BodyPattern: fmt.Sprintf("payload-%d", i),
		},
		Response: ResponseTemplate{
			ICAPStatus: 200,
			HTTPStatus: 200,
		},
	}
}

// BenchmarkScenarioMatch_Simple benchmarks Match() against a registry with 10
// scenarios that each have only a path-pattern rule.  The request is designed to
// fall through all 10 and match the default scenario.
func BenchmarkScenarioMatch_Simple(b *testing.B) {
	reg := NewScenarioRegistry()

	// Add 10 path-only scenarios that won't match our test request.
	for i := 0; i < 10; i++ {
		s := buildSimpleScenario(i)
		if err := reg.Add(s); err != nil {
			b.Fatalf("Add() error: %v", err)
		}
	}

	req := buildRequest("/scan/document")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := reg.Match(req)
		if err != nil {
			b.Fatalf("Match() unexpected error: %v", err)
		}
	}
}

// BenchmarkScenarioMatch_Complex benchmarks Match() against a registry with 100
// scenarios each using path + HTTP-method + header + body-pattern rules.
// The request falls through all 100 and matches the default scenario.
func BenchmarkScenarioMatch_Complex(b *testing.B) {
	reg := NewScenarioRegistry()

	for i := 0; i < 100; i++ {
		s := buildComplexScenario(i)
		if err := reg.Add(s); err != nil {
			b.Fatalf("Add(%s) error: %v", s.Name, err)
		}
	}

	req := buildRequest("/unmatched/path")
	req.HTTPRequest.SetLoadedBody([]byte(`{"action":"scan","data":"safe-payload"}`))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := reg.Match(req)
		if err != nil {
			b.Fatalf("Match() unexpected error: %v", err)
		}
	}
}

// BenchmarkShardedScenarioMatch benchmarks Match() against a ShardedScenarioRegistry
// with 100 complex scenarios.  The sharded registry distributes scenarios across
// multiple shards for concurrent access.
func BenchmarkShardedScenarioMatch(b *testing.B) {
	reg := NewShardedScenarioRegistry()

	for i := 0; i < 100; i++ {
		s := buildComplexScenario(i)
		if err := reg.Add(s); err != nil {
			b.Fatalf("Add(%s) error: %v", s.Name, err)
		}
	}

	req := buildRequest("/unmatched/path")
	req.HTTPRequest.SetLoadedBody([]byte(`{"action":"scan","data":"safe-payload"}`))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := reg.Match(req)
		if err != nil {
			b.Fatalf("Match() unexpected error: %v", err)
		}
	}
}
