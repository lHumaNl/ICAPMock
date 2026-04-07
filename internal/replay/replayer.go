// Copyright 2026 ICAP Mock

package replay

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// Logger interface for the replay package.
// This allows using either *slog.Logger or *logger.Logger.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// ReplayOptions configures the replay behavior.
type ReplayOptions struct {
	Callback   func(req *icap.Request, resp *icap.Response, err error)
	OnProgress func(current, total int)
	TargetURL  string
	Filter     storage.RequestFilter
	Speed      float64
	Parallel   int
	Loop       bool
}

// Stats contains replay statistics.
type Stats struct {
	StartTime          time.Time
	TotalRequests      int
	SuccessfulRequests int
	FailedRequests     int
	TotalDuration      time.Duration
}

// Replayer handles replaying recorded ICAP requests.
//
// Replayer is safe for concurrent use after creation but Start should only
// be called once at a time.
type Replayer struct {
	stats    Stats
	storage  storage.Storage
	logger   Logger
	config   *config.ReplayConfig
	client   *Client
	metrics  *metrics.Collector
	stopChan chan struct{}
	mu       sync.RWMutex
	stopOnce sync.Once
	running  bool
}

// NewReplayer creates a new Replayer instance.
//
// Parameters:
//   - cfg: Replay configuration (may be nil for defaults)
//   - store: Storage interface for loading recorded requests (must not be nil)
//   - logger: Logger for replay operations (may be nil for no logging)
//   - m: Metrics collector for recording replay metrics (may be nil for no metrics)
//
// Returns an error if store is nil.
func NewReplayer(cfg *config.ReplayConfig, store storage.Storage, log Logger, m *metrics.Collector) (*Replayer, error) {
	if store == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	// Apply default config if not provided
	if cfg == nil {
		cfg = &config.ReplayConfig{
			Enabled: true,
			Speed:   1.0,
		}
	}

	// Create default logger if not provided
	if log == nil {
		log = slog.Default()
	}

	return &Replayer{
		config:   cfg,
		storage:  store,
		client:   NewClient(30 * time.Second),
		logger:   log,
		metrics:  m,
		stopChan: make(chan struct{}),
	}, nil
}

// Start begins replaying recorded requests according to the provided options.
// This method blocks until the replay completes or the context is canceled.
//
// Parameters:
//   - ctx: Context for cancellation
//   - opts: Replay options (Speed defaults to config value if 0)
//
// Returns an error if the replay fails to start or encounters a fatal error.
// Individual request failures are recorded in stats but don't cause Start to return an error.
func (r *Replayer) Start(ctx context.Context, opts ReplayOptions) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("replay already in progress")
	}
	r.running = true
	r.stats = Stats{StartTime: time.Now()}
	r.stopChan = make(chan struct{})
	r.stopOnce = sync.Once{}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	// Apply default speed from config if not specified
	// Speed of -1 means "use config default", Speed of 0 means "no delay (max speed)"
	if opts.Speed == -1 {
		opts.Speed = r.config.Speed
		if opts.Speed == 0 {
			opts.Speed = 1.0
		}
	}
	// Speed == 0 means no delay (maximum speed) - leave as is

	// Load requests from storage
	requests, err := r.storage.ListRequests(ctx, opts.Filter)
	if err != nil {
		return fmt.Errorf("loading requests: %w", err)
	}

	if len(requests) == 0 {
		r.logger.Info("no requests to replay")
		return nil
	}

	r.logger.Info("starting replay",
		"requests", len(requests),
		"speed", opts.Speed,
		"loop", opts.Loop,
		"target", opts.TargetURL,
	)

	// Normalize parallel workers
	parallel := opts.Parallel
	if parallel < 1 {
		parallel = 1
	}

	// Track the original timeline for timing
	var lastTimestamp time.Time
	var simulatedTime time.Time

	for {
		// Reset stats for each iteration (except TotalRequests for looping)
		replayedCount := 0

		if parallel > 1 {
			// Parallel replay using worker pool
			type workItem struct {
				storedReq *storage.StoredRequest
				index     int
			}

			workCh := make(chan workItem, parallel)
			var wg sync.WaitGroup
			var progressMu sync.Mutex
			progressCount := 0

			// Start workers
			for w := 0; w < parallel; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for item := range workCh {
						req := r.convertStoredRequest(item.storedReq)
						targetURL := opts.TargetURL
						if targetURL == "" {
							targetURL = req.URI
						}

						start := time.Now()
						resp, reqErr := r.client.Do(ctx, targetURL, req)

						r.mu.Lock()
						r.stats.TotalRequests++
						if reqErr != nil {
							r.stats.FailedRequests++
						} else {
							r.stats.SuccessfulRequests++
						}
						r.stats.TotalDuration += time.Since(start)
						r.mu.Unlock()

						if r.metrics != nil {
							r.metrics.RecordReplayRequest()
							if reqErr != nil {
								r.metrics.RecordReplayFailure()
							}
							r.metrics.RecordReplayDuration(time.Since(start))
						}

						if opts.Callback != nil {
							opts.Callback(req, resp, reqErr)
						}

						if opts.OnProgress != nil {
							progressMu.Lock()
							progressCount++
							current := progressCount
							progressMu.Unlock()
							opts.OnProgress(current, len(requests))
						}
					}
				}()
			}

			// Dispatch work
			for i, storedReq := range requests {
				select {
				case <-ctx.Done():
					close(workCh)
					wg.Wait()
					r.logger.Info("replay canceled")
					return ctx.Err()
				case <-r.stopChan:
					close(workCh)
					wg.Wait()
					r.logger.Info("replay stopped")
					return nil
				case workCh <- workItem{index: i, storedReq: storedReq}:
				}
			}
			close(workCh)
			wg.Wait()
			replayedCount = len(requests)
		} else {
			// Sequential replay (original behavior)
			for i, storedReq := range requests {
				select {
				case <-ctx.Done():
					r.logger.Info("replay canceled")
					return ctx.Err()
				case <-r.stopChan:
					r.logger.Info("replay stopped")
					return nil
				default:
				}

				// Convert stored request to ICAP request
				req := r.convertStoredRequest(storedReq)

				// Override target URL if specified
				targetURL := opts.TargetURL
				if targetURL == "" {
					targetURL = req.URI
				}

				// Calculate and apply timing delay to maintain original timeline
				if opts.Speed > 0 && i > 0 {
					timeSinceLast := storedReq.Timestamp.Sub(lastTimestamp)
					delay := time.Duration(float64(timeSinceLast) / opts.Speed)

					// Wait for the delay or until context/stops
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return ctx.Err()
					case <-r.stopChan:
						return nil
					}

					// Update simulated time
					simulatedTime = simulatedTime.Add(timeSinceLast)
				}

				lastTimestamp = storedReq.Timestamp

				// Send the request
				start := time.Now()
				resp, err := r.client.Do(ctx, targetURL, req)

				// Update statistics
				r.mu.Lock()
				r.stats.TotalRequests++
				if err != nil {
					r.stats.FailedRequests++
				} else {
					r.stats.SuccessfulRequests++
				}
				r.stats.TotalDuration += time.Since(start)
				r.mu.Unlock()

				// Record metrics
				if r.metrics != nil {
					r.metrics.RecordReplayRequest()
					if err != nil {
						r.metrics.RecordReplayFailure()
					}
					r.metrics.RecordReplayDuration(time.Since(start))

					// Track how far behind we are from original timeline
					if opts.Speed > 0 {
						realElapsed := time.Since(r.stats.StartTime)
						if i > 0 {
							originalElapsed := lastTimestamp.Sub(requests[0].Timestamp)
							behind := realElapsed.Seconds() - (originalElapsed.Seconds() / opts.Speed)
							r.metrics.SetReplayBehindOriginal(behind)
						}
					}
				}

				// Call callback if provided
				if opts.Callback != nil {
					opts.Callback(req, resp, err)
				}

				// Call progress callback if provided
				if opts.OnProgress != nil {
					opts.OnProgress(i+1, len(requests))
				}

				replayedCount++
			}
		}

		// If not looping, we're done
		if !opts.Loop {
			break
		}

		r.logger.Info("replay iteration complete, restarting",
			"requests_replayed", replayedCount,
		)
	}

	r.logger.Info("replay completed",
		"total_requests", r.stats.TotalRequests,
		"successful", r.stats.SuccessfulRequests,
		"failed", r.stats.FailedRequests,
		"duration", r.stats.TotalDuration,
	)

	return nil
}

