// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// mockScenarioRegistry is a test implementation of storage.ScenarioRegistry.
type mockScenarioRegistry struct {
	scenario *storage.Scenario
	err      error
}

func (m *mockScenarioRegistry) Match(req *icap.Request) (*storage.Scenario, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.scenario, nil
}

func (m *mockScenarioRegistry) Load(path string) error {
	return nil
}

func (m *mockScenarioRegistry) Reload() error {
	return nil
}

func (m *mockScenarioRegistry) List() []*storage.Scenario {
	if m.scenario != nil {
		return []*storage.Scenario{m.scenario}
	}
	return nil
}

func (m *mockScenarioRegistry) Add(scenario *storage.Scenario) error {
	m.scenario = scenario
	return nil
}

func (m *mockScenarioRegistry) Remove(name string) error {
	return nil
}

func TestScriptProcessor_Name(t *testing.T) {
	proc := NewScriptProcessor(nil, nil, 5*time.Second)
	assert.Equal(t, "ScriptProcessor", proc.Name())
}

func TestScriptProcessor_NoScript(t *testing.T) {
	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				ICAPStatus: 204,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := proc.Process(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestScriptProcessor_SimpleScript(t *testing.T) {
	script := `return {status: 200, body: "Hello, World!"};`

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
	assert.Equal(t, []byte("Hello, World!"), resp.Body)
}

func TestScriptProcessor_ScriptWithHeaders(t *testing.T) {
	script := `
return {
	status: 200,
	headers: {
		"X-Custom-Header": "test-value",
		"X-Another-Header": "another-value"
	},
	body: "Response with headers"
};
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
	assert.Equal(t, []byte("Response with headers"), resp.Body)

	value, ok := resp.GetHeader("X-Custom-Header")
	assert.True(t, ok)
	assert.Equal(t, "test-value", value)

	value, ok = resp.GetHeader("X-Another-Header")
	assert.True(t, ok)
	assert.Equal(t, "another-value", value)
}

func TestScriptProcessor_ScriptUsesRequest(t *testing.T) {
	script := `return {status: 200, body: "Request method: " + req.method};`

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
	assert.Equal(t, []byte("Request method: REQMOD"), resp.Body)
}

func TestScriptProcessor_ScriptReturnsStatus(t *testing.T) {
	script := `return 204;`

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
	assert.Equal(t, 204, resp.StatusCode)
}

func TestScriptProcessor_ScriptReturnsString(t *testing.T) {
	script := `return "Simple string response";`

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
	assert.Equal(t, []byte("Simple string response"), resp.Body)
}

func TestScriptProcessor_ScriptReturnsNull(t *testing.T) {
	script := `return null;`

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
	assert.Equal(t, 204, resp.StatusCode)
}

func TestScriptProcessor_ScriptReturnsUndefined(t *testing.T) {
	script := `return undefined;`

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
	assert.Equal(t, 204, resp.StatusCode)
}

func TestScriptProcessor_ScriptSyntaxError(t *testing.T) {
	script := `return {status: 200;` // Missing closing brace

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

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "script execution failed")
}

func TestScriptProcessor_ScriptRuntimeError(t *testing.T) {
	script := `throw new Error("Test error");`

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

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "script execution failed")
}

func TestScriptProcessor_ScriptTimeout(t *testing.T) {
	script := `while (true) {}` // Infinite loop

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
	resp, err := proc.Process(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "timeout")
}

func TestScriptProcessor_ContextCancelled(t *testing.T) {
	script := `return {status: 200};`

	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: script,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)
	resp, err := proc.Process(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestScriptProcessor_WithLogger(t *testing.T) {
	log, err := logger.New(config.LoggingConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	defer log.Close()

	script := `return {status: 200, body: "Logged response"};`

	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: script,
			},
		},
	}

	proc := NewScriptProcessor(registry, log, 5*time.Second)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)
	resp, err := proc.Process(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []byte("Logged response"), resp.Body)
}

func TestScriptProcessor_SetLogger(t *testing.T) {
	log, err := logger.New(config.LoggingConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	defer log.Close()

	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: `return {status: 200};`,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)
	proc.SetLogger(log)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)
	resp, err := proc.Process(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestScriptProcessor_SetTimeout(t *testing.T) {
	script := `return {status: 200};`

	registry := &mockScenarioRegistry{
		scenario: &storage.Scenario{
			Name: "test",
			Response: storage.ResponseTemplate{
				Script: script,
			},
		},
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)
	proc.SetTimeout(10 * time.Second)

	assert.Equal(t, 10*time.Second, proc.timeout)
}

func TestScriptProcessor_VariablesAvailable(t *testing.T) {
	script := `
var result = {
	status: 200,
	body: JSON.stringify({
		reqMethod: req.method,
		reqUri: req.uri,
		headers: headers,
		body: body,
		config: config
	})
};
return result;
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
	assert.NotNil(t, resp.Body)
	assert.Contains(t, string(resp.Body), "reqMethod")
	assert.Contains(t, string(resp.Body), "REQMOD")
}

func TestScriptProcessor_InvalidResultType(t *testing.T) {
	script := `return function() { return 42; };` // Returns a function

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

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid script result type")
}

func TestScriptProcessor_NilScenarioMatch(t *testing.T) {
	registry := &mockScenarioRegistry{
		err: storage.ErrNoMatch,
	}

	proc := NewScriptProcessor(registry, nil, 5*time.Second)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)
	resp, err := proc.Process(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestScriptProcessor_VariousStatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		script         string
		expectedStatus int
	}{
		{"status 200", "return 200;", 200},
		{"status 204", "return 204;", 204},
		{"status 400", "return 400;", 400},
		{"status 404", "return 404;", 404},
		{"status 500", "return 500;", 500},
		{"status 503", "return 503;", 503},
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
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

var _ Processor = NewScriptProcessor(nil, nil, 0)
