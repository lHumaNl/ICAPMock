// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// MockProcessor processes requests by matching them against scenarios
// defined in the ScenarioRegistry. It returns responses based on the
// matched scenario's response template.
//
// MockProcessor supports:
//   - Scenario matching by ICAP method, URI path, headers, and body
//   - Configurable response delays for latency simulation
//   - Custom ICAP status codes and headers
//   - HTTP response modification (status, headers, body)
//
// MockProcessor is thread-safe and can be used concurrently.
type MockProcessor struct {
	registry          storage.ScenarioRegistry
	logger            *logger.Logger
	metrics           *metrics.Collector
	server            string
	maxStreamBodySize int64
}

// NewMockProcessor creates a new MockProcessor with the given registry and logger.
//
// Parameters:
//   - registry: The scenario registry for matching requests
//   - log: The logger for recording matched scenarios (can be nil for silent operation)
//
// The processor uses the registry to find matching scenarios and returns
// responses based on the scenario's response template.
func NewMockProcessor(registry storage.ScenarioRegistry, log *logger.Logger) *MockProcessor {
	return NewMockProcessorWithMaxBodySize(registry, log, defaultStreamBodyLimit)
}

// SetMetrics sets the Prometheus metrics collector for scenario-level metrics.
func (p *MockProcessor) SetMetrics(collector *metrics.Collector) {
	p.metrics = collector
	if p.server == "" {
		p.server = "default"
	}
}

// SetMetricsForServer sets the collector and server label for scenario metrics.
func (p *MockProcessor) SetMetricsForServer(collector *metrics.Collector, server string) {
	p.metrics = collector
	p.server = server
}

// NewMockProcessorWithMaxBodySize creates a processor with an explicit stream body limit.
func NewMockProcessorWithMaxBodySize(
	registry storage.ScenarioRegistry,
	log *logger.Logger,
	maxBodySize int64,
) *MockProcessor {
	return &MockProcessor{
		registry:          registry,
		logger:            log,
		server:            "default",
		maxStreamBodySize: maxBodySize,
	}
}

