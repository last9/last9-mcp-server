package alerting

import (
	"encoding/json"
	"errors"
	"fmt"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Upstream bodies are never relayed: channel requests carry secrets that a rejection can echo.
func notificationChannelErrorResult(err error, hints map[int]string) (*mcp.CallToolResult, any, error) {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) {
		return nil, nil, err
	}
	if hint, ok := hints[upstream.StatusCode]; ok {
		return utils.ToolErrorResult(hint), nil, nil
	}
	return utils.ToolErrorResult(fmt.Sprintf("the API returned HTTP %d", upstream.StatusCode)), nil, nil
}

func notificationChannelJSONResult(cfg models.Config, v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildNotificationChannelsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}
