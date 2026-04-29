// Copyright 2026 ICAP Mock

package management

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/storage"
)

func TestRuntimeManager_LoadConfigFromPathRollback(t *testing.T) {
	validPath := writeFile(t, t.TempDir(), "valid.yaml", "server:\n  port: 1344\n")
	invalidPath := writeFile(t, t.TempDir(), "invalid.yaml", "server:\n  port: 70000\n")
	manager := NewRuntimeManager(&config.Config{}, validPath)

	err := manager.LoadConfigFromPath(context.Background(), invalidPath)

	if err == nil {
		t.Fatalf("expected invalid config error")
	}
	assertConfigValidationError(t, err, "server.port")
	if manager.CurrentConfigPath() != normalizePath(validPath) {
		t.Fatalf("current config path changed after failed load")
	}
}

func TestRuntimeManager_LoadConfigFromPathUnsupportedStaticChangeRollback(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeFile(t, oldDir, "old.yaml", scenarioFile("old", "/old"))
	writeFile(t, newDir, "new.yaml", scenarioFile("new", "/new"))
	oldPath := writeConfigFileWithPort(t, oldDir, 1344)
	newPath := writeConfigFileWithPort(t, newDir, 1345)
	registry, manager := newLoadedRuntimeManager(t, oldDir, oldPath)

	err := manager.LoadConfigFromPath(context.Background(), newPath)

	assertUnsupportedRuntimeChange(t, err)
	if manager.CurrentConfigPath() != normalizePath(oldPath) {
		t.Fatalf("current config path changed after unsupported load")
	}
	assertLoaded(t, registry.List(), "old")
	assertNotLoaded(t, registry.List(), "new")
}

func TestRuntimeManager_ReloadScenariosMultiFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "one.yaml", scenarioFile("one", "/one"))
	writeFile(t, dir, "two.yml", scenarioFile("two", "/two"))
	registry := NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(ScenarioSet{Name: "default", Dir: dir, Registry: registry})

	if err := manager.ReloadScenarios(context.Background()); err != nil {
		t.Fatalf("ReloadScenarios() error = %v", err)
	}

	assertLoaded(t, registry.List(), "one", "two")
}

func TestRuntimeManager_ReloadScenariosRollback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "valid.yaml", scenarioFile("valid", "/valid"))
	registry := NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(ScenarioSet{Name: "default", Dir: dir, Registry: registry})
	if err := manager.ReloadScenarios(context.Background()); err != nil {
		t.Fatalf("initial reload error = %v", err)
	}
	writeFile(t, dir, "bad.yaml", invalidScenarioFile())

	err := manager.ReloadScenarios(context.Background())

	if err == nil {
		t.Fatalf("expected reload error")
	}
	assertLoaded(t, registry.List(), "valid")
}

func TestRuntimeManager_ScenarioCountsAggregatesSortedSets(t *testing.T) {
	avDir := t.TempDir()
	proxyDir := t.TempDir()
	writeFile(t, avDir, "av.yaml", scenarioFile("av", "/av"))
	writeFile(t, proxyDir, "one.yaml", scenarioFile("proxy-one", "/one"))
	writeFile(t, proxyDir, "two.yaml", scenarioFile("proxy-two", "/two"))
	manager := NewRuntimeManager(&config.Config{}, "")
	manager.RegisterScenarioSet(newTestScenarioSet("proxy", proxyDir))
	manager.RegisterScenarioSet(newTestScenarioSet("av", avDir))

	if err := manager.ReloadScenarios(context.Background()); err != nil {
		t.Fatalf("ReloadScenarios() error = %v", err)
	}

	counts := manager.ScenarioCounts()
	assertScenarioSetCount(t, counts, 0, "av", 2)
	assertScenarioSetCount(t, counts, 1, "proxy", 3)
	if manager.ScenarioCount() != 5 {
		t.Fatalf("ScenarioCount() = %d, want 5", manager.ScenarioCount())
	}
}

func TestRuntimeManager_LoadConfigFromPathAppliesScenarioDir(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeFile(t, oldDir, "old.yaml", scenarioFile("old", "/old"))
	writeFile(t, newDir, "new.yaml", scenarioFile("new", "/new"))
	oldPath := writeConfigFile(t, oldDir)
	newPath := writeConfigFile(t, newDir)
	cfg := loadConfigForTest(t, oldPath)
	registry := NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := NewRuntimeManager(cfg, oldPath)
	manager.RegisterScenarioSet(ScenarioSet{Dir: oldDir, Registry: registry, Name: "default", Server: cfg.Server})

	if err := manager.ReloadScenarios(context.Background()); err != nil {
		t.Fatalf("initial ReloadScenarios() error = %v", err)
	}
	if err := manager.LoadConfigFromPath(context.Background(), newPath); err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}

	assertLoaded(t, registry.List(), "new")
	assertNotLoaded(t, registry.List(), "old")
}