// Process handles the ICAP request by matching it against registered scenarios.
//
// The matching process:
//  1. Finds the first scenario that matches the request (highest priority first)
//  2. Applies any configured delay from the scenario
//  3. Builds and returns the response from the scenario's response template
//
// If no scenario matches, it returns an ErrNoMatch error.
// If the scenario specifies an error, it returns that error.
func (p *MockProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) { //nolint:gocyclo // request processing: match, select response, apply delay, build response
	start := time.Now()

	// Check context before processing
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Find matching scenario
	scenario, err := p.registry.Match(req)
	if err != nil {
		if errors.Is(err, storage.ErrNoMatch) {
			return nil, apperrors.ErrScenarioNotFound
		}
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			"failed to match scenario",
			apperrors.ErrInternalServerError.ICAPStatus,
			err,
		)
	}

	// Log matched scenario
	if p.logger != nil {
		p.logger.Debug("scenario matched",
			"request_id", util.RequestIDFromContext(ctx),
			"scenario", scenario.Name,
			"method", req.Method,
			"uri", req.URI,
		)
	}

	// Determine the response source: matched branch (if the scenario uses
	// branches) or the scenario-level response.
	baseResp := &scenario.Response
	weighted := scenario.WeightedResponses
	if len(scenario.Branches) > 0 {
		idx := scenario.SelectBranch(req)
		if idx < 0 {
			// Matcher should have rejected the scenario; defensive guard.
			return nil, apperrors.ErrScenarioNotFound
		}
		b := &scenario.Branches[idx]
		baseResp = &b.Response
		weighted = b.WeightedResponses
	}

	selectedResponse := baseResp
	var selectedDelay *storage.DelayConfig
	if len(weighted) > 0 {
		wr := selectWeightedResponse(weighted)
		merged := *baseResp // copy base
		if wr.ICAPStatus != 0 {
			merged.ICAPStatus = wr.ICAPStatus
		}
		if wr.HTTPStatus != 0 {
			merged.HTTPStatus = wr.HTTPStatus
		}
		if wr.Body != "" {
			merged.Body = wr.Body
		}
		if wr.BodyFile != "" {
			merged.BodyFile = wr.BodyFile
		}
		if wr.HTTPBody != "" {
			merged.HTTPBody = wr.HTTPBody
		}
		if wr.HTTPBodyFile != "" {
			merged.HTTPBodyFile = wr.HTTPBodyFile
		}
		if wr.Error != "" {
			merged.Error = wr.Error
		}
		if len(wr.Headers) > 0 {
			merged.Headers = wr.Headers
		}
		if len(wr.HTTPHeaders) > 0 {
			merged.HTTPHeaders = wr.HTTPHeaders
		}
		if wr.Stream != nil {
			merged.Stream = wr.Stream
		}
		if wr.Delay.Min > 0 || wr.Delay.Max > 0 {
			selectedDelay = &wr.Delay
		}
		if wr.ResponseName != "" {
			merged.ResponseName = wr.ResponseName
		}
		selectedResponse = &merged
	}

	// Substitute ${name} placeholders using captured path parameters.
	if len(req.Captures) > 0 {
		substituted := substituteCaptures(*selectedResponse, req.Captures)
		selectedResponse = &substituted
	}
	defer p.recordScenarioMetrics(scenario.Name, selectedResponse, start)

	// Apply delay
	var delay time.Duration
	switch {
	case selectedDelay != nil:
		delay = selectedDelay.Duration()
	case selectedResponse.DelayRange != nil:
		delay = selectedResponse.DelayRange.Duration()
	case selectedResponse.Delay > 0:
		delay = selectedResponse.Delay
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Handle the effective selected response error before building bodies/files.
	if selectedResponse.Error != "" {
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			selectedResponse.Error,
			apperrors.ErrInternalServerError.ICAPStatus,
			nil,
		)
	}

	// Build response
	effectiveScenario := *scenario
	effectiveScenario.Response = *selectedResponse
	resp, err := p.buildResponse(&effectiveScenario, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// buildResponse constructs an ICAP response from a scenario's response template.
//
//nolint:gocyclo // response building covers several orthogonal shapes (REQMOD/RESPMOD × modify/synthesize)
func (p *MockProcessor) buildResponse(scenario *storage.Scenario, req *icap.Request) (*icap.Response, error) {
	resp := icap.NewResponse(scenario.Response.ICAPStatus)

	// Set ICAP headers
	for key, value := range scenario.Response.Headers {
		resp.SetHeader(key, value)
	}

	// Set body if specified
	body, err := scenario.Response.GetBody()
	if err != nil {
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			"failed to get response body",
			apperrors.ErrInternalServerError.ICAPStatus,
			err,
		)
	}
	if body != "" {
		resp.Body = []byte(body)
	}

	// Load the wrapped HTTP body (if any) from http_body / http_body_file.
	httpBody, err := loadHTTPBody(&scenario.Response)
	if err != nil {
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			"failed to read http_body_file",
			apperrors.ErrInternalServerError.ICAPStatus,
			err,
		)
	}

	// Handle REQMOD with HTTP request modification
	if req.IsREQMOD() && req.HTTPRequest != nil {
		// Modify HTTP status if specified
		if scenario.Response.HTTPStatus > 0 {
			// For REQMOD, return an HTTP response instead of a modified request
			// This is typically used to return an error page or block the request
			httpResp := &icap.HTTPMessage{
				Status:     statusToString(scenario.Response.HTTPStatus),
				StatusText: http.StatusText(scenario.Response.HTTPStatus),
				Proto:      "HTTP/1.1",
				Header:     make(icap.Header),
			}

			// Add/modify HTTP headers on the response
			for key, value := range scenario.Response.HTTPHeaders {
				httpResp.Header.Set(key, value)
			}
			if len(httpBody) > 0 {
				httpResp.Body = httpBody
				setBodyContentLength(httpResp.Header, scenario.Response.HTTPHeaders, len(httpBody))
			}

			resp.SetHTTPResponse(httpResp)
		} else {
			httpReq, err := p.cloneHTTPMessageForResponse(req.HTTPRequest, scenario.Response.Stream != nil)
			if err != nil {
				return nil, cloneICAPError(err)
			}

			// Add/modify HTTP headers on the request
			for key, value := range scenario.Response.HTTPHeaders {
				httpReq.Header.Set(key, value)
			}
			if len(httpBody) > 0 {
				httpReq.Body = httpBody
				setBodyContentLength(httpReq.Header, scenario.Response.HTTPHeaders, len(httpBody))
			}

			resp.SetHTTPRequest(httpReq)
		}
	}

	// Handle RESPMOD with HTTP response modification
	if req.IsRESPMOD() && req.HTTPResponse != nil {
		httpResp, err := p.cloneHTTPMessageForResponse(req.HTTPResponse, scenario.Response.Stream != nil)
		if err != nil {
			return nil, cloneICAPError(err)
		}

		// Modify HTTP status if specified
		if scenario.Response.HTTPStatus > 0 {
			httpResp.Status = statusToString(scenario.Response.HTTPStatus)
			httpResp.StatusText = http.StatusText(scenario.Response.HTTPStatus)
		}

		// Add/modify HTTP headers
		for key, value := range scenario.Response.HTTPHeaders {
			httpResp.Header.Set(key, value)
		}
		if len(httpBody) > 0 {
			// Replace the original body with the synthesized one.
			httpResp.Body = httpBody
			setBodyContentLength(httpResp.Header, scenario.Response.HTTPHeaders, len(httpBody))
		}

		resp.SetHTTPResponse(httpResp)
	}

	if err := p.attachStream(resp, &scenario.Response, req); err != nil {
		return nil, err
	}

	return resp, nil
}

