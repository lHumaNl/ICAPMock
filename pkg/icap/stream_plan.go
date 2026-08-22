// Copyright 2026 ICAP Mock

package icap

import (
	"fmt"
	"math"
	"time"
)

const (
	automaticStreamCadence = 100 * time.Millisecond
	defaultStreamChunkSize = 16 * 1024
	maxStreamChunks        = 1_000
)

// BodyStreamPlanOptions contains values selected once for one stream.
// Zero target values mean that the corresponding planning hint is omitted.
type BodyStreamPlanOptions struct {
	FinishMode       string
	SourceSize       int64
	SelectedBytes    int64
	TargetChunkSize  int64
	Duration         time.Duration
	Every            time.Duration
	TargetChunks     int
	SelectedBytesSet bool
}

// BodyStreamPlan is an immutable delivery schedule produced by PlanBodyStream.
// Its fields are intentionally private; accessors expose selected and derived values.
type BodyStreamPlan struct {
	finishMode      string
	sourceSize      int64
	bodyBytes       int64
	targetChunkSize int64
	duration        time.Duration
	every           time.Duration
	chunkCount      int
	targetChunks    int
}

// PlanBodyStream creates a deterministic stream delivery plan without I/O or randomness.
func PlanBodyStream(options BodyStreamPlanOptions) (BodyStreamPlan, error) {
	if options.FinishMode == "" {
		options.FinishMode = StreamFinishComplete
	}
	if err := validateBodyStreamPlanOptions(options); err != nil {
		return BodyStreamPlan{}, err
	}
	bodyBytes := selectedBodyBytes(options)
	chunkCount := plannedChunkCount(options, bodyBytes)
	if err := validateCadenceRange(options.Every, chunkCount); err != nil {
		return BodyStreamPlan{}, err
	}
	return newBodyStreamPlan(options, bodyBytes, chunkCount), nil
}

func validateBodyStreamPlanOptions(options BodyStreamPlanOptions) error {
	if options.SourceSize < 0 {
		return fmt.Errorf("stream source size must be known and non-negative")
	}
	if options.Duration < 0 || options.Every < 0 {
		return fmt.Errorf("stream duration and cadence must be non-negative")
	}
	if options.TargetChunkSize < 0 || options.TargetChunks < 0 {
		return fmt.Errorf("stream target chunk values must be non-negative")
	}
	if options.TargetChunkSize > 0 && options.TargetChunks > 0 {
		return fmt.Errorf("target chunk size and target chunks are mutually exclusive")
	}
	return validateSelectedBodyBytes(options)
}

func validateSelectedBodyBytes(options BodyStreamPlanOptions) error {
	if !validSelectedFinishMode(options.FinishMode) {
		return fmt.Errorf("unsupported stream finish mode %q", options.FinishMode)
	}
	if options.SelectedBytesSet && (options.SelectedBytes < 0 || options.SelectedBytes > options.SourceSize) {
		return fmt.Errorf("selected stream bytes must be between zero and source size")
	}
	if options.FinishMode == StreamFinishComplete && options.SelectedBytesSet && options.SelectedBytes != options.SourceSize {
		return fmt.Errorf("complete stream selected bytes must equal source size")
	}
	return nil
}

func validSelectedFinishMode(mode string) bool {
	return mode == StreamFinishComplete || mode == StreamFinishFIN || mode == StreamFinishTerm
}

func selectedBodyBytes(options BodyStreamPlanOptions) int64 {
	if options.SelectedBytesSet {
		return options.SelectedBytes
	}
	return options.SourceSize
}

func plannedChunkCount(options BodyStreamPlanOptions, bodyBytes int64) int {
	if bodyBytes == 0 {
		return 0
	}
	preferred := preferredChunkCount(options, bodyBytes)
	preferred = minInt64(preferred, bodyBytes, maxStreamChunks)
	if options.Duration > 0 && options.Every > 0 {
		preferred = minInt64(preferred, cadenceSlots(options.Duration, options.Every))
	}
	return int(maxInt64(preferred, 1))
}

