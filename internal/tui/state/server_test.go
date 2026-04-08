// Copyright 2026 ICAP Mock

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewServerStatus(t *testing.T) {
	cfg := &ClientConfig{
		StatusURL: "http://localhost:8080/status",
	}

	status := NewServerStatus(cfg)

	assert.NotNil(t, status)
	assert.NotNil(t, status.client)
	assert.NotNil(t, status.ctrl)
	assert.NotNil(t, status.config)
	assert.Equal(t, "unknown", status.Current().State)
	assert.Equal(t, "N/A", status.Current().Port)
	assert.Equal(t, "N/A", status.Current().Uptime)
}

func TestServerStatus_Current(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	expected := ServerStatusInfo{
		State:  "running",
		Port:   "1344",
		Uptime: "5m",
		Error:  "",
	}

	status.Update(expected)

	current := status.Current()
	assert.Equal(t, "running", current.State)
	assert.Equal(t, "1344", current.Port)
	assert.Equal(t, "5m", current.Uptime)
	assert.Equal(t, "", current.Error)
}

func TestServerStatus_Update(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	newStatus := ServerStatusInfo{
		State:  "running",
		Port:   "1344",
		Uptime: "1m",
		Error:  "",
	}

	status.Update(newStatus)

	assert.Equal(t, newStatus, status.Current())
}

func TestServerStatus_Check(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.Check()
	assert.NotNil(t, cmd)
}

func TestServerStatus_CheckHealth(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.CheckHealth()
	assert.NotNil(t, cmd)
}

func TestServerStatus_Start(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.Start()
	assert.NotNil(t, cmd)
}

func TestServerStatus_Stop(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.Stop()
	assert.NotNil(t, cmd)
}

func TestServerStatus_Restart(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.Restart()
	assert.NotNil(t, cmd)
}

func TestServerStatus_GetConfig(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080/status"}
	status := NewServerStatus(cfg)

	cmd := status.GetConfig()
	assert.NotNil(t, cmd)
}

func TestServerStatus_GetHealthHistory(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	results := []HealthCheckResult{
		{Timestamp: time.Now(), Healthy: true},
		{Timestamp: time.Now(), Healthy: false},
		{Timestamp: time.Now(), Healthy: true},
	}

	for _, result := range results {
		status.addHealthHistory(result)
	}

	history := status.GetHealthHistory()
	assert.Len(t, history, 3)
	assert.Equal(t, results, history)
}

func TestServerStatus_GetLastHealthCheck(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	result1 := HealthCheckResult{Timestamp: time.Now(), Healthy: true}
	result2 := HealthCheckResult{Timestamp: time.Now(), Healthy: false}
	result3 := HealthCheckResult{Timestamp: time.Now(), Healthy: true}

	status.addHealthHistory(result1)
	status.addHealthHistory(result2)
	status.addHealthHistory(result3)

	last := status.GetLastHealthCheck()
	assert.NotNil(t, last)
	assert.Equal(t, result3.Healthy, last.Healthy)
}

func TestServerStatus_GetLastHealthCheck_Empty(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	last := status.GetLastHealthCheck()
	assert.Nil(t, last)
}

func TestServerStatus_addHealthHistory_MaxSize(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	for i := 0; i < 105; i++ {
		result := HealthCheckResult{
			Timestamp: time.Now(),
			Healthy:   i%2 == 0,
		}
		status.addHealthHistory(result)
	}

	history := status.GetHealthHistory()
	assert.Equal(t, 100, len(history))
}

func TestControlClient_NewControlClient(t *testing.T) {
	cfg := &ClientConfig{StatusURL: "http://localhost:8080"}

	client := NewControlClient(cfg)

	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestHealthCheckResult_DefaultValues(t *testing.T) {
	result := HealthCheckResult{}

	assert.Zero(t, result.Timestamp)
	assert.False(t, result.Healthy)
	assert.False(t, result.Ready)
	assert.Empty(t, result.ICAPStatus)
	assert.Empty(t, result.StorageStatus)
	assert.Zero(t, result.Scenarios)
	assert.Empty(t, result.Error)
}

func TestServerStatusInfo_DefaultValues(t *testing.T) {
	info := ServerStatusInfo{}

	assert.Empty(t, info.State)
	assert.Empty(t, info.Port)
	assert.Empty(t, info.Uptime)
	assert.Empty(t, info.Error)
}

func TestParseCheckStatus(t *testing.T) {
	tests := []struct {
		name  string
		check interface{}
		want  string
	}{
		{"nil check", nil, "unknown"},
		{"string check", "ok", "ok"},
		{"other type check", 123, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCheckStatus(tt.check)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestServerStatus_ConcurrentUpdates(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(_ int) {
			info := ServerStatusInfo{
				State:  "running",
				Port:   "1344",
				Uptime: "1m",
			}
			status.Update(info)
			current := status.Current()
			assert.Equal(t, "running", current.State)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestServerStatus_ConcurrentHealthChecks(t *testing.T) {
	status := NewServerStatus(&ClientConfig{})

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(index int) {
			result := HealthCheckResult{
				Timestamp: time.Now(),
				Healthy:   index%2 == 0,
			}
			status.addHealthHistory(result)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	history := status.GetHealthHistory()
	assert.Equal(t, 10, len(history))
}
