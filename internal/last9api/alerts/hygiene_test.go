package alerts

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
