// Copyright 2026 ICAP Mock

package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewScenarioWatcher tests creating a new scenario watcher.
func TestNewScenarioWatcher(t *testing.T) {
	t.Run("creates watcher successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create the file
		require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

		registry := NewScenarioRegistry()
		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       time.Second,
			WatchDirectory: true,
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		require.NotNil(t, watcher)

		// Clean up
		require.NoError(t, watcher.Stop())
	})

	t.Run("returns error for nil registry", func(t *testing.T) {
		config := HotReloadConfig{Enabled: true}

		_, err := NewScenarioWatcher(nil, "scenarios.yaml", config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry cannot be nil")
	})
}

// TestScenarioWatcher_StartStop tests starting and stopping the watcher.
func TestScenarioWatcher_StartStop(t *testing.T) {
	t.Run("starts and stops successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create initial scenario file
		yamlContent := `
scenarios:
  - name: "test-scenario"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
		require.NoError(t, os.WriteFile(scenarioFile, []byte(yamlContent), 0644))

		registry := NewScenarioRegistry()
		require.NoError(t, registry.Load(scenarioFile))

		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       100 * time.Millisecond, // Short debounce for tests
			WatchDirectory: true,
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)

		// Start watcher
		require.NoError(t, watcher.Start())
		assert.True(t, watcher.IsRunning())

		// Stop watcher
		require.NoError(t, watcher.Stop())
		assert.False(t, watcher.IsRunning())
	})

	t.Run("returns error for non-existent path", func(t *testing.T) {
		registry := NewScenarioRegistry()
		config := HotReloadConfig{Enabled: true}

		watcher, err := NewScenarioWatcher(registry, "/nonexistent/path/scenarios.yaml", config)
		require.NoError(t, err)

		err = watcher.Start()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

// TestScenarioWatcher_HotReload tests the hot-reload functionality.
func TestScenarioWatcher_HotReload(t *testing.T) {
	t.Run("reloads on file modification", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create initial scenario file
		initialContent := `
scenarios:
  - name: "initial-scenario"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
		require.NoError(t, os.WriteFile(scenarioFile, []byte(initialContent), 0644))

		registry := NewScenarioRegistry()
		require.NoError(t, registry.Load(scenarioFile))

		// Verify initial state
		scenarios := registry.List()
		initialFound := false
		for _, s := range scenarios {
			if s.Name == "initial-scenario" {
				initialFound = true
				break
			}
		}
		require.True(t, initialFound, "initial scenario should exist")

		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       50 * time.Millisecond, // Short debounce for tests
			WatchDirectory: false,                 // Watch only the file
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		require.NoError(t, watcher.Start())

		// Modify the file
		updatedContent := `
scenarios:
  - name: "updated-scenario"
    priority: 100
    match: {}
    response:
      icap_status: 200
`
		require.NoError(t, os.WriteFile(scenarioFile, []byte(updatedContent), 0644))

		// Wait for reload to complete
		time.Sleep(200 * time.Millisecond)

		// Verify updated state
		scenarios = registry.List()
		updatedFound := false
		for _, s := range scenarios {
			if s.Name == "updated-scenario" {
				updatedFound = true
				break
			}
		}
		require.True(t, updatedFound, "updated scenario should exist after hot-reload")
	})

	t.Run("reloads on new file in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create initial scenario file
		initialContent := `
scenarios:
  - name: "initial"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
		require.NoError(t, os.WriteFile(scenarioFile, []byte(initialContent), 0644))

		registry := NewScenarioRegistry()
		require.NoError(t, registry.Load(scenarioFile))

		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       50 * time.Millisecond,
			WatchDirectory: true, // Watch the entire directory
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		require.NoError(t, watcher.Start())

		// Create a new YAML file in the directory
		newFile := filepath.Join(tmpDir, "new-scenarios.yaml")
		newContent := `
scenarios:
  - name: "from-new-file"
    priority: 100
    match: {}
    response:
      icap_status: 200
`
		require.NoError(t, os.WriteFile(newFile, []byte(newContent), 0644))

		// Wait for reload to complete
		time.Sleep(200 * time.Millisecond)

		// Note: The reload happens on the original file, not the new file
		// This test just verifies the directory watching triggers correctly
		require.True(t, watcher.IsRunning())
	})
}

// TestScenarioWatcher_Debounce tests the debouncing functionality.
func TestScenarioWatcher_Debounce(t *testing.T) {
	t.Run("debounces rapid file changes", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create initial file
		require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

		registry := NewScenarioRegistry()
		require.NoError(t, registry.Load(scenarioFile))

		reloadCount := 0
		var mu sync.Mutex

		// Create a wrapper to count reloads
		countingRegistry := &countingScenarioRegistry{
			ScenarioRegistry: registry,
			reloadCount:      &reloadCount,
			mu:               &mu,
		}

		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       100 * time.Millisecond,
			WatchDirectory: false,
		}

		watcher, err := NewScenarioWatcher(countingRegistry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		require.NoError(t, watcher.Start())

		// Rapidly write to the file multiple times
		for i := 0; i < 5; i++ {
			require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))
			time.Sleep(10 * time.Millisecond)
		}

		// Wait for debounce to complete
		time.Sleep(200 * time.Millisecond)

		// Should have debounced to fewer reloads
		// Note: Exact count may vary, but should be less than 5
		mu.Lock()
		count := reloadCount
		mu.Unlock()

		// At most 1-2 reloads due to debouncing
		assert.LessOrEqual(t, count, 2, "reload should be debounced")
	})
}

// countingScenarioRegistry wraps ScenarioRegistry to count reload calls.
type countingScenarioRegistry struct {
	ScenarioRegistry
	reloadCount *int
	mu          *sync.Mutex
}

func (c *countingScenarioRegistry) Reload() error {
	c.mu.Lock()
	(*c.reloadCount)++
	c.mu.Unlock()
	return c.ScenarioRegistry.Reload()
}

// TestScenarioWatcher_ManualReload tests manual reload triggering.
func TestScenarioWatcher_ManualReload(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Create initial file
	initialContent := `
scenarios:
  - name: "manual-test"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
	require.NoError(t, os.WriteFile(scenarioFile, []byte(initialContent), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:  false, // Disabled automatic reload
		Debounce: time.Second,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)
	defer watcher.Stop()

	// Modify file
	updatedContent := `
scenarios:
  - name: "manual-updated"
    priority: 100
    match: {}
    response:
      icap_status: 200
`
	require.NoError(t, os.WriteFile(scenarioFile, []byte(updatedContent), 0644))

	// Trigger manual reload
	require.NoError(t, watcher.TriggerReload())

	// Verify updated scenario
	scenarios := registry.List()
	updatedFound := false
	for _, s := range scenarios {
		if s.Name == "manual-updated" {
			updatedFound = true
			break
		}
	}
	require.True(t, updatedFound, "manual reload should update scenarios")
}

// TestScenarioWatcher_Stats tests the stats method.
func TestScenarioWatcher_Stats(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:        true,
		Debounce:       time.Second,
		WatchDirectory: true,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)
	defer watcher.Stop()

	require.NoError(t, watcher.Start())

	stats := watcher.Stats()
	assert.True(t, stats.IsRunning)
	assert.Equal(t, config, stats.Config)
	assert.GreaterOrEqual(t, stats.PendingReloads, 0)
}

