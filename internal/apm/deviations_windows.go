package apm

import (
	"fmt"
	"math"
	"time"
)

const defaultDeviationLookback = 60 * time.Minute

func resolveDeviationWindows(args DeviationArgs, now time.Time, queryStep time.Duration) (DeviationWindows, error) {
	if queryStep <= 0 {
		return DeviationWindows{}, fmt.Errorf("query step must be positive")
	}
	if args.LookbackMinutes < 0 {
		return DeviationWindows{}, fmt.Errorf("lookback_minutes must be positive")
	}

	hasCurrentStart := args.StartTimeISO != ""
	hasCurrentEnd := args.EndTimeISO != ""
	if hasCurrentStart != hasCurrentEnd {
		return DeviationWindows{}, fmt.Errorf("start_time_iso and end_time_iso must be provided together")
	}
	hasBaselineStart := args.BaselineStartISO != ""
	hasBaselineEnd := args.BaselineEndISO != ""
	if hasBaselineStart != hasBaselineEnd {
		return DeviationWindows{}, fmt.Errorf("baseline_start_time_iso and baseline_end_time_iso must be provided together")
	}
	if hasCurrentStart && args.LookbackMinutes != 0 {
		return DeviationWindows{}, fmt.Errorf("explicit current timestamps cannot be combined with lookback_minutes")
	}

	requestedCurrent, timeSource, err := requestedCurrentWindow(args, now)
	if err != nil {
		return DeviationWindows{}, err
	}
	duration := requestedCurrent.End.Sub(requestedCurrent.Start)
	if duration <= 0 {
		return DeviationWindows{}, fmt.Errorf("current window end must be after start")
	}

	requestedBaseline := TimeWindow{
		Start: requestedCurrent.Start.Add(-duration),
		End:   requestedCurrent.Start,
	}
	baselineMode := "previous_period"
	if hasBaselineStart {
		requestedBaseline, err = parseWindow(args.BaselineStartISO, args.BaselineEndISO, "baseline")
		if err != nil {
			return DeviationWindows{}, err
		}
		if requestedBaseline.End.Sub(requestedBaseline.Start) != duration {
			return DeviationWindows{}, fmt.Errorf("baseline window duration must equal current window duration")
		}
		baselineMode = "explicit"
	}

	latestCompletedEnd := now.UTC().Truncate(queryStep).Add(-queryStep)
	effectiveCurrentEnd := requestedCurrent.End.UTC().Truncate(queryStep)
	if effectiveCurrentEnd.After(latestCompletedEnd) {
		effectiveCurrentEnd = latestCompletedEnd
	}
	effectiveCurrent := TimeWindow{Start: effectiveCurrentEnd.Add(-duration), End: effectiveCurrentEnd}

	effectiveBaseline := TimeWindow{
		Start: effectiveCurrent.Start.Add(-duration),
		End:   effectiveCurrent.Start,
	}
	if baselineMode == "explicit" {
		effectiveBaseline.End = requestedBaseline.End.UTC().Truncate(queryStep)
		effectiveBaseline.Start = effectiveBaseline.End.Add(-duration)
	}

	return DeviationWindows{
		TimeSource:             timeSource,
		BaselineMode:           baselineMode,
		QueryStepSeconds:       int64(queryStep / time.Second),
		RequestedCurrentStart:  requestedCurrent.Start,
		RequestedCurrentEnd:    requestedCurrent.End,
		RequestedBaselineStart: requestedBaseline.Start,
		RequestedBaselineEnd:   requestedBaseline.End,
		EffectiveCurrentStart:  effectiveCurrent.Start,
		EffectiveCurrentEnd:    effectiveCurrent.End,
		EffectiveBaselineStart: effectiveBaseline.Start,
		EffectiveBaselineEnd:   effectiveBaseline.End,
		ExcludedCurrentPoints:  excludedPointCount(requestedCurrent.End, effectiveCurrent.End, queryStep),
		ExcludedBaselinePoints: excludedPointCount(requestedBaseline.End, effectiveBaseline.End, queryStep),
		QueryStep:              queryStep,
	}, nil
}

func requestedCurrentWindow(args DeviationArgs, now time.Time) (TimeWindow, string, error) {
	if args.StartTimeISO != "" {
		window, err := parseWindow(args.StartTimeISO, args.EndTimeISO, "current")
		return window, "explicit", err
	}

	duration := defaultDeviationLookback
	timeSource := "default"
	if args.LookbackMinutes != 0 {
		duration = time.Duration(args.LookbackMinutes * float64(time.Minute))
		timeSource = "relative_lookback"
	}
	if duration <= 0 {
		return TimeWindow{}, "", fmt.Errorf("lookback_minutes must be positive")
	}
	now = now.UTC()
	return TimeWindow{Start: now.Add(-duration), End: now}, timeSource, nil
}

func parseWindow(startISO, endISO, name string) (TimeWindow, error) {
	start, err := time.Parse(time.RFC3339, startISO)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("invalid %s start time: %w", name, err)
	}
	end, err := time.Parse(time.RFC3339, endISO)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("invalid %s end time: %w", name, err)
	}
	if !end.After(start) {
		return TimeWindow{}, fmt.Errorf("%s window end must be after start", name)
	}
	return TimeWindow{Start: start.UTC(), End: end.UTC()}, nil
}

func excludedPointCount(requestedEnd, effectiveEnd time.Time, step time.Duration) int {
	if !requestedEnd.After(effectiveEnd) {
		return 0
	}
	return int(math.Ceil(float64(requestedEnd.Sub(effectiveEnd)) / float64(step)))
}
