package apm

import "time"

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
	Peak   float64 `json:"peak,omitempty"`
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
	ErrorPercentage *float64       `json:"error_percentage,omitempty"`
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
	Requests     *float64
	Errors       *float64
	Apdex        *float64
	P95LatencyMS *float64
}
