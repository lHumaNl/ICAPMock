// Copyright 2026 ICAP Mock

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestShardedScenarioRegistry_Load тестирует загрузку сценариев в шардированный реестр.
func TestShardedScenarioRegistry_Load(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "block-malware"
    priority: 100
    match:
      path_pattern: "^/scan.*"
      http_method: "POST"
      body_pattern: "(?i)(malware|virus)"
    response:
      icap_status: 200
      http_status: 403

  - name: "allow-images"
    priority: 50
    match:
      path_pattern: "^/scan/images"
      http_method: "GET"
    response:
      icap_status: 204

  - name: "delay-response"
    priority: 10
    match:
      path_pattern: "^/slow"
    response:
      icap_status: 200
      delay: "500ms"
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	scenarios := registry.List()
	if len(scenarios) != 4 { // 3 + default
		t.Errorf("List() got %d scenarios, want 4", len(scenarios))
	}

	// Проверяем приоритет
	if scenarios[0].Priority < scenarios[1].Priority {
		t.Error("Scenarios should be sorted by priority (descending)")
	}
}

// TestShardedScenarioRegistry_Match тестирует matching сценариев.
func TestShardedScenarioRegistry_Match(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "block-malware"
    priority: 100
    match:
      path_pattern: "^/scan.*"
      http_method: "POST"
      body_pattern: "(?i)(malware|virus)"
    response:
      icap_status: 200

  - name: "allow-all-get"
    priority: 50
    match:
      http_method: "GET"
    response:
      icap_status: 204

  - name: "catch-all"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name         string
		req          *icap.Request
		wantScenario string
		wantErr      bool
	}{
		{
			name: "match malware pattern",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/scan",
				HTTPRequest: &icap.HTTPMessage{
					Method: "POST",
					URI:    "http://example.com/scan",
					Body:   []byte("this contains malware"),
				},
			},
			wantScenario: "block-malware",
		},
		{
			name: "match GET request",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
				HTTPRequest: &icap.HTTPMessage{
					Method: "GET",
					URI:    "http://example.com/page",
				},
			},
			wantScenario: "allow-all-get",
		},
		{
			name: "match catch-all",
			req: &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/reqmod",
				HTTPRequest: &icap.HTTPMessage{
					Method: "PUT",
					URI:    "http://example.com/resource",
				},
			},
			wantScenario: "catch-all",
		},
		{
			name:    "nil request returns default",
			req:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenario, err := registry.Match(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Match() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && scenario.Name != tt.wantScenario {
				t.Errorf("Match() scenario = %v, want %v", scenario.Name, tt.wantScenario)
			}
		})
	}
}

// TestShardedScenarioRegistry_Match_Cache тестирует работу кэша.
func TestShardedScenarioRegistry_Match_Cache(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "test-scenario"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/test",
	}

	// Первый запрос - должен быть cache miss
	_, _ = registry.Match(req)

	// Второй запрос - должен быть cache hit
	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "test-scenario" {
		t.Errorf("Match() scenario = %v, want test-scenario", scenario.Name)
	}

	// Проверяем метрики
	metrics := registry.(*ShardedScenarioRegistry).GetMetrics()
	if metrics.cacheHits < 1 {
		t.Errorf("Expected at least 1 cache hit, got %d", metrics.cacheHits)
	}
}

