package dashboards

import (
	"last9-mcp/internal/deeplink"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// jsonTextResult is the one text-output path for dashboard and snapshot JSON,
// so every tool shows <, > and & literally.
func jsonTextResult(body []byte, link string) *mcp.CallToolResult {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(normalizeEscapedHTML(body))},
		},
	}
	if link != "" {
		result.Meta = deeplink.ToMeta(link)
	}
	return result
}

func textResultWithLink(body []byte, fallbackID string, extractID func([]byte) string, buildLink func(string) string) *mcp.CallToolResult {
	id := extractID(body)
	if id == "" {
		id = fallbackID
	}
	link := ""
	if id != "" {
		link = buildLink(id)
	}
	return jsonTextResult(body, link)
}

func textResultWithDashboardLink(dlBuilder *deeplink.Builder, body []byte, fallbackDashboardID string) *mcp.CallToolResult {
	return textResultWithLink(body, fallbackDashboardID, dashboardIDFromResponse, dlBuilder.BuildDashboardLink)
}

func textResultWithSnapshotLink(dlBuilder *deeplink.Builder, body []byte, fallbackSnapshotID string) *mcp.CallToolResult {
	return textResultWithLink(body, fallbackSnapshotID, snapshotIDFromResponse, dlBuilder.BuildDashboardSnapshotLink)
}
