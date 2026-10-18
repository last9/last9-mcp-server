package changeimpact

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
)

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
		{"same env, label case differs", "prod", map[string]interface{}{"service_name": "checkout", "env": "Prod"}, true, envMatchExact},
		{"same env, request case differs", "PROD", map[string]interface{}{"service_name": "checkout", "deployment.environment": "prod"}, true, envMatchExact},
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
			assertRegressionsMatchReason(t, report)
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
