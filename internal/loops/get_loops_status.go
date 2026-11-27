package loops

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
	apiloops "last9-mcp/internal/last9api/loops"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyArgs struct{}

func NewGetLoopsStatusHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, emptyArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
		enabled, err := apiloops.ProbeEnabled(ctx, last9api.NewClient(client, cfg))
		if err != nil && !apiloops.IsNotEnabled(err) {
			return nil, nil, err
		}
		return jsonResult(apiloops.StatusResponse(enabled, err), loopsIndexLink(cfg)), nil, nil
	}
}
