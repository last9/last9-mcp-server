package main

import (
	"encoding/json"
	"regexp"
	"testing"

	"last9-mcp/internal/dashboards"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDashboardExamplesMatchCreateSchema(t *testing.T) {
	session, ctx := dashboardPromptSession(t)
	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "dashboard_examples"})
	if err != nil {
		t.Fatal(err)
	}
	body := result.Messages[0].Content.(*mcp.TextContent).Text
	blocks := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllStringSubmatch(body, -1)
	if len(blocks) == 0 {
		t.Fatal("no worked JSON dashboard examples")
	}
	raw, err := json.Marshal(dashboards.GetCreateDashboardInputSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range blocks {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(block[1]), &envelope); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(envelope); err != nil {
			t.Fatalf("example violates create schema: %v", err)
		}
		dashboard := envelope["dashboard"].(map[string]any)
		panels := dashboard["panels"].([]any)
		for i, item := range panels {
			panel := item.(map[string]any)
			layout := panel["layout"].(map[string]any)
			x, y, w, h := layout["x"].(float64), layout["y"].(float64), layout["w"].(float64), layout["h"].(float64)
			if x < 0 || y < 0 || w <= 0 || h <= 0 || x+w > 12 {
				t.Errorf("panel %v exceeds grid", panel["name"])
			}
			for _, previous := range panels[:i] {
				p := previous.(map[string]any)["layout"].(map[string]any)
				if x < p["x"].(float64)+p["w"].(float64) && x+w > p["x"].(float64) && y < p["y"].(float64)+p["h"].(float64) && y+h > p["y"].(float64) {
					t.Errorf("panel %v overlaps an earlier panel", panel["name"])
				}
			}
			queries, _ := panel["queries"].([]any)
			for _, item := range queries {
				query := item.(map[string]any)
				qt := query["query_type"].(string)
				telemetry := map[string]string{"promql": "metrics", "log_json": "logs", "trace_json": "traces"}[qt]
				if telemetry == "" || query["telemetry"] != telemetry {
					t.Errorf("invalid query/telemetry pair: %v", query)
				}
				if qt == "promql" {
					continue
				}
				var pipeline []map[string]any
				if err := json.Unmarshal([]byte(query["expr"].(string)), &pipeline); err != nil {
					t.Fatal(err)
				}
				if len(pipeline) < 2 || pipeline[0]["type"] != "filter" {
					t.Fatalf("query must scope before aggregation: %v", pipeline)
				}
				if qt == "log_json" && pipeline[len(pipeline)-1]["type"] != "window_aggregate" {
					t.Error("log trend must use window_aggregate")
				}
			}
		}
		variables := dashboard["variables"].([]any)
		seen := map[string]bool{}
		for _, item := range variables {
			variable := item.(map[string]any)
			target := variable["target"].(string)
			matches, ok := variable["matches"].([]any)
			if !ok {
				t.Fatalf("variable %s needs matches array", target)
			}
			for _, match := range matches {
				for _, dependency := range regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z_0-9]*)`).FindAllStringSubmatch(match.(string), -1) {
					if !seen[dependency[1]] {
						t.Errorf("variable %s depends on missing/later variable %s", target, dependency[1])
					}
				}
			}
			seen[target] = true
		}
	}
}
