// Package metrics provides Prometheus metrics collection for the ICAP Mock Server.
package metrics

import (
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Sentinel errors for the metrics package.
var (
	// ErrNilRegistry is returned when a nil registry is passed to NewCollector.
	ErrNilRegistry = errors.New("registry cannot be nil")
)

// Handler returns an HTTP handler that serves Prometheus metrics using the
// default Prometheus registry. This is suitable for simple use cases where
// all metrics are registered with the default registry.
//
// The handler serves metrics in the Prometheus text exposition format.
//
// Example:
//
//	http.Handle("/metrics", metrics.Handler())
//	go http.ListenAndServe(":9090", nil)
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerWithRegistry returns an HTTP handler that serves Prometheus metrics
// from the specified registry. If the registry is nil, the default Prometheus
// registry is used.
//
// This is useful when you want to expose metrics from a custom registry,
// such as when you've created a Collector with a specific registry.
//
// Example:
//
//	reg := prometheus.NewRegistry()
//	collector, _ := metrics.NewCollector(reg)
//	http.Handle("/metrics", metrics.HandlerWithRegistry(reg))
//	go http.ListenAndServe(":9090", nil)
func HandlerWithRegistry(reg prometheus.Gatherer) http.Handler {
	if reg == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// HandlerFor returns an HTTP handler that serves Prometheus metrics from the
// specified registry with custom handler options. This provides the most
// flexibility for configuring the metrics endpoint.
//
// Parameters:
//   - reg: The Prometheus registry to gather metrics from. If nil, uses default.
//   - opts: Handler options for customizing error handling and other behaviors.
//
// Example:
//
//	opts := promhttp.HandlerOpts{
//	    ErrorHandling: promhttp.ContinueOnError,
//	}
//	http.Handle("/metrics", metrics.HandlerFor(reg, opts))
func HandlerFor(reg prometheus.Gatherer, opts promhttp.HandlerOpts) http.Handler {
	if reg == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(reg, opts)
}
