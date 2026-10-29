package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
)

// 2023-11-14 22:13:20 UTC; the newest completed-and-settled minute ends at 22:12.
var evaluationTestNow = time.Unix(1700000000, 0)

type fakeMonitor struct {
	mu       sync.Mutex
	requests []*http.Request
	// rules and statuses are keyed by the request's timestamp; a missing status is 200.
	rules    map[int64][]AlertRuleData
	statuses map[int64]int
	// barrier, when set, holds every request until that many have arrived.
	barrier int
	arrived chan struct{}
}

func (f *fakeMonitor) serve(t *testing.T) *last9api.Client {
	t.Helper()
	if f.barrier > 0 {
		f.arrived = make(chan struct{}, f.barrier)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointAlertsMonitor {
			http.NotFound(w, r)
			return
		}
		ts, _ := strconv.ParseInt(r.URL.Query().Get("timestamp"), 10, 64)
		f.mu.Lock()
		f.requests = append(f.requests, r)
		status := f.statuses[ts]
		rules := append([]AlertRuleData{}, f.rules[ts]...)
		f.mu.Unlock()
		if f.barrier > 0 && !f.waitForBarrier() {
			status = http.StatusGatewayTimeout
		}
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"monitor failed"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(AlertsResponse{AlertRules: rules})
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}
	return last9api.NewClient(srv.Client(), cfg)
}

