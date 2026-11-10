package main

import (
	"encoding/json"
	"regexp"
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
		for _, header := range []string{"", "dashboards"} {
			if _, body := postRPC(t, ts, header, "", call, nil); !strings.Contains(body, "user token required") || strings.Contains(body, "gridPos") {
				t.Errorf("%s (toolsets header %q): tokenless call was not refused for its token: %s", tool, header, body)
			}
		}
	}
}

func TestDashboardArgumentCheckRunsAfterIntegerCoercion(t *testing.T) {
	session := newInMemoryClientSession(t)
	args := `{"dashboard":{"name":"x","panels":[{"name":"p","version":"1","gridPos":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[]}]}}`
	result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "create_dashboard", Arguments: json.RawMessage(args)})
	assertToolError(t, result, toolErrorExpectation{
		contains: []string{"dashboard.panels[0].gridPos: unknown panel field; use layout"},
		absent:   []string{"version"},
	})
}

// Query-free panels ignore targets, so it is flagged only where queries are required.
func TestDashboardToolsFlagTargetsOnlyOnQueryBackedPanels(t *testing.T) {
	session := newInMemoryClientSession(t)
	layout := `"layout":{"x":0,"y":0,"w":6,"h":4}`
	panels := map[string]string{
		"section":    `{"name":"s",` + layout + `,"visualization":{"type":"section"},"targets":[]}`,
		"markdown":   `{"name":"m",` + layout + `,"visualization":{"type":"markdown"},"targets":[]}`,
		"csv":        `{"name":"c",` + layout + `,"visualization":{"type":"table"},"csv":{},"targets":[]}`,
		"timeseries": `{"name":"t",` + layout + `,"visualization":{"type":"timeseries"},"targets":[]}`,
	}
	calls := map[string]func(panel string) string{
		"create_dashboard": func(p string) string { return `{"dashboard":{"name":"x","panels":[` + p + `]}}` },
		"update_dashboard": func(p string) string { return `{"id":"dash-1","dashboard":{"name":"x","panels":[` + p + `]}}` },
		"patch_dashboard":  func(p string) string { return `{"id":"dash-1","ops":[{"op":"add_panel","panel":` + p + `}]}` },
		"validate_dashboard": func(p string) string {
			return `{"start_time_iso":"2026-01-01T00:00:00Z","end_time_iso":"2026-01-01T01:00:00Z","dashboard_definition":{"name":"x","panels":[` + p + `]}}`
		},
	}
	for kind, panel := range panels {
		for tool, args := range calls {
			t.Run(kind+"/"+tool, func(t *testing.T) {
				result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args(panel))})
				flagged := strings.Contains(toolResultText(result), "targets: unknown panel field; use queries")
				if want := kind == "timeseries"; flagged != want {
					t.Errorf("targets flagged = %v, want %v: %s", flagged, want, toolResultText(result))
				}
			})
		}
	}
}

// Shape checks follow the input schema's anyOf branches, including properties only a branch declares.
func TestDashboardToolsCheckPanelsAgainstEveryAnyOfBranch(t *testing.T) {
	session := newInMemoryClientSession(t)
	panel := func(fields string) string { return `{"name":"p","layout":{"x":0,"y":0,"w":6,"h":4},` + fields + `}` }
	cases := map[string]struct{ panel, want string }{
		"section":                   {panel(`"visualization":{"type":"section"}`), ""},
		"markdown":                  {panel(`"visualization":{"type":"markdown"}`), ""},
		"csv object":                {panel(`"visualization":{"type":"table"},"csv":{}`), ""},
		"csv null":                  {panel(`"visualization":{"type":"table"},"csv":null`), "csv: got null, want object"},
		"csv string":                {panel(`"visualization":{"type":"table"},"csv":"data.csv"`), "csv: got string, want object"},
		"csv array":                 {panel(`"visualization":{"type":"table"},"csv":[]`), "csv: got array, want object"},
		"timeseries targets":        {panel(`"visualization":{"type":"timeseries"},"targets":[]`), "targets: unknown panel field; use queries"},
		"no visualization, targets": {panel(`"targets":[]`), "targets: unknown panel field; use queries"},
	}
	calls := map[string]func(panel string) string{
		"create_dashboard": func(p string) string { return `{"dashboard":{"name":"x","panels":[` + p + `]}}` },
		"update_dashboard": func(p string) string { return `{"id":"dash-1","dashboard":{"name":"x","panels":[` + p + `]}}` },
		"patch_dashboard":  func(p string) string { return `{"id":"dash-1","ops":[{"op":"add_panel","panel":` + p + `}]}` },
		"validate_dashboard": func(p string) string {
			return `{"start_time_iso":"2026-01-01T00:00:00Z","end_time_iso":"2026-01-01T01:00:00Z","dashboard_definition":{"name":"x","panels":[` + p + `]}}`
		},
	}
	shapeProblem := regexp.MustCompile(`panels?(\[0\])?[.\w\[\]]*: (got |unknown |missing;|want one of)`)
	for name, c := range cases {
		for tool, args := range calls {
			t.Run(name+"/"+tool, func(t *testing.T) {
				text := toolResultText(callRegisteredTool(t, session, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args(c.panel))}))
				if c.want == "" && shapeProblem.MatchString(text) {
					t.Errorf("valid panel got a shape problem: %s", text)
				}
				if c.want != "" && !strings.Contains(text, c.want) {
					t.Errorf("missing %q: %s", c.want, text)
				}
			})
		}
	}
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
