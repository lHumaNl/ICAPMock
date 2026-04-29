// Copyright 2026 ICAP Mock

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/health"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/management"
	"github.com/icap-mock/icap-mock/internal/middleware"
	"github.com/icap-mock/icap-mock/internal/ratelimit"
	"github.com/icap-mock/icap-mock/internal/server"
)

const (
	integrationOperationTimeout = 4 * time.Second
	readinessPollInterval       = 25 * time.Millisecond
	streamingChunkDelay         = "300ms"
)

func TestRealIntegration_ScenarioReloadUpdatesLiveICAPResponses(t *testing.T) {
	scenariosDir := t.TempDir()
	scenarioPath := filepath.Join(scenariosDir, "neutral-reload.yaml")
	writeTestFile(t, scenarioPath, reloadScenarioYAML("first-body"))

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	first := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequest(rt.icapURL("/reload-check"), "/origin/reload", "ping"))
	assertContains(t, first, "HTTP/1.1 403 Forbidden")
	assertContains(t, first, "first-body")
	assertNotContains(t, first, "second-body")

	writeTestFile(t, scenarioPath, reloadScenarioYAML("second-body"))
	reloadReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rt.managementURL("/api/v1/scenarios/reload"), http.NoBody)
	if err != nil {
		t.Fatalf("create reload request: %v", err)
	}
	reloadResp, err := rt.httpClient.Do(reloadReq)
	if err != nil {
		t.Fatalf("POST /api/v1/scenarios/reload failed: %v", err)
	}
	defer reloadResp.Body.Close()
	if reloadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(reloadResp.Body)
		t.Fatalf("reload status = %d, want 200; body=%s", reloadResp.StatusCode, string(body))
	}

	second := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequest(rt.icapURL("/reload-check"), "/origin/reload", "ping"))
	assertContains(t, second, "second-body")
	assertNotContains(t, second, "first-body")
}

func TestRealIntegration_StreamingCompleteModesReturnFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-streaming-complete.yaml"), completeStreamingScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	t.Run("REQMOD request_body complete", func(t *testing.T) {
		resp := rt.sendAndReadStagedStream(t, buildREQMODRequest(rt.icapURL("/stream-request-complete"), "/origin/request", "abcd"), "2\r\nab\r\n")
		assertContains(t, resp.firstStage, "HTTP/1.1 403 Forbidden")
		assertContains(t, resp.firstStage, "2\r\nab\r\n")
		assertNotContains(t, resp.firstStage, "2\r\ncd\r\n")
		assertNotContains(t, resp.firstStage, "0\r\n\r\n")
		assertContains(t, resp.remainder, "2\r\ncd\r\n0\r\n\r\n")
	})

	t.Run("RESPMOD response_body complete", func(t *testing.T) {
		resp := rt.sendAndReadStagedStream(t, buildRESPMODRequest(rt.icapURL("/stream-response-complete"), "/origin/response", "wxyz"), "3\r\nwxy\r\n")
		assertContains(t, resp.firstStage, "HTTP/1.1 200 OK")
		assertContains(t, resp.firstStage, "3\r\nwxy\r\n")
		assertNotContains(t, resp.firstStage, "1\r\nz\r\n")
		assertNotContains(t, resp.firstStage, "0\r\n\r\n")
		assertContains(t, resp.remainder, "1\r\nz\r\n0\r\n\r\n")
	})
}

func TestRealIntegration_StreamingFINModeClosesWithoutFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-streaming-fin.yaml"), finStreamingScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	resp, err := rt.sendAndReadUntilEOF(t, buildRESPMODRequest(rt.icapURL("/stream-response-fin"), "/origin/response", "wxyz"))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after FIN stream, got %v", err)
	}
	assertContains(t, resp, "Connection: close")
	assertContains(t, resp, "3\r\nwxy\r\n")
	assertNotContains(t, resp, "1\r\nz\r\n0\r\n\r\n")
	assertNotContains(t, resp, "0\r\n\r\n")
}

