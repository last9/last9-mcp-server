package dashboards

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
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

		path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodPut, u, payload)
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(dlBuilder, body, args.ID), nil, nil
	}
}
