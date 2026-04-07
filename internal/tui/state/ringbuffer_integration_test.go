// Copyright 2026 ICAP Mock

package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsState_RingBuffer_BoundedMemory(t *testing.T) {
	t.Run("history does not exceed maxHistory", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 10
		metrics := NewMetricsState(cfg)

		for i := 0; i < 100; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)
		}

		history := metrics.GetHistory()
		assert.Len(t, history, 10)

		lastSnapshot := history[len(history)-1]
		assert.Equal(t, 99.0, lastSnapshot.RPS)
	})

	t.Run("history maintains FIFO order", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 5
		metrics := NewMetricsState(cfg)

		for i := 0; i < 10; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)
		}

		history := metrics.GetHistory()
		require.Len(t, history, 5)

		assert.Equal(t, 5.0, history[0].RPS)
		assert.Equal(t, 6.0, history[1].RPS)
		assert.Equal(t, 7.0, history[2].RPS)
		assert.Equal(t, 8.0, history[3].RPS)
		assert.Equal(t, 9.0, history[4].RPS)
	})

	t.Run("GetCurrent always returns latest", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 3
		metrics := NewMetricsState(cfg)

		snapshots := []*MetricsSnapshot{}
		for i := 0; i < 10; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			snapshots = append(snapshots, snapshot)
			metrics.Update(snapshot)

			current := metrics.GetCurrent()
			assert.Equal(t, snapshot, current)
		}

		assert.Len(t, metrics.GetHistory(), 3)
	})
}

func TestMetricsState_RingBuffer_MemoryEfficiency(t *testing.T) {
	t.Run("memory usage stays constant after capacity", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 100
		metrics := NewMetricsState(cfg)

		initialSize := len(metrics.GetHistory())
		assert.Equal(t, 0, initialSize)

		for i := 0; i < 1000; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)

			if i >= 100 {
				assert.Len(t, metrics.GetHistory(), 100)
			}
		}

		assert.Len(t, metrics.GetHistory(), 100)
	})

	t.Run("large capacity works correctly", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 10000
		metrics := NewMetricsState(cfg)

		for i := 0; i < 10000; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)
		}

		history := metrics.GetHistory()
		assert.Len(t, history, 10000)
		assert.Equal(t, 0.0, history[0].RPS)
		assert.Equal(t, 9999.0, history[9999].RPS)
	})
}

func TestMetricsState_RingBuffer_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent updates are thread-safe", func(t *testing.T) {
		cfg := DefaultClientConfig()
		metrics := NewMetricsState(cfg)
		metrics.maxHistory = 100

		for i := 0; i < 10; i++ {
			go func(id int) {
				for j := 0; j < 100; j++ {
					snapshot := &MetricsSnapshot{
						Timestamp: time.Now(),
						RPS:       float64(id*100 + j),
					}
					metrics.Update(snapshot)
				}
			}(i)
		}

		time.Sleep(100 * time.Millisecond)

		history := metrics.GetHistory()
		assert.Len(t, history, 100)
	})

	t.Run("concurrent read and write", func(t *testing.T) {
		cfg := DefaultClientConfig()
		metrics := NewMetricsState(cfg)
		metrics.maxHistory = 100

		done := make(chan bool)

		go func() {
			for i := 0; i < 1000; i++ {
				snapshot := &MetricsSnapshot{
					Timestamp: time.Now(),
					RPS:       float64(i),
				}
				metrics.Update(snapshot)
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 1000; i++ {
				metrics.GetHistory()
				metrics.GetCurrent()
			}
			done <- true
		}()

		<-done
		<-done

		assert.Len(t, metrics.GetHistory(), 100)
	})
}

func TestMetricsState_RingBuffer_EdgeCases(t *testing.T) {
	t.Run("empty history", func(t *testing.T) {
		cfg := DefaultClientConfig()
		metrics := NewMetricsState(cfg)

		history := metrics.GetHistory()
		assert.Empty(t, history)
	})

	t.Run("single item", func(t *testing.T) {
		cfg := DefaultClientConfig()
		metrics := NewMetricsState(cfg)
		metrics.maxHistory = 1

		snapshot := &MetricsSnapshot{
			Timestamp: time.Now(),
			RPS:       100.0,
		}
		metrics.Update(snapshot)

		history := metrics.GetHistory()
		assert.Len(t, history, 1)
		assert.Equal(t, 100.0, history[0].RPS)
	})

	t.Run("capacity of 1 with many updates", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 1
		metrics := NewMetricsState(cfg)

		for i := 0; i < 10; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)
		}

		history := metrics.GetHistory()
		assert.Len(t, history, 1)
		assert.Equal(t, 9.0, history[0].RPS)
	})

	t.Run("zero maxHistory handled gracefully", func(t *testing.T) {
		cfg := DefaultClientConfig()
		metrics := NewMetricsState(cfg)
		metrics.maxHistory = 0

		snapshot := &MetricsSnapshot{
			Timestamp: time.Now(),
			RPS:       100.0,
		}
		metrics.Update(snapshot)

		history := metrics.GetHistory()
		assert.Len(t, history, 1)
	})
}

