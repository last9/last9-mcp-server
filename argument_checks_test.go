package main

import (
	"encoding/json"
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
					"nothing was written",
				},
				absent: []string{"validating /properties"},
			})
		})
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
