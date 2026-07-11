package apm

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func float64Pointer(value float64) *float64 { return &value }

func completeEvidence(points int) MetricEvidence {
	return MetricEvidence{ObservedPoints: points, ExpectedPoints: points, Coverage: 1}
}

func TestSummarizeWindowUsesRatioOfTotals(t *testing.T) {
	got := summarizeWindow([]bucket{{Requests: 1, Errors: 1}, {Requests: 999, Errors: 0}}, time.Minute, 2)
	if got.ErrorPercentage != 0.1 {
		t.Fatalf("error percentage = %v, want 0.1", got.ErrorPercentage)
	}
	if got.RequestRPM != 500 || got.ErrorRPM != 0.5 {
		t.Fatalf("unexpected rates: requests=%v errors=%v", got.RequestRPM, got.ErrorRPM)
	}
}

func TestSummarizeWindowUsesRequestWeightedApdex(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 1, Apdex: float64Pointer(1)},
		{Requests: 99, Apdex: float64Pointer(0.5)},
	}, time.Minute, 2)
	if got.Apdex == nil || *got.Apdex != 0.505 {
		t.Fatalf("weighted Apdex = %v, want 0.505", got.Apdex)
	}
}

func TestSummarizeWindowDoesNotTreatMissingApdexAsZero(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 10, Apdex: float64Pointer(0.8)},
		{Requests: 90, Apdex: nil},
	}, time.Minute, 2)
	if got.Apdex == nil || *got.Apdex != 0.8 {
		t.Fatalf("Apdex = %v, want 0.8 from observed value only", got.Apdex)
	}
	if got.Evidence.Apdex.ObservedPoints != 1 || got.Evidence.Apdex.Coverage != 0.5 {
		t.Fatalf("unexpected Apdex evidence: %+v", got.Evidence.Apdex)
	}
}

func TestSummarizeWindowCalculatesLatencyDistribution(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 1, P95LatencyMS: float64Pointer(10)},
		{Requests: 1, P95LatencyMS: float64Pointer(20)},
		{Requests: 1, P95LatencyMS: float64Pointer(30)},
		{Requests: 1, P95LatencyMS: float64Pointer(100)},
	}, time.Minute, 4)

	if got.P95Latency == nil || got.P95Latency.Median != 25 || got.P95Latency.Peak != 100 {
		t.Fatalf("unexpected median/peak: %+v", got.P95Latency)
	}
	if got.P95Latency.Q25 != 17.5 || got.P95Latency.Q75 != 47.5 || got.P95Latency.IQR != 30 {
		t.Fatalf("unexpected quartiles: %+v", got.P95Latency)
	}
}

func TestSummarizeWindowUsesExplicitExpectedPopulationForGaps(t *testing.T) {
	tests := []struct {
		name    string
		buckets []bucket
	}{
		{"leading gap", []bucket{{Timestamp: time.Unix(60, 0), Requests: 30}, {Timestamp: time.Unix(120, 0), Requests: 30}}},
		{"trailing gap", []bucket{{Timestamp: time.Unix(0, 0), Requests: 30}, {Timestamp: time.Unix(60, 0), Requests: 30}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeWindow(tt.buckets, time.Minute, 3)
			if got.Evidence.Requests.ObservedPoints != 2 || got.Evidence.Requests.ExpectedPoints != 3 || got.Evidence.Requests.Coverage != 2.0/3.0 {
				t.Fatalf("unexpected request evidence: %+v", got.Evidence.Requests)
			}
			if got.RequestRPM != 20 {
				t.Fatalf("request RPM = %v, want 20 over expected 3-minute duration", got.RequestRPM)
			}
		})
	}
}

func TestSummarizeWindowExcludesNonFiniteValues(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 10, Errors: 1, Apdex: float64Pointer(0.9), P95LatencyMS: float64Pointer(10)},
		{Requests: math.NaN(), Errors: math.Inf(1), Apdex: float64Pointer(math.NaN()), P95LatencyMS: float64Pointer(math.Inf(-1))},
	}, time.Minute, 2)

	if got.RequestTotal != 10 || got.ErrorTotal != 1 || got.Apdex == nil || *got.Apdex != 0.9 {
		t.Fatalf("non-finite values affected aggregates: %+v", got)
	}
	if got.P95Latency == nil || got.P95Latency.Median != 10 {
		t.Fatalf("non-finite latency affected distribution: %+v", got.P95Latency)
	}
	if got.Evidence.Requests.ExcludedValues != 1 || got.Evidence.Errors.ExcludedValues != 1 ||
		got.Evidence.Apdex.ExcludedValues != 1 || got.Evidence.P95Latency.ExcludedValues != 1 {
		t.Fatalf("missing excluded-value counts: %+v", got.Evidence)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("summary must always produce finite JSON: %v", err)
	}
	if strings.Contains(string(payload), "NaN") || strings.Contains(string(payload), "Inf") {
		t.Fatalf("non-finite JSON: %s", payload)
	}
}

