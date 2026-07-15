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

type GetDashboardSnapshotArgs struct {
	ID string `json:"id" jsonschema:"(Required) Snapshot UUID"`
}

func NewGetDashboardSnapshotHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}

		path := fmt.Sprintf(constants.EndpointDashboardSnapshotByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u, nil)
		if err != nil {
			return nil, nil, mapSnapshotAPIError(err)
		}

		return textResultWithSnapshotLink(dlBuilder, body, args.ID), nil, nil
	}
}
