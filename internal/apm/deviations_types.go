package apm

import (
	"fmt"
	"math"
	"time"
)

type DeviationArgs struct {
	ServiceName      string  `json:"service_name,omitempty"`
	Env              string  `json:"env,omitempty"`
	Datasource       string  `json:"datasource,omitempty"`
	StartTimeISO     string  `json:"start_time_iso,omitempty"`
	EndTimeISO       string  `json:"end_time_iso,omitempty"`
	LookbackMinutes  float64 `json:"lookback_minutes,omitempty"`
	BaselineStartISO string  `json:"baseline_start_time_iso,omitempty"`
	BaselineEndISO   string  `json:"baseline_end_time_iso,omitempty"`
	MaxServices      int     `json:"max_services,omitempty"`
	MaxOperations    int     `json:"max_operations,omitempty"`
}

type EvidenceQuality struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons,omitempty"`
}

type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type DeviationWindows struct {
	TimeSource       string `json:"time_source"`
	BaselineMode     string `json:"baseline_mode"`
	QueryStepSeconds int64  `json:"query_step_seconds"`

	RequestedCurrentStart  time.Time `json:"requested_current_start"`
	RequestedCurrentEnd    time.Time `json:"requested_current_end"`
	RequestedBaselineStart time.Time `json:"requested_baseline_start"`
	RequestedBaselineEnd   time.Time `json:"requested_baseline_end"`

	EffectiveCurrentStart  time.Time `json:"effective_current_start"`
	EffectiveCurrentEnd    time.Time `json:"effective_current_end"`
	EffectiveBaselineStart time.Time `json:"effective_baseline_start"`
	EffectiveBaselineEnd   time.Time `json:"effective_baseline_end"`

	ExcludedCurrentPoints  int `json:"excluded_current_points"`
	ExcludedBaselinePoints int `json:"excluded_baseline_points"`

	QueryStep time.Duration `json:"-"`
}

type SignalDefinition struct {
	Name          string `json:"name"`
	Definition    string `json:"definition,omitempty"`
	Aggregation   string `json:"aggregation"`
	Unit          string `json:"unit"`
	HigherIsWorse bool   `json:"higher_is_worse"`
}

type Distribution struct {
	Q25    float64 `json:"q25"`
	Median float64 `json:"median"`
	Q75    float64 `json:"q75"`
	IQR    float64 `json:"iqr"`
	Peak   float64 `json:"peak"`
}

type MetricEvidence struct {
	ObservedPoints int     `json:"observed_points"`
	ExpectedPoints int     `json:"expected_points"`
	Coverage       float64 `json:"coverage"`
	MissingValues  int     `json:"missing_values,omitempty"`
	ExcludedValues int     `json:"excluded_values,omitempty"`
}

type WindowEvidence struct {
	Selected        MetricEvidence `json:"selected"`
	Requests        MetricEvidence `json:"requests"`
	Errors          MetricEvidence `json:"errors"`
	ErrorPercentage MetricEvidence `json:"error_percentage"`
	Apdex           MetricEvidence `json:"apdex"`
	P95Latency      MetricEvidence `json:"p95_latency"`
}

type WindowSummary struct {
	Value           float64        `json:"value"`
	RequestTotal    float64        `json:"request_total"`
	ErrorTotal      float64        `json:"error_total"`
	RequestRPM      float64        `json:"request_rpm"`
	ErrorRPM        float64        `json:"error_rpm"`
	ErrorPercentage float64        `json:"error_percentage"`
	Apdex           *float64       `json:"apdex,omitempty"`
	P95Latency      *Distribution  `json:"p95_latency,omitempty"`
	Distribution    Distribution   `json:"distribution"`
	Evidence        WindowEvidence `json:"evidence"`
}

type SignalComparison struct {
	Definition      SignalDefinition `json:"definition"`
	Current         WindowSummary    `json:"current"`
	Baseline        WindowSummary    `json:"baseline"`
	AbsoluteDelta   float64          `json:"absolute_delta"`
	RelativeDelta   *float64         `json:"relative_delta,omitempty"`
	Direction       string           `json:"direction"`
	Classification  string           `json:"classification"`
	PresenceChange  string           `json:"presence_change,omitempty"`
	EvidenceQuality EvidenceQuality  `json:"evidence_quality"`
}

type ServiceDeviation struct {
	ServiceName string             `json:"service_name"`
	Env         string             `json:"env,omitempty"`
	Signals     []SignalComparison `json:"signals"`
}

