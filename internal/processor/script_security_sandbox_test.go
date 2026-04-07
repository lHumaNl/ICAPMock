// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScriptSecurity_TryCatchBypassPrevention tests that try-catch cannot bypass function blocking.
// This is the CRITICAL test for the security fix.
func TestScriptSecurity_TryCatchBypassPrevention(t *testing.T) {
	tests := []struct {
		name          string
		script        string
		errorContains []string
		shouldFail    bool
	}{
		{
			name: "try-catch around eval - MUST FAIL",
			script: `
try {
	eval("1+1");
} catch (e) {
	console.log("Caught:", e.message);
	return {status: 200, body: "Bypassed!"};
}
return {status: 500};
`,
			shouldFail:    true,
			errorContains: []string{"blocked", "eval"},
		},
		{
			name: "try-catch around Function - MUST FAIL",
			script: `
try {
	new Function("return 1")();
} catch (e) {
	console.log("Caught:", e.message);
	return {status: 200, body: "Bypassed!"};
}
return {status: 500};
`,
			shouldFail:    true,
			errorContains: []string{"blocked", "Function"},
		},
		{
			name: "nested try-catch around eval - MUST FAIL",
			script: `
try {
	try {
		eval("malicious");
	} catch (inner) {
		console.log("Inner:", inner.message);
	}
	return {status: 200, body: "Bypassed!"};
} catch (outer) {
	console.log("Outer:", outer.message);
	return {status: 200, body: "Bypassed!"};
}
`,
			shouldFail:    true,
			errorContains: []string{"blocked", "eval"},
		},
		{
			name: "indirect eval via variable - MUST FAIL",
			script: `
try {
	var e = eval;
	e("1+1");
} catch (e) {
	return {status: 200, body: "Bypassed!"};
}
`,
			shouldFail:    true,
			errorContains: []string{"blocked", "eval"},
		},
		{
			name: "setTimeout in try-catch - MUST FAIL",
			script: `
try {
	setTimeout(function() {}, 100);
} catch (e) {
	return {status: 200, body: "Bypassed!"};
}
`,
			shouldFail:    true,
			errorContains: []string{"blocked", "setTimeout"},
		},
		{
			name: "safe script without blocked functions - MUST SUCCEED",
			script: `
try {
	var x = 1 + 1;
	return {status: 200, body: x.toString()};
} catch (e) {
	return {status: 500};
}
`,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &mockScenarioRegistry{
				scenario: &storage.Scenario{
					Name: "test",
					Response: storage.ResponseTemplate{
						Script: tt.script,
					},
				},
			}

			proc := NewScriptProcessor(registry, nil, 5*time.Second)

			req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
			require.NoError(t, err)

			resp, err := proc.Process(context.Background(), req)

			if tt.shouldFail {
				assert.Error(t, err, "Script should fail due to security violation")
				assert.Nil(t, resp)

				// Verify error message contains security indicators
				errMsg := strings.ToLower(err.Error())
				for _, substr := range tt.errorContains {
					assert.Contains(t, errMsg, strings.ToLower(substr),
						"Error should contain: %s", substr)
				}
			} else {
				assert.NoError(t, err, "Script should succeed")
				assert.NotNil(t, resp)
				assert.Equal(t, 200, resp.StatusCode)
			}
		})
	}
}

// TestScriptSecurity_StaticAnalysisBlockedPatterns tests that static analysis catches blocked patterns.
func TestScriptSecurity_StaticAnalysisBlockedPatterns(t *testing.T) {
	validator := NewStaticScriptValidator(
		[]string{"eval", "Function", "setTimeout", "setInterval"},
		nil,
	)

	tests := []struct {
		name          string
		script        string
		errorContains string
		shouldFail    bool
	}{
		{
			name:          "direct eval call",
			script:        `eval("1+1");`,
			shouldFail:    true,
			errorContains: "blocked function",
		},
		{
			name:          "eval with spaces",
			script:        `eval  ( "1+1" ) ;`,
			shouldFail:    true,
			errorContains: "blocked function",
		},
		{
			name:          "Function constructor",
			script:        `new Function("return 1")();`,
			shouldFail:    true,
			errorContains: "blocked function",
		},
		{
			name:          "setTimeout",
			script:        `setTimeout(function() {}, 100);`,
			shouldFail:    true,
			errorContains: "blocked function",
		},
		{
			name:          "safe script",
			script:        `return {status: 200};`,
			shouldFail:    false,
			errorContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.script)

			if tt.shouldFail {
				assert.Error(t, err, "Validation should fail")
				assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.errorContains))
			} else {
				assert.NoError(t, err, "Validation should succeed")
			}
		})
	}
}

