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
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/handler"
	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/processor"
	"github.com/icap-mock/icap-mock/internal/requestinfo"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

func TestFINAccountingOccursAfterLocalClose(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "stream")
		return directStreamResponse(t, icap.StreamFinishFIN, nil), nil
	})
	labels := lifecycleLabels(metricsinternal.OutcomeBlocked, "stream")
	countAtClose := uint64(2)
	netConn := newObservedLifecycleConn()
	netConn.onClose = func() {
		countAtClose = gatheredHistogramCount(t, reg, labels)
	}

	runDirectLifecycleRequest(srv, netConn)

	if countAtClose != 0 {
		t.Fatalf("latency count at local close = %d, want 0", countAtClose)
	}
	if got := gatheredHistogramCount(t, reg, labels); got != 1 {
		t.Fatalf("latency count after local close = %d, want 1", got)
	}
}

func TestLifecycleInFlightIncludesOrdinaryCustomHandlerDelivery(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	netConn := newObservedLifecycleConn()
	var once sync.Once
	lifecycleAtWrite := float64(-1)
	processingAtWrite := float64(-1)
	netConn.onWrite = func() {
		once.Do(func() {
			lifecycleAtWrite = gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
				"server": "edge", "method": "OPTIONS",
			})
			processingAtWrite = gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
				"server": "edge", "method": "OPTIONS",
			})
		})
	}

	runDirectLifecycleRequest(srv, netConn)

	if lifecycleAtWrite != 1 {
		t.Fatalf("lifecycle in-flight during ordinary write = %v, want 1", lifecycleAtWrite)
	}
	if processingAtWrite != 0 {
		t.Fatalf("processing in-flight during custom handler write = %v, want 0", processingAtWrite)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Fatalf("lifecycle in-flight after ordinary delivery = %v, want 0", got)
	}
}

func TestLifecycleInFlightIncludesPatternCustomHandlerDelivery(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	rtr := router.NewRouter()
	patternHandler := handler.WrapHandler(handler.Func(
		func(context.Context, *icap.Request) (*icap.Response, error) {
			return icap.NewResponse(icap.StatusNoContentNeeded), nil
		},
	), icap.MethodOPTIONS)
	if err := rtr.HandlePattern(regexp.MustCompile(`^/scan$`), patternHandler); err != nil {
		t.Fatalf("HandlePattern() error = %v", err)
	}
	srv.SetRouter(rtr)
	netConn := newObservedLifecycleConn()
	lifecycleAtWrite := float64(-1)
	var once sync.Once
	netConn.onWrite = func() {
		once.Do(func() {
			lifecycleAtWrite = gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
				"server": "edge", "method": "OPTIONS",
			})
		})
	}

	runDirectLifecycleRequest(srv, netConn)

	if lifecycleAtWrite != 1 {
		t.Fatalf("pattern lifecycle in-flight during delivery = %v, want 1", lifecycleAtWrite)
	}
	assertAllLifecycleGaugeSeriesZero(t, reg)
}

func TestLifecycleInFlightIncludesRoutedFailureResponses(t *testing.T) {
	tests := []struct {
		configure func(*ICAPServer, *observedLifecycleConn)
		name      string
	}{
		{
			name: "route not found",
			configure: func(_ *ICAPServer, conn *observedLifecycleConn) {
				conn.reader = bytes.NewReader([]byte(
					"OPTIONS icap://localhost/missing ICAP/1.0\r\nHost: localhost\r\nConnection: close\r\n\r\n",
				))
			},
		},
		{
			name: "no router",
			configure: func(srv *ICAPServer, _ *observedLifecycleConn) {
				srv.SetRouter(nil)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
				return icap.NewResponse(icap.StatusNoContentNeeded), nil
			})
			netConn := newObservedLifecycleConn()
			test.configure(srv, netConn)
			lifecycleAtWrite := float64(-1)
			var once sync.Once
			netConn.onWrite = func() {
				once.Do(func() {
					lifecycleAtWrite = gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
						"server": "edge", "method": "OPTIONS",
					})
				})
			}

			runDirectLifecycleRequest(srv, netConn)

			if lifecycleAtWrite != 1 {
				t.Fatalf("lifecycle in-flight during routed failure write = %v, want 1", lifecycleAtWrite)
			}
			assertAllLifecycleGaugeSeriesZero(t, reg)
		})
	}
}

