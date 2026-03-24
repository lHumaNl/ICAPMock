// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	prometheusmetrics "github.com/icap-mock/icap-mock/internal/metrics"
)

// Compile-time interface assertions to ensure FileStorage implements
// both RequestReader and RequestWriter interfaces (ISP compliance).
// These assertions verify that FileStorage can be used wherever
// RequestReader, RequestWriter, or the full Storage interface is required.
var (
	_ RequestReader = (*FileStorage)(nil)
	_ RequestWriter = (*FileStorage)(nil)
	_ Storage       = (*FileStorage)(nil)
)

// Error definitions for storage operations.
var (
	// ErrStorageDisabled is returned when storage operations are attempted
	// while storage is disabled.
	ErrStorageDisabled = errors.New("storage is disabled")

	// ErrRequestNotFound is returned when a requested resource is not found.
	ErrRequestNotFound = errors.New("request not found")

	// ErrStorageClosed is returned when operations are attempted on closed storage.
	ErrStorageClosed = errors.New("storage is closed")

	// ErrInvalidRequestID is returned when the request ID format is invalid.
	ErrInvalidRequestID = errors.New("invalid request ID")
)

// FileStorage implements the Storage interface using the filesystem.
// Requests are batched into files with rotation based on RotateAfter config.
// The storage is thread-safe and supports concurrent access.
//
// Thread-safety for Close/SaveRequest:
// The closed flag uses atomic.Bool for fast, lock-free checks. The mu mutex
// is used to ensure that once closed=true is observed, no new channel sends
// can occur. The shutdown sequence is:
//  1. Close() sets closed=true atomically while holding the write lock
//  2. Close() releases the lock, cancels context, waits for asyncWriter
//  3. Close() closes the channel (safe because no new sends can happen)
//
// P0 FIX: Rotation is now non-blocking - separate goroutine handles rotation.
type FileStorage struct {
	config    config.StorageConfig
	mu        sync.RWMutex
	closed    atomic.Bool // CRIT-001: atomic flag for thread-safe close detection
	counter   int64       // Legacy counter for filename generation
	date      string      // Current date for filename generation
	requestCh chan *StoredRequest
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc

	// P0 FIX: Prometheus metrics collector
	metrics *prometheusmetrics.Collector

	// P1 FIX: Disk space monitoring
	diskMonitor *DiskMonitor
	logger      *slog.Logger

	// CRIT-008: Rotation support
	currentFile  *os.File   // Current batch file handle
	fileCounter  int64      // Rotation counter for batch filenames
	requestCount int64      // Number of requests in current file
	rotationMu   sync.Mutex // Mutex for rotation operations (separate from main mu)

	// P0 FIX: Non-blocking rotation channel
	rotationSignal   chan struct{} // Signal for rotation (non-blocking)
	pendingRotations atomic.Int32  // Number of pending rotations (for backpressure)
}

// NewFileStorage creates a new file-based storage instance.
// It creates the storage directory if it doesn't exist.
// P0 FIX: Added metrics collector parameter for rotation monitoring.
func NewFileStorage(cfg config.StorageConfig, metrics *prometheusmetrics.Collector) (*FileStorage, error) {
	if !cfg.Enabled {
		return &FileStorage{
			config:  cfg,
			metrics: metrics,
		}, nil
	}

	// Ensure the directory exists
	if err := os.MkdirAll(cfg.RequestsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating storage directory: %w", err)
	}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := context.WithCancel(context.Background())

	fs := &FileStorage{
		config:         cfg,
		requestCh:      make(chan *StoredRequest, 1000),
		ctx:            ctx,
		cancel:         cancel,
		metrics:        metrics,
		rotationSignal: make(chan struct{}, 100), // P0 FIX: Larger buffer for rotation signals
		logger:         logger,
	}

	// CRIT-008: Initialize first batch file
	if err := fs.initBatchFile(); err != nil {
		cancel()
		return nil, fmt.Errorf("initializing batch file: %w", err)
	}

	// P1 FIX: Initialize and start disk monitor
	diskMonitor, err := NewDiskMonitor(cfg.DiskMonitor, cfg.RequestsDir, metrics, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("initializing disk monitor: %w", err)
	}
	fs.diskMonitor = diskMonitor
	fs.diskMonitor.Start()

	// Start async writer goroutine
	fs.wg.Add(1)
	go fs.asyncWriter()

	// P0 FIX: Start rotation handler goroutine (non-blocking rotation)
	fs.wg.Add(1)
	go fs.rotationHandler()

	return fs, nil
}