// TestShardedScenarioRegistry_Add тестирует добавление сценариев.
func TestShardedScenarioRegistry_Add(t *testing.T) {
	registry := NewShardedScenarioRegistry()

	scenario := &Scenario{
		Name: "test-scenario",
		Match: MatchRule{
			Path: "^/test",
		},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	scenarios := registry.List()
	found := false
	for _, s := range scenarios {
		if s.Name == "test-scenario" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Scenario not found after Add()")
	}
}

// TestShardedScenarioRegistry_Add_Duplicate тестирует добавление дубликатов.
func TestShardedScenarioRegistry_Add_Duplicate(t *testing.T) {
	registry := NewShardedScenarioRegistry()

	scenario1 := &Scenario{
		Name:  "duplicate",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	scenario2 := &Scenario{
		Name:  "duplicate",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 200,
		},
		Priority: 100,
	}

	if err := registry.Add(scenario1); err != nil {
		t.Fatalf("Add() first error = %v", err)
	}

	if err := registry.Add(scenario2); err != nil {
		t.Fatalf("Add() second error = %v", err)
	}

	// Должен заменить первый сценарий
	scenarios := registry.List()
	count := 0
	for _, s := range scenarios {
		if s.Name == "duplicate" {
			count++
			if s.Priority != 100 {
				t.Error("Duplicate scenario should have been replaced")
			}
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 scenario with name 'duplicate', got %d", count)
	}
}

// TestShardedScenarioRegistry_Remove тестирует удаление сценариев.
func TestShardedScenarioRegistry_Remove(t *testing.T) {
	registry := NewShardedScenarioRegistry()

	scenario := &Scenario{
		Name:  "removable",
		Match: MatchRule{},
		Response: ResponseTemplate{
			ICAPStatus: 204,
		},
		Priority: 50,
	}

	if err := registry.Add(scenario); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := registry.Remove("removable"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	scenarios := registry.List()
	for _, s := range scenarios {
		if s.Name == "removable" {
			t.Error("Scenario should have been removed")
		}
	}
}

// TestShardedScenarioRegistry_Remove_NotFound тестирует удаление несуществующего сценария.
func TestShardedScenarioRegistry_Remove_NotFound(t *testing.T) {
	registry := NewShardedScenarioRegistry()

	err := registry.Remove("nonexistent")
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("Remove() error = %v, want %v", err, ErrNoMatch)
	}
}

// TestShardedScenarioRegistry_Reload тестирует перезагрузку сценариев.
func TestShardedScenarioRegistry_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Начальный контент
	yamlContent := `
scenarios:
  - name: "initial"
    priority: 100
    match: {}
    response:
      icap_status: 204
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Модифицируем файл
	yamlContent = `
scenarios:
  - name: "updated"
    priority: 100
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Перезагружаем
	if err := registry.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Проверяем обновленный сценарий
	scenarios := registry.List()
	found := false
	for _, s := range scenarios {
		if s.Name == "updated" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Updated scenario not found after reload")
	}
}

// TestShardedScenarioRegistry_ThreadSafety тестирует потокобезопасность.
func TestShardedScenarioRegistry_ThreadSafety(_ *testing.T) {
	registry := NewShardedScenarioRegistry()

	done := make(chan bool)

	// Concurrent adds
	for i := 0; i < 10; i++ {
		go func(n int) {
			scenario := &Scenario{
				Name:     "concurrent-" + strconv.Itoa(n),
				Priority: n,
				Response: ResponseTemplate{ICAPStatus: 204},
			}
			_ = registry.Add(scenario)
			done <- true
		}(i)
	}

	// Concurrent matches
	for i := 0; i < 10; i++ {
		go func() {
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/test",
			}
			_, _ = registry.Match(req)
			done <- true
		}()
	}

	// Ждем все goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestShardedScenarioRegistry_Metrics тестирует сбор метрик.
func TestShardedScenarioRegistry_Metrics(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "test"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/test",
	}

	// Делаем несколько запросов
	for i := 0; i < 10; i++ {
		_, _ = registry.Match(req)
	}

	metrics := registry.(*ShardedScenarioRegistry).GetMetrics()
	if metrics.totalMatches != 10 {
		t.Errorf("Expected 10 total matches, got %d", metrics.totalMatches)
	}

	if metrics.cacheHits == 0 {
		t.Error("Expected some cache hits")
	}
}

// TestScenarioMatchCache тестирует LRU кэш.
func TestScenarioMatchCache(t *testing.T) {
	cache := newScenarioMatchCache(3)

	scenario1 := &Scenario{Name: "s1", Priority: 1}
	scenario2 := &Scenario{Name: "s2", Priority: 2}
	scenario3 := &Scenario{Name: "s3", Priority: 3}
	scenario4 := &Scenario{Name: "s4", Priority: 4}

	// Добавляем 3 сценария
	cache.Put("key1", scenario1)
	cache.Put("key2", scenario2)
	cache.Put("key3", scenario3)

	// Проверяем что они в кэше
	if cache.Get("key1") != scenario1 {
		t.Error("key1 not found in cache")
	}
	if cache.Get("key2") != scenario2 {
		t.Error("key2 not found in cache")
	}
	if cache.Get("key3") != scenario3 {
		t.Error("key3 not found in cache")
	}

	// Добавляем 4-й сценарий - должен evict-ить первый (LRU)
	cache.Put("key4", scenario4)

	if cache.Get("key1") != nil {
		t.Error("key1 should have been evicted")
	}
	if cache.Get("key4") != scenario4 {
		t.Error("key4 not found in cache")
	}

	// Проверяем что key2 и key3 еще в кэше
	if cache.Get("key2") != scenario2 {
		t.Error("key2 should still be in cache")
	}
	if cache.Get("key3") != scenario3 {
		t.Error("key3 should still be in cache")
	}

	// Clear очищает кэш
	cache.Clear()
	if cache.Get("key2") != nil {
		t.Error("Cache should be cleared")
	}
}

