// Package handler provides ICAP request handlers for the ICAP Mock Server.
package handler

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// OptionsHandlerConfig holds configuration for the OptionsHandler.
type OptionsHandlerConfig struct {
	// ServiceTag is the ISTag value returned in OPTIONS responses.
	// This should be a unique identifier for the server instance.
	ServiceTag string

	// ServiceID is the Service-ID value returned in OPTIONS responses.
	// This identifies the ICAP service instance (RFC 3507).
	ServiceID string

	// Methods is the list of supported ICAP methods.
	// Valid values are "REQMOD" and "RESPMOD".
	Methods []string

	// MaxConnections is the maximum number of concurrent connections.
	// This is advertised in the Max-Connections header.
	MaxConnections int

	// OptionsTTL is the time clients should cache OPTIONS responses.
	OptionsTTL time.Duration

	// PreviewSize is the number of preview bytes the server requests
	// from the client (RFC 3507 Section 4.5). A value of 0 means the
	// server supports preview but does not require any preview bytes.
	// A negative value disables the Preview header entirely.
	PreviewSize int
}

// OptionsHandler handles ICAP OPTIONS requests.
// It returns server capabilities and configuration information
// as specified in RFC 3507 Section 4.11.
//
// The OPTIONS method allows an ICAP client to discover the
// capabilities of an ICAP server before sending modification requests.
type OptionsHandler struct {
	mu             sync.RWMutex
	serviceTag     string
	serviceID      string
	methods        []string
	maxConnections int
	optionsTTL     time.Duration
	previewSize    int
}

// NewOptionsHandler creates a new OptionsHandler with the given configuration.
//
// Parameters:
//   - config: Configuration options for the handler
//
// Returns a new OptionsHandler instance.
//
// Example:
//
//	h := handler.NewOptionsHandler(handler.OptionsHandlerConfig{
//	    ServiceTag:     `"server-instance-1"`,
//	    ServiceID:      "icap-service-1",
//	    Methods:        []string{"REQMOD", "RESPMOD"},
//	    MaxConnections: 100,
//	    OptionsTTL:     3600 * time.Second,
//	})
func NewOptionsHandler(config OptionsHandlerConfig) *OptionsHandler {
	return &OptionsHandler{
		serviceTag:     config.ServiceTag,
		serviceID:      config.ServiceID,
		methods:        config.Methods,
		maxConnections: config.MaxConnections,
		optionsTTL:     config.OptionsTTL,
		previewSize:    config.PreviewSize,
	}
}

// Handle processes an ICAP OPTIONS request and returns the server capabilities.
// It always returns a 200 OK response with the appropriate headers.
//
// The response includes the following headers:
//   - Methods: List of supported ICAP methods
//   - Service: Server identification string
//   - ISTag: Server instance tag
//   - Service-ID: ICAP service instance identifier (RFC 3507)
//   - Max-Connections: Maximum concurrent connections
//   - Options-TTL: Seconds to cache this response
//   - Preview: Number of preview bytes requested (RFC 3507 Section 4.5)
//   - Allow: Indicates support for 204 responses
func (h *OptionsHandler) Handle(_ context.Context, _ *icap.Request) (*icap.Response, error) {
	h.mu.RLock()
	methods := h.methods
	serviceTag := h.serviceTag
	serviceID := h.serviceID
	maxConns := h.maxConnections
	ttl := h.optionsTTL
	previewSize := h.previewSize
	h.mu.RUnlock()

	resp := icap.NewResponse(icap.StatusOK)

	resp.SetHeader("Methods", strings.Join(methods, ", "))
	resp.SetHeader("Service", "ICAP-Mock-Server/1.0")
	resp.SetHeader("ISTag", serviceTag)
	resp.SetHeader("Service-ID", serviceID)
	resp.SetHeader("Max-Connections", strconv.Itoa(maxConns))
	resp.SetHeader("Options-TTL", strconv.Itoa(int(ttl.Seconds())))
	resp.SetHeader("Allow", "204")
	if previewSize >= 0 {
		resp.SetHeader("Preview", strconv.Itoa(previewSize))
	}

	return resp, nil
}

// Method returns "OPTIONS" as the ICAP method this handler processes.
func (h *OptionsHandler) Method() string {
	return icap.MethodOPTIONS
}

// UpdateMethods allows updating the list of supported methods at runtime.
// This is useful for dynamic configuration changes.
func (h *OptionsHandler) UpdateMethods(methods []string) {
	h.mu.Lock()
	h.methods = methods
	h.mu.Unlock()
}

// UpdateServiceTag allows updating the service tag at runtime.
// This is typically called when the server configuration changes.
func (h *OptionsHandler) UpdateServiceTag(tag string) {
	h.mu.Lock()
	h.serviceTag = tag
	h.mu.Unlock()
}

// UpdateServiceID allows updating the service ID at runtime.
// This is typically called when the server configuration changes.
func (h *OptionsHandler) UpdateServiceID(serviceID string) {
	h.mu.Lock()
	h.serviceID = serviceID
	h.mu.Unlock()
}

// UpdateMaxConnections allows updating the max connections value at runtime.
func (h *OptionsHandler) UpdateMaxConnections(max int) {
	h.mu.Lock()
	h.maxConnections = max
	h.mu.Unlock()
}

// UpdateOptionsTTL allows updating the options TTL at runtime.
func (h *OptionsHandler) UpdateOptionsTTL(ttl time.Duration) {
	h.mu.Lock()
	h.optionsTTL = ttl
	h.mu.Unlock()
}

// UpdatePreviewSize allows updating the preview size at runtime.
// A negative value disables the Preview header entirely.
func (h *OptionsHandler) UpdatePreviewSize(size int) {
	h.mu.Lock()
	h.previewSize = size
	h.mu.Unlock()
}
