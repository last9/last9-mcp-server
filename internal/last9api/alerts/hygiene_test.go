package alerts

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testHygieneInput = GetAlertHygieneInput{
	From:     time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC),
	To:       time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	Timezone: "UTC",
}

func TestGetAlertHygiene_SendsRFC3339RangeAndTimezone(t *testing.T) {
	var method, path, from, to, timezone string
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		q := r.URL.Query()
		method, path, from, to, timezone = r.Method, r.URL.Path, q.Get("from"), q.Get("to"), q.Get("timezone")
		_, _ = w.Write([]byte(`{"rules":[]}`))
	})

	if _, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if method != http.MethodGet || path != "/alerts/hygiene" {
		t.Errorf("request = %s %s, want GET /alerts/hygiene", method, path)
	}
	if from != "2026-10-27T00:00:00Z" || to != "2026-11-03T00:00:00Z" || timezone != "UTC" {
		t.Errorf("from, to, timezone = %q, %q, %q", from, to, timezone)
	}
}

func TestGetAlertHygiene_DecodesAvailabilityCoverageAndTruncation(t *testing.T) {
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{
			"period":{"from":"2026-10-27T00:00:00Z","to":"2026-11-03T00:00:00Z","timezone":"UTC"},
			"coverage":{"lifecycle":{"status":"partial","range":{"from":"2026-10-30T00:00:00Z","to":"2026-11-03T00:00:00Z"},"reason":"retention"},
			            "delivery":{"status":"unavailable","reason":"no ledger"}},
			"rules":[
				{"rule_id":"r-zero","alert_group_id":"g1","fire_count":{"status":"available","count":0},
				 "flaps_per_day":{"status":"available","value":0},"ack_rate":{"status":"available","value":0},
				 "last_fired_at":"2026-10-01T00:00:00Z","routed":false},
				{"rule_id":"r-unavailable","fire_count":{"status":"unavailable","reason":"no ledger"},
				 "flaps_per_day":{"status":"unavailable"},"ack_rate":{"status":"unavailable"}}
			],
			"truncated":true}`))
	})

	resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput)
	if err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if !resp.Truncated || !resp.Period.To.Equal(testHygieneInput.To) || len(resp.Rules) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if lc := resp.Coverage.Lifecycle; lc.Status != HygieneStatusPartial || lc.Range == nil || lc.Range.From.Day() != 30 {
		t.Errorf("lifecycle coverage = %+v", lc)
	}
	zero := resp.Rules[0]
	if zero.FireCount.Count == nil || *zero.FireCount.Count != 0 || zero.AckRate.Value == nil || *zero.AckRate.Value != 0 {
		t.Errorf("observed zeros decoded as %+v", zero)
	}
	if zero.LastFiredAt == nil || zero.Routed == nil || *zero.Routed {
		t.Errorf("last_fired_at, routed = %v, %v", zero.LastFiredAt, zero.Routed)
	}
	unavailable := resp.Rules[1]
	if unavailable.FireCount.Count != nil || unavailable.FlapsPerDay.Value != nil || unavailable.AckRate.Value != nil ||
		unavailable.LastFiredAt != nil || unavailable.Routed != nil {
		t.Errorf("unavailable values decoded as observed: %+v", unavailable)
	}
}

// The tool reads an empty rules list as "nothing to fix", so a body without one must fail.
func TestGetAlertHygiene_RejectsResponseWithoutRules(t *testing.T) {
	for _, body := range []string{``, `null`, `{}`, `{"truncated":false}`, `{"rules":null}`} {
		t.Run(body, func(t *testing.T) {
			c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(body))
			})
			if resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err == nil {
				t.Fatalf("expected an error, got %+v", resp)
			}
		})
	}
}

func TestGetAlertHygiene_AcceptsEmptyRules(t *testing.T) {
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"rules":[]}`))
	})
	resp, err := GetAlertHygiene(context.Background(), c, testHygieneInput)
	if err != nil {
		t.Fatalf("GetAlertHygiene: %v", err)
	}
	if resp.Rules == nil || len(resp.Rules) != 0 {
		t.Fatalf("rules = %#v, want empty", resp.Rules)
	}
}

func TestGetAlertHygiene_UpstreamErrorSurfaces(t *testing.T) {
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := GetAlertHygiene(context.Background(), c, testHygieneInput); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestGetHygieneRequestAndResponse(t *testing.T) {
	var got *http.Request
	c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{"coverage":{"lifecycle":{"status":"partial"}},"truncated":true,"rules":[{"alert_group_id":"g1","rule_id":"r1","rule_type":"alert_rule","rule_name":"High error rate","service":"api","severity":"P1","group_labels":{"team":"payments"},"fire_count":{"status":"available","count":12},"flap_count":{"status":"available","count":3},"flaps_per_day":{"status":"available","value":1.5},"time_firing_seconds":{"status":"partial","count":60,"reason":"ledger gap"},"ack_rate":{"status":"available","value":0.5},"median_time_to_ack_seconds":{"status":"unavailable","reason":"no acks"},"notification_attempts":{"status":"available","count":4},"routed":false}]}`)
	})

	resp, err := GetHygiene(context.Background(), c, GetHygieneInput{
		From: testFrom, To: testTo, Timezone: "UTC", Services: []string{"api", "web"},
		Labels: map[string]string{"team": "payments"}, GroupBy: "team",
	})
	if err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Path != "/alerts/hygiene" || q.Get("from") != "2026-10-16T00:00:00Z" || q.Get("to") != "2026-10-17T00:00:00Z" || q.Get("timezone") != "UTC" || len(q["services"]) != 2 || q.Has("severities") || q.Get("label.team") != "payments" || q.Get("group_by") != "team" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules = %+v", resp.Rules)
	}
	r := resp.Rules[0]
	if r.AlertGroupID != "g1" || r.RuleID != "r1" || r.RuleType != "alert_rule" || r.Service != "api" || r.GroupLabels["team"] != "payments" || *r.Routed {
		t.Errorf("rule = %+v", r)
	}
	if *r.FireCount.Count != 12 || *r.FlapCount.Count != 3 || *r.FlapsPerDay.Value != 1.5 || r.TimeFiringSeconds.Status != "partial" || *r.NotificationAttempts.Count != 4 {
		t.Errorf("counts = %+v", r)
	}
	if *r.AckRate.Value != 0.5 || r.MedianTimeToAckSeconds.Status != "unavailable" || r.MedianTimeToAckSeconds.Value != nil {
		t.Errorf("ack = %+v %+v", r.AckRate, r.MedianTimeToAckSeconds)
	}
	if !resp.Truncated || string(resp.Coverage) != `{"lifecycle":{"status":"partial"}}` {
		t.Errorf("truncated = %v, coverage = %s", resp.Truncated, resp.Coverage)
	}
}

func TestGetHygieneRejectsMissingRulesList(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "no rules": `{}`, "null rules": `{"rules":null}`} {
		t.Run(name, func(t *testing.T) {
			c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			if _, err := GetHygiene(context.Background(), c, GetHygieneInput{From: testFrom, To: testTo}); err == nil || !strings.Contains(err.Error(), "no rules list") {
				t.Errorf("err = %v", err)
			}
		})
	}
}
