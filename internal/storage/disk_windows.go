//go:build windows

// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows API declarations
var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// getDiskUsagePlatform retrieves disk usage statistics using Windows-specific API.
// This is much faster than filepath.Walk as it uses GetDiskFreeSpaceEx system call.
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
	// Convert path to UTF-16
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("converting path: %w", err)
	}

	// Call GetDiskFreeSpaceExW
	// Parameters:
	//   - lpDirectoryName: Directory path
	//   - lpFreeBytesAvailable: Pointer to receive available bytes
	//   - lpTotalNumberOfBytes: Pointer to receive total bytes
	//   - lpTotalNumberOfFreeBytes: Pointer to receive free bytes
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	retval, _, err := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)

	if retval == 0 {
		return 0, 0, 0, fmt.Errorf("GetDiskFreeSpaceExW failed: %w", err)
	}

	total = totalBytes
	available = freeBytesAvailable
	used = total - totalFreeBytes

	return total, used, available, nil
}
