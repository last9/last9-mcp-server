package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewCreateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateDashboardArgs) (*mcp.CallToolResult, any, error) {
		if err := validateDashboardRequest(args.Dashboard); err != nil {
			return nil, nil, err
		}
		if len(args.Metadata) > 0 && !json.Valid(args.Metadata) {
			return nil, nil, errors.New("metadata must be valid JSON")
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

		return textResultWithDashboardLink(cfg, body, ""), nil, nil
	}
}
