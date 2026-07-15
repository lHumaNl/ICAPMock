// Copyright 2026 ICAP Mock

package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestMockProcessorLogsSlowScenarioMatch(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	proc := NewMockProcessor(storage.NewScenarioRegistry(), log)
	proc.server = "edge"
	req := loggedRequest()
	req.HTTPRequest.URI = "/files/sample.exe"
	scenario := loggedScenario()

	ctx := util.WithRequestID(context.Background(), "must-not-be-logged")
	proc.logSlowScenarioMatch(ctx, req, scenario, 2*time.Second)

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogField(t, entry, "level", "WARN")
	assertLogField(t, entry, "msg", "slow scenario match")
	assertLogField(t, entry, "server", "edge")
	assertLogField(t, entry, "scenario", scenario.Name)
	assertLogField(t, entry, "client_ip", req.ClientIP)
	assertLogField(t, entry, "uri", req.URI)
	assertLogFieldAbsent(t, entry, "url")
	assertLogFieldAbsent(t, entry, "request_id")
	if got := entry["context_expired"]; got != false {
		t.Errorf("context_expired = %v, want false", got)
	}
	if got := entry["http_uri_length"]; got != float64(len(req.HTTPRequest.URI)) {
		t.Errorf("http_uri_length = %v, want %d", got, len(req.HTTPRequest.URI))
	}
}

func TestMockProcessorWatchdogLogsWhileMatchIsRunningAndTimeoutCancelsIt(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	proc := NewMockProcessor(contextBlockingRegistry{}, log)
	proc.server = "edge"
	req := loggedRequest()
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()

	_, err = proc.Process(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Process() error = %v, want context.DeadlineExceeded", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"msg":"scenario match still running"`) {
		t.Fatalf("watchdog log missing from %q", output)
	}
	if !strings.Contains(output, `"uri":"`+req.URI+`"`) {
		t.Fatalf("watchdog URI missing from %q", output)
	}
	if strings.Contains(output, `"request_id"`) {
		t.Fatalf("watchdog must not log request_id: %q", output)
	}
}

func TestMockProcessorLogsMatchedScenarioAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}

	registry := storage.NewScenarioRegistry()
	if err := registry.Add(loggedScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc := NewMockProcessor(registry, log)
	proc.server = "edge"

	ctx := util.WithRequestID(context.Background(), "req-123")
	if _, err := proc.Process(ctx, loggedRequest()); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogField(t, entry, "level", "INFO")
	assertLogField(t, entry, "msg", "scenario matched")
	assertLogField(t, entry, "server", "edge")
	assertLogField(t, entry, "scenario", "clean-html")
	assertLogField(t, entry, "method", icap.MethodREQMOD)
	assertLogField(t, entry, "uri", "icap://localhost/avscan")
	assertLogField(t, entry, "content_type", "text/html")
	assertLogField(t, entry, "client_ip", "192.0.2.10")
	assertLogField(t, entry, "response", "clean")
	assertLogFieldAbsent(t, entry, "request_id")
	assertLogFieldAbsent(t, entry, "icap_status")
	assertLogFieldAbsent(t, entry, "http_status")
	assertLogFieldAbsent(t, entry, "outcome")
}

