// Copyright 2026 ICAP Mock

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// StreamMultipartConfig selects multipart form-data content from HTTP bodies.
type StreamMultipartConfig struct {
	Fields     []string                   `yaml:"fields,omitempty" json:"fields,omitempty"`
	Files      StreamMultipartFilesConfig `yaml:"files,omitempty" json:"files,omitempty"`
	AllowEmpty bool                       `yaml:"allow_empty,omitempty" json:"allow_empty,omitempty"`
	IsSet      bool                       `yaml:"-" json:"-"`
}

// StreamMultipartFilesConfig selects file parts, optionally by filename regex.
type StreamMultipartFilesConfig struct {
	Filename []string `yaml:"filename,omitempty" json:"filename,omitempty"`
	Enabled  bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	IsSet    bool     `yaml:"-" json:"-"`
}

// StreamFallbackConfig resolves bytes when multipart selection cannot produce data.
type StreamFallbackConfig struct {
	Body     string                `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile string                `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	From     string                `yaml:"from,omitempty" json:"from,omitempty"`
	RawFile  StreamRawFileFallback `yaml:"raw_file,omitempty" json:"raw_file,omitempty"`
}

// StreamRawFileFallback returns the whole source body, optionally filename-gated.
type StreamRawFileFallback struct {
	Filename []string `yaml:"filename,omitempty" json:"filename,omitempty"`
	Enabled  bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	IsSet    bool     `yaml:"-" json:"-"`
}

func (s *StreamConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawStreamConfig StreamConfig
	var raw rawStreamConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*s = StreamConfig(raw)
	s.PartsSet = yamlMappingHasKey(node, "parts")
	return nil
}

func (s *StreamConfig) UnmarshalJSON(data []byte) error {
	type rawStreamConfig StreamConfig
	var raw rawStreamConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = StreamConfig(raw)
	keys, err := jsonObjectKeys(data)
	if err != nil {
		return err
	}
	_, s.PartsSet = keys["parts"]
	return nil
}

func (m *StreamMultipartConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawMultipart StreamMultipartConfig
	var raw rawMultipart
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*m = StreamMultipartConfig(raw)
	m.IsSet = true
	return nil
}

func (m *StreamMultipartConfig) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*m = StreamMultipartConfig{}
		return nil
	}
	type rawMultipart StreamMultipartConfig
	var raw rawMultipart
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = StreamMultipartConfig(raw)
	m.IsSet = true
	return nil
}

func (f *StreamMultipartFilesConfig) UnmarshalYAML(node *yaml.Node) error {
	f.IsSet = true
	if node.Kind == yaml.ScalarNode {
		return decodeBoolFileSelector(node.Value, &f.Enabled)
	}
	f.Enabled = true
	return decodeFilenameSelector(node, &f.Filename)
}

func (f *StreamMultipartFilesConfig) UnmarshalJSON(data []byte) error {
	return decodeJSONFileSelector(data, &f.Enabled, &f.Filename, &f.IsSet)
}

func (r *StreamRawFileFallback) UnmarshalYAML(node *yaml.Node) error {
	r.IsSet = true
	if node.Kind == yaml.ScalarNode {
		return decodeBoolFileSelector(node.Value, &r.Enabled)
	}
	r.Enabled = true
	return decodeFilenameSelector(node, &r.Filename)
}

func (r *StreamRawFileFallback) UnmarshalJSON(data []byte) error {
	return decodeJSONFileSelector(data, &r.Enabled, &r.Filename, &r.IsSet)
}

func decodeJSONFileSelector(data []byte, enabled *bool, filename *[]string, isSet *bool) error {
	*isSet = !isJSONNull(data)
	if !*isSet {
		return nil
	}
	var scalar bool
	if err := json.Unmarshal(data, &scalar); err == nil {
		*enabled = scalar
		return nil
	}
	return decodeJSONFileSelectorObject(data, enabled, filename)
}

