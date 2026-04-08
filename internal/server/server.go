// Copyright 2026 ICAP Mock

// Package server implements the core ICAP protocol server and connection handling.
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/icap-mock/icap-mock/internal/circuitbreaker"
	"github.com/icap-mock/icap-mock/internal/config"
	icaperrors "github.com/icap-mock/icap-mock/internal/errors"
	"github.com/icap-mock/icap-mock/internal/metrics"
	"github.com/icap-mock/icap-mock/internal/router"
	"github.com/icap-mock/icap-mock/internal/util"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// contextKey is the type for context keys in this package.
// Using a custom type prevents collisions with keys from other packages.
type contextKey string

const (
	// requestIDKey is the context key for the request ID.
	requestIDKey contextKey = "request_id"
	// clientIPKey is the context key for the client IP address.
	clientIPKey contextKey = "client_ip"
)

// RequestIDFromContext retrieves the request ID from the context.
// Returns an empty string if not found.
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// ClientIPFromContext retrieves the client IP from the context.
// Returns an empty string if not found.
func ClientIPFromContext(ctx context.Context) string {
	if v := ctx.Value(clientIPKey); v != nil {
		if ip, ok := v.(string); ok {
			return ip
		}
	}
	return ""
}

// Server is the ICAP server interface.
type Server interface {
	// Start starts the server and begins accepting connections.
	// It returns an error if the server fails to start.
	Start(ctx context.Context) error
	// Stop gracefully stops the server.
	// It waits for all active connections to complete.
	Stop(ctx context.Context) error
	// Addr returns the server's listening address.
	// Returns nil if the server hasn't been started.
	Addr() net.Addr
}

// GoroutineMonitorConfig holds configuration for goroutine leak detection.
type GoroutineMonitorConfig struct {
	// CheckInterval is the interval between goroutine count checks.
	CheckInterval time.Duration
	// WarningThreshold is the multiplier for warning level alerts.
	// A warning is logged when current count exceeds baseline * WarningThreshold.
	WarningThreshold float64
	// CriticalThreshold is the multiplier for critical level alerts.
	// A critical warning is logged when current count exceeds baseline * CriticalThreshold.
	CriticalThreshold float64
	// SustainedGrowthChecks is the number of consecutive checks with growth
	// before alerting on sustained growth.
	SustainedGrowthChecks int
}

// DefaultGoroutineMonitorConfig returns the default configuration for goroutine monitoring.
func DefaultGoroutineMonitorConfig() GoroutineMonitorConfig {
	return GoroutineMonitorConfig{
		CheckInterval:         30 * time.Second,
		WarningThreshold:      1.5,
		CriticalThreshold:     2.0,
		SustainedGrowthChecks: 3,
	}
}

// GoroutineStats holds current goroutine monitoring statistics.
type GoroutineStats struct {
	LastCheck         time.Time
	AlertLevel        string
	Baseline          int
	Current           int
	Peak              int
	GrowthRate        float64
	ConsecutiveGrowth int
}

// ICAPServer implements the ICAP Server interface.
// It handles incoming ICAP connections and routes requests to handlers.
type ICAPServer struct {
	addr                       net.Addr
	listener                   net.Listener
	serverCtx                  context.Context
	scenarioCircuitBreaker     *circuitbreaker.CircuitBreaker
	router                     *router.Router
	pool                       *ConnectionPool
	semaphore                  chan struct{}
	metricsCircuitBreaker      *circuitbreaker.CircuitBreaker
	tlsMonitor                 *TLSCertificateMonitor
	storageCircuitBreaker      *circuitbreaker.CircuitBreaker
	stopChan                   chan struct{}
	tcpListener                *net.TCPListener
	config                     *config.ServerConfig
	logger                     *slog.Logger
	metrics                    *metrics.Collector
	goroutineConfig            GoroutineMonitorConfig
	wg                         sync.WaitGroup
	goroutinePeak              int
	goroutineConsecutiveGrowth int
	goroutineBaseline          int
	goroutineMu                sync.RWMutex
	runningMu                  sync.RWMutex
	stopOnce                   sync.Once
	running                    bool
}

