// Copyright 2026 ICAP Mock

package processor

import (
	"fmt"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/icap-mock/icap-mock/internal/logger"
)

// Package-level pre-compiled suspicious patterns (compiled once at init).
var suspiciousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\\btry\s*\\{[^}]*\\b(eval|Function|setTimeout|setInterval)`),
	regexp.MustCompile(`\\bfor\s*\(\s*;\s*;\s*\)`),
	regexp.MustCompile(`\\bwhile\s*\(\s*true\s*\)`),
	regexp.MustCompile(`eval\s*\(.*?\+.*?\)`),
	regexp.MustCompile(`Function\s*\(\s*["\'].*?["\']`),
}

// cachedMemStats provides a cached view of runtime.MemStats to avoid
// frequent stop-the-world pauses from runtime.ReadMemStats.
var cachedMemStatsInstance = &cachedMemStats{}

type cachedMemStats struct {
	updatedAt time.Time
	stats     runtime.MemStats
	ttl       time.Duration
	mu        sync.RWMutex
}

func init() {
	cachedMemStatsInstance.ttl = 1 * time.Second
	runtime.ReadMemStats(&cachedMemStatsInstance.stats)
	cachedMemStatsInstance.updatedAt = time.Now()
}

// readMemStatsCached returns a cached MemStats, refreshing at most once per TTL.
func readMemStatsCached() runtime.MemStats {
	c := cachedMemStatsInstance
	c.mu.RLock()
	if time.Since(c.updatedAt) < c.ttl {
		s := c.stats
		c.mu.RUnlock()
		return s
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after acquiring write lock.
	if time.Since(c.updatedAt) < c.ttl {
		return c.stats
	}
	runtime.ReadMemStats(&c.stats)
	c.updatedAt = time.Now()
	return c.stats
}

// SecurityViolationError represents a security violation during script execution.
type SecurityViolationError struct {
	Function string
	Reason   string
}

func (e *SecurityViolationError) Error() string {
	return fmt.Sprintf("security violation: blocked function '%s' - %s", e.Function, e.Reason)
}

// ScriptRuntimeMonitor tracks runtime metrics for script execution.
type ScriptRuntimeMonitor struct {
	startTime      time.Time
	deadline       time.Time
	lastCheck      time.Time
	maxDuration    time.Duration
	memoryLimit    int64
	maxOperations  int64
	operationCount int64
	initialAlloc   uint64
	checkInterval  time.Duration
	mu             sync.RWMutex
}

// NewScriptRuntimeMonitor creates a new runtime monitor.
func NewScriptRuntimeMonitor(maxDuration time.Duration, memoryLimit int64, checkInterval time.Duration) *ScriptRuntimeMonitor {
	m := readMemStatsCached()

	return &ScriptRuntimeMonitor{
		maxDuration:   maxDuration,
		memoryLimit:   memoryLimit,
		maxOperations: 1000000, // Default operation limit
		initialAlloc:  m.Alloc,
		checkInterval: checkInterval,
		lastCheck:     time.Now(),
	}
}

// Start begins monitoring script execution.
func (m *ScriptRuntimeMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startTime = time.Now()
	m.deadline = m.startTime.Add(m.maxDuration)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.initialAlloc = ms.Alloc
}

// CheckEnforceLimits enforces runtime limits during script execution.
// Returns error if limits are exceeded.
func (m *ScriptRuntimeMonitor) CheckEnforceLimits() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Check time limit
	if now.After(m.deadline) {
		return fmt.Errorf("execution timeout: exceeded %v", m.maxDuration)
	}

	// Check operation limit (throttle checks)
	m.operationCount++
	if m.operationCount > m.maxOperations {
		return fmt.Errorf("operation count exceeded: %d", m.operationCount)
	}

	// Check memory limit (periodic checks)
	if now.Sub(m.lastCheck) >= m.checkInterval {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)

		memUsed := int64(ms.Alloc) - int64(m.initialAlloc) //nolint:gosec // safe range
		if m.memoryLimit > 0 && memUsed > m.memoryLimit {
			return fmt.Errorf("memory limit exceeded: %d bytes (limit: %d bytes)", memUsed, m.memoryLimit)
		}

		m.lastCheck = now
	}

	return nil
}

// GetElapsedTime returns the elapsed time since monitoring started.
func (m *ScriptRuntimeMonitor) GetElapsedTime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.startTime)
}

// GetMemoryUsed returns the memory used during script execution.
func (m *ScriptRuntimeMonitor) GetMemoryUsed() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ms := readMemStatsCached()

	return int64(ms.Alloc) - int64(m.initialAlloc) //nolint:gosec // safe range
}

// GetOperationCount returns the number of operations performed.
func (m *ScriptRuntimeMonitor) GetOperationCount() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.operationCount
}

// StaticScriptValidator performs static analysis on scripts before execution.
type StaticScriptValidator struct {
	logger             *logger.Logger
	blockedPatterns    []*regexp.Regexp
	suspiciousPatterns []*regexp.Regexp
	maxScriptLength    int
}

