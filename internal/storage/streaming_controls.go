// Copyright 2026 ICAP Mock

package storage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *StreamSendConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawSend StreamSendConfig
	var raw rawSend
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*s = StreamSendConfig(raw)
	s.IsSet = true
	return nil
}

func (s *StreamSendConfig) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*s = StreamSendConfig{}
		return nil
	}
	type rawSend StreamSendConfig
	var raw rawSend
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = StreamSendConfig(raw)
	s.IsSet = true
	return nil
}

func (t *StreamThrottleConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawThrottle StreamThrottleConfig
	var raw rawThrottle
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*t = StreamThrottleConfig(raw)
	t.IsSet = true
	return nil
}

func (t *StreamThrottleConfig) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*t = StreamThrottleConfig{}
		return nil
	}
	type rawThrottle StreamThrottleConfig
	var raw rawThrottle
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*t = StreamThrottleConfig(raw)
	t.IsSet = true
	return nil
}

func (e *StreamEndConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawEnd StreamEndConfig
	var raw rawEnd
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*e = StreamEndConfig(raw)
	e.IsSet = true
	return nil
}

func (e *StreamEndConfig) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*e = StreamEndConfig{}
		return nil
	}
	type rawEnd StreamEndConfig
	var raw rawEnd
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*e = StreamEndConfig(raw)
	e.IsSet = true
	return nil
}

func (p *PercentSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("percent must be a scalar")
	}
	minVal, maxVal, err := parsePercentSpec(node.Value)
	if err != nil {
		return err
	}
	*p = PercentSpec{Min: minVal, Max: maxVal, IsSet: true}
	return nil
}

func (p *PercentSpec) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	minVal, maxVal, err := parseJSONPercentSpec(raw)
	if err != nil {
		return err
	}
	*p = PercentSpec{Min: minVal, Max: maxVal, IsSet: true}
	return nil
}

func validateStreamControls(s *StreamConfig) error {
	if err := validateNewStreamConflicts(s); err != nil {
		return err
	}
	if hasNewStreamControls(s) {
		if err := validateNewStreamControls(s); err != nil {
			return err
		}
		applyNewStreamControls(s)
	}
	if err := validateStreamTiming(s); err != nil {
		return err
	}
	return validateStreamFinish(s)
}

func validateNewStreamConflicts(s *StreamConfig) error {
	if !hasNewStreamControls(s) {
		return nil
	}
	if s.derivedControls {
		return nil
	}
	if s.Duration.IsSet {
		return fmt.Errorf("send/throttle/end and duration are mutually exclusive")
	}
	if hasLegacyChunksConfig(s.Chunks) {
		return fmt.Errorf("send/throttle/end and chunks are mutually exclusive")
	}
	if hasLegacyFinishConfig(s.Finish) {
		return fmt.Errorf("send/throttle/end and finish are mutually exclusive")
	}
	return nil
}

func validateNewStreamControls(s *StreamConfig) error {
	mode := newStreamEndMode(s.End)
	if !validNewEndMode(mode) {
		return fmt.Errorf("end.mode must be complete, fin, or term")
	}
	if partialNewEndMode(mode) {
		return validatePartialEndControls(s, mode)
	}
	if s.Send.Percent.IsSet {
		return fmt.Errorf("send.percent is only allowed when end.mode is fin or term")
	}
	return nil
}

func validatePartialEndControls(s *StreamConfig, mode string) error {
	if !s.Send.Percent.IsSet {
		return fmt.Errorf("end.mode %s requires send.percent", mode)
	}
	if !partialPercentSpec(s.Send.Percent) {
		return fmt.Errorf("send.percent for %s must be between 1 and 99", mode)
	}
	if !newFINHasPacing(s) {
		return fmt.Errorf("end.mode %s requires send.duration or throttle.every", mode)
	}
	return nil
}

func applyNewStreamControls(s *StreamConfig) {
	s.derivedControls = true
	if s.End.IsSet || s.End.Mode != "" {
		s.Finish.Mode = newStreamEndMode(s.End)
	}
	if s.Send.Duration.IsSet && !s.Throttle.Every.IsSet {
		s.Duration = s.Send.Duration
	}
	if s.Throttle.ChunkSize.IsSet {
		s.Chunks.Size = s.Throttle.ChunkSize
	}
	if s.Throttle.Every.IsSet {
		s.Chunks.Delay = s.Throttle.Every
	}
}

func hasNewStreamControls(s *StreamConfig) bool {
	return s.Send.IsSet || s.Throttle.IsSet || s.End.IsSet ||
		s.Send.Percent.IsSet || s.Send.Duration.IsSet ||
		s.Throttle.ChunkSize.IsSet || s.Throttle.Every.IsSet || s.End.Mode != ""
}

func hasLegacyFinishConfig(f StreamFinishConfig) bool {
	return f.Mode != "" || hasFinishFINConfig(f.Fin) ||
		f.CompletePercent != 0 || f.FinPercent != 0
}

func hasLegacyChunksConfig(c StreamChunksConfig) bool {
	return c.Size.IsSet || c.Delay.IsSet
}

func newStreamEndMode(end StreamEndConfig) string {
	if end.Mode == "" {
		return streamFinishComplete
	}
	return end.Mode
}

func validNewEndMode(mode string) bool {
	return mode == streamFinishComplete || partialNewEndMode(mode)
}

func partialNewEndMode(mode string) bool {
	return mode == streamFinishFIN || mode == streamFinishTerm
}

func partialPercentSpec(p PercentSpec) bool {
	return p.Min >= 1 && p.Max <= 99
}

func newFINHasPacing(s *StreamConfig) bool {
	return positiveDurationSpec(s.Send.Duration) || positiveDurationSpec(s.Throttle.Every)
}

func positiveDurationSpec(d DurationSpec) bool { return d.IsSet && d.Max > 0 }

func parsePercentSpec(raw string) (minVal, maxVal int, err error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid percent range %q", raw)
	}
	minVal, err = parsePercentValue(parts[0])
	if err != nil || len(parts) == 1 {
		return minVal, minVal, err
	}
	maxVal, err = parsePercentValue(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return validatePercentRange(minVal, maxVal)
}

func parseJSONPercentSpec(raw any) (minVal, maxVal int, err error) {
	switch v := raw.(type) {
	case string:
		return parsePercentSpec(v)
	case float64:
		if v != float64(int(v)) {
			return 0, 0, fmt.Errorf("percent must be an integer")
		}
		return validatePercentRange(int(v), int(v))
	default:
		return 0, 0, fmt.Errorf("percent must be a string or number")
	}
}

func parsePercentValue(raw string) (int, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "%")
	value, err := strconv.Atoi(strings.TrimSpace(trimmed))
	if err != nil {
		return 0, fmt.Errorf("invalid percent %q: %w", raw, err)
	}
	return value, nil
}

func validatePercentRange(minVal, maxVal int) (minPercent, maxPercent int, err error) {
	if !validPercent(minVal) || !validPercent(maxVal) {
		return 0, 0, fmt.Errorf("percent must be between 0 and 100")
	}
	if minVal > maxVal {
		return 0, 0, fmt.Errorf("percent range min is greater than max")
	}
	return minVal, maxVal, nil
}