func TestMalformedRequestDoesNotActivateInFlightGauges(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		t.Fatal("handler called for malformed request")
		return nil, nil
	})
	netConn := newObservedLifecycleConn()
	netConn.reader = bytes.NewReader([]byte("not an ICAP request\r\n\r\n"))

	runDirectLifecycleRequest(srv, netConn)

	if got := gatheredGaugeTotal(t, reg, "icap_requests_in_flight"); got != 0 {
		t.Fatalf("lifecycle in-flight after malformed request = %v, want 0", got)
	}
	if got := gatheredGaugeTotal(t, reg, "icap_requests_processing_in_flight"); got != 0 {
		t.Fatalf("processing in-flight after malformed request = %v, want 0", got)
	}
}

func TestLifecycleAndProcessingInFlightOverlapWhileProcessorIsBlocked(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	collector, _ := srv.metricsSnapshot()
	started := make(chan struct{})
	release := make(chan struct{})
	proc := processor.Func(func(context.Context, *icap.Request) (*icap.Response, error) {
		close(started)
		<-release
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	})
	reqmodHandler := handler.NewReqmodHandlerForServer("edge", proc, collector, nil)
	rtr := router.NewRouter()
	if err := rtr.Handle("/scan", reqmodHandler); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	srv.SetRouter(rtr)
	netConn := newObservedLifecycleConn()
	netConn.reader = bytes.NewReader([]byte(
		"REQMOD icap://localhost/scan ICAP/1.0\r\n" +
			"Host: localhost\r\nConnection: close\r\nEncapsulated: null-body=0\r\n\r\n",
	))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		runDirectLifecycleRequestContext(ctx, srv, netConn)
		close(done)
	}()
	<-started

	labels := map[string]string{"server": "edge", "method": "REQMOD"}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", labels); got != 1 {
		t.Fatalf("lifecycle in-flight while processor is blocked = %v, want 1", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", labels); got != 1 {
		t.Fatalf("processing in-flight while processor is blocked = %v, want 1", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
		t.Fatalf("active streams while processor is blocked = %v, want 0", got)
	}
	cancel()
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", labels); got != 1 {
		t.Fatalf("lifecycle in-flight after cancellation signal = %v, want 1 until unwind", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", labels); got != 1 {
		t.Fatalf("processing in-flight after cancellation signal = %v, want 1 until unwind", got)
	}

	close(release)
	<-done
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", labels); got != 0 {
		t.Fatalf("lifecycle in-flight after request = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", labels); got != 0 {
		t.Fatalf("processing in-flight after request = %v, want 0", got)
	}
}

