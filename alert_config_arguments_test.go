package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/alerting"
	"last9-mcp/internal/prompts"
)

func TestAlertConfigRuleArguments(t *testing.T) {
	session := connectToolFixture(t, registeredToolFixture{tool: &mcp.Tool{Name: "get_alert_config", Description: prompts.GetAlertConfigDescription}}, func(_ context.Context, _ *mcp.CallToolRequest, in alerting.GetAlertConfigArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.RuleID + "/" + in.Severity}}}, nil, nil
	})
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := toolByName(t, list.Tools, "get_alert_config")
	schema := schemaAsMap(t, tool.InputSchema)
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["alert_rule_id"]; !ok {
		t.Fatal("get_alert_config schema does not expose alert_rule_id")
	}
	for name, description := range map[string]string{
		"tool":    tool.Description,
		"rule_id": properties["rule_id"].(map[string]any)["description"].(string),
	} {
		for _, behavior := range []string{"case-insensitive", "spaces"} {
			if !strings.Contains(strings.ToLower(description), behavior) {
				t.Errorf("%s description does not document %s matching: %q", name, behavior, description)
			}
		}
	}
	for _, tc := range []struct {
		args, want string
		invalid    bool
	}{
		{`{"rule_id":"rule","severity":"breach"}`, "rule/breach", false},
		{`{"alert_rule_id":"rule","severity":"breach"}`, "rule/breach", false},
		{`{"alert_rule_id":"rule","rule_id":"rule"}`, "rule/", false},
		{`{"alert_rule_id":"RULE","rule_id":" rule "}`, "/", false},
		{`{"alert_rule_id":"other","rule_id":"rule"}`, "conflicting", true},
		{`{"alert_rule_id":"rule","rule_id":null}`, "rule_id must be a nonempty string", true},
		{`{"alert_rule_id":"rule","rule_id":" "}`, "rule_id must be a nonempty string", true},
		{`{"alert_rule_id":null}`, "nonempty string", true},
		{`{"alert_rule_id":42}`, "nonempty string", true},
		{`{"alert_rule_id":" "}`, "nonempty string", true},
		{`{"alert_id":"instance"}`, `"alert_id"`, true},
		{`{"alert_id":"instance"}`, "Rule ID by get_alerts", true},
		{`{"alert_rule_id":"rule","unknown":1}`, "unknown", true},
	} {
		result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_alert_config", Arguments: json.RawMessage(tc.args)})
		if result.IsError != tc.invalid || !strings.Contains(toolResultText(result), tc.want) {
			t.Errorf("%s: %s", tc.args, toolResultText(result))
		}
	}
}
