// Copyright 2026 ICAP Mock

package processor

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/icap-mock/icap-mock/internal/storage"
	"github.com/icap-mock/icap-mock/pkg/icap"
)

const weightedFinishScale int64 = 100

type inclusiveStreamSelector interface {
	SelectInclusive(minimum, maximum int64) int64
}

type randomStreamSelector struct{}

func (randomStreamSelector) SelectInclusive(minimum, maximum int64) int64 {
	if maximum <= minimum {
		return minimum
	}
	difference := maximum - minimum
	if difference == math.MaxInt64 {
		return int64(rand.Uint64() >> 1) //nolint:gosec // scenario randomness is non-security.
	}
	return minimum + rand.Int63n(difference+1) //nolint:gosec // scenario randomness is non-security.
}

func newBodyStream(
	cfg *storage.StreamConfig,
	payload icap.StreamPayload,
	selector inclusiveStreamSelector,
) (*icap.BodyStream, error) {
	if cfg == nil || payload == nil {
		return nil, fmt.Errorf("stream configuration and payload are required")
	}
	size, known := payload.SizeHint()
	if !known {
		return nil, fmt.Errorf("stream source size must be known before planning")
	}
	options, err := selectBodyStreamPlanOptions(cfg, size, selector)
	if err != nil {
		return nil, err
	}
	plan, err := icap.PlanBodyStream(options)
	if err != nil {
		return nil, err
	}
	return &icap.BodyStream{Payload: payload, Plan: plan}, nil
}

func selectBodyStreamPlanOptions(
	cfg *storage.StreamConfig,
	sourceSize int64,
	selector inclusiveStreamSelector,
) (icap.BodyStreamPlanOptions, error) {
	if selector == nil {
		selector = randomStreamSelector{}
	}
	if usesDirectStreamControls(cfg) {
		return selectDirectPlanOptions(cfg, sourceSize, selector)
	}
	return selectLegacyPlanOptions(cfg, sourceSize, selector)
}

func selectDirectPlanOptions(
	cfg *storage.StreamConfig,
	sourceSize int64,
	selector inclusiveStreamSelector,
) (icap.BodyStreamPlanOptions, error) {
	duration, err := selectDuration(cfg.Send.Duration, selector)
	if err != nil {
		return icap.BodyStreamPlanOptions{}, err
	}
	targetSize, every, err := selectThrottle(
		cfg.Throttle.TargetChunkSize, cfg.Throttle.Every, selector,
	)
	if err != nil {
		return icap.BodyStreamPlanOptions{}, err
	}
	options := icap.BodyStreamPlanOptions{
		FinishMode: cfg.End.Mode, SourceSize: sourceSize, Duration: duration,
		TargetChunkSize: targetSize, TargetChunks: cfg.Throttle.TargetChunks, Every: every,
	}
	return selectDirectBodyBytes(options, cfg.Send.Percent, selector)
}

func selectDirectBodyBytes(
	options icap.BodyStreamPlanOptions,
	percentSpec storage.PercentSpec,
	selector inclusiveStreamSelector,
) (icap.BodyStreamPlanOptions, error) {
	if percentSpec.IsSet {
		percent, selectErr := selectPercent(percentSpec, selector)
		if selectErr != nil {
			return icap.BodyStreamPlanOptions{}, selectErr
		}
		options.SelectedBytes = percentOf(options.SourceSize, percent)
		options.SelectedBytesSet = true
	}
	return options, nil
}

func selectLegacyPlanOptions(
	cfg *storage.StreamConfig,
	sourceSize int64,
	selector inclusiveStreamSelector,
) (icap.BodyStreamPlanOptions, error) {
	finishMode, err := selectLegacyFinishMode(cfg.Finish, selector)
	if err != nil {
		return icap.BodyStreamPlanOptions{}, err
	}
	targetSize, every, err := selectThrottle(cfg.Chunks.Size, cfg.Chunks.Delay, selector)
	if err != nil {
		return icap.BodyStreamPlanOptions{}, err
	}
	duration, err := selectLegacyDuration(cfg, finishMode, selector)
	if err != nil {
		return icap.BodyStreamPlanOptions{}, err
	}
	options := icap.BodyStreamPlanOptions{
		FinishMode: finishMode, SourceSize: sourceSize, Duration: duration,
		TargetChunkSize: targetSize, Every: every,
	}
	return selectLegacyBodyBytes(options, cfg.Finish.Fin.After.Bytes, selector)
}