// SaveRequest persists an ICAP request to storage.
// The request is queued for async writing to avoid blocking.
//
// Thread-safety: Uses double-check locking pattern with atomic.Bool.
//  1. Fast path: check closed atomically without lock
//  2. Acquire read lock and check again (Close might have happened)
//  3. If still open, send to channel while holding RLock
func (fs *FileStorage) SaveRequest(ctx context.Context, sr *StoredRequest) error {
	if !fs.config.Enabled {
		return ErrStorageDisabled
	}

	// Fast path: atomic check without lock (CRIT-001 fix)
	if fs.closed.Load() {
		return ErrStorageClosed
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Double-check after acquiring lock (CRIT-001 fix)
	// This ensures that if Close() set closed=true after our first check,
	// we detect it before attempting channel send
	if fs.closed.Load() {
		return ErrStorageClosed
	}

	// P0 FIX: Non-blocking send - if channel is full, return error
	// instead of blocking synchronously which violates async I/O principle
	select {
	case fs.requestCh <- sr:
		return nil
	default:
		// Channel full - log warning and drop request
		fs.logger.Warn("Request channel full, dropping request",
			"request_id", sr.ID,
			"reason", "async I/O principle",
		)
		return fmt.Errorf("request queue full, dropping request")
	}
}

// asyncWriter processes queued requests and writes them to disk.
// WARN-001 FIX: Added panic recovery to prevent silent goroutine crashes.
func (fs *FileStorage) asyncWriter() {
	defer func() {
		// WARN-001 FIX: Recover from panics to prevent silent goroutine crashes
		if r := recover(); r != nil {
			stack := debug.Stack()
			fs.logger.Error("PANIC in asyncWriter",
				"error", r,
				"stack", string(stack),
			)
		}
		fs.wg.Done()
	}()

	for {
		select {
		case <-fs.ctx.Done():
			// Drain remaining requests
			for len(fs.requestCh) > 0 {
				sr := <-fs.requestCh
				if err := fs.writeRequest(sr); err != nil {
					fs.logger.Error("failed to write request during drain", "error", err)
				}
			}
			return
		case sr := <-fs.requestCh:
			if err := fs.writeRequest(sr); err != nil {
				fs.logger.Error("failed to write request", "error", err)
			}
		}
	}
}

// rotationHandler handles file rotation in a separate goroutine.
// P0 FIX: Non-blocking rotation - rotation is signaled via channel
// and handled asynchronously to prevent blocking writes.
// Uses drain pattern to handle multiple pending rotation signals efficiently.
func (fs *FileStorage) rotationHandler() {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			fs.logger.Error("PANIC in rotationHandler",
				"error", r,
				"stack", string(stack),
			)
		}
		fs.wg.Done()
	}()

	for {
		select {
		case <-fs.ctx.Done():
			return
		case <-fs.rotationSignal:
			// P0 FIX: Drain all pending rotation signals
			// This prevents multiple consecutive rotations when many requests arrive quickly
			for {
				select {
				case <-fs.rotationSignal:
					// Drain the channel
				default:
					// No more pending rotations
					goto DO_ROTATION
				}
			}

		DO_ROTATION:
			// P0 FIX: Record rotation start in metrics
			startTime := time.Now()
			if fs.metrics != nil {
				fs.metrics.IncStorageRotationActive()
			}

			// Perform rotation
			if err := fs.rotateFile(); err != nil {
				fs.logger.Error("Rotation error", "error", err)
				// Record rotation failure in metrics
				if fs.metrics != nil {
					fs.metrics.RecordStorageRotation("failure")
					fs.metrics.DecStorageRotationActive()
				}
			} else {
				// Record rotation success in metrics
				if fs.metrics != nil {
					duration := time.Since(startTime)
					fs.metrics.RecordStorageRotationDuration(duration)
					fs.metrics.RecordStorageRotation("success")
					fs.metrics.DecStorageRotationActive()
				}
			}
		}
	}
}

