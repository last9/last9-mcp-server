package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const queryGuideDescription = "Metric semantics, time windows, percentages and JSON pipeline rules for dashboard queries."

var QueryGuide = Workflow{
	Name:        "query_guide",
	Title:       "Dashboard Query Guide",
	Description: queryGuideDescription,
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: queryGuideDescription,
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.QueryGuideWorkflow}}},
		}, nil
	},
}