func TestRealIntegration_REQMODRequestHTTPBodyStreamingCompleteWithStagedReads(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-request-http-body-complete.yaml"), requestHTTPBodyCompleteScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	conn, err := openStreamingConn(rt.icapAddr, buildREQMODRequestHead(rt.icapURL("/stream-request-http-complete"), "/origin/request-http-complete", map[string]string{"Content-Type": "application/octet-stream"}, 4)+"2\r\nab\r\n")
	if err != nil {
		t.Fatalf("open staged request_http_body stream: %v", err)
	}
	defer conn.Close()

	firstStage := readUntilToken(t, conn, "2\r\nab\r\n")
	assertContains(t, firstStage, "HTTP/1.1 403 Forbidden")
	assertContains(t, firstStage, "2\r\nab\r\n")
	assertNotContains(t, firstStage, "2\r\ncd\r\n")
	assertNotContains(t, firstStage, "0\r\n\r\n")

	writeConnString(t, conn, "2\r\ncd\r\n0\r\n\r\n")
	remainder := readUntilToken(t, conn, "0\r\n\r\n")
	assertContains(t, remainder, "2\r\ncd\r\n0\r\n\r\n")
}

func TestRealIntegration_REQMODRequestHTTPBodyFINClosesWithoutFinalChunk(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-request-http-body-fin.yaml"), requestHTTPBodyFINScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	resp, err := rt.sendAndReadUntilEOF(t, buildREQMODRequestWithHeaders(rt.icapURL("/stream-request-http-fin"), "/origin/request-http-fin", "abcd", map[string]string{"Content-Type": "application/octet-stream"}))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected clean EOF after request_http_body FIN stream, got %v", err)
	}
	assertContains(t, resp, "Connection: close")
	assertContains(t, resp, "2\r\nab\r\n1\r\nc\r\n")
	assertNotContains(t, resp, "1\r\nd\r\n")
	assertNotContains(t, resp, "0\r\n\r\n")
}

func TestRealIntegration_RESPMODSegmentedRequestBodyStreamsResponseHTTPBody(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-segmented-response-http-body.yaml"), segmentedResponseHTTPBodyScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	resp := rt.sendAndReadStagedStream(t, buildSegmentedRESPMODRequest(rt.icapURL("/stream-segmented-response"), "/origin/upload", "abcde", "wxyz"), "3\r\nwxy\r\n")
	assertContains(t, resp.firstStage, "ICAP/1.0 200 OK")
	assertContains(t, resp.firstStage, "3\r\nwxy\r\n")
	assertNotContains(t, resp.firstStage, "1\r\nz\r\n")
	assertNotContains(t, resp.firstStage, "abcde")
	assertContains(t, resp.remainder, "1\r\nz\r\n0\r\n\r\n")
	assertNotContains(t, resp.remainder, "abcde")
}

func TestRealIntegration_MultipartSelectorStreamsFieldsAndFilenameMatches(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-multipart-select.yaml"), multipartSelectorScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	body, contentType := buildNeutralMultipartBody(t)
	resp := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequestWithHeaders(rt.icapURL("/multipart-select"), "/origin/multipart-select", body, map[string]string{"Content-Type": contentType}))
	assertContains(t, resp, "5\r\nhello\r\n4\r\nSAFE\r\n0\r\n\r\n")
	assertNotContains(t, resp, "skip-field")
	assertNotContains(t, resp, "IGNORE")
}

func TestRealIntegration_RawFileFallbackStreamsNonMultipartContentDispositionMatch(t *testing.T) {
	scenariosDir := t.TempDir()
	writeTestFile(t, filepath.Join(scenariosDir, "neutral-raw-file-fallback.yaml"), rawFileFallbackScenarioYAML())

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	headers := map[string]string{
		"Content-Type":        "application/octet-stream",
		"Content-Disposition": `attachment; filename="sample.bin"`,
	}
	resp := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequestWithHeaders(rt.icapURL("/raw-file-fallback"), "/origin/raw-file-fallback", "PAYLOAD", headers))
	assertContains(t, resp, "7\r\nPAYLOAD\r\n0\r\n\r\n")
	assertContains(t, resp, "HTTP/1.1 403 Forbidden")
}

