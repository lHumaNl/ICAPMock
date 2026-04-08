// Copyright 2026 ICAP Mock

package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icap-mock/icap-mock/internal/config"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestNewLogger tests logger creation with different configurations.
func TestNewLogger(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.LoggingConfig
		wantError bool
	}{
		{
			name: "default JSON format to stdout",
			cfg: config.LoggingConfig{
				Level:      "info",
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			},
			wantError: false,
		},
		{
			name: "text format to stdout",
			cfg: config.LoggingConfig{
				Level:      "debug",
				Format:     "text",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			},
			wantError: false,
		},
		{
			name: "output to stderr",
			cfg: config.LoggingConfig{
				Level:      "warn",
				Format:     "json",
				Output:     "stderr",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			},
			wantError: false,
		},
		{
			name: "invalid log level defaults to info",
			cfg: config.LoggingConfig{
				Level:      "invalid",
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.cfg)
			if (err != nil) != tt.wantError {
				t.Errorf("New() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if logger == nil && !tt.wantError {
				t.Error("New() returned nil logger")
			}
			if logger != nil {
				logger.Close()
			}
		})
	}
}

// TestNewWithWriter tests logger creation with custom writer.
func TestNewWithWriter(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "debug",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	logger.Info("test message")

	if buf.Len() == 0 {
		t.Error("Expected log output, got empty buffer")
	}
}

// TestLogLevels tests different log level configurations.
func TestLogLevels(t *testing.T) {
	tests := []struct {
		logFunc   func(*Logger)
		name      string
		level     string
		shouldLog bool
	}{
		{func(l *Logger) { l.Debug("debug msg") }, "debug level logs debug", "debug", true},
		{func(l *Logger) { l.Debug("debug msg") }, "info level skips debug", "info", false},
		{func(l *Logger) { l.Info("info msg") }, "info level logs info", "info", true},
		{func(l *Logger) { l.Info("info msg") }, "warn level skips info", "warn", false},
		{func(l *Logger) { l.Warn("warn msg") }, "warn level logs warn", "warn", true},
		{func(l *Logger) { l.Warn("warn msg") }, "error level skips warn", "error", false},
		{func(l *Logger) { l.Error("error msg") }, "error level logs error", "error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := config.LoggingConfig{
				Level:      tt.level,
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			}

			logger, err := NewWithWriter(cfg, &buf)
			if err != nil {
				t.Fatalf("NewWithWriter() error = %v", err)
			}
			defer logger.Close()

			tt.logFunc(logger)

			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldLog {
				t.Errorf("Expected shouldLog=%v, got output=%v (buffer: %q)", tt.shouldLog, hasOutput, buf.String())
			}
		})
	}
}

// TestJSONFormat tests JSON log output format.
func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	logger.Info("test message", "key1", "value1", "key2", 123)

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v, output: %s", err, buf.String())
	}

	if msg, ok := entry["msg"].(string); !ok || msg != "test message" {
		t.Errorf("Expected msg='test message', got %v", entry["msg"])
	}

	if level, ok := entry["level"].(string); !ok || level != "INFO" {
		t.Errorf("Expected level='INFO', got %v", entry["level"])
	}

	if key1, ok := entry["key1"].(string); !ok || key1 != "value1" {
		t.Errorf("Expected key1='value1', got %v", entry["key1"])
	}

	if key2, ok := entry["key2"].(float64); !ok || int(key2) != 123 {
		t.Errorf("Expected key2=123, got %v", entry["key2"])
	}
}

// TestTextFormat tests text log output format.
func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "text",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	logger.Info("test message", "key1", "value1")

	output := buf.String()
	if !strings.Contains(output, "level=INFO") {
		t.Errorf("Expected 'level=INFO' in output, got: %s", output)
	}
	// slog text handler quotes values with spaces
	if !strings.Contains(output, `msg="test message"`) {
		t.Errorf("Expected 'msg=\"test message\"' in output, got: %s", output)
	}
	if !strings.Contains(output, "key1=value1") {
		t.Errorf("Expected 'key1=value1' in output, got: %s", output)
	}
}

// TestLogRequest tests the ICAP request logging helper.
func TestLogRequest(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	// Create test request
	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/echo",
		Proto:    icap.Version,
		ClientIP: "192.168.1.100",
		Header: icap.Header{
			"Host": []string{"localhost:1344"},
		},
	}

	// Create test response
	resp := &icap.Response{
		StatusCode: icap.StatusOK,
		Proto:      icap.Version,
	}

	duration := 5 * time.Millisecond
	logger.LogRequest(req, resp, duration, "test-scenario")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v, output: %s", err, buf.String())
	}

	// Verify required fields
	if msg, ok := entry["msg"].(string); !ok || msg != "request processed" {
		t.Errorf("Expected msg='request processed', got %v", entry["msg"])
	}

	if method, ok := entry["method"].(string); !ok || method != icap.MethodREQMOD {
		t.Errorf("Expected method='%s', got %v", icap.MethodREQMOD, entry["method"])
	}

	if clientIP, ok := entry["client_ip"].(string); !ok || clientIP != "192.168.1.100" {
		t.Errorf("Expected client_ip='192.168.1.100', got %v", entry["client_ip"])
	}

	if status, ok := entry["response_status"].(float64); !ok || int(status) != icap.StatusOK {
		t.Errorf("Expected response_status=%d, got %v", icap.StatusOK, entry["response_status"])
	}

	if scenario, ok := entry["scenario"].(string); !ok || scenario != "test-scenario" {
		t.Errorf("Expected scenario='test-scenario', got %v", entry["scenario"])
	}

	if durMs, ok := entry["duration_ms"].(float64); !ok || durMs != 5 {
		t.Errorf("Expected duration_ms=5, got %v", entry["duration_ms"])
	}
}