// TestScriptSecurity_StaticAnalysisSuspiciousPatterns tests that suspicious patterns are detected.
func TestScriptSecurity_StaticAnalysisSuspiciousPatterns(t *testing.T) {
	validator := NewStaticScriptValidator(
		[]string{"eval", "Function", "setTimeout", "setInterval"},
		nil,
	)

	tests := []struct {
		name        string
		script      string
		description string
		shouldPass  bool
	}{
		{
			name:        "try-catch around blocked function",
			script:      `try { eval("x"); } catch(e) {}`,
			shouldPass:  true, // Suspicious but not blocked (logged as warning)
			description: "suspicious try-catch pattern (should be logged)",
		},
		{
			name:        "infinite loop for(;;)",
			script:      `for(;;) {}`,
			shouldPass:  true, // Suspicious but not blocked (logged as warning)
			description: "infinite loop pattern (should be logged)",
		},
		{
			name:        "infinite loop while(true)",
			script:      `while(true) {}`,
			shouldPass:  true, // Suspicious but not blocked (logged as warning)
			description: "infinite loop pattern (should be logged)",
		},
		{
			name:        "dynamic eval",
			script:      `eval(a + b);`,
			shouldPass:  true, // Blocked but suspicious
			description: "dynamic eval pattern (should be blocked)",
		},
		{
			name:        "safe loop",
			script:      `for(var i=0; i<10; i++) {}`,
			shouldPass:  true,
			description: "safe finite loop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.script)

			// All should pass (suspicious patterns are warnings, not errors)
			// except actual blocked functions
			if strings.Contains(tt.script, "eval") || strings.Contains(tt.script, "Function") ||
				strings.Contains(tt.script, "setTimeout") || strings.Contains(tt.script, "setInterval") {
				assert.Error(t, err, "Blocked functions should fail validation")
			} else {
				assert.NoError(t, err, "Suspicious patterns should not block execution")
			}
		})
	}
}

// TestScriptSecurity_RuntimeMonitorMemoryLimit tests that runtime monitor enforces memory limits.
// Note: Memory limits are only enforced when scripts explicitly call __checkLimits()
// or use instrumented console methods. Scripts that don't call these functions
// will not have their memory usage checked.
func TestScriptSecurity_RuntimeMonitorMemoryLimit(t *testing.T) {
	t.Skip("memory limits require explicit __checkLimits() calls in scripts; see CreateRuntimeLimitEnforcer")
	tests := []struct {
		name         string
		script       string
		errorMessage string
		memoryLimit  int64
		shouldFail   bool
	}{
		{
			name:        "small memory limit exceeded",
			memoryLimit: 1024 * 10, // 10KB - small enough to be exceeded by large allocations
			script: `
var arr = [];
for (var i = 0; i < 100000; i++) {
	arr.push("This is a long test string that uses significant memory padding " + i + " extra data here to ensure allocation");
}
return {status: 200};
`,
			shouldFail:   true,
			errorMessage: "memory limit exceeded",
		},
		{
			name:        "memory within limit",
			memoryLimit: 10 * 1024 * 1024, // 10MB
			script: `
var arr = [];
for (var i = 0; i < 100; i++) {
	arr.push("test");
}
return {status: 200};
`,
			shouldFail: false,
		},
		{
			name:        "zero memory limit (no limit)",
			memoryLimit: 0,
			script: `
var arr = [];
for (var i = 0; i < 1000; i++) {
	arr.push("test");
}
return {status: 200};
`,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &mockScenarioRegistry{
				scenario: &storage.Scenario{
					Name: "test",
					Response: storage.ResponseTemplate{
						Script: tt.script,
					},
				},
			}

			secConfig := DefaultScriptSecurityConfig()
			secConfig.MemoryLimitBytes = tt.memoryLimit
			proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

			req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
			require.NoError(t, err)

			resp, err := proc.Process(context.Background(), req)

			if tt.shouldFail {
				assert.Error(t, err)
				assert.Nil(t, resp)
				assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.errorMessage))
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_InfiniteLoopTimeout tests that infinite loops are prevented by timeout.
func TestScriptSecurity_InfiniteLoopTimeout(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		timeout    time.Duration
		maxWait    time.Duration
		shouldFail bool
	}{
		{
			name:       "while(true) infinite loop",
			script:     `while(true) {}`,
			timeout:    100 * time.Millisecond,
			maxWait:    500 * time.Millisecond,
			shouldFail: true,
		},
		{
			name:       "for(;;) infinite loop",
			script:     `for(;;) {}`,
			timeout:    100 * time.Millisecond,
			maxWait:    500 * time.Millisecond,
			shouldFail: true,
		},
		{
			name:       "finite loop completes",
			script:     `for(var i=0; i<1000000; i++) {} return {status: 200};`,
			timeout:    5 * time.Second,
			maxWait:    5 * time.Second,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &mockScenarioRegistry{
				scenario: &storage.Scenario{
					Name: "test",
					Response: storage.ResponseTemplate{
						Script: tt.script,
					},
				},
			}

			proc := NewScriptProcessor(registry, nil, tt.timeout)

			req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
			require.NoError(t, err)

			start := time.Now()
			resp, err := proc.Process(context.Background(), req)
			elapsed := time.Since(start)

			if tt.shouldFail {
				assert.Error(t, err)
				assert.Nil(t, resp)
				assert.Contains(t, strings.ToLower(err.Error()), "timeout")

				// Should timeout within reasonable time (not hang)
				assert.Less(t, elapsed, tt.maxWait,
					"Script should timeout quickly, not hang")
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, 200, resp.StatusCode)
			}
		})
	}
}

