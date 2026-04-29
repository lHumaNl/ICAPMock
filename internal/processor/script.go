// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"

	"github.com/dop251/goja"
)

// ScriptSecurityConfig contains security settings for script execution.
// These settings prevent malicious scripts from accessing sensitive resources
// and limit resource usage to protect the server.
type ScriptSecurityConfig struct {
	AllowedFunctions []string
	BlockedFunctions []string
	Timeout          time.Duration
	MemoryLimitBytes int64
}

// DefaultScriptSecurityConfig returns secure defaults for script execution.
func DefaultScriptSecurityConfig() ScriptSecurityConfig {
	return ScriptSecurityConfig{
		Timeout:          5 * time.Second,
		MemoryLimitBytes: 10485760, // 10MB
		AllowedFunctions: []string{"Math.*", "String.*", "Date.*", "JSON.*", "console.*"},
		BlockedFunctions: []string{"eval", "Function", "setTimeout", "setInterval", "require", "import"},
	}
}

// ScriptProcessor processes requests by executing JavaScript scripts.
// It supports dynamic response generation based on request data.
//
// ScriptProcessor is useful for:
//   - Complex response logic that doesn't fit in static scenarios
//   - Dynamic content generation
//   - Custom request validation
//   - Advanced routing logic
//
// ScriptProcessor is thread-safe and can be used concurrently.
type ScriptProcessor struct {
	registry    storage.ScenarioRegistry
	logger      *logger.Logger
	pool        *ScriptWorkerPool
	validator   *StaticScriptValidator
	security    ScriptSecurityConfig
	timeout     time.Duration
	maxBodySize int64
	mu          sync.RWMutex
}

// NewScriptProcessor creates a new ScriptProcessor with the given registry, logger, and timeout.
//
// Parameters:
//   - registry: The scenario registry for finding script scenarios
//   - log: The logger for recording script execution (can be nil)
//   - timeout: The maximum time to allow for script execution (0 for no timeout)
//
// The processor uses the registry to find scenarios with scripts and executes them.
// It creates a worker pool with default configuration to prevent goroutine explosion.
// Security defaults are applied (timeout, memory limit, function whitelist/blacklist).
func NewScriptProcessor(registry storage.ScenarioRegistry, log *logger.Logger, timeout time.Duration) *ScriptProcessor {
	return NewScriptProcessorWithMaxBodySize(registry, log, timeout, 0)
}

// NewScriptProcessorWithMaxBodySize creates a script processor with a body read limit.
func NewScriptProcessorWithMaxBodySize(
	registry storage.ScenarioRegistry,
	log *logger.Logger,
	timeout time.Duration,
	maxBodySize int64,
) *ScriptProcessor {
	secConfig := DefaultScriptSecurityConfig()
	if timeout != 0 {
		secConfig.Timeout = timeout
	}
	return NewScriptProcessorWithSecurityAndMaxBodySize(registry, log, secConfig, maxBodySize)
}

// NewScriptProcessorWithSecurity creates a new ScriptProcessor with custom security settings.
//
// Parameters:
//   - registry: The scenario registry for finding script scenarios
//   - log: The logger for recording script execution (can be nil)
//   - security: Security configuration for script execution
//
// This allows fine-grained control over script security including:
// - Execution timeout
// - Memory limits
// - Function whitelist/blacklist
//
// Example:
//
//	sec := processor.DefaultScriptSecurityConfig()
//	sec.MemoryLimitBytes = 20 * 1024 * 1024 // 20MB
//	sec.AllowedFunctions = []string{"Math.*", "JSON.*"}
//	proc := NewScriptProcessorWithSecurity(registry, log, sec)
func NewScriptProcessorWithSecurity(registry storage.ScenarioRegistry, log *logger.Logger, security ScriptSecurityConfig) *ScriptProcessor {
	return NewScriptProcessorWithSecurityAndMaxBodySize(registry, log, security, 0)
}

// NewScriptProcessorWithSecurityAndMaxBodySize creates a script processor with security and body limits.
func NewScriptProcessorWithSecurityAndMaxBodySize(
	registry storage.ScenarioRegistry,
	log *logger.Logger,
	security ScriptSecurityConfig,
	maxBodySize int64,
) *ScriptProcessor {
	// Apply defaults for zero values
	if security.Timeout == 0 {
		security.Timeout = 5 * time.Second
	}
	if security.MemoryLimitBytes == 0 {
		security.MemoryLimitBytes = 10485760 // 10MB
	}
	if len(security.AllowedFunctions) == 0 {
		security.AllowedFunctions = []string{"Math.*", "String.*", "Date.*", "JSON.*", "console.*"}
	}
	if len(security.BlockedFunctions) == 0 {
		security.BlockedFunctions = []string{"eval", "Function", "setTimeout", "setInterval", "require", "import"}
	}

	processor := &ScriptProcessor{
		registry:    registry,
		logger:      log,
		timeout:     security.Timeout,
		security:    security,
		maxBodySize: maxBodySize,
		validator:   NewStaticScriptValidator(security.BlockedFunctions, log),
	}

	// Create worker pool with default configuration
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Logger = log
	processor.pool = NewScriptWorkerPool(cfg, processor.executeScript)

	return processor
}

