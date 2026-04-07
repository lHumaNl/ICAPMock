// Copyright 2026 ICAP Mock

package storage

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	// DefaultShardCount - количество shard-ов по умолчанию.
	DefaultShardCount = 16
	// DefaultCacheSize - размер LRU кэша по умолчанию.
	DefaultCacheSize = 1000
	// MaxCacheSize - максимальный размер LRU кэша для защиты памяти.
	MaxCacheSize = 10000
	// MinShardCount - минимальное количество shard-ов.
	MinShardCount = 1
	// MaxShardCount - максимальное количество shard-ов.
	MaxShardCount = 256

	defaultScenarioName = "default"
	operationAdd        = "add"
)

// ShardingConfig содержит конфигурацию шардирования для оптимизации
// поиска сценариев с O(n/shard_count) сложностью вместо O(n).
type ShardingConfig struct {
	// ShardCount определяет количество shard-ов для индексирования.
	// Больше shard-ов = меньшие индексы, но больше памяти.
	// Default: 16
	ShardCount int `yaml:"shard_count" json:"shard_count"`

	// CacheSize определяет размер LRU кэша для частых запросов.
	// Default: 1000, Max: 10000
	CacheSize int `yaml:"cache_size" json:"cache_size"`

	// EnableCache включает LRU кэширование.
	// Default: true
	EnableCache bool `yaml:"enable_cache" json:"enable_cache"`
}

// DefaultShardingConfig возвращает конфигурацию шардирования по умолчанию.
func DefaultShardingConfig() ShardingConfig {
	return ShardingConfig{
		ShardCount:  DefaultShardCount,
		CacheSize:   DefaultCacheSize,
		EnableCache: true,
	}
}

// ShardedScenarioRegistry реализует оптимизированный реестр сценариев
// с шардированием для O(n/shard_count) поиска вместо O(n).
//
// Архитектура:
//   - Shard-ы распределяются по hash(path) % shardCount
//   - Каждый shard содержит индекс по (Method + PathPrefix)
//   - LRU кэш для частых запросов
//   - Graceful degradation при ошибках индекса
//   - Интеграция с Prometheus metrics
type ShardedScenarioRegistry struct {
	cache            *ScenarioMatchCache
	metrics          *shardingMetrics
	metricsCollector *metrics.Collector
	filePath         string
	shards           []*ScenarioShard
	config           ShardingConfig
	shardCount       int
	mu               sync.RWMutex
}

// ScenarioShard представляет один shard в шардированном индексе.
// Каждый shard содержит свои сценарии и индекс для быстрого поиска.
type ScenarioShard struct {
	index     map[string][]*Scenario
	scenarios []*Scenario
	mu        sync.RWMutex
}

// ScenarioMatchCache реализует LRU кэш для результатов matching.
// Используется для ускорения повторных запросов с теми же параметрами.
type ScenarioMatchCache struct {
	entries map[string]*cacheEntry
	// Двусвязный список для LRU eviction
	head *cacheEntry
	tail *cacheEntry
	size int
	cap  int
	mu   sync.RWMutex
}

// cacheEntry представляет одну запись в LRU кэше.
type cacheEntry struct {
	timestamp time.Time
	scenario  *Scenario
	prev      *cacheEntry
	next      *cacheEntry
	key       string
}

// shardingMetrics собирает метрики производительности шардирования (internal, atomic).
type shardingMetrics struct {
	totalMatches        atomic.Int64
	cacheHits           atomic.Int64
	cacheMisses         atomic.Int64
	fallbackMatches     atomic.Int64
	avgScenariosChecked uint64 // stores float64 bits via math.Float64bits/Float64frombits
}

// ShardingMetrics — snapshot метрик для чтения (копируемый).
type ShardingMetrics struct {
	totalMatches        int64
	cacheHits           int64
	cacheMisses         int64
	fallbackMatches     int64
	avgScenariosChecked float64
}

// NewShardedScenarioRegistry создает новый шардированный реестр сценариев.
func NewShardedScenarioRegistry() ScenarioRegistry {
	return newShardedScenarioRegistryWithConfig(DefaultShardingConfig())
}