func TestLogsState_RingBuffer_BoundedMemory(t *testing.T) {
	t.Run("entries do not exceed maxLines", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 10
		logs := NewLogsState(cfg)

		for i := 0; i < 100; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 10)

		lastEntry := entries[0]
		assert.Contains(t, lastEntry.Message, "90")
	})

	t.Run("entries maintain insertion order", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 5
		logs := NewLogsState(cfg)

		for i := 0; i < 10; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 0)
		require.Len(t, entries, 5)

		assert.Contains(t, entries[0].Message, "5")
		assert.Contains(t, entries[1].Message, "6")
		assert.Contains(t, entries[2].Message, "7")
		assert.Contains(t, entries[3].Message, "8")
		assert.Contains(t, entries[4].Message, "9")
	})

	t.Run("GetEntries respects limit", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 100
		logs := NewLogsState(cfg)

		for i := 0; i < 100; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 10)
		assert.Len(t, entries, 10)

		assert.Contains(t, entries[0].Message, "90")
		assert.Contains(t, entries[9].Message, "99")
	})
}

func TestLogsState_RingBuffer_MemoryEfficiency(t *testing.T) {
	t.Run("memory usage stays constant after capacity", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 100
		logs := NewLogsState(cfg)

		initialSize := len(logs.GetEntries(nil, 0))
		assert.Equal(t, 0, initialSize)

		for i := 0; i < 1000; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)

			if i >= 100 {
				assert.Len(t, logs.GetEntries(nil, 0), 100)
			}
		}

		assert.Len(t, logs.GetEntries(nil, 0), 100)
	})

	t.Run("large capacity works correctly", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 10000
		logs := NewLogsState(cfg)

		for i := 0; i < 10000; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 10000)
		assert.Contains(t, entries[0].Message, "0")
		assert.Contains(t, entries[9999].Message, "9999")
	})
}

func TestLogsState_RingBuffer_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent AddEntry are thread-safe", func(t *testing.T) {
		cfg := DefaultClientConfig()
		logs := NewLogsState(cfg)
		logs.maxLines = 100

		for i := 0; i < 10; i++ {
			go func(id int) {
				for j := 0; j < 100; j++ {
					entry := &LogEntry{
						Timestamp: time.Now(),
						Level:     "INFO",
						Message:   fmt.Sprintf("Log entry %d-%d", id, j),
					}
					logs.AddEntry(entry)
				}
			}(i)
		}

		time.Sleep(100 * time.Millisecond)

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 100)
	})

	t.Run("concurrent AddEntry and GetEntries", func(t *testing.T) {
		cfg := DefaultClientConfig()
		logs := NewLogsState(cfg)
		logs.maxLines = 100

		done := make(chan bool)

		go func() {
			for i := 0; i < 1000; i++ {
				entry := &LogEntry{
					Timestamp: time.Now(),
					Level:     "INFO",
					Message:   fmt.Sprintf("Log entry %d", i),
				}
				logs.AddEntry(entry)
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 1000; i++ {
				logs.GetEntries(nil, 10)
				logs.GetEntries(nil, 0)
			}
			done <- true
		}()

		<-done
		<-done

		assert.Len(t, logs.GetEntries(nil, 0), 100)
	})
}

func TestLogsState_RingBuffer_EdgeCases(t *testing.T) {
	t.Run("empty entries", func(t *testing.T) {
		cfg := DefaultClientConfig()
		logs := NewLogsState(cfg)

		entries := logs.GetEntries(nil, 0)
		assert.Empty(t, entries)
	})

	t.Run("single entry", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 1
		logs := NewLogsState(cfg)

		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "Single entry",
		}
		logs.AddEntry(entry)

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 1)
		assert.Equal(t, "Single entry", entries[0].Message)
	})

	t.Run("capacity of 1 with many updates", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 1
		logs := NewLogsState(cfg)

		for i := 0; i < 10; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 1)
		assert.Contains(t, entries[0].Message, "9")
	})

	t.Run("nil entries handled gracefully", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 10
		logs := NewLogsState(cfg)

		logs.UpdateEntries([]*LogEntry{
			{Timestamp: time.Now(), Level: "INFO", Message: "Valid entry 1"},
			nil,
			{Timestamp: time.Now(), Level: "INFO", Message: "Valid entry 2"},
		})

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 2)
	})

	t.Run("filtering works with ring buffer", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 100
		logs := NewLogsState(cfg)

		for i := 0; i < 20; i++ {
			level := "INFO"
			if i%3 == 0 {
				level = "ERROR"
			}
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     level,
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)
		}

		filter := &LogFilter{Level: "ERROR"}
		entries := logs.GetEntries(filter, 0)

		assert.Len(t, entries, 7)
		for _, entry := range entries {
			assert.Equal(t, "ERROR", entry.Level)
		}
	})
}