// Process handles the ICAP request by matching it against scenarios with scripts.
//
// The processing process:
//  1. Finds the first scenario that matches the request
//  2. Executes the scenario's script (if present) via the worker pool
//  3. Builds and returns the response from the script's return value
//
// If no scenario matches, it returns an ErrScenarioNotFound error.
// If the script execution fails, it returns an error with details.
// If the worker pool queue is full, it returns an error.
func (p *ScriptProcessor) Process(ctx context.Context, req *icap.Request) (*icap.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scenario, err := p.registry.Match(req)
	if err != nil {
		return nil, apperrors.ErrScenarioNotFound
	}

	if scenario.Response.Script == "" {
		return nil, apperrors.ErrScenarioNotFound
	}

	if p.logger != nil {
		p.logger.Debug("executing script",
			"request_id", util.RequestIDFromContext(ctx),
			"scenario", scenario.Name,
			"method", req.Method,
			"uri", req.URI,
		)
	}

	// Execute script via worker pool
	resp, err := p.pool.Execute(ctx, req, scenario.Response.Script)
	if err != nil {
		var icapErr *apperrors.Error
		if errors.As(err, &icapErr) {
			return nil, icapErr
		}
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			fmt.Sprintf("script execution failed: %v", err),
			apperrors.ErrInternalServerError.ICAPStatus,
			err,
		)
	}

	return resp, nil
}

// executeScript executes a JavaScript script and returns the ICAP response.
func (p *ScriptProcessor) executeScript(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) { //nolint:gocyclo // script execution pipeline: validate, isolate, setup, monitor, execute, check limits
	// Step 1: Static validation BEFORE execution (defense in depth)
	if err := p.validateScript(script); err != nil {
		if p.logger != nil {
			p.logger.Error("script validation failed",
				"error", err,
				"script_length", len(script),
			)
		}
		return nil, apperrors.NewICAPError(
			apperrors.ErrInternalServerError.Code,
			"script validation failed: "+err.Error(),
			apperrors.ErrInternalServerError.ICAPStatus,
			err,
		)
	}

	// Step 2: Create isolated VM with security restrictions
	vm, err := p.createIsolatedVM()
	if err != nil {
		return nil, fmt.Errorf("failed to create isolated VM: %w", err)
	}

	// Step 3: Setup script context with request data
	err = p.setupScriptContext(vm, req)
	if err != nil {
		if errors.Is(err, icap.ErrBodyTooLarge) {
			return nil, apperrors.NewICAPError(
				apperrors.ErrBodyTooLarge.Code,
				"script body too large",
				apperrors.ErrBodyTooLarge.ICAPStatus,
				err,
			)
		}
		return nil, fmt.Errorf("failed to setup script context: %w", err)
	}

	p.mu.RLock()
	timeout := p.timeout
	memLimit := p.security.MemoryLimitBytes
	p.mu.RUnlock()

	// Step 4: Create runtime monitor for execution tracking
	monitor := NewScriptRuntimeMonitor(timeout, memLimit, 10*time.Millisecond)
	monitor.Start()

	// Step 5: Create limit enforcer in VM
	if err := p.CreateRuntimeLimitEnforcer(vm, monitor); err != nil {
		if p.logger != nil {
			p.logger.Error("failed to create runtime limit enforcer", "error", err)
		}
		return nil, fmt.Errorf("failed to create runtime limit enforcer: %w", err)
	}

	// Step 6: Execute script with context-based timeout
	scriptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan goja.Value, 1)
	errChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("script panic: %v", r)
			}
		}()
		wrappedScript := "(function() { " + script + " })()"
		val, err := vm.RunString(wrappedScript)
		if err != nil {
			errChan <- err
			return
		}
		done <- val
	}()

	select {
	case <-scriptCtx.Done():
		vm.Interrupt("execution timeout")
		if p.logger != nil {
			p.logger.Warn("script execution timeout",
				"timeout", timeout,
				"script_length", len(script),
				"elapsed", monitor.GetElapsedTime(),
				"operations", monitor.GetOperationCount(),
				"memory_used", monitor.GetMemoryUsed(),
			)
		}
		select {
		case val := <-done:
			return p.parseScriptResult(val)
		case err := <-errChan:
			return nil, fmt.Errorf("script execution error: %w", err)
		default:
			return nil, apperrors.NewICAPError(
				apperrors.ErrTimeout.Code,
				"script execution timeout exceeded",
				apperrors.ErrTimeout.ICAPStatus,
				scriptCtx.Err(),
			)
		}
	case err := <-errChan:
		// Check for security violations in error message
		if p.isSecurityViolation(err) {
			if p.logger != nil {
				p.logger.Error("script security violation",
					"error", err,
					"script_length", len(script),
				)
			}
			return nil, apperrors.NewICAPError(
				apperrors.ErrInternalServerError.Code,
				"script security violation: "+err.Error(),
				apperrors.ErrInternalServerError.ICAPStatus,
				err,
			)
		}
		return nil, fmt.Errorf("script execution error: %w", err)
	case val := <-done:
		// Final memory limit check after execution
		memUsed := monitor.GetMemoryUsed()

		if memLimit > 0 && memUsed > memLimit {
			if p.logger != nil {
				p.logger.Error("script memory limit exceeded",
					"memory_used", memUsed,
					"memory_limit", memLimit,
					"script_length", len(script),
				)
			}
			return nil, apperrors.NewICAPError(
				apperrors.ErrInternalServerError.Code,
				fmt.Sprintf("script memory limit exceeded: %d bytes (limit: %d bytes)", memUsed, memLimit),
				apperrors.ErrInternalServerError.ICAPStatus,
				nil,
			)
		}

		if p.logger != nil {
			p.logger.Debug("script execution completed",
				"elapsed", monitor.GetElapsedTime(),
				"operations", monitor.GetOperationCount(),
				"memory_used", memUsed,
			)
		}

		return p.parseScriptResult(val)
	}
}

