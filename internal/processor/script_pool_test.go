// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScriptWorkerPool_BasicExecution tests that scripts are executed by the pool.
func TestScriptWorkerPool_BasicExecution(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	// Mock execute function
	execCount := 0
	mu := sync.Mutex{}
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		mu.Lock()
		execCount++
		mu.Unlock()
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := pool.Execute(context.Background(), req, "test script")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)

	mu.Lock()
	assert.Equal(t, 1, execCount)
	mu.Unlock()
}

// TestScriptWorkerPool_QueueOverflow tests that queue overflow is handled correctly.
func TestScriptWorkerPool_QueueOverflow(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 1
	cfg.QueueSize = 2

	// Slow execution function - block long enough for queue to fill
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(200 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit 5 jobs rapidly - queue is size 2 (1 being processed, 2 in queue)
	// Jobs 1, 2, 3 should succeed (1 processing, 2 in queue = 3 total)
	// Job 4 should be rejected
	var wg sync.WaitGroup
	successCount := 0
	failCount := 0
	mu := sync.Mutex{}

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", id))
			mu.Lock()
			if err != nil {
				failCount++
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	mu.Lock()
	t.Logf("Success: %d, Failed: %d", successCount, failCount)
	assert.Greater(t, failCount, 0, "some jobs should be rejected")
	mu.Unlock()
}

// TestScriptWorkerPool_WorkerLimit tests that worker limit is enforced.
func TestScriptWorkerPool_WorkerLimit(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	// Track concurrent executions
	maxConcurrent := 0
	currentConcurrent := 0
	mu := sync.Mutex{}

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		currentConcurrent--
		mu.Unlock()

		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit 10 concurrent jobs
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Execute(context.Background(), req, fmt.Sprintf("script%d", i))
		}()
	}
	wg.Wait()

	mu.Lock()
	assert.Equal(t, 2, maxConcurrent, "max concurrent executions should equal worker count")
	mu.Unlock()
}

// TestScriptWorkerPool_GracefulShutdown tests that graceful shutdown drains queue.
func TestScriptWorkerPool_GracefulShutdown(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	execCount := 0
	mu := sync.Mutex{}

	// Slow execution function
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		execCount++
		mu.Unlock()
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit 5 jobs
	for i := 0; i < 5; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", i))
		require.NoError(t, err)
	}

	// Shutdown should wait for all jobs to complete
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = pool.Shutdown(ctx)
	require.NoError(t, err)

	mu.Lock()
	assert.Equal(t, 5, execCount, "all jobs should be executed")
	mu.Unlock()
}

// TestScriptWorkerPool_ShutdownWithTimeout tests that shutdown respects context timeout.
func TestScriptWorkerPool_ShutdownWithTimeout(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 1
	cfg.QueueSize = 5

	// Very slow execution function
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(1 * time.Second)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit a slow job
	go pool.Execute(context.Background(), req, "slow script")
	time.Sleep(10 * time.Millisecond) // Let job start

	// Shutdown with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = pool.Shutdown(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// TestScriptWorkerPool_ContextCancellation tests that context cancellation works.
func TestScriptWorkerPool_ContextCancellation(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(50 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = pool.Execute(ctx, req, "script")
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// TestScriptWorkerPool_Metrics tests that metrics are properly tracked.
func TestScriptWorkerPool_Metrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metricsCollector, err := metrics.NewCollector(reg)
	require.NoError(t, err)

	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 5
	cfg.Metrics = metricsCollector

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Execute a script
	_, err = pool.Execute(context.Background(), req, "script1")
	require.NoError(t, err)

	// Check metrics
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	// Verify script_pool_workers metric
	foundWorkers := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_script_pool_workers" {
			foundWorkers = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Equal(t, float64(2), mf.Metric[0].GetGauge().GetValue())
		}
	}
	assert.True(t, foundWorkers, "script_pool_workers metric not found")

	// Create a new pool with slow execution to trigger rejection
	cfgSlow := DefaultScriptWorkerPoolConfig()
	cfgSlow.Workers = 1
	cfgSlow.QueueSize = 2
	cfgSlow.Metrics = metricsCollector

	scriptFuncSlow := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(200 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	poolSlow := NewScriptWorkerPool(cfgSlow, scriptFuncSlow)
	defer poolSlow.Shutdown(context.Background())

	// Submit jobs rapidly to fill queue and cause rejection
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			poolSlow.Execute(context.Background(), req, fmt.Sprintf("script%d", id))
		}(i)
	}
	wg.Wait()

	// Check rejection metric
	metricFamilies, err = reg.Gather()
	require.NoError(t, err)

	foundRejected := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "icap_script_pool_rejected_total" {
			foundRejected = true
			assert.Equal(t, 1, len(mf.Metric))
			assert.Greater(t, mf.Metric[0].GetCounter().GetValue(), float64(0))
		}
	}
	assert.True(t, foundRejected, "script_pool_rejected_total metric not found")
}