func TestFINInFlightGaugesRemainActiveUntilLocalCloseReturns(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "stream")
		return directStreamResponse(t, icap.StreamFinishFIN, nil), nil
	})
	netConn := newObservedLifecycleConn()
	netConn.closeEntered = make(chan struct{})
	netConn.closeRelease = make(chan struct{})
	done := make(chan struct{})
	go func() {
		runDirectLifecycleRequest(srv, netConn)
		close(done)
	}()
	<-netConn.closeEntered

	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 1 {
		t.Fatalf("lifecycle in-flight while close is blocked = %v, want 1", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Fatalf("processing in-flight while close is blocked = %v, want 0", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 1 {
		t.Fatalf("active streams while close is blocked = %v, want 1", got)
	}

	close(netConn.closeRelease)
	<-done
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Fatalf("lifecycle in-flight after close = %v, want 0", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
		t.Fatalf("active streams after close = %v, want 0", got)
	}
}

func TestStreamingFailuresFinalizeActiveGaugeAndMetrics(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*observedLifecycleConn)
		stream    func(*testing.T) *icap.Response
	}{
		{
			name: "source",
			stream: func(t *testing.T) *icap.Response {
				return directStreamResponse(t, icap.StreamFinishComplete, errorReader{})
			},
		},
		{
			name: "flush",
			configure: func(conn *observedLifecycleConn) {
				conn.writeErr = errors.New("flush failed")
			},
			stream: func(t *testing.T) *icap.Response {
				return directStreamResponse(t, icap.StreamFinishComplete, nil)
			},
		},
		{
			name: "panic",
			stream: func(t *testing.T) *icap.Response {
				resp := directStreamResponse(t, icap.StreamFinishComplete, nil)
				resp.HTTPResponse.BodyStream.Sleeper = func(context.Context, time.Duration) error {
					panic("stream sleeper panic")
				}
				return resp
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reg, srv := newDirectLifecycleServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
				requestinfo.SetScenarioMetadata(ctx, "scan", "stream")
				return test.stream(t), nil
			})
			netConn := newObservedLifecycleConn()
			if test.configure != nil {
				test.configure(netConn)
			}

			runDirectLifecycleRequest(srv, netConn)

			labels := lifecycleLabels(metricsinternal.OutcomeError, "stream")
			if got := gatheredCounterValue(t, reg, requestsTotalMetricName, labels); got != 1 {
				t.Errorf("error requests = %v, want 1", got)
			}
			if got := gatheredHistogramCount(t, reg, labels); got != 1 {
				t.Errorf("error latency samples = %d, want 1", got)
			}
			if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
				t.Errorf("active streams = %v, want 0", got)
			}
		})
	}
}