// newShardedScenarioRegistryWithConfig создает шардированный реестр
// с указанной конфигурацией.
func newShardedScenarioRegistryWithConfig(config ShardingConfig) *ShardedScenarioRegistry {
	// Валидация ShardCount
	if config.ShardCount < MinShardCount {
		config.ShardCount = DefaultShardCount
	}
	if config.ShardCount > MaxShardCount {
		config.ShardCount = MaxShardCount
	}

	// Валидация CacheSize - защита от избыточного потребления памяти
	if config.CacheSize <= 0 {
		config.CacheSize = DefaultCacheSize
	}
	if config.CacheSize > MaxCacheSize {
		config.CacheSize = MaxCacheSize
	}

	reg := &ShardedScenarioRegistry{
		shardCount: config.ShardCount,
		config:     config,
		shards:     make([]*ScenarioShard, config.ShardCount),
		metrics:    &shardingMetrics{},
	}

	// Инициализация shard-ов
	for i := 0; i < config.ShardCount; i++ {
		reg.shards[i] = &ScenarioShard{
			scenarios: []*Scenario{DefaultScenario()},
			index:     make(map[string][]*Scenario),
		}
	}

	// Инициализация кэша если включен
	if config.EnableCache {
		reg.cache = newScenarioMatchCache(config.CacheSize)
	}

	return reg
}

// SetMetricsCollector устанавливает Prometheus collector для метрик шардирования.
// Это позволяет интегрировать метрики шардирования с Prometheus.
func (r *ShardedScenarioRegistry) SetMetricsCollector(collector *metrics.Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metricsCollector = collector
}

// newScenarioMatchCache создает новый LRU кэш.
func newScenarioMatchCache(capacity int) *ScenarioMatchCache {
	// Dummy head/tail для упрощения LRU operations
	head := &cacheEntry{}
	tail := &cacheEntry{}
	head.next = tail
	tail.prev = head

	return &ScenarioMatchCache{
		entries: make(map[string]*cacheEntry),
		head:    head,
		tail:    tail,
		cap:     capacity,
	}
}

// Load загружает сценарии из YAML файла и индексирует их по shard-ам.
func (r *ShardedScenarioRegistry) Load(path string) error {
	// Загружаем сценарии через базовый registry для валидации
	baseReg := &scenarioRegistry{}
	if err := baseReg.Load(path); err != nil {
		return err
	}

	scenarios := baseReg.List()

	// Очищаем старые индексы
	for _, shard := range r.shards {
		shard.mu.Lock()
		shard.scenarios = nil
		shard.index = make(map[string][]*Scenario)
		shard.mu.Unlock()
	}

	// Индексируем сценарии по shard-ам
	for _, s := range scenarios {
		r.indexScenario(s)
	}

	r.mu.Lock()
	r.filePath = path
	r.mu.Unlock()

	// Очищаем кэш при перезагрузке
	if r.cache != nil {
		r.cache.Clear()
	}

	return nil
}

// indexScenario добавляет сценарий в соответствующий shard и индекс.
func (r *ShardedScenarioRegistry) indexScenario(s *Scenario) {
	// Определяем shard для сценария
	shardIdx := r.getShardForScenario(s)
	shard := r.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Добавляем в список сценариев
	shard.scenarios = append(shard.scenarios, s)

	// Индексируем по method + path prefix
	key := r.buildIndexKey(s)
	shard.index[key] = append(shard.index[key], s)
}

// getShardForScenario определяет shard для сценария.
// Использует hash от path_pattern если есть, иначе от имени.
func (r *ShardedScenarioRegistry) getShardForScenario(s *Scenario) int {
	path := s.Match.Path
	if path == "" {
		// Если нет path pattern, используем имя
		return r.hashString(s.Name)
	}
	return r.hashString(path)
}

// getShardForRequest определяет shard для запроса.
// Использует hash от extracted path.
func (r *ShardedScenarioRegistry) getShardForRequest(req *icap.Request) int {
	path := extractPath(req.URI)
	return r.hashString(path)
}

// hashString вычисляет hash строки для определения shard-а.
// Использует uint32 для избежания negative index на 32-bit системах.
func (r *ShardedScenarioRegistry) hashString(s string) int {
	// Inline FNV-1a to avoid allocations (fnv.New32a() + []byte(s))
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return int(h % uint32(r.shardCount)) //nolint:gosec // safe range
}

// buildIndexKey строит ключ индекса для сценария.
// Формат: "METHOD:path_prefix".
func (r *ShardedScenarioRegistry) buildIndexKey(s *Scenario) string {
	method := s.Match.Method
	if method == "" {
		method = "*"
	}

	pathPrefix := extractPathPrefix(s.Match.Path)
	if pathPrefix == "" {
		pathPrefix = "*"
	}

	return method + ":" + pathPrefix
}