// createIsolatedVM creates a new JavaScript VM with security restrictions.
// It blocks dangerous functions and enforces function whitelist/blacklist.
// Uses safe error returns instead of panic to prevent try-catch bypass.
func (p *ScriptProcessor) createIsolatedVM() (*goja.Runtime, error) {
	vm := goja.New()

	// Block dangerous functions using SAFE method (returns error instead of panic)
	// This prevents try-catch bypass vulnerabilities
	for _, funcName := range p.security.BlockedFunctions {
		if err := p.CreateSafeFunctionBlocker(vm, funcName); err != nil {
			return nil, fmt.Errorf("failed to block function '%s': %w", funcName, err)
		}
	}

	// Block Node.js/CommonJS globals that might be exposed
	globalObj := vm.GlobalObject()
	blockedGlobals := []string{"require", "module", "exports", "__dirname", "__filename", "process", "global", "Buffer"}
	for _, globalName := range blockedGlobals {
		_ = globalObj.Delete(globalName)
		// Also set to undefined to prevent access
		if err := vm.Set(globalName, goja.Undefined()); err != nil {
			// Log but don't fail if already undefined
			if p.logger != nil {
				p.logger.Debug("global already undefined", "name", globalName)
			}
		}
	}

	// Note: File I/O and network operations are not exposed by default in goja,
	// so we don't need to explicitly block fs.readFile, http.request, etc.
	// Only JavaScript built-in functions are available by default.

	return vm, nil
}

