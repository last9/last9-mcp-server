package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewUpdateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == "" {
			return nil, nil, errors.New("id is required")
		}
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

		path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodPut, u, payload)
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(cfg, body, args.ID), nil, nil
	}
}