func TestRealIntegration_MultipartNoMatchUsesCurrentFallbackSemantics(t *testing.T) {
	body, contentType := buildNeutralMultipartBody(t)

	tests := []struct {
		name         string
		scenarioYAML string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "raw-file-no-match-errors-without-envelope",
			scenarioYAML: multipartNoMatchRawFileErrorScenarioYAML(),
			wantContains: []string{"ICAP/1.0 500", "failed to resolve stream source"},
			wantMissing:  []string{"helloSAFE", "skip-field", "boundary="},
		},
		{
			name:         "raw-file-no-match-allow-empty-stays-empty",
			scenarioYAML: multipartNoMatchRawFileAllowEmptyScenarioYAML(),
			wantContains: []string{"HTTP/1.1 403 Forbidden", "0\r\n\r\n"},
			wantMissing:  []string{"helloSAFE", "skip-field", "boundary="},
		},
		{
			name:         "body-fallback-returns-fallback-bytes",
			scenarioYAML: multipartNoMatchBodyFallbackScenarioYAML(),
			wantContains: []string{"HTTP/1.1 403 Forbidden", "b\r\nfallback-ok\r\n0\r\n\r\n"},
			wantMissing:  []string{"helloSAFE", "skip-field", "boundary="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenariosDir := t.TempDir()
			writeTestFile(t, filepath.Join(scenariosDir, "neutral-multipart-nomatch.yaml"), tt.scenarioYAML)

			rt := startIntegrationRuntime(t, scenariosDir)
			t.Cleanup(rt.Close)

			var resp string
			if strings.Contains(tt.name, "raw-file-no-match-errors") {
				var readErr error
				resp, readErr = rt.sendAndReadUntilEOF(t, buildREQMODRequestWithHeaders(rt.icapURL("/multipart-no-match"), "/origin/multipart-no-match", body, map[string]string{"Content-Type": contentType}))
				if !errors.Is(readErr, io.EOF) {
					t.Fatalf("expected EOF from error response, got %v", readErr)
				}
			} else {
				resp = rt.sendAndReadUntilFinalChunk(t, buildREQMODRequestWithHeaders(rt.icapURL("/multipart-no-match"), "/origin/multipart-no-match", body, map[string]string{"Content-Type": contentType}))
			}
			for _, want := range tt.wantContains {
				assertContains(t, resp, want)
			}
			for _, unwanted := range tt.wantMissing {
				assertNotContains(t, resp, unwanted)
			}
		})
	}
}

func TestRealIntegration_ScenarioReloadFailurePreservesPreviousGoodScenario(t *testing.T) {
	scenariosDir := t.TempDir()
	scenarioPath := filepath.Join(scenariosDir, "neutral-reload-rollback.yaml")
	writeTestFile(t, scenarioPath, reloadScenarioYAML("stable-body"))

	rt := startIntegrationRuntime(t, scenariosDir)
	t.Cleanup(rt.Close)

	before := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequest(rt.icapURL("/reload-check"), "/origin/reload", "ping"))
	assertContains(t, before, "stable-body")

	writeTestFile(t, scenarioPath, "scenarios:\n  broken: [\n")
	reloadReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rt.managementURL("/api/v1/scenarios/reload"), http.NoBody)
	if err != nil {
		t.Fatalf("create rollback reload request: %v", err)
	}
	reloadResp, err := rt.httpClient.Do(reloadReq)
	if err != nil {
		t.Fatalf("POST /api/v1/scenarios/reload failed: %v", err)
	}
	defer reloadResp.Body.Close()
	if reloadResp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(reloadResp.Body)
		t.Fatalf("reload status = %d, want 500; body=%s", reloadResp.StatusCode, string(body))
	}

	after := rt.sendAndReadUntilFinalChunk(t, buildREQMODRequest(rt.icapURL("/reload-check"), "/origin/reload", "ping"))
	assertContains(t, after, "stable-body")
	assertNotContains(t, after, "reload failed")
}

type integrationRuntime struct {
	icapServer   *server.ICAPServer
	healthServer *health.Server
	icapAddr     string
	healthPort   int
	httpClient   *http.Client
	shutdown     func()
}

