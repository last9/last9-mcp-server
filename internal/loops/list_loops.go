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

type ListLoopsArgs struct {
	Query string `json:"query,omitempty" jsonschema:"Optional case-insensitive substring of loop name or id"`
}

func NewListLoopsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListLoopsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListLoopsArgs) (*mcp.CallToolResult, any, error) {
		items, err := apiloops.ListLoops(ctx, last9api.NewClient(client, cfg), args.Query)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(map[string]any{"loops": items, "count": len(items)}, loopsIndexLink(cfg)), nil, nil
	}
}