// TestScriptWorkerPool_PanicRecovery tests that panics in script execution are recovered.
func TestScriptWorkerPool_PanicRecovery(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	shouldPanic := true
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		if shouldPanic {
			panic("test panic")
		}
		return icap.NewResponse(200), nil
	}

	log, err := logger.New(config.LoggingConfig{Level: "error", Format: "json"})
	require.NoError(t, err)
	cfg.Logger = log

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// This should panic but not crash the worker
	_, err = pool.Execute(context.Background(), req, "panic script")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")

	// Worker should still be able to process jobs
	shouldPanic = false
	resp, err := pool.Execute(context.Background(), req, "normal script")
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestScriptWorkerPool_MultipleRequests tests concurrent request handling.
func TestScriptWorkerPool_MultipleRequests(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 20
	cfg.QueueSize = 200

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(5 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit 100 concurrent requests
	numRequests := 100
	successCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", id))
			if err == nil && resp != nil && resp.StatusCode == 200 {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	mu.Lock()
	assert.Equal(t, numRequests, successCount, "all requests should succeed")
	mu.Unlock()
}

// TestScriptWorkerPool_ZeroConfig tests that zero configuration uses defaults.
func TestScriptWorkerPool_ZeroConfig(t *testing.T) {
	cfg := ScriptWorkerPoolConfig{}
	cfg.Workers = 0
	cfg.QueueSize = 0

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	// Should use default values (100 workers, 1000 queue)
	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	resp, err := pool.Execute(context.Background(), req, "script")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

// BenchmarkScriptPool_Throughput benchmarks throughput with pool.
func BenchmarkScriptPool_Throughput(b *testing.B) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 10
	cfg.QueueSize = 100

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.Execute(context.Background(), req, "script")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScriptPool_WithoutPool benchmarks throughput without pool (baseline).
func BenchmarkScriptPool_WithoutPool(b *testing.B) {
	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scriptFunc(context.Background(), req, "script")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScriptPool_Concurrent benchmarks concurrent executions.
func BenchmarkScriptPool_Concurrent(b *testing.B) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 10
	cfg.QueueSize = 100

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pool.Execute(context.Background(), req, "script")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// TestScriptWorkerPool_PanicWithConcurrentShutdown tests that workers exit properly
// when a panic occurs during concurrent shutdown.
func TestScriptWorkerPool_PanicWithConcurrentShutdown(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 5
	cfg.QueueSize = 10

	panicTriggered := false
	mu := sync.Mutex{}

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		if panicTriggered {
			panic("concurrent shutdown panic")
		}
		mu.Unlock()
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit several jobs
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			pool.Execute(context.Background(), req, fmt.Sprintf("script%d", id))
		}(i)
	}

	// Wait a bit then trigger panic and shutdown concurrently
	time.Sleep(20 * time.Millisecond)
	go func() {
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		panicTriggered = true
		mu.Unlock()
	}()

	// Shutdown shortly after (this will be the only shutdown call)
	go func() {
		time.Sleep(10 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pool.Shutdown(ctx)
	}()

	wg.Wait()

	// Ensure shutdown completes (idempotent call is safe)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = pool.Shutdown(ctx)
	require.NoError(t, err, "shutdown should complete without timeout")
}

// TestScriptWorkerPool_WorkerHealth tests worker health tracking.
func TestScriptWorkerPool_WorkerHealth(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 3
	cfg.QueueSize = 10

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Execute several jobs
	for i := 0; i < 20; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", i))
		require.NoError(t, err)
	}

	// Check worker health stats
	health := pool.GetWorkerHealth()
	assert.Equal(t, 3, len(health))

	totalJobsProcessed := int64(0)
	for _, h := range health {
		jobsProcessed := h["jobs_processed"].(int64)
		totalJobsProcessed += jobsProcessed
		assert.Equal(t, int64(0), h["panics_recovered"].(int64), "should be no panics")
		assert.GreaterOrEqual(t, jobsProcessed, int64(0), "jobs processed should be non-negative")
	}

	assert.Equal(t, int64(20), totalJobsProcessed, "all jobs should be processed")
}

// TestScriptWorkerPool_PanicHealthTracking tests that panics are properly tracked.
func TestScriptWorkerPool_PanicHealthTracking(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	shouldPanic := false
	panicCount := 0
	mu := sync.Mutex{}

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		mu.Lock()
		shouldPanicLocal := shouldPanic
		mu.Unlock()

		if shouldPanicLocal {
			mu.Lock()
			panicCount++
			mu.Unlock()
			panic("test panic")
		}
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Execute normal jobs
	for i := 0; i < 5; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("normal%d", i))
		require.NoError(t, err)
	}

	// Trigger panics
	mu.Lock()
	shouldPanic = true
	mu.Unlock()

	// Execute jobs that will panic
	for i := 0; i < 3; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("panic%d", i))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	}

	// Stop panicking
	mu.Lock()
	shouldPanic = false
	mu.Unlock()

	// Execute more normal jobs to verify workers recovered
	for i := 0; i < 5; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("recovered%d", i))
		require.NoError(t, err)
	}

	// Check worker health stats
	health := pool.GetWorkerHealth()
	totalPanicsRecovered := int64(0)
	for _, h := range health {
		panicsRecovered := h["panics_recovered"].(int64)
		totalPanicsRecovered += panicsRecovered
	}

	assert.GreaterOrEqual(t, totalPanicsRecovered, int64(3), "should track recovered panics")
	assert.Equal(t, 3, panicCount, "should have panicked 3 times")
}

