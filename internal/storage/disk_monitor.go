// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	prometheusmetrics "github.com/icap-mock/icap-mock/internal/metrics"
)

// DiskMonitor monitors disk space usage for the storage directory.
// It periodically checks disk usage and provides methods to check if
// writes should be allowed based on configured thresholds.
//
// Thread-safety:
// - All methods are safe for concurrent use
// - State is protected by mutex and atomic operations
type DiskMonitor struct {
	cfg     config.DiskMonitorConfig
	metrics *prometheusmetrics.Collector
	mu      sync.RWMutex
	logger  *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Monitoring state
	path          string
	lastCheckTime atomic.Value // time.Time
	currentUsage  atomic.Value // DiskUsage
	isRunning     atomic.Bool
	lastError     atomic.Value // error

	// Rate limiting - prevents multiple concurrent disk checks
	checkMu sync.Mutex
}

// DiskUsage represents the current disk usage statistics.
type DiskUsage struct {
	Total        int64
	Used         int64
	Available    int64
	UsagePercent float64
}

// NewDiskMonitor creates a new disk monitor instance.
//
// Parameters:
//   - cfg: Disk monitor configuration
//   - requestsDir: The requests directory (fallback if path is empty)
//   - metrics: Prometheus metrics collector (optional)
//   - logger: Structured logger (optional)
//
// Returns:
//   - *DiskMonitor: The created disk monitor
//   - error: An error if initialization fails
func NewDiskMonitor(
	cfg config.DiskMonitorConfig,
	requestsDir string,
	metrics *prometheusmetrics.Collector,
	logger *slog.Logger,
) (*DiskMonitor, error) {
	// Determine path to monitor
	path := cfg.Path
	if path == "" {
		path = requestsDir
	}

	// Resolve absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path: %w", err)
	}

	// Apply defaults for zero-value thresholds
	if cfg.WarningThreshold == 0 && cfg.ErrorThreshold == 0 {
		cfg.WarningThreshold = 0.80
		cfg.ErrorThreshold = 0.95
	}

	// Validate configuration
	if cfg.WarningThreshold < 0 || cfg.WarningThreshold > 1 {
		return nil, fmt.Errorf("invalid warning_threshold: must be between 0.0 and 1.0")
	}
	if cfg.ErrorThreshold < 0 || cfg.ErrorThreshold > 1 {
		return nil, fmt.Errorf("invalid error_threshold: must be between 0.0 and 1.0")
	}
	if cfg.WarningThreshold >= cfg.ErrorThreshold {
		return nil, fmt.Errorf("warning_threshold must be less than error_threshold")
	}

	ctx, cancel := context.WithCancel(context.Background())

	dm := &DiskMonitor{
		cfg:     cfg,
		metrics: metrics,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		path:    absPath,
	}

	// Initialize atomic values
	dm.lastCheckTime.Store(time.Time{})
	dm.currentUsage.Store(DiskUsage{})

	return dm, nil
}

// Start begins periodic disk space monitoring.
// If the monitor is already running, this is a no-op.
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) Start() {
	if !dm.cfg.Enabled {
		if dm.logger != nil {
			dm.logger.Info("Disk monitor is disabled")
		}
		return
	}

	if dm.isRunning.CompareAndSwap(false, true) {
		dm.wg.Add(1)
		go dm.monitorLoop()

		if dm.logger != nil {
			dm.logger.Info("Disk monitor started",
				"path", dm.path,
				"check_interval", dm.cfg.CheckInterval,
				"warning_threshold", dm.cfg.WarningThreshold,
				"error_threshold", dm.cfg.ErrorThreshold,
			)
		}
	}
}

// Stop stops the disk monitor gracefully.
// It waits for the monitoring loop to finish.
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) Stop() {
	if dm.isRunning.CompareAndSwap(true, false) {
		dm.cancel()
		dm.wg.Wait()

		if dm.logger != nil {
			dm.logger.Info("Disk monitor stopped")
		}
	}
}

