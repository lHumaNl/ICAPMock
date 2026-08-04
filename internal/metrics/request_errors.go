// Copyright 2026 ICAP Mock

package metrics

const (
	// RequestErrorStageContext labels errors caused by request context cancellation.
	RequestErrorStageContext = "context"
	// RequestErrorStageBodyReceive labels failures while receiving request bodies.
	RequestErrorStageBodyReceive = "body_receive"
	// RequestErrorStageRouting labels routing failures before handler execution.
	RequestErrorStageRouting = "routing"
	// RequestErrorStageProcessorMatch labels scenario matching failures.
	RequestErrorStageProcessorMatch = "processor_match"
	// RequestErrorStageProcessorBuild labels response construction failures.
	RequestErrorStageProcessorBuild = "processor_build"
	// RequestErrorStageProcessorResponse labels configured scenario response errors.
	RequestErrorStageProcessorResponse = "processor_response"
	// RequestErrorStageWriteResponse labels failures while delivering the ICAP response.
	RequestErrorStageWriteResponse = "write_response"

	// RequestErrorTypeContextCanceled labels context.Canceled errors.
	RequestErrorTypeContextCanceled = "context_canceled"
	// RequestErrorTypeDeadlineExceeded labels context deadline errors.
	RequestErrorTypeDeadlineExceeded = "deadline_exceeded"
	// RequestErrorTypeBodyReceiveFailed labels request body receive failures.
	RequestErrorTypeBodyReceiveFailed = "body_receive_failed"
	// RequestErrorTypeBodyTooLarge labels body size limit failures.
	RequestErrorTypeBodyTooLarge = "body_too_large"
	// RequestErrorTypeRouteNotFound labels unmatched ICAP routes.
	RequestErrorTypeRouteNotFound = "route_not_found"
	// RequestErrorTypeNoScenarioMatch labels requests that matched no scenario.
	RequestErrorTypeNoScenarioMatch = "no_scenario_match"
	// RequestErrorTypeScenarioMatchFailed labels scenario matcher failures.
	RequestErrorTypeScenarioMatchFailed = "scenario_match_failed"
	// RequestErrorTypeResponseBuildFailed labels response construction failures.
	RequestErrorTypeResponseBuildFailed = "response_build_failed"
	// RequestErrorTypeScenarioResponseError labels configured scenario errors.
	RequestErrorTypeScenarioResponseError = "scenario_response_error"
	// RequestErrorTypeNilProcessor labels missing processor configuration.
	RequestErrorTypeNilProcessor = "nil_processor"
	// RequestErrorTypeResponseWriteFailed labels response serialization or flush failures.
	RequestErrorTypeResponseWriteFailed = "response_write_failed"
)

var knownRequestErrorStages = map[string]struct{}{
	RequestErrorStageContext:           {},
	RequestErrorStageBodyReceive:       {},
	RequestErrorStageRouting:           {},
	RequestErrorStageProcessorMatch:    {},
	RequestErrorStageProcessorBuild:    {},
	RequestErrorStageProcessorResponse: {},
	RequestErrorStageWriteResponse:     {},
}

var knownRequestErrorTypes = map[string]struct{}{
	RequestErrorTypeContextCanceled:       {},
	RequestErrorTypeDeadlineExceeded:      {},
	RequestErrorTypeBodyReceiveFailed:     {},
	RequestErrorTypeBodyTooLarge:          {},
	RequestErrorTypeRouteNotFound:         {},
	RequestErrorTypeNoScenarioMatch:       {},
	RequestErrorTypeScenarioMatchFailed:   {},
	RequestErrorTypeResponseBuildFailed:   {},
	RequestErrorTypeScenarioResponseError: {},
	RequestErrorTypeNilProcessor:          {},
	RequestErrorTypeResponseWriteFailed:   {},
}

// RecordRequestError records a bounded ICAP request error metric.
func (c *Collector) RecordRequestError(method, stage, errorType, scenario, response string) {
	c.RecordRequestErrorForServer(defaultServerMetricLabel, method, stage, errorType, scenario, response)
}

// RecordRequestErrorForServer records a bounded ICAP request error metric by server.
func (c *Collector) RecordRequestErrorForServer(server, method, stage, errorType, scenario, response string) {
	labels := c.admitScenarioLabels(server, method, ContentTypeNone, OutcomeError, scenario, response)
	c.requestErrorsTotal.WithLabelValues(
		labels.server,
		labels.method,
		normalizeRequestErrorStage(stage),
		normalizeRequestErrorType(errorType),
		labels.scenario,
		labels.response,
	).Inc()
}

func normalizeRequestErrorStage(stage string) string {
	if _, ok := knownRequestErrorStages[stage]; ok {
		return stage
	}
	return unknownMetricLabel
}

func normalizeRequestErrorType(errorType string) string {
	if _, ok := knownRequestErrorTypes[errorType]; ok {
		return errorType
	}
	return unknownMetricLabel
}
