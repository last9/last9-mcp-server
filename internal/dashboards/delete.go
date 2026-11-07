package dashboards

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewDeleteDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteDashboardArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteDashboardArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}

		api := last9api.NewClient(client, cfg)
		body, err := apidashboards.DeleteDashboard(ctx, api, args.ID)
		if upstreamLockedRefusal(err) {
			return nil, nil, dashboardLockedError(lockedDashboardAuthor(ctx, api, args.ID))
		}
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		text := strings.TrimSpace(string(body))
		if text == "" {
			text = fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID)
		}

		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(dlBuilder.BuildDashboardsIndexLink()),
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil, nil
	}
}