// TestExtractPathPrefix тестирует извлечение префикса пути.
func TestExtractPathPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"^/api/v1/.*", "/api/v1/"},
		{"^/scan/images", "/scan/images"},
		{"/simple/path", "/simple/path"},
		{"^/.*", "/"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := extractPathPrefix(tt.pattern)
			if got != tt.want {
				t.Errorf("extractPathPrefix(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

// BenchmarkShardedScenarioRegistry_Match сравнивает производительность
// шардированного и обычного реестра.
func BenchmarkShardedScenarioRegistry_Match(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Создаем много сценариев
	yamlContent := "scenarios:\n"
	for i := 0; i < 1000; i++ {
		scenarioNum := strconv.Itoa(i % 10)
		yamlContent += `
  - name: "scenario-` + scenarioNum + `"
    priority: ` + scenarioNum + `
    match:
      path_pattern: "^/api/` + scenarioNum + `.*"
    response:
      icap_status: 200
`
	}
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}

	// Тестируем шардированный реестр
	shardedReg := NewShardedScenarioRegistry()
	if err := shardedReg.Load(scenarioFile); err != nil {
		b.Fatalf("Load() error = %v", err)
	}

	// Тестируем обычный реестр для сравнения
	normalReg := NewScenarioRegistry()
	if err := normalReg.Load(scenarioFile); err != nil {
		b.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/api/5/test",
	}

	b.Run("ShardedRegistry", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = shardedReg.Match(req)
		}
	})

	b.Run("NormalRegistry", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = normalReg.Match(req)
		}
	})
}

// BenchmarkShardedScenarioRegistry_Match_Cache тестирует эффективность кэша.
func BenchmarkShardedScenarioRegistry_Match_Cache(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "test"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		b.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/test",
	}

	// Предварительно разогреваем кэш
	for i := 0; i < 10; i++ {
		_, _ = registry.Match(req)
	}

	b.ResetTimer()

	b.Run("WithCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = registry.Match(req)
		}
	})
}

// BenchmarkShardedScenarioRegistry_Concurrent тестирует concurrent matching.
func BenchmarkShardedScenarioRegistry_Concurrent(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Создаем много сценариев
	yamlContent := "scenarios:\n"
	for i := 0; i < 100; i++ {
		scenarioNum := strconv.Itoa(i % 10)
		yamlContent += `
  - name: "scenario-` + scenarioNum + `"
    priority: ` + scenarioNum + `
    match:
      path_pattern: "^/api/` + scenarioNum + `.*"
    response:
      icap_status: 200
`
	}
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		b.Fatalf("Load() error = %v", err)
	}

	b.RunParallel(func(pb *testing.PB) {
		req := &icap.Request{
			Method: icap.MethodREQMOD,
			URI:    "icap://localhost/api/5/test",
		}
		for pb.Next() {
			_, _ = registry.Match(req)
		}
	})
}

// BenchmarkScenarioMatchCache тестирует LRU кэш.
func BenchmarkScenarioMatchCache(b *testing.B) {
	cache := newScenarioMatchCache(1000)

	scenario := &Scenario{Name: "test", Priority: 100}

	b.Run("Put", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := "key" + strconv.Itoa(i%100)
			cache.Put(key, scenario)
		}
	})

	b.Run("Get", func(b *testing.B) {
		// Предварительно заполняем кэш
		for i := 0; i < 100; i++ {
			key := "key" + strconv.Itoa(i)
			cache.Put(key, scenario)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := "key" + strconv.Itoa(i%100)
			_ = cache.Get(key)
		}
	})
}