// writeRequest writes a single request to disk with rotation support.
// HIGH-005 FIX: Removed JSON indentation to reduce I/O overhead.
// P0 FIX: Non-blocking rotation - signals rotation instead of blocking.
// P1 FIX: Checks disk space before writing to prevent crash when disk is full.
func (fs *FileStorage) writeRequest(sr *StoredRequest) error {
	// P1 FIX: Check disk space before writing
	if fs.diskMonitor != nil {
		// Estimate request size (roughly 1KB for safety)
		estimatedSize := int64(1024)
		canWrite, err := fs.diskMonitor.CheckDiskSpace(estimatedSize)
		if err != nil {
			fs.logger.Warn("Failed to check disk space", "error", err)
			// Continue anyway - best effort
		} else if !canWrite {
			// Not enough space - skip this write
			return fmt.Errorf("insufficient disk space, skipping write")
		}
	}

	fs.rotationMu.Lock()

	// Check if we need to rotate before writing
	if fs.currentFile == nil {
		if err := fs.initBatchFile(); err != nil {
			fs.rotationMu.Unlock()
			return err
		}
	}

	// HIGH-005 FIX: Encode without indentation (5-10 MB/sec I/O savings)
	encoder := json.NewEncoder(fs.currentFile)
	// Note: SetIndent removed for performance - was: encoder.SetIndent("", "  ")
	if err := encoder.Encode(sr); err != nil {
		fs.rotationMu.Unlock()
		return fmt.Errorf("encoding request: %w", err)
	}

	// P0 FIX: Track request count and signal rotation if needed
	// Non-blocking: send signal to rotation handler instead of blocking
	fs.requestCount++
	needsRotation := fs.config.RotateAfter > 0 && fs.requestCount >= int64(fs.config.RotateAfter)

	// Release lock before signaling rotation to avoid deadlock
	fs.rotationMu.Unlock()

	// Signal rotation if needed (non-blocking)
	if needsRotation {
		select {
		case fs.rotationSignal <- struct{}{}:
			// Signal sent successfully, rotation handler will process it
		default:
			// Rotation channel full - log warning but continue
			fs.logger.Warn("Rotation signal channel full, rotation delayed")
		}
	}

	return nil
}

// initBatchFile initializes the first batch file for writing.
// CRIT-008: Creates a new batch file with rotation counter.
func (fs *FileStorage) initBatchFile() error {
	// Generate initial batch filename
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	filename := filepath.Join(
		fs.config.RequestsDir,
		fmt.Sprintf("%s_batch_%06d.jsonl", dateStr, fs.fileCounter),
	)

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("creating batch file: %w", err)
	}

	fs.currentFile = f
	fs.requestCount = 0
	return nil
}

// rotateFile closes the current batch file and creates a new one.
// CRIT-008: Atomic rotation to prevent data loss.
// WARN-006 FIX: Uses syncWithRetry before Close() to ensure data is flushed to disk.
// P0 FIX: Protected by rotationMu to prevent race condition with writeRequest.
func (fs *FileStorage) rotateFile() error {
	fs.rotationMu.Lock()
	defer fs.rotationMu.Unlock()

	// Close current file if open
	if fs.currentFile != nil {
		// WARN-006 FIX: Sync with retry before close to ensure data is flushed to disk
		if err := fs.syncWithRetry(fs.currentFile); err != nil {
			fs.logger.Warn("Failed to sync file before rotation after retries",
				"error", err,
			)
			// Continue with close anyway - best effort
		}
		if err := fs.currentFile.Close(); err != nil {
			return fmt.Errorf("closing current file: %w", err)
		}
	}

	// Increment rotation counter
	fs.fileCounter++

	// Create new batch file
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	filename := filepath.Join(
		fs.config.RequestsDir,
		fmt.Sprintf("%s_batch_%06d.jsonl", dateStr, fs.fileCounter),
	)

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("creating new batch file: %w", err)
	}

	fs.currentFile = f
	fs.requestCount = 0
	return nil
}