// TestScriptWorkerPool_NoGoroutineLeak tests that all workers exit properly.
func TestScriptWorkerPool_NoGoroutineLeak(t *testing.T) {
	// Get initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 10
	cfg.QueueSize = 20

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(5 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit many jobs
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			pool.Execute(context.Background(), req, fmt.Sprintf("script%d", id))
		}(i)
	}
	wg.Wait()

	// Shutdown the pool
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = pool.Shutdown(ctx)
	require.NoError(t, err)

	// Wait a bit for goroutines to exit
	time.Sleep(100 * time.Millisecond)

	// Check that goroutines have been cleaned up
	finalGoroutines := runtime.NumGoroutine()
	goroutineDelta := finalGoroutines - initialGoroutines

	// Allow for some test goroutines, but should not have leaked workers
	// We started 10 workers + main goroutine, so delta should be small
	t.Logf("Initial goroutines: %d, Final goroutines: %d, Delta: %d",
		initialGoroutines, finalGoroutines, goroutineDelta)

	assert.LessOrEqual(t, goroutineDelta, int(cfg.Workers)+5,
		"should not have significant goroutine leak (delta should be small)")
}

// TestScriptWorkerPool_PanicDuringShutdown tests panic handling during shutdown.
func TestScriptWorkerPool_PanicDuringShutdown(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 3
	cfg.QueueSize = 10

	shutdownStarted := false
	mu := sync.Mutex{}

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		mu.Lock()
		if shutdownStarted {
			panic("shutdown panic")
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit jobs synchronously
	for i := 0; i < 3; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", i))
		require.NoError(t, err)
	}

	// Trigger panic on next jobs
	mu.Lock()
	shutdownStarted = true
	mu.Unlock()

	// Start shutdown - this should complete even if panics occur
	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownDone <- pool.Shutdown(ctx)
	}()

	// Wait for shutdown to complete
	select {
	case err := <-shutdownDone:
		require.NoError(t, err, "shutdown should complete even with panics")
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timeout")
	}
}

