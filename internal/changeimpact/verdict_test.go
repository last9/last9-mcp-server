package changeimpact

import (
	"maps"
	"strings"
	"testing"

	"last9-mcp/internal/apm"
)

func signals(requests, errors, p95 float64) apm.GoldenSignals {
	s := apm.GoldenSignals{RequestCount: requests, ErrorCount: errors, ThroughputRPM: requests / 60, RequestsByEnv: map[string]float64{"prod": requests}}
	if requests > 0 {
		rate := errors / requests * 100
		s.ErrorRatePct = &rate
	}
	if p95 > 0 {
		s.LatencyByEnv = map[string]apm.Latency{"prod": {P95Ms: &p95}}
	}
	return s
}

// withEnv adds an environment's traffic and p95 to s, as a query with no env
// returns them.
func withEnv(s apm.GoldenSignals, env string, requests, p95 float64) apm.GoldenSignals {
	s.RequestCount += requests
	s.RequestsByEnv = maps.Clone(s.RequestsByEnv)
	s.RequestsByEnv[env] = requests
	s.LatencyByEnv = maps.Clone(s.LatencyByEnv)
	if s.LatencyByEnv == nil {
		s.LatencyByEnv = map[string]apm.Latency{}
	}
	s.LatencyByEnv[env] = apm.Latency{P95Ms: &p95}
	return s
}

// withTraffic adds an environment's requests to s without any latency.
func withTraffic(s apm.GoldenSignals, env string, requests float64) apm.GoldenSignals {
	s.RequestCount += requests
	s.RequestsByEnv = maps.Clone(s.RequestsByEnv)
	s.RequestsByEnv[env] = requests
	return s
}

// Without an env, both windows must compare one environment's latency: the
// busiest. Picking the slowest per window compared staging's baseline against
// prod's after window, and hid prod's own rise.
func TestLatencyComparesOneEnvironmentAcrossWindows(t *testing.T) {
	baseline := withEnv(signals(1000, 0, 100), "staging", 10, 1500)
	after := withEnv(signals(1000, 0, 130), "staging", 0, 90)

	got := strings.Join(serviceRegressions("checkout", baseline, after), "; ")
	if !strings.Contains(got, "p95 latency rose 30% (100.0 ms → 130.0 ms) in prod") {
		t.Fatalf("regressions = %q, want prod's p95 rise", got)
	}
	compared := compareSignals("checkout", "changed", baseline, after)
	if compared.LatencyEnv != "prod" || *compared.LatencyP95Ms.Baseline != 100 || *compared.LatencyP95Ms.After != 130 {
		t.Fatalf("compared latency = env %q, p95 %+v; want prod 100 → 130", compared.LatencyEnv, compared.LatencyP95Ms)
	}
}

func TestLatencyEnvPrefersTheBusiestThenTheName(t *testing.T) {
	cases := []struct {
		name            string
		baseline, after apm.GoldenSignals
		want            string
	}{
		{"busiest over both windows", withEnv(signals(100, 0, 100), "eu", 60, 100), withEnv(signals(10, 0, 100), "eu", 60, 100), "eu"},
		{"tie by name", withEnv(signals(100, 0, 100), "eu", 100, 100), signals(0, 0, 0), "eu"},
		{"busy env without latency is skipped", withEnv(signals(1000, 0, 0), "eu", 1, 100), signals(1000, 0, 0), "eu"},
		{"no latency anywhere", signals(1000, 0, 0), signals(1000, 0, 0), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := latencyEnv(tc.baseline, tc.after); got != tc.want {
				t.Fatalf("latencyEnv = %q, want %q", got, tc.want)
			}
		})
	}
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
		{"too little after traffic", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow-1, 10, 900), ""},
		{"traffic at the minimum", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow, 10, 100), "error rate"},
		{"throughput drop below threshold", signals(1000, 0, 100), signals(501, 0, 100), ""},
		{"throughput drop at threshold", signals(1000, 0, 100), signals(500, 0, 100), "throughput fell"},
		{"throughput drop to the minimum traffic", signals(1200, 0, 100), signals(minRequestsPerWindow, 0, 100), "throughput fell"},
		{"throughput collapse below the minimum traffic", signals(1200, 0, 100), signals(5, 0, 100), "throughput fell"},
		{"throughput to zero", signals(1200, 0, 100), signals(0, 0, 0), ""},
		// A p95 over a few dozen requests rests on its slowest one or two.
		{"thin baseline p95 spike", signals(73, 0, 100), signals(1000, 0, 493), ""},
		{"thin baseline error spike", signals(73, 0, 100), signals(1000, 50, 100), ""},
		{"latency env thinner than the service", withTraffic(signals(60, 0, 100), "eu", 60), withTraffic(signals(60, 0, 200), "eu", 60), ""},
		{"throughput rise", signals(500, 0, 100), signals(5000, 0, 100), ""},
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
		{"trigger despite missing golden signals", verdictInputs{afterMinutes: 60, unavailable: []string{"golden signals unavailable: boom"}, triggers: []string{"new alert"}}, VerdictRegressionLikely},
		{"golden signals failed", verdictInputs{afterMinutes: 60, unavailable: []string{"golden signals unavailable: boom"}}, VerdictInsufficientData},
		{"flat but a section failed", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, unavailable: []string{"alerts unavailable: boom"}}, VerdictInsufficientData},
		{"no after traffic", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat}, VerdictInsufficientData},
		{"thin baseline", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: signals(73, 0, 100), changedAfter: flat}, VerdictInsufficientData},
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
	section, triggers := summarizeAPMDeviations([]apm.LeaderboardEntry{entry("apdex", "improvement"), entry("error_percentage", "regression")})
	if len(section.Items) != 2 || len(triggers) != 1 || !strings.Contains(triggers[0], "error_percentage") {
		t.Fatalf("items = %+v, triggers = %v; want both items and only the regression as a trigger", section.Items, triggers)
	}
	flat := signals(1000, 5, 100)
	verdict, reason, _ := decideVerdict(verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, triggers: triggers})
	if verdict != VerdictRegressionLikely || !strings.Contains(reason, "get_apm_service_deviations") {
		t.Fatalf("verdict = %q (%s), want a regression citing get_apm_service_deviations", verdict, reason)
	}
}