// extractPathPrefix извлекает префикс пути из regex паттерна.
// Для "^/api/v1/.*" возвращает "/api/v1/".
func extractPathPrefix(pattern string) string {
	if pattern == "" {
		return ""
	}

	// Если паттерн начинается с ^
	prefix := ""
	if len(pattern) > 0 && pattern[0] == '^' {
		prefix = pattern[1:]
	} else {
		prefix = pattern
	}

	// Ищем первый спецсимвол regex (для индексации нужны только статичные части)
	for i, ch := range prefix {
		if ch == '*' || ch == '?' || ch == '[' || ch == '(' || ch == '+' || ch == '$' || ch == '.' {
			return prefix[:i]
		}
	}

	return prefix
}

// Match находит сценарий, соответствующий запросу, с O(n/shard_count) сложностью.
// Использует шардирование, индексирование и LRU кэш для ускорения.
func (r *ShardedScenarioRegistry) Match(req *icap.Request) (*Scenario, error) {
	if req == nil {
		return nil, NewScenarioMatchError(
			"cannot match against nil request",
			nil,
		)
	}

	// Обновляем метрики
	r.metrics.totalMatches.Add(1)

	// Проверяем кэш
	if r.cache != nil {
		cacheKey := r.buildCacheKey(req)
		if cached := r.cache.Get(cacheKey); cached != nil {
			r.metrics.cacheHits.Add(1)
			// Интеграция с Prometheus metrics
			if r.metricsCollector != nil {
				r.metricsCollector.RecordScenarioShardingCacheHit()
			}
			return cached, nil
		}
		r.metrics.cacheMisses.Add(1)
		// Интеграция с Prometheus metrics
		if r.metricsCollector != nil {
			r.metricsCollector.RecordScenarioShardingCacheMiss()
		}
	}

	// Определяем shard для запроса
	shardIdx := r.getShardForRequest(req)
	shard := r.shards[shardIdx]

	// Пытаемся найти через индекс
	scenario, found := r.matchInShard(shard, req)

	// Если не нашли в shard, пробуем полный поиск по всем shard-ам
	if !found {
		scenario, found = r.fallbackMatch(req)
	}

	if found {
		// Сохраняем в кэш
		if r.cache != nil {
			cacheKey := r.buildCacheKey(req)
			r.cache.Put(cacheKey, scenario)
		}
		return scenario, nil
	}

	// Возвращаем default scenario
	defaultScenario := DefaultScenario()
	return defaultScenario, nil
}

// matchInShard ищет сценарий в указанном shard используя индекс.
func (r *ShardedScenarioRegistry) matchInShard(shard *ScenarioShard, req *icap.Request) (*Scenario, bool) {
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	// Строим ключи для поиска (с учетом wildcard)
	keys := r.buildSearchKeys(req)

	checkedCount := 0
	var bestMatch *Scenario
	var bestPriority = -9999

	// Проверяем все возможные ключи в индексе
	for _, key := range keys {
		scenarios, exists := shard.index[key]
		if !exists {
			continue
		}

		// Проверяем сценарии из индекса
		for _, s := range scenarios {
			checkedCount++
			if r.matches(s, req) && s.Priority > bestPriority {
				bestMatch = s
				bestPriority = s.Priority
			}
		}
	}

	// Обновляем метрики
	r.updateAvgScenariosChecked(checkedCount)

	if bestMatch != nil {
		return bestMatch, true
	}

	return nil, false
}

// buildSearchKeys строит ключи для поиска в индексе.
// Возвращает несколько вариантов для поддержки wildcard matching.
func (r *ShardedScenarioRegistry) buildSearchKeys(req *icap.Request) []string {
	method := req.Method
	if method == "" {
		method = "*"
	}

	path := extractPath(req.URI)

	// Строим возможные ключи: самый конкретный к наиболее общему
	keys := []string{
		method + ":" + path, // Точное совпадение
		method + ":*",       // Любой путь
		"*:" + path,         // Любой метод, точный путь
		"*:*",               // Полный wildcard
	}

	// Добавляем префиксные ключи
	for i := len(path) - 1; i > 0; i-- {
		if path[i] == '/' {
			prefix := path[:i]
			keys = append(keys, method+":"+prefix)
		}
	}

	return keys
}

// buildCacheKey строит ключ для кэша на основе запроса.
func (r *ShardedScenarioRegistry) buildCacheKey(req *icap.Request) string {
	httpMethod := ""
	if req.HTTPRequest != nil {
		httpMethod = req.HTTPRequest.Method
	}
	return req.Method + "|" + req.URI + "|" + httpMethod
}

