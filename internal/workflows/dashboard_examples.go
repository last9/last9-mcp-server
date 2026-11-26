package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const dashboardExamplesDescription = "Worked dashboard patterns for RED, endpoints, status classes, infrastructure, logs and traces."

var DashboardExamples = Workflow{
	Name:        "dashboard_examples",
	Title:       "Dashboard Examples",
	Description: dashboardExamplesDescription,
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: dashboardExamplesDescription,
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.DashboardExamplesWorkflow}}},
		}, nil
	},
}
