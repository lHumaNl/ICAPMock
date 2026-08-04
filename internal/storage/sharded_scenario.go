// Copyright 2026 ICAP Mock

package storage

import (
	"context"
	"math"
	"runtime"
	"sort"
	"strings"
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
	cache                        *ScenarioMatchCache
	metrics                      *shardingMetrics
	metricsCollector             *metrics.Collector
	bodyPattern                  BodyPatternOptions
	filePath                     string
	globalScenarios              []*Scenario
	shards                       []*ScenarioShard
	config                       ShardingConfig
	cacheDisabledForComplexMatch atomic.Bool
	generation                   atomic.Uint64
	shardCount                   int
	mu                           sync.RWMutex
	mutationMu                   sync.Mutex
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
	timestamp  time.Time
	scenario   *Scenario
	prev       *cacheEntry
	next       *cacheEntry
	key        string
	generation uint64
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
	return NewShardedScenarioRegistryWithBodyPatternOptions(DefaultBodyPatternOptions())
}

// NewShardedScenarioRegistryWithBodyPatternOptions creates a sharded registry with body_pattern limits.
func NewShardedScenarioRegistryWithBodyPatternOptions(options BodyPatternOptions) ScenarioRegistry {
	return newShardedScenarioRegistryWithConfig(DefaultShardingConfig(), options)
}

// NewShardedScenarioRegistryWithConfig creates a sharded registry with explicit sharding settings.
func NewShardedScenarioRegistryWithConfig(config ShardingConfig, options ...BodyPatternOptions) ScenarioRegistry {
	return newShardedScenarioRegistryWithConfig(config, options...)
}

// Config returns the normalized sharding configuration used by this registry.
func (r *ShardedScenarioRegistry) Config() ShardingConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// newShardedScenarioRegistryWithConfig создает шардированный реестр
// с указанной конфигурацией.
func newShardedScenarioRegistryWithConfig(config ShardingConfig, options ...BodyPatternOptions) *ShardedScenarioRegistry {
	bodyPattern := DefaultBodyPatternOptions()
	if len(options) > 0 {
		bodyPattern = options[0]
	}
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
		bodyPattern: bodyPattern.normalized(),
		shardCount:  config.ShardCount,
		config:      config,
		shards:      make([]*ScenarioShard, config.ShardCount),
		metrics:     &shardingMetrics{},
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
	baseReg := &scenarioRegistry{bodyPattern: r.bodyPattern}
	if err := baseReg.Load(path); err != nil {
		return err
	}

	scenarios := baseReg.List()
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.generation.Add(1)
	defer r.generation.Add(1)
	r.cacheDisabledForComplexMatch.Store(scenariosDisableMatchCache(scenarios))

	// Очищаем старые индексы
	for _, shard := range r.shards {
		shard.mu.Lock()
		shard.scenarios = nil
		shard.index = make(map[string][]*Scenario)
		shard.mu.Unlock()
	}
	r.mu.Lock()
	r.globalScenarios = nil
	r.mu.Unlock()

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
	if entries, ok := r.exactRouteShardEntries(s); ok {
		for shardIdx, keys := range entries {
			shard := r.shards[shardIdx]
			shard.mu.Lock()
			shard.scenarios = append(shard.scenarios, s)
			for _, key := range keys {
				shard.index[key] = append(shard.index[key], s)
			}
			shard.mu.Unlock()
		}
		return
	}
	if scenarioRequiresGlobalPriorityCheck(s) {
		r.addGlobalScenario(s)
		return
	}

	// Определяем shard для сценария
	shardIdx := r.getShardForScenario(s)
	shard := r.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Добавляем в список сценариев
	shard.scenarios = append(shard.scenarios, s)

	// Индексируем по каждому (method, path_prefix). Для multi-method сценария
	// добавляем одну и ту же запись под каждый из его методов.
	for _, key := range r.buildIndexKeys(s) {
		shard.index[key] = append(shard.index[key], s)
	}
}

