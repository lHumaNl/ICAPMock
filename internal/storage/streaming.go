// Copyright 2026 ICAP Mock

package storage

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

const (
	streamSourceRequestBody      = "request_body"
	streamSourceResponseBody     = "response_body"
	streamSourceRequestHTTPBody  = "request_http_body"
	streamSourceResponseHTTPBody = "response_http_body"
	streamSourceAdaptedHTTPBody  = "adapted_http_body"
	streamSourceBody             = "body"
	streamSourceBodyFile         = "body_file"
	streamFinishComplete         = "complete"
	streamFinishFIN              = "fin"
	streamFinishTerm             = "term"
	streamFinishWeighted         = "weighted"
	defaultStreamChunkSize       = 1
)

// StreamConfig defines gradual chunked encapsulated body streaming.
//
//nolint:govet // field order follows the YAML schema groups for readability.
type StreamConfig struct {
	Fallback        StreamFallbackConfig  `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	Source          StreamSourceConfig    `yaml:"source,omitempty" json:"source,omitempty"`
	Parts           []StreamPartConfig    `yaml:"parts,omitempty" json:"parts,omitempty"`
	From            string                `yaml:"from,omitempty" json:"from,omitempty"`
	Body            string                `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile        string                `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	Multipart       StreamMultipartConfig `yaml:"multipart,omitempty" json:"multipart,omitempty"`
	Send            StreamSendConfig      `yaml:"send,omitempty" json:"send,omitempty"`
	Throttle        StreamThrottleConfig  `yaml:"throttle,omitempty" json:"throttle,omitempty"`
	End             StreamEndConfig       `yaml:"end,omitempty" json:"end,omitempty"`
	Finish          StreamFinishConfig    `yaml:"finish,omitempty" json:"finish,omitempty"`
	Chunks          StreamChunksConfig    `yaml:"chunks,omitempty" json:"chunks,omitempty"`
	StartDelay      DurationSpec          `yaml:"start_delay,omitempty" json:"start_delay,omitempty"`
	Duration        DurationSpec          `yaml:"duration,omitempty" json:"duration,omitempty"`
	PartsSet        bool                  `yaml:"-" json:"-"`
	derivedControls bool                  `yaml:"-" json:"-"` // legacy fields were populated from send/throttle/end.
}

// StreamSendConfig controls how much data to send and over what duration.
type StreamSendConfig struct {
	Percent  PercentSpec  `yaml:"percent,omitempty" json:"percent,omitempty"`
	Duration DurationSpec `yaml:"duration,omitempty" json:"duration,omitempty"`
	IsSet    bool         `yaml:"-" json:"-"`
}

// StreamThrottleConfig controls chunk sizing and inter-chunk pacing.
type StreamThrottleConfig struct {
	ChunkSize SizeSpec     `yaml:"chunk_size,omitempty" json:"chunk_size,omitempty"`
	Every     DurationSpec `yaml:"every,omitempty" json:"every,omitempty"`
	IsSet     bool         `yaml:"-" json:"-"`
}

// StreamEndConfig controls whether the stream completes, closes with FIN, or terminates early.
type StreamEndConfig struct {
	Mode  string `yaml:"mode,omitempty" json:"mode,omitempty"`
	IsSet bool   `yaml:"-" json:"-"`
}

// StreamSourceConfig selects bytes to stream.
type StreamSourceConfig struct {
	From     string `yaml:"from,omitempty" json:"from,omitempty"`
	Body     string `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile string `yaml:"body_file,omitempty" json:"body_file,omitempty"`
}