type stagedStreamingResponse struct {
	firstStage string
	remainder  string
}

func (r *integrationRuntime) Close() {
	r.shutdown()
}

func (r *integrationRuntime) icapURL(path string) string {
	return "icap://" + r.icapAddr + path
}

func (r *integrationRuntime) managementURL(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", r.healthPort, path)
}

func (r *integrationRuntime) sendAndReadUntilFinalChunk(t *testing.T, rawRequest string) string {
	t.Helper()
	resp, err := sendRawICAPRequest(r.icapAddr, rawRequest, true)
	if err != nil {
		t.Fatalf("sendRawICAPRequest(final chunk) failed: %v\nresponse=%q", err, resp)
	}
	return resp
}

func (r *integrationRuntime) sendAndReadUntilEOF(t *testing.T, rawRequest string) (string, error) {
	t.Helper()
	return sendRawICAPRequest(r.icapAddr, rawRequest, false)
}

func (r *integrationRuntime) sendAndReadStagedStream(t *testing.T, rawRequest, firstChunk string) stagedStreamingResponse {
	t.Helper()
	conn, err := openStreamingConn(r.icapAddr, rawRequest)
	if err != nil {
		t.Fatalf("open staged ICAP stream: %v", err)
	}
	defer conn.Close()
	firstStage := readUntilToken(t, conn, firstChunk)
	remainder := readUntilToken(t, conn, "0\r\n\r\n")
	return stagedStreamingResponse{firstStage: firstStage, remainder: remainder}
}

func startIntegrationRuntime(t *testing.T, scenariosDir string) *integrationRuntime {
	return startIntegrationRuntimeWithConfig(t, scenariosDir, nil)
}

func startIntegrationRuntimeWithConfig(t *testing.T, scenariosDir string, mutate func(*config.Config)) *integrationRuntime {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	log := newTestIntegrationLogger(t)

	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.SourcePath = filepath.Join(scenariosDir, "test-config.yaml")
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = integrationOperationTimeout
	cfg.Server.WriteTimeout = integrationOperationTimeout
	cfg.Server.ShutdownTimeout = integrationOperationTimeout
	cfg.Health.Enabled = true
	cfg.Health.Port = freeTCPPort(t)
	cfg.Health.HealthPath = "/health"
	cfg.Health.ReadyPath = "/ready"
	cfg.Management.Enabled = true
	cfg.Management.ScenarioReloadEnabled = true
	cfg.Management.ConfigReloadEnabled = false
	cfg.Metrics.Enabled = false
	cfg.Storage.Enabled = false
	cfg.RateLimit.Enabled = false
	cfg.Mock.ScenariosDir = scenariosDir
	if mutate != nil {
		mutate(cfg)
	}

	metricsRegistry, collector, err := createMetricsCollector()
	if err != nil {
		cancel()
		_ = log.Close()
		t.Fatalf("createMetricsCollector() error = %v", err)
	}
	_ = metricsRegistry

	healthServer, err := createHealthServer(cfg)
	if err != nil {
		cancel()
		_ = log.Close()
		t.Fatalf("createHealthServer() error = %v", err)
	}

	runtimeManager := management.NewRuntimeManager(cfg, cfg.SourcePath)
	servers, firstRegistry, err := startAllServers(ctx, cfg, buildServerEntries(cfg), collector, ratelimit.Limiter(nil), (*middleware.StorageMiddleware)(nil), log, runtimeManager)
	if err != nil {
		cancel()
		if healthServer != nil {
			_ = healthServer.Stop(context.Background())
		}
		_ = log.Close()
		t.Fatalf("startAllServers() error = %v", err)
	}
	if len(servers) != 1 {
		cancel()
		for _, srv := range servers {
			_ = srv.Stop(context.Background())
		}
		if healthServer != nil {
			_ = healthServer.Stop(context.Background())
		}
		_ = log.Close()
		t.Fatalf("expected exactly one ICAP server, got %d", len(servers))
	}

	startHealthServer(ctx, cfg, healthServer, firstRegistry, runtimeManager, log)
	waitForReady(t, cfg.Health.Port)

	shutdown := func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if healthServer != nil {
			_ = healthServer.Stop(shutdownCtx)
		}
		for _, srv := range servers {
			_ = srv.Stop(shutdownCtx)
		}
		_ = log.Close()
	}

	return &integrationRuntime{
		icapServer:   servers[0],
		healthServer: healthServer,
		icapAddr:     servers[0].Addr().String(),
		healthPort:   cfg.Health.Port,
		httpClient:   &http.Client{Timeout: integrationOperationTimeout},
		shutdown:     shutdown,
	}
}

