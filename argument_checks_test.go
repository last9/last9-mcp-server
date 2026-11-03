package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const grafanaPanel = `{"title":"p","type":"timeseries","gridPos":{"x":0,"y":0,"w":24,"h":8},"queries":[{"legend":"l","query":"up","type":"prometheus"}]}`

func TestDashboardWritesNameEveryWrongFieldBeforeSchemaValidation(t *testing.T) {
	session := newInMemoryClientSession(t)
	for tool, args := range map[string]string{
		"create_dashboard": `{"dashboard":{"name":"x","panels":[` + grafanaPanel + `]},"metadata":{"_category":"custom","_type":"metrics"}}`,
		"update_dashboard": `{"id":"dash-1","dashboard":{"name":"x","panels":[` + grafanaPanel + `]}}`,
	} {
		t.Run(tool, func(t *testing.T) {
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
			assertToolError(t, result, toolErrorExpectation{
				contains: []string{
					"dashboard.panels[0].gridPos: unknown panel field; use layout {x, y, w, h}",
					"dashboard.panels[0].title: unknown panel field; use name",
					"dashboard.panels[0].queries[0].legend: got string, want object {type, value}",
					"dashboard does not match the dashboard schema; nothing was written",
				},
				absent: []string{"validating /properties"},
			})
		})
	}
}

func TestDashboardWritesNameEveryConditionallyRequiredField(t *testing.T) {
	session := newInMemoryClientSession(t)
	logsPanel := `{"name":"a","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"logs"},"queries":[]}`
	panels := `"panels":[` + logsPanel + "," + logsPanel + `]`
	for tool, args := range map[string]string{
		"create_dashboard": `{"dashboard":{"name":"x",` + panels + `}}`,
		"update_dashboard": `{"id":"dash-1","dashboard":{"name":"x",` + panels + `}}`,
	} {
		t.Run(tool, func(t *testing.T) {
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
			assertToolError(t, result, toolErrorExpectation{
				contains: []string{
					"dashboard.panels[0].visualization.logs_config: missing; want object {columns}",
					"dashboard.panels[1].visualization.logs_config: missing; want object {columns}",
				},
				absent: []string{"validating /properties"},
			})
		})
	}
}

func TestDashboardWritesNameEveryFractionalIntegerField(t *testing.T) {
	session := newInMemoryClientSession(t)
	logsPanel := `{"name":"a","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"logs","logs_config":{"columns":["body"],"row_limit":1.5}},"queries":[]}`
	panels := `"panels":[` + logsPanel + "," + logsPanel + `]`
	for tool, args := range map[string]string{
		"create_dashboard": `{"dashboard":{"name":"x",` + panels + `}}`,
		"update_dashboard": `{"id":"dash-1","dashboard":{"name":"x",` + panels + `}}`,
	} {
		t.Run(tool, func(t *testing.T) {
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
			assertToolError(t, result, toolErrorExpectation{
				contains: []string{
					"dashboard.panels[0].visualization.logs_config.row_limit: got number, want integer",
					"dashboard.panels[1].visualization.logs_config.row_limit: got number, want integer",
				},
				absent: []string{"validating /properties"},
			})
		})
	}
}

func TestDashboardArgumentCheckLeavesOtherShapesAndToolsToTheSchema(t *testing.T) {
	session := newInMemoryClientSession(t)
	badDashboard := `{"name":"x","panels":[` + grafanaPanel + `]}`
	for tool, args := range map[string]string{
		"create_dashboard": `{"dashboard":"x","metadata":{"_category":"custom","_type":"metrics"}}`,
		"patch_dashboard":  `{"id":"dash-1","ops":[],"dashboard":` + badDashboard + `}`,
	} {
		t.Run(tool, func(t *testing.T) {
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
			assertToolError(t, result, toolErrorExpectation{absent: []string{"does not match the dashboard schema"}})
		})
	}
}

// A strict --http server refuses a tokenless call before reading its arguments.
func TestStrictModeRefusesTokenlessDashboardWritesBeforeArgumentChecks(t *testing.T) {
	cfg := testToolRegistrationConfig()
	cfg.HTTPMode = true
	cfg.UserTokenFallback = false
	ts, _ := newToolsetHTTPServerFor(t, cfg, "all")
	for tool, args := range map[string]string{
		"create_dashboard": `{"dashboard":{"name":"x","panels":[` + grafanaPanel + `]}}`,
		"update_dashboard": `{"id":"dash-1","dashboard":{"name":"x","panels":[` + grafanaPanel + `]}}`,
	} {
		call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
		if _, body := postRPC(t, ts, "", "", call, nil); !strings.Contains(body, "user token required") || strings.Contains(body, "gridPos") {
			t.Errorf("%s: tokenless call was not refused for its token: %s", tool, body)
		}
	}
}

func TestDashboardArgumentCheckRunsAfterIntegerCoercion(t *testing.T) {
	session := newInMemoryClientSession(t)
	args := `{"dashboard":{"name":"x","panels":[{"name":"p","version":"1","gridPos":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"section"}}]}}`
	result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "create_dashboard", Arguments: json.RawMessage(args)})
	assertToolError(t, result, toolErrorExpectation{
		contains: []string{"dashboard.panels[0].gridPos: unknown panel field; use layout"},
		absent:   []string{"version"},
	})
}

func TestDashboardWritesNameEveryMistypedAlternativeField(t *testing.T) {
	session := newInMemoryClientSession(t)
	panel := func(name, fields string) string {
		return `{"name":"` + name + `","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},` + fields + `}`
	}
	query := `{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""}}`
	for tool, prefix := range map[string]string{
		"create_dashboard": `{"dashboard":`,
		"update_dashboard": `{"id":"dash-1","dashboard":`,
	} {
		t.Run(tool+"/invalid csv", func(t *testing.T) {
			args := prefix + `{"name":"x","panels":[` + panel("a", `"csv":"bad"`) + "," + panel("b", `"csv":"bad"`) + `]}}`
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
			assertToolError(t, result, toolErrorExpectation{
				contains: []string{
					"dashboard.panels[0].csv: got string, want object",
					"dashboard.panels[1].csv: got string, want object",
				},
				absent: []string{"validating /properties"},
			})
		})
		for name, fields := range map[string]string{
			"valid csv":         `"csv":{"columns":{}}`,
			"queries satisfied": `"csv":"bad","queries":[` + query + `]`,
		} {
			t.Run(tool+"/"+name, func(t *testing.T) {
				args := prefix + `{"name":"x","panels":[` + panel("a", fields) + `]}}`
				result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
				if text := toolResultText(result); strings.Contains(text, "does not match the dashboard schema") || strings.Contains(text, "validating /properties") {
					t.Errorf("valid alternative refused: %s", text)
				}
			})
		}
	}
}
