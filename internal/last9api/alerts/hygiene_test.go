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
		_, _ = io.WriteString(w, `{"rules":[{"rule_id":"r1","rule_name":"High error rate","fire_count":12,"flaps_per_day":1.5,"routed":false,"firing_seconds":60,"ack":{"ack_rate":0.5,"median_seconds_to_ack":null}}]}`)
	})

	resp, err := GetHygiene(context.Background(), c, GetHygieneInput{
		From: 1700000000, To: 1700086400, Timezone: "UTC", Services: []string{"api", "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Path != "/alerts/hygiene" || q.Get("from") != "1700000000" || q.Get("to") != "1700086400" || q.Get("timezone") != "UTC" || len(q["services"]) != 2 || q.Has("severities") {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules = %+v", resp.Rules)
	}
	r := resp.Rules[0]
	if r.RuleID != "r1" || *r.FireCount != 12 || *r.FlapsPerDay != 1.5 || *r.Routed || *r.FiringSeconds != 60 || *r.Ack.AckRate != 0.5 || r.Ack.MedianSecondsToAck != nil {
		t.Errorf("rule = %+v", r)
	}
}

func TestGetHygieneRejectsMissingRulesList(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "no rules": `{}`, "null rules": `{"rules":null}`} {
		t.Run(name, func(t *testing.T) {
			c := newAlertsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			if _, err := GetHygiene(context.Background(), c, GetHygieneInput{From: 1, To: 2}); err == nil || !strings.Contains(err.Error(), "no rules list") {
				t.Errorf("err = %v", err)
			}
		})
	}
}
