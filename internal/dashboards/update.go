package dashboards

import (
	"context"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewUpdateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}
		if err := validateDashboardRequest(args.Dashboard); err != nil {
			return nil, nil, err
		}
		if err := validateMetadata(args.Metadata); err != nil {
			return nil, nil, err
		}

		payload, err := marshalDashboardRequest(args.DashboardRequest)
		if err != nil {
			return nil, nil, err
		}

		api := last9api.NewClient(client, cfg)
		body, err := apidashboards.UpdateDashboard(ctx, api, args.ID, payload)
		if upstreamLockedRefusal(err) {
			return nil, nil, dashboardLockedError(lockedDashboardAuthor(ctx, api, args.ID))
		}
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(dlBuilder, body, args.ID), nil, nil
	}
}
