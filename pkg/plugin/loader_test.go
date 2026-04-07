// Copyright 2026 ICAP Mock

package plugin_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/plugin"
)

// TestLoader tests the Loader type.
func TestLoader(t *testing.T) {
	registry := plugin.NewRegistry()
	loader := plugin.NewLoader(plugin.WithRegistry(registry))

	// Test initial state
	if len(loader.Loaded()) != 0 {
		t.Fatal("expected 0 loaded plugins initially")
	}
}

// TestLoadDirNonExistent tests loading from a non-existent directory.
func TestLoadDirNonExistent(t *testing.T) {
	registry := plugin.NewRegistry()
	loader := plugin.NewLoader(plugin.WithRegistry(registry))

	err := loader.LoadDir("/non/existent/directory")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

// TestLoaded tests the Loaded method.
func TestLoaded(t *testing.T) {
	registry := plugin.NewRegistry()
	loader := plugin.NewLoader(plugin.WithRegistry(registry))

	loaded := loader.Loaded()
	if len(loaded) != 0 {
		t.Fatal("expected 0 loaded plugins initially")
	}
}

// TestGetLoadedPlugin tests GetLoadedPlugin.
func TestGetLoadedPlugin(t *testing.T) {
	registry := plugin.NewRegistry()
	loader := plugin.NewLoader(plugin.WithRegistry(registry))

	// Test non-loaded plugin
	_, exists := loader.GetLoadedPlugin("/path/to/plugin.so")
	if exists {
		t.Fatal("expected plugin to not exist")
	}
}

// TestLoaderClose tests the Close method.
func TestLoaderClose(t *testing.T) {
	registry := plugin.NewRegistry()
	loader := plugin.NewLoader(plugin.WithRegistry(registry))

	// Close should not error on empty loader
	if err := loader.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadError tests the LoadError type.
func TestLoadError(t *testing.T) {
	err := &plugin.LoadError{
		Path:     "/path/to/plugin.so",
		Phase:    "open",
		Internal: os.ErrNotExist,
	}

	// Test Error method
	if err.Path != "/path/to/plugin.so" {
		t.Fatal("LoadError.Path should match expected value")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Fatal("LoadError.Phase should be in error message")
	}
}

// TestLoadErrorUnwrap tests Unwrap method.
func TestLoadErrorUnwrap(t *testing.T) {
	internalErr := os.ErrNotExist
	err := &plugin.LoadError{
		Path:     "/path/to/plugin.so",
		Phase:    "open",
		Internal: internalErr,
	}

	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, internalErr) {
		t.Fatal("Unwrap should return internal error")
	}
}
