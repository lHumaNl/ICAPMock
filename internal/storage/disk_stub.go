//go:build !windows && !unix

// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"fmt"
	"path/filepath"
)

// getDiskUsagePlatform is a stub implementation for platforms without
// specific syscall support. It uses directory walking as a fallback.
//
// Parameters:
//   - path: The directory path to check
//
// Returns:
//   - total: Total disk space in bytes (estimated)
//   - used: Used disk space in bytes
//   - available: Available disk space in bytes (estimated)
//   - error: An error if the check fails
func getDiskUsagePlatform(path string) (total, used, available uint64, err error) {
	// This platform doesn't have specific syscall support
	// We'll return an error to trigger fallback to directory walk
	return 0, 0, 0, fmt.Errorf("platform-specific disk usage not available")
}