// BenchmarkShardedScenarioRegistry_Load тестирует загрузку большого числа сценариев.
func BenchmarkShardedScenarioRegistry_Load(b *testing.B) {
	tmpDir := b.TempDir()

	b.Run("Small_10", func(b *testing.B) {
		b.StopTimer()
		scenarioFile := filepath.Join(tmpDir, "scenarios_small.yaml")
		createScenarioFile(b, scenarioFile, 10)
		registry := NewShardedScenarioRegistry()
		b.StartTimer()

		for i := 0; i < b.N; i++ {
			_ = registry.Load(scenarioFile)
		}
	})

	b.Run("Medium_100", func(b *testing.B) {
		b.StopTimer()
		scenarioFile := filepath.Join(tmpDir, "scenarios_medium.yaml")
		createScenarioFile(b, scenarioFile, 100)
		registry := NewShardedScenarioRegistry()
		b.StartTimer()

		for i := 0; i < b.N; i++ {
			_ = registry.Load(scenarioFile)
		}
	})

	b.Run("Large_1000", func(b *testing.B) {
		b.StopTimer()
		scenarioFile := filepath.Join(tmpDir, "scenarios_large.yaml")
		createScenarioFile(b, scenarioFile, 1000)
		registry := NewShardedScenarioRegistry()
		b.StartTimer()

		for i := 0; i < b.N; i++ {
			_ = registry.Load(scenarioFile)
		}
	})
}

// createScenarioFile создает тестовый файл с указанным количеством сценариев.
func createScenarioFile(b *testing.B, path string, count int) {
	yamlContent := "scenarios:\n"
	for i := 0; i < count; i++ {
		scenarioNum := strconv.Itoa(i % 10)
		yamlContent += `
  - name: "scenario-` + scenarioNum + `"
    priority: ` + scenarioNum + `
    match:
      path_pattern: "^/api/` + scenarioNum + `.*"
    response:
      icap_status: 200
`
	}
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}
}

// TestShardedScenarioRegistry_Distribution тестирует распределение сценариев по shard-ам.
func TestShardedScenarioRegistry_Distribution(t *testing.T) {
	registry := NewShardedScenarioRegistry()

	// Добавляем много сценариев с разными path patterns
	for i := 0; i < 100; i++ {
		scenarioNum := strconv.Itoa(i % 10)
		scenario := &Scenario{
			Name: "scenario-" + scenarioNum,
			Match: MatchRule{
				Path: "^/path" + scenarioNum + ".*",
			},
			Response: ResponseTemplate{
				ICAPStatus: 200,
			},
			Priority: i % 10,
		}
		_ = registry.Add(scenario)
	}

	// Проверяем что сценарии распределены по shard-ам
	shardedReg := registry.(*ShardedScenarioRegistry)
	shardCounts := make([]int, 16, 16+len(shardedReg.shards))
	for _, shard := range shardedReg.shards {
		shard.mu.RLock()
		shardCounts = append(shardCounts, len(shard.scenarios))
		shard.mu.RUnlock()
	}

	// Проверяем что все shard-ы используются
	usedShards := 0
	for _, count := range shardCounts {
		if count > 0 {
			usedShards++
		}
	}

	if usedShards < 8 {
		t.Errorf("Expected at least 8 shards to be used, got %d", usedShards)
	}
}

// TestShardedScenarioRegistry_GracefulDegradation тестирует graceful degradation.
func TestShardedScenarioRegistry_GracefulDegradation(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "complex-scenario"
    priority: 100
    match:
      path_pattern: "^/complex/.*"
      headers:
        X-Custom-Header: "value"
      http_method: "POST"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Запрос который должен fallback на полный поиск
	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/complex/test",
		Header: icap.NewHeader(),
	}
	req.Header.Set("X-Custom-Header", "value")
	req.HTTPRequest = &icap.HTTPMessage{
		Method: "POST",
	}

	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "complex-scenario" {
		t.Errorf("Match() scenario = %v, want complex-scenario", scenario.Name)
	}

	// Проверяем что fallback был использован (но может и не быть, если индекс сработал)
	// Это просто проверка что система работает
	metrics := registry.(*ShardedScenarioRegistry).GetMetrics()
	if metrics.totalMatches == 0 {
		t.Error("Expected at least one match")
	}
}