// generateFilename generates a unique filename for a request.
// Format: YYYY-MM-DD_NNN.json
func (fs *FileStorage) generateFilename(t time.Time) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	dateStr := t.Format("2006-01-02")
	if fs.date != dateStr {
		fs.date = dateStr
		fs.counter = 0
	}

	fs.counter++
	return filepath.Join(
		fs.config.RequestsDir,
		fmt.Sprintf("%s_%03d.json", dateStr, fs.counter),
	)
}

// GetRequest retrieves a previously stored request by its ID.
func (fs *FileStorage) GetRequest(ctx context.Context, id string) (*StoredRequest, error) {
	if !fs.config.Enabled {
		return nil, ErrStorageDisabled
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed.Load() {
		return nil, ErrStorageClosed
	}

	// Parse ID to get date
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		return nil, ErrRequestNotFound
	}

	// Extract date from ID: req-YYYYMMDD-NNN
	datePart := parts[1]
	if len(datePart) != 8 {
		return nil, ErrRequestNotFound
	}

	// Parse the date
	t, err := time.Parse("20060102", datePart)
	if err != nil {
		return nil, ErrRequestNotFound
	}

	// Search in old format files first (YYYY-MM-DD_*.json)
	dateStr := t.Format("2006-01-02")
	oldPattern := filepath.Join(fs.config.RequestsDir, dateStr+"_*.json")

	matches, err := filepath.Glob(oldPattern)
	if err != nil {
		return nil, fmt.Errorf("searching for request files: %w", err)
	}

	for _, path := range matches {
		sr, err := fs.readRequestFile(path)
		if err != nil {
			continue
		}
		if sr.ID == id {
			return sr, nil
		}
	}

	// Search in new batch files (YYYY-MM-DD_batch_*.jsonl)
	batchPattern := filepath.Join(fs.config.RequestsDir, dateStr+"_batch_*.jsonl")
	batchMatches, err := filepath.Glob(batchPattern)
	if err != nil {
		return nil, fmt.Errorf("searching for batch files: %w", err)
	}

	for _, path := range batchMatches {
		requests, err := fs.readBatchFile(path)
		if err != nil {
			continue
		}
		for _, sr := range requests {
			if sr.ID == id {
				return sr, nil
			}
		}
	}

	return nil, ErrRequestNotFound
}

