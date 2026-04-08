//go:build !windows

// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"fmt"
	"syscall"
)

// getDiskUsagePlatform retrieves disk usage statistics using Unix-specific syscalls.
// This is much faster than filepath.Walk as it uses statfs system call.
//
// Parameters:
//   - path: The directory path to check
//
// Returns:
//   - total: Total disk space in bytes
//   - used: Used disk space in bytes
//   - available: Available disk space in bytes
//   - error: An error if the check fails
func getDiskUsagePlatform(path string) (total, used, available uint64, err error) {
	var stat syscall.Statfs_t

	// Get filesystem statistics
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, fmt.Errorf("statfs failed: %w", err)
	}

	// Calculate disk usage
	// Bsize: Block size
	// Blocks: Total blocks
	// Bfree: Free blocks
	// Bavail: Available blocks (for non-root)
	blockSize := uint64(stat.Bsize)             //nolint:gosec // Bsize is always positive
	total = blockSize * uint64(stat.Blocks)     //nolint:gosec,unconvert // int64 on Linux, uint64 on macOS
	freeBytes := blockSize * uint64(stat.Bfree) //nolint:gosec,unconvert // int64 on Linux, uint64 on macOS
	available = blockSize * uint64(stat.Bavail) //nolint:gosec,unconvert // int64 on Linux, uint64 on macOS
	used = total - freeBytes

	return total, used, available, nil
}