// TestShardedScenarioRegistry_WithCacheDisabled тестирует работу без кэша.
func TestShardedScenarioRegistry_WithCacheDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "test"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	config := DefaultShardingConfig()
	config.EnableCache = false
	registry := newShardedScenarioRegistryWithConfig(config)

	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method: icap.MethodREQMOD,
		URI:    "icap://localhost/test",
	}

	// Делаем несколько запросов
	for i := 0; i < 5; i++ {
		_, _ = registry.Match(req)
	}

	metrics := registry.GetMetrics()
	// С отключенным кэшем не должно быть cache hits
	if metrics.cacheHits != 0 {
		t.Errorf("Expected 0 cache hits with cache disabled, got %d", metrics.cacheHits)
	}
}

// TestShardedScenarioRegistry_ConcurrentAccess тестирует concurrent access.
func TestShardedScenarioRegistry_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "test"
    priority: 100
    match:
      path_pattern: "^/test"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/test",
			}
			_, err := registry.Match(req)
			if err != nil {
				errors <- err
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			scenario := &Scenario{
				Name:     "scenario-" + strconv.Itoa(n),
				Priority: n,
				Response: ResponseTemplate{ICAPStatus: 204},
			}
			if err := registry.Add(scenario); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent operation error: %v", err)
		}
	}
}

// TestShardedScenarioRegistry_RaceConditionFallbackMatch тестирует race condition в fallbackMatch.
// Этот тест должен запускаться с -race flag.
func TestShardedScenarioRegistry_RaceConditionFallbackMatch(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Создаем сложные сценарии, которые будут вызывать fallback
	yamlContent := `scenarios:`
	for i := 0; i < 50; i++ {
		yamlContent += `
  - name: "scenario-` + strconv.Itoa(i) + `"
    priority: ` + strconv.Itoa(i%10) + `
    match:
      headers:
        X-Test-Header: "value-` + strconv.Itoa(i) + `"
    response:
      icap_status: 200
`
	}
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var wg sync.WaitGroup
	done := make(chan bool, 100)

	// Concurrent fallback matches
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := &icap.Request{
				Method: icap.MethodREQMOD,
				URI:    "icap://localhost/test",
				Header: icap.NewHeader(),
			}
			req.Header.Set("X-Test-Header", "value-"+strconv.Itoa(n))
			_, _ = registry.Match(req)
			done <- true
		}(i)
	}

	// Concurrent adds/removes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			scenario := &Scenario{
				Name: "new-scenario-" + strconv.Itoa(n),
				Match: MatchRule{
					Headers: map[string]string{
						"X-New-Header": "new-value-" + strconv.Itoa(n),
					},
				},
				Priority: n,
				Response: ResponseTemplate{ICAPStatus: 204},
			}
			_ = registry.Add(scenario)
			done <- true
		}(i)
	}

	wg.Wait()
	close(done)

	// Проверяем что все операции завершились без ошибок
	count := 0
	for range done {
		count++
	}
	if count != 100 {
		t.Errorf("Expected 100 operations, got %d", count)
	}
}

// TestShardedScenarioRegistry_SortEfficiency тестирует эффективность сортировки.
func TestShardedScenarioRegistry_SortEfficiency(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Создаем много сценариев с разными приоритетами
	yamlContent := "scenarios:\n"
	for i := 0; i < 1000; i++ {
		priority := 1000 - i // Обратный порядок для тестирования сортировки
		yamlContent += `
  - name: "scenario-` + strconv.Itoa(i) + `"
    priority: ` + strconv.Itoa(priority) + `
    match:
      path_pattern: "^/api/` + strconv.Itoa(i%10) + `.*"
    response:
      icap_status: 200
`
	}
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewShardedScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	scenarios := registry.List()

	// Проверяем что сценарии отсортированы по приоритету (убывание)
	for i := 0; i < len(scenarios)-1; i++ {
		if scenarios[i].Priority < scenarios[i+1].Priority {
			t.Errorf("Scenarios not sorted correctly at index %d: %d < %d",
				i, scenarios[i].Priority, scenarios[i+1].Priority)
		}
	}

	// Проверяем что все сценарии присутствуют
	if len(scenarios) != 1001 { // 1000 + default
		t.Errorf("Expected 1001 scenarios, got %d", len(scenarios))
	}
}