// CheckDiskSpace checks if there is sufficient disk space for a write.
//
// Parameters:
//   - requiredBytes: The number of bytes required for the write
//
// Returns:
//   - bool: true if write should be allowed, false otherwise
//   - error: An error if disk space check fails
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) CheckDiskSpace(requiredBytes int64) (bool, error) {
	if !dm.cfg.Enabled {
		return true, nil
	}

	// Get current disk usage
	usage, err := dm.getDiskUsage()
	if err != nil {
		return false, err
	}

	// Check if we have enough space
	if usage.Available < requiredBytes {
		// Not enough space
		if dm.metrics != nil {
			dm.metrics.IncStorageDiskErrors()
		}
		if dm.logger != nil {
			dm.logger.Error("Insufficient disk space",
				"path", dm.path,
				"required_bytes", requiredBytes,
				"available_bytes", usage.Available,
				"usage_percent", usage.UsagePercent,
			)
		}
		return false, nil
	}

	// Check error threshold
	if usage.UsagePercent >= dm.cfg.ErrorThreshold {
		// At or above error threshold
		if dm.metrics != nil {
			dm.metrics.IncStorageDiskErrors()
		}
		if dm.logger != nil {
			dm.logger.Warn("Disk usage at error threshold, rejecting write",
				"path", dm.path,
				"usage_percent", usage.UsagePercent,
				"error_threshold", dm.cfg.ErrorThreshold,
			)
		}
		return false, nil
	}

	// Check warning threshold
	if usage.UsagePercent >= dm.cfg.WarningThreshold {
		// At or above warning threshold but below error threshold
		if dm.metrics != nil {
			dm.metrics.IncStorageDiskWarnings()
		}
		if dm.logger != nil {
			dm.logger.Warn("Disk usage at warning threshold",
				"path", dm.path,
				"usage_percent", usage.UsagePercent,
				"warning_threshold", dm.cfg.WarningThreshold,
			)
		}
		return true, nil
	}

	// Below warning threshold - allow write
	return true, nil
}

// GetUsage returns the last known disk usage statistics.
//
// Returns:
//   - DiskUsage: The current disk usage
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) GetUsage() DiskUsage {
	val := dm.currentUsage.Load()
	usage, _ := val.(DiskUsage)
	return usage
}

// IsRunning returns whether the disk monitor is currently running.
//
// Returns:
//   - bool: true if running, false otherwise
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) IsRunning() bool {
	return dm.isRunning.Load()
}

// monitorLoop periodically checks disk space usage.
// It runs in a separate goroutine until Stop() is called or context is cancelled.
func (dm *DiskMonitor) monitorLoop() {
	defer func() {
		if r := recover(); r != nil {
			if dm.logger != nil {
				dm.logger.Error("Disk monitor panic recovered",
					"error", r,
				)
			}
		}
		dm.wg.Done()
	}()

	ticker := time.NewTicker(dm.cfg.CheckInterval)
	defer ticker.Stop()

	// Initial check
	dm.checkDiskSpace()

	for {
		select {
		case <-dm.ctx.Done():
			return
		case <-ticker.C:
			dm.checkDiskSpace()
		}
	}
}

// checkDiskSpace performs a single disk space check.
// It updates metrics and logs warnings/errors as needed.
func (dm *DiskMonitor) checkDiskSpace() {
	usage, err := dm.getDiskUsage()
	if err != nil {
		// Store error for GetLastError() - wrap it to avoid storing nil
		dm.lastError.Store(fmt.Errorf("disk usage check failed: %w", err))

		if dm.logger != nil {
			dm.logger.Error("Failed to get disk usage",
				"path", dm.path,
				"error", err,
			)
		}
		return
	}

	// Clear error on success - store a non-nil sentinel value
	dm.lastError.Store((*error)(nil))

	// Update metrics
	if dm.metrics != nil {
		dm.metrics.SetStorageDiskUsage(usage.Used)
		dm.metrics.SetStorageDiskAvailable(usage.Available)
	}

	// Log at warning threshold
	if usage.UsagePercent >= dm.cfg.WarningThreshold && dm.logger != nil {
		if usage.UsagePercent >= dm.cfg.ErrorThreshold {
			dm.logger.Error("Disk usage at error threshold",
				"path", dm.path,
				"usage_percent", usage.UsagePercent,
				"error_threshold", dm.cfg.ErrorThreshold,
			)
		} else {
			dm.logger.Warn("Disk usage at warning threshold",
				"path", dm.path,
				"usage_percent", usage.UsagePercent,
				"warning_threshold", dm.cfg.WarningThreshold,
			)
		}
	}
}