// ListRequests retrieves requests matching the given filter.
func (fs *FileStorage) ListRequests(ctx context.Context, filter RequestFilter) ([]*StoredRequest, error) {
	if !fs.config.Enabled {
		return nil, ErrStorageDisabled
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed.Load() {
		return nil, ErrStorageClosed
	}

	// Build glob patterns for both old and new formats
	var patterns []string
	if !filter.Start.IsZero() || !filter.End.IsZero() {
		// List all files and filter by date
		patterns = []string{
			filepath.Join(fs.config.RequestsDir, "*.json"),
			filepath.Join(fs.config.RequestsDir, "*.jsonl"),
		}
	} else {
		patterns = []string{
			filepath.Join(fs.config.RequestsDir, "*.json"),
			filepath.Join(fs.config.RequestsDir, "*.jsonl"),
		}
	}

	var results []*StoredRequest

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("searching for request files: %w", err)
		}

		for _, path := range matches {
			// Check file extension to determine format
			ext := filepath.Ext(path)
			if ext == ".jsonl" {
				// New batch format - read all requests from file
				requests, err := fs.readBatchFile(path)
				if err != nil {
					continue
				}
				for _, sr := range requests {
					if fs.matchesFilter(sr, filter) {
						results = append(results, sr)
					}
				}
			} else {
				// Old format - single request per file
				sr, err := fs.readRequestFile(path)
				if err != nil {
					continue
				}
				if fs.matchesFilter(sr, filter) {
					results = append(results, sr)
				}
			}
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	// Apply offset and limit
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	} else if filter.Offset >= len(results) {
		results = nil
	}

	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

// matchesFilter checks if a stored request matches the filter criteria.
func (fs *FileStorage) matchesFilter(sr *StoredRequest, filter RequestFilter) bool {
	// Check time range
	if !filter.Start.IsZero() && sr.Timestamp.Before(filter.Start) {
		return false
	}
	if !filter.End.IsZero() && !sr.Timestamp.Before(filter.End) {
		return false
	}

	// Check method
	if filter.Method != "" && sr.Method != filter.Method {
		return false
	}

	// Check client IP
	if filter.ClientIP != "" && sr.ClientIP != filter.ClientIP {
		return false
	}

	return true
}

// readRequestFile reads and parses a request from a file.
// Supports both old format (single JSON object) and new format (NDJSON batch).
func (fs *FileStorage) readRequestFile(path string) (*StoredRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading request file: %w", err)
	}

	// Try to parse as single JSON object first (old format)
	var sr StoredRequest
	if err := json.Unmarshal(data, &sr); err == nil {
		return &sr, nil
	}

	// If that fails, try NDJSON format (new batch format)
	// Each line is a separate JSON object
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var batchSR StoredRequest
		if err := json.Unmarshal([]byte(line), &batchSR); err == nil {
			return &batchSR, nil
		}
	}

	return nil, fmt.Errorf("parsing request file: invalid format")
}

// readBatchFile reads all requests from an NDJSON batch file.
func (fs *FileStorage) readBatchFile(path string) ([]*StoredRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading batch file: %w", err)
	}

	var requests []*StoredRequest
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var sr StoredRequest
		if err := json.Unmarshal([]byte(line), &sr); err != nil {
			continue // Skip malformed lines
		}
		requests = append(requests, &sr)
	}

	return requests, nil
}