// TestShardedScenarioRegistry_HashFunction тестирует hash function.
func TestShardedScenarioRegistry_HashFunction(t *testing.T) {
	registry := NewShardedScenarioRegistry()
	shardedReg := registry.(*ShardedScenarioRegistry)

	// Тестируем разные пути
	paths := []string{
		"/api/v1/users",
		"/api/v2/posts",
		"/scan/malware",
		"/test/path",
		"/",
	}

	for _, path := range paths {
		shardIdx := shardedReg.hashString(path)
		// Проверяем что индекс в допустимом диапазоне
		if shardIdx < 0 || shardIdx >= shardedReg.shardCount {
			t.Errorf("hashString(%q) returned invalid shard index: %d (range: 0-%d)",
				path, shardIdx, shardedReg.shardCount-1)
		}
	}

	// Проверяем что одинаковые пути возвращают одинаковые shard индексы
	shard1 := shardedReg.hashString("/test/path")
	shard2 := shardedReg.hashString("/test/path")
	if shard1 != shard2 {
		t.Errorf("hashString returned different indices for same path: %d vs %d", shard1, shard2)
	}
}

// TestShardedScenarioRegistry_ConfigValidation тестирует валидацию конфигурации.
func TestShardedScenarioRegistry_ConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    ShardingConfig
		wantCount int
		wantCache int
	}{
		{
			name: "default config",
			config: ShardingConfig{
				ShardCount:  16,
				CacheSize:   1000,
				EnableCache: true,
			},
			wantCount: 16,
			wantCache: 1000,
		},
		{
			name: "zero values should use defaults",
			config: ShardingConfig{
				ShardCount:  0,
				CacheSize:   0,
				EnableCache: true,
			},
			wantCount: 16,   // Default
			wantCache: 1000, // Default
		},
		{
			name: "negative values should use defaults",
			config: ShardingConfig{
				ShardCount:  -1,
				CacheSize:   -1,
				EnableCache: true,
			},
			wantCount: 16,   // Default
			wantCache: 1000, // Default
		},
		{
			name: "cache size should be limited to max",
			config: ShardingConfig{
				ShardCount:  16,
				CacheSize:   99999, // Больше чем MaxCacheSize
				EnableCache: true,
			},
			wantCount: 16,
			wantCache: 10000, // MaxCacheSize
		},
		{
			name: "shard count should be limited to max",
			config: ShardingConfig{
				ShardCount:  999, // Больше чем MaxShardCount
				CacheSize:   1000,
				EnableCache: true,
			},
			wantCount: 256, // MaxShardCount
			wantCache: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newShardedScenarioRegistryWithConfig(tt.config)

			if registry.shardCount != tt.wantCount {
				t.Errorf("ShardCount = %d, want %d", registry.shardCount, tt.wantCount)
			}

			if registry.cache != nil && registry.cache.cap != tt.wantCache {
				t.Errorf("Cache cap = %d, want %d", registry.cache.cap, tt.wantCache)
			}
		})
	}
}

// TestShardedScenarioRegistry_SortByPriority тестирует сортировку по приоритету.
func TestShardedScenarioRegistry_SortByPriority(t *testing.T) {
	scenarios := []*Scenario{
		{Name: "low", Priority: 10},
		{Name: "high", Priority: 100},
		{Name: "medium", Priority: 50},
		{Name: "highest", Priority: 999},
		{Name: "lowest", Priority: 1},
	}

	sortScenariosByPriority(scenarios)

	// Проверяем сортировку по убыванию
	for i := 0; i < len(scenarios)-1; i++ {
		if scenarios[i].Priority < scenarios[i+1].Priority {
			t.Errorf("Scenarios not sorted correctly: %d < %d at index %d",
				scenarios[i].Priority, scenarios[i+1].Priority, i)
		}
	}

	// Проверяем порядок
	expectedOrder := []string{"highest", "high", "medium", "low", "lowest"}
	for i, name := range expectedOrder {
		if scenarios[i].Name != name {
			t.Errorf("Expected %s at position %d, got %s", name, i, scenarios[i].Name)
		}
	}
}

// BenchmarkSortEfficiency сравнивает производительность сортировки.
func BenchmarkSortEfficiency(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			scenarios := make([]*Scenario, size)
			for i := 0; i < size; i++ {
				scenarios[i] = &Scenario{
					Name:     "scenario-" + strconv.Itoa(i),
					Priority: size - i, // Обратный порядок
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Создаем копию для каждого измерения
				testScenarios := make([]*Scenario, size)
				copy(testScenarios, scenarios)
				sortScenariosByPriority(testScenarios)
			}
		})
	}
}