// NewServer creates a new ICAP server with the given configuration, connection pool, and logger.
//
// Dependencies are injected to enable:
//   - Testing with mock pools and loggers (improved testability)
//   - Pool sharing across components if needed
//   - Consistent logging configuration across the application
//   - Decoupling server creation from dependency creation
//   - Avoiding global state in the server implementation
//
// Parameters:
//   - cfg: Server configuration including host, port, timeouts, etc.
//   - pool: Connection pool for managing active connections (injected dependency).
//     Must not be nil - use NewConnectionPool() to create a default pool.
//   - logger: Structured logger for server operations. If nil, uses slog.Default().
//
// Returns the new server or an error if the configuration or pool is invalid.
func NewServer(cfg *config.ServerConfig, pool *ConnectionPool, logger *slog.Logger) (*ICAPServer, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}
	if pool == nil {
		return nil, errors.New("pool cannot be nil")
	}

	// Use provided logger or fall back to default
	// This maintains backward compatibility while preferring explicit dependency injection
	if logger == nil {
		logger = slog.Default()
	}

	return &ICAPServer{
		config:            cfg,
		pool:              pool,
		semaphore:         make(chan struct{}, cfg.MaxConnections),
		stopChan:          make(chan struct{}),
		logger:            logger,
		goroutineConfig:   DefaultGoroutineMonitorConfig(),
		goroutineBaseline: runtime.NumGoroutine(),
		goroutinePeak:     runtime.NumGoroutine(),
	}, nil
}

// SetLogger sets the logger for the server.
//
// Deprecated: Prefer passing the logger to NewServer for explicit dependency injection.
// This method is kept for backward compatibility with existing code.
func (s *ICAPServer) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

// SetRouter sets the router for the server.
// This must be called before Start.
func (s *ICAPServer) SetRouter(r *router.Router) {
	s.router = r
}

// SetMetrics sets the metrics collector for the server.
// This is optional - if not set, goroutine monitoring will only log warnings.
func (s *ICAPServer) SetMetrics(m *metrics.Collector) {
	s.metrics = m
}

// SetGoroutineMonitorConfig configures the goroutine leak detection monitoring.
// This should be called before Start() for the configuration to take effect.
// If not called, default configuration is used.
func (s *ICAPServer) SetGoroutineMonitorConfig(cfg GoroutineMonitorConfig) {
	s.goroutineMu.Lock()
	defer s.goroutineMu.Unlock()
	s.goroutineConfig = cfg
}

// SetCircuitBreakers sets the circuit breakers for the server.
// This should be called before Start() for the circuit breakers to be active.
// Circuit breakers protect external operations from cascade failures.
//
// Parameters:
//   - storageCB: Circuit breaker for storage operations
//   - scenarioCB: Circuit breaker for scenario loading
//   - metricsCB: Circuit breaker for metrics collection
//
// Example:
//
//	server.SetCircuitBreakers(storageCB, scenarioCB, metricsCB)
func (s *ICAPServer) SetCircuitBreakers(
	storageCB *circuitbreaker.CircuitBreaker,
	scenarioCB *circuitbreaker.CircuitBreaker,
	metricsCB *circuitbreaker.CircuitBreaker,
) {
	s.storageCircuitBreaker = storageCB
	s.scenarioCircuitBreaker = scenarioCB
	s.metricsCircuitBreaker = metricsCB
}

// StorageCircuitBreaker returns the storage circuit breaker.
func (s *ICAPServer) StorageCircuitBreaker() *circuitbreaker.CircuitBreaker {
	return s.storageCircuitBreaker
}

// ScenarioCircuitBreaker returns the scenario loader circuit breaker.
func (s *ICAPServer) ScenarioCircuitBreaker() *circuitbreaker.CircuitBreaker {
	return s.scenarioCircuitBreaker
}

// MetricsCircuitBreaker returns the metrics server circuit breaker.
func (s *ICAPServer) MetricsCircuitBreaker() *circuitbreaker.CircuitBreaker {
	return s.metricsCircuitBreaker
}

