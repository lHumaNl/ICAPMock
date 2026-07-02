// Copyright 2026 ICAP Mock

package main

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealIntegration_RESPMODResponseHTTPBodyPercentFINClosesWithoutFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-fin.yaml"), responseHTTPBodyPercentFINScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := strings.Repeat("a", 40) + strings.Repeat("b", 60)
	resp, err := rt.sendAndReadUntilEOF(t, buildRESPMODRequest(rt.icapURL("/stream-response-http-percent-fin"), "/origin/response-http-percent-fin", body))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after percent FIN stream, got %v", err)
	}

	assertContains(t, resp, "Connection: close")
	assertContains(t, resp, "28\r\n"+body[:40]+"\r\n")
	assertNotContains(t, resp, "28\r\n"+body[40:80]+"\r\n")
	assertNotContains(t, resp, "14\r\n"+body[80:]+"\r\n")
	assertNotContains(t, resp, body)
	assertNotContains(t, resp, body[:40]+"\r\n0\r\n\r\n")
}

func TestRealIntegration_RESPMODResponseHTTPBodyPercentTermSendsFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-term.yaml"), responseHTTPBodyPercentTermScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := strings.Repeat("a", 40) + strings.Repeat("b", 60)
	resp := rt.sendAndReadUntilFinalChunk(t, buildRESPMODRequest(rt.icapURL("/stream-response-http-percent-term"), "/origin/response-http-percent-term", body))

	assertNotContains(t, resp, "Connection: close")
	assertContains(t, resp, "28\r\n"+body[:40]+"\r\n0\r\n\r\n")
	assertNotContains(t, resp, "28\r\n"+body[40:80]+"\r\n")
	assertNotContains(t, resp, "14\r\n"+body[80:]+"\r\n")
	assertNotContains(t, resp, body)
}

func responseHTTPBodyPercentFINScenarioYAML() string {
	return `scenarios:
  neutral_response_http_percent_fin:
    method: RESPMOD
    endpoint: /stream-response-http-percent-fin
    status: 200
    stream:
      source:
        from: response_http_body
      send:
        percent: 40
        duration: 1ms
      throttle:
        chunk_size: 40
      end:
        mode: fin
`
}

func responseHTTPBodyPercentTermScenarioYAML() string {
	return `scenarios:
  neutral_response_http_percent_term:
    method: RESPMOD
    endpoint: /stream-response-http-percent-term
    status: 200
    stream:
      source:
        from: response_http_body
      send:
        percent: 40
        duration: 1ms
      throttle:
        chunk_size: 40
      end:
        mode: term
`
}