// TestScenarioWatcher_GetWatchedPath tests the watched path getter.
func TestScenarioWatcher_GetWatchedPath(t *testing.T) {
	t.Run("returns directory when watching directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

		registry := NewScenarioRegistry()
		config := HotReloadConfig{
			Enabled:        true,
			WatchDirectory: true,
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		path := watcher.GetWatchedPath()
		assert.Equal(t, tmpDir, path)
	})

	t.Run("returns file when not watching directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

		registry := NewScenarioRegistry()
		config := HotReloadConfig{
			Enabled:        true,
			WatchDirectory: false,
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		absPath, _ := filepath.Abs(scenarioFile)
		path := watcher.GetWatchedPath()
		assert.Equal(t, absPath, path)
	})
}

// TestScenarioWatcher_ContextCancellation tests that watcher stops on context cancellation.
func TestScenarioWatcher_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:  true,
		Debounce: time.Second,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)

	require.NoError(t, watcher.Start())
	assert.True(t, watcher.IsRunning())

	// Stop via context
	require.NoError(t, watcher.Stop())

	// Give it time to stop
	time.Sleep(50 * time.Millisecond)
	assert.False(t, watcher.IsRunning())
}

// TestScenarioWatcher_WithCustomLogger tests watcher with custom logger.
func TestScenarioWatcher_WithCustomLogger(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	// Create custom logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	config := HotReloadConfig{Enabled: true}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config, WithWatcherLogger(logger))
	require.NoError(t, err)
	require.NotNil(t, watcher)
	defer watcher.Stop()
}

