package dashboards

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewDeleteDashboardSnapshotHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}

		path := fmt.Sprintf(constants.EndpointDashboardSnapshotByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodDelete, u, nil)
		if err != nil {
			return nil, nil, mapSnapshotAPIError(err)
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
