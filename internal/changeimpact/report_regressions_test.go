package changeimpact

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/models"
)

// last9/ai matches these strings exactly when it files one ticket per regression.
func TestRegressionMetricsAreTheAutoFileContract(t *testing.T) {
	got := []string{MetricErrorRatePct, MetricLatencyP95Ms, MetricThroughputRPM, MetricAlert, MetricAPMDeviation}
	want := []string{"error_rate_pct", "latency_p95_ms", "throughput_rpm", "alert", "apm_deviation"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	if RoleChanged != "changed" || RoleDependent != "dependent" {
		t.Fatalf("roles = %q, %q", RoleChanged, RoleDependent)
	}
}

type plantedRegression struct {
	errorRate, p95, throughput, alert, dependentP95, apmP95 bool
	// everyEnv requests no env, so counts sum over every environment.
	everyEnv bool
}

// analyzePlanted runs Analyze against a fake that moves only the planted
// signals past their thresholds, the changed service being checkout.
func analyzePlanted(t *testing.T, p plantedRegression) Report {
	t.Helper()
	change := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)
	after := flatWindow()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after, callers: map[string]float64{"web": 10}}
	if p.errorRate {
		after.errors["checkout"] = 60
	}
	if p.p95 {
		after.p95["checkout"] = 180
	}
	if p.throughput {
		after.requests["checkout"] = 300
		if !p.errorRate {
			// Keeps the error rate from rising with fewer requests.
			after.errors["checkout"] = 0
		}
	}
	if p.dependentP95 {
		after.p95["web"] = 200
	}
	if p.alert {
		// Resolved by the end of the after window, as most deploy alerts are:
		// the item says so, so a filer can skip it.
		f.alerts = []alerting.AlertRuleData{{RuleID: "r1", RuleName: "checkout errors", AlertGroupName: "checkout",
			Alerts: []alerting.AlertInstance{{Since: change.Add(10 * time.Minute).Unix(), State: "normal", GroupLabels: map[string]interface{}{"env": "prod"}}}}}
	}
	rollup := apmRollup{}
	if p.apmP95 {
		rollup.p95Ms = map[string]float64{"baseline": 100, "after": 200}
	}
	env := "prod"
	if p.everyEnv {
		env = ""
	}
	srv := apmRollupServer(t, f, rollup)
	return Analyze(context.Background(), srv.Client(), testConfig(srv.URL),
		Request{Service: "checkout", Env: env, Change: change, Now: change.Add(2 * time.Hour), BaselineMinutes: 60, AfterMinutes: 60})
}

func TestRegressionsListEveryTriggerInReasonOrder(t *testing.T) {
	errorRate := Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricErrorRatePct, Baseline: ptr(0.5), After: ptr(5)}
	p95 := Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricLatencyP95Ms, Baseline: ptr(100), After: ptr(180)}
	throughput := Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricThroughputRPM, Baseline: ptr(20), After: ptr(5)}
	alert := Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricAlert, RuleID: "r1", State: "normal"}
	dependentP95 := Regression{Service: "web", Role: RoleDependent, Env: "prod", Metric: MetricLatencyP95Ms, Baseline: ptr(80), After: ptr(200)}
	apmP95 := Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricAPMDeviation, Baseline: ptr(100), After: ptr(200)}
	everyEnvErrorRate := errorRate
	everyEnvErrorRate.Env = ""
	cases := []struct {
		name    string
		planted plantedRegression
		want    []Regression
	}{
		{"error rate", plantedRegression{errorRate: true}, []Regression{errorRate}},
		{"p95 latency", plantedRegression{p95: true}, []Regression{p95}},
		{"throughput", plantedRegression{throughput: true}, []Regression{throughput}},
		{"alert", plantedRegression{alert: true}, []Regression{alert}},
		{"dependent p95 latency", plantedRegression{dependentP95: true}, []Regression{dependentP95}},
		{"APM deviation", plantedRegression{apmP95: true}, []Regression{apmP95}},
		// Counts sum over every env, while p95 and alerts name the env they came from.
		{"no env requested", plantedRegression{everyEnv: true, errorRate: true, p95: true, alert: true}, []Regression{everyEnvErrorRate, p95, alert}},
		{
			// 60 errors in 300 requests: the error rate rises to 20%.
			"all together",
			plantedRegression{errorRate: true, p95: true, throughput: true, alert: true, dependentP95: true, apmP95: true},
			[]Regression{{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricErrorRatePct, Baseline: ptr(0.5), After: ptr(20)}, p95, throughput, alert, dependentP95, apmP95},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := analyzePlanted(t, tc.planted)
			if report.Verdict != VerdictRegressionLikely {
				t.Fatalf("verdict = %q (%s)", report.Verdict, report.Reason)
			}
			assertRegressionsMatchReason(t, report)
			if len(report.Regressions) != len(tc.want) {
				t.Fatalf("regressions = %+v, want %d", report.Regressions, len(tc.want))
			}
			for i, want := range tc.want {
				assertRegression(t, report.Regressions[i], want)
			}
			if !strings.HasPrefix(report.Reason, report.Regressions[0].trigger) {
				t.Fatalf("reason %q does not lead with the first regression %q", report.Reason, report.Regressions[0].trigger)
			}
		})
	}
}