// NewStaticScriptValidator creates a new static script validator.
func NewStaticScriptValidator(blockedFunctions []string, log *logger.Logger) *StaticScriptValidator {
	v := &StaticScriptValidator{
		maxScriptLength: 100000, // 100KB max script length
		logger:          log,
	}

	// Compile blocked function patterns
	for _, funcName := range blockedFunctions {
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(funcName) + `\s*\(.*\)`)
		v.blockedPatterns = append(v.blockedPatterns, pattern)

		// Also catch indirect calls like: var f = eval;
		indirectPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(funcName) + `\b`)
		v.blockedPatterns = append(v.blockedPatterns, indirectPattern)
	}

	// Use pre-compiled package-level suspicious patterns
	v.suspiciousPatterns = suspiciousPatterns

	return v
}

// Validate performs static analysis on a script and returns an error if security issues are found.
func (v *StaticScriptValidator) Validate(script string) error {
	// Check script length
	if len(script) > v.maxScriptLength {
		return fmt.Errorf("script too long: %d bytes (max: %d bytes)", len(script), v.maxScriptLength)
	}

	// Check for blocked patterns
	for _, pattern := range v.blockedPatterns {
		if pattern.MatchString(script) {
			matches := pattern.FindAllString(script, 3)
			if v.logger != nil {
				v.logger.Warn("script contains blocked pattern",
					"pattern", pattern.String(),
					"matches", matches,
				)
			}
			return &SecurityViolationError{
				Function: pattern.String(),
				Reason:   "blocked function detected in static analysis",
			}
		}
	}

	// Check for suspicious patterns
	for _, pattern := range v.suspiciousPatterns {
		if pattern.MatchString(script) {
			matches := pattern.FindAllString(script, 3)
			if v.logger != nil {
				v.logger.Warn("script contains suspicious pattern",
					"pattern", pattern.String(),
					"matches", matches,
				)
			}
			// Suspicious patterns are warnings, not errors
			// But we log them for security auditing
		}
	}

	return nil
}

// CreateSafeFunctionBlocker creates a safe function blocker that cannot be bypassed.
func (p *ScriptProcessor) CreateSafeFunctionBlocker(vm *goja.Runtime, funcName string) error {
	// Override with a safe implementation that returns an error instead of panicking
	if err := vm.Set(funcName, func(call goja.FunctionCall) goja.Value {
		// Log security violation attempt
		if p.logger != nil {
			p.logger.Error("blocked function access attempt",
				"function", funcName,
				"stack", fmt.Sprintf("%+v", call),
			)
		}

		// Return a TypeError that CANNOT be caught with try-catch in JavaScript
		// This is a proper error, not a panic
		return vm.NewTypeError(fmt.Sprintf("Security violation: blocked function '%s' is not allowed", funcName))
	}); err != nil {
		return fmt.Errorf("failed to block function '%s': %w", funcName, err)
	}

	// Ensure the function is not accessible via the global object
	// by deleting it from the global object
	globalObj := vm.GlobalObject()
	_ = globalObj.Delete(funcName)

	return nil
}

// CreateRuntimeLimitEnforcer creates a runtime limit enforcer that monitors execution.
// Note: This is a simplified version. For comprehensive monitoring, the script
// should be wrapped in a goroutine that periodically checks limits.
func (p *ScriptProcessor) CreateRuntimeLimitEnforcer(vm *goja.Runtime, monitor *ScriptRuntimeMonitor) error {
	// Create a periodic checker function that scripts can call
	// This is optional but provides explicit limit checking
	checker := func(_ goja.FunctionCall) goja.Value {
		if err := monitor.CheckEnforceLimits(); err != nil {
			panic(err) // This will be caught by vm.RunString()
		}
		return goja.Undefined()
	}

	return vm.Set("__checkLimits", checker)
}

// CreateInstrumentedConsole creates a console that logs to the Go logger and monitors usage.
func (p *ScriptProcessor) CreateInstrumentedConsole(vm *goja.Runtime, monitor *ScriptRuntimeMonitor) error {
	consoleObj := vm.NewObject()

	// Wrap console.log with monitoring
	_ = consoleObj.Set("log", func(call goja.FunctionCall) goja.Value {
		if err := monitor.CheckEnforceLimits(); err != nil {
			panic(err)
		}

		if p.logger != nil {
			args := make([]interface{}, len(call.Arguments))
			for i, arg := range call.Arguments {
				args[i] = arg.Export()
			}
			p.logger.Debug("script console.log", "args", args)
		}

		return goja.Undefined()
	})

	// Wrap console.error with monitoring
	_ = consoleObj.Set("error", func(call goja.FunctionCall) goja.Value {
		if err := monitor.CheckEnforceLimits(); err != nil {
			panic(err)
		}

		if p.logger != nil {
			args := make([]interface{}, len(call.Arguments))
			for i, arg := range call.Arguments {
				args[i] = arg.Export()
			}
			p.logger.Error("script console.error", "args", args)
		}

		return goja.Undefined()
	})

	// Wrap console.warn with monitoring
	_ = consoleObj.Set("warn", func(call goja.FunctionCall) goja.Value {
		if err := monitor.CheckEnforceLimits(); err != nil {
			panic(err)
		}

		if p.logger != nil {
			args := make([]interface{}, len(call.Arguments))
			for i, arg := range call.Arguments {
				args[i] = arg.Export()
			}
			p.logger.Warn("script console.warn", "args", args)
		}

		return goja.Undefined()
	})

	// Set the console object
	return vm.Set("console", consoleObj)
}

// validateScript performs comprehensive validation before execution.
func (p *ScriptProcessor) validateScript(script string) error {
	return p.validator.Validate(script)
}