func writeConnString(t *testing.T, conn net.Conn, data string) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(integrationOperationTimeout)); err != nil {
		t.Fatalf("set write deadline: %v", err)
	}
	if _, err := io.WriteString(conn, data); err != nil {
		t.Fatalf("write connection data: %v", err)
	}
}

func waitForReady(t *testing.T, port int) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(integrationOperationTimeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/ready", port)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
		if err != nil {
			t.Fatalf("create ready request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(readinessPollInterval)
	}

	t.Fatalf("health server on port %d did not become ready", port)
}

func openStreamingConn(addr, rawRequest string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, integrationOperationTimeout)
	if err != nil {
		return nil, err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(integrationOperationTimeout)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := io.WriteString(conn, rawRequest); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func readUntilToken(t *testing.T, conn net.Conn, token string) string {
	t.Helper()
	got, err := readConnUntilToken(conn, token, integrationOperationTimeout)
	if err != nil {
		t.Fatalf("read until %q: %v\npartial response=%q", token, err, got)
	}
	return got
}

func readConnUntilToken(conn net.Conn, token string, timeout time.Duration) (string, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for !strings.Contains(buf.String(), token) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return buf.String(), err
		}
		n, err := conn.Read(tmp)
		buf.Write(tmp[:n])
		if err != nil {
			return buf.String(), err
		}
	}
	return buf.String(), nil
}

func sendRawICAPRequest(addr, rawRequest string, stopOnFinalChunk bool) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, integrationOperationTimeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(integrationOperationTimeout)); err != nil {
		return "", err
	}
	if _, err := io.WriteString(conn, rawRequest); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	tmp := make([]byte, 256)
	for {
		n, readErr := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if stopOnFinalChunk && strings.Contains(buf.String(), "0\r\n\r\n") {
				return buf.String(), nil
			}
		}
		if readErr == nil {
			continue
		}
		if stopOnFinalChunk && errors.Is(readErr, io.EOF) && strings.Contains(buf.String(), "0\r\n\r\n") {
			return buf.String(), nil
		}
		return buf.String(), readErr
	}
}

func buildREQMODRequest(serviceURL, httpPath, body string) string {
	return buildREQMODRequestWithHeaders(serviceURL, httpPath, body, nil)
}

func buildREQMODRequestWithHeaders(serviceURL, httpPath, body string, headers map[string]string) string {
	return buildREQMODRequestHead(serviceURL, httpPath, headers, len(body)) + chunked(body)
}

func buildREQMODRequestHead(serviceURL, httpPath string, headers map[string]string, bodyLen int) string {
	httpHead := "POST " + httpPath + " HTTP/1.1\r\n" +
		"Host: origin.example\r\n" +
		buildHTTPHeaderLines(headers) +
		"Content-Length: " + strconv.Itoa(bodyLen) + "\r\n\r\n"
	return "REQMOD " + serviceURL + " ICAP/1.0\r\n" +
		"Host: " + hostFromICAPURL(serviceURL) + "\r\n" +
		"Encapsulated: req-hdr=0, req-body=" + strconv.Itoa(len(httpHead)) + "\r\n\r\n" +
		httpHead
}

func buildRESPMODRequest(serviceURL, httpPath, responseBody string) string {
	requestHead := "GET " + httpPath + " HTTP/1.1\r\n" +
		"Host: origin.example\r\n\r\n"
	responseHead := "HTTP/1.1 200 OK\r\n" +
		"Content-Length: " + strconv.Itoa(len(responseBody)) + "\r\n\r\n"
	chunkedBody := chunked(responseBody)
	return "RESPMOD " + serviceURL + " ICAP/1.0\r\n" +
		"Host: " + hostFromICAPURL(serviceURL) + "\r\n" +
		"Encapsulated: req-hdr=0, res-hdr=" + strconv.Itoa(len(requestHead)) + ", res-body=" + strconv.Itoa(len(requestHead)+len(responseHead)) + "\r\n\r\n" +
		requestHead + responseHead + chunkedBody
}

