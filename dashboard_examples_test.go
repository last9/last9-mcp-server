package main

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
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
			layout, hasLayout := panel["layout"].(map[string]any)
			if !hasLayout {
				if panel["visualization"].(map[string]any)["type"] != "section" {
					t.Errorf("panel %v has no layout", panel["name"])
				}
				continue
			}
			x, y, w, h := layout["x"].(float64), layout["y"].(float64), layout["w"].(float64), layout["h"].(float64)
			if x < 0 || y < 0 || w <= 0 || h <= 0 || x+w > 12 {
				t.Errorf("panel %v exceeds grid", panel["name"])
			}
			for _, previous := range panels[:i] {
				p, ok := previous.(map[string]any)["layout"].(map[string]any)
				if !ok {
					continue
				}
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

func TestDashboardExamplesTopNTable(t *testing.T) {
	session, ctx := dashboardPromptSession(t)
	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "dashboard_examples"})
	if err != nil {
		t.Fatal(err)
	}
	body := result.Messages[0].Content.(*mcp.TextContent).Text
	blocks := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllStringSubmatch(body, -1)
	if len(blocks) < 2 {
		t.Fatal("missing top-N table JSON example")
	}
	var envelope struct {
		Dashboard struct {
			Panels []struct {
				Visualization struct {
					Type        string `json:"type"`
					TableConfig struct {
						ColumnConfig []struct {
							ID      string `json:"id"`
							Visible bool   `json:"visible"`
							Order   int    `json:"order"`
						} `json:"columnConfig"`
						Sorting []struct {
							ID   string `json:"id"`
							Desc bool   `json:"desc"`
						} `json:"sorting"`
					} `json:"table_config"`
				} `json:"visualization"`
				Queries []struct {
					Name      string `json:"name"`
					Type      string `json:"type"`
					QueryType string `json:"query_type"`
					Expr      string `json:"expr"`
				} `json:"queries"`
			} `json:"panels"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(blocks[1][1]), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Dashboard.Panels) != 1 {
		t.Fatalf("top-N example has %d panels, want 1", len(envelope.Dashboard.Panels))
	}
	panel := envelope.Dashboard.Panels[0]
	if panel.Visualization.Type != "table" {
		t.Fatalf("visualization type = %q, want table", panel.Visualization.Type)
	}
	wantColumns := []string{"key", "Total", "OK", "OK Rate", "Error", "Error Rate"}
	var columns []string
	for order, column := range panel.Visualization.TableConfig.ColumnConfig {
		columns = append(columns, column.ID)
		if !column.Visible || column.Order != order {
			t.Errorf("column %q has visible=%v order=%d, want true/%d", column.ID, column.Visible, column.Order, order)
		}
	}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Errorf("column order = %v, want %v", columns, wantColumns)
	}
	if got := panel.Visualization.TableConfig.Sorting; len(got) != 1 || got[0].ID != "Total" || !got[0].Desc {
		t.Errorf("sorting = %#v, want Total descending", got)
	}
	wantQueries := wantColumns[1:]
	var queries []string
	for _, query := range panel.Queries {
		queries = append(queries, query.Name)
		if query.Type != "instant" || query.QueryType != "promql" {
			t.Errorf("query %q has type=%q query_type=%q", query.Name, query.Type, query.QueryType)
		}
		if !strings.Contains(query.Expr, "topk(10, sum by (key) (requests_total))") {
			t.Errorf("query %q is not constrained to the shared top-10 set: %s", query.Name, query.Expr)
		}
		if query.Name != "Total" && (!strings.Contains(query.Expr, " and topk(") || !strings.Contains(query.Expr, ") or 0 * topk(")) {
			t.Errorf("query %q does not join and zero-fill the shared top-10 set: %s", query.Name, query.Expr)
		}
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Errorf("query names = %v, want column headers %v", queries, wantQueries)
	}
	for _, query := range panel.Queries {
		if strings.Contains(query.Name, "Rate") && !strings.HasPrefix(query.Expr, "round(100 * ") {
			t.Errorf("rate query %q is not rounded percentage: %s", query.Name, query.Expr)
		}
	}
}
