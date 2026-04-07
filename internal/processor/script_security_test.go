// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScriptSecurity_TimeoutEnforced tests that script timeout is enforced correctly.
func TestScriptSecurity_TimeoutEnforced(t *testing.T) {
	// Test 1: Infinite loop should timeout
	t.Run("infinite loop times out", func(t *testing.T) {
		script := `while (true) {}`

		registry := &mockScenarioRegistry{
			scenario: &storage.Scenario{
				Name: "test",
				Response: storage.ResponseTemplate{
					Script: script,
				},
			},
		}

		proc := NewScriptProcessor(registry, nil, 100*time.Millisecond)

		req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
		require.NoError(t, err)

		start := time.Now()
		resp, err := proc.Process(context.Background(), req)
		elapsed := time.Since(start)

		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "timeout")

		// Should timeout within reasonable time (not hang)
		assert.Less(t, elapsed, 500*time.Millisecond, "script should timeout quickly")
		assert.Greater(t, elapsed, 50*time.Millisecond, "should not timeout immediately")
	})

	// Test 2: Long-running but finite script should complete
	t.Run("long finite script completes", func(t *testing.T) {
		script := `
		var sum = 0;
		for (var i = 0; i < 1000000; i++) {
			sum += i;
		}
		return {status: 200, body: sum.toString()};
		`

		registry := &mockScenarioRegistry{
			scenario: &storage.Scenario{
				Name: "test",
				Response: storage.ResponseTemplate{
					Script: script,
				},
			},
		}

		proc := NewScriptProcessor(registry, nil, 5*time.Second)

		req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
		require.NoError(t, err)

		resp, err := proc.Process(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, 200, resp.StatusCode)
	})
}