func TestMockProcessorLogsRequestScopedContentTypeLabel(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(loggedScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc := NewMockProcessor(registry, log)
	ctx := requestinfo.WithContentTypeLabel(context.Background(), metrics.ContentTypeOther)

	if _, err := proc.Process(ctx, loggedRequest()); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogField(t, entry, "content_type", metrics.ContentTypeOther)
}

func TestMockProcessorOmitsClientIPWhenUnavailable(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}

	registry := storage.NewScenarioRegistry()
	if err := registry.Add(loggedScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc := NewMockProcessor(registry, log)

	if _, err := proc.Process(context.Background(), loggedRequestWithoutClientIP()); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogFieldAbsent(t, entry, "client_ip")
}

func TestMockProcessorLogsAndCountsNoScenarioMatchAtError(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "error", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	proc := NewMockProcessor(noMatchRegistry{}, log)
	proc.SetMetricsForServer(collector, "edge")

	if _, err := proc.Process(context.Background(), loggedRequest()); err == nil {
		t.Fatal("Process() error = nil, want no-match error")
	}

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogField(t, entry, "level", "ERROR")
	assertLogField(t, entry, "stage", metrics.RequestErrorStageProcessorMatch)
	assertLogField(t, entry, "error_type", metrics.RequestErrorTypeNoScenarioMatch)
	assertProcessorErrorMetric(t, reg, metrics.RequestErrorStageProcessorMatch, metrics.RequestErrorTypeNoScenarioMatch)
}

func TestMockProcessorLogsAndCountsBuildErrorAtError(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.NewWithWriter(config.LoggingConfig{Level: "error", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	reg := prometheus.NewRegistry()
	collector, err := metrics.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	registry := storage.NewScenarioRegistry()
	if err := registry.Add(buildErrorScenario()); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	proc := NewMockProcessor(registry, log)
	proc.SetMetricsForServer(collector, "edge")

	if _, err := proc.Process(context.Background(), buildErrorRequest()); err == nil {
		t.Fatal("Process() error = nil, want build error")
	}

	entry := decodeLogEntry(t, buf.Bytes())
	assertLogField(t, entry, "level", "ERROR")
	assertLogField(t, entry, "stage", metrics.RequestErrorStageProcessorBuild)
	assertLogField(t, entry, "error_type", metrics.RequestErrorTypeResponseBuildFailed)
	assertLogField(t, entry, "scenario", "broken-body")
	assertProcessorErrorMetric(t, reg, metrics.RequestErrorStageProcessorBuild, metrics.RequestErrorTypeResponseBuildFailed)
}

func loggedScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:     "clean-html",
		Match:    storage.MatchRule{Methods: []string{icap.MethodREQMOD}},
		Response: storage.ResponseTemplate{ICAPStatus: icap.StatusOK, ResponseName: "clean"},
		Priority: 100,
	}
}

func buildErrorScenario() *storage.Scenario {
	return &storage.Scenario{
		Name:     "broken-body",
		Match:    storage.MatchRule{Methods: []string{icap.MethodREQMOD}},
		Response: storage.ResponseTemplate{ICAPStatus: icap.StatusOK},
		Priority: 100,
	}
}

func loggedRequest() *icap.Request {
	return &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/avscan",
		ClientIP: "192.0.2.10",
		Header:   icap.NewHeader(),
		HTTPRequest: &icap.HTTPMessage{
			Header: icap.Header{"Content-Type": {"Text/HTML; charset=utf-8"}},
		},
	}
}

func loggedRequestWithoutClientIP() *icap.Request {
	req := loggedRequest()
	req.ClientIP = ""
	return req
}

func buildErrorRequest() *icap.Request {
	req := loggedRequest()
	req.HTTPRequest.BodyReader = errReader{}
	return req
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("body reader failed")
}

type noMatchRegistry struct{}

func (noMatchRegistry) Load(string) error { return nil }

func (noMatchRegistry) Match(context.Context, *icap.Request) (*storage.Scenario, error) {
	return nil, storage.ErrNoMatch
}

func (noMatchRegistry) Reload() error { return nil }

func (noMatchRegistry) List() []*storage.Scenario { return nil }

func (noMatchRegistry) Add(*storage.Scenario) error { return nil }

func (noMatchRegistry) Remove(string) error { return nil }

type contextBlockingRegistry struct{}

func (contextBlockingRegistry) Load(string) error { return nil }
func (contextBlockingRegistry) Match(ctx context.Context, _ *icap.Request) (*storage.Scenario, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (contextBlockingRegistry) Reload() error               { return nil }
func (contextBlockingRegistry) List() []*storage.Scenario   { return nil }
func (contextBlockingRegistry) Add(*storage.Scenario) error { return nil }
func (contextBlockingRegistry) Remove(string) error         { return nil }

func decodeLogEntry(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return entry
}

func assertLogField(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	if got, ok := entry[key].(string); !ok || got != want {
		t.Fatalf("%s = %v, want %q", key, entry[key], want)
	}
}

func assertLogFieldAbsent(t *testing.T, entry map[string]any, key string) {
	t.Helper()
	if _, ok := entry[key]; ok {
		t.Fatalf("unexpected log field %q", key)
	}
}

func assertProcessorErrorMetric(t *testing.T, reg prometheus.Gatherer, stage, errorType string) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "icap_request_errors_total" && errorMetricExists(mf.GetMetric(), stage, errorType) {
			return
		}
	}
	t.Fatalf("request error metric not found for stage=%s error_type=%s", stage, errorType)
}

func errorMetricExists(metricsList []*dto.Metric, stage, errorType string) bool {
	for _, metric := range metricsList {
		if processorMetricLabelsMatch(metric, stage, errorType) {
			return true
		}
	}
	return false
}

func processorMetricLabelsMatch(metric *dto.Metric, stage, errorType string) bool {
	labels := map[string]string{"stage": stage, "error_type": errorType}
	found := map[string]bool{"stage": false, "error_type": false}
	for _, label := range metric.GetLabel() {
		if want, ok := labels[label.GetName()]; ok && label.GetValue() != want {
			return false
		} else if ok {
			found[label.GetName()] = true
		}
	}
	return found["stage"] && found["error_type"]
}