// isSecurityViolation checks if an error indicates a security violation.
func (p *ScriptProcessor) isSecurityViolation(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Check for common security violation indicators
	violationPatterns := []string{
		"security violation",
		"blocked function",
		"not allowed",
		"access denied",
		"forbidden",
		"restricted",
	}

	for _, pattern := range violationPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// setupScriptContext sets up the JavaScript execution environment with request data.
func (p *ScriptProcessor) setupScriptContext(vm *goja.Runtime, req *icap.Request) error {
	headersMap := make(map[string]string)
	if req.Header != nil {
		for k, v := range req.Header {
			if len(v) > 0 {
				headersMap[k] = v[0]
			}
		}
	}

	body, err := p.scriptBody(req)
	if err != nil {
		return fmt.Errorf("reading script body: %w", err)
	}
	bodyStr := string(body)

	reqData := map[string]interface{}{
		"method": req.Method,
		"uri":    req.URI,
		"header": headersMap,
		"body":   bodyStr,
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	reqObj, err := vm.RunString("(function() { return " + string(reqJSON) + "; })()")
	if err != nil {
		return fmt.Errorf("failed to create request object: %w", err)
	}

	if err := vm.Set("req", reqObj); err != nil {
		return fmt.Errorf("failed to set req variable: %w", err)
	}

	if err := vm.Set("headers", headersMap); err != nil {
		return fmt.Errorf("failed to set headers variable: %w", err)
	}

	if err := vm.Set("body", bodyStr); err != nil {
		return fmt.Errorf("failed to set body variable: %w", err)
	}

	config := map[string]interface{}{
		"timeout": p.timeout.Milliseconds(),
	}

	if err := vm.Set("config", config); err != nil {
		return fmt.Errorf("failed to set config variable: %w", err)
	}

	return nil
}

func (p *ScriptProcessor) scriptBody(req *icap.Request) ([]byte, error) {
	msg := scriptBodyMessage(req)
	if msg == nil {
		return nil, nil
	}
	p.mu.RLock()
	limit := p.maxBodySize
	p.mu.RUnlock()
	if limit <= 0 {
		return msg.GetBody()
	}
	return msg.GetBodyLimited(limit)
}

func scriptBodyMessage(req *icap.Request) *icap.HTTPMessage {
	if req.Method == icap.MethodRESPMOD && req.HTTPResponse != nil {
		return req.HTTPResponse
	}
	if req.HTTPRequest != nil {
		return req.HTTPRequest
	}
	return req.HTTPResponse
}

// parseScriptResult parses the script result and builds an ICAP response.
func (p *ScriptProcessor) parseScriptResult(val goja.Value) (*icap.Response, error) {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}

	result := val.Export()

	if result == nil {
		return icap.NewResponse(icap.StatusNoContentNeeded), nil
	}

	if resultMap, ok := result.(map[string]interface{}); ok {
		return p.buildResponseFromMap(resultMap)
	}

	if num, ok := result.(int64); ok {
		return icap.NewResponse(int(num)), nil
	}

	if num, ok := result.(float64); ok {
		return icap.NewResponse(int(num)), nil
	}

	if str, ok := result.(string); ok {
		resp := icap.NewResponse(icap.StatusOK)
		resp.Body = []byte(str)
		return resp, nil
	}

	return nil, fmt.Errorf("invalid script result type: %T", result)
}

// buildResponseFromMap builds an ICAP response from a map.
func (p *ScriptProcessor) buildResponseFromMap(result map[string]interface{}) (*icap.Response, error) {
	status := icap.StatusOK
	switch s := result["status"].(type) {
	case int64:
		status = int(s)
	case float64:
		status = int(s)
	}

	resp := icap.NewResponse(status)

	if body, ok := result["body"].(string); ok {
		resp.Body = []byte(body)
	}

	if headers, ok := result["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if valStr, ok := v.(string); ok {
				resp.SetHeader(k, valStr)
			}
		}
	}

	return resp, nil
}

// Name returns "ScriptProcessor" as the processor name.
func (p *ScriptProcessor) Name() string {
	return "ScriptProcessor"
}

// SetLogger sets the logger for the processor.
func (p *ScriptProcessor) SetLogger(log *logger.Logger) {
	if log != nil {
		p.logger = log
	}
}

// SetTimeout sets the script execution timeout.
func (p *ScriptProcessor) SetTimeout(timeout time.Duration) {
	p.mu.Lock()
	p.timeout = timeout
	p.security.Timeout = timeout
	p.mu.Unlock()
}

// SetSecurityConfig updates the security configuration for script execution.
// This allows changing security settings at runtime without recreating the processor.
//
// Parameters:
//   - security: New security configuration
//
// Example:
//
//	sec := processor.DefaultScriptSecurityConfig()
//	sec.MemoryLimitBytes = 20 * 1024 * 1024 // 20MB
//	processor.SetSecurityConfig(sec)
func (p *ScriptProcessor) SetSecurityConfig(security ScriptSecurityConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Apply defaults for zero values
	if security.Timeout == 0 {
		security.Timeout = 5 * time.Second
	}
	if security.MemoryLimitBytes == 0 {
		security.MemoryLimitBytes = 10485760 // 10MB
	}
	if len(security.AllowedFunctions) == 0 {
		security.AllowedFunctions = []string{"Math.*", "String.*", "Date.*", "JSON.*", "console.*"}
	}
	if len(security.BlockedFunctions) == 0 {
		security.BlockedFunctions = []string{"eval", "Function", "setTimeout", "setInterval", "require", "import"}
	}

	p.security = security
	p.timeout = security.Timeout
}

// GetSecurityConfig returns the current security configuration.
func (p *ScriptProcessor) GetSecurityConfig() ScriptSecurityConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.security
}

// SetMetrics sets the Prometheus metrics collector for tracking script pool metrics.
// This allows enabling or disabling metrics after the processor is created.
func (p *ScriptProcessor) SetMetrics(collector *metrics.Collector) {
	if p.pool != nil {
		p.pool.SetMetrics(collector)
	}
}

// Shutdown gracefully stops the script worker pool.
// It waits for all pending script executions to complete.
//
// Parameters:
//   - ctx: Context for shutdown timeout
//
// Returns:
//   - error: An error if shutdown is interrupted
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := processor.Shutdown(ctx); err != nil {
//	    log.Printf("shutdown error: %v", err)
//	}
func (p *ScriptProcessor) Shutdown(ctx context.Context) error {
	if p.pool != nil {
		return p.pool.Shutdown(ctx)
	}
	return nil
}
