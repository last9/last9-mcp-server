package dashboards

import (
	"last9-mcp/internal/deeplink"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResultWithDashboardLink(dlBuilder *deeplink.Builder, body []byte, fallbackDashboardID string) *mcp.CallToolResult {
	dashboardID := dashboardIDFromResponse(body)
	if dashboardID == "" {
		dashboardID = fallbackDashboardID
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(body)},
		},
	}
	if dashboardID != "" {
		result.Meta = deeplink.ToMeta(dlBuilder.BuildDashboardLink(dashboardID))
	}
	return result
}
