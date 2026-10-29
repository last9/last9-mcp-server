package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAlertRuleStateHandler_EmptyIdentityFiltersThroughTransport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		filter    string
		value     string
		match     bool
		wantError bool
	}{
		{name: "unknown group", filter: "alert_group_id", value: "missing-group", wantError: true},
		{name: "unknown rule", filter: "rule_name", value: "^missing-rule$", wantError: true},
		{name: "unmatched group name", filter: "alert_group_name", value: "^missing-group$", wantError: true},
		{name: "unfiltered empty"},
		{name: "no firing rules", filter: "state", value: "firing"},
		{name: "no matching labels", filter: "label_filters", value: "env=test"},
		{name: "rule appears in later sample", filter: "rule_name", value: "^test-rule$", match: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if tc.filter != "" && r.URL.Query().Get(tc.filter) != tc.value {
					t.Errorf("filter %s = %q, want %q", tc.filter, r.URL.Query().Get(tc.filter), tc.value)
				}
				if tc.match && r.URL.Query().Get("timestamp") == "1718000060" {
					_, _ = w.Write([]byte(`{"alert_rules":[{"rule_id":"r","state":"firing"}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"alert_rules":[]}`))
			}))
			defer upstream.Close()
			server := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "v"}, nil)
			mcp.AddTool(server, &mcp.Tool{Name: "get_alert_rule_state", Description: "x", InputSchema: AlertRuleStateInputSchema()},
				NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL)))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			t1, t2 := mcp.NewInMemoryTransports()
			if _, err := server.Connect(ctx, t1, nil); err != nil {
				t.Fatal(err)
			}
			session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v"}, nil).Connect(ctx, t2, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			args := map[string]any{"start_time": 1718000000, "end_time": 1718000060, "step": 60}
			if tc.filter != "" {
				args[tc.filter] = tc.value
			}
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_alert_rule_state", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tc.wantError {
				t.Fatalf("IsError = %v, want %v: %+v", res.IsError, tc.wantError, res.Content)
			}
			if hits.Load() != 2 {
				t.Fatalf("upstream calls = %d, want 2", hits.Load())
			}
			if tc.wantError {
				text := res.Content[0].(*mcp.TextContent).Text
				for _, want := range []string{"No alert rules matched", "sampled time range", "get_alert_config", "filters"} {
					if !strings.Contains(text, want) {
						t.Errorf("message %q lacks %q", text, want)
					}
				}
				return
			}
			got := decodeAlertRuleStateResult(t, res)
			if !tc.match {
				if len(got) != 0 {
					t.Fatalf("want empty result, got %v", got)
				}
				return
			}
			series := got["r"]
			if len(series) != 2 || series[0].IsFiring != 0 || series[1].IsFiring != 1 {
				t.Fatalf("want zero-filled first sample and firing second sample, got %v", series)
			}
		})
	}
}