// TestHotReloadConfig_SetDefaults tests the SetDefaults method.
func TestHotReloadConfig_SetDefaults(t *testing.T) {
	config := HotReloadConfig{}
	config.SetDefaults()

	assert.False(t, config.Enabled)
	assert.Equal(t, time.Second, config.Debounce)
	assert.True(t, config.WatchDirectory)
}

// TestScenarioWatcher_ConcurrentAccess tests thread safety of the watcher.
func TestScenarioWatcher_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:        true,
		Debounce:       50 * time.Millisecond,
		WatchDirectory: true,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)
	defer watcher.Stop()

	require.NoError(t, watcher.Start())

	var wg sync.WaitGroup

	// Concurrent Stats calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = watcher.Stats()
		}()
	}

	// Concurrent GetWatchedPath calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = watcher.GetWatchedPath()
		}()
	}

	// Concurrent file modifications
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644)
			time.Sleep(10 * time.Millisecond)
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Let debounce complete
}

// TestScenarioWatcher_IsRelevantEvent tests event filtering (internal test).
func TestScenarioWatcher_IsRelevantEvent(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	absPath, _ := filepath.Abs(scenarioFile)

	tests := []struct {
		event    fsnotify.Event
		name     string
		filePath string
		watchDir bool
		expected bool
	}{
		{
			name:     "yaml file write",
			event:    fsnotify.Event{Name: absPath, Op: fsnotify.Write},
			watchDir: true,
			expected: true,
		},
		{
			name:     "yaml file create",
			event:    fsnotify.Event{Name: absPath, Op: fsnotify.Create},
			watchDir: true,
			expected: true,
		},
		{
			name:     "non-yaml file",
			event:    fsnotify.Event{Name: filepath.Join(tmpDir, "test.txt"), Op: fsnotify.Write},
			watchDir: true,
			expected: false,
		},
		{
			name:     "chmod event ignored",
			event:    fsnotify.Event{Name: absPath, Op: fsnotify.Chmod},
			watchDir: true,
			expected: false,
		},
		{
			name:     "file specific watch - matching file",
			event:    fsnotify.Event{Name: absPath, Op: fsnotify.Write},
			watchDir: false,
			filePath: absPath,
			expected: true,
		},
		{
			name:     "file specific watch - different file",
			event:    fsnotify.Event{Name: filepath.Join(tmpDir, "other.yaml"), Op: fsnotify.Write},
			watchDir: false,
			filePath: absPath,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := tt.filePath
			if filePath == "" {
				filePath = absPath
			}

			sw := &ScenarioWatcher{
				filePath: filePath,
				config: HotReloadConfig{
					WatchDirectory: tt.watchDir,
				},
			}
			result := sw.isRelevantEvent(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestScenarioWatcher_MultipleStops tests that Stop is idempotent.
func TestScenarioWatcher_MultipleStops(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{Enabled: true}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)

	require.NoError(t, watcher.Start())

	// Multiple stops should not panic or error
	require.NoError(t, watcher.Stop())
	require.NoError(t, watcher.Stop())
	require.NoError(t, watcher.Stop())

	assert.False(t, watcher.IsRunning())
}

// TestScenarioWatcher_ErrorHandling tests error handling during reload.
func TestScenarioWatcher_ErrorHandling(t *testing.T) {
	t.Run("handles invalid YAML gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

		// Create valid initial file
		require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

		registry := NewScenarioRegistry()
		require.NoError(t, registry.Load(scenarioFile))

		config := HotReloadConfig{
			Enabled:        true,
			Debounce:       50 * time.Millisecond,
			WatchDirectory: false,
		}

		watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
		require.NoError(t, err)
		defer watcher.Stop()

		require.NoError(t, watcher.Start())

		// Write invalid YAML
		require.NoError(t, os.WriteFile(scenarioFile, []byte("invalid: yaml: content: ["), 0644))

		// Wait for reload attempt
		time.Sleep(200 * time.Millisecond)

		// Watcher should still be running
		assert.True(t, watcher.IsRunning())
	})
}

// TestScenarioWatcher_Integration tests full integration scenario.
func TestScenarioWatcher_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Create initial scenarios
	initialContent := `
scenarios:
  - name: "scenario-v1"
    priority: 100
    match:
      path_pattern: "^/api/v1"
    response:
      icap_status: 200
      headers:
        X-Version: "1"
`
	require.NoError(t, os.WriteFile(scenarioFile, []byte(initialContent), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:        true,
		Debounce:       100 * time.Millisecond,
		WatchDirectory: true,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)
	defer watcher.Stop()

	require.NoError(t, watcher.Start())

	// Verify initial scenario
	scenarios := registry.List()
	require.True(t, scenarioExists(scenarios, "scenario-v1"))

	// Update to v2
	updatedContent := `
scenarios:
  - name: "scenario-v2"
    priority: 100
    match:
      path_pattern: "^/api/v2"
    response:
      icap_status: 200
      headers:
        X-Version: "2"
`
	require.NoError(t, os.WriteFile(scenarioFile, []byte(updatedContent), 0644))

	// Wait for hot-reload
	time.Sleep(300 * time.Millisecond)

	// Verify updated scenario
	scenarios = registry.List()
	require.True(t, scenarioExists(scenarios, "scenario-v2"), "scenario-v2 should exist after reload")
	require.False(t, scenarioExists(scenarios, "scenario-v1"), "scenario-v1 should be replaced")
}

// scenarioExists is a helper function to check if a scenario exists.
func scenarioExists(scenarios []*Scenario, name string) bool {
	for _, s := range scenarios {
		if s.Name == name {
			return true
		}
	}
	return false
}

// TestScenarioWatcher_PendingReloads tests the pending reloads counter.
func TestScenarioWatcher_PendingReloads(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(t, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:        true,
		Debounce:       200 * time.Millisecond, // Longer debounce
		WatchDirectory: false,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(t, err)
	defer watcher.Stop()

	require.NoError(t, watcher.Start())

	// Trigger a file change
	require.NoError(t, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	// Immediately check - should have pending reload
	time.Sleep(50 * time.Millisecond)

	// Wait for debounce to complete
	time.Sleep(300 * time.Millisecond)

	// No pending reloads after debounce completes
	stats := watcher.Stats()
	assert.Equal(t, 0, stats.PendingReloads)
}

// BenchmarkScenarioWatcher_Reload benchmarks the reload operation.
func BenchmarkScenarioWatcher_Reload(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "bench-scenario"
    priority: 100
    match:
      path_pattern: "^/api/.*"
    response:
      icap_status: 200
`
	require.NoError(b, os.WriteFile(scenarioFile, []byte(yamlContent), 0644))

	registry := NewScenarioRegistry()
	require.NoError(b, registry.Load(scenarioFile))

	config := HotReloadConfig{
		Enabled:  false, // Disable auto-reload for benchmark
		Debounce: time.Millisecond,
	}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(b, err)
	defer watcher.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = watcher.TriggerReload()
	}
}

// BenchmarkScenarioWatcher_Stats benchmarks the stats operation.
func BenchmarkScenarioWatcher_Stats(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	require.NoError(b, os.WriteFile(scenarioFile, []byte("scenarios: []"), 0644))

	registry := NewScenarioRegistry()
	require.NoError(b, registry.Load(scenarioFile))

	config := HotReloadConfig{Enabled: true}

	watcher, err := NewScenarioWatcher(registry, scenarioFile, config)
	require.NoError(b, err)
	defer watcher.Stop()

	require.NoError(b, watcher.Start())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = watcher.Stats()
	}
}
