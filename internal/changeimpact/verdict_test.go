package changeimpact

import (
	"strings"
	"testing"

	"last9-mcp/internal/apm"
)

func signals(requests, errors, p95 float64) apm.GoldenSignals {
	s := apm.GoldenSignals{RequestCount: requests, ErrorCount: errors, ThroughputRPM: requests / 60}
	if requests > 0 {
		rate := errors / requests * 100
		s.ErrorRatePct = &rate
	}
	if p95 > 0 {
		s.LatencyP95Ms = &p95
	}
	return s
}

// Each golden signal is tested just under, at and just over its threshold.
func TestServiceRegressionThresholds(t *testing.T) {
	cases := []struct {
		name            string
		baseline, after apm.GoldenSignals
		want            string
	}{
		{"error rate below threshold", signals(1000, 10, 100), signals(1000, 19.9, 100), ""},
		{"error rate at threshold", signals(1000, 10, 100), signals(1000, 20, 100), MetricErrorRatePct},
		{"error rate above threshold", signals(1000, 10, 100), signals(1000, 20.1, 100), MetricErrorRatePct},
		{"p95 below threshold", signals(1000, 0, 100), signals(1000, 0, 124.9), ""},
		{"p95 at threshold", signals(1000, 0, 100), signals(1000, 0, 125), MetricLatencyP95Ms},
		{"p95 above threshold", signals(1000, 0, 100), signals(1000, 0, 125.1), MetricLatencyP95Ms},
		{"too little baseline traffic", signals(minRequestsPerWindow-1, 0, 100), signals(1000, 500, 900), ""},
		{"too little after traffic", signals(1000, 0, 100), signals(minRequestsPerWindow-1, 10, 900), ""},
		{"traffic at the minimum", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow, 10, 100), MetricErrorRatePct},
		{"throughput drop below threshold", signals(1000, 0, 100), signals(501, 0, 100), ""},
		{"throughput drop at threshold", signals(1000, 0, 100), signals(500, 0, 100), MetricThroughputRPM},
		{"throughput drop above threshold", signals(1000, 0, 100), signals(499, 0, 100), MetricThroughputRPM},
		{"throughput drop to the minimum traffic", signals(1200, 0, 100), signals(minRequestsPerWindow, 0, 100), MetricThroughputRPM},
		{"throughput rise", signals(500, 0, 100), signals(5000, 0, 100), ""},
		{"no baseline p95", signals(1000, 0, 0), signals(1000, 0, 900), ""},
		{"improvement", signals(1000, 50, 300), signals(1000, 0, 100), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceRegressions(scopedService{name: "checkout", role: RoleChanged}, tc.baseline, tc.after)
			var metrics []string
			for _, r := range got {
				metrics = append(metrics, r.Metric)
			}
			if tc.want == "" && len(got) != 0 {
				t.Fatalf("got %v, want no regression", metrics)
			}
			if tc.want != "" && (len(got) != 1 || got[0].Metric != tc.want) {
				t.Fatalf("got %v, want only %q", metrics, tc.want)
			}
		})
	}
}

func TestDecideVerdictPrecedence(t *testing.T) {
	flat := signals(1000, 5, 100)
	cases := []struct {
		name string
		in   verdictInputs
		want string
	}{
		{"short after window wins", verdictInputs{afterMinutes: minAfterMinutes - 1, regressions: []Regression{{trigger: "x"}}}, VerdictInsufficientData},
		{"any trigger", verdictInputs{afterMinutes: minAfterMinutes, haveChanged: true, changedBaseline: flat, changedAfter: flat, regressions: []Regression{{trigger: "new alert"}}}, VerdictRegressionLikely},
		{"trigger despite missing golden signals", verdictInputs{afterMinutes: 60, unavailable: []string{"golden signals unavailable: boom"}, regressions: []Regression{{trigger: "new alert"}}}, VerdictRegressionLikely},
		{"golden signals failed", verdictInputs{afterMinutes: 60, unavailable: []string{"golden signals unavailable: boom"}}, VerdictInsufficientData},
		{"flat but a section failed", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, unavailable: []string{"alerts unavailable: boom"}}, VerdictInsufficientData},
		{"no after traffic", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat}, VerdictInsufficientData},
		{"flat", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat}, VerdictNoSignificantChange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, reason, note := decideVerdict(tc.in)
			if verdict != tc.want {
				t.Fatalf("verdict = %q (%s), want %q", verdict, reason, tc.want)
			}
			if reason == "" {
				t.Fatal("every verdict needs a reason")
			}
			if (note != "") != (verdict == VerdictNoSignificantChange) {
				t.Fatalf("note = %q for verdict %q", note, verdict)
			}
		})
	}
}

func TestVerdictStringsAreTheEvalContract(t *testing.T) {
	// last9-mcp-evals matches these strings exactly.
	if VerdictRegressionLikely != "regression likely" || VerdictNoSignificantChange != "no significant change" || VerdictInsufficientData != "insufficient data" {
		t.Fatal("verdict strings changed")
	}
}

func TestAPMDeviationRegressionAloneIsRegressionLikely(t *testing.T) {
	entry := func(signal, classification string) apm.LeaderboardEntry {
		return apm.LeaderboardEntry{ServiceName: "checkout", Env: "prod", Comparison: apm.SignalComparison{
			Definition: apm.SignalDefinition{Name: signal}, Classification: classification,
		}}
	}
	section, regressions := summarizeAPMDeviations([]apm.LeaderboardEntry{entry("apdex", "improvement"), entry("error_percentage", "regression")}, "checkout")
	if len(section.Items) != 2 || len(regressions) != 1 || !strings.Contains(regressions[0].trigger, "error_percentage") {
		t.Fatalf("items = %+v, regressions = %+v; want both items and only the regression as a trigger", section.Items, regressions)
	}
	flat := signals(1000, 5, 100)
	verdict, reason, _ := decideVerdict(verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, regressions: regressions})
	if verdict != VerdictRegressionLikely || !strings.Contains(reason, "get_apm_service_deviations") {
		t.Fatalf("verdict = %q (%s), want a regression citing get_apm_service_deviations", verdict, reason)
	}
}
