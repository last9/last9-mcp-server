package dashboards

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewDeleteDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteDashboardArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == "" {
			return nil, nil, errors.New("id is required")
		}

		path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodDelete, u, nil)
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		text := strings.TrimSpace(string(body))
		if text == "" {
			text = fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	}
}
