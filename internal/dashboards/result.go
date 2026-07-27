package dashboards

import (
	"last9-mcp/internal/deeplink"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResultWithLink(body []byte, fallbackID string, extractID func([]byte) string, buildLink func(string) string) *mcp.CallToolResult {
	id := extractID(body)
	if id == "" {
		id = fallbackID
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(body)},
		},
	}
	if id != "" {
		result.Meta = deeplink.ToMeta(buildLink(id))
	}
	return result
}

func textResultWithDashboardLink(dlBuilder *deeplink.Builder, body []byte, fallbackDashboardID string) *mcp.CallToolResult {
	return textResultWithLink(body, fallbackDashboardID, dashboardIDFromResponse, dlBuilder.BuildDashboardLink)
}

func textResultWithSnapshotLink(dlBuilder *deeplink.Builder, body []byte, fallbackSnapshotID string) *mcp.CallToolResult {
	return textResultWithLink(body, fallbackSnapshotID, snapshotIDFromResponse, dlBuilder.BuildDashboardSnapshotLink)
}
