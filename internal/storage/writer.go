// Copyright 2026 ICAP Mock

package storage

import (
	"context"
)

// RequestWriter defines the interface for write and lifecycle operations on stored requests.
// This interface follows the Interface Segregation Principle (ISP) by separating
// write operations from read operations.
//
// Clients that only need to persist requests (e.g., the ICAP server handling
// incoming requests) should depend on this interface rather than the full
// Storage interface.
//
// Implementations must be thread-safe for concurrent access.
type RequestWriter interface {
	// SaveRequest persists a stored request to storage.
	// The request is written asynchronously for performance.
	// Returns an error if the storage is disabled or on write failure.
	SaveRequest(ctx context.Context, req *StoredRequest) error

	// Flush forces any buffered data to be written to persistent storage.
	// This is useful for ensuring data durability before shutdown or
	// for testing purposes.
	Flush(ctx context.Context) error

	// Clear removes all stored requests from storage.
	// This is a destructive operation and should be used with caution.
	// Returns the number of requests cleared.
	Clear(ctx context.Context) (int64, error)

	// DeleteRequest removes a request from storage.
	// Returns ErrRequestNotFound if no request matches the ID.
	DeleteRequest(ctx context.Context, id string) error

	// DeleteRequests removes multiple requests matching the given filter.
	// Returns the number of requests deleted.
	// This is useful for bulk cleanup operations.
	DeleteRequests(ctx context.Context, filter RequestFilter) (int64, error)

	// Close releases any resources used by the storage.
	// It waits for pending writes to complete and flushes any remaining data.
	// After Close is called, all other methods will return ErrStorageClosed.
	Close() error
}
