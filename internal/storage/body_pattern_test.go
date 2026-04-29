// Copyright 2026 ICAP Mock

package storage

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestBodyPatternMatchesUsesBoundedRead(t *testing.T) {
	const limit int64 = 32
	reader := &countingBodyPatternReader{remaining: limit + 64}
	msg := &icap.HTTPMessage{BodyReader: reader}
	pattern := regexp.MustCompile("x")
	options := BodyPatternOptions{Limit: limit, LimitAction: BodyPatternLimitActionNoMatch}

	matched, err := bodyPatternMatches(pattern, msg, options)
	if err != nil {
		t.Fatalf("bodyPatternMatches() error = %v", err)
	}
	if matched {
		t.Fatal("bodyPatternMatches() = true, want false for oversized body")
	}
	if reader.read > limit+1 {
		t.Fatalf("read %d bytes, want at most %d", reader.read, limit+1)
	}
}

func TestBodyPatternMatchesUnlimitedUsesUnboundedRead(t *testing.T) {
	reader := &countingBodyPatternReader{remaining: 96}
	msg := &icap.HTTPMessage{BodyReader: reader}
	pattern := regexp.MustCompile("x")
	options := BodyPatternOptions{Limit: -1, LimitAction: BodyPatternLimitActionNoMatch}

	matched, err := bodyPatternMatches(pattern, msg, options)
	if err != nil {
		t.Fatalf("bodyPatternMatches() error = %v", err)
	}
	if !matched {
		t.Fatal("bodyPatternMatches() = false, want true")
	}
	if reader.read != 96 {
		t.Fatalf("read %d bytes, want 96", reader.read)
	}
}

func TestScenarioRegistryBodyPatternLimitNoMatch(t *testing.T) {
	registry := NewScenarioRegistryWithBodyPatternOptions(bodyPatternTestOptions(BodyPatternLimitActionNoMatch))
	addBodyPatternScenario(t, registry)
	req := bodyPatternTestRequest(strings.NewReader("malware-payload"))

	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != defaultScenarioName {
		t.Fatalf("Match() scenario = %s, want %s", scenario.Name, defaultScenarioName)
	}
}

func TestScenarioRegistryBodyPatternLimitError(t *testing.T) {
	registry := NewScenarioRegistryWithBodyPatternOptions(bodyPatternTestOptions(BodyPatternLimitActionError))
	addBodyPatternScenario(t, registry)
	req := bodyPatternTestRequest(strings.NewReader("malware-payload"))

	_, err := registry.Match(req)
	if !errors.Is(err, ErrBodyPatternLimitExceeded) {
		t.Fatalf("Match() error = %v, want ErrBodyPatternLimitExceeded", err)
	}
}

func TestRegistriesUseConfiguredBodyPatternLimit(t *testing.T) {
	registries := map[string]ScenarioRegistry{
		"standard": NewScenarioRegistryWithBodyPatternOptions(bodyPatternTestOptions(BodyPatternLimitActionNoMatch)),
		"sharded":  NewShardedScenarioRegistryWithBodyPatternOptions(bodyPatternTestOptions(BodyPatternLimitActionNoMatch)),
	}
	for name, registry := range registries {
		t.Run(name, func(t *testing.T) {
			reader := &countingBodyPatternReader{remaining: 64}
			addBodyPatternScenario(t, registry)
			_, err := registry.Match(bodyPatternTestRequest(reader))
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if reader.read > 9 {
				t.Fatalf("read %d bytes, want at most 9", reader.read)
			}
		})
	}
}

func TestRegistriesUseResponseBodyPatternForRESPMOD(t *testing.T) {
	registries := map[string]ScenarioRegistry{
		"standard": NewScenarioRegistry(),
		"sharded":  NewShardedScenarioRegistry(),
	}
	for name, registry := range registries {
		t.Run(name, func(t *testing.T) {
			addResponseBodyPatternScenario(t, registry)
			assertMatchedScenario(t, registry, respmodBodyPatternRequest("clean", "response-malware"), "response-body")
			assertMatchedScenario(t, registry, respmodBodyPatternRequest("response-malware", "clean"), defaultScenarioName)
		})
	}
}