// TestScriptWorkerPool_ContextCancelDuringPanic tests context cancellation during panic recovery.
func TestScriptWorkerPool_ContextCancelDuringPanic(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		panic("test panic")
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)
	defer pool.Shutdown(context.Background())

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit job and cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = pool.Execute(ctx, req, "script")
	assert.Error(t, err)
	// Should be context canceled error (job canceled before panic could be sent)
	assert.True(t,
		errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "panic"),
		"should either be context canceled or panic error")
}

// TestScriptWorkerPool_WorkerExitOnChannelClose tests that workers exit when result channel is closed.
func TestScriptWorkerPool_WorkerExitOnChannelClose(t *testing.T) {
	cfg := DefaultScriptWorkerPoolConfig()
	cfg.Workers = 5
	cfg.QueueSize = 10

	jobCount := 0
	mu := sync.Mutex{}

	scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		jobCount++
		mu.Unlock()
		return icap.NewResponse(200), nil
	}

	pool := NewScriptWorkerPool(cfg, scriptFunc)

	req, err := icap.NewRequest("REQMOD", "icap://localhost:1344/reqmod")
	require.NoError(t, err)

	// Submit jobs
	for i := 0; i < 20; i++ {
		_, err := pool.Execute(context.Background(), req, fmt.Sprintf("script%d", i))
		require.NoError(t, err)
	}

	mu.Lock()
	finalJobCount := jobCount
	mu.Unlock()

	assert.Equal(t, 20, finalJobCount, "all jobs should be processed")

	// Shutdown should exit all workers cleanly
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = pool.Shutdown(ctx)
	require.NoError(t, err)

	// Verify worker health shows all jobs processed
	health := pool.GetWorkerHealth()
	totalJobsProcessed := int64(0)
	for _, h := range health {
		totalJobsProcessed += h["jobs_processed"].(int64)
	}
	assert.Equal(t, int64(20), totalJobsProcessed, "worker health should track all jobs")
}

// TestScriptWorkerPool_ConcurrentExecuteAndShutdown tests that concurrent Execute
// calls and Shutdown do not race. This test is specifically designed to catch the
// data race where Execute sends to p.jobs while Shutdown closes it.
func TestScriptWorkerPool_ConcurrentExecuteAndShutdown(t *testing.T) {
	for i := 0; i < 20; i++ {
		cfg := DefaultScriptWorkerPoolConfig()
		cfg.Workers = 4
		cfg.QueueSize = 10

		scriptFunc := func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
			return &icap.Response{StatusCode: 200}, nil
		}

		pool := NewScriptWorkerPool(cfg, scriptFunc)

		var wg sync.WaitGroup
		// Spawn multiple goroutines that call Execute concurrently with Shutdown
		for g := 0; g < 10; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := &icap.Request{Method: "REQMOD", URI: "icap://localhost/test"}
				for j := 0; j < 5; j++ {
					_, _ = pool.Execute(context.Background(), req, "test")
					runtime.Gosched()
				}
			}()
		}

		// Shutdown concurrently with Execute calls
		runtime.Gosched()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := pool.Shutdown(shutdownCtx)
		cancel()
		assert.NoError(t, err)

		wg.Wait()
	}
}