func TestLogsState_RingBuffer_UpdateEntries(t *testing.T) {
	t.Run("merges new entries avoiding duplicates", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 10
		logs := NewLogsState(cfg)

		timestamp := time.Now()

		logs.UpdateEntries([]*LogEntry{
			{Timestamp: timestamp, Level: "INFO", Message: "Entry 1"},
			{Timestamp: timestamp.Add(1 * time.Second), Level: "INFO", Message: "Entry 2"},
			{Timestamp: timestamp.Add(2 * time.Second), Level: "INFO", Message: "Entry 3"},
		})

		logs.UpdateEntries([]*LogEntry{
			{Timestamp: timestamp, Level: "INFO", Message: "Entry 1"},
			{Timestamp: timestamp.Add(3 * time.Second), Level: "INFO", Message: "Entry 4"},
		})

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 4)
	})

	t.Run("enforces max limit on update", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 5
		logs := NewLogsState(cfg)

		entries := make([]*LogEntry, 10)
		for i := 0; i < 10; i++ {
			entries[i] = &LogEntry{
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				Level:     "INFO",
				Message:   fmt.Sprintf("Entry %d", i),
			}
		}

		logs.UpdateEntries(entries)

		result := logs.GetEntries(nil, 0)
		assert.Len(t, result, 5)
	})

	t.Run("handles nil input gracefully", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 10
		logs := NewLogsState(cfg)

		logs.UpdateEntries(nil)

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 0)
	})
}

func TestRingBuffer_MemoryUsage(t *testing.T) {
	t.Run("metrics with large number of snapshots", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 1000
		metrics := NewMetricsState(cfg)

		for i := 0; i < 10000; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp:     time.Now(),
				RPS:           float64(i),
				LatencyP50:    float64(i) * 0.5,
				LatencyP95:    float64(i) * 0.9,
				LatencyP99:    float64(i) * 0.99,
				Connections:   i % 100,
				Errors:        i % 10,
				BytesSent:     int64(i * 1024),
				BytesReceived: int64(i * 2048),
			}
			metrics.Update(snapshot)
		}

		history := metrics.GetHistory()
		assert.Len(t, history, 1000)
	})

	t.Run("logs with large number of entries", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 1000
		logs := NewLogsState(cfg)

		for i := 0; i < 10000; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     []string{"DEBUG", "INFO", "WARN", "ERROR"}[i%4],
				Message:   fmt.Sprintf("Log message with some content for entry %d", i),
				Fields:    map[string]interface{}{"index": i, "value": float64(i)},
			}
			logs.AddEntry(entry)
		}

		entries := logs.GetEntries(nil, 0)
		assert.Len(t, entries, 1000)
	})
}

func TestRingBuffer_NoMemoryLeaks(t *testing.T) {
	t.Run("continuous metrics updates", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxHistory = 100
		metrics := NewMetricsState(cfg)

		for i := 0; i < 10000; i++ {
			snapshot := &MetricsSnapshot{
				Timestamp: time.Now(),
				RPS:       float64(i),
			}
			metrics.Update(snapshot)

			if i%1000 == 0 && i > 0 {
				assert.Len(t, metrics.GetHistory(), 100)
			}
		}

		assert.Len(t, metrics.GetHistory(), 100)
	})

	t.Run("continuous log entries", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.MaxLogs = 100
		logs := NewLogsState(cfg)

		for i := 0; i < 10000; i++ {
			entry := &LogEntry{
				Timestamp: time.Now(),
				Level:     "INFO",
				Message:   fmt.Sprintf("Log entry %d", i),
			}
			logs.AddEntry(entry)

			if i%1000 == 0 && i > 0 {
				assert.Len(t, logs.GetEntries(nil, 0), 100)
			}
		}

		assert.Len(t, logs.GetEntries(nil, 0), 100)
	})
}
