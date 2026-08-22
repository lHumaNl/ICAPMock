// Copyright 2026 ICAP Mock

package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	internalhandler "github.com/icap-mock/icap-mock/internal/handler"
	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const shortLifecyclePacing = 100 * time.Millisecond

func TestServerPacedStreamCompletionMetrics(t *testing.T) {
	for _, test := range []struct {
		mode    string
		outcome string
	}{
		{mode: icap.StreamFinishComplete, outcome: metricsinternal.OutcomeAllowed},
		{mode: icap.StreamFinishFIN, outcome: metricsinternal.OutcomeBlocked},
		{mode: icap.StreamFinishTerm, outcome: metricsinternal.OutcomeBlocked},
	} {
		t.Run(test.mode, func(t *testing.T) {
			reg, srv := newLifecycleMetricServer(t, pacedResponseHandler(t, test.mode))
			conn := dialLifecycleServer(t, srv)
			defer conn.Close()

			writeLifecycleOPTIONS(t, conn, srv.Addr().String(), true)
			require.Eventually(t, func() bool {
				return gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}) == 1
			}, time.Second, 5*time.Millisecond)

			labels := lifecycleLabels(test.outcome, "paced")
			require.Zero(t, gatheredHistogramCount(t, reg, labels))
			_, err := io.ReadAll(conn)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				return gatheredHistogramCount(t, reg, labels) == 1
			}, time.Second, 5*time.Millisecond)
			require.Equal(t, float64(1), gatheredCounterValue(t, reg, requestsTotalMetricName, labels))
			require.Zero(t, gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}))
			require.GreaterOrEqual(t, gatheredHistogramSum(t, reg, labels), 0.07)
		})
	}
}

func TestServerOrdinaryDelayIsTimedThroughFlush(t *testing.T) {
	delay := 80 * time.Millisecond
	reg, srv := newLifecycleMetricServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "ordinary")
		time.Sleep(delay)
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()

	writeLifecycleOPTIONS(t, conn, srv.Addr().String(), true)
	labels := lifecycleLabels(metricsinternal.OutcomeAllowed, "ordinary")
	time.Sleep(delay / 2)
	require.Zero(t, gatheredHistogramCount(t, reg, labels))
	require.Zero(t, gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}))
	_, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 1
	}, time.Second, 5*time.Millisecond)
	require.GreaterOrEqual(t, gatheredHistogramSum(t, reg, labels), delay.Seconds()*0.8)
}

func TestServerUploadWaitIsExcludedFromLatency(t *testing.T) {
	reg, srv := newLifecycleMetricServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "upload")
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()

	httpReq := "POST /upload HTTP/1.1\r\nHost: origin.example\r\nContent-Length: 10\r\n\r\n"
	encap := fmt.Sprintf("req-hdr=0, req-body=%d", len(httpReq))
	prefix := fmt.Sprintf(
		"REQMOD icap://%s/scan ICAP/1.0\r\nHost: localhost\r\nConnection: close\r\nEncapsulated: %s\r\n\r\n%s5\r\nhello\r\n",
		srv.Addr().String(), encap, httpReq,
	)
	_, err := conn.Write([]byte(prefix))
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	_, err = conn.Write([]byte("5\r\nworld\r\n0\r\n\r\n"))
	require.NoError(t, err)
	_, err = io.ReadAll(conn)
	require.NoError(t, err)

	labels := lifecycleREQMODLabels(metricsinternal.OutcomeAllowed, "upload")
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 1
	}, time.Second, 5*time.Millisecond)
	require.Less(t, gatheredHistogramSum(t, reg, labels), 0.15)
}

func TestServerKeepAliveWaitDoesNotLeakIntoNextRequestLatency(t *testing.T) {
	reg, srv := newLifecycleMetricServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "keepalive")
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	labels := lifecycleLabels(metricsinternal.OutcomeAllowed, "keepalive")

	writeLifecycleOPTIONS(t, conn, srv.Addr().String(), false)
	require.Contains(t, readICAPResponseStatus(t, reader), "204")
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 1
	}, time.Second, 5*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	writeLifecycleOPTIONS(t, conn, srv.Addr().String(), true)
	require.Contains(t, readICAPResponseStatus(t, reader), "204")
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 2
	}, time.Second, 5*time.Millisecond)
	require.Less(t, gatheredHistogramSum(t, reg, labels), 0.1)
}

