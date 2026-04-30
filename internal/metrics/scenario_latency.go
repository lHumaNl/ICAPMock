// Copyright 2026 ICAP Mock

package metrics

import (
	"math"
	"sort"
	"strings"
	"sync"
)

const (
	// maxScenarioLatencySeries includes the reserved overflow aggregate series.
	maxScenarioLatencySeries      = 1024
	scenarioLatencyWindowCapacity = 1024
	unknownMetricLabel            = "unknown"
	defaultServerMetricLabel      = "default"
	managementServerMetricLabel   = "management"
	fallbackScenarioMetricLabel   = "fallback"
	overflowMetricLabel           = "__overflow__"
	userMetricLabelEscapePrefix   = "__user_label__"
)

var scenarioLatencyStatNames = []string{"min", "max", "avg", "p50", "p75", "p90", "p92", "p95"}

type scenarioLatencyKey struct {
	server   string
	scenario string
	response string
}

type scenarioLatencyStat struct {
	name  string
	value float64
}

type scenarioLatencyObservation struct {
	server   string
	scenario string
	response string
	stats    []scenarioLatencyStat
}

type scenarioLatencyWindows struct {
	windows   map[scenarioLatencyKey]*rollingLatencyWindow
	capacity  int
	maxSeries int
	mu        sync.Mutex
}

type rollingLatencyWindow struct {
	samples []float64
	next    int
}

func newScenarioLatencyWindows(capacity, maxSeries int) *scenarioLatencyWindows {
	return &scenarioLatencyWindows{
		windows:   make(map[scenarioLatencyKey]*rollingLatencyWindow),
		capacity:  capacity,
		maxSeries: maxSeries,
	}
}

func (w *scenarioLatencyWindows) observe(
	server, scenario, response string,
	value float64,
) scenarioLatencyObservation {
	if value < 0 {
		value = 0
	}
	key := scenarioLatencyKey{server: server, scenario: scenario, response: response}
	w.mu.Lock()
	window, key := w.windowForLocked(key)
	window.add(value, w.capacity)
	values := window.values()
	w.mu.Unlock()
	stats := calculateScenarioLatencyStats(values)
	return scenarioLatencyObservation{key.server, key.scenario, key.response, stats}
}

func (w *scenarioLatencyWindows) windowForLocked(
	key scenarioLatencyKey,
) (*rollingLatencyWindow, scenarioLatencyKey) {
	if window, ok := w.windows[key]; ok {
		return window, key
	}
	if w.shouldUseOverflowLocked(key) {
		key = overflowScenarioLatencyKey()
		if window, ok := w.windows[key]; ok {
			return window, key
		}
	}
	window := &rollingLatencyWindow{samples: make([]float64, 0, w.capacity)}
	w.windows[key] = window
	return window, key
}

func (w *scenarioLatencyWindows) shouldUseOverflowLocked(key scenarioLatencyKey) bool {
	if key == overflowScenarioLatencyKey() {
		return false
	}
	if w.maxSeries <= 1 {
		return true
	}
	if _, ok := w.windows[overflowScenarioLatencyKey()]; ok {
		return len(w.windows) >= w.maxSeries
	}
	return len(w.windows) >= w.maxSeries-1
}

func overflowScenarioLatencyKey() scenarioLatencyKey {
	return scenarioLatencyKey{server: overflowMetricLabel, scenario: overflowMetricLabel, response: overflowMetricLabel}
}

func (w *rollingLatencyWindow) add(value float64, capacity int) {
	if len(w.samples) < capacity {
		w.samples = append(w.samples, value)
		return
	}
	w.samples[w.next] = value
	w.next = (w.next + 1) % capacity
}

func (w *rollingLatencyWindow) values() []float64 {
	values := make([]float64, len(w.samples))
	copy(values, w.samples)
	return values
}

func calculateScenarioLatencyStats(values []float64) []scenarioLatencyStat {
	if len(values) == 0 {
		return nil
	}
	sort.Float64s(values)
	stats := make([]scenarioLatencyStat, 0, len(scenarioLatencyStatNames))
	stats = append(stats,
		scenarioLatencyStat{name: "min", value: values[0]},
		scenarioLatencyStat{name: "max", value: values[len(values)-1]},
		scenarioLatencyStat{name: "avg", value: average(values)},
	)
	return append(stats, percentileStats(values)...)
}

func percentileStats(values []float64) []scenarioLatencyStat {
	percentiles := []struct {
		name string
		p    float64
	}{{"p50", 0.50}, {"p75", 0.75}, {"p90", 0.90}, {"p92", 0.92}, {"p95", 0.95}}
	stats := make([]scenarioLatencyStat, 0, len(percentiles))
	for _, percentile := range percentiles {
		stats = append(stats, scenarioLatencyStat{percentile.name, nearestRank(values, percentile.p)})
	}
	return stats
}

func average(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func nearestRank(values []float64, percentile float64) float64 {
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	if index < 0 {
		return values[0]
	}
	if index >= len(values) {
		return values[len(values)-1]
	}
	return values[index]
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
