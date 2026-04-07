// Copyright 2026 ICAP Mock

package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// defaultLogger is the global default logger instance, stored atomically for thread safety.
var defaultLogger atomic.Value // stores *Logger

// Logger wraps slog.Logger with additional functionality for ICAP logging.
// It provides structured logging with support for JSON and text formats,
// configurable log levels, and specialized helpers for ICAP request logging.
type Logger struct {
	closer io.Closer
	*slog.Logger
	levelVar *slog.LevelVar
	cfg      config.LoggingConfig
	cfgMu    sync.RWMutex
}

// Option is a functional option for configuring the logger.
type Option func(*loggerOptions)

type loggerOptions struct {
	writer io.Writer
}

// WithWriter returns an option that sets the output writer for the logger.
// This is useful for testing or custom output destinations.
func WithWriter(w io.Writer) Option {
	return func(o *loggerOptions) {
		o.writer = w
	}
}

// New creates a new Logger instance with the given configuration.
// It supports output to stdout, stderr, or a file path.
// For file output, log rotation is automatically configured using lumberjack.
//
// The configuration supports the following options:
//   - Level: Log level (debug, info, warn, error)
//   - Format: Output format (json, text)
//   - Output: Output destination (stdout, stderr, or file path)
//   - MaxSize: Maximum log file size in MB before rotation
//   - MaxBackups: Maximum number of old log files to retain
//   - MaxAge: Maximum number of days to retain old log files
func New(cfg config.LoggingConfig, opts ...Option) (*Logger, error) {
	options := &loggerOptions{}
	for _, opt := range opts {
		opt(options)
	}

	var writer io.Writer
	var closer io.Closer

	if options.writer != nil {
		writer = options.writer
	} else {
		switch cfg.Output {
		case "stdout", "":
			writer = os.Stdout
		case "stderr":
			writer = os.Stderr
		default:
			// File output with rotation
			lj := &lumberjack.Logger{
				Filename:   cfg.Output,
				MaxSize:    cfg.MaxSize,    // MB
				MaxBackups: cfg.MaxBackups, // Number of backups
				MaxAge:     cfg.MaxAge,     // Days
				Compress:   true,           // Compress rotated files
			}
			writer = lj
			closer = lj
		}
	}

	return newLoggerWithWriter(cfg, writer, closer)
}

// NewWithWriter creates a new Logger that writes to the provided writer.
// This is primarily useful for testing or when you need complete control
// over the output destination.
func NewWithWriter(cfg config.LoggingConfig, w io.Writer) (*Logger, error) {
	return newLoggerWithWriter(cfg, w, nil)
}

// newLoggerWithWriter creates a logger with the specified writer and closer.
func newLoggerWithWriter(cfg config.LoggingConfig, w io.Writer, closer io.Closer) (*Logger, error) {
	levelVar := &slog.LevelVar{}
	levelVar.Set(parseLevel(cfg.Level))

	opts := &slog.HandlerOptions{
		Level: levelVar,
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		// Default to JSON format
		handler = slog.NewJSONHandler(w, opts)
	}

	logger := slog.New(handler)

	return &Logger{
		Logger:   logger,
		cfg:      cfg,
		closer:   closer,
		levelVar: levelVar,
	}, nil
}

// parseLevel converts a string log level to slog.Level.
// Returns slog.LevelInfo for invalid or empty values.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetDefault creates and sets the global default logger.
// Returns the created logger for convenience.
func SetDefault(cfg config.LoggingConfig) (*Logger, error) {
	logger, err := New(cfg)
	if err != nil {
		return nil, err
	}
	defaultLogger.Store(logger)
	return logger, nil
}

// Default returns the global default logger.
// Returns nil if no default logger has been set.
func Default() *Logger {
	v := defaultLogger.Load()
	if v == nil {
		return nil
	}
	return v.(*Logger) //nolint:errcheck
}

// Close closes the logger and releases any resources.
// This should be called when the logger is no longer needed,
// especially when using file output with rotation.
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// Sync flushes any buffered log entries.
// This is a no-op for most handlers but is provided for compatibility.
func (l *Logger) Sync() error {
	return nil
}

// Handler returns the underlying slog.Handler.
// This allows advanced customization of the logging behavior.
func (l *Logger) Handler() slog.Handler {
	return l.Logger.Handler()
}

// With returns a Logger that includes the given key-value pairs in all log entries.
// This is useful for adding context to a series of log messages.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		Logger:   l.Logger.With(args...),
		cfg:      l.cfg,
		closer:   nil, // Don't allow closing the derived logger
		levelVar: l.levelVar,
	}
}

// WithRequestID returns a Logger that includes the request ID in all log entries.
// This is useful for tracing a request through the system.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.With("request_id", requestID)
}

// LogRequest logs an ICAP request/response pair with standardized fields.
// This helper ensures consistent log formatting for request processing.
//
// The log entry includes the following fields:
//   - msg: "request processed"
//   - request_id: From X-Request-ID header if present
//   - method: ICAP method (REQMOD, RESPMOD, OPTIONS)
//   - client_ip: Client IP address
//   - duration_ms: Processing duration in milliseconds
//   - response_status: ICAP response status code
//   - scenario: Optional scenario name
//   - error: Error message if applicable (null otherwise)
//
// Example log output:
//
//	{
//	  "time": "2024-01-15T10:30:00.123Z",
//	  "level": "INFO",
//	  "msg": "request processed",
//	  "request_id": "req-20240115-001",
//	  "method": "REQMOD",
//	  "client_ip": "192.168.1.100",
//	  "duration_ms": 5,
//	  "response_status": 204,
//	  "scenario": "block-ads",
//	  "error": null
//	}
func (l *Logger) LogRequest(req *icap.Request, resp *icap.Response, duration time.Duration, scenario string) {
	if req == nil {
		return
	}

	// Extract request ID from headers
	requestID, _ := req.GetHeader("X-Request-ID")

	// Build log attributes
	args := []any{
		"msg", "request processed",
		"method", req.Method,
		"client_ip", req.ClientIP,
		"duration_ms", duration.Milliseconds(),
	}

	if requestID != "" {
		args = append(args, "request_id", requestID)
	}

	if resp != nil {
		args = append(args, "response_status", resp.StatusCode)
	}

	if scenario != "" {
		args = append(args, "scenario", scenario)
	}

	l.Info("", args...)
}