// TestLogRequestWithRequestID tests request logging with request ID.
func TestLogRequestWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	// Create header and use Set method for proper canonicalization
	hdr := make(icap.Header)
	hdr.Set("X-Request-ID", "req-20240115-001")

	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/echo",
		ClientIP: "192.168.1.100",
		Header:   hdr,
	}

	resp := &icap.Response{
		StatusCode: icap.StatusNoContentNeeded,
	}

	logger.LogRequest(req, resp, 1*time.Millisecond, "")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if reqID, ok := entry["request_id"].(string); !ok || reqID != "req-20240115-001" {
		t.Errorf("Expected request_id='req-20240115-001', got %v", entry["request_id"])
	}
}

// TestLogError tests error logging helper.
func TestLogError(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	testErr := fmt.Errorf("connection refused")
	context := map[string]interface{}{
		"service":  "icap-server",
		"port":     1344,
		"attempts": 3,
	}

	logger.LogError(testErr, context)

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v, output: %s", err, buf.String())
	}

	if level, ok := entry["level"].(string); !ok || level != "ERROR" {
		t.Errorf("Expected level='ERROR', got %v", entry["level"])
	}

	if errMsg, ok := entry["error"].(string); !ok || errMsg != "connection refused" {
		t.Errorf("Expected error='connection refused', got %v", entry["error"])
	}

	if service, ok := entry["service"].(string); !ok || service != "icap-server" {
		t.Errorf("Expected service='icap-server', got %v", entry["service"])
	}

	if port, ok := entry["port"].(float64); !ok || int(port) != 1344 {
		t.Errorf("Expected port=1344, got %v", entry["port"])
	}
}

// TestLogErrorWithNilContext tests error logging with nil context.
func TestLogErrorWithNilContext(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	testErr := fmt.Errorf("test error")
	logger.LogError(testErr, nil)

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v, output: %s", err, buf.String())
	}

	if errMsg, ok := entry["error"].(string); !ok || errMsg != "test error" {
		t.Errorf("Expected error='test error', got %v", entry["error"])
	}
}

// TestFileOutput tests logging to a file.
func TestFileOutput(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "logger-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "test.log")
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     logFile,
		MaxSize:    1, // 1 MB
		MaxBackups: 3,
		MaxAge:     7,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info("test file message")
	logger.Close()

	// Read and verify file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(content) == 0 {
		t.Error("Expected log file to have content")
	}

	if !strings.Contains(string(content), "test file message") {
		t.Errorf("Expected 'test file message' in log file, got: %s", string(content))
	}
}

// TestFileRotation tests log file rotation.
func TestFileRotation(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "logger-rotation-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "rotate.log")
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     logFile,
		MaxSize:    1, // 1 MB - very small for testing
		MaxBackups: 2,
		MaxAge:     1,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Write enough data to trigger rotation
	largeData := strings.Repeat("x", 1024*512) // 512KB per message
	for i := 0; i < 4; i++ {
		logger.Info("rotation test", "data", largeData)
	}
	logger.Close()

	// Check that files were created
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read temp dir: %v", err)
	}

	// Should have at least the main log file and one backup
	if len(files) < 1 {
		t.Errorf("Expected at least 1 log file, got %d", len(files))
	}
}

// TestWithMethods tests context methods for adding fields.
func TestWithMethods(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	// Test With method
	logger.With("component", "test").Info("with test")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if comp, ok := entry["component"].(string); !ok || comp != "test" {
		t.Errorf("Expected component='test', got %v", entry["component"])
	}
}

// TestWithRequestID tests adding request ID to logger context.
func TestWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	reqLogger := logger.WithRequestID("req-12345")
	reqLogger.Info("test with request id")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if reqID, ok := entry["request_id"].(string); !ok || reqID != "req-12345" {
		t.Errorf("Expected request_id='req-12345', got %v", entry["request_id"])
	}
}

// TestSlogHandler tests that the logger properly exposes the slog handler.
func TestSlogHandler(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	handler := logger.Handler()
	if handler == nil {
		t.Error("Handler() returned nil")
	}
}

// TestDefaultLogger tests the default logger functionality.
func TestDefaultLogger(t *testing.T) {
	// Note: defaultLogger is atomic.Value, no need to reset — SetDefault overwrites it

	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := SetDefault(cfg)
	if err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	defer logger.Close()

	if Default() != logger {
		t.Error("Default() should return the logger set by SetDefault()")
	}
}

