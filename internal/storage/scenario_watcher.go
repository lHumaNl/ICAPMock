// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// HotReloadConfig contains configuration for scenario hot-reloading.
type HotReloadConfig struct {
	// Enabled enables automatic hot-reload of scenario files.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Debounce is the duration to wait before reloading after a file change.
	// This prevents multiple reloads when a file is saved multiple times quickly.
	// Default: 1s
	Debounce time.Duration `yaml:"debounce" json:"debounce"`

	// WatchDirectory enables watching the entire directory for changes.
	// If false, only watches the specific scenario file.
	// Default: true
	WatchDirectory bool `yaml:"watch_directory" json:"watch_directory"`
}

// SetDefaults sets default values for HotReloadConfig.
func (c *HotReloadConfig) SetDefaults() {
	c.Enabled = false
	c.Debounce = time.Second
	c.WatchDirectory = true
}

// ScenarioWatcher watches scenario files for changes and triggers hot-reload.
// It uses fsnotify for efficient filesystem monitoring and supports debouncing
// to prevent rapid consecutive reloads.
type ScenarioWatcher struct {
	mu          sync.RWMutex
	watcher     *fsnotify.Watcher
	registry    ScenarioRegistry
	config      HotReloadConfig
	filePath    string
	watchedDir  string
	logger      *slog.Logger
	debounceMu  sync.Mutex
	debounceTim map[string]*time.Timer
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	events      chan fsnotify.Event
	errors      chan error
}

// WatcherOption is a functional option for configuring the scenario watcher.
type WatcherOption func(*watcherOptions)

type watcherOptions struct {
	logger *slog.Logger
}

// WithWatcherLogger sets a custom logger for the watcher.
func WithWatcherLogger(logger *slog.Logger) WatcherOption {
	return func(o *watcherOptions) {
		o.logger = logger
	}
}

// NewScenarioWatcher creates a new scenario file watcher.
// The watcher monitors the specified file (and optionally its directory)
// for changes and automatically reloads scenarios when files are modified.
func NewScenarioWatcher(registry ScenarioRegistry, filePath string, config HotReloadConfig, opts ...WatcherOption) (*ScenarioWatcher, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry cannot be nil")
	}

	options := &watcherOptions{}
	for _, opt := range opts {
		opt(options)
	}

	logger := options.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	// Get absolute path and directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	watchedDir := filepath.Dir(absPath)

	// Create fsnotify watcher
	fswatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem watcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	sw := &ScenarioWatcher{
		watcher:     fswatcher,
		registry:    registry,
		config:      config,
		filePath:    absPath,
		watchedDir:  watchedDir,
		logger:      logger,
		debounceTim: make(map[string]*time.Timer),
		ctx:         ctx,
		cancel:      cancel,
		events:      fswatcher.Events,
		errors:      fswatcher.Errors,
	}

	return sw, nil
}

// Start begins watching for file changes.
// If WatchDirectory is enabled, it watches the entire directory.
// Otherwise, it watches only the specific scenario file.
func (sw *ScenarioWatcher) Start() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Determine what to watch
	watchPath := sw.watchedDir
	if !sw.config.WatchDirectory {
		watchPath = sw.filePath
	}

	// Check if path exists
	if _, err := os.Stat(watchPath); os.IsNotExist(err) {
		return fmt.Errorf("watch path does not exist: %s", watchPath)
	}

	if err := sw.watcher.Add(watchPath); err != nil {
		return fmt.Errorf("failed to add watch path: %w", err)
	}

	sw.logger.Info("scenario watcher started",
		"path", watchPath,
		"debounce", sw.config.Debounce,
		"watch_directory", sw.config.WatchDirectory,
	)

	// Start event processing goroutine
	sw.wg.Add(1)
	go sw.processEvents()

	return nil
}

// Stop stops the watcher and releases resources.
// It waits for the event processing goroutine to finish.
func (sw *ScenarioWatcher) Stop() error {
	sw.cancel()

	// Cancel all pending debounce timers
	sw.debounceMu.Lock()
	for path, timer := range sw.debounceTim {
		timer.Stop()
		delete(sw.debounceTim, path)
	}
	sw.debounceMu.Unlock()

	// Close fsnotify watcher
	if err := sw.watcher.Close(); err != nil {
		sw.logger.Warn("error closing watcher", "error", err)
	}

	// Wait for event processing goroutine
	sw.wg.Wait()

	sw.logger.Info("scenario watcher stopped")
	return nil
}

