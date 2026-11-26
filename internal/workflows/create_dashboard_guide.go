package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const createDashboardGuideDescription = "Discover data, design panels, validate before approval, then create once and refine by id."

var CreateDashboardGuide = Workflow{
	Name:        "create_dashboard_guide",
	Title:       "Create Dashboard Guide",
	Description: createDashboardGuideDescription,
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: createDashboardGuideDescription,
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.CreateDashboardGuideWorkflow}}},
		}, nil
	},
}
