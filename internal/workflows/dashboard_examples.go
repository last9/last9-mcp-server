package workflows

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/prompts"
)

var DashboardExamples = Workflow{
	Name:        "dashboard_examples",
	Title:       "Dashboard Examples",
	Description: "Worked dashboard patterns for RED, endpoints, status classes, infrastructure, logs and traces.",
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Worked dashboard patterns for RED, endpoints, status classes, infrastructure, logs and traces.",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.DashboardExamplesWorkflow}}},
		}, nil
	},
}