// processEvents handles filesystem events in a goroutine.
func (sw *ScenarioWatcher) processEvents() {
	defer sw.wg.Done()

	for {
		select {
		case <-sw.ctx.Done():
			return
		case event, ok := <-sw.events:
			if !ok {
				return
			}
			sw.handleEvent(event)
		case err, ok := <-sw.errors:
			if !ok {
				return
			}
			sw.logger.Error("watcher error", "error", err)
		}
	}
}

// handleEvent processes a single filesystem event.
func (sw *ScenarioWatcher) handleEvent(event fsnotify.Event) {
	// Only handle write and create events for .yaml/.yml files
	if !sw.isRelevantEvent(event) {
		return
	}

	sw.logger.Debug("filesystem event",
		"name", event.Name,
		"op", event.Op.String(),
	)

	// Debounce the reload
	sw.scheduleReload(event.Name)
}

// isRelevantEvent checks if the event should trigger a reload.
func (sw *ScenarioWatcher) isRelevantEvent(event fsnotify.Event) bool {
	// Only handle write and create events
	if event.Op&fsnotify.Write == 0 && event.Op&fsnotify.Create == 0 {
		return false
	}

	// Check if it's a YAML file
	ext := strings.ToLower(filepath.Ext(event.Name))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}

	// If not watching directory, only handle the specific file
	if !sw.config.WatchDirectory {
		return event.Name == sw.filePath
	}

	return true
}

// scheduleReload schedules a debounced reload operation.
func (sw *ScenarioWatcher) scheduleReload(path string) {
	sw.debounceMu.Lock()
	defer sw.debounceMu.Unlock()

	// Cancel existing timer if any
	if timer, exists := sw.debounceTim[path]; exists {
		timer.Stop()
	}

	// Schedule new reload
	sw.debounceTim[path] = time.AfterFunc(sw.config.Debounce, func() {
		sw.debounceMu.Lock()
		delete(sw.debounceTim, path)
		sw.debounceMu.Unlock()

		sw.reload(path)
	})
}

// reload performs the actual scenario reload.
func (sw *ScenarioWatcher) reload(path string) {
	sw.logger.Info("reloading scenarios due to file change", "path", path)

	if err := sw.registry.Reload(); err != nil {
		sw.logger.Error("failed to reload scenarios",
			"path", path,
			"error", err,
		)
		return
	}

	sw.logger.Info("scenarios reloaded successfully", "path", path)
}

// ReloadTrigger is an interface for manually triggering a reload.
type ReloadTrigger interface {
	// TriggerReload manually triggers a scenario reload.
	TriggerReload() error
}

// TriggerReload manually triggers a scenario reload.
// This is useful for testing or manual intervention.
func (sw *ScenarioWatcher) TriggerReload() error {
	sw.logger.Info("manual reload triggered")
	return sw.registry.Reload()
}

// GetWatchedPath returns the path being watched.
func (sw *ScenarioWatcher) GetWatchedPath() string {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	if sw.config.WatchDirectory {
		return sw.watchedDir
	}
	return sw.filePath
}

// IsRunning returns whether the watcher is currently running.
func (sw *ScenarioWatcher) IsRunning() bool {
	select {
	case <-sw.ctx.Done():
		return false
	default:
		return true
	}
}

// WatcherStats contains statistics about the watcher.
type WatcherStats struct {
	// IsRunning indicates if the watcher is active.
	IsRunning bool `json:"is_running"`

	// WatchedPath is the path being watched.
	WatchedPath string `json:"watched_path"`

	// PendingReloads is the number of pending debounced reloads.
	PendingReloads int `json:"pending_reloads"`

	// Config is the current configuration.
	Config HotReloadConfig `json:"config"`
}

// Stats returns current watcher statistics.
func (sw *ScenarioWatcher) Stats() WatcherStats {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	sw.debounceMu.Lock()
	pending := len(sw.debounceTim)
	sw.debounceMu.Unlock()

	return WatcherStats{
		IsRunning:      sw.IsRunning(),
		WatchedPath:    sw.GetWatchedPath(),
		PendingReloads: pending,
		Config:         sw.config,
	}
}
