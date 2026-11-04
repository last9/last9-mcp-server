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

		id := dashboardIDFromResponse(body)
		// No fallback id: the helper re-extracts from the same body.
		result := textResultWithDashboardLink(dlBuilder, body, "")
		if id != "" {
			result.Content = append(result.Content, &mcp.TextContent{Text: refineNudge(cfg, id)})
		}
		return result, nil, nil
	}
}

func refineNudge(cfg models.Config, id string) string {
	const noRecreate = "Do not call create_dashboard again for this dashboard."
	patch := cfg.ToolFilter.ServesWrite("patch_dashboard")
	update := cfg.ToolFilter.ServesWrite("update_dashboard")
	switch {
	case patch && update:
		return "To add, edit, remove or move panels, call patch_dashboard with id=" + id + "; call update_dashboard only to restructure most of the dashboard (it replaces the whole body). " + noRecreate
	case patch:
		return "To add, edit, remove or move panels, call patch_dashboard with id=" + id + ". " + noRecreate
	case update:
		return "To refine this dashboard, call update_dashboard with id=" + id + " and the full dashboard body (update replaces the whole dashboard). " + noRecreate
	default:
		return noRecreate
	}
}