func TestRuntimeManager_LoadConfigFromPathRejectsDirectory(t *testing.T) {
	manager := NewRuntimeManager(&config.Config{}, "")

	err := manager.LoadConfigFromPath(context.Background(), t.TempDir())

	assertConfigLoadErrorIs(t, err, ErrConfigFileNotRegular)
}

func TestRuntimeManager_LoadConfigFromPathRejectsOversizedFile(t *testing.T) {
	path := writeLargeConfigFile(t)
	manager := NewRuntimeManager(&config.Config{}, "")

	err := manager.LoadConfigFromPath(context.Background(), path)

	assertConfigLoadErrorIs(t, err, ErrConfigFileTooLarge)
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func writeLargeConfigFile(t *testing.T) string {
	t.Helper()
	body := strings.Repeat("#", int(MaxConfigFileBytes)+1)
	return writeFile(t, t.TempDir(), "large.yaml", body)
}

func scenarioFile(name, path string) string {
	return "scenarios:\n  - name: " + name + "\n    match:\n      method: REQMOD\n      path_pattern: \"" + path + "\"\n    response:\n      icap_status: 204\n"
}

func writeConfigFile(t *testing.T, scenariosDir string) string {
	t.Helper()
	return writeConfigFileWithPort(t, scenariosDir, 1344)
}

func writeConfigFileWithPort(t *testing.T, scenariosDir string, port int) string {
	t.Helper()
	body := "server:\n  port: " + strconv.Itoa(port) + "\nmock:\n  scenarios_dir: \"" + scenariosDir + "\"\n"
	return writeFile(t, t.TempDir(), "config.yaml", body)
}

func loadConfigForTest(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.NewLoader().Load(config.LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func invalidScenarioFile() string {
	return "scenarios:\n  - name: bad\n    match:\n      method: REQMOD\n      path_pattern: \"[\"\n    response:\n      icap_status: 204\n"
}

func newTestScenarioSet(name, dir string) ScenarioSet {
	return ScenarioSet{
		Name:     name,
		Dir:      dir,
		Registry: NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry()),
	}
}

func assertScenarioSetCount(t *testing.T, counts []ScenarioSetCount, idx int, name string, count int) {
	t.Helper()
	if len(counts) <= idx {
		t.Fatalf("counts length = %d, want index %d", len(counts), idx)
	}
	if counts[idx].Name != name || counts[idx].Count != count {
		t.Fatalf("counts[%d] = %+v, want {%s %d}", idx, counts[idx], name, count)
	}
}

func assertLoaded(t *testing.T, scenarios []*storage.Scenario, names ...string) {
	t.Helper()
	found := make(map[string]bool, len(scenarios))
	for _, scenario := range scenarios {
		found[scenario.Name] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Fatalf("scenario %q not loaded", name)
		}
	}
}

func assertNotLoaded(t *testing.T, scenarios []*storage.Scenario, names ...string) {
	t.Helper()
	found := make(map[string]bool, len(scenarios))
	for _, scenario := range scenarios {
		found[scenario.Name] = true
	}
	for _, name := range names {
		if found[name] {
			t.Fatalf("scenario %q should not be loaded", name)
		}
	}
}

func newLoadedRuntimeManager(
	t *testing.T,
	dir string,
	path string,
) (*ManagedScenarioRegistry, *RuntimeManager) {
	t.Helper()
	cfg := loadConfigForTest(t, path)
	registry := NewManagedScenarioRegistry(storage.NewShardedScenarioRegistry())
	manager := NewRuntimeManager(cfg, path)
	manager.RegisterScenarioSet(ScenarioSet{Dir: dir, Registry: registry, Name: "default", Server: cfg.Server})
	if err := manager.ReloadScenarios(context.Background()); err != nil {
		t.Fatalf("initial ReloadScenarios() error = %v", err)
	}
	return registry, manager
}

func assertUnsupportedRuntimeChange(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnsupportedRuntimeChange) {
		t.Fatalf("expected unsupported runtime change, got %v", err)
	}
}

func assertConfigValidationError(t *testing.T, err error, field string) {
	t.Helper()
	var validationErr *ConfigValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected config validation error, got %T: %v", err, err)
	}
	if !strings.Contains(validationErr.Error(), field) {
		t.Fatalf("expected validation error to contain %q, got %q", field, validationErr.Error())
	}
}

func assertConfigLoadErrorIs(t *testing.T, err, target error) {
	t.Helper()
	var loadErr *ConfigLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected config load error, got %T: %v", err, err)
	}
	if !errors.Is(loadErr.Err, target) {
		t.Fatalf("expected %v, got %v", target, loadErr.Err)
	}
}