func (r *ShardedScenarioRegistry) exactRouteShardEntries(s *Scenario) (map[int][]string, bool) {
	if len(s.Match.Routes) == 0 {
		return nil, false
	}
	entries := make(map[int][]string)
	seen := make(map[string]struct{})
	for _, method := range orderedRouteMethods(s.Match.Routes) {
		for _, path := range s.Match.Routes[method] {
			if path == "" || strings.HasPrefix(path, "re:") || strings.Contains(path, "{") {
				return nil, false
			}
			key := method + ":" + path
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			shardIdx := r.hashString(path)
			entries[shardIdx] = append(entries[shardIdx], key)
		}
	}
	return entries, true
}

func (r *ShardedScenarioRegistry) addGlobalScenario(s *Scenario) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalScenarios = append(r.globalScenarios, s)
}

func scenarioRequiresGlobalPriorityCheck(s *Scenario) bool {
	_, ok := shardSafePath(s)
	return !ok
}

func shardSafePath(s *Scenario) (string, bool) {
	if len(s.Match.Paths) != 1 {
		return "", false
	}
	path := s.Match.Paths[0]
	if path == "" || strings.HasPrefix(path, "re:") {
		return "", false
	}
	if strings.ContainsAny(path, "{}") {
		return "", false
	}
	return path, true
}

// getShardForScenario определяет shard для сценария.
// Использует hash от первого endpoint'а (если есть) или path_pattern, иначе от имени.
func (r *ShardedScenarioRegistry) getShardForScenario(s *Scenario) int {
	if path, ok := shardSafePath(s); ok {
		return r.hashString(path)
	}
	if len(s.Match.Paths) > 0 {
		return r.hashString(s.Match.Paths[0])
	}
	if s.Match.Path != "" {
		return r.hashString(s.Match.Path)
	}
	return r.hashString(s.Name)
}

// getShardForRequest определяет shard для запроса.
// Использует hash от extracted path.
func (r *ShardedScenarioRegistry) getShardForRequest(req *icap.Request) int {
	path := shardLookupPath(req.URI)
	return r.hashString(path)
}

func shardLookupPath(uri string) string {
	path := extractPath(uri)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		return path[:i]
	}
	return path
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