// loadHTTPBody returns the body bytes for the wrapped HTTP response.
// Precedence: HTTPBody (inline string) > HTTPBodyFile (file path).
// Returns (nil, nil) when neither is set.
func loadHTTPBody(resp *storage.ResponseTemplate) ([]byte, error) {
	if resp.HTTPBody != "" {
		return []byte(resp.HTTPBody), nil
	}
	if resp.HTTPBodyFile != "" {
		b, err := os.ReadFile(resp.HTTPBodyFile) //nolint:gosec // path comes from a loaded scenario file, not end-user input
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return nil, nil
}

// setBodyContentLength sets Content-Length on h to len(body) = n. It
// unconditionally overwrites any existing value (e.g. one carried over from
// cloning the original HTTP message) unless the user explicitly declared
// Content-Length in http_set — in which case that declared value wins, even
// if it disagrees with the body length.
//
// The sentinel "auto" in http_set is a way to say "always recompute".
func setBodyContentLength(h icap.Header, userHTTPSet map[string]string, n int) {
	if userValue, userSet := userHTTPSet["Content-Length"]; userSet && userValue != "auto" {
		return
	}
	h.Set("Content-Length", strconv.Itoa(n))
}

// cloneHTTPMessageForResponse creates a bounded copy of an HTTPMessage.
// Streaming responses only need HTTP metadata; attachStream installs the body.
func (p *MockProcessor) cloneHTTPMessageForResponse(msg *icap.HTTPMessage, stream bool) (*icap.HTTPMessage, error) {
	clone := cloneHTTPMessageMetadata(msg)
	if clone == nil || stream {
		return clone, nil
	}
	if err := p.cloneHTTPMessageBody(clone, msg); err != nil {
		return nil, err
	}
	return clone, nil
}

func cloneHTTPMessageMetadata(msg *icap.HTTPMessage) *icap.HTTPMessage {
	if msg == nil {
		return nil
	}
	clone := &icap.HTTPMessage{
		Method:     msg.Method,
		URI:        msg.URI,
		Status:     msg.Status,
		StatusText: msg.StatusText,
		Proto:      msg.Proto,
	}

	if msg.Header != nil {
		clone.Header = msg.Header.Clone()
	}
	return clone
}

func (p *MockProcessor) cloneHTTPMessageBody(clone, msg *icap.HTTPMessage) error {
	body, err := getHTTPMessageBodyForClone(msg, p.maxStreamBodySize)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	clone.SetLoadedBody(bodyCopy)
	return nil
}

func getHTTPMessageBodyForClone(msg *icap.HTTPMessage, maxBodySize int64) ([]byte, error) {
	if maxBodySize <= 0 {
		return msg.GetBody()
	}
	return msg.GetBodyLimited(maxBodySize)
}

func cloneICAPError(err error) error {
	return apperrors.NewICAPError(
		apperrors.ErrInternalServerError.Code,
		"failed to clone HTTP message body",
		apperrors.ErrInternalServerError.ICAPStatus,
		err,
	)
}

// Name returns "MockProcessor" as the processor name.
func (p *MockProcessor) Name() string {
	return "MockProcessor"
}

// SetLogger sets the logger for the processor.
// This can be used to update the logger after creation.
func (p *MockProcessor) SetLogger(log *logger.Logger) {
	if log != nil {
		p.logger = log
	}
}

// statusToString converts an HTTP status code to its string representation.
func statusToString(status int) string {
	return strconv.Itoa(status)
}

// capturePlaceholder matches ${name} substitution tokens. The escape "$${…}"
// (double dollar) is preserved by replacing "$${" with a placeholder before
// substitution and restoring it afterwards.
var capturePlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// substituteCaptures returns a copy of resp with all ${name} placeholders
// replaced using vars. Applies to Body, HTTPBody, Headers and HTTPHeaders
// values. Literal "$${" is preserved as "${".
func substituteCaptures(resp storage.ResponseTemplate, vars map[string]string) storage.ResponseTemplate {
	out := resp
	out.Body = substituteString(resp.Body, vars)
	out.BodyFile = substituteString(resp.BodyFile, vars)
	out.HTTPBody = substituteString(resp.HTTPBody, vars)
	out.HTTPBodyFile = substituteString(resp.HTTPBodyFile, vars)
	out.Error = substituteString(resp.Error, vars)
	if resp.Stream != nil {
		out.Stream = substituteStream(resp.Stream, vars)
	}
	if len(resp.Headers) > 0 {
		out.Headers = substituteMap(resp.Headers, vars)
	}
	if len(resp.HTTPHeaders) > 0 {
		out.HTTPHeaders = substituteMap(resp.HTTPHeaders, vars)
	}
	return out
}

func substituteStream(stream *storage.StreamConfig, vars map[string]string) *storage.StreamConfig {
	out := *stream
	out.Body = substituteString(stream.Body, vars)
	out.BodyFile = substituteString(stream.BodyFile, vars)
	out.Source.Body = substituteString(stream.Source.Body, vars)
	out.Source.BodyFile = substituteString(stream.Source.BodyFile, vars)
	out.Parts = substituteStreamParts(stream.Parts, vars)
	out.Fallback = substituteStreamFallback(stream.Fallback, vars)
	out.Multipart.Fields = substituteStringSlice(stream.Multipart.Fields, vars)
	out.Multipart.Files.Filename = substituteStringSlice(stream.Multipart.Files.Filename, vars)
	return &out
}

func substituteStreamParts(parts []storage.StreamPartConfig, vars map[string]string) []storage.StreamPartConfig {
	if len(parts) == 0 {
		return parts
	}
	out := make([]storage.StreamPartConfig, len(parts))
	for i, part := range parts {
		out[i] = part
		out[i].Body = substituteString(part.Body, vars)
		out[i].BodyFile = substituteString(part.BodyFile, vars)
	}
	return out
}

func substituteStreamFallback(fallback storage.StreamFallbackConfig, vars map[string]string) storage.StreamFallbackConfig {
	fallback.Body = substituteString(fallback.Body, vars)
	fallback.BodyFile = substituteString(fallback.BodyFile, vars)
	fallback.RawFile.Filename = substituteStringSlice(fallback.RawFile.Filename, vars)
	return fallback
}

func substituteStringSlice(in []string, vars map[string]string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	for i, value := range in {
		out[i] = substituteString(value, vars)
	}
	return out
}

func substituteMap(m, vars map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = substituteString(v, vars)
	}
	return out
}

