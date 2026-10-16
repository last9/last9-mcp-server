package changeimpact

import (
	"strings"
	"testing"

	"last9-mcp/internal/apm"
)

func signals(requests, errors, p95 float64) apm.GoldenSignals {
	s := apm.GoldenSignals{RequestCount: requests, ErrorCount: errors}
	if requests > 0 {
		rate := errors / requests * 100
		s.ErrorRatePct = &rate
	}
	if p95 > 0 {
		s.LatencyP95Ms = &p95
	}
	return s
}

func TestServiceRegressionThresholds(t *testing.T) {
	cases := []struct {
		name            string
		baseline, after apm.GoldenSignals
		want            string
	}{
		{"error rate below threshold", signals(1000, 10, 100), signals(1000, 19.9, 100), ""},
		{"error rate at threshold", signals(1000, 10, 100), signals(1000, 20, 100), "error rate"},
		{"p95 below threshold", signals(1000, 0, 100), signals(1000, 0, 124.9), ""},
		{"p95 at threshold", signals(1000, 0, 100), signals(1000, 0, 125), "p95"},
		{"too little baseline traffic", signals(minRequestsPerWindow-1, 0, 100), signals(1000, 500, 900), ""},
		{"too little after traffic", signals(1000, 0, 100), signals(minRequestsPerWindow-1, 10, 900), ""},
		{"traffic at the minimum", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow, 10, 100), "error rate"},
		{"no baseline p95", signals(1000, 0, 0), signals(1000, 0, 900), ""},
		{"improvement", signals(1000, 50, 300), signals(1000, 0, 100), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(serviceRegressions("checkout", tc.baseline, tc.after), "; ")
			if tc.want == "" && got != "" {
				t.Fatalf("got %q, want no regression", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want it to mention %q", got, tc.want)
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
		{"short after window wins", verdictInputs{afterMinutes: minAfterMinutes - 1, triggers: []string{"x"}}, VerdictInsufficientData},
		{"any trigger", verdictInputs{afterMinutes: minAfterMinutes, haveChanged: true, changedBaseline: flat, changedAfter: flat, triggers: []string{"new alert"}}, VerdictRegressionLikely},
		{"trigger despite missing golden signals", verdictInputs{afterMinutes: 60, goldenErr: "boom", triggers: []string{"new alert"}}, VerdictRegressionLikely},
		{"golden signals failed", verdictInputs{afterMinutes: 60, goldenErr: "boom"}, VerdictInsufficientData},
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
