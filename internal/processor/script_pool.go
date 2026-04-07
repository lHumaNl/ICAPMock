// Copyright 2026 ICAP Mock

package processor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/icap-mock/icap-mock/internal/logger"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// scriptJob represents a job for the script worker pool.
type scriptJob struct {
	ctx    context.Context
	req    *icap.Request
	result chan<- scriptJobResult
	script string
}

// scriptJobResult holds the result of a script execution.
type scriptJobResult struct {
	resp *icap.Response
	err  error
}

// WorkerHealth tracks the health status of a worker.
type WorkerHealth struct {
	lastError       error
	workerID        int
	jobsProcessed   int64
	panicsRecovered int64
	lastPanicTime   int64
	mu              sync.RWMutex
}

// RecordJobProcessed records that a job was processed.
func (h *WorkerHealth) RecordJobProcessed() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobsProcessed++
}

// RecordPanicRecovered records that a panic was recovered.
func (h *WorkerHealth) RecordPanicRecovered() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.panicsRecovered++
	h.lastPanicTime = time.Now().Unix()
}

// GetStats returns the current worker statistics.
func (h *WorkerHealth) GetStats() (int64, int64, int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.jobsProcessed, h.panicsRecovered, h.lastPanicTime
}

// ScriptWorkerPoolConfig holds configuration for the script worker pool.
type ScriptWorkerPoolConfig struct {
	Metrics   *metrics.Collector
	Logger    *logger.Logger
	Workers   int
	QueueSize int
}

// DefaultScriptWorkerPoolConfig returns the default configuration.
func DefaultScriptWorkerPoolConfig() ScriptWorkerPoolConfig {
	return ScriptWorkerPoolConfig{
		Workers:   100,
		QueueSize: 1000,
	}
}

// ScriptWorkerPool manages a pool of workers for script execution.
// It prevents goroutine explosion by limiting the number of concurrent script executions
// and providing a bounded queue for pending jobs.
//
// The pool provides graceful shutdown capabilities to prevent goroutine leaks.
// Scripts are rejected when the queue is full, preventing memory exhaustion.
type ScriptWorkerPool struct {
	ctx           context.Context
	scriptFunc    executeScriptFunc
	cancel        context.CancelFunc
	logger        *logger.Logger
	metrics       *metrics.Collector
	jobs          chan *scriptJob
	workerHealth  []*WorkerHealth
	wg            sync.WaitGroup
	maxQueueSize  int
	rejectedCount int64
	healthMu      sync.RWMutex
	jobsMu        sync.RWMutex
	shutdownOnce  sync.Once
	stopped       atomic.Bool
}

// executeScriptFunc is the function type for executing scripts.
type executeScriptFunc func(ctx context.Context, req *icap.Request, script string) (*icap.Response, error)