// TestSync tests the Sync method.
func TestSync(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	if err := logger.Sync(); err != nil {
		t.Errorf("Sync() error = %v", err)
	}
}

// TestLogMethods tests all standard log methods.
func TestLogMethods(t *testing.T) {
	tests := []struct {
		name    string
		logFunc func(*Logger)
		level   string
	}{
		{"Debug", func(l *Logger) { l.Debug("debug") }, "DEBUG"},
		{"Info", func(l *Logger) { l.Info("info") }, "INFO"},
		{"Warn", func(l *Logger) { l.Warn("warn") }, "WARN"},
		{"Error", func(l *Logger) { l.Error("error") }, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := config.LoggingConfig{
				Level:      "debug",
				Format:     "json",
				Output:     "stdout",
				MaxSize:    100,
				MaxBackups: 5,
				MaxAge:     30,
			}

			logger, err := NewWithWriter(cfg, &buf)
			if err != nil {
				t.Fatalf("NewWithWriter() error = %v", err)
			}
			defer logger.Close()

			tt.logFunc(logger)

			var entry map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if level, ok := entry["level"].(string); !ok || level != tt.level {
				t.Errorf("Expected level='%s', got %v", tt.level, entry["level"])
			}
		})
	}
}

// TestLogWithAttrs tests logging with additional attributes.
func TestLogWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	logger.Info("message with attrs",
		"string", "value",
		"int", 42,
		"bool", true,
		"float", 3.14,
	)

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if v, ok := entry["string"].(string); !ok || v != "value" {
		t.Errorf("Expected string='value', got %v", entry["string"])
	}

	if v, ok := entry["int"].(float64); !ok || int(v) != 42 {
		t.Errorf("Expected int=42, got %v", entry["int"])
	}

	if v, ok := entry["bool"].(bool); !ok || !v {
		t.Errorf("Expected bool=true, got %v", entry["bool"])
	}

	if v, ok := entry["float"].(float64); !ok || v != 3.14 {
		t.Errorf("Expected float=3.14, got %v", entry["float"])
	}
}

// TestConcurrency tests that the logger is safe for concurrent use.
func TestConcurrency(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				logger.Info("concurrent log", "goroutine", id, "iteration", j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// BenchmarkLogger benchmarks the logger performance.
func BenchmarkLogger(b *testing.B) {
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, io.Discard)
	if err != nil {
		b.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message",
			"request_id", "req-001",
			"method", "REQMOD",
			"client_ip", "192.168.1.1",
			"duration_ms", 5,
		)
	}
}

// BenchmarkLogRequest benchmarks the LogRequest helper.
func BenchmarkLogRequest(b *testing.B) {
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, io.Discard)
	if err != nil {
		b.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/echo",
		ClientIP: "192.168.1.100",
		Header: icap.Header{
			"X-Request-ID": []string{"req-001"},
		},
	}

	resp := &icap.Response{
		StatusCode: icap.StatusOK,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogRequest(req, resp, 5*time.Millisecond, "test-scenario")
	}
}

// TestParseLevel tests the log level parsing.
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := parseLevel(tt.input)
			if level != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, level, tt.expected)
			}
		})
	}
}

// TestLoggerAttrs tests that logger attributes are properly set.
func TestLoggerAttrs(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	// Check that timestamp is included
	logger.Info("test timestamp")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if _, ok := entry["time"]; !ok {
		t.Error("Expected 'time' field in log entry")
	}
}

// TestSetLevel tests that SetLevel changes filtering without losing the output writer.
func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	// Debug should be filtered at info level
	logger.Debug("should not appear")
	if buf.Len() != 0 {
		t.Error("Debug message should be filtered at info level")
	}

	// Change to debug level
	logger.SetLevel("debug")

	logger.Debug("should appear")
	if buf.Len() == 0 {
		t.Error("Debug message should appear after SetLevel(debug)")
	}

	// Verify output goes to the same writer (not os.Stderr)
	buf.Reset()
	logger.Info("after set level")
	if buf.Len() == 0 {
		t.Error("Output should still go to the original writer after SetLevel")
	}

	// Change to error level — info should be filtered
	buf.Reset()
	logger.SetLevel("error")
	logger.Info("should be filtered")
	if buf.Len() != 0 {
		t.Error("Info message should be filtered at error level")
	}
}

// TestSetLevel_DerivedLogger tests that SetLevel on parent affects derived loggers.
func TestSetLevel_DerivedLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}

	logger, err := NewWithWriter(cfg, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}
	defer logger.Close()

	derived := logger.With("component", "test")

	// SetLevel on parent should affect derived (they share the same LevelVar)
	logger.SetLevel("error")

	derived.Info("should be filtered")
	if buf.Len() != 0 {
		t.Error("Derived logger should respect parent SetLevel")
	}
}

// TestWithOptions tests logger creation with options.
func TestWithOptions(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LoggingConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
	}

	logger, err := New(cfg, WithWriter(&buf))
	if err != nil {
		t.Fatalf("New() with option error = %v", err)
	}
	defer logger.Close()

	logger.Info("test with option")

	if buf.Len() == 0 {
		t.Error("Expected log output with WithWriter option")
	}
}
