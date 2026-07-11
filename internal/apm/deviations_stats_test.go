package apm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSummarizeWindowUsesRatioOfTotals(t *testing.T) {
	got := summarizeWindow([]bucket{{Requests: 1, Errors: 1}, {Requests: 999, Errors: 0}}, time.Minute)
	if got.ErrorPercentage != 0.1 {
		t.Fatalf("error percentage = %v, want 0.1", got.ErrorPercentage)
	}
	if got.RequestRPM != 500 || got.ErrorRPM != 0.5 {
		t.Fatalf("unexpected rates: requests=%v errors=%v", got.RequestRPM, got.ErrorRPM)
	}
}

func TestSummarizeWindowUsesRequestWeightedApdex(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 1, Apdex: 1},
		{Requests: 99, Apdex: 0.5},
	}, time.Minute)
	if got.Apdex != 0.505 {
		t.Fatalf("weighted Apdex = %v, want 0.505", got.Apdex)
	}
}

func TestSummarizeWindowCalculatesLatencyDistribution(t *testing.T) {
	got := summarizeWindow([]bucket{
		{Requests: 1, P95LatencyMS: 10},
		{Requests: 1, P95LatencyMS: 20},
		{Requests: 1, P95LatencyMS: 30},
		{Requests: 1, P95LatencyMS: 100},
	}, time.Minute)

	if got.P95Latency.Median != 25 || got.P95Latency.Peak != 100 {
		t.Fatalf("unexpected median/peak: %+v", got.P95Latency)
	}
	if got.P95Latency.Q25 != 17.5 || got.P95Latency.Q75 != 47.5 || got.P95Latency.IQR != 30 {
		t.Fatalf("unexpected quartiles: %+v", got.P95Latency)
	}
}

func TestSummarizeWindowReportsCountsAndCoverage(t *testing.T) {
	start := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	got := summarizeWindow([]bucket{
		{Timestamp: start, Requests: 1},
		{Timestamp: start.Add(time.Minute), Requests: 1},
		{Timestamp: start.Add(3 * time.Minute), Requests: 1},
	}, time.Minute)

	if got.ObservedPoints != 3 || got.ExpectedPoints != 4 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.Coverage != 0.75 {
		t.Fatalf("coverage = %v", got.Coverage)
	}
}

func TestCompareSignalOmitsRelativeDeltaForZeroBaseline(t *testing.T) {
	comparison := compareSignal(
		SignalDefinition{Name: "error_percentage", Unit: "percent", Aggregation: "ratio_of_totals", HigherIsWorse: true},
		WindowSummary{Value: 2, ObservedPoints: 4, ExpectedPoints: 4, Distribution: Distribution{Q25: 2, Median: 2, Q75: 2}},
		WindowSummary{Value: 0, ObservedPoints: 4, ExpectedPoints: 4, Distribution: Distribution{Q25: 0, Median: 0, Q75: 0}},
	)
	if comparison.RelativeDelta != nil {
		t.Fatalf("relative delta = %v, want absent", *comparison.RelativeDelta)
	}
	if comparison.AbsoluteDelta != 2 {
		t.Fatalf("absolute delta = %v", comparison.AbsoluteDelta)
	}
}

func TestCompareSignalClassifiesEvidenceQuality(t *testing.T) {
	definition := SignalDefinition{Name: "p95_latency", Unit: "milliseconds", Aggregation: "median", HigherIsWorse: true}
	tests := []struct {
		name     string
		current  WindowSummary
		baseline WindowSummary
		want     string
	}{
		{
			name:     "non comparable when current has no series",
			current:  WindowSummary{},
			baseline: WindowSummary{ObservedPoints: 4, ExpectedPoints: 4},
			want:     "non_comparable",
		},
		{
			name:     "sparse with fewer than four observations",
			current:  WindowSummary{ObservedPoints: 3, ExpectedPoints: 3},
			baseline: WindowSummary{ObservedPoints: 3, ExpectedPoints: 3},
			want:     "sparse",
		},
		{
			name:     "sparse with incomplete coverage",
			current:  WindowSummary{ObservedPoints: 4, ExpectedPoints: 5},
			baseline: WindowSummary{ObservedPoints: 5, ExpectedPoints: 5},
			want:     "sparse",
		},
		{
			name:     "sufficient with complete aligned observations",
			current:  WindowSummary{ObservedPoints: 4, ExpectedPoints: 4},
			baseline: WindowSummary{ObservedPoints: 4, ExpectedPoints: 4},
			want:     "sufficient",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareSignal(definition, tt.current, tt.baseline)
			if got.EvidenceQuality.Level != tt.want {
				t.Fatalf("quality = %+v, want %q", got.EvidenceQuality, tt.want)
			}
			if len(got.EvidenceQuality.Reasons) == 0 {
				t.Fatal("expected machine-readable evidence reason")
			}
		})
	}
}

func TestCompareSignalClassifiesPresenceChanges(t *testing.T) {
	definition := SignalDefinition{Name: "apdex", Unit: "score", Aggregation: "request_weighted", HigherIsWorse: false}
	newlyObserved := compareSignal(definition,
		WindowSummary{ObservedPoints: 4, ExpectedPoints: 4},
		WindowSummary{},
	)
	if newlyObserved.PresenceChange != "newly_observed" {
		t.Fatalf("presence change = %q", newlyObserved.PresenceChange)
	}

	noLongerObserved := compareSignal(definition,
		WindowSummary{},
		WindowSummary{ObservedPoints: 4, ExpectedPoints: 4},
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
			current := WindowSummary{
				Value:          tt.currentMedian,
				ObservedPoints: 4,
				ExpectedPoints: 4,
				Distribution:   Distribution{Q25: tt.currentQ25, Median: tt.currentMedian, Q75: tt.currentQ75},
			}
			baseline := WindowSummary{
				Value:          tt.baselineMed,
				ObservedPoints: 4,
				ExpectedPoints: 4,
				Distribution:   Distribution{Q25: tt.baselineQ25, Median: tt.baselineMed, Q75: tt.baselineQ75},
			}
			got := compareSignal(tt.definition, current, baseline)
			if got.Classification != tt.want {
				t.Fatalf("classification = %q, want %q", got.Classification, tt.want)
			}
		})
	}
}

func TestDeviationEvidenceJSONHasNoRawQueryOrNumericConfidence(t *testing.T) {
	value := 1.0
	payload, err := json.Marshal(SignalComparison{
		Definition:      SignalDefinition{Name: "requests", Unit: "requests_per_minute", Aggregation: "mean_rate"},
		Current:         WindowSummary{Value: 2, ObservedPoints: 4, ExpectedPoints: 4, Coverage: 1},
		Baseline:        WindowSummary{Value: 1, ObservedPoints: 4, ExpectedPoints: 4, Coverage: 1},
		AbsoluteDelta:   1,
		RelativeDelta:   &value,
		EvidenceQuality: EvidenceQuality{Level: "sufficient", Reasons: []string{"complete_aligned_coverage"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(payload))
	for _, forbidden := range []string{"promql", "raw_query", "storage", "confidence"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("response JSON contains forbidden field %q: %s", forbidden, payload)
		}
	}
}
