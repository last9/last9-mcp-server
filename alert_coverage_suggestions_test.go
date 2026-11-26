package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every key get_alert_coverage puts in suggested_args must be one its suggested_tool
// accepts, with a value that tool's served schema allows, so following the hint never
// fails schema validation.
func TestAlertCoverageSuggestedArgsMatchSuggestedToolSchemas(t *testing.T) {
	const gaps = `[
		{"kind":"uncovered_signal","entity_id":"ent-1","service":"checkout","signal":"latency","suggested_tool":"create_alert"},
		{"kind":"uncovered_signal","entity_id":"ent-1","service":"checkout","signal":"latency","suggested_tool":"create_alert_from_chart"},
		{"kind":"uncovered_signal","entity_id":"ent-1","service":"checkout","signal":"error_rate","rule_id":"rule-2","suggested_tool":"patch_alert"},
		{"kind":"all_rules_disabled","entity_id":"ent-2","suggested_tool":"patch_alert"},
		{"kind":"no_notification_channel","entity_id":"ent-3","rule_id":"rule-5","severity":"breach","suggested_tool":"attach_notification_channel"},
		{"kind":"no_notification_channel","entity_id":"ent-3","rule_id":"rule-6","severity":"threat","suggested_tool":"attach_notification_channel"},
		{"kind":"muted_too_long","entity_id":"ent-4","rule_id":"rule-9","severity":"breach","muted_since":1690000000,"mute_until":-1,"suggested_tool":"patch_alert"}]`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"generated_at":1700000000,"muted_days":7,"gaps":` + gaps + `,"counts":{"uncovered_signal":3}}`))
	}))
	t.Cleanup(upstream.Close)

	session := connectTestSession(t, writeToolTestConfig(upstream.URL), upstream.Client())
	ctx := context.Background()
	schemas := map[string]*jsonschema.Schema{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s schema: %v", tool.Name, err)
		}
		schemas[tool.Name] = &schema
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_alert_coverage", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("get_alert_coverage: err=%v result=%+v", err, result)
	}
	var out struct {
		Groups []struct {
			Gaps []struct {
				SuggestedTool string         `json:"suggested_tool"`
				SuggestedArgs map[string]any `json:"suggested_args"`
			} `json:"gaps"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, group := range out.Groups {
		for _, gap := range group.Gaps {
			schema, ok := schemas[gap.SuggestedTool]
			if !ok {
				t.Errorf("suggested_tool %q is not served", gap.SuggestedTool)
				continue
			}
			if len(gap.SuggestedArgs) == 0 {
				t.Errorf("%s gap has no suggested_args", gap.SuggestedTool)
			}
			for key, value := range gap.SuggestedArgs {
				prop, ok := schema.Properties[key]
				if !ok {
					t.Errorf("%s does not accept suggested arg %q", gap.SuggestedTool, key)
					continue
				}
				resolved, err := prop.Resolve(nil)
				if err != nil {
					t.Fatalf("%s.%s schema: %v", gap.SuggestedTool, key, err)
				}
				if err := resolved.Validate(value); err != nil {
					t.Errorf("%s.%s = %v: %v", gap.SuggestedTool, key, value, err)
				}
			}
			checked++
		}
	}
	if checked != 7 {
		t.Errorf("checked %d gaps, want 7", checked)
	}
}
