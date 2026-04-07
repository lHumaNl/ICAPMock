// Copyright 2026 ICAP Mock

package storage

import (
	"context"
)

// RequestReader defines the interface for read and query operations on stored requests.
// This interface follows the Interface Segregation Principle (ISP) by separating
// read/query operations from write operations.
//
// Clients that only need to retrieve or query requests (e.g., replay engines,
// monitoring tools, admin dashboards) should depend on this interface rather
// than the full Storage interface.
//
// Implementations must be thread-safe for concurrent access.
type RequestReader interface {
	// GetRequest retrieves a previously stored request by its ID.
	// Returns ErrRequestNotFound if no request matches the ID.
	GetRequest(ctx context.Context, id string) (*StoredRequest, error)

	// ListRequests retrieves requests matching the given filter.
	// An empty filter returns all requests (use with caution).
	// Results are ordered by timestamp (newest first).
	ListRequests(ctx context.Context, filter RequestFilter) ([]*StoredRequest, error)
}
