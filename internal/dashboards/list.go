package dashboards

import (
	"context"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListDashboardsArgs struct{}

func NewListDashboardsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDashboardsArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ ListDashboardsArgs) (*mcp.CallToolResult, any, error) {
		u := cfg.APIBaseURL + constants.EndpointDashboards
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u, nil)
		if err != nil {
			return nil, nil, err
		}

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dlBuilder.BuildDashboardsIndexLink()),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}
}
