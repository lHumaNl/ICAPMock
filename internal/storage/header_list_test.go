// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"regexp"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestScenarioRegistries_HTTPHeaderListMatchesCaseInsensitiveSubstring(t *testing.T) {
	scenarios, err := loadScenariosFromData([]byte(`
defaults:
  method: [REQMOD, RESPMOD]
  endpoint: /scan
scenarios:
  selected-mime:
    when_http:
      headers:
        Content-Type:
          - application/pdf
          - application/x-dosexec
    status: 204
`), "header-list.yaml")
	if err != nil {
		t.Fatalf("loadScenariosFromData() error = %v", err)
	}

	registries := map[string]ScenarioRegistry{
		"standard": NewScenarioRegistry(),
		"sharded":  NewShardedScenarioRegistry(),
	}
	for name, registry := range registries {
		t.Run(name, func(t *testing.T) {
			for i := range scenarios {
				scenario := scenarios[i]
				if err := registry.Add(&scenario); err != nil {
					t.Fatalf("Add() error = %v", err)
				}
			}

			for _, method := range []string{icap.MethodREQMOD, icap.MethodRESPMOD} {
				matched, err := registry.Match(context.Background(), requestWithHTTPContentType(method, "Application/PDF; charset=binary"))
				if err != nil {
					t.Fatalf("Match(%s) error = %v", method, err)
				}
				if matched.Name != "selected-mime" {
					t.Fatalf("Match(%s) scenario = %q, want selected-mime", method, matched.Name)
				}
			}

			notMatched, err := registry.Match(context.Background(), requestWithHTTPContentType(icap.MethodREQMOD, "application/xml"))
			if err != nil {
				t.Fatalf("Match() nonmatching request error = %v", err)
			}
			if notMatched.Name == "selected-mime" {
				t.Fatal("contains list matched unrelated Content-Type")
			}

			wrongSuffix, err := registry.Match(context.Background(), requestWithHTTPContentType(icap.MethodREQMOD, "application/pdfx; charset=binary"))
			if err != nil {
				t.Fatalf("Match() media type prefix request error = %v", err)
			}
			if wrongSuffix.Name == "selected-mime" {
				t.Fatal("Content-Type list matched a longer media type by prefix")
			}
		})
	}
}

func TestScenarioRegistry_HTTPContentTypeListWithParametersMatchesSemantically(t *testing.T) {
	scenarios, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  parameterized-mime:
    when_http:
      headers:
        Content-Type:
          - application/pdf; charset=binary; profile=Production
    status: 204
`), "parameterized-content-type.yaml")
	if err != nil {
		t.Fatalf("loadScenariosFromData() error = %v", err)
	}
	registry := NewScenarioRegistry()
	if err := registry.Add(&scenarios[0]); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	tests := []struct {
		contentType string
		wantMatch   bool
	}{
		{contentType: "Application/PDF; version=1; PROFILE=Production; CHARSET=BINARY", wantMatch: true},
		{contentType: "application/pdf; charset=binary; profile=production", wantMatch: false},
		{contentType: "application/pdf; charset=text; profile=Production", wantMatch: false},
		{contentType: "application/pdf", wantMatch: false},
	}
	for _, tt := range tests {
		matched, err := registry.Match(context.Background(), requestWithHTTPContentType(icap.MethodREQMOD, tt.contentType))
		if err != nil {
			t.Fatalf("Match(%q) error = %v", tt.contentType, err)
		}
		if got := matched.Name == "parameterized-mime"; got != tt.wantMatch {
			t.Fatalf("Match(%q) selected parameterized scenario = %v, want %v", tt.contentType, got, tt.wantMatch)
		}
	}
}

func TestScenarioRegistry_HTTPContentTypeListRejectsInvalidParameters(t *testing.T) {
	invalidValues := []string{
		"application/pdf; charset",
		"application",
		"not/a media type",
	}
	registries := map[string]func() ScenarioRegistry{
		"standard": NewScenarioRegistry,
		"sharded":  NewShardedScenarioRegistry,
	}
	for _, invalid := range invalidValues {
		for registryName, newRegistry := range registries {
			t.Run(registryName+"/"+invalid, func(t *testing.T) {
				scenarios, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  invalid-mime:
    when_http:
      headers:
        Content-Type:
          - `+invalid+`
    status: 204
`), "invalid-parameterized-content-type.yaml")
				if err != nil {
					t.Fatalf("loadScenariosFromData() error = %v", err)
				}
				if err := newRegistry().Add(&scenarios[0]); err == nil {
					t.Fatal("Add() error = nil, want invalid Content-Type matcher error")
				}
			})
		}
	}
}