func (f *fakeMonitor) waitForBarrier() bool {
	f.arrived <- struct{}{}
	deadline := time.After(2 * time.Second)
	for {
		if len(f.arrived) == f.barrier {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (f *fakeMonitor) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func minute(i int) int64 {
	return time.Date(2023, 11, 14, 22, 12, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute).Unix()
}

func enabledRules(n int) AlertConfigResponse {
	rules := make(AlertConfigResponse, n)
	for i := range rules {
		rules[i] = AlertRule{ID: fmt.Sprintf("rule-%d", i), State: "active"}
	}
	return rules
}

func TestEvaluationMinutesSkipTheUnsettledMinute(t *testing.T) {
	for _, now := range []time.Time{
		evaluationTestNow,
		time.Date(2023, 11, 14, 22, 13, 0, 0, time.UTC),
		time.Date(2023, 11, 14, 22, 13, 59, 999, time.UTC),
	} {
		got := evaluationMinutes(now)
		if len(got) != evaluationHistorySamples {
			t.Fatalf("now %s: %d minutes, want %d", now, len(got), evaluationHistorySamples)
		}
		for i, m := range got {
			if m.Unix() != minute(i) {
				t.Errorf("now %s: minute %d = %s, want %s", now, i, m.UTC(), time.Unix(minute(i), 0).UTC())
			}
		}
	}
}

func TestNormalizeMonitorState(t *testing.T) {
	cases := map[string]string{
		"firing":   evaluationFiring,
		" FIRING ": evaluationFiring,
		"normal":   evaluationOK,
		"resolved": evaluationOK,
		"pending":  evaluationOK,
		"":         evaluationOK,
		"no_data":  evaluationUnknown,
		"silenced": evaluationUnknown,
	}
	for in, want := range cases {
		if got := normalizeMonitorState(in); got != want {
			t.Errorf("normalizeMonitorState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAttachRecentEvaluationsNormalizesEachMinute(t *testing.T) {
	f := &fakeMonitor{rules: map[int64][]AlertRuleData{
		// Severity rows for one rule: firing wins regardless of order.
		minute(0): {{RuleID: "rule-0", State: "resolved"}, {RuleID: "rule-0", State: "firing"}, {RuleID: "rule-1", State: "normal"}},
		minute(1): {{RuleID: "rule-0", State: "firing"}, {RuleID: "rule-0", State: "normal"}, {RuleID: "rule-1", State: "pending"}},
		minute(2): {{RuleID: "rule-0", State: "normal"}, {RuleID: "rule-0", State: "silenced"}},
		minute(3): {{RuleID: "rule-0", State: "normal"}, {RuleID: "", State: "firing"}},
		minute(4): {{RuleID: "rule-0", State: "resolved"}, {RuleID: "rule-1", State: "firing"}},
	}}
	rules := enabledRules(2)

	note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

	if note != "" {
		t.Errorf("note = %q, want none", note)
	}
	want := map[string]string{
		"rule-0": "  Recent Evaluations (1-minute samples, minute ending UTC, newest first): 2023-11-14T22:12Z firing, 2023-11-14T22:11Z firing, 2023-11-14T22:10Z unknown, 2023-11-14T22:09Z ok, 2023-11-14T22:08Z ok\n",
		"rule-1": "  Recent Evaluations (1-minute samples, minute ending UTC, newest first): 2023-11-14T22:12Z ok, 2023-11-14T22:11Z ok, 2023-11-14T22:10Z unknown, 2023-11-14T22:09Z unknown, 2023-11-14T22:08Z firing\n",
	}
	for _, rule := range rules {
		if got := formatRecentEvaluations(rule); got != want[rule.ID] {
			t.Errorf("%s:\n got %q\nwant %q", rule.ID, got, want[rule.ID])
		}
	}
	if n := f.requestCount(); n != evaluationHistorySamples {
		t.Fatalf("monitor calls = %d, want %d", n, evaluationHistorySamples)
	}
	seen := map[int64]bool{}
	for _, r := range f.requests {
		q := r.URL.Query()
		if q.Get("window") != "60" {
			t.Errorf("window = %q, want 60", q.Get("window"))
		}
		ts, _ := strconv.ParseInt(q.Get("timestamp"), 10, 64)
		seen[ts] = true
	}
	for i := 0; i < evaluationHistorySamples; i++ {
		if !seen[minute(i)] {
			t.Errorf("minute %d (%d) never requested", i, minute(i))
		}
	}
}

func TestAttachRecentEvaluationsFetchesMinutesConcurrently(t *testing.T) {
	f := &fakeMonitor{barrier: evaluationHistorySamples}
	rules := enabledRules(1)

	note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

	if note != "" {
		t.Fatalf("minutes were not fetched concurrently: %q", note)
	}
}

func TestAttachRecentEvaluationsReportsFailedMinutes(t *testing.T) {
	f := &fakeMonitor{
		rules:    map[int64][]AlertRuleData{minute(0): {{RuleID: "rule-0", State: "firing"}}},
		statuses: map[int64]int{minute(1): http.StatusInternalServerError, minute(3): http.StatusBadGateway},
	}
	rules := enabledRules(1)

	note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

	if !strings.HasPrefix(note, "Recent Evaluations: the alert monitor lookup failed for 2 of 5 minutes, shown as unknown: ") {
		t.Errorf("note = %q", note)
	}
	got := formatRecentEvaluations(rules[0])
	if !strings.Contains(got, "22:12Z firing, 2023-11-14T22:11Z unknown, 2023-11-14T22:10Z unknown, 2023-11-14T22:09Z unknown") {
		t.Errorf("line = %q", got)
	}
}

func TestAttachRecentEvaluationsSkipsDisabledRules(t *testing.T) {
	f := &fakeMonitor{}
	rules := AlertConfigResponse{{ID: "draft", State: "Disabled"}}

	note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

	if note != "" || f.requestCount() != 0 {
		t.Errorf("note = %q, monitor calls = %d; want none for disabled-only rules", note, f.requestCount())
	}
	if got, want := formatRecentEvaluations(rules[0]), "  Recent Evaluations: none, the rule is disabled\n"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestAttachRecentEvaluationsRuleLimit(t *testing.T) {
	t.Run("at the limit", func(t *testing.T) {
		f := &fakeMonitor{}
		rules := append(enabledRules(evaluationHistoryMaxRules), AlertRule{ID: "draft", State: "disabled"})

		note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

		if note != "" || f.requestCount() != evaluationHistorySamples {
			t.Errorf("note = %q, monitor calls = %d", note, f.requestCount())
		}
		if !strings.Contains(formatRecentEvaluations(rules[evaluationHistoryMaxRules-1]), "unknown") {
			t.Errorf("last enabled rule has no history")
		}
	})
	t.Run("over the limit", func(t *testing.T) {
		f := &fakeMonitor{}
		rules := enabledRules(evaluationHistoryMaxRules + 1)

		note := attachRecentEvaluations(context.Background(), f.serve(t), rules, evaluationTestNow)

		want := "Recent Evaluations omitted: 26 enabled rules matched, more than the 25 that get history; narrow the filters (e.g. rule_id or rule_name) to see them.\n\n"
		if note != want {
			t.Errorf("note = %q, want %q", note, want)
		}
		if f.requestCount() != 0 {
			t.Errorf("monitor calls = %d, want 0", f.requestCount())
		}
		if got := formatRecentEvaluations(rules[0]); got != "" {
			t.Errorf("line = %q, want none", got)
		}
	})
}

func TestGetAlertConfigShowsRecentEvaluations(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:   AlertConfigResponse{{ID: "rule-1", RuleName: "Latency", State: "active", EntityID: "entity-1"}},
		monitorRules: []AlertRuleData{{RuleID: "rule-1", State: "firing"}},
	}

	text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(text, "  Recent Evaluations (1-minute samples, minute ending UTC, newest first): ") {
		t.Errorf("missing recent evaluations:\n%s", text)
	}
	if strings.Count(text, " firing") != evaluationHistorySamples {
		t.Errorf("want %d firing samples:\n%s", evaluationHistorySamples, text)
	}
	if state.monitorCalls.Load() != evaluationHistorySamples {
		t.Errorf("monitor calls = %d, want %d", state.monitorCalls.Load(), evaluationHistorySamples)
	}
}

func TestGetAlertConfigHistoryFollowsIsDisabled(t *testing.T) {
	for name, tt := range map[string]struct {
		rule      string
		disabled  bool
		wantCalls int32
	}{
		"is_disabled false wins over state disabled": {`{"id":"rule-1","entity_id":"entity-1","state":"disabled","is_disabled":false}`, false, evaluationHistorySamples},
		"is_disabled true wins over state muted":     {`{"id":"rule-1","entity_id":"entity-1","state":"muted","is_disabled":true}`, true, 0},
	} {
		t.Run(name, func(t *testing.T) {
			var rule AlertRule
			if err := json.Unmarshal([]byte(tt.rule), &rule); err != nil {
				t.Fatal(err)
			}
			state := alertConfigTestServerState{
				alertRules:   AlertConfigResponse{rule},
				monitorRules: []AlertRuleData{{RuleID: "rule-1", State: "firing"}},
			}

			text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{RuleID: "rule-1"})
			if err != nil {
				t.Fatal(err)
			}

			if want := fmt.Sprintf("  Disabled: %t\n", tt.disabled); !strings.Contains(text, want) {
				t.Errorf("missing %q:\n%s", want, text)
			}
			if got := strings.Contains(text, "the rule is disabled"); got != tt.disabled {
				t.Errorf("reports history suppressed = %t, want %t:\n%s", got, tt.disabled, text)
			}
			if got := state.monitorCalls.Load(); got != tt.wantCalls {
				t.Errorf("monitor calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestGetAlertConfigUnsupportedMonitorStateIsUnknown(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:   AlertConfigResponse{{ID: "rule-1", State: "active", EntityID: "entity-1"}},
		monitorRules: []AlertRuleData{{RuleID: "rule-1", State: "silenced"}},
	}

	text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{RuleID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(text, " unknown") != evaluationHistorySamples {
		t.Errorf("want %d unknown samples:\n%s", evaluationHistorySamples, text)
	}
	if strings.Contains(text, "lookup failed") {
		t.Errorf("a returned row was reported as a failed lookup:\n%s", text)
	}
}

func TestGetAlertConfigPrependsEvaluationNote(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:    enabledRules(evaluationHistoryMaxRules + 1),
		monitorStatus: http.StatusInternalServerError,
	}

	text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(text, "Recent Evaluations omitted: 26 enabled rules matched") {
		t.Errorf("missing note:\n%s", text)
	}
}

func TestGetAlertConfigDescriptionStatesEvaluationLimits(t *testing.T) {
	for _, phrase := range []string{
		fmt.Sprintf("%d one-minute samples, newest first", evaluationHistorySamples),
		"the newest ends 1-2 minutes before the call",
		"firing; ok (normal, resolved, pending or empty state);",
		"unknown (missing row, failed lookup or unrecognized state, e.g. no_data, silenced).",
		"Failures noted at top.",
		"Disabled rules show none",
		fmt.Sprintf("more than %d enabled rules match", evaluationHistoryMaxRules),
	} {
		if !strings.Contains(prompts.GetAlertConfigDescription, phrase) {
			t.Errorf("get_alert_config description is missing %q", phrase)
		}
	}
}
