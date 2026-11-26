package main

import (
	"context"
	"regexp"
	"testing"

	"last9-mcp/internal/workflows"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// author-alert is served under every toolset, but its home is alerts: every
// tool it tells the agent to call must be served there.
func TestAuthorAlertNamesOnlyAlertsToolsetTools(t *testing.T) {
	registry := servedDescriptions(t, "")
	served := servedDescriptions(t, "alerts")
	for _, args := range []map[string]string{
		{"goal": "checkout p99 latency"},
		{"goal": "panel latency", "dashboard_id": "dash-1", "panel": "p99"},
	} {
		res, err := workflows.AuthorAlert.Handler(context.Background(), &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{Arguments: args},
		})
		if err != nil {
			t.Fatal(err)
		}
		text := res.Messages[0].Content.(*mcp.TextContent).Text
		for name := range registry {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(text) {
				if _, ok := served[name]; !ok {
					t.Errorf("author-alert (args %v) names %s, which --toolsets=alerts does not serve", args, name)
				}
			}
		}
	}
}