func TestLoadScenarios_HTTPHeaderListRejectsEmptyList(t *testing.T) {
	_, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  invalid:
    when_http:
      headers:
        Content-Type: []
    status: 204
`), "empty-header-list.yaml")
	if err == nil {
		t.Fatal("loadScenariosFromData() error = nil, want validation error")
	}
}

func TestLoadScenarios_HTTPHeaderListRejectsNonStringItems(t *testing.T) {
	tests := map[string]string{
		"boolean":  "true",
		"mapping":  "{value: application/pdf}",
		"number":   "42",
		"sequence": "[application/pdf]",
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  invalid:
    when_http:
      headers:
        Content-Type:
          - `+item+`
    status: 204
`), "invalid-header-list.yaml")
			if err == nil {
				t.Fatal("loadScenariosFromData() error = nil, want validation error")
			}
		})
	}
}

func TestScenarioRegistry_BranchHeaderListUsesContainsAny(t *testing.T) {
	scenarios, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  branch-list:
    branches:
      - when:
          X-Mode:
            - fast
            - slow
        status: 204
      - status: 200
`), "branch-header-list.yaml")
	if err != nil {
		t.Fatalf("loadScenariosFromData() error = %v", err)
	}
	req := requestWithHTTPContentType(icap.MethodREQMOD, "application/pdf")
	req.Header = icap.NewHeader()
	req.Header.Set("X-Mode", "PREFIX-SLOW-SUFFIX")
	registry := NewScenarioRegistry()
	if err := registry.Add(&scenarios[0]); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	matched, err := registry.Match(context.Background(), req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if branch := matched.SelectBranch(req); branch != 0 {
		t.Fatalf("SelectBranch() = %d, want 0", branch)
	}
}

func TestScenarioRegistries_BranchParameterizedContentTypeList(t *testing.T) {
	registries := map[string]func() ScenarioRegistry{
		"standard": NewScenarioRegistry,
		"sharded":  NewShardedScenarioRegistry,
	}
	for name, newRegistry := range registries {
		t.Run(name, func(t *testing.T) {
			scenarios, err := loadScenariosFromData([]byte(`
defaults:
  method: REQMOD
  endpoint: /scan
scenarios:
  branch-content-type:
    branches:
      - when_http:
          headers:
            Content-Type:
              - application/pdf; charset=binary
        status: 204
      - status: 200
`), "branch-content-type-list.yaml")
			if err != nil {
				t.Fatalf("loadScenariosFromData() error = %v", err)
			}
			registry := newRegistry()
			if err := registry.Add(&scenarios[0]); err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			req := requestWithHTTPContentType(icap.MethodREQMOD, "Application/PDF; CHARSET=BINARY; version=1")
			matched, err := registry.Match(context.Background(), req)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if branch := matched.SelectBranch(req); branch != 0 {
				t.Fatalf("SelectBranch() = %d, want 0", branch)
			}
		})
	}
}

func requestWithHTTPContentType(method, contentType string) *icap.Request {
	header := icap.NewHeader()
	header.Set("Content-Type", contentType)
	req := &icap.Request{
		Method: method,
		URI:    "icap://localhost/scan",
		HTTPRequest: &icap.HTTPMessage{
			Header: icap.NewHeader(),
		},
	}
	if method == icap.MethodRESPMOD {
		req.HTTPRequest.Header.Set("Content-Type", "application/xml")
		req.HTTPResponse = &icap.HTTPMessage{Header: header}
	} else {
		req.HTTPRequest.Header = header
	}
	return req
}

func BenchmarkContentTypeListMatcher(b *testing.B) {
	matcher, err := compileContentTypeListMatcher([]string{
		"application/octet-stream",
		"application/x-dosexec",
		"application/x-executable",
		"application/x-msdownload",
		"application/pdf",
		"application/zip",
		"application/gzip",
		"application/x-rar",
		"application/vnd.debian.binary-package",
		"application/java-archive",
		"application/x-7z-compressed",
		"application/msexcel",
		"application/msword",
		"text/x-php",
		"text/rtf",
		"text/x-shellscript",
	})
	if err != nil {
		b.Fatalf("compileContentTypeListMatcher() error = %v", err)
	}
	const contentType = "Application/PDF; charset=binary"
	b.ReportAllocs()
	for b.Loop() {
		if !matcher.matches(contentType) {
			b.Fatal("matcher did not match")
		}
	}
}

func BenchmarkContentTypeRegexMatcher(b *testing.B) {
	matcher := regexp.MustCompile(`(?i)^(application/(octet-stream|x-dosexec|x-executable|x-msdownload|pdf|zip|gzip|x-rar|vnd\.debian\.binary-package|java-archive|x-7z-compressed|msexcel|msword)|text/(x-php|rtf|x-shellscript))(?:\s*;|$)`)
	const contentType = "Application/PDF; charset=binary"
	b.ReportAllocs()
	for b.Loop() {
		if !matcher.MatchString(contentType) {
			b.Fatal("matcher did not match")
		}
	}
}