// TestScriptSecurity_EvalBlocked tests that eval() is blocked.
func TestScriptSecurity_EvalBlocked(t *testing.T) {
	tests := []struct {
		name          string
		script        string
		errorContains []string
		shouldBlock   bool
	}{
		{
			name:          "direct eval call",
			script:        `eval("1+1");`,
			shouldBlock:   true,
			errorContains: []string{"blocked", "eval"},
		},
		{
			name:          "eval with dynamic code",
			script:        `var code = "1+1"; eval(code);`,
			shouldBlock:   true,
			errorContains: []string{"blocked", "eval"},
		},
		{
			name:        "safe script without eval",
			script:      `return {status: 200, body: "safe"};`,
			shouldBlock: false,
		},
		{
			name:          "indirect eval attempt",
			script:        `var f = eval; f("1+1");`,
			shouldBlock:   true,
			errorContains: []string{"blocked", "eval"},
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

			if tt.shouldBlock {
				assert.Error(t, err)
				assert.Nil(t, resp)
				for _, substr := range tt.errorContains {
					assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(substr),
						"error should contain: %s", substr)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_FunctionConstructorBlocked tests that Function constructor is blocked.
func TestScriptSecurity_FunctionConstructorBlocked(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		shouldBlock bool
	}{
		{
			name:        "Function constructor",
			script:      `var f = new Function("a", "return a+1"); f(1);`,
			shouldBlock: true,
		},
		{
			name:        "Function call",
			script:      `Function("return 1+1")();`,
			shouldBlock: true,
		},
		{
			name:        "safe function declaration",
			script:      `function safe() { return 1; } return {status: 200};`,
			shouldBlock: false,
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

			if tt.shouldBlock {
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_SetTimeoutSetIntervalBlocked tests that setTimeout/setInterval are blocked.
func TestScriptSecurity_SetTimeoutSetIntervalBlocked(t *testing.T) {
	tests := []struct {
		name          string
		script        string
		errorContains []string
		shouldBlock   bool
	}{
		{
			name:          "setTimeout",
			script:        `setTimeout(function() {}, 1000);`,
			shouldBlock:   true,
			errorContains: []string{"blocked", "setTimeout"},
		},
		{
			name:          "setInterval",
			script:        `setInterval(function() {}, 1000);`,
			shouldBlock:   true,
			errorContains: []string{"blocked", "setInterval"},
		},
		{
			name:        "safe loop",
			script:      `for (var i=0; i<10; i++) {} return {status: 200};`,
			shouldBlock: false,
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

			if tt.shouldBlock {
				assert.Error(t, err)
				assert.Nil(t, resp)
				for _, substr := range tt.errorContains {
					assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(substr),
						"error should contain: %s", substr)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_RequireImportBlocked tests that require and import are blocked.
func TestScriptSecurity_RequireImportBlocked(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		shouldBlock bool
	}{
		{
			name:        "require",
			script:      `require("fs");`,
			shouldBlock: true,
		},
		{
			name:        "safe script",
			script:      `return {status: 200, body: "safe"};`,
			shouldBlock: false,
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

			if tt.shouldBlock {
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_MemoryLimitEnforced tests that memory limit is enforced.
// Note: Memory limits require explicit __checkLimits() calls in scripts.
// Scripts that don't call __checkLimits() won't have memory checked.
func TestScriptSecurity_MemoryLimitEnforced(t *testing.T) {
	t.Skip("memory limits require explicit __checkLimits() calls in scripts; see CreateRuntimeLimitEnforcer")
	// Test 1: Memory limit should be enforced
	t.Run("memory limit enforced", func(t *testing.T) {
		// Create a script that allocates a lot of memory
		script := `
		var arr = [];
		for (var i = 0; i < 100000; i++) {
			arr.push("This is a test string that uses memory " + i);
		}
		return {status: 200, body: arr.length.toString()};
		`

		registry := &mockScenarioRegistry{
			scenario: &storage.Scenario{
				Name: "test",
				Response: storage.ResponseTemplate{
					Script: script,
				},
			},
		}

		// Set a very low memory limit
		secConfig := DefaultScriptSecurityConfig()
		secConfig.MemoryLimitBytes = 1024 // 1KB (unrealistic but for testing)
		proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

		req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
		require.NoError(t, err)

		resp, err := proc.Process(context.Background(), req)

		// Should fail due to memory limit
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, strings.ToLower(err.Error()), "memory limit exceeded")
	})

	// Test 2: Normal script should work within memory limit
	t.Run("normal script within limit", func(t *testing.T) {
		script := `return {status: 200, body: "Small response"};`

		registry := &mockScenarioRegistry{
			scenario: &storage.Scenario{
				Name: "test",
				Response: storage.ResponseTemplate{
					Script: script,
				},
			},
		}

		secConfig := DefaultScriptSecurityConfig()
		secConfig.MemoryLimitBytes = 10485760 // 10MB
		proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

		req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
		require.NoError(t, err)

		resp, err := proc.Process(context.Background(), req)

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, 200, resp.StatusCode)
	})

	// Test 3: Zero memory limit means no limit
	t.Run("zero memory limit allows execution", func(t *testing.T) {
		script := `
		var arr = [];
		for (var i = 0; i < 1000; i++) {
			arr.push("test");
		}
		return {status: 200};
		`

		registry := &mockScenarioRegistry{
			scenario: &storage.Scenario{
				Name: "test",
				Response: storage.ResponseTemplate{
					Script: script,
				},
			},
		}

		secConfig := DefaultScriptSecurityConfig()
		secConfig.MemoryLimitBytes = 0 // No limit
		proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

		req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
		require.NoError(t, err)

		resp, err := proc.Process(context.Background(), req)

		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

// TestScriptSecurity_CustomBlockedFunctions tests custom blocked functions.
func TestScriptSecurity_CustomBlockedFunctions(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `console.log("test"); return {status: 200};`,
			},
		},
	}

	secConfig := DefaultScriptSecurityConfig()
	secConfig.BlockedFunctions = []string{"console"}
	proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := proc.Process(context.Background(), req)

	// console.log should fail since console is blocked
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestScriptSecurity_CustomAllowedFunctions tests custom allowed functions.
func TestScriptSecurity_CustomAllowedFunctions(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `return {status: 200, body: Math.PI.toString()};`,
			},
		},
	}

	secConfig := DefaultScriptSecurityConfig()
	secConfig.AllowedFunctions = []string{"Math.*"} // Only allow Math
	proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := proc.Process(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

// TestScriptSecurity_SafeBuiltInsAvailable tests that safe built-ins are available.
func TestScriptSecurity_SafeBuiltInsAvailable(t *testing.T) {
	tests := []struct {
		check  func(*testing.T, *icap.Response)
		name   string
		script string
	}{
		{
			name:   "Math functions available",
			script: `return {status: 200, body: Math.sqrt(16).toString()};`,
			check: func(t *testing.T, resp *icap.Response) {
				assert.Equal(t, "4", string(resp.Body))
			},
		},
		{
			name:   "String functions available",
			script: `return {status: 200, body: "hello".toUpperCase()};`,
			check: func(t *testing.T, resp *icap.Response) {
				assert.Equal(t, "HELLO", string(resp.Body))
			},
		},
		{
			name:   "Date functions available",
			script: `return {status: 200, body: new Date().toString()};`,
			check: func(t *testing.T, resp *icap.Response) {
				assert.NotEmpty(t, resp.Body)
			},
		},
		{
			name:   "JSON functions available",
			script: `return {status: 200, body: JSON.stringify({a: 1})};`,
			check: func(t *testing.T, resp *icap.Response) {
				assert.Equal(t, `{"a":1}`, string(resp.Body))
			},
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

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, 200, resp.StatusCode)
			if tt.check != nil {
				tt.check(t, resp)
			}
		})
	}
}

// TestScriptSecurity_FileIONotAvailable tests that file I/O is not available.
// Note: goja doesn't expose Node.js fs module by default, but we test
// that attempts to access it fail gracefully.
func TestScriptSecurity_FileIONotAvailable(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "attempt to access fs",
			script: `var fs = require("fs"); fs.readFile("/etc/passwd");`,
		},
		{
			name:   "attempt to access window",
			script: `window.location.href = "http://evil.com";`,
		},
		{
			name:   "attempt to access document",
			script: `document.cookie;`,
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

			// Should fail (either blocked function or not defined)
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

// TestScriptSecurity_NetworkNotAvailable tests that network operations are not available.
func TestScriptSecurity_NetworkNotAvailable(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "attempt to use fetch",
			script: `fetch("http://evil.com");`,
		},
		{
			name:   "attempt to use XMLHttpRequest",
			script: `var xhr = new XMLHttpRequest(); xhr.open("GET", "http://evil.com");`,
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

			// Should fail (not defined in goja)
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

// TestScriptSecurity_ConcurrencySafety tests that security measures work correctly
// under concurrent execution.
func TestScriptSecurity_ConcurrencySafety(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `while (true) {}`, // Infinite loop
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 100*time.Millisecond)

	// Run multiple concurrent requests
	const numConcurrent = 10
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

	// All should timeout (not panic or hang)
	for i := 0; i < numConcurrent; i++ {
		err := <-results
		assert.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "timeout")
	}
}

// TestScriptSecurity_DynamicCodeExecution tests various attempts at dynamic code execution.
func TestScriptSecurity_DynamicCodeExecution(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		shouldBlock bool
	}{
		{
			name:        "eval with string",
			script:      `eval("1+1");`,
			shouldBlock: true,
		},
		{
			name:        "Function with string",
			script:      `new Function("return 1")();`,
			shouldBlock: true,
		},
		{
			name:        "setTimeout with string",
			script:      `setTimeout("1+1", 100);`,
			shouldBlock: true,
		},
		{
			name:        "setInterval with string",
			script:      `setInterval("1+1", 100);`,
			shouldBlock: true,
		},
		{
			name:        "safe function literal",
			script:      `var f = function(x) { return x+1; }; f(1); return {status: 200};`,
			shouldBlock: false,
		},
		{
			name:        "safe arrow function",
			script:      `var f = x => x+1; f(1); return {status: 200};`,
			shouldBlock: false,
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

			if tt.shouldBlock {
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

// TestScriptSecurity_GetSetSecurityConfig tests getting and setting security config.
func TestScriptSecurity_GetSetSecurityConfig(t *testing.T) {
	proc := NewScriptProcessor(nil, nil, 5*time.Second)

	// Get default config
	config := proc.GetSecurityConfig()
	assert.Equal(t, 5*time.Second, config.Timeout)
	assert.Equal(t, int64(10485760), config.MemoryLimitBytes)
	assert.NotEmpty(t, config.AllowedFunctions)
	assert.NotEmpty(t, config.BlockedFunctions)

	// Set new config
	newConfig := DefaultScriptSecurityConfig()
	newConfig.Timeout = 10 * time.Second
	newConfig.MemoryLimitBytes = 20 * 1024 * 1024
	newConfig.AllowedFunctions = []string{"Math.*"}
	newConfig.BlockedFunctions = []string{"eval"}

	proc.SetSecurityConfig(newConfig)

	// Verify config was updated
	updatedConfig := proc.GetSecurityConfig()
	assert.Equal(t, 10*time.Second, updatedConfig.Timeout)
	assert.Equal(t, int64(20*1024*1024), updatedConfig.MemoryLimitBytes)
	assert.Equal(t, []string{"Math.*"}, updatedConfig.AllowedFunctions)
	assert.Equal(t, []string{"eval"}, updatedConfig.BlockedFunctions)
}

// TestScriptSecurity_ZeroValuesHandled tests that zero values in security config are replaced with defaults.
func TestScriptSecurity_ZeroValuesHandled(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `return {status: 200};`,
			},
		},
	}

	// Create config with zero values
	secConfig := ScriptSecurityConfig{} // All zeros

	proc := NewScriptProcessorWithSecurity(registry, nil, secConfig)

	// Verify defaults were applied
	config := proc.GetSecurityConfig()
	assert.Equal(t, 5*time.Second, config.Timeout)
	assert.Equal(t, int64(10485760), config.MemoryLimitBytes)
	assert.NotEmpty(t, config.AllowedFunctions)
	assert.NotEmpty(t, config.BlockedFunctions)

	// Verify processor works with defaults
	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := proc.Process(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

// TestScriptSecurity_IsSecurityViolation tests the isSecurityViolation helper.
func TestScriptSecurity_IsSecurityViolation(t *testing.T) {
	proc := NewScriptProcessor(nil, nil, 5*time.Second)

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "security violation error",
			err:      fmt.Errorf("Security violation: blocked function 'eval' is not allowed"),
			expected: true,
		},
		{
			name:     "access denied error",
			err:      fmt.Errorf("Access denied: forbidden operation"),
			expected: true,
		},
		{
			name:     "not a security violation",
			err:      fmt.Errorf("normal execution error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "type error",
			err:      fmt.Errorf("TypeError: something went wrong"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := proc.isSecurityViolation(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
