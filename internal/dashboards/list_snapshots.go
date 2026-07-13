package dashboards

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewListDashboardSnapshotsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDashboardSnapshotsArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListDashboardSnapshotsArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.DashboardID) == "" {
			return nil, nil, errors.New("dashboard_id is required")
		}

		u := cfg.APIBaseURL + constants.EndpointDashboardSnapshots + "?" + url.Values{
			"dashboard_id": {args.DashboardID},
		}.Encode()

		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u, nil)
		if err != nil {
			return nil, nil, mapSnapshotAPIError(err)
		}

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dlBuilder.BuildDashboardLink(args.DashboardID)),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}
}