// NewScriptWorkerPool creates a new ScriptWorkerPool with the given configuration.
//
// The pool starts the configured number of workers immediately.
// Use the Shutdown method for graceful termination to prevent goroutine leaks.
//
// Parameters:
//   - cfg: Configuration for the worker pool
//   - scriptFunc: Function to execute scripts (typically ScriptProcessor.executeScript)
//
// Returns:
//   - *ScriptWorkerPool: The created worker pool
//
// Example:
//
//	pool := NewScriptWorkerPool(processor.DefaultScriptWorkerPoolConfig(), processor.executeScript)
//	defer pool.Shutdown(context.Background())
//	resp, err := pool.Execute(ctx, req, script)
func NewScriptWorkerPool(cfg ScriptWorkerPoolConfig, scriptFunc executeScriptFunc) *ScriptWorkerPool {
	if cfg.Workers <= 0 {
		cfg.Workers = 100
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel managed elsewhere
	jobs := make(chan *scriptJob, cfg.QueueSize)

	pool := &ScriptWorkerPool{
		jobs:         jobs,
		ctx:          ctx,
		cancel:       cancel,
		logger:       cfg.Logger,
		metrics:      cfg.Metrics,
		scriptFunc:   scriptFunc,
		maxQueueSize: cfg.QueueSize,
		workerHealth: make([]*WorkerHealth, cfg.Workers),
	}

	// Start fixed number of workers
	for i := 0; i < cfg.Workers; i++ {
		pool.healthMu.Lock()
		pool.workerHealth[i] = &WorkerHealth{workerID: i}
		pool.healthMu.Unlock()
		pool.wg.Add(1)
		go pool.scriptWorker(i)
	}

	// Update metrics for active workers
	if pool.metrics != nil {
		pool.metrics.SetScriptPoolWorkers(float64(cfg.Workers))
	}

	return pool
}

// Execute submits a script execution job to the worker pool and waits for completion.
//
// If the job queue is full, the script is rejected with an error.
// This prevents memory exhaustion by limiting the number of pending executions.
//
// Parameters:
//   - ctx: Context for cancellation and deadline propagation
//   - req: The ICAP request to process
//   - script: The JavaScript script to execute
//
// Returns:
//   - *icap.Response: The ICAP response from script execution
//   - error: An error if queue is full or script execution fails
//
// Example:
//
//	resp, err := pool.Execute(ctx, req, script)
//	if err != nil {
//	    return nil, fmt.Errorf("script execution rejected: %w", err)
//	}
func (p *ScriptWorkerPool) Execute(ctx context.Context, req *icap.Request, script string) (*icap.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resultChan := make(chan scriptJobResult, 1)

	// Check if pool is stopped before attempting to send.
	if p.stopped.Load() {
		return nil, fmt.Errorf("script pool is shut down")
	}

	// Hold a read lock on jobsMu to prevent the channel from being closed
	// while we send. Shutdown acquires the write lock before closing.
	p.jobsMu.RLock()
	if p.stopped.Load() {
		p.jobsMu.RUnlock()
		return nil, fmt.Errorf("script pool is shut down")
	}

	// Try to enqueue the job (non-blocking)
	var enqueued bool
	select {
	case p.jobs <- &scriptJob{ctx: ctx, req: req, script: script, result: resultChan}:
		enqueued = true
	default:
	}
	p.jobsMu.RUnlock()

	if enqueued {
		// Update queue length gauge
		if p.metrics != nil {
			p.metrics.SetScriptPoolQueueLength(float64(len(p.jobs)))
		}
	} else {
		// Queue full, reject the request
		rejected := atomic.AddInt64(&p.rejectedCount, 1)
		currentQueueSize := len(p.jobs)

		if p.logger != nil {
			p.logger.Warn("script pool queue full, rejecting script execution",
				"method", req.Method,
				"uri", req.URI,
				"rejected_count", rejected,
				"queue_size", currentQueueSize,
				"max_queue_size", p.maxQueueSize,
			)
		}

		// Track rejection in metrics
		if p.metrics != nil {
			p.metrics.RecordScriptPoolRejected(float64(currentQueueSize), float64(p.maxQueueSize))
		}

		return nil, fmt.Errorf("script pool queue full (%d/%d), try again later", currentQueueSize, p.maxQueueSize)
	}

	// Wait for result
	select {
	case result := <-resultChan:
		// Update queue length gauge after processing
		if p.metrics != nil {
			p.metrics.SetScriptPoolQueueLength(float64(len(p.jobs)))
		}
		return result.resp, result.err
	case <-ctx.Done():
		// Context canceled, return error
		return nil, ctx.Err()
	case <-p.ctx.Done():
		// Pool is shutting down; the job may never be processed
		return nil, fmt.Errorf("script pool is shut down")
	}
}

// Shutdown gracefully stops all workers and waits for them to complete.
// It closes the job queue, processes all pending jobs, and waits for workers to finish.
//
// The method will wait up to the provided context's deadline for graceful shutdown.
// If the context is canceled, the shutdown is aborted immediately.
//
// This method is idempotent - calling it multiple times is safe.
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
//	if err := pool.Shutdown(ctx); err != nil {
//	    log.Printf("shutdown error: %v", err)
//	}
func (p *ScriptWorkerPool) Shutdown(ctx context.Context) error {
	// Signal workers to stop and close the jobs channel exactly once.
	// Both operations must be inside shutdownOnce to avoid races between
	// cancel/close and concurrent Execute() or worker goroutine operations.
	p.shutdownOnce.Do(func() {
		// Mark pool as stopped so new Execute calls are rejected
		p.stopped.Store(true)
		// Cancel the internal context to signal workers to exit their main loop
		p.cancel()
		// Acquire write lock to ensure no Execute call is mid-send to p.jobs
		p.jobsMu.Lock()
		close(p.jobs)
		p.jobsMu.Unlock()
	})

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Reset metrics
		if p.metrics != nil {
			p.metrics.SetScriptPoolWorkers(0)
			p.metrics.SetScriptPoolQueueLength(0)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scriptWorker processes script execution jobs from the queue.
// Each worker runs in its own goroutine and processes jobs until the channel is closed
// or a shutdown signal is received.
func (p *ScriptWorkerPool) scriptWorker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			// Context canceled, drain remaining jobs quickly during shutdown
			for job := range p.jobs {
				// Check if job context is already canceled
				select {
				case <-job.ctx.Done():
					// Job canceled, skip processing
					continue
				default:
				}

				resp, err := p.scriptFunc(job.ctx, job.req, job.script)
				if err != nil && p.logger != nil {
					p.logger.Error("script execution error during shutdown",
						"error", err,
						"worker_id", id,
					)
				}
				// Send result (ignore if channel is closed or job canceled)
				select {
				case job.result <- scriptJobResult{resp: resp, err: err}:
				case <-job.ctx.Done():
				case <-p.ctx.Done():
				default:
				}
			}
			return
		case job, ok := <-p.jobs:
			if !ok {
				// Channel closed, exit
				return
			}

			// Process job with panic recovery
			shouldExit := p.processJobWithRecovery(id, job)
			if shouldExit {
				// Worker should exit (e.g., pool shutdown detected)
				return
			}

			// Update queue length gauge after processing
			if p.metrics != nil {
				p.metrics.SetScriptPoolQueueLength(float64(len(p.jobs)))
			}
		}
	}
}