func TestCompareSignalUsesRatioRelativeDelta(t *testing.T) {
	comparison := compareSignal(
		SignalDefinition{Name: "error_percentage", Unit: "percent", Aggregation: "ratio_of_totals", HigherIsWorse: true},
		WindowSummary{Value: 15, Distribution: Distribution{Q25: 14, Median: 15, Q75: 16}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
		WindowSummary{Value: 10, Distribution: Distribution{Q25: 9, Median: 10, Q75: 11}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
	)
	if comparison.RelativeDelta == nil || *comparison.RelativeDelta != 0.5 {
		t.Fatalf("relative delta = %v, want ratio 0.5", comparison.RelativeDelta)
	}
}

func TestCompareSignalOmitsRelativeDeltaForZeroBaseline(t *testing.T) {
	comparison := compareSignal(
		SignalDefinition{Name: "error_percentage", Unit: "percent", Aggregation: "ratio_of_totals", HigherIsWorse: true},
		WindowSummary{Value: 2, Distribution: Distribution{Q25: 2, Median: 2, Q75: 2}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
		WindowSummary{Value: 0, Distribution: Distribution{Q25: 0, Median: 0, Q75: 0}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
	)
	if comparison.RelativeDelta != nil {
		t.Fatalf("relative delta = %v, want absent", *comparison.RelativeDelta)
	}
}

func TestCompareSignalUsesMetricSpecificEvidence(t *testing.T) {
	definition := SignalDefinition{Name: "apdex", Unit: "score", Aggregation: "request_weighted", HigherIsWorse: false}
	current := WindowSummary{Value: 0.8, Distribution: Distribution{Q25: 0.7, Median: 0.8, Q75: 0.9}, Evidence: WindowEvidence{Selected: MetricEvidence{ObservedPoints: 3, ExpectedPoints: 4, Coverage: 0.75, ExcludedValues: 1}}}
	baseline := WindowSummary{Value: 0.9, Distribution: Distribution{Q25: 0.85, Median: 0.9, Q75: 0.95}, Evidence: WindowEvidence{Selected: completeEvidence(4)}}
	got := compareSignal(definition, current, baseline)
	if got.EvidenceQuality.Level != "sparse" || len(got.EvidenceQuality.Reasons) == 0 {
		t.Fatalf("quality = %+v", got.EvidenceQuality)
	}
}

func TestCompareSignalSelectsSummarizedMetricEvidence(t *testing.T) {
	current := summarizeWindow([]bucket{
		{Requests: 1, Apdex: float64Pointer(0.8)},
		{Requests: 1, Apdex: float64Pointer(0.81)},
		{Requests: 1, Apdex: float64Pointer(0.82)},
		{Requests: 1, Apdex: float64Pointer(0.83)},
	}, time.Minute, 4)
	baseline := summarizeWindow([]bucket{
		{Requests: 1, Apdex: float64Pointer(0.9)},
		{Requests: 1, Apdex: float64Pointer(0.91)},
		{Requests: 1, Apdex: float64Pointer(0.92)},
		{Requests: 1, Apdex: float64Pointer(0.93)},
	}, time.Minute, 4)
	current.Value, baseline.Value = *current.Apdex, *baseline.Apdex
	current.Distribution = Distribution{Q25: 0.8, Median: 0.815, Q75: 0.83}
	baseline.Distribution = Distribution{Q25: 0.9, Median: 0.915, Q75: 0.93}

	got := compareSignal(SignalDefinition{Name: "apdex", HigherIsWorse: false}, current, baseline)
	if got.EvidenceQuality.Level != "sufficient" {
		t.Fatalf("comparison did not select Apdex evidence: %+v", got.EvidenceQuality)
	}
}

func TestCompareSignalSanitizesNonFiniteOutput(t *testing.T) {
	got := compareSignal(
		SignalDefinition{Name: "p95_latency", HigherIsWorse: true},
		WindowSummary{Value: math.NaN(), Distribution: Distribution{Median: math.Inf(1)}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
		WindowSummary{Value: 1, Distribution: Distribution{Q25: 1, Median: 1, Q75: 1}, Evidence: WindowEvidence{Selected: completeEvidence(4)}},
	)
	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("comparison emitted non-finite JSON: %v", err)
	}
}

func TestCompareSignalClassifiesPresenceChanges(t *testing.T) {
	definition := SignalDefinition{Name: "apdex", HigherIsWorse: false}
	newlyObserved := compareSignal(definition,
		WindowSummary{Evidence: WindowEvidence{Selected: completeEvidence(4)}},
		WindowSummary{},
	)
	if newlyObserved.PresenceChange != "newly_observed" {
		t.Fatalf("presence change = %q", newlyObserved.PresenceChange)
	}
	noLongerObserved := compareSignal(definition,
		WindowSummary{},
		WindowSummary{Evidence: WindowEvidence{Selected: completeEvidence(4)}},
	)
	if noLongerObserved.PresenceChange != "no_longer_observed" {
		t.Fatalf("presence change = %q", noLongerObserved.PresenceChange)
	}
}

func TestCompareSignalClassifiesRegressionImprovementAndStable(t *testing.T) {
	tests := []struct {
		name          string
		definition    SignalDefinition
		currentMedian float64
		baselineQ25   float64
		baselineMed   float64
		baselineQ75   float64
		currentQ25    float64
		currentQ75    float64
		want          string
	}{
		{"higher is worse regression", SignalDefinition{HigherIsWorse: true}, 20, 8, 10, 12, 18, 22, "regression"},
		{"higher is worse improvement", SignalDefinition{HigherIsWorse: true}, 5, 8, 10, 12, 4, 6, "improvement"},
		{"lower is worse regression", SignalDefinition{HigherIsWorse: false}, 0.5, 0.8, 0.85, 0.9, 0.45, 0.55, "regression"},
		{"lower is worse improvement", SignalDefinition{HigherIsWorse: false}, 0.95, 0.8, 0.85, 0.9, 0.94, 0.96, "improvement"},
		{"overlapping distributions are stable", SignalDefinition{HigherIsWorse: true}, 11, 8, 10, 12, 9, 13, "stable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := WindowSummary{Value: tt.currentMedian, Distribution: Distribution{Q25: tt.currentQ25, Median: tt.currentMedian, Q75: tt.currentQ75}, Evidence: WindowEvidence{Selected: completeEvidence(4)}}
			baseline := WindowSummary{Value: tt.baselineMed, Distribution: Distribution{Q25: tt.baselineQ25, Median: tt.baselineMed, Q75: tt.baselineQ75}, Evidence: WindowEvidence{Selected: completeEvidence(4)}}
			if got := compareSignal(tt.definition, current, baseline); got.Classification != tt.want {
				t.Fatalf("classification = %q, want %q", got.Classification, tt.want)
			}
		})
	}
}

func TestDeviationResponseJSONContract(t *testing.T) {
	entry := LeaderboardEntry{
		ServiceName:    "checkout",
		Env:            "production",
		SignalCategory: "reliability",
		Comparison:     SignalComparison{Definition: SignalDefinition{Name: "error_percentage"}},
	}
	response := DeviationResponse{
		Scope: "fleet",
		Leaderboards: DeviationLeaderboards{
			Reliability:      SignalLeaderboard{Regressions: []LeaderboardEntry{entry}},
			Experience:       SignalLeaderboard{Improvements: []LeaderboardEntry{entry}},
			SustainedLatency: SignalLeaderboard{Regressions: []LeaderboardEntry{entry}},
		},
		TelemetryChanges: []TelemetryChange{{ServiceName: entry.ServiceName, Env: entry.Env, Change: "newly_observed"}},
		ThroughputShifts: []LeaderboardEntry{entry},
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{"service_name", "env", "reliability", "experience", "sustained_latency", "telemetry_changes", "throughput_shifts", "signal_category"} {
		if !strings.Contains(text, `"`+required+`"`) {
			t.Fatalf("response JSON missing %q: %s", required, text)
		}
	}
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"promql", "raw_query", "storage", "confidence"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("response JSON contains forbidden field %q: %s", forbidden, text)
		}
	}
	var decoded struct {
		Leaderboards struct {
			Reliability struct {
				Regressions []map[string]any `json:"regressions"`
			} `json:"reliability"`
		} `json:"leaderboards"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	first := decoded.Leaderboards.Reliability.Regressions[0]
	if first["service_name"] != "checkout" || first["env"] != "production" {
		t.Fatalf("leaderboard identity must be directly addressable: %+v", first)
	}
}
