package changeimpact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/telemetry/traces"
)

func TestReportThresholdsMirrorVerdictConstants(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"min_after_minutes":        minAfterMinutes,
		"min_requests_per_window":  minRequestsPerWindow,
		"error_rate_increase_pp":   errorRateIncreasePP,
		"p95_latency_increase_pct": p95LatencyIncreasePct,
		"throughput_drop_pct":      throughputDropPct,
	}
	if len(report.Thresholds) != len(want) {
		t.Fatalf("thresholds = %v, want %v", report.Thresholds, want)
	}
	for k, v := range want {
		if got, ok := report.Thresholds[k]; !ok || got != v {
			t.Fatalf("thresholds[%q] = %v (ok=%v), want %v; full=%v", k, got, ok, v, report.Thresholds)
		}
	}
}

func TestPlantedRegressionIsRegressionLikely(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.errors = map[string]float64{"checkout": 60, "web": 3}
	after.p95 = map[string]float64{"checkout": 180, "web": 80}
	f := &fakeLast9{
		change: change, baseline: flatWindow(), after: after,
		callers: map[string]float64{"web": 10},
		alerts: []alerting.AlertRuleData{{
			RuleID: "r1", RuleName: "checkout error rate", AlertGroupName: "checkout", Severity: "breach",
			Alerts: []alerting.AlertInstance{{State: "firing", Since: change.Add(10 * time.Minute).Unix()}},
		}},
	}

	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely {
		t.Fatalf("verdict = %q (%s), want %q", report.Verdict, report.Reason, VerdictRegressionLikely)
	}
	assertRegressionsMatchReason(t, report)
	if !strings.Contains(report.Reason, "checkout") || !strings.Contains(report.Reason, "error rate") {
		t.Fatalf("reason %q should name the service and the error-rate rise", report.Reason)
	}
	checkout := signalsFor(t, report, "checkout")
	if checkout.Role != "changed" || checkout.ErrorRatePct.DeltaPP == nil || *checkout.ErrorRatePct.DeltaPP != 4.5 {
		t.Fatalf("checkout error rate = %+v", checkout.ErrorRatePct)
	}
	if checkout.LatencyP95Ms.DeltaPct == nil || *checkout.LatencyP95Ms.DeltaPct != 80 {
		t.Fatalf("checkout p95 = %+v", checkout.LatencyP95Ms)
	}
	if web := signalsFor(t, report, "web"); web.Role != "dependent" {
		t.Fatalf("web role = %q", web.Role)
	}
	if len(report.Alerts.Started) != 1 || report.Alerts.Started[0].RuleID != "r1" || report.Alerts.Started[0].Service != "checkout" {
		t.Fatalf("alerts = %+v", report.Alerts)
	}
	if report.Note != "" {
		t.Fatalf("note %q belongs only to no significant change", report.Note)
	}
}

func TestNewFiringAlertAloneIsRegressionLikely(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{
		change: change, baseline: flatWindow(), after: flatWindow(),
		callers: map[string]float64{"web": 10},
		alerts: []alerting.AlertRuleData{{
			RuleID: "r2", RuleName: "web latency", State: "firing", Since: change.Add(5 * time.Minute).Unix(),
			Alerts: []alerting.AlertInstance{{GroupLabels: map[string]interface{}{"service_name": "web"}}},
		}},
	}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely || !strings.Contains(report.Reason, "web latency") {
		t.Fatalf("verdict = %q (%s), want a regression citing the dependent's new alert", report.Verdict, report.Reason)
	}
	assertRegressionsMatchReason(t, report)
}

// A deploy that sheds most traffic while errors and p95 stay flat must not
// read as safe: the requests that vanished may be failing before the service.
func TestTrafficCollapseIsRegressionLikely(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.requests = map[string]float64{"checkout": 25, "web": 600}
	after.errors = map[string]float64{"checkout": 0, "web": 3}
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely || !strings.Contains(report.Reason, "checkout throughput fell") {
		t.Fatalf("verdict=%q reason=%q; a ~98%% traffic drop must not clear the change", report.Verdict, report.Reason)
	}
	assertRegressionsMatchReason(t, report)
}

func TestDependentRegressionAloneIsRegressionLikely(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.p95 = map[string]float64{"checkout": 100, "web": 200}
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after, callers: map[string]float64{"web": 10}}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely || !strings.Contains(report.Reason, "web p95") {
		t.Fatalf("verdict = %q (%s), want a regression citing the dependent's p95", report.Verdict, report.Reason)
	}
	assertRegressionsMatchReason(t, report)
}