func TestStreamingCancellationFinalizesActiveGauge(t *testing.T) {
	started := make(chan struct{})
	reg, srv := newDirectLifecycleServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "stream")
		resp := directStreamResponse(t, icap.StreamFinishComplete, nil)
		resp.HTTPResponse.BodyStream.Sleeper = func(ctx context.Context, _ time.Duration) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return resp, nil
	})
	netConn := newObservedLifecycleConn()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runDirectLifecycleRequestContext(ctx, srv, netConn)
		close(done)
	}()
	<-started
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 1 {
		t.Fatalf("active streams during cancellation test = %v, want 1", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 1 {
		t.Fatalf("lifecycle in-flight during streaming = %v, want 1", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Fatalf("processing in-flight during streaming = %v, want 0", got)
	}
	cancel()
	<-done

	labels := lifecycleLabels(metricsinternal.OutcomeError, "stream")
	if got := gatheredHistogramCount(t, reg, labels); got != 1 {
		t.Errorf("canceled latency samples = %d, want 1", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
		t.Errorf("active streams after cancellation = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "OPTIONS",
	}); got != 0 {
		t.Errorf("lifecycle in-flight after cancellation = %v, want 0", got)
	}
}

func TestWriteDeadlineSetupFailureDoesNotActivateStream(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(ctx context.Context, _ *icap.Request) (*icap.Response, error) {
		requestinfo.SetScenarioMetadata(ctx, "scan", "stream")
		return directStreamResponse(t, icap.StreamFinishComplete, nil), nil
	})
	netConn := newObservedLifecycleConn()
	netConn.writeDeadlineErr = errors.New("deadline setup failed")

	runDirectLifecycleRequest(srv, netConn)

	labels := lifecycleLabels(metricsinternal.OutcomeError, "stream")
	if got := gatheredHistogramCount(t, reg, labels); got != 1 {
		t.Errorf("setup failure latency samples = %d, want 1", got)
	}
	if got := gatheredGaugeValue(t, reg, map[string]string{"server": "edge"}); got != 0 {
		t.Errorf("active streams = %v, want 0", got)
	}
}

func TestBodyReceiveFailureDoesNotRecordLatency(t *testing.T) {
	reg, srv := newDirectLifecycleServer(t, func(context.Context, *icap.Request) (*icap.Response, error) {
		t.Fatal("handler called after body receive failure")
		return nil, nil
	})
	httpRequest := "POST / HTTP/1.1\r\nHost: origin.example\r\n\r\n"
	raw := "REQMOD icap://localhost/scan ICAP/1.0\r\nHost: localhost\r\nEncapsulated: req-hdr=0, req-body=" +
		fmt.Sprint(len(httpRequest)) + "\r\n\r\n" + httpRequest + "5\r\nhel"
	netConn := newObservedLifecycleConn()
	netConn.reader = bytes.NewReader([]byte(raw))

	runDirectLifecycleRequest(srv, netConn)

	if got := gatheredHistogramTotalCount(t, reg, "icap_scenario_response_duration_seconds"); got != 0 {
		t.Fatalf("latency samples after body receive failure = %d, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Fatalf("lifecycle in-flight after body receive failure = %v, want 0", got)
	}
	if got := gatheredNamedGaugeValue(t, reg, "icap_requests_processing_in_flight", map[string]string{
		"server": "edge", "method": "REQMOD",
	}); got != 0 {
		t.Fatalf("processing in-flight after body receive failure = %v, want 0", got)
	}
}

func TestConcurrentTerminalAccountingIsPerServerAndRaceSafe(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	const streamsPerServer = 20
	accounting := make([]*terminalAccounting, 0, streamsPerServer*2)
	for _, serverName := range []string{"edge-a", "edge-b"} {
		srv := &ICAPServer{metrics: collector, metricsServerName: serverName}
		for range streamsPerServer {
			accounting = append(accounting, newTerminalAccounting(
				context.Background(), srv, &icap.Request{Method: icap.MethodOPTIONS}, time.Now(),
			))
		}
	}
	var wg sync.WaitGroup
	for _, item := range accounting {
		wg.Add(1)
		go func(account *terminalAccounting) {
			defer wg.Done()
			account.streamingStarted()
		}(item)
	}
	wg.Wait()
	for _, serverName := range []string{"edge-a", "edge-b"} {
		if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
			"server": serverName, "method": "OPTIONS",
		}); got != streamsPerServer {
			t.Fatalf("%s lifecycle in-flight = %v, want %d", serverName, got, streamsPerServer)
		}
		if got := gatheredGaugeValue(t, reg, map[string]string{"server": serverName}); got != streamsPerServer {
			t.Fatalf("%s active streams = %v, want %d", serverName, got, streamsPerServer)
		}
	}
	for _, item := range accounting {
		wg.Add(1)
		go func(account *terminalAccounting) {
			defer wg.Done()
			account.finalize(icap.NewResponse(icap.StatusNoContentNeeded), "")
		}(item)
	}
	wg.Wait()
	for _, serverName := range []string{"edge-a", "edge-b"} {
		if got := gatheredNamedGaugeValue(t, reg, "icap_requests_in_flight", map[string]string{
			"server": serverName, "method": "OPTIONS",
		}); got != 0 {
			t.Fatalf("%s lifecycle in-flight after finalization = %v, want 0", serverName, got)
		}
		if got := gatheredGaugeValue(t, reg, map[string]string{"server": serverName}); got != 0 {
			t.Fatalf("%s active streams = %v, want 0", serverName, got)
		}
	}
}

func newDirectLifecycleServer(
	t *testing.T,
	handlerFunc func(context.Context, *icap.Request) (*icap.Response, error),
) (*prometheus.Registry, *ICAPServer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	collector, err := metricsinternal.NewCollector(reg)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	srv, err := NewServer(&config.ServerConfig{
		ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second,
		MaxConnections: 1, MaxBodySize: 1024,
	}, NewConnectionPool(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	rtr := router.NewRouter()
	if err := rtr.HandleFunc("/scan", handlerFunc); err != nil {
		t.Fatalf("HandleFunc() error = %v", err)
	}
	srv.SetRouter(rtr)
	srv.SetMetrics(collector)
	srv.SetMetricsServerName("edge")
	return reg, srv
}

func directStreamResponse(t *testing.T, mode string, reader io.Reader) *icap.Response {
	t.Helper()
	plan, err := icap.PlanBodyStream(icap.BodyStreamPlanOptions{
		FinishMode: mode, SourceSize: 1, SelectedBytes: 1, SelectedBytesSet: true,
		Duration: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("PlanBodyStream() error = %v", err)
	}
	stream := &icap.BodyStream{Plan: plan}
	if reader == nil {
		stream.Payload = icap.NewBytesStreamPayload([]byte("x"))
	} else {
		stream.Reader = reader
	}
	resp := icap.NewResponse(icap.StatusOK)
	resp.SetHTTPResponse(&icap.HTTPMessage{
		Proto: "HTTP/1.1", Status: "200", StatusText: "OK", Header: icap.NewHeader(), BodyStream: stream,
	})
	if mode == icap.StreamFinishFIN {
		resp.MarkCloseAfterWrite()
	}
	return resp
}

func runDirectLifecycleRequest(srv *ICAPServer, netConn net.Conn) {
	runDirectLifecycleRequestContext(context.Background(), srv, netConn)
}

func runDirectLifecycleRequestContext(ctx context.Context, srv *ICAPServer, netConn net.Conn) {
	conn := newConnection(netConn, &ConnectionConfig{
		ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxBodySize: 1024,
	})
	srv.pool.Add(conn)
	srv.metrics.IncActiveConnectionsForServer(srv.metricsServerName)
	srv.semaphore <- struct{}{}
	srv.wg.Add(1)
	srv.handleConnection(ctx, conn)
}

type observedLifecycleConn struct {
	reader           *bytes.Reader
	onWrite          func()
	onClose          func()
	closeEntered     chan struct{}
	closeRelease     chan struct{}
	writeErr         error
	writeDeadlineErr error
	mu               sync.Mutex
	closed           bool
}

func newObservedLifecycleConn() *observedLifecycleConn {
	raw := "OPTIONS icap://localhost/scan ICAP/1.0\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	return &observedLifecycleConn{reader: bytes.NewReader([]byte(raw))}
}

func gatheredHistogramTotalCount(t *testing.T, reg prometheus.Gatherer, name string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	var count uint64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			count += metric.GetHistogram().GetSampleCount()
		}
	}
	return count
}

func gatheredGaugeTotal(t *testing.T, reg prometheus.Gatherer, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	var total float64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			total += metric.GetGauge().GetValue()
		}
	}
	return total
}

func (c *observedLifecycleConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *observedLifecycleConn) Write(p []byte) (int, error) {
	if c.onWrite != nil {
		c.onWrite()
	}
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}
func (c *observedLifecycleConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	onClose := c.onClose
	c.mu.Unlock()
	if onClose != nil {
		onClose()
	}
	if c.closeEntered != nil {
		close(c.closeEntered)
	}
	if c.closeRelease != nil {
		<-c.closeRelease
	}
	return nil
}
func (*observedLifecycleConn) LocalAddr() net.Addr                { return staticAddr("127.0.0.1:1344") }
func (*observedLifecycleConn) RemoteAddr() net.Addr               { return staticAddr("192.0.2.10:44354") }
func (*observedLifecycleConn) SetDeadline(time.Time) error        { return nil }
func (*observedLifecycleConn) SetReadDeadline(time.Time) error    { return nil }
func (c *observedLifecycleConn) SetWriteDeadline(time.Time) error { return c.writeDeadlineErr }

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("source failed") }