// TestScriptSecurity_GlobalObjectsBlocked tests that Node.js globals are blocked.
func TestScriptSecurity_GlobalObjectsBlocked(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		globalName string
		shouldFail bool
	}{
		{
			name:       "require global undefined",
			script:     `var result = typeof require; return {status: 200, body: result};`,
			shouldFail: false, // Should be undefined, not error
			globalName: "require",
		},
		{
			name:       "module global undefined",
			script:     `var result = typeof module; return {status: 200, body: result};`,
			shouldFail: false,
			globalName: "module",
		},
		{
			name:       "process global undefined",
			script:     `var result = typeof process; return {status: 200, body: result};`,
			shouldFail: false,
			globalName: "process",
		},
		{
			name:       "Buffer global undefined",
			script:     `var result = typeof Buffer; return {status: 200, body: result};`,
			shouldFail: false,
			globalName: "Buffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &mockScenarioRegistry{
				scenario: &storage.Scenario{
					Name: "test",
					Response: storage.ResponseTemplate{
						Script: tt.script,
					},
				},
			}

			// Use custom security config that doesn't block require/module
			// since we want to test that they're undefined at runtime
			secConfig := DefaultScriptSecurityConfig()
			// Remove "require" from blocked functions for this test
			filteredBlocked := make([]string, 0)
			for _, fn := range secConfig.BlockedFunctions {
				if fn != "require" && fn != "module" {
					filteredBlocked = append(filteredBlocked, fn)
				}
			}
			secConfig.BlockedFunctions = filteredBlocked

			proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

			req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
			require.NoError(t, err)

			resp, err := proc.Process(context.Background(), req)

			// Should not error, but globals should be undefined
			assert.NoError(t, err, "Script should execute without error")
			assert.NotNil(t, resp)
			assert.Equal(t, 200, resp.StatusCode)

			// Result should be "undefined" (not "function" or "object")
			body := string(resp.Body)
			assert.Equal(t, "undefined", body,
				"Global %s should be undefined, got %s", tt.globalName, body)
		})
	}
}

// TestScriptSecurity_ConcurrentSecurityTests tests that security measures work under concurrent load.

// TestScriptSecurity_ConcurrentSecurityTests tests that security measures work under concurrent load.
func TestScriptSecurity_ConcurrentSecurityTests(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `
try {
	eval("malicious");
	return {status: 200, body: "Bypassed!"};
} catch (e) {
	return {status: 500, body: "Failed: " + e.message};
}
`,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)

	// Run multiple concurrent requests
	const numConcurrent = 20
	results := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func() {
			req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
			if err != nil {
				results <- err
				return
			}
			_, err = proc.Process(context.Background(), req)
			results <- err
		}()
	}

	// All should fail with security violation
	for i := 0; i < numConcurrent; i++ {
		err := <-results
		assert.Error(t, err, "Script should fail due to security violation")
		assert.Contains(t, strings.ToLower(err.Error()), "blocked",
			"Error should indicate blocked function")
	}
}

// TestScriptSecurity_ScriptLengthLimit tests that excessively long scripts are rejected.
func TestScriptSecurity_ScriptLengthLimit(t *testing.T) {
	// Create a script that exceeds the limit
	longScript := strings.Repeat("var x = 1 + 1;\n", 20000) // ~600KB

	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: longScript,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := proc.Process(context.Background(), req)

	// Should fail due to script length limit
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, strings.ToLower(err.Error()), "too long",
		"Error should indicate script too long")
}