func decodeJSONFileSelectorObject(data []byte, enabled *bool, filename *[]string) error {
	var raw struct {
		Enabled  *bool           `json:"enabled"`
		Filename json.RawMessage `json:"filename"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*enabled = true
	if raw.Enabled != nil {
		*enabled = *raw.Enabled
	}
	return decodeJSONFilenameList(raw.Filename, filename)
}

func decodeJSONFilenameList(data json.RawMessage, target *[]string) error {
	if len(data) == 0 || isJSONNull(data) {
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*target = []string{one}
		return nil
	}
	return json.Unmarshal(data, target)
}

func jsonObjectKeys(data []byte) (map[string]json.RawMessage, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func isJSONNull(data []byte) bool { return bytes.Equal(bytes.TrimSpace(data), []byte("null")) }

func decodeBoolFileSelector(raw string, target *bool) error {
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("selector flag must be a boolean")
	}
	*target = v
	return nil
}

func decodeFilenameSelector(node *yaml.Node, target *[]string) error {
	var raw struct {
		Filename regexStringList `yaml:"filename"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*target = []string(raw.Filename)
	return nil
}

type regexStringList []string

func (r *regexStringList) UnmarshalYAML(node *yaml.Node) error {
	var values stringList
	if err := values.UnmarshalYAML(node); err != nil {
		return err
	}
	*r = regexStringList(values)
	return nil
}

func normalizeStreamConfig(s *StreamConfig) error {
	if s.PartsSet && len(s.Parts) == 0 {
		return fmt.Errorf("stream.parts must not be empty")
	}
	if hasTopLevelStreamSource(s) && !streamSourceEmpty(s.Source) {
		return fmt.Errorf("stream top-level source and source block are mutually exclusive")
	}
	if s.PartsSet && (!streamSourceEmpty(s.Source) || hasTopLevelStreamSource(s)) {
		return fmt.Errorf("stream.from and stream.parts are mutually exclusive")
	}
	return normalizeStreamSourceShape(s)
}

func normalizeStreamSourceShape(s *StreamConfig) error {
	if len(s.Parts) > 0 {
		return normalizeStreamParts(s.Parts)
	}
	if streamSourceEmpty(s.Source) {
		s.Source = StreamSourceConfig{From: s.From, Body: s.Body, BodyFile: s.BodyFile}
	}
	return normalizeSingleStreamSource(&s.Source)
}

func normalizeStreamParts(parts []StreamPartConfig) error {
	if len(parts) == 0 {
		return fmt.Errorf("stream.parts must not be empty")
	}
	for i := range parts {
		if err := normalizeStreamPart(&parts[i]); err != nil {
			return fmt.Errorf("parts[%d]: %w", i, err)
		}
	}
	return nil
}

func normalizeSingleStreamSource(src *StreamSourceConfig) error {
	if src.From == "" && src.Body != "" {
		src.From = streamSourceBody
	}
	if src.From == "" && src.BodyFile != "" {
		src.From = streamSourceBodyFile
	}
	return nil
}

func normalizeStreamPart(part *StreamPartConfig) error {
	if countPartSources(*part) != 1 {
		return fmt.Errorf("each part must specify exactly one of from, body, or body_file")
	}
	if part.Body != "" {
		part.From = streamSourceBody
	}
	if part.BodyFile != "" {
		part.From = streamSourceBodyFile
	}
	return nil
}

func validateStreamPart(part StreamPartConfig, methods MethodList) error {
	src := StreamSourceConfig(part)
	return validateStreamSourceWithMethods(src, methods)
}

func validateFallbackShape(f StreamFallbackConfig) error {
	if countFallbackSources(f) != 1 {
		return fmt.Errorf("fallback must specify exactly one of raw_file, body, body_file, or from")
	}
	if f.From != "" && !streamSourceCanFallbackFrom(f.From) {
		return fmt.Errorf("unsupported fallback.from %q", f.From)
	}
	return nil
}

func streamSourceSupportsMultipart(source string) bool {
	return source == streamSourceRequestHTTPBody || source == streamSourceResponseHTTPBody
}

func streamSourceCanFallbackFrom(source string) bool {
	return source == streamSourceRequestBody || source == streamSourceResponseBody ||
		source == streamSourceRequestHTTPBody || source == streamSourceResponseHTTPBody
}

func (f StreamFallbackConfig) IsSet() bool {
	return f.RawFile.IsSet || f.Body != "" || f.BodyFile != "" || f.From != ""
}

func hasTopLevelStreamSource(s *StreamConfig) bool {
	return s.From != "" || s.Body != "" || s.BodyFile != ""
}

func streamSourceEmpty(src StreamSourceConfig) bool {
	return src.From == "" && src.Body == "" && src.BodyFile == ""
}

func countPartSources(part StreamPartConfig) int {
	return boolCount(part.From != "", part.Body != "", part.BodyFile != "")
}

func countFallbackSources(f StreamFallbackConfig) int {
	return boolCount(f.RawFile.IsSet, f.Body != "", f.BodyFile != "", f.From != "")
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func yamlMappingHasKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// StreamHasBodyFile reports whether a stream references scenario-local files.
func StreamHasBodyFile(stream *StreamConfig) bool {
	return stream != nil && streamHasBodyFileConfig(stream)
}

func streamHasBodyFileConfig(stream *StreamConfig) bool {
	if stream.Source.BodyFile != "" || stream.BodyFile != "" || stream.Fallback.BodyFile != "" {
		return true
	}
	for _, part := range stream.Parts {
		if part.BodyFile != "" {
			return true
		}
	}
	return false
}
