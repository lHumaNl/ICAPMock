// Copyright 2026 ICAP Mock

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestResponseFlushFailureRecordsRequestAndErrorMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	pool := NewConnectionPool()
	srv, err := NewServer(&config.ServerConfig{
		Host: "127.0.0.1", Port: 0, ReadTimeout: time.Second, WriteTimeout: time.Second,
		IdleTimeout: time.Second, MaxConnections: 1, MaxBodySize: 1024 * 1024, Streaming: true,
	}, pool, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	srv.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetMetrics(collector)
	srv.SetMetricsServerName("edge")

	rtr := router.NewRouter()
	if err := rtr.HandleFunc("/scan", func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "clean")
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}); err != nil {
		t.Fatalf("HandleFunc() error = %v", err)
	}
	srv.SetRouter(rtr)

	httpRequest := "GET /archive.zip HTTP/1.1\r\nHost: origin.example\r\nContent-Type: application/zip\r\n\r\n"
	rawRequest := fmt.Sprintf(
		"REQMOD icap://localhost/scan ICAP/1.0\r\nHost: localhost\r\nEncapsulated: req-hdr=0, null-body=%d\r\n\r\n%s",
		len(httpRequest),
		httpRequest,
	)
	baseRequestLabels := map[string]string{
		"server": "edge", "method": "REQMOD", "content_type": "application/zip",
		"response": "clean", "scenario": "scan",
	}
	errorLabels := cloneMetricLabels(baseRequestLabels)
	errorLabels["outcome"] = metricsinternal.OutcomeError
	allowedLabels := cloneMetricLabels(baseRequestLabels)
	allowedLabels["outcome"] = metricsinternal.OutcomeAllowed
	requestsBeforeFlush := -1.0
	netConn := &responseWriteFailConn{
		reader:   bytes.NewReader([]byte(rawRequest)),
		writeErr: errors.New("connection reset by peer"),
		beforeWrite: func() {
			requestsBeforeFlush = gatheredCounterValue(t, reg, "icap_requests_total", errorLabels) +
				gatheredCounterValue(t, reg, "icap_requests_total", allowedLabels)
		},
	}
	conn := newConnection(netConn, &ConnectionConfig{
		ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second,
		MaxBodySize: 1024 * 1024, Streaming: true,
	})
	pool.Add(conn)
	collector.IncActiveConnectionsForServer("edge")
	srv.semaphore <- struct{}{}
	srv.wg.Add(1)

	srv.handleConnection(context.Background(), conn)

	if requestsBeforeFlush != 0 {
		t.Errorf("request count before response flush = %v, want 0", requestsBeforeFlush)
	}
	if got := gatheredCounterValue(t, reg, "icap_requests_total", errorLabels); got != 1 {
		t.Errorf("error request count = %v, want 1", got)
	}
	if got := gatheredHistogramCount(t, reg, errorLabels); got != 1 {
		t.Errorf("error latency count = %v, want 1", got)
	}
	if got := gatheredCounterValue(t, reg, "icap_requests_total", allowedLabels); got != 0 {
		t.Errorf("allowed request count = %v, want 0", got)
	}

	requestErrorLabels := map[string]string{
		"server": "edge", "method": "REQMOD", "stage": "write_response",
		"error_type": "response_write_failed", "scenario": "scan", "response": "clean",
	}
	if got := gatheredCounterValue(t, reg, "icap_request_errors_total", requestErrorLabels); got != 1 {
		t.Errorf("request write errors = %v, want 1", got)
	}
	if got := gatheredCounterValue(t, reg, "icap_errors_total", map[string]string{
		"server": "edge", "type": "response_write_failed",
	}); got != 1 {
		t.Errorf("aggregate write errors = %v, want 1", got)
	}
	if got := gatheredCounterValue(t, reg, "icap_connection_closes_total", map[string]string{
		"server": "edge", "reason": "write_error",
	}); got != 1 {
		t.Errorf("write-error connection closes = %v, want 1", got)
	}
}

func cloneMetricLabels(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source)+1)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func gatheredCounterValue(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric, labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

type responseWriteFailConn struct {
	reader      *bytes.Reader
	writeErr    error
	beforeWrite func()
}

func (c *responseWriteFailConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *responseWriteFailConn) Write([]byte) (int, error) {
	if c.beforeWrite != nil {
		c.beforeWrite()
	}
	return 0, c.writeErr
}
func (c *responseWriteFailConn) Close() error                { return nil }
func (c *responseWriteFailConn) LocalAddr() net.Addr         { return staticAddr("127.0.0.1:1344") }
func (c *responseWriteFailConn) RemoteAddr() net.Addr        { return staticAddr("192.0.2.10:44354") }
func (c *responseWriteFailConn) SetDeadline(time.Time) error { return nil }
func (c *responseWriteFailConn) SetReadDeadline(time.Time) error {
	return nil
}
func (c *responseWriteFailConn) SetWriteDeadline(time.Time) error {
	return nil
}

type staticAddr string

func (a staticAddr) Network() string { return "tcp" }
func (a staticAddr) String() string  { return string(a) }
