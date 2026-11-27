package loops

import (
	"encoding/json"
	"fmt"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func jsonResult(v any, link string) *mcp.CallToolResult {
	body, err := json.Marshal(v)
	if err != nil {
		return utils.ToolErrorResult(fmt.Sprintf("encode response: %v", err))
	}
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}
	if link != "" {
		result.Meta = deeplink.ToMeta(link)
	}
	return result
}

func loopsIndexLink(cfg models.Config) string {
	return deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildLoopsLink()
}

func loopsDetailLink(cfg models.Config, id string) string {
	return deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildLoopLink(id)
}