func chunked(body string) string {
	if body == "" {
		return "0\r\n\r\n"
	}
	return strconv.FormatInt(int64(len(body)), 16) + "\r\n" + body + "\r\n0\r\n\r\n"
}

func hostFromICAPURL(raw string) string {
	trimmed := strings.TrimPrefix(raw, "icap://")
	idx := strings.IndexByte(trimmed, '/')
	if idx >= 0 {
		return trimmed[:idx]
	}
	return trimmed
}

func buildHTTPHeaderLines(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	order := []string{"Content-Type", "Content-Disposition"}
	var b strings.Builder
	seen := make(map[string]struct{}, len(headers))
	for _, key := range order {
		value, ok := headers[key]
		if !ok {
			continue
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
		seen[key] = struct{}{}
	}
	for key, value := range headers {
		if _, ok := seen[key]; ok {
			continue
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
	}
	return b.String()
}

func buildSegmentedRESPMODRequest(serviceURL, httpPath, requestBody, responseBody string) string {
	requestHeader := "POST " + httpPath + " HTTP/1.1\r\n" +
		"Host: origin.example\r\n" +
		"Content-Length: " + strconv.Itoa(len(requestBody)) + "\r\n\r\n"
	requestChunked := chunked(requestBody)
	responseHeader := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Length: " + strconv.Itoa(len(responseBody)) + "\r\n\r\n"
	responseChunked := chunked(responseBody)
	reqBodyOffset := len(requestHeader)
	resHdrOffset := reqBodyOffset + len(requestChunked)
	resBodyOffset := resHdrOffset + len(responseHeader)
	return fmt.Sprintf("RESPMOD %s ICAP/1.0\r\nHost: %s\r\nEncapsulated: req-hdr=0, req-body=%d, res-hdr=%d, res-body=%d\r\n\r\n%s%s%s%s",
		serviceURL,
		hostFromICAPURL(serviceURL),
		reqBodyOffset,
		resHdrOffset,
		resBodyOffset,
		requestHeader,
		requestChunked,
		responseHeader,
		responseChunked,
	)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type %T", ln.Addr())
	}
	return addr.Port
}

func newTestIntegrationLogger(t *testing.T) *logger.Logger {
	t.Helper()

	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "debug", Format: "text"}, io.Discard)
	if err != nil {
		t.Fatalf("logger.NewWithWriter() error = %v", err)
	}
	return log
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("response missing %q\nfull response:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Fatalf("response unexpectedly contained %q\nfull response:\n%s", unwanted, got)
	}
}

func reloadScenarioYAML(body string) string {
	return "defaults:\n" +
		"  method: REQMOD\n" +
		"  endpoint: /reload-check\n" +
		"scenarios:\n" +
		"  neutral_reload:\n" +
		"    status: 200\n" +
		"    http_status: 403\n" +
		"    http_body: \"" + body + "\"\n"
}

func completeStreamingScenarioYAML() string {
	return fmt.Sprintf(`scenarios:
  neutral_request_stream_complete:
    method: REQMOD
    endpoint: /stream-request-complete
    status: 200
    http_status: 403
    stream:
      source:
        from: request_body
      chunks:
        size: 2
        delay: %s
      finish:
        mode: complete

  neutral_response_stream_complete:
    method: RESPMOD
    endpoint: /stream-response-complete
    status: 200
    stream:
      source:
        from: response_body
      chunks:
        size: 3
        delay: %s
      finish:
        mode: complete
`, streamingChunkDelay, streamingChunkDelay)
}

func finStreamingScenarioYAML() string {
	return `scenarios:
  neutral_response_stream_fin:
    method: RESPMOD
    endpoint: /stream-response-fin
    status: 200
    stream:
      source:
        from: response_body
      chunks:
        size: 3
      finish:
        mode: fin
        fin:
          close: clean
          after:
            bytes: 3
`
}

