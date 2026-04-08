// Copyright 2026 ICAP Mock

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
)

// PluginSymbol is the expected symbol name that plugins must export.
// Plugins should export a variable or function with this name.
const PluginSymbol = "Plugin"

// PluginInitSymbol is the symbol name for the plugin initialization function.
// If exported, this function is called after loading.
const PluginInitSymbol = "Init"

// InfoSymbol is the symbol name for plugin metadata.
const InfoSymbol = "PluginInfo"

// LoadError represents an error that occurred during plugin loading.
type LoadError struct {
	Internal error
	Path     string
	Phase    string
}

// Error implements the error interface.
func (e *LoadError) Error() string {
	return fmt.Sprintf("plugin load error [%s] %s: %v", e.Phase, e.Path, e.Internal)
}

// Unwrap returns the underlying error.
func (e *LoadError) Unwrap() error {
	return e.Internal
}

// Loader defines the interface for loading plugins from disk.
type Loader interface {
	// Load loads a plugin from the given path.
	// The path must point to a .so file or a directory containing plugin files.
	Load(path string) error

	// LoadDir loads all plugins from the given directory.
	LoadDir(dir string) error

	// Loaded returns a list of loaded plugin paths.
	Loaded() []string

	// Close unloads all loaded plugins.
	Close() error
}

// DynamicLoader loads Go plugins (.so files) at runtime via the plugin package.
type DynamicLoader struct {
	loaded   map[string]string
	plugins  map[string]*plugin.Plugin
	registry *Registry
	mu       sync.RWMutex
}

// LoaderOption is a function that configures a DynamicLoader.
type LoaderOption func(*DynamicLoader)

// WithRegistry sets a custom registry for the loader.
func WithRegistry(registry *Registry) LoaderOption {
	return func(l *DynamicLoader) {
		l.registry = registry
	}
}

// NewLoader creates a new DynamicLoader that loads .so plugins at runtime.
func NewLoader(opts ...LoaderOption) *DynamicLoader {
	l := &DynamicLoader{
		loaded:   make(map[string]string),
		plugins:  make(map[string]*plugin.Plugin),
		registry: globalRegistry,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Load loads a plugin from the given path.
// The path must be a .so file compiled with `go build -buildmode=plugin`.
//
// The plugin must export a symbol named "Plugin" that implements ProcessorPlugin:
//
//	// Plugin is the exported plugin instance
//	var Plugin plugin.ProcessorPlugin = &MyPlugin{}
//
// Optionally, plugins can export:
//   - "Init" function: func() error - called after loading
//   - "PluginInfo" variable: *Info - plugin metadata
func (l *DynamicLoader) Load(path string) error { //nolint:gocyclo // plugin loading requires sequential steps: stat, open, lookup, init, register
	absPath, err := filepath.Abs(path)
	if err != nil {
		return &LoadError{Path: path, Phase: "abs", Internal: err}
	}

	// Check if file exists
	if _, statErr := os.Stat(absPath); statErr != nil {
		return &LoadError{Path: path, Phase: "stat", Internal: statErr}
	}

	// Check if already loaded
	l.mu.RLock()
	_, alreadyLoaded := l.loaded[absPath]
	l.mu.RUnlock()
	if alreadyLoaded {
		return nil // Already loaded, skip
	}

	// Open plugin
	pl, err := plugin.Open(absPath)
	if err != nil {
		return &LoadError{Path: path, Phase: "open", Internal: err}
	}

	// Look up Plugin symbol
	sym, err := pl.Lookup(PluginSymbol)
	if err != nil {
		return &LoadError{Path: path, Phase: "lookup", Internal: fmt.Errorf("symbol %q not found: %w", PluginSymbol, err)}
	}

	p, ok := sym.(ProcessorPlugin)
	if !ok {
		return &LoadError{
			Path:     path,
			Phase:    "lookup",
			Internal: fmt.Errorf("symbol %q does not implement ProcessorPlugin", PluginSymbol),
		}
	}

	// Look up optional PluginInfo
	var pluginInfo *Info
	if infoSym, err := pl.Lookup(InfoSymbol); err == nil {
		if info, ok := infoSym.(*Info); ok {
			pluginInfo = info
		}
	}

	// Look up and call optional Init function
	if initSym, err := pl.Lookup(PluginInitSymbol); err == nil {
		if initFunc, ok := initSym.(func() error); ok {
			if err := initFunc(); err != nil {
				return &LoadError{Path: path, Phase: "init", Internal: err}
			}
		}
	}

	// Set plugin info if available
	if pluginInfo != nil {
		l.mu.Lock()
		if l.registry != nil {
			_ = l.registry.SetInfo(p.Name(), *pluginInfo)
		}
		l.mu.Unlock()
	}

	// Register with the registry
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.registry != nil {
		if err := l.registry.Register(p.Name(), p); err != nil {
			return &LoadError{
				Path:     path,
				Phase:    "register",
				Internal: err,
			}
		}
	}

	l.loaded[absPath] = p.Name()
	l.plugins[absPath] = pl

	return nil
}

// LoadDir loads all plugins from the given directory.
// Only files with .so extension will be loaded.
func (l *DynamicLoader) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &LoadError{
			Path:     dir,
			Phase:    "readdir",
			Internal: err,
		}
	}

	var loadErrors []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".so") {
			continue
		}

		path := filepath.Join(dir, name)
		if err := l.Load(path); err != nil {
			loadErrors = append(loadErrors, err)
		}
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("errors loading plugins: %v", loadErrors)
	}

	return nil
}

// Loaded returns a list of loaded plugin paths.
func (l *DynamicLoader) Loaded() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	paths := make([]string, 0, len(l.loaded))
	for path := range l.loaded {
		paths = append(paths, path)
	}
	return paths
}

// Close unloads all loaded plugins.
// Note: Go plugins cannot be unloaded at runtime.
// This method only removes references from the registry.
func (l *DynamicLoader) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error
	for path, name := range l.loaded {
		if p, exists := l.registry.Get(name); exists {
			if err := p.Close(); err != nil {
				errs = append(errs, fmt.Errorf("closing plugin %s: %w", name, err))
			}
			l.registry.Unregister(name)
		}
		delete(l.loaded, path)
		delete(l.plugins, path)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing plugins: %v", errs)
	}
	return nil
}

// GetLoadedPlugin returns the name of the plugin loaded from the given path.
func (l *DynamicLoader) GetLoadedPlugin(path string) (string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}

	name, exists := l.loaded[absPath]
	return name, exists
}

// Count returns the number of loaded plugins.
func (l *DynamicLoader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.loaded)
}