// GetGoroutineStats returns the current goroutine monitoring statistics.
// This is safe to call at any time and provides insight into goroutine health.
func (s *ICAPServer) GetGoroutineStats() GoroutineStats {
	s.goroutineMu.RLock()
	defer s.goroutineMu.RUnlock()

	current := runtime.NumGoroutine()
	var growthRate float64
	if s.goroutineBaseline > 0 {
		growthRate = float64(current-s.goroutineBaseline) / float64(s.goroutineBaseline) * 100
	}

	alertLevel := "normal"
	if float64(current) > float64(s.goroutineBaseline)*s.goroutineConfig.CriticalThreshold {
		alertLevel = "critical"
	} else if float64(current) > float64(s.goroutineBaseline)*s.goroutineConfig.WarningThreshold {
		alertLevel = "warning"
	}

	return GoroutineStats{
		Baseline:          s.goroutineBaseline,
		Current:           current,
		Peak:              s.goroutinePeak,
		GrowthRate:        growthRate,
		ConsecutiveGrowth: s.goroutineConsecutiveGrowth,
		LastCheck:         time.Now(),
		AlertLevel:        alertLevel,
	}
}

// ResetGoroutineBaseline resets the goroutine baseline to the current count.
// This can be useful after a known spike in goroutines (e.g., load test)
// to establish a new normal baseline.
func (s *ICAPServer) ResetGoroutineBaseline() {
	s.goroutineMu.Lock()
	defer s.goroutineMu.Unlock()

	current := runtime.NumGoroutine()
	s.goroutineBaseline = current
	s.goroutinePeak = current
	s.goroutineConsecutiveGrowth = 0

	s.logger.Info("goroutine baseline reset",
		"new_baseline", current,
	)
}

// Start starts the ICAP server and begins accepting connections.
// It returns an error if the server is already running or fails to start.
func (s *ICAPServer) Start(ctx context.Context) error {
	if s.router == nil {
		if s.logger != nil {
			s.logger.Warn("starting server without router: all requests will return 'No router configured'")
		}
	}

	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		return errors.New("server already running")
	}
	s.running = true
	s.serverCtx = ctx
	s.runningMu.Unlock()

	// Create listener
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var err error
	// Always create TCP listener first to enable deadline control
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		s.running = false
		return fmt.Errorf("resolving TCP address: %w", err)
	}

	s.tcpListener, err = net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		s.running = false
		return fmt.Errorf("creating TCP listener: %w", err)
	}

	if s.config.TLS.Enabled {
		// Wrap TCP listener with TLS
		cert, err := tls.LoadX509KeyPair(s.config.TLS.CertFile, s.config.TLS.KeyFile)
		if err != nil {
			_ = s.tcpListener.Close()
			s.running = false
			return fmt.Errorf("loading TLS certificate: %w", err)
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		// mTLS: load client CA and set client auth policy if configured
		if s.config.TLS.ClientCAFile != "" {
			caCert, err := os.ReadFile(s.config.TLS.ClientCAFile)
			if err != nil {
				_ = s.tcpListener.Close()
				s.running = false
				return fmt.Errorf("loading client CA certificate: %w", err)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caCert) {
				_ = s.tcpListener.Close()
				s.running = false
				return fmt.Errorf("parsing client CA certificate: no valid PEM block found in %s", s.config.TLS.ClientCAFile)
			}
			tlsConfig.ClientCAs = caPool
		}

		switch s.config.TLS.ClientAuth {
		case "optional":
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		case "required":
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		default:
			// "none" or empty
			tlsConfig.ClientAuth = tls.NoClientCert
		}

		s.listener = tls.NewListener(s.tcpListener, tlsConfig)
	} else {
		// Use TCP listener directly
		s.listener = s.tcpListener
	}

	s.addr = s.listener.Addr()

	// Initialize goroutine baseline at startup
	s.goroutineMu.Lock()
	s.goroutineBaseline = runtime.NumGoroutine()
	s.goroutinePeak = s.goroutineBaseline
	s.goroutineConsecutiveGrowth = 0
	s.goroutineMu.Unlock()

	s.logger.Info("goroutine monitoring initialized",
		"baseline", s.goroutineBaseline,
		"check_interval", s.goroutineConfig.CheckInterval,
	)

	// Start accepting connections in a goroutine
	s.wg.Add(1)
	go s.acceptLoop() //nolint:contextcheck // acceptLoop uses s.serverCtx stored from Start's ctx parameter

	// Start goroutine leak monitoring
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.monitorGoroutines()
	}()

	// Start TLS certificate monitoring
	s.tlsMonitor = NewTLSCertificateMonitor(&s.config.TLS, s.logger, s.metrics)
	if s.tlsMonitor != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.tlsMonitor.Start()
		}()
		s.logger.Info("TLS certificate monitoring initialized",
			"cert_file", s.config.TLS.CertFile,
			"check_interval", s.config.TLS.CertCheckInterval,
			"warning_days", s.config.TLS.ExpiryWarningDays,
		)
	}

	return nil
}