// processJobWithRecovery executes a single job with panic recovery and proper exit condition checking.
// Returns true if the worker should exit (e.g., on pool shutdown), false otherwise.
func (p *ScriptWorkerPool) processJobWithRecovery(workerID int, job *scriptJob) bool {
	// Get worker health tracker
	p.healthMu.RLock()
	health := p.workerHealth[workerID]
	p.healthMu.RUnlock()

	// Execute with panic recovery
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("script execution panic: %v", r)

			// Log the panic
			if p.logger != nil {
				p.logger.Error("panic in script worker",
					"error", r,
					"worker_id", workerID,
				)
			}

			// Update worker health
			health.RecordPanicRecovered()

			// Try to send panic result with proper exit condition checking
			p.sendResultSafely(job, scriptJobResult{err: err})
		}
	}()

	// Check for exit conditions before processing
	select {
	case <-job.ctx.Done():
		// Job context canceled, return error and continue
		p.sendResultSafely(job, scriptJobResult{err: job.ctx.Err()})
		return false
	case <-p.ctx.Done():
		// Pool context canceled, worker should exit
		return true
	default:
		// Continue processing
	}

	// Execute the script
	resp, err := p.scriptFunc(job.ctx, job.req, job.script)

	// Update worker health
	health.RecordJobProcessed()

	// Send result with proper exit condition checking
	p.sendResultSafely(job, scriptJobResult{resp: resp, err: err})

	// After sending result, check if pool is shutting down
	select {
	case <-p.ctx.Done():
		return true
	default:
		return false
	}
}

// sendResultSafely sends a job result to the result channel with proper exit condition checking.
// It checks if the result channel is closed, job context is canceled, or pool is shutting down.
func (p *ScriptWorkerPool) sendResultSafely(job *scriptJob, result scriptJobResult) {
	select {
	case job.result <- result:
		// Result sent successfully
	case <-job.ctx.Done():
		// Job context canceled, channel may be closed
	case <-p.ctx.Done():
		// Pool shutting down, channel may be closed
	default:
		// Channel closed, unable to send result
		// This is expected during shutdown or when job context is canceled
	}
}

// SetMetrics sets the Prometheus metrics collector for tracking script pool metrics.
// This can be used to enable or disable metrics after the pool is created.
func (p *ScriptWorkerPool) SetMetrics(collector *metrics.Collector) {
	p.metrics = collector
}

// GetWorkerHealth returns the health statistics for all workers.
// This is useful for monitoring and debugging worker pool behavior.
func (p *ScriptWorkerPool) GetWorkerHealth() []map[string]interface{} {
	p.healthMu.RLock()
	defer p.healthMu.RUnlock()

	stats := make([]map[string]interface{}, len(p.workerHealth))
	for i, health := range p.workerHealth {
		jobsProcessed, panicsRecovered, lastPanicTime := health.GetStats()
		stats[i] = map[string]interface{}{
			"worker_id":        i,
			"jobs_processed":   jobsProcessed,
			"panics_recovered": panicsRecovered,
			"last_panic_time":  lastPanicTime,
		}
	}
	return stats
}
