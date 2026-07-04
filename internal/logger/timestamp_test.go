// Copyright 2026 ICAP Mock

package logger

import (
	"bytes"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/icap-mock/icap-mock/internal/config"
)

func TestJSONTimestampUsesMilliseconds(t *testing.T) {
	var buf bytes.Buffer
	log, err := NewWithWriter(config.LoggingConfig{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() error = %v", err)
	}

	log.Info("timestamp precision")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	timestamp, ok := entry["time"].(string)
	if !ok {
		t.Fatalf("time field = %v, want string", entry["time"])
	}
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}(Z|[+-]\d{2}:\d{2})$`)
	if !pattern.MatchString(timestamp) {
		t.Fatalf("time field = %q, want millisecond precision", timestamp)
	}
}
