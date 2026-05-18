package dashboards

import (
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResultWithDashboardLink(cfg models.Config, body []byte, fallbackDashboardID string) *mcp.CallToolResult {
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
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		result.Meta = deeplink.ToMeta(dlBuilder.BuildDashboardLink(dashboardID))
	}
	return result
}