// getDiskUsage retrieves the current disk usage statistics with caching.
// This method implements rate-limiting to prevent multiple concurrent I/O operations.
//
// Returns:
//   - DiskUsage: The disk usage
//   - error: An error if the check fails
func (dm *DiskMonitor) getDiskUsage() (DiskUsage, error) {
	// Check if cached result is still valid (rate-limiting)
	lastTime, _ := dm.lastCheckTime.Load().(time.Time)
	lastUsage, _ := dm.currentUsage.Load().(DiskUsage)

	if !lastTime.IsZero() && time.Since(lastTime) < dm.cfg.CacheInterval {
		// Return cached result
		return lastUsage, nil
	}

	// Acquire lock to prevent multiple concurrent checks
	dm.checkMu.Lock()
	defer dm.checkMu.Unlock()

	// Double-checked locking pattern
	lastTime, _ = dm.lastCheckTime.Load().(time.Time)
	lastUsage, _ = dm.currentUsage.Load().(DiskUsage)

	if !lastTime.IsZero() && time.Since(lastTime) < dm.cfg.CacheInterval {
		// Another goroutine already refreshed the cache
		return lastUsage, nil
	}

	// Perform actual disk usage check
	usage, err := dm.getDiskUsageInternal()
	if err != nil {
		return DiskUsage{}, err
	}

	// Update cache
	dm.currentUsage.Store(usage)
	dm.lastCheckTime.Store(time.Now())

	return usage, nil
}

// getDiskUsageInternal performs the actual disk usage check.
// This method uses platform-specific syscalls when available.
//
// Returns:
//   - DiskUsage: The disk usage
//   - error: An error if the check fails
func (dm *DiskMonitor) getDiskUsageInternal() (DiskUsage, error) {
	// Check if directory exists
	info, err := os.Stat(dm.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist yet, return safe defaults
			return DiskUsage{
				Total:        100 * 1024 * 1024 * 1024, // 100GB default
				Used:         0,
				Available:    100 * 1024 * 1024 * 1024,
				UsagePercent: 0.0,
			}, nil
		}
		return DiskUsage{}, fmt.Errorf("stat failed: %w", err)
	}

	// Check if path is a directory (not a file)
	if !info.IsDir() {
		return DiskUsage{}, fmt.Errorf("path is not a directory: %s", dm.path)
	}

	// Use platform-specific syscalls if enabled
	if dm.cfg.UseSyscalls {
		usage, err := dm.getDiskUsageSyscalls()
		if err == nil {
			return usage, nil
		}
		// Log warning if syscall fails, fall back to directory walk
		if dm.logger != nil {
			dm.logger.Warn("Syscall disk usage check failed, falling back to directory walk",
				"path", dm.path,
				"error", err,
			)
		}
	}

	// Fallback: use directory walk (slow but works everywhere)
	return dm.getDiskUsageWalk()
}

// getDiskUsageSyscalls retrieves disk usage using platform-specific syscalls.
// This is the preferred method as it's fast and accurate.
//
// Returns:
//   - DiskUsage: The disk usage
//   - error: An error if the check fails
func (dm *DiskMonitor) getDiskUsageSyscalls() (DiskUsage, error) {
	var total, used, available uint64
	var err error

	// Use platform-specific implementation
	total, used, available, err = getDiskUsagePlatform(dm.path)
	if err != nil {
		return DiskUsage{}, err
	}

	// Calculate usage percentage
	var usagePercent float64
	if total > 0 {
		usagePercent = float64(used) / float64(total)
	}

	return DiskUsage{
		Total:        int64(total),
		Used:         int64(used),
		Available:    int64(available),
		UsagePercent: usagePercent,
	}, nil
}

// getDiskUsageWalk retrieves disk usage by walking the directory tree.
// This is a fallback method that is slower but works on all platforms.
//
// Returns:
//   - DiskUsage: The disk usage
//   - error: An error if the check fails
func (dm *DiskMonitor) getDiskUsageWalk() (DiskUsage, error) {
	// Get directory size
	var size int64
	walkErr := filepath.Walk(dm.path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignore temporary access errors (e.g., locked files on Windows)
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	if walkErr != nil {
		return DiskUsage{}, fmt.Errorf("walking directory: %w", walkErr)
	}

	// Default to a reasonable estimate (100GB total, minus our usage)
	// This is a fallback when syscalls are not available
	estimatedTotal := int64(100 * 1024 * 1024 * 1024) // 100GB default
	total := estimatedTotal
	available := total - size

	// Calculate usage percentage
	var usagePercent float64
	if total > 0 {
		usagePercent = float64(size) / float64(total)
	}

	return DiskUsage{
		Total:        total,
		Used:         size,
		Available:    available,
		UsagePercent: usagePercent,
	}, nil
}

// GetLastError returns the last error encountered by the disk monitor.
//
// Returns:
//   - error: The last error, or nil if no error occurred or monitor hasn't checked yet
//
// This method is safe for concurrent use.
func (dm *DiskMonitor) GetLastError() error {
	val := dm.lastError.Load()
	if val == nil {
		return nil
	}
	// Check for sentinel value (nil error pointer)
	if errPtr, ok := val.(*error); ok && errPtr == nil {
		return nil
	}
	// Check for actual error value
	if err, ok := val.(error); ok {
		return err
	}
	return nil
}