// acceptLoop accepts incoming connections and handles them.
func (s *ICAPServer) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopChan:
			return
		default:
			// Set accept deadline to allow checking stopChan periodically.
			//
			// IMPORTANT: We set the deadline on tcpListener (the underlying TCP listener)
			// rather than on listener (which may be a TLS wrapper). This works because:
			//
			// For non-TLS: listener == tcpListener, so deadline affects Accept() directly.
			//
			// For TLS: listener = tls.NewListener(tcpListener, config), which wraps tcpListener.
			// The TLS listener's Accept() internally calls tcpListener.Accept(), so the
			// deadline set on tcpListener is respected. This ensures graceful shutdown
			// works correctly for both TLS and non-TLS connections.
			//
			// The 100ms deadline allows the accept loop to check stopChan frequently
			// during graceful shutdown, minimizing shutdown latency.
			if s.tcpListener != nil {
				_ = s.tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond))
			}

			netConn, err := s.listener.Accept()
			if err != nil {
				// Check if this is a timeout (expected for deadline)
				var netErr net.Error
				if errors.As(err, &netErr) {
					continue
				}
				// Check if listener was closed
				select {
				case <-s.stopChan:
					return
				default:
					// Log the error but continue accepting
					continue
				}
			}

			// Check connection limit
			select {
			case s.semaphore <- struct{}{}:
				// Got a slot, handle the connection
			default:
				// Connection limit reached, reject
				_ = netConn.Close()
				continue
			}

			// Create connection wrapper
			connConfig := &ConnectionConfig{
				ReadTimeout:  s.config.ReadTimeout,
				WriteTimeout: s.config.WriteTimeout,
				MaxBodySize:  s.config.MaxBodySize,
				Streaming:    s.config.Streaming,
				IdleTimeout:  s.config.IdleTimeout,
			}
			conn := newConnection(netConn, connConfig)

			// Add to pool
			s.pool.Add(conn)

			// Handle connection in a goroutine
			s.wg.Add(1)
			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single client connection.
