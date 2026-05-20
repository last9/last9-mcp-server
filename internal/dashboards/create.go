package dashboards

import (
	"context"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewCreateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateDashboardArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateDashboardArgs) (*mcp.CallToolResult, any, error) {
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

		u := cfg.APIBaseURL + constants.EndpointDashboards + "/"
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodPost, u, payload)
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(dlBuilder, body, ""), nil, nil
	}
}
