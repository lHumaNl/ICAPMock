// Copyright 2026 ICAP Mock

package metrics

import (
	"strings"
	"sync"
)

const (
	// maxScenarioLatencySeries includes the reserved overflow aggregate series.
	maxScenarioLatencySeries    = 1024
	unknownMetricLabel          = "unknown"
	defaultServerMetricLabel    = "default"
	managementServerMetricLabel = "management"
	fallbackScenarioMetricLabel = "fallback"
	overflowMetricLabel         = "__overflow__"
	userMetricLabelEscapePrefix = "__user_label__"
)

type scenarioMetricKey struct {
	server   string
	scenario string
	response string
	block    string
}

type scenarioLabelLimiter struct {
	admitted  map[scenarioMetricKey]struct{}
	maxSeries int
	mu        sync.Mutex
}

func newScenarioLabelLimiter(maxSeries int) *scenarioLabelLimiter {
	return &scenarioLabelLimiter{
		admitted:  make(map[scenarioMetricKey]struct{}),
		maxSeries: maxSeries,
	}
}

func (l *scenarioLabelLimiter) admit(key scenarioMetricKey) scenarioMetricKey {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.admitted[key]; ok {
		return key
	}
	if l.shouldUseOverflowLocked(key) {
		key = overflowScenarioMetricKey()
	}
	l.admitted[key] = struct{}{}
	return key
}

func (l *scenarioLabelLimiter) shouldUseOverflowLocked(key scenarioMetricKey) bool {
	if key == overflowScenarioMetricKey() {
		return false
	}
	if l.maxSeries <= 1 {
		return true
	}
	if _, ok := l.admitted[overflowScenarioMetricKey()]; ok {
		return len(l.admitted) >= l.maxSeries
	}
	return len(l.admitted) >= l.maxSeries-1
}

func overflowScenarioMetricKey() scenarioMetricKey {
	return scenarioMetricKey{overflowMetricLabel, overflowMetricLabel, overflowMetricLabel, overflowMetricLabel}
}

func normalizedMetricLabel(value string) string {
	if value == "" {
		return unknownMetricLabel
	}
	if shouldEscapeUserMetricLabel(value) {
		return userMetricLabelEscapePrefix + value
	}
	return value
}

func shouldEscapeUserMetricLabel(value string) bool {
	return value == overflowMetricLabel || strings.HasPrefix(value, userMetricLabelEscapePrefix)
}