// DeleteRequest removes a request from storage.
// Note: For batch files, this marks the request as deleted but doesn't remove
// it from the file (would require rewriting the entire batch).
// For old format files, it removes the file entirely.
func (fs *FileStorage) DeleteRequest(ctx context.Context, id string) error {
	if !fs.config.Enabled {
		return ErrStorageDisabled
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed.Load() {
		return ErrStorageClosed
	}

	// Find the file by ID - search both old and new formats
	patterns := []string{
		filepath.Join(fs.config.RequestsDir, "*.json"),
		filepath.Join(fs.config.RequestsDir, "*.jsonl"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("searching for request files: %w", err)
		}

		for _, path := range matches {
			ext := filepath.Ext(path)
			if ext == ".jsonl" {
				// Batch file - search within
				requests, err := fs.readBatchFile(path)
				if err != nil {
					continue
				}
				for _, sr := range requests {
					if sr.ID == id {
						// Found in batch file - we need to rewrite the file without this request
						// This is expensive but necessary for data integrity
						var remaining []*StoredRequest
						for _, r := range requests {
							if r.ID != id {
								remaining = append(remaining, r)
							}
						}
						return fs.rewriteBatchFile(path, remaining)
					}
				}
			} else {
				// Old format - single request file
				sr, err := fs.readRequestFile(path)
				if err != nil {
					continue
				}
				if sr.ID == id {
					return os.Remove(path)
				}
			}
		}
	}

	return ErrRequestNotFound
}

// rewriteBatchFile rewrites a batch file with the remaining requests.
// On Windows, this requires closing the file handle if it's the current batch file.
func (fs *FileStorage) rewriteBatchFile(path string, requests []*StoredRequest) error {
	// Check if this is the current batch file and close it if so
	fs.rotationMu.Lock()
	if fs.currentFile != nil {
		// Get the current file's path
		currentPath := ""
		if fs.currentFile != nil {
			if info, err := fs.currentFile.Stat(); err == nil {
				currentPath = filepath.Join(fs.config.RequestsDir, info.Name())
			}
		}

		// Normalize paths for comparison
		absPath, _ := filepath.Abs(path)
		absCurrentPath, _ := filepath.Abs(currentPath)

		if absPath == absCurrentPath {
			// This is the current file - close it first
			_ = fs.syncWithRetry(fs.currentFile)
			_ = fs.currentFile.Close()
			fs.currentFile = nil
		}
	}
	fs.rotationMu.Unlock()

	// Write to temp file first for atomicity
	tempPath := path + ".tmp"
	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	encoder := json.NewEncoder(f)
	for _, sr := range requests {
		if err := encoder.Encode(sr); err != nil {
			f.Close()
			os.Remove(tempPath)
			return fmt.Errorf("encoding request: %w", err)
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("renaming file: %w", err)
	}

	return nil
}

// syncWithRetry attempts to sync a file with exponential backoff retry.
// This helps handle transient I/O errors that can occur during high load
// or when the underlying filesystem is temporarily unavailable.
func (fs *FileStorage) syncWithRetry(file *os.File) error {
	const maxRetries = 3
	const baseDelay = 50 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := file.Sync(); err != nil {
			lastErr = err
			if attempt < maxRetries-1 {
				// Exponential backoff: 50ms, 100ms, 200ms
				delay := baseDelay * time.Duration(1<<attempt)
				fs.logger.Warn("Sync attempt failed, retrying",
					"attempt", attempt+1,
					"error", err,
					"retry_delay", delay,
				)
				time.Sleep(delay)
			}
			continue
		}
		return nil
	}

	return fmt.Errorf("sync failed after %d retries: %w", maxRetries, lastErr)
}

// Close releases resources used by the storage.
// It waits for pending writes to complete.
//
// Thread-safety (CRIT-001 fix):
//  1. Acquire write lock to prevent new SaveRequest from proceeding
//  2. Use CompareAndSwap to atomically set closed=true (handles double-close)
//  3. Release lock (no new SaveRequest can pass the closed check now)
//  4. Cancel context to signal asyncWriter to stop and drain
//  5. Wait for asyncWriter to complete draining
//  6. Close channel (safe because no more sends can occur)
//  7. Close current batch file (CRIT-008)
//  8. Stop disk monitor (P1 FIX)
//
// WARN-006 FIX: Sync before close to ensure data is flushed to disk.
func (fs *FileStorage) Close() error {
	if !fs.config.Enabled {
		return nil
	}

	fs.mu.Lock()
	// Use CompareAndSwap for atomic double-close protection
	if !fs.closed.CompareAndSwap(false, true) {
		fs.mu.Unlock()
		return nil // Already closed
	}
	fs.mu.Unlock()

	// Cancel context and wait for async writer to finish
	fs.cancel()
	fs.wg.Wait()
	close(fs.requestCh)

	// P1 FIX: Stop disk monitor
	if fs.diskMonitor != nil {
		fs.diskMonitor.Stop()
	}

	// CRIT-008: Close current batch file
	fs.rotationMu.Lock()
	if fs.currentFile != nil {
		// WARN-006 FIX: Sync with retry before close to ensure data is flushed to disk
		if err := fs.syncWithRetry(fs.currentFile); err != nil {
			fs.logger.Warn("Failed to sync file before close after retries",
				"error", err,
			)
			// Continue with close anyway - best effort
		}
		if err := fs.currentFile.Close(); err != nil {
			fs.rotationMu.Unlock()
			return fmt.Errorf("closing batch file: %w", err)
		}
		fs.currentFile = nil
	}
	fs.rotationMu.Unlock()

	return nil
}

// IsEnabled returns true if storage is enabled.
func (fs *FileStorage) IsEnabled() bool {
	return fs.config.Enabled
}

// Flush forces any buffered data to be written to persistent storage.
// This ensures all pending requests in the async writer queue are written
// to disk before returning.
func (fs *FileStorage) Flush(ctx context.Context) error {
	if !fs.config.Enabled {
		return nil
	}

	if fs.closed.Load() {
		return ErrStorageClosed
	}

	// Wait for the channel to drain
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if len(fs.requestCh) == 0 {
				// Channel is empty, now sync the file
				fs.rotationMu.Lock()
				if fs.currentFile != nil {
					err := fs.syncWithRetry(fs.currentFile)
					fs.rotationMu.Unlock()
					return err
				}
				fs.rotationMu.Unlock()
				return nil
			}
			// Small sleep to avoid busy waiting
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Clear removes all stored requests from storage.
// This deletes all request files in the storage directory.
// Returns the number of requests cleared.
func (fs *FileStorage) Clear(ctx context.Context) (int64, error) {
	if !fs.config.Enabled {
		return 0, nil
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed.Load() {
		return 0, ErrStorageClosed
	}

	// First, flush and close the current batch file to release the handle
	fs.rotationMu.Lock()
	if fs.currentFile != nil {
		// Sync and close the current file to release the handle
		_ = fs.syncWithRetry(fs.currentFile)
		_ = fs.currentFile.Close()
		fs.currentFile = nil
		fs.requestCount = 0
	}
	fs.rotationMu.Unlock()

	// Count and remove all request files
	var count int64
	patterns := []string{
		filepath.Join(fs.config.RequestsDir, "*.json"),
		filepath.Join(fs.config.RequestsDir, "*.jsonl"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return count, fmt.Errorf("searching for request files: %w", err)
		}

		for _, path := range matches {
			// Count requests in the file before deleting
			ext := filepath.Ext(path)
			if ext == ".jsonl" {
				requests, err := fs.readBatchFile(path)
				if err == nil {
					count += int64(len(requests))
				}
			} else {
				count++
			}

			if err := os.Remove(path); err != nil {
				return count, fmt.Errorf("removing file %s: %w", path, err)
			}
		}
	}

	// Reset file counter so next file starts fresh
	fs.fileCounter = 0

	return count, nil
}

// DeleteRequests removes multiple requests matching the given filter.
// Returns the number of requests deleted.
func (fs *FileStorage) DeleteRequests(ctx context.Context, filter RequestFilter) (int64, error) {
	if !fs.config.Enabled {
		return 0, nil
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed.Load() {
		return 0, ErrStorageClosed
	}

	// Build glob patterns for both old and new formats
	patterns := []string{
		filepath.Join(fs.config.RequestsDir, "*.json"),
		filepath.Join(fs.config.RequestsDir, "*.jsonl"),
	}

	var deleted int64

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return deleted, fmt.Errorf("searching for request files: %w", err)
		}

		for _, path := range matches {
			ext := filepath.Ext(path)
			if ext == ".jsonl" {
				// Batch file - find and remove matching requests
				requests, err := fs.readBatchFile(path)
				if err != nil {
					continue
				}

				var remaining []*StoredRequest
				for _, sr := range requests {
					if fs.matchesFilter(sr, filter) {
						deleted++
					} else {
						remaining = append(remaining, sr)
					}
				}

				// Only rewrite if we deleted something
				if len(remaining) < len(requests) {
					if err := fs.rewriteBatchFile(path, remaining); err != nil {
						// Log but continue with other files
						continue
					}
				}
			} else {
				// Old format - single request file
				sr, err := fs.readRequestFile(path)
				if err != nil {
					continue
				}
				if fs.matchesFilter(sr, filter) {
					if err := os.Remove(path); err == nil {
						deleted++
					}
				}
			}
		}
	}

	return deleted, nil
}