func selectLegacyBodyBytes(
	options icap.BodyStreamPlanOptions,
	bytesSpec storage.SizeSpec,
	selector inclusiveStreamSelector,
) (icap.BodyStreamPlanOptions, error) {
	if partialStreamFinish(options.FinishMode) && bytesSpec.IsSet {
		selected, selectErr := selectSize(bytesSpec, selector)
		if selectErr != nil {
			return icap.BodyStreamPlanOptions{}, selectErr
		}
		options.SelectedBytes = min(selected, options.SourceSize)
		options.SelectedBytesSet = true
	}
	return options, nil
}

func selectThrottle(
	sizeSpec storage.SizeSpec,
	everySpec storage.DurationSpec,
	selector inclusiveStreamSelector,
) (int64, time.Duration, error) {
	targetSize, err := selectSize(sizeSpec, selector)
	if err != nil {
		return 0, 0, err
	}
	every, err := selectDuration(everySpec, selector)
	return targetSize, every, err
}

func selectLegacyDuration(
	cfg *storage.StreamConfig,
	finishMode string,
	selector inclusiveStreamSelector,
) (time.Duration, error) {
	duration, err := selectDuration(cfg.Duration, selector)
	if err != nil || !partialStreamFinish(finishMode) || !cfg.Finish.Fin.After.Time.IsSet {
		return duration, err
	}
	finishDuration, err := selectDuration(cfg.Finish.Fin.After.Time, selector)
	if err != nil || duration == 0 || finishDuration < duration {
		return finishDuration, err
	}
	return duration, nil
}

func selectLegacyFinishMode(
	finish storage.StreamFinishConfig,
	selector inclusiveStreamSelector,
) (string, error) {
	if finish.Mode == "" {
		return icap.StreamFinishComplete, nil
	}
	if finish.Mode != icap.StreamFinishWeighted {
		return finish.Mode, nil
	}
	draw, err := selectInclusive(0, weightedFinishScale-1, selector)
	if err != nil {
		return "", err
	}
	if draw < int64(finish.CompletePercent) {
		return icap.StreamFinishComplete, nil
	}
	return icap.StreamFinishFIN, nil
}

func selectDuration(spec storage.DurationSpec, selector inclusiveStreamSelector) (time.Duration, error) {
	if !spec.IsSet {
		return 0, nil
	}
	selected, err := selectInclusive(int64(spec.Min), int64(spec.Max), selector)
	return time.Duration(selected), err
}

func selectSize(spec storage.SizeSpec, selector inclusiveStreamSelector) (int64, error) {
	if !spec.IsSet {
		return 0, nil
	}
	return selectInclusive(spec.Min, spec.Max, selector)
}

func selectPercent(spec storage.PercentSpec, selector inclusiveStreamSelector) (int64, error) {
	return selectInclusive(int64(spec.Min), int64(spec.Max), selector)
}

func selectInclusive(minimum, maximum int64, selector inclusiveStreamSelector) (int64, error) {
	if minimum < 0 || minimum > maximum {
		return 0, fmt.Errorf("stream selection range %d-%d is invalid", minimum, maximum)
	}
	selected := selector.SelectInclusive(minimum, maximum)
	if selected < minimum || selected > maximum {
		return 0, fmt.Errorf("stream selector returned %d outside %d-%d", selected, minimum, maximum)
	}
	return selected, nil
}

func usesDirectStreamControls(cfg *storage.StreamConfig) bool {
	return cfg.Send.IsSet || cfg.Throttle.IsSet || cfg.End.IsSet ||
		cfg.Send.Percent.IsSet || cfg.Send.Duration.IsSet ||
		cfg.Throttle.TargetChunkSize.IsSet || cfg.Throttle.TargetChunks != 0 ||
		cfg.Throttle.Every.IsSet || cfg.End.Mode != ""
}

func partialStreamFinish(mode string) bool {
	return mode == icap.StreamFinishFIN || mode == icap.StreamFinishTerm
}

func percentOf(size, percent int64) int64 {
	return (size/weightedFinishScale)*percent + (size%weightedFinishScale)*percent/weightedFinishScale
}