// substituteString replaces ${name} with vars[name] (empty string if absent)
// and treats "$${" as an escape for a literal "${".
func substituteString(s string, vars map[string]string) string {
	if s == "" {
		return s
	}
	const escaped = "\x00ESC\x00"
	tmp := strings.ReplaceAll(s, "$${", escaped)
	tmp = capturePlaceholder.ReplaceAllStringFunc(tmp, func(match string) string {
		name := match[2 : len(match)-1]
		return vars[name]
	})
	return strings.ReplaceAll(tmp, escaped, "${")
}

func (p *MockProcessor) recordScenarioMetrics(
	scenario string,
	response *storage.ResponseTemplate,
	start time.Time,
) {
	if p.metrics == nil || response == nil {
		return
	}
	p.metrics.RecordScenarioRequestForServer(p.server, scenario, scenarioResponseLabel(response), time.Since(start))
}

func scenarioResponseLabel(response *storage.ResponseTemplate) string {
	if response.ResponseName != "" {
		return response.ResponseName
	}
	return strconv.Itoa(responseStatusCode(response))
}

func responseStatusCode(response *storage.ResponseTemplate) int {
	if response.ICAPStatus != 0 {
		return response.ICAPStatus
	}
	return response.Status
}

// selectWeightedResponse picks a response variant based on weights.
func selectWeightedResponse(responses []storage.WeightedResponse) *storage.WeightedResponse {
	totalWeight := 0
	for _, r := range responses {
		w := r.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}
	if totalWeight == 0 {
		return &responses[0]
	}
	n := rand.Intn(totalWeight) //nolint:gosec // crypto not needed here
	cumulative := 0
	for i := range responses {
		w := responses[i].Weight
		if w <= 0 {
			w = 1
		}
		cumulative += w
		if n < cumulative {
			return &responses[i]
		}
	}
	return &responses[len(responses)-1]
}
