// Copyright 2026 ICAP Mock

package state

import (
	"context"
	"log"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/icap-mock/icap-mock/internal/tui/utils"
)

// MetricsState manages metrics data for the TUI.
type MetricsState struct {
	snapshot   *MetricsSnapshot
	history    *utils.RingBuffer[*MetricsSnapshot]
	client     *MetricsClient
	cancel     context.CancelFunc
	maxHistory int
	mu         sync.RWMutex
	streaming  bool
}

// MetricsSnapshot represents a snapshot of server metrics.
type MetricsSnapshot struct {
	Timestamp     time.Time
	RPS           float64
	LatencyP50    float64
	LatencyP95    float64
	LatencyP99    float64
	Connections   int
	Errors        int
	BytesSent     int64
	BytesReceived int64
}

// NewMetricsState creates a new metrics state with provided configuration.
func NewMetricsState(cfg *ClientConfig) *MetricsState {
	// Validate configuration
	if cfg == nil {
		cfg = DefaultClientConfig()
	} else {
		// Merge with defaults to fill in missing fields
		defaultCfg := DefaultClientConfig()
		if cfg.MetricsURL == "" {
			cfg.MetricsURL = defaultCfg.MetricsURL
		}
		if cfg.LogsURL == "" {
			cfg.LogsURL = defaultCfg.LogsURL
		}
		if cfg.StatusURL == "" {
			cfg.StatusURL = defaultCfg.StatusURL
		}
		if cfg.Timeout <= 0 {
			cfg.Timeout = defaultCfg.Timeout
		}
		if cfg.MaxConcurrentRequests <= 0 {
			cfg.MaxConcurrentRequests = defaultCfg.MaxConcurrentRequests
		}
		if cfg.RequestInterval == 0 {
			cfg.RequestInterval = defaultCfg.RequestInterval
		}
		if cfg.RetryMax < 0 {
			cfg.RetryMax = defaultCfg.RetryMax
		}
		if cfg.MaxHistory <= 0 {
			cfg.MaxHistory = defaultCfg.MaxHistory
		}
	}

	return &MetricsState{
		snapshot:   &MetricsSnapshot{Timestamp: time.Now()},
		history:    utils.NewRingBuffer[*MetricsSnapshot](cfg.MaxHistory),
		maxHistory: cfg.MaxHistory,
		client:     NewMetricsClient(cfg),
	}
}

// StartStreaming begins streaming metrics updates.
func (s *MetricsState) StartStreaming() tea.Cmd {
	s.mu.Lock()
	s.streaming = true
	s.mu.Unlock()

	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		// Use the stored context to check if streaming is still active
		s.mu.RLock()
		streaming := s.streaming
		s.mu.RUnlock()

		if !streaming {
			return nil
		}

		// Create a timeout context for each request
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		snapshot, err := s.client.GetMetrics(ctx)
		if err != nil {
			log.Printf("Error fetching metrics: %v", err)
			return MetricsUpdatedMsg{Data: &MetricsSnapshot{Timestamp: time.Now()}}
		}
		return MetricsUpdatedMsg{Data: snapshot}
	})
}

// Refresh fetches the latest metrics from the server.
func (s *MetricsState) Refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		snapshot, err := s.client.GetMetrics(ctx)
		if err != nil {
			log.Printf("Error fetching metrics: %v", err)
			return nil
		}
		return MetricsUpdatedMsg{Data: snapshot}
	}
}

// Update updates the metrics state with a new snapshot.
func (s *MetricsState) Update(snapshot *MetricsSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot = snapshot

	// RingBuffer automatically handles capacity limit and FIFO behavior
	s.history.Add(snapshot)
}

// GetCurrent returns the current metrics snapshot.
func (s *MetricsState) GetCurrent() *MetricsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

// GetHistory returns the metrics history.
func (s *MetricsState) GetHistory() []*MetricsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.history.GetAll()
}

// MetricsUpdatedMsg is sent when metrics are refreshed.
type MetricsUpdatedMsg struct {
	Data *MetricsSnapshot
}

// StopStreaming stops the metrics streaming.
func (s *MetricsState) StopStreaming() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.streaming = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Shutdown releases all resources.
func (s *MetricsState) Shutdown() {
	s.StopStreaming()
}