// fallbackMatch выполняет полный поиск по всем shard-ам.
// Используется как graceful degradation когда индекс не сработал.
func (r *ShardedScenarioRegistry) fallbackMatch(req *icap.Request) (*Scenario, bool) {
	r.metrics.fallbackMatches.Add(1)

	// Интеграция с Prometheus metrics
	if r.metricsCollector != nil {
		r.metricsCollector.RecordScenarioShardingFallback()
	}

	var bestMatch *Scenario
	var bestPriority = -9999

	// Iterate directly under RLock since matches() is read-only — no copy needed
	for _, shard := range r.shards {
		shard.mu.RLock()
		for _, s := range shard.scenarios {
			if r.matches(s, req) && s.Priority > bestPriority {
				bestMatch = s
				bestPriority = s.Priority
			}
		}
		shard.mu.RUnlock()
	}

	if bestMatch != nil {
		return bestMatch, true
	}
	return nil, false
}

// matches проверяет соответствует ли сценарий запросу.
func (r *ShardedScenarioRegistry) matches(s *Scenario, req *icap.Request) bool {
	// Check ICAP method
	if s.Match.Method != "" && s.Match.Method != req.Method {
		return false
	}

	// Check path pattern
	if s.compiledPath != nil {
		path := extractPath(req.URI)
		if !s.compiledPath.MatchString(path) {
			return false
		}
	}

	// Check headers
	for key, value := range s.Match.Headers {
		h, ok := req.Header.Get(key)
		if !ok || h != value {
			return false
		}
	}

	// Check HTTP method
	if s.Match.HTTPMethod != "" {
		if req.HTTPRequest == nil {
			return false
		}
		if req.HTTPRequest.Method != s.Match.HTTPMethod {
			return false
		}
	}

	// Check body pattern
	if s.compiledBody != nil && req.HTTPRequest != nil {
		body, err := req.HTTPRequest.GetBody()
		if err != nil {
			return false
		}
		if !s.compiledBody.MatchString(string(body)) {
			return false
		}
	}

	// Check client IP
	if s.Match.ClientIP != "" {
		if !matchClientIP(s.Match.ClientIP, req.ClientIP) {
			return false
		}
	}

	return true
}

// Reload перезагружает сценарии из последнего загруженного файла.
func (r *ShardedScenarioRegistry) Reload() error {
	r.mu.RLock()
	path := r.filePath
	r.mu.RUnlock()

	if path == "" {
		return nil
	}

	return r.Load(path)
}

// List возвращает все сценарии отсортированные по приоритету.
func (r *ShardedScenarioRegistry) List() []*Scenario {
	var all []*Scenario

	// Собираем все сценарии из всех shard-ов
	for _, shard := range r.shards {
		shard.mu.RLock()
		all = append(all, shard.scenarios...)
		shard.mu.RUnlock()
	}

	// Удаляем дубликаты default scenario
	unique := make(map[string]bool)
	result := make([]*Scenario, 0, len(all))

	for _, s := range all {
		if s.Name != defaultScenarioName || !unique[defaultScenarioName] {
			result = append(result, s)
			if s.Name == defaultScenarioName {
				unique[defaultScenarioName] = true
			}
		}
	}

	// Сортируем по приоритету
	sortScenariosByPriority(result)

	return result
}

// Add добавляет сценарий в реестр.
func (r *ShardedScenarioRegistry) Add(scenario *Scenario) error {
	if scenario == nil {
		return &ScenarioError{
			Operation:  operationAdd,
			Message:    "cannot add nil scenario",
			Suggestion: "provide a valid scenario with at least a name field",
		}
	}

	// Валидация через базовый registry
	baseReg := &scenarioRegistry{}
	if err := baseReg.validateAndCompile(scenario); err != nil {
		var se *ScenarioError
		if AsScenarioError(err, &se) {
			se.Operation = operationAdd
			return se
		}
		return &ScenarioError{
			Operation:    operationAdd,
			ScenarioName: scenario.Name,
			Message:      err.Error(),
			Suggestion:   "fix the validation error before adding the scenario",
		}
	}

	// Удаляем существующий сценарий с тем же именем
	_ = r.Remove(scenario.Name)

	// Индексируем новый сценарий
	r.indexScenario(scenario)

	// Очищаем кэш
	if r.cache != nil {
		r.cache.Clear()
	}

	return nil
}

