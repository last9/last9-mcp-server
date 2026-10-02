package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDashboardArgs struct {
	UID string `json:"uid" jsonschema:"(Required) Grafana dashboard UID from grafana_search_dashboards or grafana_list_folder_dashboards"`
	// FullJSON returns the raw Grafana dashboard JSON (dashboard definition
	// plus metadata) instead of the filtered summary.
	FullJSON bool `json:"full_json,omitempty" jsonschema:"Return the raw Grafana dashboard JSON instead of the filtered summary (default false)"`
}

func NewGetDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetDashboardArgs) (*mcp.CallToolResult, any, error) {
		if args.UID == "" {
			return nil, nil, fmt.Errorf("uid is required")
		}
		u := cfg.GrafanaAPIBaseURL + "/api/dashboards/uid/" + url.PathEscape(args.UID)

		body, err := doJSONRequest(ctx, client, cfg, u)
		if err != nil {
			return nil, nil, err
		}
		if args.FullJSON {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
			}, nil, nil
		}

		var resp DashboardResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, nil, fmt.Errorf("failed to parse dashboard response: %w", err)
		}
		sum := summarizeDashboard(resp.Dashboard)
		out, err := json.MarshalIndent(sum, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
