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

type SearchDashboardsArgs struct {
	Query string `json:"query" jsonschema:"(Required) Substring to match against dashboard titles"`
}

func NewSearchDashboardsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, SearchDashboardsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SearchDashboardsArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}
		params := url.Values{
			"query": {args.Query},
			"type":  {"dash-db"},
		}
		res, err := collectSearchHits(ctx, client, cfg, cfg.GrafanaAPIBaseURL+"/api/search", params)
		if err != nil {
			return nil, nil, err
		}
		out, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