func requestHTTPBodyCompleteScenarioYAML() string {
	return fmt.Sprintf(`scenarios:
  neutral_request_http_body_complete:
    method: REQMOD
    endpoint: /stream-request-http-complete
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      chunks:
        size: 2
        delay: %s
      finish:
        mode: complete
`, streamingChunkDelay)
}

func requestHTTPBodyFINScenarioYAML() string {
	return `scenarios:
  neutral_request_http_body_fin:
    method: REQMOD
    endpoint: /stream-request-http-fin
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      chunks:
        size: 2
      finish:
        mode: fin
        fin:
          close: clean
          after:
            bytes: 3
`
}

func segmentedResponseHTTPBodyScenarioYAML() string {
	return fmt.Sprintf(`scenarios:
  neutral_segmented_response_http_body:
    method: RESPMOD
    endpoint: /stream-segmented-response
    status: 200
    stream:
      source:
        from: response_http_body
      chunks:
        size: 3
        delay: %s
      finish:
        mode: complete
`, streamingChunkDelay)
}

func multipartSelectorScenarioYAML() string {
	return `scenarios:
  neutral_multipart_selector:
    method: REQMOD
    endpoint: /multipart-select
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      multipart:
        fields:
          - comment
        files:
          enabled: true
          filename:
            - '.*\.bin$'
      chunks:
        size: 64
      finish:
        mode: complete
`
}

func rawFileFallbackScenarioYAML() string {
	return `scenarios:
  neutral_raw_file_fallback:
    method: REQMOD
    endpoint: /raw-file-fallback
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      multipart:
        files:
          enabled: true
      fallback:
        raw_file:
          enabled: true
          filename:
            - '.*\.bin$'
      chunks:
        size: 64
      finish:
        mode: complete
`
}

func multipartNoMatchRawFileErrorScenarioYAML() string {
	return `scenarios:
  neutral_multipart_no_match_raw_file_error:
    method: REQMOD
    endpoint: /multipart-no-match
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      multipart:
        files:
          enabled: true
          filename:
            - 'nomatch$'
      fallback:
        raw_file:
          enabled: true
      chunks:
        size: 64
      finish:
        mode: complete
`
}

func multipartNoMatchRawFileAllowEmptyScenarioYAML() string {
	return `scenarios:
  neutral_multipart_no_match_raw_file_empty:
    method: REQMOD
    endpoint: /multipart-no-match
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      multipart:
        allow_empty: true
        files:
          enabled: true
          filename:
            - 'nomatch$'
      fallback:
        raw_file:
          enabled: true
      chunks:
        size: 64
      finish:
        mode: complete
`
}

func multipartNoMatchBodyFallbackScenarioYAML() string {
	return `scenarios:
  neutral_multipart_no_match_body_fallback:
    method: REQMOD
    endpoint: /multipart-no-match
    status: 200
    http_status: 403
    stream:
      source:
        from: request_http_body
      multipart:
        files:
          enabled: true
          filename:
            - 'nomatch$'
      fallback:
        body: fallback-ok
      chunks:
        size: 64
      finish:
        mode: complete
`
}

func buildNeutralMultipartBody(t *testing.T) (body, contentType string) {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("comment", "hello"); err != nil {
		t.Fatalf("write multipart comment: %v", err)
	}
	if err := writer.WriteField("note", "skip-field"); err != nil {
		t.Fatalf("write multipart note: %v", err)
	}
	selectedFile, err := writer.CreateFormFile("upload", "neutral.bin")
	if err != nil {
		t.Fatalf("create selected multipart file: %v", err)
	}
	if _, err := io.WriteString(selectedFile, "SAFE"); err != nil {
		t.Fatalf("write selected multipart file: %v", err)
	}
	ignoredFile, err := writer.CreateFormFile("upload", "ignored.txt")
	if err != nil {
		t.Fatalf("create ignored multipart file: %v", err)
	}
	if _, err := io.WriteString(ignoredFile, "IGNORE"); err != nil {
		t.Fatalf("write ignored multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buf.String(), writer.FormDataContentType()
}
