package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var CreateDashboardGuide = Workflow{
	Name:        "create_dashboard_guide",
	Title:       "Create Dashboard Guide",
	Description: "Discover data, design panels, validate before approval, then create once and refine by id.",
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Dashboard authoring and targeted editing workflow",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.CreateDashboardGuideWorkflow}}},
		}, nil
	},
}
