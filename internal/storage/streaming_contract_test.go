// Copyright 2026 ICAP Mock

package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestStreamConfig_RejectsRemovedKeys(t *testing.T) {
	tests := []struct {
		name    string
		decode  func(*StreamConfig) error
		wantErr string
	}{
		{
			name: "yaml start delay",
			decode: func(config *StreamConfig) error {
				return yaml.Unmarshal([]byte("start_delay: 1s\n"), config)
			},
			wantErr: "stream.start_delay is not supported; use response delay or stream.send.duration",
		},
		{
			name: "json start delay",
			decode: func(config *StreamConfig) error {
				return json.Unmarshal([]byte(`{"start_delay":"1s"}`), config)
			},
			wantErr: "stream.start_delay is not supported; use response delay or stream.send.duration",
		},
		{
			name: "yaml chunk size",
			decode: func(config *StreamConfig) error {
				return yaml.Unmarshal([]byte("throttle:\n  chunk_size: 16KB\n"), config)
			},
			wantErr: "throttle.chunk_size is not supported; use throttle.target_chunk_size",
		},
		{
			name: "json chunk size",
			decode: func(config *StreamConfig) error {
				return json.Unmarshal([]byte(`{"throttle":{"chunk_size":"16KB"}}`), config)
			},
			wantErr: "throttle.chunk_size is not supported; use throttle.target_chunk_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config StreamConfig
			err := tt.decode(&config)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("decode error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestStreamConfig_RejectsRemovedKeysFromYAMLMerge(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "merged start delay",
			raw:     "legacy: &legacy\n  start_delay: 1s\n<<: *legacy\n",
			wantErr: "stream.start_delay is not supported",
		},
		{
			name:    "merged throttle chunk size",
			raw:     "throttle:\n  <<: &legacy\n    chunk_size: 16KB\n",
			wantErr: "throttle.chunk_size is not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config StreamConfig
			err := yaml.Unmarshal([]byte(tt.raw), &config)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("yaml.Unmarshal() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestStreamConfig_AcceptsDurationWithEvery(t *testing.T) {
	config := decodeYAMLStreamConfig(t, `
source:
  body: payload
send:
  duration: 10s-20s
throttle:
  target_chunk_size: 16KB
  every: 100ms
end:
  mode: complete
`)

	if err := validateStreamConfig(&config, MethodList{"REQMOD"}); err != nil {
		t.Fatalf("validateStreamConfig() error = %v", err)
	}
	if config.Send.Duration.Min != 10*time.Second || config.Send.Duration.Max != 20*time.Second {
		t.Fatalf("send.duration = %v-%v, want 10s-20s", config.Send.Duration.Min, config.Send.Duration.Max)
	}
	if config.Throttle.Every.Min != 100*time.Millisecond || config.Throttle.Every.Max != 100*time.Millisecond {
		t.Fatalf("throttle.every = %v-%v, want 100ms", config.Throttle.Every.Min, config.Throttle.Every.Max)
	}
	if config.Duration.IsSet || config.Chunks.Size.IsSet || config.Chunks.Delay.IsSet || config.Finish.Mode != "" {
		t.Fatalf("new controls changed legacy controls: duration=%+v chunks=%+v finish=%+v", config.Duration, config.Chunks, config.Finish)
	}
}

func TestStreamConfig_ValidatesTargetChunks(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "positive", raw: "target_chunks: 100"},
		{name: "zero", raw: "target_chunks: 0", wantErr: "throttle.target_chunks must be positive"},
		{name: "negative", raw: "target_chunks: -1", wantErr: "throttle.target_chunks must be positive"},
		{
			name:    "mutually exclusive",
			raw:     "target_chunk_size: 16KB\n  target_chunks: 100",
			wantErr: "throttle.target_chunk_size and throttle.target_chunks are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := decodeYAMLStreamConfig(t, "source:\n  body: payload\nthrottle:\n  "+tt.raw+"\n")
			err := validateStreamConfig(&config, MethodList{"REQMOD"})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateStreamConfig() error = %v", err)
				}
				if config.Throttle.TargetChunks != 100 {
					t.Fatalf("TargetChunks = %d, want 100", config.Throttle.TargetChunks)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateStreamConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestStreamConfig_JSONScalarSpecs(t *testing.T) {
	var config StreamConfig
	err := json.Unmarshal([]byte(`{
		"source":{"body":"payload"},
		"send":{"duration":"10s-20s"},
		"throttle":{"target_chunk_size":"8KB-16KB","every":"75ms-125ms"}
	}`), &config)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if config.Send.Duration.Min != 10*time.Second || config.Send.Duration.Max != 20*time.Second {
		t.Fatalf("send.duration = %+v, want 10s-20s", config.Send.Duration)
	}
	if config.Throttle.TargetChunkSize.Min != 8*1024 || config.Throttle.TargetChunkSize.Max != 16*1024 {
		t.Fatalf("target_chunk_size = %+v, want 8KB-16KB", config.Throttle.TargetChunkSize)
	}
	if config.Throttle.Every.Min != 75*time.Millisecond || config.Throttle.Every.Max != 125*time.Millisecond {
		t.Fatalf("throttle.every = %+v, want 75ms-125ms", config.Throttle.Every)
	}
	if err := validateStreamConfig(&config, MethodList{"REQMOD"}); err != nil {
		t.Fatalf("validateStreamConfig() error = %v", err)
	}

	var zeroTargetChunks StreamConfig
	if err := json.Unmarshal([]byte(`{"source":{"body":"payload"},"throttle":{"target_chunks":0}}`), &zeroTargetChunks); err != nil {
		t.Fatalf("json.Unmarshal() zero target_chunks error = %v", err)
	}
	if err := validateStreamConfig(&zeroTargetChunks, MethodList{"REQMOD"}); err == nil ||
		!strings.Contains(err.Error(), "throttle.target_chunks must be positive") {
		t.Fatalf("validateStreamConfig() error = %v, want positive target_chunks error", err)
	}
}

func TestStreamConfig_LegacyControlsRemainIndependent(t *testing.T) {
	config := decodeYAMLStreamConfig(t, `
source:
  body: payload
chunks:
  size: 2
  delay: 5ms
finish:
  mode: complete
`)

	if err := validateStreamConfig(&config, MethodList{"REQMOD"}); err != nil {
		t.Fatalf("validateStreamConfig() error = %v", err)
	}
	if config.Chunks.Size.Min != 2 || config.Chunks.Delay.Min != 5*time.Millisecond {
		t.Fatalf("legacy chunks changed: %+v", config.Chunks)
	}
	if config.Send.IsSet || config.Throttle.IsSet || config.End.IsSet {
		t.Fatalf("legacy controls populated new controls: send=%+v throttle=%+v end=%+v", config.Send, config.Throttle, config.End)
	}
}

func decodeYAMLStreamConfig(t *testing.T, raw string) StreamConfig {
	t.Helper()
	var config StreamConfig
	if err := yaml.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return config
}