// It creates a request-scoped context for proper cancellation and timeout handling.
// The context is canceled when:
//   - The server shuts down (stopChan closed)
//   - The request timeout is exceeded
//   - The connection is closed
func (s *ICAPServer) handleConnection(conn *Connection) { //nolint:gocyclo // connection lifecycle requires sequential checks for idle, parse, route, write, close
	// Create connection-scoped context that cancels on server shutdown
	// This ensures all in-flight requests are canceled during graceful shutdown
	connCtx, connCancel := context.WithCancel(s.serverCtx)

	// No extra goroutine needed to cancel on server stop:
	// The main request loop checks stopChan and returns, which triggers
	// the deferred connCancel(). This avoids one goroutine per connection.

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in connection handler",
				"error", r,
				"remote_addr", conn.RemoteAddr(),
			)
			// connCancel() will be called by the defer below
		}
		// Cancel connection-scoped context to stop any ongoing operations
		connCancel()
		_ = conn.Close()
		s.pool.Remove(conn)
		<-s.semaphore // Release slot
		s.wg.Done()
	}()

	// Set initial deadline
	if s.config.ReadTimeout > 0 {
		now := time.Now()
		_ = conn.SetReadDeadline(now.Add(s.config.ReadTimeout))
	}

	// Handle requests in a loop for connection reuse
	for {
		select {
		case <-s.stopChan:
			return
		case <-connCtx.Done():
			// Server is shutting down, stop handling
			return
		default:
			// Cache current time for this iteration to avoid repeated syscalls
			now := time.Now()

			// Check for idle timeout before processing request
			if conn.IsIdle() {
				// Connection has been idle longer than allowed timeout
				s.logger.Warn("connection closed due to idle timeout",
					"remote_addr", conn.RemoteAddr(),
					"idle_duration", time.Since(conn.LastActivity()),
					"idle_timeout", conn.config.IdleTimeout,
				)

				// Record metric
				if s.metrics != nil {
					s.metrics.RecordIdleConnectionClosed("idle")
				}

				// Send error to client if possible
				_ = writeResponseFromICAP(conn.Writer(), icap.NewResponseError(
					icaperrors.ErrIdleTimeout.ICAPStatus,
					icaperrors.ErrIdleTimeout.Message,
				))
				_ = conn.Flush()

				// Close connection
				return
			}

			// Parse the ICAP request
			req, err := parseICAPRequest(conn.Reader())
			if err != nil {
				// Connection closed or error, stop handling
				return
			}

			// Extract client IP
			req.RemoteAddr = conn.RemoteAddr()
			req.ClientIP = extractClientIP(req.Header, conn.RemoteAddr())

			// Reset deadline for processing
			if s.config.WriteTimeout > 0 {
				_ = conn.SetWriteDeadline(now.Add(s.config.WriteTimeout))
			}

			// Create request-scoped context with timeout
			// Derive from connection context to inherit server shutdown cancellation
			// Use WriteTimeout as the processing deadline to ensure timely response
			ctxTimeout := s.config.WriteTimeout
			if ctxTimeout <= 0 {
				ctxTimeout = 30 * time.Second // Default fallback
			}
			ctx, cancel := context.WithTimeout(connCtx, ctxTimeout)

			// Add request metadata to context for tracing and logging
			requestID := util.GenerateRequestID(now)
			ctx = context.WithValue(ctx, requestIDKey, requestID)
			ctx = context.WithValue(ctx, clientIPKey, req.ClientIP)

			// Route and handle the request with request-scoped context
			var resp *icap.Response
			if s.router != nil {
				resp, err = s.router.Serve(ctx, req)
			} else {
				// No router configured, return error
				resp = icap.NewResponseError(icap.StatusInternalServerError, "No router configured")
			}

			// Cancel the request context after processing
			cancel()

			if err != nil {
				resp = icap.NewResponseError(icap.StatusInternalServerError, err.Error())
			}

			// Write the response
			if err := writeResponseFromICAP(conn.Writer(), resp); err != nil {
				return
			}

			// Check for connection close header
			if connHeader, ok := resp.GetHeader("Connection"); ok {
				if connHeader == "close" {
					return
				}
			}

			// Reset deadline for next request
			if s.config.ReadTimeout > 0 {
				_ = conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)) // fresh time for next request wait
			}
		}
	}
}

// Stop gracefully stops the server.
// It stops accepting new connections and waits for all active connections to complete.
// If the context deadline is exceeded, it forcibly closes all remaining connections.
func (s *ICAPServer) Stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		// Log shutdown start with timeout info
		if deadline, ok := ctx.Deadline(); ok {
			timeout := time.Until(deadline)
			s.logger.Info("initiating graceful shutdown",
				"timeout", timeout,
				"active_connections", s.pool.Count(),
			)
		} else {
			s.logger.Info("initiating graceful shutdown",
				"active_connections", s.pool.Count(),
			)
		}

		// Stop TLS certificate monitor
		if s.tlsMonitor != nil {
			s.tlsMonitor.Stop()
		}

		// Signal accept loop to stop
		close(s.stopChan)

		// Close the listener
		if s.listener != nil {
			err = s.listener.Close()
		}

		// Wait for all connection handlers to complete with timeout
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			s.logger.Info("all connections closed gracefully")
		case <-ctx.Done():
			// Graceful shutdown timeout exceeded, force close remaining connections
			s.logger.Warn("graceful shutdown timeout exceeded, forcing connection close",
				"timeout", ctx.Err(),
				"active_connections", s.pool.Count(),
			)

			// Force close all remaining connections
			s.forceCloseAllConnections()
		}

		// Log shutdown progress before waiting for pool drain
		remainingConns := s.pool.Count()
		if remainingConns > 0 {
			s.logger.Info("waiting for connections to close",
				"remaining_connections", remainingConns,
			)
		}

		// Wait for connection pool to drain (with remaining timeout from ctx)
		s.pool.Wait(ctx)

		s.runningMu.Lock()
		s.running = false
		s.runningMu.Unlock()
	})

	return err
}