func TestFlatSignalsAreNoSignificantChange(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{
		change: change, baseline: flatWindow(), after: flatWindow(),
		callers: map[string]float64{"web": 10},
		alerts: []alerting.AlertRuleData{
			// Fired before the change, and on an unrelated service: neither counts.
			{RuleID: "old", AlertGroupName: "checkout", Alerts: []alerting.AlertInstance{{Since: change.Add(-time.Minute).Unix()}}},
			{RuleID: "other", AlertGroupName: "billing", Alerts: []alerting.AlertInstance{{Since: change.Add(time.Minute).Unix()}}},
		},
	}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictNoSignificantChange {
		t.Fatalf("verdict = %q (%s)", report.Verdict, report.Reason)
	}
	if !strings.Contains(report.Note, "valid answer") || !strings.Contains(report.Note, "not a failure") {
		t.Fatalf("note = %q", report.Note)
	}
	if len(report.Alerts.Started) != 0 {
		t.Fatalf("alerts = %+v, want none", report.Alerts.Started)
	}
	delta := signalsFor(t, report, "checkout").ThroughputRPM.DeltaPct
	if delta == nil || *delta != 0 {
		t.Fatalf("throughput delta = %v, want 0", delta)
	}
}

// An empty list is [] whether its section ran or was skipped, so callers never
// tell "nothing found" from "not queried" by null.
func TestEmptyListsAreNeverNull(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	server := f.serve(t)
	defer server.Close()
	report := Analyze(context.Background(), server.Client(), testConfig(server.URL), Request{Service: "checkout", Env: "prod", Change: change})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []string{`"started":null`, `"items":null`, `"services":null`, `"drill_down":null`} {
		if strings.Contains(string(raw), list) {
			t.Errorf("report has %s: %s", list, raw)
		}
	}
}

func TestEmptyAfterWindowIsInsufficient(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: windowData{}}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictInsufficientData || !strings.Contains(report.Reason, "no requests") {
		t.Fatalf("verdict = %q (%s)", report.Verdict, report.Reason)
	}
}

func TestResultBoundsReportDroppedCounts(t *testing.T) {
	change := recentChange()
	callers := map[string]float64{}
	baseline, after := flatWindow(), flatWindow()
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("caller-%02d", i)
		callers[name] = float64(i)
		baseline.requests[name], after.requests[name] = 100, 100
	}
	var deviations []traces.AttributeDeviation
	var alerts []alerting.AlertRuleData
	for i := 12; i >= 1; i-- {
		deviations = append(deviations, traces.AttributeDeviation{Attribute: "k8s.pod", Value: fmt.Sprintf("pod-%d", i), Rank: i, PercentagePointDelta: float64(i)})
		alerts = append(alerts, alerting.AlertRuleData{RuleID: fmt.Sprintf("r%02d", i), AlertGroupName: "checkout",
			Alerts: []alerting.AlertInstance{{State: "firing", Since: change.Add(time.Duration(i) * time.Minute).Unix()}}})
	}
	f := &fakeLast9{change: change, baseline: baseline, after: after, callers: callers, traceDeviations: deviations, alerts: alerts}

	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	g := report.GoldenSignals
	if len(g.Services) != 1+maxDependents || g.DependentsTotal != 12 || g.DependentsDropped != 2 {
		t.Fatalf("golden services=%d total=%d dropped=%d", len(g.Services), g.DependentsTotal, g.DependentsDropped)
	}
	for _, s := range g.Services {
		if s.Service == "caller-01" || s.Service == "caller-02" {
			t.Fatalf("%s is among the least busy callers and should be dropped", s.Service)
		}
	}
	tr := report.Deviations.TraceAttributes
	if len(tr.Items) != sectionTopN || tr.Dropped != 2 || tr.Items[0].Rank != 1 {
		t.Fatalf("trace deviations = %d items, dropped %d, first rank %d", len(tr.Items), tr.Dropped, tr.Items[0].Rank)
	}
	if len(report.Alerts.Started) != sectionTopN || report.Alerts.Dropped != 2 || report.Alerts.Started[0].RuleID != "r01" {
		t.Fatalf("alerts = %d, dropped %d, first %+v", len(report.Alerts.Started), report.Alerts.Dropped, report.Alerts.Started[0])
	}
	// The alerts section keeps 10, but every firing alert is a regression.
	if report.Verdict != VerdictRegressionLikely || len(report.Regressions) != 12 {
		t.Fatalf("verdict = %q, regressions = %d, want all 12 alerts", report.Verdict, len(report.Regressions))
	}
	assertRegressionsMatchReason(t, report)
}