type LeaderboardEntry struct {
	ServiceName    string           `json:"service_name"`
	Env            string           `json:"env,omitempty"`
	SignalCategory string           `json:"signal_category"`
	Comparison     SignalComparison `json:"comparison"`
}

type SignalLeaderboard struct {
	Regressions  []LeaderboardEntry `json:"regressions"`
	Improvements []LeaderboardEntry `json:"improvements"`
}

type DeviationLeaderboards struct {
	Reliability      SignalLeaderboard `json:"reliability"`
	Experience       SignalLeaderboard `json:"experience"`
	SustainedLatency SignalLeaderboard `json:"sustained_latency"`
}

type TelemetryChange struct {
	ServiceName string `json:"service_name"`
	Env         string `json:"env,omitempty"`
	Change      string `json:"change"`
}

type DeviationResponse struct {
	Scope            string                `json:"scope"`
	Datasource       string                `json:"datasource,omitempty"`
	Windows          DeviationWindows      `json:"windows"`
	Services         []ServiceDeviation    `json:"services"`
	Leaderboards     DeviationLeaderboards `json:"leaderboards"`
	TelemetryChanges []TelemetryChange     `json:"telemetry_changes"`
	ThroughputShifts []LeaderboardEntry    `json:"throughput_shifts"`
	Outcome          string                `json:"outcome"`
	Warnings         []string              `json:"warnings,omitempty"`
}

type bucket struct {
	Timestamp    time.Time
	Requests     float64
	Errors       float64
	Apdex        *float64
	P95LatencyMS *float64
}

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

	requestedBaseline := TimeWindow{Start: requestedCurrent.Start.Add(-duration), End: requestedCurrent.Start}
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

	if timeSource == "explicit" && (!isAligned(requestedCurrent.Start, queryStep) || !isAligned(requestedCurrent.End, queryStep)) {
		return DeviationWindows{}, fmt.Errorf("explicit current window boundaries must align to query step")
	}
	if baselineMode == "explicit" && (!isAligned(requestedBaseline.Start, queryStep) || !isAligned(requestedBaseline.End, queryStep)) {
		return DeviationWindows{}, fmt.Errorf("explicit baseline window boundaries must align to query step")
	}

	effectiveCurrent := requestedCurrent
	if timeSource != "explicit" {
		effectiveCurrent.Start = ceilTime(requestedCurrent.Start, queryStep)
		effectiveCurrent.End = requestedCurrent.End.UTC().Truncate(queryStep)
		completedEnd := now.UTC().Truncate(queryStep)
		if effectiveCurrent.End.After(completedEnd) {
			effectiveCurrent.End = completedEnd
		}
		if !effectiveCurrent.End.After(effectiveCurrent.Start) {
			return DeviationWindows{}, fmt.Errorf("requested window contains no completed buckets")
		}
	}

	startOffset := effectiveCurrent.Start.Sub(requestedCurrent.Start)
	endOffset := requestedCurrent.End.Sub(effectiveCurrent.End)
	effectiveBaseline := TimeWindow{
		Start: requestedBaseline.Start.Add(startOffset),
		End:   requestedBaseline.End.Add(-endOffset),
	}
	if baselineMode == "explicit" {
		if startOffset != 0 || endOffset != 0 {
			return DeviationWindows{}, fmt.Errorf("explicit baseline requires an aligned current window")
		}
		effectiveBaseline = requestedBaseline
	}

	requestedCurrentCapacity := bucketCapacity(requestedCurrent, queryStep)
	requestedBaselineCapacity := bucketCapacity(requestedBaseline, queryStep)
	effectiveCurrentCapacity := bucketCapacity(effectiveCurrent, queryStep)
	effectiveBaselineCapacity := bucketCapacity(effectiveBaseline, queryStep)

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
		ExcludedCurrentPoints:  requestedCurrentCapacity - effectiveCurrentCapacity,
		ExcludedBaselinePoints: requestedBaselineCapacity - effectiveBaselineCapacity,
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

func ceilTime(value time.Time, step time.Duration) time.Time {
	floor := value.UTC().Truncate(step)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(step)
}

func isAligned(value time.Time, step time.Duration) bool {
	return value.Equal(value.UTC().Truncate(step))
}

func bucketCapacity(window TimeWindow, step time.Duration) int {
	if !window.End.After(window.Start) {
		return 0
	}
	return int(math.Ceil(float64(window.End.Sub(window.Start)) / float64(step)))
}
