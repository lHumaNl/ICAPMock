// Copyright 2026 ICAP Mock

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewMetricsState(t *testing.T) {
	cfg := &ClientConfig{
		MetricsURL: "http://localhost:8080/metrics",
	}

	state := NewMetricsState(cfg)

	assert.NotNil(t, state)
	assert.NotNil(t, state.snapshot)
	assert.NotNil(t, state.client)
	assert.NotNil(t, state.history)
	assert.Equal(t, 100, state.maxHistory)
	assert.Equal(t, 0, state.history.Size())
}

func TestMetricsState_GetCurrent(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshot := &MetricsSnapshot{
		Timestamp:     time.Now(),
		RPS:           100.5,
		LatencyP50:    10.0,
		LatencyP95:    25.0,
		LatencyP99:    50.0,
		Connections:   10,
		Errors:        1,
		BytesSent:     1024,
		BytesReceived: 2048,
	}

	state.Update(snapshot)

	current := state.GetCurrent()
	assert.Equal(t, snapshot, current)
}

func TestMetricsState_GetHistory(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshots := []*MetricsSnapshot{
		{Timestamp: time.Now(), RPS: 10.0},
		{Timestamp: time.Now(), RPS: 20.0},
		{Timestamp: time.Now(), RPS: 30.0},
	}

	for _, snapshot := range snapshots {
		state.Update(snapshot)
	}

	history := state.GetHistory()
	assert.Len(t, history, 3)
	assert.Equal(t, snapshots, history)
}

func TestMetricsState_Update(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshot1 := &MetricsSnapshot{Timestamp: time.Now(), RPS: 10.0}
	state.Update(snapshot1)

	assert.Equal(t, 1, state.history.Size())
	assert.Equal(t, snapshot1, state.snapshot)

	snapshot2 := &MetricsSnapshot{Timestamp: time.Now(), RPS: 20.0}
	state.Update(snapshot2)

	assert.Equal(t, 2, state.history.Size())
	assert.Equal(t, snapshot2, state.snapshot)
}

func TestMetricsState_Update_MaxHistory(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	for i := 0; i < 105; i++ {
		snapshot := &MetricsSnapshot{
			Timestamp: time.Now(),
			RPS:       float64(i),
		}
		state.Update(snapshot)
	}

	history := state.GetHistory()
	assert.Equal(t, 100, len(history))
	assert.Equal(t, 5.0, history[0].RPS)
	assert.Equal(t, 104.0, history[len(history)-1].RPS)
}

func TestMetricsState_StartStreaming(t *testing.T) {
	cfg := &ClientConfig{MetricsURL: "http://localhost:8080/metrics"}
	state := NewMetricsState(cfg)

	cmd := state.StartStreaming()
	assert.NotNil(t, cmd)
	assert.True(t, state.streaming)
}

func TestMetricsState_Refresh(t *testing.T) {
	cfg := &ClientConfig{MetricsURL: "http://localhost:8080/metrics"}
	state := NewMetricsState(cfg)

	cmd := state.Refresh()
	assert.NotNil(t, cmd)
}

func TestMetricsState_ConcurrentUpdates(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(index int) {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(index),
			}
			state.Update(snapshot)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	history := state.GetHistory()
	assert.Equal(t, 10, len(history))
}

func TestMetricsSnapshot_DefaultValues(t *testing.T) {
	snapshot := &MetricsSnapshot{}

	assert.Zero(t, snapshot.Timestamp)
	assert.Zero(t, snapshot.RPS)
	assert.Zero(t, snapshot.LatencyP50)
	assert.Zero(t, snapshot.LatencyP95)
	assert.Zero(t, snapshot.LatencyP99)
	assert.Zero(t, snapshot.Connections)
	assert.Zero(t, snapshot.Errors)
	assert.Zero(t, snapshot.BytesSent)
	assert.Zero(t, snapshot.BytesReceived)
}

func TestMetricsState_GetCurrent_ThreadSafety(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshot := &MetricsSnapshot{RPS: 100.0}
	state.Update(snapshot)

	done := make(chan *MetricsSnapshot, 10)

	for i := 0; i < 10; i++ {
		go func() {
			current := state.GetCurrent()
			done <- current
		}()
	}

	for i := 0; i < 10; i++ {
		current := <-done
		assert.Equal(t, snapshot, current)
	}
}

func TestMetricsState_GetHistory_ThreadSafety(t *testing.T) {
	state := NewMetricsState(&ClientConfig{})

	snapshots := []*MetricsSnapshot{
		{RPS: 10.0},
		{RPS: 20.0},
		{RPS: 30.0},
	}

	for _, s := range snapshots {
		state.Update(s)
	}

	done := make(chan []*MetricsSnapshot, 10)

	for i := 0; i < 10; i++ {
		go func() {
			history := state.GetHistory()
			done <- history
		}()
	}

	for i := 0; i < 10; i++ {
		history := <-done
		assert.Len(t, history, 3)
	}
}

func TestMetricsUpdatedMsg(t *testing.T) {
	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		RPS:       100.5,
	}

	msg := MetricsUpdatedMsg{Data: snapshot}

	assert.Equal(t, snapshot, msg.Data)
}

func TestMetricsState_StopStreaming(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewMetricsState(cfg)

	// Start streaming
	state.StartStreaming()
	assert.True(t, state.streaming)

	// Stop streaming
	state.StopStreaming()
	assert.False(t, state.streaming)
}

func TestMetricsState_StopStreaming_Multiple(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewMetricsState(cfg)

	// Multiple stop calls should not panic
	assert.NotPanics(t, func() {
		state.StopStreaming()
		state.StopStreaming()
		state.StopStreaming()
	})
}

func TestMetricsState_Shutdown(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewMetricsState(cfg)

	// Start streaming
	state.StartStreaming()

	// Shutdown
	state.Shutdown()
	assert.False(t, state.streaming)
}

func TestMetricsState_Shutdown_Nil(t *testing.T) {
	cfg := DefaultClientConfig()
	state := NewMetricsState(cfg)

	// Shutdown without start should not panic
	assert.NotPanics(t, func() {
		state.Shutdown()
	})
}

func TestMetricsState_DefaultConfig(t *testing.T) {
	cfg := DefaultClientConfig()

	// Verify default config is valid
	err := cfg.Validate()
	assert.NoError(t, err)
	assert.NotEmpty(t, cfg.MetricsURL)
	assert.NotEmpty(t, cfg.LogsURL)
	assert.NotEmpty(t, cfg.StatusURL)
	assert.Greater(t, cfg.Timeout, 0)
}