// buildIndexKeys строит набор ключей индекса — по одному на каждую пару
// (метод, endpoint-префикс). Формат: "METHOD:path_prefix". Пустой список
// методов в MatchRule означает "любой метод" (ключ "*"); пустой список
// путей — "любой путь" (префикс "*").
func (r *ShardedScenarioRegistry) buildIndexKeys(s *Scenario) []string {
	prefixes := make([]string, 0)
	switch {
	case len(s.Match.Paths) > 0:
		for _, p := range s.Match.Paths {
			pre := extractPathPrefix(p)
			if pre == "" {
				pre = "*"
			}
			prefixes = append(prefixes, pre)
		}
	case s.Match.Path != "":
		pre := extractPathPrefix(s.Match.Path)
		if pre == "" {
			pre = "*"
		}
		prefixes = append(prefixes, pre)
	default:
		prefixes = append(prefixes, "*")
	}

	methods := s.Match.Methods
	if len(methods) == 0 {
		methods = MethodList{"*"}
	}
	keys := make([]string, 0, len(prefixes)*len(methods))
	seen := make(map[string]bool, len(prefixes)*len(methods))
	for _, pre := range prefixes {
		for _, m := range methods {
			k := m + ":" + pre
			if seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

// extractPathPrefix извлекает префикс пути из regex паттерна.
// Для "^/api/v1/.*" возвращает "/api/v1/".
func extractPathPrefix(pattern string) string {
	if pattern == "" {
		return ""
	}

	// Если паттерн начинается с ^
	var prefix string
	if pattern != "" && pattern[0] == '^' {
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
func (r *ShardedScenarioRegistry) Match(ctx context.Context, req *icap.Request) (*Scenario, error) {
	if req == nil {
		return nil, NewScenarioMatchError(
			"cannot match against nil request",
			nil,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Обновляем метрики
	r.metrics.totalMatches.Add(1)
	generation, err := r.stableGeneration(ctx)
	if err != nil {
		return nil, err
	}

	cacheEnabled := r.matchCacheEnabled()
	if cached, ok := r.cachedMatch(req, cacheEnabled); ok {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return cached, nil
	}

	scenario, found, err := r.matchShardedAndFallback(ctx, req)
	if err != nil {
		return nil, err
	}
	if scenario, found, err = r.applyGlobalPriorityCandidate(ctx, req, scenario, found); err != nil {
		return nil, err
	}
	if r.generation.Load() != generation {
		return r.Match(ctx, req)
	}

	if found {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.storeMatchCache(req, scenario, cacheEnabled, generation)
		return scenario, nil
	}

	// Возвращаем default scenario
	defaultScenario := DefaultScenario()
	return defaultScenario, nil
}

func (r *ShardedScenarioRegistry) stableGeneration(ctx context.Context) (uint64, error) {
	for {
		generation := r.generation.Load()
		if generation%2 == 0 {
			return generation, nil
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		runtime.Gosched()
	}
}

func (r *ShardedScenarioRegistry) matchCacheEnabled() bool {
	return r.cache != nil && !r.cacheDisabledForComplexMatch.Load()
}

func (r *ShardedScenarioRegistry) cachedMatch(req *icap.Request, enabled bool) (*Scenario, bool) {
	if !enabled {
		return nil, false
	}
	if cached := r.cache.GetGeneration(r.buildCacheKey(req), r.generation.Load()); cached != nil {
		r.recordCacheHit()
		return cached, true
	}
	r.recordCacheMiss()
	return nil, false
}

func (r *ShardedScenarioRegistry) recordCacheHit() {
	r.metrics.cacheHits.Add(1)
	if r.metricsCollector != nil {
		r.metricsCollector.RecordScenarioShardingCacheHit()
	}
}

func (r *ShardedScenarioRegistry) recordCacheMiss() {
	r.metrics.cacheMisses.Add(1)
	if r.metricsCollector != nil {
		r.metricsCollector.RecordScenarioShardingCacheMiss()
	}
}

func (r *ShardedScenarioRegistry) storeMatchCache(
	req *icap.Request,
	scenario *Scenario,
	enabled bool,
	generation uint64,
) {
	if enabled && r.generation.Load() == generation {
		r.cache.PutGeneration(r.buildCacheKey(req), scenario, generation)
	}
}

func (r *ShardedScenarioRegistry) matchShardedAndFallback(ctx context.Context, req *icap.Request) (*Scenario, bool, error) {
	shard := r.shards[r.getShardForRequest(req)]
	scenario, found, err := r.matchInShard(ctx, shard, req)
	if err != nil || found {
		return scenario, found, err
	}
	return r.fallbackMatch(ctx, req)
}

func (r *ShardedScenarioRegistry) applyGlobalPriorityCandidate(
	ctx context.Context,
	req *icap.Request,
	current *Scenario,
	found bool,
) (*Scenario, bool, error) {
	global, globalFound, err := r.matchGlobalPriorityCandidates(ctx, req)
	if err != nil || !globalFound {
		return current, found, err
	}
	if !found || scenarioOutranks(global, current) {
		return global, true, nil
	}
	return current, found, nil
}

// matchInShard ищет сценарий в указанном shard используя индекс.
func (r *ShardedScenarioRegistry) matchInShard(ctx context.Context, shard *ScenarioShard, req *icap.Request) (*Scenario, bool, error) {
	// Строим ключи для поиска (с учетом wildcard)
	keys := r.buildSearchKeys(req)
	candidates := make([]*Scenario, 0)
	shard.mu.RLock()
	for _, key := range keys {
		candidates = append(candidates, shard.index[key]...)
	}
	shard.mu.RUnlock()
	candidates = uniqueScenarioPointers(candidates)

	checkedCount := 0
	var bestMatch *Scenario

	for _, s := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		checkedCount++
		matched, err := r.matches(ctx, s, req)
		if err != nil {
			return nil, false, err
		}
		if matched && (bestMatch == nil || scenarioOutranks(s, bestMatch)) {
			bestMatch = s
		}
	}

	// Обновляем метрики
	r.updateAvgScenariosChecked(checkedCount)

	if bestMatch != nil {
		return bestMatch, true, nil
	}

	return nil, false, nil
}

// buildSearchKeys строит ключи для поиска в индексе.
// Возвращает несколько вариантов для поддержки wildcard matching.
func (r *ShardedScenarioRegistry) buildSearchKeys(req *icap.Request) []string {
	method := req.Method
	if method == "" {
		method = "*"
	}

	path := shardLookupPath(req.URI)

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

func (r *ShardedScenarioRegistry) matchGlobalPriorityCandidates(ctx context.Context, req *icap.Request) (*Scenario, bool, error) {
	r.mu.RLock()
	candidates := append([]*Scenario(nil), r.globalScenarios...)
	r.mu.RUnlock()

	var bestMatch *Scenario
	for _, s := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		matched, err := r.matches(ctx, s, req)
		if err != nil {
			return nil, false, err
		}
		if matched && (bestMatch == nil || scenarioOutranks(s, bestMatch)) {
			bestMatch = s
		}
	}
	return bestMatch, bestMatch != nil, nil
}

// buildCacheKey строит ключ для кэша на основе запроса. Ключ включает
// дискриминаторы, по которым чаще всего матчат сценарии: ICAP метод/URI,
// метод и URI вложенного HTTP-запроса, и Content-Type сканируемого контента
// (из response у RESPMOD или request у REQMOD).
//
// Полностью 100% корректный кеш пришлось бы выключить для любого сценария
// с header-матчингом (ключей бесконечно много). Этот набор покрывает
// практические кейсы (Content-Type — самый частый дискриминатор), но если
// понадобится матч по произвольному HTTP-заголовку, кеш будет возвращать
// стейл. В таком случае отключайте его через ShardingConfig.EnableCache=false.
func (r *ShardedScenarioRegistry) buildCacheKey(req *icap.Request) string {
	var httpMethod, httpURI, contentType string
	if req.HTTPRequest != nil {
		httpMethod = req.HTTPRequest.Method
		httpURI = req.HTTPRequest.URI
	}
	if req.HTTPResponse != nil {
		if ct, ok := req.HTTPResponse.Header.Get("Content-Type"); ok {
			contentType = ct
		}
	}
	if contentType == "" && req.HTTPRequest != nil {
		if ct, ok := req.HTTPRequest.Header.Get("Content-Type"); ok {
			contentType = ct
		}
	}
	return req.Method + "|" + req.URI + "|" + httpMethod + "|" + httpURI + "|" + contentType
}

func scenariosDisableMatchCache(scenarios []*Scenario) bool {
	for _, s := range scenarios {
		if scenarioDisablesMatchCache(s) {
			return true
		}
	}
	return false
}

func scenarioDisablesMatchCache(s *Scenario) bool {
	if s == nil {
		return false
	}
	return hasDynamicMatchRule(s) || hasEndpointCaptures(s.compiledPaths) || len(s.Branches) > 0
}

func hasDynamicMatchRule(s *Scenario) bool {
	return len(s.Match.Headers) > 0 ||
		len(s.Match.HeaderContains) > 0 ||
		len(s.Match.HTTPHeaders) > 0 ||
		len(s.Match.HTTPHeaderContains) > 0 ||
		s.Match.ClientIP != "" ||
		len(s.Match.CIDRRanges) > 0 ||
		s.Match.BodyPattern != ""
}

func hasEndpointCaptures(paths []compiledEndpoint) bool {
	for _, path := range paths {
		if len(path.captures) > 0 {
			return true
		}
	}
	return false
}

// fallbackMatch выполняет полный поиск по всем shard-ам.
// Используется как graceful degradation когда индекс не сработал.
func (r *ShardedScenarioRegistry) fallbackMatch(ctx context.Context, req *icap.Request) (*Scenario, bool, error) {
	r.metrics.fallbackMatches.Add(1)

	// Интеграция с Prometheus metrics
	if r.metricsCollector != nil {
		r.metricsCollector.RecordScenarioShardingFallback()
	}

	var bestMatch *Scenario
	seen := make(map[*Scenario]struct{})
	defaultSeen := false

	for _, shard := range r.shards {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		shard.mu.RLock()
		candidates := append([]*Scenario(nil), shard.scenarios...)
		shard.mu.RUnlock()
		for _, s := range candidates {
			if s.Name == defaultScenarioName {
				if defaultSeen {
					continue
				}
				defaultSeen = true
			}
			if _, exists := seen[s]; exists {
				continue
			}
			seen[s] = struct{}{}
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			matched, err := r.matches(ctx, s, req)
			if err != nil {
				return nil, false, err
			}
			if matched && (bestMatch == nil || scenarioOutranks(s, bestMatch)) {
				bestMatch = s
			}
		}
	}

	if bestMatch != nil {
		return bestMatch, true, nil
	}
	return nil, false, nil
}

// matches проверяет соответствует ли сценарий запросу.
func (r *ShardedScenarioRegistry) matches(ctx context.Context, s *Scenario, req *icap.Request) (bool, error) {
	return matchesScenario(ctx, s, req, r.bodyPattern)
}

// Reload перезагружает сценарии из последнего загруженного файла.
func (r *ShardedScenarioRegistry) Reload() error {
	r.mu.RLock()
	path := r.filePath
	r.mu.RUnlock()

	if path == "" {
		return nil
	}
	next := newShardedScenarioRegistryWithConfig(r.config, r.bodyPattern)
	if err := next.Load(path); err != nil {
		return err
	}
	if err := ValidateExactRouteTopology(r.List(), next.List()); err != nil {
		return err
	}
	r.replaceValidated(next, path)
	return nil
}

func (r *ShardedScenarioRegistry) replaceValidated(next *ShardedScenarioRegistry, path string) {
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.generation.Add(1)
	defer r.generation.Add(1)

	for i := range r.shards {
		nextShard := next.shards[i]
		nextShard.mu.RLock()
		scenarios := append([]*Scenario(nil), nextShard.scenarios...)
		index := cloneScenarioIndex(nextShard.index)
		nextShard.mu.RUnlock()

		shard := r.shards[i]
		shard.mu.Lock()
		shard.scenarios = scenarios
		shard.index = index
		shard.mu.Unlock()
	}

	next.mu.RLock()
	global := append([]*Scenario(nil), next.globalScenarios...)
	next.mu.RUnlock()
	r.mu.Lock()
	r.globalScenarios = global
	r.filePath = path
	r.mu.Unlock()
	r.cacheDisabledForComplexMatch.Store(next.cacheDisabledForComplexMatch.Load())
	if r.cache != nil {
		r.cache.Clear()
	}
}

func cloneScenarioIndex(source map[string][]*Scenario) map[string][]*Scenario {
	clone := make(map[string][]*Scenario, len(source))
	for key, scenarios := range source {
		clone[key] = append([]*Scenario(nil), scenarios...)
	}
	return clone
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
	r.mu.RLock()
	all = append(all, r.globalScenarios...)
	r.mu.RUnlock()

	result := uniqueScenarioPointers(all)

	// Сортируем по приоритету
	sortScenariosByPriority(result)

	return result
}

func uniqueScenarioPointers(scenarios []*Scenario) []*Scenario {
	seen := make(map[*Scenario]struct{}, len(scenarios))
	unique := make([]*Scenario, 0, len(scenarios))
	defaultSeen := false
	for _, scenario := range scenarios {
		if scenario == nil {
			continue
		}
		if scenario.Name == defaultScenarioName {
			if defaultSeen {
				continue
			}
			defaultSeen = true
		}
		if _, exists := seen[scenario]; exists {
			continue
		}
		seen[scenario] = struct{}{}
		unique = append(unique, scenario)
	}
	return unique
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
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.generation.Add(1)
	defer r.generation.Add(1)

	// Валидация через базовый registry
	baseReg := &scenarioRegistry{bodyPattern: r.bodyPattern}
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
	scenario.order = scenarioOrderForAdd(r.List(), scenario.Name)

	// Удаляем существующий сценарий с тем же именем
	_ = r.remove(scenario.Name)

	// Индексируем новый сценарий
	r.indexScenario(scenario)
	if scenarioDisablesMatchCache(scenario) {
		r.cacheDisabledForComplexMatch.Store(true)
	}

	// Очищаем кэш
	if r.cache != nil {
		r.cache.Clear()
	}

	return nil
}

// Remove удаляет сценарий по имени.
func (r *ShardedScenarioRegistry) Remove(name string) error {
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.generation.Add(1)
	defer r.generation.Add(1)
	return r.remove(name)
}

func (r *ShardedScenarioRegistry) remove(name string) error {
	removed := false
	for _, shard := range r.shards {
		shard.mu.Lock()
		// Удаляем из списка. A scenario name can appear in multiple shards: the
		// initial implicit default is installed into every shard, and replacing it
		// must remove all copies before adding a configured fallback.
		for i := 0; i < len(shard.scenarios); {
			s := shard.scenarios[i]
			if s.Name == name {
				shard.scenarios = append(shard.scenarios[:i], shard.scenarios[i+1:]...)
				removed = true
				continue
			}
			i++
		}

		// Удаляем из индекса
		for key, scenarios := range shard.index {
			for i := 0; i < len(scenarios); {
				s := scenarios[i]
				if s.Name == name {
					if len(scenarios) == 1 {
						delete(shard.index, key)
						removed = true
						break
					}
					shard.index[key] = append(scenarios[:i], scenarios[i+1:]...)
					scenarios = shard.index[key]
					removed = true
					continue
				}
				i++
			}
		}
		shard.mu.Unlock()
	}
	if r.removeGlobalScenario(name) {
		removed = true
	}
	if removed {
		r.afterScenarioRemoved()
		return nil
	}

	return ErrNoMatch
}

func (r *ShardedScenarioRegistry) removeGlobalScenario(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.globalScenarios {
		if s.Name == name {
			r.globalScenarios = append(r.globalScenarios[:i], r.globalScenarios[i+1:]...)
			return true
		}
	}
	return false
}

func (r *ShardedScenarioRegistry) afterScenarioRemoved() {
	r.cacheDisabledForComplexMatch.Store(r.registryDisablesMatchCache())
	if r.cache != nil {
		r.cache.Clear()
	}
}

func (r *ShardedScenarioRegistry) registryDisablesMatchCache() bool {
	for _, shard := range r.shards {
		if r.shardDisablesMatchCache(shard) {
			return true
		}
	}
	return r.globalScenariosDisableMatchCache()
}

func (r *ShardedScenarioRegistry) globalScenariosDisableMatchCache() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return scenariosDisableMatchCache(r.globalScenarios)
}

func (r *ShardedScenarioRegistry) shardDisablesMatchCache(shard *ScenarioShard) bool {
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	return scenariosDisableMatchCache(shard.scenarios)
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
	return c.GetGeneration(key, 0)
}

// GetGeneration returns a cached scenario only for the requested registry generation.
func (c *ScenarioMatchCache) GetGeneration(key string, generation uint64) *Scenario {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil
	}
	if entry.generation != generation {
		return nil
	}

	// Перемещаем в начало (most recently used)
	c.moveToFront(entry)

	return entry.scenario
}

// Put сохраняет сценарий в кэш.
func (c *ScenarioMatchCache) Put(key string, scenario *Scenario) {
	c.PutGeneration(key, scenario, 0)
}

// PutGeneration stores a scenario tagged with its immutable registry generation.
func (c *ScenarioMatchCache) PutGeneration(key string, scenario *Scenario, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Если уже есть, обновляем
	if entry, exists := c.entries[key]; exists {
		entry.scenario = scenario
		entry.generation = generation
		entry.timestamp = time.Now()
		c.moveToFront(entry)
		return
	}

	// Создаем новую запись
	entry := &cacheEntry{
		key:        key,
		scenario:   scenario,
		generation: generation,
		timestamp:  time.Now(),
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
	sort.SliceStable(scenarios, func(i, j int) bool {
		return scenarioOutranks(scenarios[i], scenarios[j])
	})
}

func scenarioOutranks(left, right *Scenario) bool {
	if right == nil {
		return left != nil
	}
	if left == nil {
		return false
	}
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	return left.order < right.order
}

func nextScenarioOrder(scenarios []*Scenario) int {
	next := 0
	for _, scenario := range scenarios {
		if scenario != nil && scenario.order >= next {
			next = scenario.order + 1
		}
	}
	return next
}

func scenarioOrderForAdd(scenarios []*Scenario, name string) int {
	for _, scenario := range scenarios {
		if scenario != nil && scenario.Name == name {
			return scenario.order
		}
	}
	return nextScenarioOrder(scenarios)
}
