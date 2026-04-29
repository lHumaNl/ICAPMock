// Copyright 2026 ICAP Mock

package management

import (
	"sync"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// ManagedScenarioRegistry delegates reads and writes to an atomically swappable registry.
type ManagedScenarioRegistry struct {
	active storage.ScenarioRegistry
	mu     sync.RWMutex
}

// NewManagedScenarioRegistry wraps the provided registry for atomic replacement.
func NewManagedScenarioRegistry(active storage.ScenarioRegistry) *ManagedScenarioRegistry {
	if active == nil {
		active = storage.NewShardedScenarioRegistry()
	}
	return &ManagedScenarioRegistry{active: active}
}

// Replace atomically swaps the active registry.
func (r *ManagedScenarioRegistry) Replace(next storage.ScenarioRegistry) {
	if next == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = next
}

func (r *ManagedScenarioRegistry) current() storage.ScenarioRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// Load delegates scenario loading to the active registry.
func (r *ManagedScenarioRegistry) Load(path string) error {
	return r.current().Load(path)
}

// Match delegates scenario matching to the active registry.
func (r *ManagedScenarioRegistry) Match(req *icap.Request) (*storage.Scenario, error) {
	return r.current().Match(req)
}

// Reload delegates reload to the active registry.
func (r *ManagedScenarioRegistry) Reload() error {
	return r.current().Reload()
}

// List delegates listing to the active registry.
func (r *ManagedScenarioRegistry) List() []*storage.Scenario {
	return r.current().List()
}

// Add delegates scenario addition to the active registry.
func (r *ManagedScenarioRegistry) Add(scenario *storage.Scenario) error {
	return r.current().Add(scenario)
}

// Remove delegates scenario removal to the active registry.
func (r *ManagedScenarioRegistry) Remove(name string) error {
	return r.current().Remove(name)
}
