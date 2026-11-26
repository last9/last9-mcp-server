package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetAlertsInstanceLimit(t *testing.T) {
	rules := []AlertRuleData{
		{RuleID: "r1", Alerts: []AlertInstance{{LabelHash: "one", State: "normal"}, {LabelHash: "two", State: "firing"}}},
		{RuleID: "r2", Alerts: []AlertInstance{{LabelHash: "three", State: "firing"}, {LabelHash: "four", State: "firing"}}},
		{RuleID: "r3", Alerts: []AlertInstance{{LabelHash: "five", State: "normal"}}},
	}
	limit := func(n int) *int { return &n }
	for _, tc := range []struct {
		name           string
		limit          *int
		state, heading string
		hashes, ids    []string
	}{
		{"omit", nil, "", "Found 3 alert rule(s) with 5 alert instance(s)", []string{"one", "two", "three", "four", "five"}, []string{"r1", "r2", "r3"}},
		{"cut inside second rule", limit(3), "", "Showing 3 of 5 alert instance(s) across 2 of 3 alert rule(s)", []string{"one", "two", "three"}, []string{"r1", "r2"}},
		{"exact rule boundary", limit(2), "", "Showing 2 of 5 alert instance(s) across 1 of 3 alert rule(s)", []string{"one", "two"}, []string{"r1"}},
		{"after state filter", limit(2), "firing", "Showing 2 of 3 alert instance(s) across 2 of 2 alert rule(s)", []string{"two", "three"}, []string{"r1", "r2"}},
		{"over total", limit(20), "normal", "Showing 2 of 2 alert instance(s) across 2 of 2 alert rule(s)", []string{"one", "five"}, []string{"r1", "r3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("limit") || r.URL.Query().Get("state") != tc.state {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				var matched []AlertRuleData
				for _, rule := range rules {
					var instances []AlertInstance
					for _, instance := range rule.Alerts {
						if tc.state == "" || tc.state == instance.State {
							instances = append(instances, instance)
						}
					}
					if len(instances) > 0 {
						rule.Alerts = instances
						matched = append(matched, rule)
					}
				}
				_ = json.NewEncoder(w).Encode(AlertsResponse{AlertRules: matched})
			}))
			defer srv.Close()
			res, _, err := NewGetAlertsHandler(srv.Client(), alertsMonitorTestConfig(srv.URL))(context.Background(), nil, GetAlertsArgs{Limit: tc.limit, State: tc.state})
			if err != nil {
				t.Fatal(err)
			}
			out := res.Content[0].(*mcp.TextContent).Text
			var hashes, ids []string
			for line := range strings.SplitSeq(out, "\n") {
				if hash, ok := strings.CutPrefix(line, "      Label Hash: "); ok {
					hashes = append(hashes, hash)
				}
				if id, ok := strings.CutPrefix(line, "  Rule ID: "); ok {
					ids = append(ids, id)
				}
			}
			if !strings.Contains(out, tc.heading) || !reflect.DeepEqual(hashes, tc.hashes) || !reflect.DeepEqual(ids, tc.ids) {
				t.Errorf("unexpected output:\n%s", out)
			}
		})
	}
}

func TestGetAlertsRejectsNonpositiveLimitBeforeFetch(t *testing.T) {
	handler := NewGetAlertsHandler(nil, alertsMonitorTestConfig(""))
	for _, limit := range []int{0, -1} {
		_, _, err := handler(context.Background(), nil, GetAlertsArgs{Limit: &limit})
		if err == nil || !strings.Contains(err.Error(), "limit must be a positive integer") {
			t.Errorf("limit=%d: %v", limit, err)
		}
	}
}
