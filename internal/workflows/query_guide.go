package workflows

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"last9-mcp/internal/prompts"
)

var QueryGuide = Workflow{
	Name:        "query_guide",
	Title:       "Dashboard Query Guide",
	Description: "Metric semantics, time windows, percentages and JSON pipeline rules for dashboard queries.",
	Handler: func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Metric semantics, time windows, percentages and JSON pipeline rules for dashboard queries.",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompts.QueryGuideWorkflow}}},
		}, nil
	},
}
