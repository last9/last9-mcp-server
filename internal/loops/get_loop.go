package loops

import (
	"context"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	apiloops "last9-mcp/internal/last9api/loops"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetLoopArgs struct {
	LoopID string `json:"loop_id" jsonschema:"(Required) Loop id from list_loops or create_loop"`
}

func NewGetLoopHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLoopArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetLoopArgs) (*mcp.CallToolResult, any, error) {
		id := strings.TrimSpace(args.LoopID)
		if id == "" {
			return utils.ToolErrorResult("loop_id is required"), nil, nil
		}
		detail, err := apiloops.Get(ctx, last9api.NewClient(client, cfg), id)
		if err != nil {
			if apiloops.IsNotEnabled(err) {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(detail, loopsDetailLink(cfg, id)), nil, nil
	}
}