func TestShardedRegistryDisablesCacheForBodyPattern(t *testing.T) {
	registry := NewShardedScenarioRegistry()
	addBodyPatternScenario(t, registry)

	assertMatchedScenario(t, registry, reqmodBodyPatternRequest("clean"), defaultScenarioName)
	assertMatchedScenario(t, registry, reqmodBodyPatternRequest("malware"), "body-pattern")

	metrics := registry.(*ShardedScenarioRegistry).GetMetrics()
	if metrics.cacheHits != 0 {
		t.Fatalf("cacheHits = %d, want 0 for body_pattern registry", metrics.cacheHits)
	}
}

func TestBodyPatternLimitActionNormalizesCase(t *testing.T) {
	reader := &countingBodyPatternReader{remaining: 64}
	msg := &icap.HTTPMessage{BodyReader: reader}
	pattern := regexp.MustCompile("malware")
	options := BodyPatternOptions{Limit: 8, LimitAction: "No_Match"}

	matched, err := bodyPatternMatches(pattern, msg, options)
	if err != nil {
		t.Fatalf("bodyPatternMatches() error = %v", err)
	}
	if matched {
		t.Fatal("bodyPatternMatches() = true, want false for oversized body")
	}
}

func TestBodyPatternNoMatchReturnsReadErrors(t *testing.T) {
	readErr := errors.New("read failed")
	msg := &icap.HTTPMessage{BodyReader: errorBodyPatternReader{err: readErr}}
	pattern := regexp.MustCompile("malware")
	options := BodyPatternOptions{Limit: 8, LimitAction: BodyPatternLimitActionNoMatch}

	matched, err := bodyPatternMatches(pattern, msg, options)
	if err == nil || !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want read failure", err)
	}
	if matched {
		t.Fatal("bodyPatternMatches() = true, want false")
	}
}

func bodyPatternTestOptions(action BodyPatternLimitAction) BodyPatternOptions {
	return BodyPatternOptions{Limit: 8, LimitAction: action}
}

func addBodyPatternScenario(t *testing.T, registry ScenarioRegistry) {
	t.Helper()
	err := registry.Add(&Scenario{
		Name:     "body-pattern",
		Match:    MatchRule{BodyPattern: "malware"},
		Response: ResponseTemplate{ICAPStatus: 200},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func bodyPatternTestRequest(reader io.Reader) *icap.Request {
	return &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/scan",
		HTTPRequest: &icap.HTTPMessage{
			Method:     "POST",
			URI:        "http://example.test/scan",
			BodyReader: reader,
		},
	}
}

func addResponseBodyPatternScenario(t *testing.T, registry ScenarioRegistry) {
	t.Helper()
	err := registry.Add(&Scenario{
		Name: "response-body",
		Match: MatchRule{
			Methods:     MethodList{icap.MethodRESPMOD},
			BodyPattern: "response-malware",
		},
		Response: ResponseTemplate{ICAPStatus: 200},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func assertMatchedScenario(t *testing.T, registry ScenarioRegistry, req *icap.Request, want string) {
	t.Helper()
	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != want {
		t.Fatalf("Match() scenario = %s, want %s", scenario.Name, want)
	}
}

func reqmodBodyPatternRequest(body string) *icap.Request {
	return &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/scan",
		HTTPRequest: &icap.HTTPMessage{
			Method: "POST",
			URI:    "http://example.test/scan",
			Body:   []byte(body),
		},
	}
}

func respmodBodyPatternRequest(reqBody, resBody string) *icap.Request {
	return &icap.Request{
		Method: icap.MethodRESPMOD,
		URI:    "icap://localhost/scan",
		HTTPRequest: &icap.HTTPMessage{
			Method: "GET",
			URI:    "http://example.test/scan",
			Body:   []byte(reqBody),
		},
		HTTPResponse: &icap.HTTPMessage{Body: []byte(resBody)},
	}
}

type countingBodyPatternReader struct {
	remaining int64
	read      int64
}

func (r *countingBodyPatternReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := int64(0); i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

type errorBodyPatternReader struct {
	err error
}

func (r errorBodyPatternReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