func preferredChunkCount(options BodyStreamPlanOptions, bodyBytes int64) int64 {
	if options.TargetChunkSize > 0 {
		return ceilPositive(bodyBytes, options.TargetChunkSize)
	}
	if options.TargetChunks > 0 {
		return int64(options.TargetChunks)
	}
	if options.Duration > 0 {
		return ceilPositive(int64(options.Duration), int64(automaticStreamCadence)) + 1
	}
	return ceilPositive(bodyBytes, defaultStreamChunkSize)
}

func cadenceSlots(duration, every time.Duration) int64 {
	intervals := int64(duration / every)
	if intervals >= maxStreamChunks-1 {
		return maxStreamChunks
	}
	return intervals + 1
}

func validateCadenceRange(every time.Duration, chunks int) error {
	if every == 0 || chunks <= 1 {
		return nil
	}
	if int64(every) > math.MaxInt64/int64(chunks-1) {
		return fmt.Errorf("stream cadence schedule exceeds time duration range")
	}
	return nil
}

func newBodyStreamPlan(options BodyStreamPlanOptions, bodyBytes int64, chunks int) BodyStreamPlan {
	return BodyStreamPlan{
		sourceSize:      options.SourceSize,
		bodyBytes:       bodyBytes,
		targetChunkSize: options.TargetChunkSize,
		duration:        options.Duration,
		every:           options.Every,
		chunkCount:      chunks,
		targetChunks:    options.TargetChunks,
		finishMode:      options.FinishMode,
	}
}

// SourceSize returns the stable source size used to create the plan.
func (p BodyStreamPlan) SourceSize() int64 { return p.sourceSize }

// BodyBytes returns the maximum body bytes selected for delivery.
func (p BodyStreamPlan) BodyBytes() int64 { return p.bodyBytes }

// Duration returns the selected target delivery duration.
func (p BodyStreamPlan) Duration() time.Duration { return p.duration }

// Every returns the selected minimum interval between planned chunk starts.
func (p BodyStreamPlan) Every() time.Duration { return p.every }

// TargetChunkSize returns the selected preferred chunk size.
func (p BodyStreamPlan) TargetChunkSize() int64 { return p.targetChunkSize }

// TargetChunks returns the preferred non-strict chunk count.
func (p BodyStreamPlan) TargetChunks() int { return p.targetChunks }

// ChunkCount returns the planned non-empty protocol chunk count.
func (p BodyStreamPlan) ChunkCount() int { return p.chunkCount }

// EffectiveChunkSize returns the largest balanced chunk size in the plan.
func (p BodyStreamPlan) EffectiveChunkSize() int64 {
	if p.chunkCount == 0 {
		return 0
	}
	return ceilPositive(p.bodyBytes, int64(p.chunkCount))
}

// FinishMode returns the selected terminal framing mode.
func (p BodyStreamPlan) FinishMode() string { return p.finishMode }

// ChunkSize returns one balanced planned chunk size.
func (p BodyStreamPlan) ChunkSize(index int) (int64, bool) {
	if index < 0 || index >= p.chunkCount {
		return 0, false
	}
	base := p.bodyBytes / int64(p.chunkCount)
	if int64(index) < p.bodyBytes%int64(p.chunkCount) {
		base++
	}
	return base, true
}

// ChunkOffset returns the planned chunk-start offset from stream activation.
func (p BodyStreamPlan) ChunkOffset(index int) (time.Duration, bool) {
	if index < 0 || index >= p.chunkCount {
		return 0, false
	}
	if p.duration > 0 {
		return durationChunkOffset(p.duration, p.chunkCount, index), true
	}
	return time.Duration(index) * p.every, true
}

func durationChunkOffset(duration time.Duration, chunks, index int) time.Duration {
	if chunks == 1 {
		return duration
	}
	intervals := int64(chunks - 1)
	position := int64(index)
	whole := int64(duration) / intervals
	remainder := int64(duration) % intervals
	return time.Duration(whole*position + remainder*position/intervals)
}

func ceilPositive(value, divisor int64) int64 {
	result := value / divisor
	if value%divisor != 0 {
		result++
	}
	return result
}

func minInt64(value int64, others ...int64) int64 {
	for _, other := range others {
		if other < value {
			value = other
		}
	}
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
