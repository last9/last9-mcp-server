package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewCreateDashboardSnapshotHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
		if err := validateCreateSnapshotArgs(args); err != nil {
			return nil, nil, err
		}

		payload, err := json.Marshal(args)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal snapshot request: %w", err)
		}

		u := cfg.APIBaseURL + constants.EndpointDashboardSnapshots
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodPost, u, payload)
		if err != nil {
			return nil, nil, mapSnapshotAPIError(err)
		}

		return textResultWithSnapshotLink(dlBuilder, body, ""), nil, nil
	}
}