// forceCloseAllConnections forcibly closes all active connections.
// This is called during force shutdown when graceful shutdown timeout is exceeded.
// All connections are closed immediately without waiting for them to complete.
func (s *ICAPServer) forceCloseAllConnections() {
	// Get list of all active connections from the pool
	conns := s.pool.List()
	count := len(conns)

	// Log the number of connections being force closed
	s.logger.Warn("forcing close of all active connections",
		"connection_count", count,
	)

	// Close each connection
	for _, conn := range conns {
		// Connection.Close() is safe to call multiple times (uses sync.Once)
		// It will flush any pending data and close the underlying net.Conn
		if closeErr := conn.Close(); closeErr != nil {
			// Log error but continue closing other connections
			s.logger.Debug("error closing connection during force shutdown",
				"error", closeErr,
				"remote_addr", conn.RemoteAddr(),
			)
		}
	}

	s.logger.Info("force shutdown completed",
		"connections_closed", count,
	)
}

// Addr returns the server's listening address.
// Returns nil if the server hasn't been started.
func (s *ICAPServer) Addr() net.Addr {
	return s.addr
}

// IsRunning returns true if the server is currently running.
func (s *ICAPServer) IsRunning() bool {
	s.runningMu.RLock()
	defer s.runningMu.RUnlock()
	return s.running
}

// ConnectionCount returns the current number of active connections.
func (s *ICAPServer) ConnectionCount() int {
	return s.pool.Count()
}

// monitorGoroutines periodically checks for potential goroutine leaks.
// It implements a multi-level alerting system based on growth from baseline:
//   - Warning: goroutine count exceeds baseline * WarningThreshold
//   - Critical: goroutine count exceeds baseline * CriticalThreshold
//   - Sustained Growth: consecutive checks show growth exceeding SustainedGrowthChecks
//
// The function also tracks peak goroutine count and updates Prometheus metrics.
func (s *ICAPServer) monitorGoroutines() {
	ticker := time.NewTicker(s.goroutineConfig.CheckInterval)
	defer ticker.Stop()

	var lastCount int
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			current := runtime.NumGoroutine()

			s.goroutineMu.Lock()
			// Update peak if current is higher
			if current > s.goroutinePeak {
				s.goroutinePeak = current
			}

			// Track consecutive growth
			if current > lastCount && lastCount > 0 {
				s.goroutineConsecutiveGrowth++
			} else {
				s.goroutineConsecutiveGrowth = 0
			}

			baseline := s.goroutineBaseline
			peak := s.goroutinePeak
			consecutiveGrowth := s.goroutineConsecutiveGrowth
			cfg := s.goroutineConfig
			s.goroutineMu.Unlock()

			// Calculate growth rate
			var growthRate float64
			if baseline > 0 {
				growthRate = float64(current-baseline) / float64(baseline) * 100
			}

			// Check for critical threshold
			if float64(current) > float64(baseline)*cfg.CriticalThreshold {
				s.logger.Warn("critical goroutine count detected",
					"current", current,
					"baseline", baseline,
					"peak", peak,
					"growth_rate", fmt.Sprintf("%.1f%%", growthRate),
					"threshold", cfg.CriticalThreshold,
				)
			} else if float64(current) > float64(baseline)*cfg.WarningThreshold {
				// Check for warning threshold
				s.logger.Warn("elevated goroutine count detected",
					"current", current,
					"baseline", baseline,
					"peak", peak,
					"growth_rate", fmt.Sprintf("%.1f%%", growthRate),
					"threshold", cfg.WarningThreshold,
				)
			}

			// Check for sustained growth pattern
			if consecutiveGrowth >= cfg.SustainedGrowthChecks {
				s.logger.Warn("sustained goroutine growth detected",
					"current", current,
					"baseline", baseline,
					"consecutive_checks", consecutiveGrowth,
					"growth_rate", fmt.Sprintf("%.1f%%", growthRate),
				)
			}

			// Log periodic status at debug level
			s.logger.Debug("goroutine monitor check",
				"current", current,
				"baseline", baseline,
				"peak", peak,
				"growth_rate", fmt.Sprintf("%.1f%%", growthRate),
			)

			lastCount = current

			// Update metric if available
			if s.metrics != nil {
				s.metrics.SetGoroutines(current)
			}
		}
	}
}

// ensure ICAPServer implements Server interface.
var _ Server = (*ICAPServer)(nil)