func TestServerPreviewContinuationExcludesDeferredUploadFromLatency(t *testing.T) {
	var calls atomic.Int32
	reg, srv := newLifecycleProcessorServer(t, processor.Func(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		calls.Add(1)
		requestinfo.SetScenarioMetadata(ctx, "scan", "preview-body")
		body, err := req.HTTPRequest.GetBody()
		if err != nil {
			return nil, err
		}
		if string(body) != "helloworld" {
			return nil, fmt.Errorf("materialized body = %q", body)
		}
		requestinfo.StartScenarioTiming(ctx)
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}))
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()

	prefix := strings.Replace(
		reqmodPreviewChunkedPrefix(srv.Addr().String()),
		"Encapsulated:", "Connection: close\r\nEncapsulated:", 1,
	)
	_, err := conn.Write([]byte(prefix + "5\r\nhello\r\n0\r\n\r\n"))
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	require.Contains(t, readICAPLine(t, reader), "100 Continue")
	require.Empty(t, readICAPLine(t, reader))
	require.Equal(t, float64(1), gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))
	require.Equal(t, float64(1), gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))
	time.Sleep(200 * time.Millisecond)
	_, err = conn.Write([]byte("5\r\nworld\r\n0\r\n\r\n"))
	require.NoError(t, err)
	response, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(response), "ICAP/1.0 204")
	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))
	require.Zero(t, gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))

	labels := lifecycleREQMODLabels(metricsinternal.OutcomeAllowed, "preview-body")
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 1
	}, time.Second, 5*time.Millisecond)
	require.Less(t, gatheredHistogramSum(t, reg, labels), 0.15)
}

func TestServerPreviewErrorAfterMaterializationUsesRecordedTimingBoundary(t *testing.T) {
	reg, srv := newLifecycleMetricServer(t, func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "preview-error")
		if _, err := req.HTTPRequest.GetBody(); err != nil {
			return nil, err
		}
		return nil, errors.New("post-materialization failure")
	})
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()
	prefix := strings.Replace(
		reqmodPreviewChunkedPrefix(srv.Addr().String()),
		"Encapsulated:", "Connection: close\r\nEncapsulated:", 1,
	)
	_, err := conn.Write([]byte(prefix + "5\r\nhello\r\n0\r\n\r\n"))
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	require.Contains(t, readICAPLine(t, reader), "100 Continue")
	require.Empty(t, readICAPLine(t, reader))
	time.Sleep(200 * time.Millisecond)
	_, err = conn.Write([]byte("5\r\nworld\r\n0\r\n\r\n"))
	require.NoError(t, err)
	_, err = io.ReadAll(reader)
	require.NoError(t, err)

	labels := lifecycleREQMODLabels(metricsinternal.OutcomeError, "preview-error")
	require.Eventually(t, func() bool {
		return gatheredHistogramCount(t, reg, labels) == 1
	}, time.Second, 5*time.Millisecond)
	require.Less(t, gatheredHistogramSum(t, reg, labels), 0.15)
}

func TestServerPreviewIEOFMaterializesWithoutContinue(t *testing.T) {
	reg, srv := newLifecycleProcessorServer(t, processor.Func(func(ctx context.Context, req *icap.Request) (*icap.Response, error) {
		body, err := req.HTTPRequest.GetBody()
		if err != nil {
			return nil, err
		}
		if string(body) != "hello" {
			return nil, fmt.Errorf("materialized body = %q", body)
		}
		requestinfo.SetScenarioMetadata(ctx, "scan", "preview-ieof")
		requestinfo.StartScenarioTiming(ctx)
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}))
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()
	prefix := strings.Replace(
		reqmodPreviewChunkedPrefix(srv.Addr().String()),
		"Encapsulated:", "Connection: close\r\nEncapsulated:", 1,
	)

	_, err := conn.Write([]byte(prefix + "5\r\nhello\r\n0; ieof\r\n\r\n"))
	require.NoError(t, err)
	response, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Contains(t, string(response), "ICAP/1.0 204")
	require.NotContains(t, string(response), "100 Continue")
	require.Zero(t, gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))
	require.Zero(t, gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}))
}

