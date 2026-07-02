// Copyright 2026 ICAP Mock

package server

import (
	"strconv"

	metricsinternal "github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	incomingResultSuccess = "success"
	incomingResultError   = "error"
	missingStatusLabel    = "none"
)

func (s *ICAPServer) recordIncomingRequest(req *icap.Request, resp *icap.Response) {
	if s.metrics == nil || req == nil {
		return
	}
	status := icapStatusLabel(resp)
	s.metrics.RecordIncomingRequest(
		s.metricsServerName,
		req.Method,
		metricsinternal.NormalizeEndpointLabel(s.metricsEndpointLabelMode, req.URI),
		metricsinternal.ExtractExtension(req.URI),
		incomingResult(resp),
		status,
		isBlockedResponse(resp),
	)
}

func icapStatusLabel(resp *icap.Response) string {
	if resp == nil {
		return missingStatusLabel
	}
	return strconv.Itoa(resp.StatusCode)
}

func incomingResult(resp *icap.Response) string {
	if resp != nil && resp.StatusCode < icap.StatusBadRequest {
		return incomingResultSuccess
	}
	return incomingResultError
}

func isBlockedResponse(resp *icap.Response) bool {
	if resp == nil || resp.StatusCode >= icap.StatusBadRequest {
		return true
	}
	return encapsulatedHTTPStatusCode(resp) >= icap.StatusBadRequest
}

func encapsulatedHTTPStatusCode(resp *icap.Response) int {
	if resp == nil || resp.HTTPResponse == nil {
		return 0
	}
	statusCode, err := strconv.Atoi(resp.HTTPResponse.Status)
	if err != nil {
		return 0
	}
	return statusCode
}