var moreTriggers = regexp.MustCompile(`\(\+(\d+) more\)`)

// assertRegressionsMatchReason checks that regressions[] lists exactly the
// triggers reason counts.
func assertRegressionsMatchReason(t *testing.T, r Report) {
	t.Helper()
	want := 1
	if m := moreTriggers.FindStringSubmatch(r.Reason); m != nil {
		extra, _ := strconv.Atoi(m[1])
		want += extra
	}
	if len(r.Regressions) != want {
		t.Fatalf("reason %q counts %d triggers, regressions has %d: %+v", r.Reason, want, len(r.Regressions), r.Regressions)
	}
}

func TestRegressionsAreAnEmptyArrayUnlessRegressionLikely(t *testing.T) {
	change := recentChange()
	cases := []struct {
		name    string
		fake    *fakeLast9
		args    func(AnalyzeChangeImpactArgs) AnalyzeChangeImpactArgs
		verdict string
	}{
		{"flat", &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}, nil, VerdictNoSignificantChange},
		{"no after traffic", &fakeLast9{change: change, baseline: flatWindow(), after: windowData{}}, nil, VerdictInsufficientData},
		{"golden signals failed", &fakeLast9{change: change, fail: map[string]int{"prom": 502}}, nil, VerdictInsufficientData},
		{
			"after window too short to query",
			&fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()},
			func(a AnalyzeChangeImpactArgs) AnalyzeChangeImpactArgs {
				a.Timestamp = time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
				return a
			},
			VerdictInsufficientData,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := serviceArgs(change)
			if tc.args != nil {
				args = tc.args(args)
			}
			text, err := runToolText(t, tc.fake, args, func(*models.Config) {})
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(text), &raw); err != nil {
				t.Fatal(err)
			}
			var verdict string
			_ = json.Unmarshal(raw["verdict"], &verdict)
			if verdict != tc.verdict {
				t.Fatalf("verdict = %q, want %q\n%s", verdict, tc.verdict, text)
			}
			if got := string(raw["regressions"]); got != "[]" {
				t.Fatalf("regressions = %s, want []", got)
			}
		})
	}
}

// No endpoint: every regression is service-wide, which a filer keys as '*'.
// Only alerts carry a rule id and state.
func TestRegressionsSerializeEveryField(t *testing.T) {
	cases := []struct {
		name string
		in   Regression
		want string
	}{
		{
			"alert",
			Regression{Service: "checkout", Role: RoleChanged, Env: "prod", Metric: MetricAlert, RuleID: "r1", State: "firing", trigger: "x"},
			`{"service":"checkout","role":"changed","env":"prod","metric":"alert","rule_id":"r1","state":"firing","baseline":null,"after":null}`,
		},
		{
			"signal",
			Regression{Service: "checkout", Role: RoleChanged, Metric: MetricErrorRatePct, Baseline: ptr(1), After: ptr(6), trigger: "x"},
			`{"service":"checkout","role":"changed","env":"","metric":"error_rate_pct","baseline":1,"after":6}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.want {
				t.Fatalf("got %s, want %s", raw, tc.want)
			}
		})
	}
}