// StreamPartConfig is one source in an ordered stream composition.
type StreamPartConfig struct {
	From     string `yaml:"from,omitempty" json:"from,omitempty"`
	Body     string `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile string `yaml:"body_file,omitempty" json:"body_file,omitempty"`
}

// StreamChunksConfig controls chunk size and inter-chunk delay.
type StreamChunksConfig struct {
	Size  SizeSpec     `yaml:"size,omitempty" json:"size,omitempty"`
	Delay DurationSpec `yaml:"delay,omitempty" json:"delay,omitempty"`
}

// StreamFinishConfig controls final chunk, clean FIN, and weighted termination.
type StreamFinishConfig struct {
	Mode            string          `yaml:"mode,omitempty" json:"mode,omitempty"`
	Fin             StreamFINConfig `yaml:"fin,omitempty" json:"fin,omitempty"`
	CompletePercent int             `yaml:"complete_percent,omitempty" json:"complete_percent,omitempty"`
	FinPercent      int             `yaml:"fin_percent,omitempty" json:"fin_percent,omitempty"`
}

// StreamFINConfig controls clean connection close behavior.
type StreamFINConfig struct {
	Close string               `yaml:"close,omitempty" json:"close,omitempty"`
	After StreamFINAfterConfig `yaml:"after,omitempty" json:"after,omitempty"`
}

// StreamFINAfterConfig defines first-wins partial FIN triggers.
type StreamFINAfterConfig struct {
	Bytes SizeSpec     `yaml:"bytes,omitempty" json:"bytes,omitempty"`
	Time  DurationSpec `yaml:"time,omitempty" json:"time,omitempty"`
}

// SizeSpec is a byte size or inclusive byte range.
type SizeSpec struct {
	Min   int64
	Max   int64
	IsSet bool
}

// DurationSpec is a duration or inclusive duration range.
type DurationSpec struct {
	Min   time.Duration
	Max   time.Duration
	IsSet bool
}

// PercentSpec is an inclusive percentage or percentage range.
type PercentSpec struct {
	Min   int
	Max   int
	IsSet bool
}

func (s *SizeSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("size must be a scalar")
	}
	minVal, maxVal, err := parseSizeSpec(node.Value)
	if err != nil {
		return err
	}
	*s = SizeSpec{Min: minVal, Max: maxVal, IsSet: true}
	return nil
}

func (d *DurationSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar")
	}
	dc, err := ParseDelay(node.Value)
	if err != nil {
		return err
	}
	*d = DurationSpec{Min: dc.Min, Max: dc.Max, IsSet: true}
	return nil
}

func validateResponseStreaming(where string, resp *ResponseTemplate, methods MethodList) error {
	if resp.Stream == nil {
		return nil
	}
	if err := validateStreamHTTPBodyConflict(resp.HTTPBody, resp.HTTPBodyFile); err != nil {
		return fmt.Errorf("%s stream: %w", where, err)
	}
	if err := validateStreamConfig(resp.Stream, methods); err != nil {
		return fmt.Errorf("%s stream: %w", where, err)
	}
	return nil
}

func validateWeightedStreaming(where string, weighted []WeightedResponse, methods MethodList) error {
	for i := range weighted {
		if weighted[i].Stream == nil {
			continue
		}
		if err := validateStreamHTTPBodyConflict(weighted[i].HTTPBody, weighted[i].HTTPBodyFile); err != nil {
			return fmt.Errorf("%s weighted response #%d stream: %w", where, i+1, err)
		}
		if err := validateStreamConfig(weighted[i].Stream, methods); err != nil {
			return fmt.Errorf("%s weighted response #%d stream: %w", where, i+1, err)
		}
	}
	return nil
}

func validateStreamConfig(s *StreamConfig, methods MethodList) error {
	if err := normalizeStreamConfig(s); err != nil {
		return err
	}
	if err := validateStreamSources(s, methods); err != nil {
		return err
	}
	if err := validateStreamMultipart(s); err != nil {
		return err
	}
	if err := validateStreamFallback(s.Fallback, methods); err != nil {
		return err
	}
	return validateStreamControls(s)
}

func validateStreamSources(s *StreamConfig, methods MethodList) error {
	if len(s.Parts) == 0 {
		return validateStreamSourceWithMethods(s.Source, methods)
	}
	for i := range s.Parts {
		if err := validateStreamPart(s.Parts[i], methods); err != nil {
			return fmt.Errorf("parts[%d]: %w", i, err)
		}
	}
	return nil
}

func validateStreamSourceWithMethods(src StreamSourceConfig, methods MethodList) error {
	if err := validateStreamSource(src); err != nil {
		return err
	}
	return validateStreamSourceMethods(src.From, methods)
}

func validateStreamSource(src StreamSourceConfig) error {
	switch src.From {
	case streamSourceRequestBody, streamSourceResponseBody,
		streamSourceRequestHTTPBody, streamSourceResponseHTTPBody, streamSourceAdaptedHTTPBody:
		if src.Body != "" || src.BodyFile != "" {
			return fmt.Errorf("source.%s cannot be combined with source.body or source.body_file", src.From)
		}
		return nil
	case streamSourceBody:
		if src.Body == "" {
			return fmt.Errorf("source.body is required when source.from is body")
		}
		if src.BodyFile != "" {
			return fmt.Errorf("source.body_file is not allowed when source.from is body")
		}
		return nil
	case streamSourceBodyFile:
		if src.Body != "" {
			return fmt.Errorf("source.body is not allowed when source.from is body_file")
		}
		if src.BodyFile == "" {
			return fmt.Errorf("source.body_file is required when source.from is body_file")
		}
		_, err := os.Stat(src.BodyFile) //nolint:gosec // scenario-controlled path
		return err
	default:
		return fmt.Errorf("unsupported source.from %q", src.From)
	}
}

func validateStreamSourceMethods(source string, methods MethodList) error {
	switch source {
	case streamSourceRequestBody, streamSourceRequestHTTPBody:
		return validateBodyStreamMethods(methods, icap.MethodREQMOD, source)
	case streamSourceResponseBody, streamSourceResponseHTTPBody:
		return validateBodyStreamMethods(methods, icap.MethodRESPMOD, source)
	case streamSourceAdaptedHTTPBody:
		return validateAdaptedHTTPBodyMethods(methods)
	}
	if hasImplicitAnyMethod(methods) {
		return nil
	}
	if !methodSetAllows(methods, icap.MethodREQMOD) && !methodSetAllows(methods, icap.MethodRESPMOD) {
		return fmt.Errorf("stream requires a REQMOD or RESPMOD scenario")
	}
	return nil
}

func validateAdaptedHTTPBodyMethods(methods MethodList) error {
	if hasImplicitAnyMethod(methods) {
		return fmt.Errorf("source.%s requires explicit REQMOD and/or RESPMOD scenario methods", streamSourceAdaptedHTTPBody)
	}
	for _, method := range methods {
		if !strings.EqualFold(method, icap.MethodREQMOD) && !strings.EqualFold(method, icap.MethodRESPMOD) {
			return fmt.Errorf("source.%s does not support %s scenario methods", streamSourceAdaptedHTTPBody, method)
		}
	}
	return nil
}

func validateBodyStreamMethods(methods MethodList, required, source string) error {
	if hasImplicitAnyMethod(methods) {
		return fmt.Errorf("source.%s requires an explicit %s scenario method", source, required)
	}
	for _, method := range methods {
		if !strings.EqualFold(method, required) {
			return fmt.Errorf("source.%s requires only %s scenario methods", source, required)
		}
	}
	return nil
}

func hasImplicitAnyMethod(methods MethodList) bool {
	return len(methods) == 0 || methodSetAllows(methods, "*")
}

func methodSetAllows(methods MethodList, method string) bool {
	for _, item := range methods {
		if strings.EqualFold(item, method) {
			return true
		}
	}
	return false
}

func validateStreamHTTPBodyConflict(httpBody, httpBodyFile string) error {
	if httpBody == "" && httpBodyFile == "" {
		return nil
	}
	return fmt.Errorf("stream cannot be combined with http_body or http_body_file")
}

func validateStreamMultipart(s *StreamConfig) error {
	if !s.Multipart.IsSet {
		return nil
	}
	if len(s.Parts) > 0 || !streamSourceSupportsMultipart(s.Source.From) {
		return fmt.Errorf("multipart is only allowed with request_http_body, response_http_body, or adapted_http_body")
	}
	return validateRegexList("multipart.files.filename", s.Multipart.Files.Filename)
}

func validateStreamFallback(f StreamFallbackConfig, methods MethodList) error {
	if !f.IsSet() {
		return nil
	}
	if err := validateFallbackShape(f); err != nil {
		return err
	}
	if err := validateRegexList("fallback.raw_file.filename", f.RawFile.Filename); err != nil {
		return err
	}
	if f.From != "" {
		return validateStreamSourceMethods(f.From, methods)
	}
	if f.BodyFile != "" {
		_, err := os.Stat(f.BodyFile) //nolint:gosec // scenario-controlled path
		return err
	}
	return nil
}

func validateRegexList(name string, patterns []string) error {
	for _, pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%s %q is invalid: %w", name, pattern, err)
		}
	}
	return nil
}

func validateStreamTiming(s *StreamConfig) error {
	if s.Chunks.Size.IsSet && s.Chunks.Size.Min <= 0 {
		return fmt.Errorf("chunks.size must be positive")
	}
	if !s.Chunks.Size.IsSet {
		s.Chunks.Size = SizeSpec{Min: defaultStreamChunkSize, Max: defaultStreamChunkSize, IsSet: true}
	}
	if s.Chunks.Delay.IsSet && s.Duration.IsSet {
		return fmt.Errorf("chunks.delay and duration are mutually exclusive")
	}
	if s.StartDelay.IsSet && s.StartDelay.Min <= 0 {
		return fmt.Errorf("start_delay must be positive")
	}
	return nil
}

func validateStreamFinish(s *StreamConfig) error {
	f := &s.Finish
	if f.Mode == "" {
		f.Mode = streamFinishComplete
	}
	if !validFinishMode(f.Mode) {
		return fmt.Errorf("finish.mode must be complete, fin, term, or weighted")
	}
	switch f.Mode {
	case streamFinishComplete:
		return validateCompleteFinish(f)
	case streamFinishFIN:
		return validateFINFinish(f)
	case streamFinishTerm:
		return validateTermFinish(s)
	case streamFinishWeighted:
		return validateWeightedFinish(f)
	}
	return nil
}

func validateTermFinish(s *StreamConfig) error {
	if !s.derivedControls {
		return fmt.Errorf("finish.mode term requires send/throttle/end controls")
	}
	return validatePartialEndControls(s, streamFinishTerm)
}

func validateCompleteFinish(f *StreamFinishConfig) error {
	if hasFinishFINConfig(f.Fin) {
		return fmt.Errorf("finish.fin is only allowed for fin or weighted finish modes")
	}
	if f.CompletePercent != 0 || f.FinPercent != 0 {
		return fmt.Errorf("finish percentages are only allowed for weighted finish mode")
	}
	return nil
}

func validateFINFinish(f *StreamFinishConfig) error {
	if f.CompletePercent != 0 || f.FinPercent != 0 {
		return fmt.Errorf("finish percentages are only allowed for weighted finish mode")
	}
	return validateFINConfig(f.Fin)
}

func validateWeightedFinish(f *StreamFinishConfig) error {
	if err := validatePercentages(f); err != nil {
		return err
	}
	if f.FinPercent > 0 && !hasFinishFINConfig(f.Fin) {
		return fmt.Errorf("weighted finish with fin_percent requires finish.fin configuration")
	}
	return validateFINConfig(f.Fin)
}

func validatePercentages(f *StreamFinishConfig) error {
	if !validPercent(f.CompletePercent) || !validPercent(f.FinPercent) {
		return fmt.Errorf("finish percentages must be between 0 and 100")
	}
	if f.Mode == streamFinishWeighted && f.CompletePercent+f.FinPercent != 100 {
		return fmt.Errorf("weighted finish percentages must sum to 100")
	}
	return nil
}

func validateFINConfig(fin StreamFINConfig) error {
	if fin.Close != "" && fin.Close != "clean" {
		return fmt.Errorf("finish.fin.close must be clean")
	}
	if fin.After.Bytes.IsSet && fin.After.Bytes.Min <= 0 {
		return fmt.Errorf("finish.fin.after.bytes must be positive")
	}
	return nil
}

func hasFinishFINConfig(fin StreamFINConfig) bool {
	return fin.Close != "" || fin.After.Bytes.IsSet || fin.After.Time.IsSet
}

func validFinishMode(mode string) bool {
	return mode == streamFinishComplete || mode == streamFinishFIN ||
		mode == streamFinishTerm || mode == streamFinishWeighted
}

func validPercent(v int) bool { return v >= 0 && v <= 100 }

func parseSizeSpec(raw string) (minVal, maxVal int64, err error) {
	parts := strings.Split(raw, "-")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid size range %q", raw)
	}
	minVal, err = parseByteSize(parts[0])
	if err != nil || len(parts) == 1 {
		return minVal, minVal, err
	}
	maxVal, err = parseByteSize(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if minVal > maxVal {
		return 0, 0, fmt.Errorf("size range min is greater than max")
	}
	return minVal, maxVal, nil
}

func parseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{{"kb", 1024}, {"k", 1024}, {"mb", 1024 * 1024}, {"m", 1024 * 1024}} {
		if strings.HasSuffix(s, unit.suffix) {
			return parseByteNumber(strings.TrimSuffix(s, unit.suffix), unit.mult)
		}
	}
	return parseByteNumber(s, 1)
}

func parseByteNumber(raw string, multiplier int64) (int64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("byte size must be positive")
	}
	return int64(value * float64(multiplier)), nil
}
