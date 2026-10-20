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

var checkout = scopedService{name: "checkout", role: RoleChanged}

// fixedRegressions judges with no baseline band, by the fixed thresholds alone.
func fixedRegressions(baseline, after apm.GoldenSignals) []Regression {
	regressions, _ := serviceFindings(checkout, serviceWindows{baseline: baseline, after: after})
	return regressions
}

func triggers(regressions []Regression) string {
	var out []string
	for _, r := range regressions {
		out = append(out, r.trigger)
	}
	return strings.Join(out, "; ")
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
	after := withEnv(signals(1000, 0, 150), "staging", 0, 90)

	got := triggers(fixedRegressions(baseline, after))
	if !strings.Contains(got, "p95 latency rose 50% (100.0 ms → 150.0 ms) in prod") {
		t.Fatalf("regressions = %q, want prod's p95 rise", got)
	}
	compared := compareSignals("checkout", "changed", serviceWindows{baseline: baseline, after: after})
	if compared.LatencyEnv != "prod" || *compared.LatencyP95Ms.Baseline != 100 || *compared.LatencyP95Ms.After != 150 {
		t.Fatalf("compared latency = env %q, p95 %+v; want prod 100 → 150", compared.LatencyEnv, compared.LatencyP95Ms)
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
		{"error rate at threshold", signals(1000, 10, 100), signals(1000, 20, 100), MetricErrorRatePct},
		{"error rate up 30% relative", signals(1000, 100, 100), signals(1000, 130, 100), MetricErrorRatePct},
		{"p95 below threshold", signals(1000, 0, 100), signals(1000, 0, 139.9), ""},
		{"p95 at threshold", signals(1000, 0, 100), signals(1000, 0, 140), MetricLatencyP95Ms},
		{"p95 up 35%", signals(1000, 0, 100), signals(1000, 0, 135), ""},
		{"p95 up 45%", signals(1000, 0, 100), signals(1000, 0, 145), MetricLatencyP95Ms},
		{"too little baseline traffic", signals(minRequestsPerWindow-1, 0, 100), signals(1000, 500, 900), ""},
		{"too little after traffic", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow-1, 10, 900), ""},
		{"traffic at the minimum", signals(minRequestsPerWindow, 0, 100), signals(minRequestsPerWindow, 10, 100), MetricErrorRatePct},
		{"throughput drop below threshold", signals(1000, 0, 100), signals(501, 0, 100), ""},
		{"throughput drop at threshold", signals(1000, 0, 100), signals(500, 0, 100), MetricThroughputRPM},
		{"throughput drop to the minimum traffic", signals(1200, 0, 100), signals(minRequestsPerWindow, 0, 100), MetricThroughputRPM},
		{"throughput collapse below the minimum traffic", signals(1200, 0, 100), signals(5, 0, 100), MetricThroughputRPM},
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
			got := fixedRegressions(tc.baseline, tc.after)
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

// The agent explains a verdict from its reason, so each trigger names the
// threshold it crossed.
func TestRegressionsNameTheThresholdCrossed(t *testing.T) {
	cases := []struct {
		name            string
		baseline, after apm.GoldenSignals
		want            string
	}{
		{"error rate", signals(1000, 100, 100), signals(1000, 130, 100), "checkout error rate rose 3.00 pp (10.00% → 13.00%) (threshold 1 pp)"},
		{"p95", signals(1000, 0, 100), signals(1000, 0, 141), "checkout p95 latency rose 41% (100.0 ms → 141.0 ms) in prod (threshold 40%)"},
		{"throughput", signals(1200, 0, 100), signals(600, 0, 100), "checkout throughput fell 50% (20.0 → 10.0 rpm) (threshold 50%)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fixedRegressions(tc.baseline, tc.after)
			if len(got) != 1 || got[0].trigger != tc.want {
				t.Fatalf("regressions = %q, want [%q]", triggers(got), tc.want)
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

func TestReasonCountsAlertsNoLongerFiring(t *testing.T) {
	flat := signals(1000, 5, 100)
	const two = "2 alerts that started after the change were no longer firing at the end of the after window"
	cases := []struct {
		name string
		in   verdictInputs
		want string
	}{
		{"no significant change", verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, alertsNotFiring: 2},
			"no threshold crossed: error rate, p95 latency, throughput, alerts and APM deviations are within bounds; " + two},
		{"regression likely", verdictInputs{afterMinutes: 60, alertsNotFiring: 2, regressions: []Regression{{trigger: "first"}}}, "first; " + two},
		{"none", verdictInputs{afterMinutes: 60, regressions: []Regression{{trigger: "first"}}}, "first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, reason, _ := decideVerdict(tc.in); reason != tc.want {
				t.Fatalf("reason = %q, want %q", reason, tc.want)
			}
		})
	}
}

func TestVerdictStringsAreTheEvalContract(t *testing.T) {
	// last9-mcp-evals matches these strings exactly.
	if VerdictRegressionLikely != "regression likely" || VerdictPossibleChange != "possible change" || VerdictNoSignificantChange != "no significant change" || VerdictInsufficientData != "insufficient data" {
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
