// Copyright 2026 ICAP Mock

package main

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRealIntegration_REQMODRequestHTTPBodyPercentFINWaitsForFullUpload(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-request-http-percent-fin.yaml"), requestHTTPBodyPercentFINScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := percentStreamBody()
	conn := openPartialREQMODBody(t, rt, "/stream-request-http-percent-fin", "/origin/request-http-percent-fin", body)
	defer conn.Close()
	assertNoResponseAvailable(t, conn)

	writeConnString(t, conn, body[40:]+"\r\n0\r\n\r\n")
	resp, err := readUntilEOF(t, conn)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after percent FIN stream, got %v", err)
	}
	assertPercentFINResponse(t, resp, body)
}

func TestRealIntegration_RESPMODResponseHTTPBodyPercentFINWaitsForFullUpload(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-fin.yaml"), responseHTTPBodyPercentFINScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := percentStreamBody()
	conn := openPartialRESPMODBody(t, rt, "/stream-response-http-percent-fin", "/origin/response-http-percent-fin", body)
	defer conn.Close()
	assertNoResponseAvailable(t, conn)

	writeConnString(t, conn, body[40:]+"\r\n0\r\n\r\n")
	resp, err := readUntilEOF(t, conn)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after percent FIN stream, got %v", err)
	}
	assertPercentFINResponse(t, resp, body)
}

func TestRealIntegration_RESPMODResponseHTTPBodyPercentTermWaitsForFullUpload(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-term.yaml"), responseHTTPBodyPercentTermScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := percentStreamBody()
	conn := openPartialRESPMODBody(t, rt, "/stream-response-http-percent-term", "/origin/response-http-percent-term", body)
	defer conn.Close()
	assertNoResponseAvailable(t, conn)

	writeConnString(t, conn, body[40:]+"\r\n0\r\n\r\n")
	resp := readUntilTokensInOrder(t, conn, "28\r\n"+body[:40]+"\r\n", "0\r\n\r\n")
	assertPercentTermResponse(t, resp, body)
}

func TestRealIntegration_RESPMODResponseHTTPBodyPercentFINClosesWithoutFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-fin.yaml"), responseHTTPBodyPercentFINScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := percentStreamBody()
	resp, err := rt.sendAndReadUntilEOF(t, buildRESPMODRequest(rt.icapURL("/stream-response-http-percent-fin"), "/origin/response-http-percent-fin", body))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after percent FIN stream, got %v", err)
	}

	assertPercentFINResponse(t, resp, body)
}

func TestRealIntegration_RESPMODResponseHTTPBodyPercentTermSendsFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-response-http-percent-term.yaml"), responseHTTPBodyPercentTermScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body := percentStreamBody()
	resp := rt.sendAndReadUntilFinalChunk(t, buildRESPMODRequest(rt.icapURL("/stream-response-http-percent-term"), "/origin/response-http-percent-term", body))

	assertPercentTermResponse(t, resp, body)
}

func requestHTTPBodyPercentFINScenarioYAML() string {
	return `scenarios:
  neutral_request_http_percent_fin:
    method: REQMOD
    endpoint: /stream-request-http-percent-fin
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      send:
        percent: 40
        duration: 1ms
      throttle:
        target_chunk_size: 40
      end:
        mode: fin
`
}

func percentStreamBody() string {
	return strings.Repeat("a", 40) + strings.Repeat("b", 60)
}

func openPartialREQMODBody(t *testing.T, rt *integrationRuntime, path, httpPath, body string) net.Conn {
	t.Helper()
	request := buildREQMODRequestHead(rt.icapURL(path), httpPath, map[string]string{"Content-Type": "application/octet-stream"}, len(body))
	return openPartialChunk(t, rt, request, body)
}

func openPartialRESPMODBody(t *testing.T, rt *integrationRuntime, path, httpPath, body string) net.Conn {
	t.Helper()
	request := buildRESPMODRequestHead(rt.icapURL(path), httpPath, body)
	return openPartialChunk(t, rt, request, body)
}

func openPartialChunk(t *testing.T, rt *integrationRuntime, requestHead, body string) net.Conn {
	t.Helper()
	chunkHead := strconv.FormatInt(int64(len(body)), 16) + "\r\n"
	conn, err := openStreamingConn(rt.icapAddr, requestHead+chunkHead+body[:40])
	if err != nil {
		t.Fatalf("open partial ICAP body stream: %v", err)
	}
	return conn
}

func assertPercentFINResponse(t *testing.T, resp, body string) {
	t.Helper()
	assertNotContains(t, resp, "Connection: close")
	assertContains(t, resp, "28\r\n"+body[:40]+"\r\n")
	assertNotContains(t, resp, "28\r\n"+body[40:80]+"\r\n")
	assertNotContains(t, resp, "14\r\n"+body[80:]+"\r\n")
	assertNotContains(t, resp, body)
	assertNotContains(t, resp, body[:40]+"\r\n0\r\n\r\n")
}

func assertPercentTermResponse(t *testing.T, resp, body string) {
	t.Helper()
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
        target_chunk_size: 40
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
        target_chunk_size: 40
      end:
        mode: term
`
}
