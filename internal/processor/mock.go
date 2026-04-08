// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"time"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/logger"
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
	registry storage.ScenarioRegistry
	logger   *logger.Logger
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
	return &MockProcessor{
		registry: registry,
		logger:   log,
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
func (p *MockProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
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
		p.logger.Info("scenario matched",
			"request_id", util.RequestIDFromContext(ctx),
			"scenario", scenario.Name,
			"method", req.Method,
			"uri", req.URI,
		)
	}

	// Select weighted response if available
	selectedResponse := &scenario.Response
	var selectedDelay *storage.DelayConfig
	if len(scenario.WeightedResponses) > 0 {
		wr := selectWeightedResponse(scenario.WeightedResponses)
		// Build a merged response template
		merged := scenario.Response // copy base
		if wr.ICAPStatus != 0 {
			merged.ICAPStatus = wr.ICAPStatus
		}
		if wr.HTTPStatus != 0 {
			merged.HTTPStatus = wr.HTTPStatus
		}
		if wr.Body != "" {
			merged.Body = wr.Body
		}
		if len(wr.Headers) > 0 {
			merged.Headers = wr.Headers
		}
		if wr.Delay.Min > 0 || wr.Delay.Max > 0 {
			selectedDelay = &wr.Delay
		}
		selectedResponse = &merged
	}

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

	// Build response
	effectiveScenario := *scenario
	effectiveScenario.Response = *selectedResponse
	resp, err := p.buildResponse(&effectiveScenario, req)
	if err != nil {
		return nil, err
	}

	// Handle scenario-defined error
	if scenario.Response.Error != "" {
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			scenario.Response.Error,
			apperrors.ErrInternalServerError.ICAPStatus,
			nil,
		)
	}

	return resp, nil
}

// buildResponse constructs an ICAP response from a scenario's response template.
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

	// Handle REQMOD with HTTP request modification
	if req.IsREQMOD() && req.HTTPRequest != nil {
		httpReq := p.cloneHTTPMessage(req.HTTPRequest)

		// Modify HTTP status if specified
		if scenario.Response.HTTPStatus > 0 {
			// For REQMOD, return an HTTP response instead of a modified request
			// This is typically used to return an error page or block the request
			httpResp := &icap.HTTPMessage{
				Status:     statusToString(scenario.Response.HTTPStatus),
				StatusText: icap.StatusText(scenario.Response.HTTPStatus),
				Proto:      "HTTP/1.1",
				Header:     make(icap.Header),
			}

			// Add/modify HTTP headers on the response
			for key, value := range scenario.Response.HTTPHeaders {
				httpResp.Header.Set(key, value)
			}

			resp.SetHTTPResponse(httpResp)
		} else {
			// Add/modify HTTP headers on the request
			for key, value := range scenario.Response.HTTPHeaders {
				httpReq.Header.Set(key, value)
			}

			resp.SetHTTPRequest(httpReq)
		}
	}

	// Handle RESPMOD with HTTP response modification
	if req.IsRESPMOD() && req.HTTPResponse != nil {
		httpResp := p.cloneHTTPMessage(req.HTTPResponse)

		// Modify HTTP status if specified
		if scenario.Response.HTTPStatus > 0 {
			httpResp.Status = statusToString(scenario.Response.HTTPStatus)
			httpResp.StatusText = icap.StatusText(scenario.Response.HTTPStatus)
		}

		// Add/modify HTTP headers
		for key, value := range scenario.Response.HTTPHeaders {
			httpResp.Header.Set(key, value)
		}

		resp.SetHTTPResponse(httpResp)
	}

	return resp, nil
}

// cloneHTTPMessage creates a deep copy of an HTTPMessage.
// It lazily loads the body only if it's available.
func (p *MockProcessor) cloneHTTPMessage(msg *icap.HTTPMessage) *icap.HTTPMessage {
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

	// Lazy load body for cloning
	if body, err := msg.GetBody(); err == nil && len(body) > 0 {
		// Create a copy of the body and mark it as loaded
		bodyCopy := make([]byte, len(body))
		copy(bodyCopy, body)
		clone.SetLoadedBody(bodyCopy)
	}

	return clone
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