// Stop stops an in-progress replay.
// If no replay is running, this is a no-op.
// Stop signals the replayer to stop. Safe to call multiple times or concurrently.
func (r *Replayer) Stop() {
	r.mu.RLock()
	running := r.running
	r.mu.RUnlock()

	if running {
		r.stopOnce.Do(func() {
			close(r.stopChan)
		})
	}
}

// Stats returns the current replay statistics.
// This is safe to call while a replay is in progress.
func (r *Replayer) Stats() Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}

// IsRunning returns true if a replay is currently in progress.
func (r *Replayer) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// convertStoredRequest converts a StoredRequest to an icap.Request.
func (r *Replayer) convertStoredRequest(sr *storage.StoredRequest) *icap.Request {
	req := &icap.Request{
		Method:     sr.Method,
		URI:        sr.URI,
		Proto:      icap.Version,
		Header:     make(icap.Header),
		ClientIP:   sr.ClientIP,
		RemoteAddr: sr.RemoteAddr,
	}

	// Copy ICAP headers
	for k, v := range sr.Headers {
		req.Header[k] = v
	}

	// Convert HTTP request if present
	if sr.HTTPRequest != nil {
		req.HTTPRequest = &icap.HTTPMessage{
			Method: sr.HTTPRequest.Method,
			URI:    sr.HTTPRequest.URI,
			Proto:  sr.HTTPRequest.Proto,
			Header: make(icap.Header),
		}
		for k, v := range sr.HTTPRequest.Headers {
			req.HTTPRequest.Header[k] = v
		}
		if sr.HTTPRequest.Body != "" {
			req.HTTPRequest.SetLoadedBody([]byte(sr.HTTPRequest.Body))
		}
	}

	// Convert HTTP response if present
	if sr.HTTPResponse != nil {
		req.HTTPResponse = &icap.HTTPMessage{
			Status:     sr.HTTPResponse.Status,
			StatusText: sr.HTTPResponse.StatusText,
			Proto:      sr.HTTPResponse.Proto,
			Header:     make(icap.Header),
		}
		for k, v := range sr.HTTPResponse.Headers {
			req.HTTPResponse.Header[k] = v
		}
		if sr.HTTPResponse.Body != "" {
			req.HTTPResponse.SetLoadedBody([]byte(sr.HTTPResponse.Body))
		}
	}

	return req
}

// SetClient allows setting a custom ICAP client.
// This is useful for testing or using custom client configurations.
func (r *Replayer) SetClient(client *Client) {
	if client != nil {
		r.client = client
	}
}
