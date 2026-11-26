package changeimpact

import (
	"math"
	"testing"

	"last9-mcp/internal/apm"
)

func ptr(v float64) *float64 { return &v }

func apmEntry(service, signal, classification string, baseline, after float64) apm.LeaderboardEntry {
	return apm.LeaderboardEntry{ServiceName: service, Env: "prod", Comparison: apm.SignalComparison{
		Definition:     apm.SignalDefinition{Name: signal},
		Classification: classification,
		Baseline:       apm.WindowSummary{Value: baseline},
		Current:        apm.WindowSummary{Value: after},
	}}
}

// Each trigger carries both the structured regression and the text reason
// renders, built together so they cannot drift.
func TestRegressionsCarryValuesAndReasonText(t *testing.T) {
	changed := scopedService{name: "checkout", role: RoleChanged}
	inProd := scopedService{name: "checkout", role: RoleChanged, env: "prod"}
	web := scopedService{name: "web", role: RoleDependent}
	only := func(t *testing.T, rs []Regression) Regression {
		t.Helper()
		if len(rs) != 1 {
			t.Fatalf("got %d regressions %+v, want 1", len(rs), rs)
		}
		return rs[0]
	}
	_, apmRegressions := summarizeAPMDeviations([]apm.LeaderboardEntry{apmEntry("checkout", "error_percentage", "regression", 0.5, 4.25)}, "checkout")
	cases := []struct {
		name        string
		got         Regression
		want        Regression
		wantTrigger string
	}{
		{
			name:        "error rate over every env",
			got:         only(t, serviceRegressions(changed, signals(1000, 10, 100), signals(1000, 60, 100))),
			want:        Regression{Service: "checkout", Role: RoleChanged, Metric: MetricErrorRatePct, Baseline: ptr(1), After: ptr(6)},
			wantTrigger: "checkout error rate rose 5.00 pp (1.00% → 6.00%) (threshold 1 pp)",
		},
		{
			name:        "error rate in the requested env",
			got:         only(t, serviceRegressions(inProd, signals(1000, 10, 100), signals(1000, 60, 100))),
			want:        Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricErrorRatePct, Baseline: ptr(1), After: ptr(6)},
			wantTrigger: "checkout error rate rose 5.00 pp (1.00% → 6.00%) (threshold 1 pp)",
		},
		{
			name:        "p95 latency of a dependent, in its latency env",
			got:         only(t, serviceRegressions(web, signals(1000, 0, 100), signals(1000, 0, 180))),
			want:        Regression{Service: "web", Role: RoleDependent, Env: "prod", Metric: MetricLatencyP95Ms, Baseline: ptr(100), After: ptr(180)},
			wantTrigger: "web p95 latency rose 80% (100.0 ms → 180.0 ms) in prod (threshold 40%)",
		},
		{
			name:        "throughput",
			got:         only(t, serviceRegressions(changed, signals(1200, 0, 100), signals(300, 0, 100))),
			want:        Regression{Service: "checkout", Role: RoleChanged, Metric: MetricThroughputRPM, Baseline: ptr(20), After: ptr(5)},
			wantTrigger: "checkout throughput fell 75% (20.0 → 5.0 rpm) (threshold 50%)",
		},
		{
			name:        "alert on the changed service",
			got:         alertRegression(StartedAlert{RuleID: "r1", RuleName: "checkout errors", State: "firing", Service: "checkout", StartedAt: "2026-10-17T06:10:00Z", EnvMatch: envMatchExact, env: "prod"}, "checkout"),
			want:        Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricAlert, RuleID: "r1", State: "firing"},
			wantTrigger: `alert "checkout errors" started firing for checkout at 2026-10-17T06:10:00Z`,
		},
		{
			name:        "unlabelled alert on a dependent, named by rule id",
			got:         alertRegression(StartedAlert{RuleID: "r2", State: "firing", Service: "web", StartedAt: "2026-10-17T06:15:00Z", EnvMatch: envMatchUnlabelled}, "checkout"),
			want:        Regression{Service: "web", Role: RoleDependent, Metric: MetricAlert, RuleID: "r2", State: "firing"},
			wantTrigger: `alert "r2" started firing for web at 2026-10-17T06:15:00Z (no env label)`,
		},
		{
			name:        "APM deviation",
			got:         only(t, apmRegressions),
			want:        Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricAPMDeviation, Baseline: ptr(0.5), After: ptr(4.25)},
			wantTrigger: "get_apm_service_deviations classifies checkout error_percentage as a regression",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.trigger != tc.wantTrigger {
				t.Errorf("trigger = %q, want %q", tc.got.trigger, tc.wantTrigger)
			}
			assertRegression(t, tc.got, tc.want)
		})
	}
}

func assertRegression(t *testing.T, got, want Regression) {
	t.Helper()
	if got.Service != want.Service || got.Role != want.Role || got.Env != want.Env || got.Metric != want.Metric ||
		got.RuleID != want.RuleID || got.State != want.State {
		t.Errorf("got %+v, want %+v", got, want)
	}
	for _, v := range []struct {
		name      string
		got, want *float64
	}{{"baseline", got.Baseline, want.Baseline}, {"after", got.After, want.After}} {
		switch {
		case v.want == nil && v.got != nil:
			t.Errorf("%s = %v, want null", v.name, *v.got)
		case v.want != nil && (v.got == nil || *v.got != *v.want):
			t.Errorf("%s = %v, want %v", v.name, v.got, *v.want)
		}
	}
}

func TestAPMRegressionRoleAndMissingValues(t *testing.T) {
	_, got := summarizeAPMDeviations([]apm.LeaderboardEntry{
		apmEntry("Checkout", "p95_latency", "regression", math.NaN(), math.Inf(1)),
		apmEntry("web", "apdex", "regression", 0.9, 0.7),
		apmEntry("checkout", "request_rpm", "shift", 100, 10),
	}, "checkout")
	if len(got) != 2 {
		t.Fatalf("got %+v, want the two regressions only", got)
	}
	assertRegression(t, got[0], Regression{Service: "Checkout", Role: RoleChanged, Env: "prod", Metric: MetricAPMDeviation})
	assertRegression(t, got[1], Regression{Service: "web", Role: RoleDependent, Env: "prod", Metric: MetricAPMDeviation, Baseline: ptr(0.9), After: ptr(0.7)})
}

func TestReasonNamesTheFirstRegressionAndCountsTheRest(t *testing.T) {
	_, reason, _ := decideVerdict(verdictInputs{afterMinutes: 60, regressions: []Regression{{trigger: "first"}, {trigger: "second"}, {trigger: "third"}}})
	if reason != "first (+2 more)" {
		t.Fatalf("reason = %q", reason)
	}
}