func TestFailingUpstreamDegradesOnlyItsSection(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.errors = map[string]float64{"checkout": 60, "web": 3}
	f := &fakeLast9{
		change: change, baseline: flatWindow(), after: after,
		fail: map[string]int{"alerts": http.StatusServiceUnavailable, "trace_deviations": http.StatusInternalServerError},
	}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Alerts.Error, "503") {
		t.Fatalf("alerts error = %q, want the 503 named", report.Alerts.Error)
	}
	if report.Deviations.TraceAttributes.Error == "" {
		t.Fatal("trace deviations error must name its cause")
	}
	if report.Verdict != VerdictRegressionLikely {
		t.Fatalf("verdict = %q (%s); the golden signals still show the regression", report.Verdict, report.Reason)
	}
	assertRegressionsMatchReason(t, report)
}

// The on-call runbook rules a change out on `no significant change`, so a
// section that never completed must block that verdict, not be claimed in it.
func TestMissingEvidenceCannotClearTheChange(t *testing.T) {
	cases := map[string]struct {
		fail, failQuery map[string]int
		section         func(Report) string
		want            string
	}{
		"alerts": {
			fail:    map[string]int{"alerts": http.StatusServiceUnavailable},
			section: func(r Report) string { return r.Alerts.Error },
			want:    "alerts",
		},
		"apm deviations": {
			failQuery: map[string]int{"apdex": http.StatusServiceUnavailable},
			section:   func(r Report) string { return r.Deviations.APM.Error },
			want:      "APM deviations",
		},
		"dependents": {
			failQuery: map[string]int{"trace_call_graph_count": http.StatusServiceUnavailable},
			section:   func(r Report) string { return r.GoldenSignals.DependentsError },
			want:      "direct callers",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			change := recentChange()
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(), fail: tc.fail, failQuery: tc.failQuery}
			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if tc.section(report) == "" {
				t.Fatal("failure injection did not reach the section")
			}
			if report.Verdict != VerdictInsufficientData || !strings.Contains(report.Reason, tc.want) {
				t.Fatalf("verdict = %q (%s), want %q naming %s", report.Verdict, report.Reason, VerdictInsufficientData, tc.want)
			}
			if strings.Contains(report.Reason, "within bounds") || report.Note != "" {
				t.Fatalf("reason %q / note %q must not claim the signals were within bounds", report.Reason, report.Note)
			}
		})
	}
}

// Trace-attribute deviations explain a verdict but never set one, so losing
// them hides no regression the verdict would have found.
func TestFailingTraceDeviationsLeaveTheVerdictAlone(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(), fail: map[string]int{"trace_deviations": http.StatusInternalServerError}}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Deviations.TraceAttributes.Error == "" {
		t.Fatal("failure injection did not reach the trace deviations section")
	}
	if report.Verdict != VerdictNoSignificantChange {
		t.Fatalf("verdict = %q (%s), want %q", report.Verdict, report.Reason, VerdictNoSignificantChange)
	}
}

func TestRegressionDisclosesMissingEvidence(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.errors = map[string]float64{"checkout": 60, "web": 3}
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after, fail: map[string]int{"alerts": http.StatusServiceUnavailable}}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely || !strings.Contains(report.Reason, "error rate") || !strings.Contains(report.Reason, "alerts unavailable") {
		t.Fatalf("verdict = %q (%s), want the regression with the missing alerts disclosed", report.Verdict, report.Reason)
	}
	assertRegressionsMatchReason(t, report)
}

func TestFailingGoldenSignalsIsInsufficientWithCause(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, fail: map[string]int{"prom": http.StatusBadGateway}}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.GoldenSignals.Error == "" || report.Verdict != VerdictInsufficientData || !strings.Contains(report.Reason, "golden signals") {
		t.Fatalf("golden error=%q verdict=%q reason=%q", report.GoldenSignals.Error, report.Verdict, report.Reason)
	}
	if f.count("alerts") == 0 {
		t.Fatal("the alerts section must still be queried")
	}
}

func TestTraceDeviationsNeedAnEnv(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	args := serviceArgs(change)
	args.Env = ""
	report, err := runTool(t, f, args)
	if err != nil {
		t.Fatal(err)
	}
	if report.Deviations.TraceAttributes.Skipped == "" || f.count("trace_deviations") != 0 {
		t.Fatalf("trace deviations = %+v, calls = %d; want skipped without env", report.Deviations.TraceAttributes, f.count("trace_deviations"))
	}
}
