package loops

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
	apiloops "last9-mcp/internal/last9api/loops"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListLoopTemplatesArgs struct {
	Capability string `json:"capability,omitempty" jsonschema:"Optional capability filter (substring of slug or exact capability label)"`
}

func NewListLoopTemplatesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListLoopTemplatesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListLoopTemplatesArgs) (*mcp.CallToolResult, any, error) {
		items, err := apiloops.ListTemplates(ctx, last9api.NewClient(client, cfg), args.Capability)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(map[string]any{"templates": items, "count": len(items)}, loopsIndexLink(cfg)), nil, nil
	}
}
