package changeimpact

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type windowData struct {
	requests map[string]float64
	errors   map[string]float64
	p95      map[string]float64
}

// fakeLast9 serves the calls analyze_change_impact makes. Prom instant queries
// are told apart by metric name and by their evaluation timestamp: at or before
// the change is the baseline window, later is the after window.
type fakeLast9 struct {
	change          time.Time
	baseline, after windowData
	callers         map[string]float64
	alerts          []alerting.AlertRuleData
	traceDeviations []traces.AttributeDeviation
	fail            map[string]int // route → status
	failQuery       map[string]int // PromQL substring → status

	mu       sync.Mutex
	requests []string
}

func (f *fakeLast9) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := routeOf(r.URL.Path)
		f.mu.Lock()
		f.requests = append(f.requests, route)
		f.mu.Unlock()
		if status := f.fail[route]; status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch route {
		case "prom":
			var req struct {
				Query     string `json:"query"`
				Timestamp int64  `json:"timestamp"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode prom body: %v", err)
			}
			for substr, status := range f.failQuery {
				if strings.Contains(req.Query, substr) {
					w.WriteHeader(status)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(f.promAnswer(req.Query, req.Timestamp))
		case "alerts":
			_ = json.NewEncoder(w).Encode(alerting.AlertsResponse{AlertRules: f.alerts})
		case "trace_deviations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contract_version": "investigation-evidence/v1",
				"analysis_version": "trace-attribute-deviations/v1",
				"evidence":         map[string]any{"warnings": []string{"candidate_limit_reached"}},
				"data":             map[string]any{"deviations": f.traceDeviations},
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func routeOf(path string) string {
	switch {
	case strings.HasSuffix(path, "/prom_query_instant"):
		return "prom"
	case strings.HasSuffix(path, "/alerts/monitor"):
		return "alerts"
	case strings.HasSuffix(path, "/attribute-deviations"):
		return "trace_deviations"
	}
	return path
}

func (f *fakeLast9) promAnswer(query string, ts int64) []map[string]any {
	w := f.after
	if ts <= f.change.Unix() {
		w = f.baseline
	}
	sample := func(labels map[string]string, v float64) map[string]any {
		return map[string]any{"metric": labels, "value": []any{float64(ts), strconv.FormatFloat(v, 'f', -1, 64)}}
	}
	out := []map[string]any{}
	switch {
	case strings.Contains(query, "trace_call_graph_count"):
		for caller, rpm := range f.callers {
			out = append(out, sample(map[string]string{"client": caller}, rpm))
		}
	case strings.Contains(query, "trace_service_response_time") && strings.Contains(query, "p99"):
		for svc, v := range w.p95 {
			out = append(out, sample(map[string]string{"service_name": svc, "quantile": "p95"}, v))
		}
	case strings.HasPrefix(query, "sum by (service_name, env)(sum_over_time(trace_endpoint_count{"):
		values := w.requests
		switch {
		case strings.Contains(query, `http_status_code=~"5.*"`):
			values = w.errors
		case strings.Contains(query, "http_status_code"), strings.Contains(query, "grpc_status_code"):
			values = nil
		}
		for svc, v := range values {
			out = append(out, sample(map[string]string{"service_name": svc, "env": "prod"}, v))
		}
	}
	return out
}

func (f *fakeLast9) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == route {
			n++
		}
	}
	return n
}

func testConfig(url string) models.Config {
	return models.Config{
		APIBaseURL: url,
		Region:     "us-east-1",
		OrgSlug:    "acme",
		ClusterID:  "c1",
		TokenManager: &auth.TokenManager{
			AccessToken: "token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

// recentChange is a change two hours ago, so the default after window is complete.
func recentChange() time.Time {
	return time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Hour)
}

func flatWindow() windowData {
	return windowData{
		requests: map[string]float64{"checkout": 1200, "web": 600},
		errors:   map[string]float64{"checkout": 6, "web": 3},
		p95:      map[string]float64{"checkout": 100, "web": 80},
	}
}

func runTool(t *testing.T, f *fakeLast9, args AnalyzeChangeImpactArgs) (Report, error) {
	t.Helper()
	return runToolWith(t, f, args, func(*models.Config) {})
}

func runToolWith(t *testing.T, f *fakeLast9, args AnalyzeChangeImpactArgs, configure func(*models.Config)) (Report, error) {
	t.Helper()
	server := f.serve(t)
	defer server.Close()
	cfg := testConfig(server.URL)
	configure(&cfg)
	result, _, err := NewAnalyzeChangeImpactHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return Report{}, err
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var report Report
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, text)
	}
	return report, nil
}

func serviceArgs(change time.Time) AnalyzeChangeImpactArgs {
	return AnalyzeChangeImpactArgs{Service: "checkout", Env: "prod", Timestamp: change.Format(time.RFC3339)}
}

func signalsFor(t *testing.T, r Report, service string) ServiceSignals {
	t.Helper()
	for _, s := range r.GoldenSignals.Services {
		if s.Service == service {
			return s
		}
	}
	t.Fatalf("no golden signals for %q in %+v", service, r.GoldenSignals.Services)
	return ServiceSignals{}
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
			RuleID: "r2", RuleName: "web latency", Since: change.Add(5 * time.Minute).Unix(),
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
}

func TestRuleWithoutAlertInstancesHasNotStarted(t *testing.T) {
	change := recentChange()
	for name, instances := range map[string][]alerting.AlertInstance{"empty": {}, "omitted": nil} {
		t.Run(name, func(t *testing.T) {
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(), alerts: []alerting.AlertRuleData{{
				RuleID: "stale", RuleName: "checkout latency", AlertGroupName: "checkout",
				Since: change.Add(5 * time.Minute).Unix(), Alerts: instances,
			}}}
			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Alerts.Started) != 0 || report.Verdict == VerdictRegressionLikely {
				t.Fatalf("rule with no instances counted: verdict=%q (%s) alerts=%+v", report.Verdict, report.Reason, report.Alerts.Started)
			}
		})
	}
}

func TestAlertsMatchAServiceOnlyByServiceLabelsOrGroupName(t *testing.T) {
	cases := []struct {
		name        string
		group       string
		labels      map[string]interface{}
		wantService string
	}{
		{"service_name label", "", map[string]interface{}{"service_name": "web"}, "web"},
		{"service label", "", map[string]interface{}{"service": "web"}, "web"},
		{"service.name label", "", map[string]interface{}{"service.name": "web"}, "web"},
		{"service label in another case", "", map[string]interface{}{"service_name": "Web"}, "web"},
		{"group name in another case", "Checkout", nil, "checkout"},
		{"team label", "", map[string]interface{}{"team": "web"}, ""},
		{"exported_job label", "", map[string]interface{}{"exported_job": "checkout"}, ""},
		{"other service named", "", map[string]interface{}{"service_name": "billing", "component": "checkout"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := recentChange()
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(), callers: map[string]float64{"web": 10},
				alerts: []alerting.AlertRuleData{{
					RuleID: "r", RuleName: "some alert", AlertGroupName: tc.group,
					Alerts: []alerting.AlertInstance{{Since: change.Add(10 * time.Minute).Unix(), GroupLabels: tc.labels}},
				}}}
			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantService == "" {
				if len(report.Alerts.Started) != 0 || report.Verdict == VerdictRegressionLikely {
					t.Fatalf("unrelated alert attributed: verdict=%q (%s) alerts=%+v", report.Verdict, report.Reason, report.Alerts.Started)
				}
				return
			}
			if len(report.Alerts.Started) != 1 || report.Alerts.Started[0].Service != tc.wantService {
				t.Fatalf("alerts = %+v, want one for %s", report.Alerts.Started, tc.wantService)
			}
		})
	}
}

func TestAlertsStayInsideTheRequestedEnv(t *testing.T) {
	cases := []struct {
		name, reqEnv string
		labels       map[string]interface{}
		wantStarted  bool
		wantEnvMatch string
	}{
		{"same env", "prod", map[string]interface{}{"service_name": "checkout", "env": "prod"}, true, envMatchExact},
		{"other env", "prod", map[string]interface{}{"service_name": "checkout", "env": "staging"}, false, ""},
		{"other environment label", "prod", map[string]interface{}{"service_name": "checkout", "deployment_environment": "staging"}, false, ""},
		{"no env label", "prod", map[string]interface{}{"service_name": "checkout"}, true, envMatchUnlabelled},
		{"no env requested", "", map[string]interface{}{"service_name": "checkout", "env": "staging"}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := recentChange()
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow(), alerts: []alerting.AlertRuleData{{
				RuleID: "latency", AlertGroupName: "checkout", RuleName: "checkout latency",
				Alerts: []alerting.AlertInstance{{State: "firing", Since: change.Add(10 * time.Minute).Unix(), GroupLabels: tc.labels}},
			}}}
			args := serviceArgs(change)
			args.Env = tc.reqEnv
			report, err := runTool(t, f, args)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantStarted {
				if len(report.Alerts.Started) != 0 || report.Verdict == VerdictRegressionLikely {
					t.Fatalf("alert from another env attributed to the change: verdict=%q (%s) alerts=%+v", report.Verdict, report.Reason, report.Alerts.Started)
				}
				return
			}
			if len(report.Alerts.Started) != 1 || report.Verdict != VerdictRegressionLikely {
				t.Fatalf("verdict=%q (%s) alerts=%+v, want the alert kept", report.Verdict, report.Reason, report.Alerts.Started)
			}
			if got := report.Alerts.Started[0].EnvMatch; got != tc.wantEnvMatch {
				t.Fatalf("env_match = %q, want %q", got, tc.wantEnvMatch)
			}
			if tc.wantEnvMatch == envMatchUnlabelled && !strings.Contains(report.Reason, "no env label") {
				t.Fatalf("reason %q must not imply the alert is in %s", report.Reason, tc.reqEnv)
			}
		})
	}
}

func TestFetchStartedAlertsPagesHourSlicesAndDedups(t *testing.T) {
	change := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
	cases := []struct {
		after     time.Duration
		wantSpans []int64
	}{
		{120 * time.Minute, []int64{3600, 3600}},
		{90 * time.Minute, []int64{3600, 1800}},
	}
	for _, tc := range cases {
		t.Run(tc.after.String(), func(t *testing.T) {
			var calls []alerting.AlertsMonitorQuery
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				ts, _ := strconv.ParseInt(r.URL.Query().Get("timestamp"), 10, 64)
				win, _ := strconv.ParseInt(r.URL.Query().Get("window"), 10, 64)
				calls = append(calls, alerting.AlertsMonitorQuery{Timestamp: ts, Window: win})
				// Every slice reports the same instance, as a rule still firing across slices does.
				rule := alerting.AlertRuleData{RuleID: "r1", AlertGroupName: "checkout",
					Alerts: []alerting.AlertInstance{{Since: change.Add(20 * time.Minute).Unix()}}}
				_ = json.NewEncoder(w).Encode(alerting.AlertsResponse{AlertRules: []alerting.AlertRuleData{rule}})
			}))
			defer server.Close()
			end := change.Add(tc.after)
			got, err := fetchStartedAlerts(context.Background(), server.Client(), testConfig(server.URL),
				alertScope{window: apm.TimeWindow{Start: change, End: end}, services: []string{"checkout"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != len(tc.wantSpans) {
				t.Fatalf("monitor calls = %+v, want %d slices", calls, len(tc.wantSpans))
			}
			wantEnd := end.Unix()
			for i, c := range calls {
				if c.Timestamp != wantEnd || c.Window != tc.wantSpans[i] {
					t.Fatalf("slice %d = %+v, want ending %d spanning %d", i, c, wantEnd, tc.wantSpans[i])
				}
				wantEnd -= c.Window
			}
			if wantEnd != change.Unix() {
				t.Fatalf("slices stop at %d, want the window start %d", wantEnd, change.Unix())
			}
			if len(got) != 1 || got[0].RuleID != "r1" {
				t.Fatalf("started = %+v, want one deduped alert", got)
			}
		})
	}
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

func TestShortOrFutureAfterWindowIsInsufficientWithoutQuerying(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	for name, change := range map[string]time.Time{
		"future":     now.Add(time.Hour),
		"3m elapsed": now.Add(-3 * time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeLast9{change: change}
			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict != VerdictInsufficientData || !strings.Contains(report.Reason, "after window") {
				t.Fatalf("verdict = %q (%s)", report.Verdict, report.Reason)
			}
			if len(f.requests) != 0 {
				t.Fatalf("made %d requests, want none", len(f.requests))
			}
			if report.GoldenSignals.Skipped == "" || report.Alerts.Skipped == "" {
				t.Fatal("skipped sections must say why")
			}
		})
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

func TestInputValidationRejectsBeforeAnyRequest(t *testing.T) {
	change := recentChange()
	event := &change_events.TimeSeries{
		Metric: map[string]string{"service": "checkout"},
		Values: []change_events.TimeSeriesPoint{{Timestamp: uint64(change.Unix()), Value: 1}},
	}
	cases := map[string]struct {
		args AnalyzeChangeImpactArgs
		want string
	}{
		"neither form":          {AnalyzeChangeImpactArgs{}, "change_event or service"},
		"blank service":         {AnalyzeChangeImpactArgs{Service: "  ", Timestamp: "1700000000"}, "change_event or service"},
		"service only":          {AnalyzeChangeImpactArgs{Service: "checkout"}, "timestamp"},
		"both forms":            {AnalyzeChangeImpactArgs{ChangeEvent: event, Service: "checkout", Timestamp: "1700000000"}, "not both"},
		"event plus timestamp":  {AnalyzeChangeImpactArgs{ChangeEvent: event, Timestamp: "1700000000"}, "not both"},
		"bad timestamp":         {AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: "yesterday"}, "timestamp"},
		"milliseconds":          {AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: "1700000000000"}, "milliseconds"},
		"event without service": {AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{Values: event.Values}}, "metric.service"},
		"event without a point": {AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{Metric: event.Metric}}, "values"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeLast9{change: change}
			_, err := runTool(t, f, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if len(f.requests) != 0 {
				t.Fatalf("made %d requests before rejecting", len(f.requests))
			}
		})
	}
}

func TestChangeEventFormUsesServiceEnvAndEarliestPoint(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{
		Metric: map[string]string{"service": "checkout", "deployment_environment": "prod", "event_name": "deploy"},
		Values: []change_events.TimeSeriesPoint{
			{Timestamp: uint64(change.Add(time.Minute).Unix()), Value: 1},
			{Timestamp: uint64(change.Unix()), Value: 1},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Service != "checkout" || report.Env != "prod" || report.ChangeTime != change.Format(time.RFC3339) {
		t.Fatalf("service=%q env=%q change_time=%q", report.Service, report.Env, report.ChangeTime)
	}
}

func TestUnixSecondsTimestampIsAccepted(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: strconv.FormatInt(change.Unix(), 10)})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChangeTime != change.Format(time.RFC3339) {
		t.Fatalf("change_time = %q", report.ChangeTime)
	}
}

func TestWindowMinutesAreClamped(t *testing.T) {
	cases := []struct {
		baseline, after         int
		wantBaseline, wantAfter int
		clamped                 bool
	}{
		{0, 0, 60, 60, false},
		{45, 0, 45, 45, false},
		{10, 0, 30, 30, true},
		{29, 31, 30, 31, true},
		{30, 120, 30, 120, false},
		{121, 500, 120, 120, true},
		{-5, 90, 30, 90, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d/%d", tc.baseline, tc.after), func(t *testing.T) {
			change := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
			args := serviceArgs(change)
			args.BaselineMinutes, args.AfterMinutes = tc.baseline, tc.after
			report, err := runTool(t, f, args)
			if err != nil {
				t.Fatal(err)
			}
			w := report.Windows
			if w.BaselineMinutes != tc.wantBaseline || w.AfterMinutes != tc.wantAfter {
				t.Fatalf("windows = %d/%d, want %d/%d", w.BaselineMinutes, w.AfterMinutes, tc.wantBaseline, tc.wantAfter)
			}
			if w.BaselineStart != change.Add(-time.Duration(tc.wantBaseline)*time.Minute).Format(time.RFC3339) ||
				w.AfterEnd != change.Add(time.Duration(tc.wantAfter)*time.Minute).Format(time.RFC3339) {
				t.Fatalf("windows = %+v", w)
			}
			if gotClamped := strings.Contains(strings.Join(report.Notes, " "), "clamped"); gotClamped != tc.clamped {
				t.Fatalf("notes = %v, clamped note expected %v", report.Notes, tc.clamped)
			}
		})
	}
}

func TestAfterWindowIsCappedAtNow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	change := now.Add(-20 * time.Minute)
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Windows.AfterMinutes > 21 || report.Windows.AfterMinutes < 20 || report.Windows.RequestedAfterMinutes != 60 {
		t.Fatalf("windows = %+v, want the after window capped at now", report.Windows)
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
			Alerts: []alerting.AlertInstance{{Since: change.Add(time.Duration(i) * time.Minute).Unix()}}})
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

func TestDrillDownNamesNextToolsWithWindows(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]map[string]any{}
	for _, d := range report.DrillDown {
		tools[d.Tool] = d.Arguments
	}
	for _, want := range []string{"get_service_performance_details", "get_apm_service_deviations", "get_service_traces", "get_alerts"} {
		if tools[want] == nil {
			t.Fatalf("drill_down missing %s: %+v", want, report.DrillDown)
		}
	}
	if tools["get_apm_service_deviations"]["baseline_end_time_iso"] != change.Format(time.RFC3339) || tools["get_service_traces"]["service_name"] != "checkout" {
		t.Fatalf("drill_down arguments = %+v", tools)
	}
}

func TestDrillDownNamesOnlyServedTools(t *testing.T) {
	for _, spec := range []string{"investigate", "alerts", "metrics,alerts,traces"} {
		t.Run(spec, func(t *testing.T) {
			allowed, err := toolsets.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			change := recentChange()
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
			report, err := runToolWith(t, f, serviceArgs(change), func(cfg *models.Config) { cfg.AllowedTools = allowed })
			if err != nil {
				t.Fatal(err)
			}
			if len(report.DrillDown) == 0 && spec != "alerts" {
				t.Fatal("drill_down is empty")
			}
			for _, d := range report.DrillDown {
				if !allowed.Allows(d.Tool) {
					t.Fatalf("drill_down names %s, which --toolsets=%s does not serve", d.Tool, spec)
				}
			}
			if spec == "metrics,alerts,traces" && len(report.DrillDown) != 5 {
				t.Fatalf("drill_down = %+v, want all five tools when all are served", report.DrillDown)
			}
		})
	}
}