func TestServerRejectsPreviewBytesBeyondDeclaredAggregateLimit(t *testing.T) {
	var calls atomic.Int32
	_, srv := newLifecycleMetricServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		calls.Add(1)
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	conn := dialLifecycleServer(t, srv)
	defer conn.Close()
	httpReq := "POST /upload HTTP/1.1\r\nHost: origin.example\r\nContent-Length: 1\r\n\r\n"
	encap := fmt.Sprintf("req-hdr=0, req-body=%d", len(httpReq))
	request := fmt.Sprintf(
		"REQMOD icap://%s/scan ICAP/1.0\r\nHost: localhost\r\nPreview: 0\r\nEncapsulated: %s\r\n\r\n%s1\r\nx\r\n0\r\n\r\n",
		srv.Addr().String(), encap, httpReq,
	)

	_, err := conn.Write([]byte(request))
	require.NoError(t, err)
	_, err = io.ReadAll(conn)
	require.NoError(t, err)
	require.Zero(t, calls.Load())
}

func newLifecycleMetricServer(
	t *testing.T,
	handlerFunc func(context.Context, *icap.Request) (*icap.Response, error),
) (*prometheus.Registry, *ICAPServer) {
	return newLifecycleServer(t, func(rtr *router.Router, _ *metricsinternal.Collector) error {
		return rtr.HandleFunc("/scan", handlerFunc)
	})
}

func newLifecycleProcessorServer(
	t *testing.T,
	proc processor.Processor,
) (*prometheus.Registry, *ICAPServer) {
	return newLifecycleServer(t, func(rtr *router.Router, collector *metricsinternal.Collector) error {
		return rtr.Handle("/scan", internalhandler.NewReqmodHandlerForServer("edge", proc, collector, nil))
	})
}

func newLifecycleServer(
	t *testing.T,
	register func(*router.Router, *metricsinternal.Collector) error,
) (*prometheus.Registry, *ICAPServer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	require.NoError(t, err)
	cfg := &config.ServerConfig{
		Host: "127.0.0.1", Port: 0, ReadTimeout: time.Second, WriteTimeout: time.Second,
		IdleTimeout: time.Second, MaxConnections: 10, MaxBodySize: 1024,
	}
	srv, err := NewServer(cfg, NewConnectionPool(), nil)
	require.NoError(t, err)
	rtr := router.NewRouter()
	require.NoError(t, register(rtr, collector))
	srv.SetRouter(rtr)
	srv.SetMetrics(collector)
	srv.SetMetricsServerName("edge")
	require.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, srv.Stop(context.Background())) })
	return reg, srv
}

func pacedResponseHandler(t *testing.T, mode string) func(context.Context, *icap.Request) (*icap.Response, error) {
	t.Helper()
	return func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "paced")
		plan, err := icap.PlanBodyStream(icap.BodyStreamPlanOptions{
			FinishMode: mode, SourceSize: 2, SelectedBytes: 2, SelectedBytesSet: true,
			Duration: shortLifecyclePacing,
		})
		if err != nil {
			return nil, err
		}
		resp := icap.NewResponse(icap.StatusOK)
		resp.SetHTTPResponse(&icap.HTTPMessage{
			Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader(),
			BodyStream: &icap.BodyStream{Payload: icap.NewBytesStreamPayload([]byte("ok")), Plan: plan},
		})
		if mode == icap.StreamFinishFIN {
			resp.MarkCloseAfterWrite()
		}
		return resp, nil
	}
}

func dialLifecycleServer(t *testing.T, srv *ICAPServer) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	return conn
}

func writeLifecycleOPTIONS(t *testing.T, conn net.Conn, addr string, closeConnection bool) {
	t.Helper()
	connectionHeader := ""
	if closeConnection {
		connectionHeader = "Connection: close\r\n"
	}
	_, err := fmt.Fprintf(conn, "OPTIONS icap://%s/scan ICAP/1.0\r\nHost: localhost\r\n%s\r\n", addr, connectionHeader)
	require.NoError(t, err)
}

func lifecycleLabels(outcome, response string) map[string]string {
	return map[string]string{
		"server": "edge", "method": "OPTIONS", "content_type": "none",
		"outcome": outcome, "response": response, "scenario": "scan",
	}
}

func lifecycleREQMODLabels(outcome, response string) map[string]string {
	labels := lifecycleLabels(outcome, response)
	labels["method"] = icap.MethodREQMOD
	return labels
}

func gatheredHistogramSum(t *testing.T, reg prometheus.Gatherer, labels map[string]string) float64 {
	t.Helper()
	for _, metric := range metricFamily(t, reg, "icap_scenario_response_duration_seconds").GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetHistogram().GetSampleSum()
		}
	}
	return 0
}