// Remove удаляет сценарий по имени.
func (r *ShardedScenarioRegistry) Remove(name string) error {
	for _, shard := range r.shards {
		shard.mu.Lock()
		// Удаляем из списка
		found := false
		for i, s := range shard.scenarios {
			if s.Name == name {
				shard.scenarios = append(shard.scenarios[:i], shard.scenarios[i+1:]...)
				found = true
				break
			}
		}

		// Удаляем из индекса
		for key, scenarios := range shard.index {
			for i, s := range scenarios {
				if s.Name == name {
					if len(scenarios) == 1 {
						delete(shard.index, key)
					} else {
						shard.index[key] = append(scenarios[:i], scenarios[i+1:]...)
					}
					break
				}
			}
		}
		shard.mu.Unlock()

		if found {
			// Очищаем кэш
			if r.cache != nil {
				r.cache.Clear()
			}
			return nil
		}
	}

	return ErrNoMatch
}

// GetMetrics возвращает snapshot метрик производительности шардирования.
func (r *ShardedScenarioRegistry) GetMetrics() ShardingMetrics {
	return ShardingMetrics{
		totalMatches:        r.metrics.totalMatches.Load(),
		cacheHits:           r.metrics.cacheHits.Load(),
		cacheMisses:         r.metrics.cacheMisses.Load(),
		fallbackMatches:     r.metrics.fallbackMatches.Load(),
		avgScenariosChecked: math.Float64frombits(atomic.LoadUint64(&r.metrics.avgScenariosChecked)),
	}
}

// updateAvgScenariosChecked обновляет среднее количество проверенных сценариев.
// Uses atomic CAS loop instead of global write lock for better concurrency.
func (r *ShardedScenarioRegistry) updateAvgScenariosChecked(count int) {
	n := float64(r.metrics.totalMatches.Load())
	for {
		oldBits := atomic.LoadUint64(&r.metrics.avgScenariosChecked)
		oldAvg := math.Float64frombits(oldBits)
		newAvg := oldAvg + (float64(count)-oldAvg)/n
		newBits := math.Float64bits(newAvg)
		if atomic.CompareAndSwapUint64(&r.metrics.avgScenariosChecked, oldBits, newBits) {
			break
		}
	}
}

// Get возвращает сценарий из кэша.
func (c *ScenarioMatchCache) Get(key string) *Scenario {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil
	}

	// Перемещаем в начало (most recently used)
	c.moveToFront(entry)

	return entry.scenario
}

// Put сохраняет сценарий в кэш.
func (c *ScenarioMatchCache) Put(key string, scenario *Scenario) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Если уже есть, обновляем
	if entry, exists := c.entries[key]; exists {
		entry.scenario = scenario
		entry.timestamp = time.Now()
		c.moveToFront(entry)
		return
	}

	// Создаем новую запись
	entry := &cacheEntry{
		key:       key,
		scenario:  scenario,
		timestamp: time.Now(),
	}
	c.entries[key] = entry
	c.addToFront(entry)
	c.size++

	// Evict если превышен размер
	if c.size > c.cap {
		c.evict()
	}
}

// Clear очищает кэш.
func (c *ScenarioMatchCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.head.next = c.tail
	c.tail.prev = c.head
	c.size = 0
}

// moveToFront перемещает запись в начало списка (most recently used).
func (c *ScenarioMatchCache) moveToFront(entry *cacheEntry) {
	// Удаляем с текущего места
	entry.prev.next = entry.next
	entry.next.prev = entry.prev

	// Добавляем в начало
	c.addToFront(entry)
}

// addToFront добавляет запись в начало списка.
func (c *ScenarioMatchCache) addToFront(entry *cacheEntry) {
	entry.next = c.head.next
	entry.prev = c.head
	c.head.next.prev = entry
	c.head.next = entry
}

// evict удаляет наименее используемую запись (tail).
func (c *ScenarioMatchCache) evict() {
	if c.tail.prev == c.head {
		return
	}

	// Удаляем последнюю запись
	toRemove := c.tail.prev
	toRemove.prev.next = c.tail
	c.tail.prev = toRemove.prev

	delete(c.entries, toRemove.key)
	c.size--
}

// sortScenariosByPriority сортирует сценарии по приоритету (убывание).
// Использует efficient sort с O(n log n) сложностью.
func sortScenariosByPriority(scenarios []*Scenario) {
	// Используем efficient sort вместо bubble sort (O(n log n) вместо O(n²))
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Priority > scenarios[j].Priority // Сортировка по убыванию
	})
}