// LogError logs an error with additional context information.
// This helper ensures consistent formatting for error logging.
//
// The log entry includes the following fields:
//   - msg: "error occurred"
//   - error: The error message
//   - All key-value pairs from the context map
//
// Example usage:
//
//	logger.LogError(err, map[string]interface{}{
//	    "service":  "icap-server",
//	    "port":     1344,
//	    "attempts": 3,
//	})
//
// Example log output:
//
//	{
//	  "time": "2024-01-15T10:30:00.123Z",
//	  "level": "ERROR",
//	  "msg": "error occurred",
//	  "error": "connection refused",
//	  "service": "icap-server",
//	  "port": 1344,
//	  "attempts": 3
//	}
func (l *Logger) LogError(err error, context map[string]interface{}) {
	if err == nil {
		return
	}

	args := []any{
		"error", err.Error(),
	}

	// Add context fields
	for k, v := range context {
		args = append(args, k, v)
	}

	l.Error("", args...)
}

// Debug logs a message at DEBUG level.
// Debug messages contain detailed information for debugging and development.
func (l *Logger) Debug(msg string, args ...any) {
	l.Logger.Debug(msg, args...)
}

// Info logs a message at INFO level.
// Info messages contain general operational information.
func (l *Logger) Info(msg string, args ...any) {
	l.Logger.Info(msg, args...)
}

// Warn logs a message at WARN level.
// Warn messages indicate potential issues that don't prevent operation.
func (l *Logger) Warn(msg string, args ...any) {
	l.Logger.Warn(msg, args...)
}

// Error logs a message at ERROR level.
// Error messages indicate failures that affect operation.
func (l *Logger) Error(msg string, args ...any) {
	l.Logger.Error(msg, args...)
}

// DebugContext logs a message at DEBUG level with context.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.Logger.DebugContext(ctx, msg, args...)
}

// InfoContext logs a message at INFO level with context.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.Logger.InfoContext(ctx, msg, args...)
}

// WarnContext logs a message at WARN level with context.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.Logger.WarnContext(ctx, msg, args...)
}

// ErrorContext logs a message at ERROR level with context.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.Logger.ErrorContext(ctx, msg, args...)
}

// Log logs a message at the specified level.
// This is provided for compatibility with slog.Logger.Log.
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.Logger.Log(ctx, level, msg, args...)
}

// LogAttrs logs a message at the specified level with slog.Attr values.
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.Logger.LogAttrs(ctx, level, msg, attrs...)
}

// WithGroup returns a Logger that starts a group for all log entries.
// All subsequent attributes will be nested under the group name.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		Logger:   l.Logger.WithGroup(name),
		cfg:      l.cfg,
		closer:   nil,
		levelVar: l.levelVar,
	}
}

// Config returns the logger's configuration.
// This is useful for inspecting the current configuration.
func (l *Logger) Config() config.LoggingConfig {
	l.cfgMu.RLock()
	defer l.cfgMu.RUnlock()
	return l.cfg
}

// SetLevel changes the logger's level at runtime.
// This is safe to call concurrently — it updates the underlying LevelVar
// without recreating the handler, preserving the original output destination.
func (l *Logger) SetLevel(level string) {
	l.cfgMu.Lock()
	l.cfg.Level = level
	l.cfgMu.Unlock()
	l.levelVar.Set(parseLevel(level))
}

// FormatRequestInfo extracts common request information for logging.
// This is a helper function that can be used outside the logger.
func FormatRequestInfo(req *icap.Request) map[string]interface{} {
	if req == nil {
		return nil
	}

	info := map[string]interface{}{
		"method":    req.Method,
		"uri":       req.URI,
		"client_ip": req.ClientIP,
	}

	if reqID, ok := req.GetHeader("X-Request-ID"); ok {
		info["request_id"] = reqID
	}

	if req.HTTPRequest != nil {
		info["http_method"] = req.HTTPRequest.Method
		info["http_uri"] = req.HTTPRequest.URI
	}

	return info
}

// FormatResponseInfo extracts common response information for logging.
// This is a helper function that can be used outside the logger.
func FormatResponseInfo(resp *icap.Response) map[string]interface{} {
	if resp == nil {
		return nil
	}

	info := map[string]interface{}{
		"status_code": resp.StatusCode,
		"status_text": icap.StatusText(resp.StatusCode),
	}

	if resp.HTTPRequest != nil {
		info["has_http_request"] = true
	}

	if resp.HTTPResponse != nil {
		info["has_http_response"] = true
	}

	return info
}

// MustNew creates a new Logger and panics if an error occurs.
// This is useful for initialization code where failure should not continue.
func MustNew(cfg config.LoggingConfig) *Logger {
	logger, err := New(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create logger: %v", err))
	}
	return logger
}
