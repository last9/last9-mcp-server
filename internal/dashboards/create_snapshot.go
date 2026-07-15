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

// CreateDashboardSnapshotArgs matches POST /dashboards/snapshots wire format.
type CreateDashboardSnapshotArgs struct {
	DashboardID         string          `json:"dashboard_id" jsonschema:"(Required) Dashboard UUID to snapshot"`
	Name                string          `json:"name" jsonschema:"(Required) Snapshot name"`
	Description         string          `json:"description,omitempty" jsonschema:"Optional snapshot description"`
	ExpiresAt           *int64          `json:"expires_at,omitempty" jsonschema:"Optional Unix expiry timestamp in seconds; must be in the future"`
	TimeRange           json.RawMessage `json:"time_range" jsonschema:"(Required) Absolute time range with from/to Unix seconds"`
	Variables           json.RawMessage `json:"variables,omitempty" jsonschema:"Selected dashboard variable values at capture time"`
	Region              string          `json:"region,omitempty" jsonschema:"Region used when capturing panel data"`
	DashboardDefinition json.RawMessage `json:"dashboard_definition" jsonschema:"(Required) Frozen dashboard definition object"`
	PanelData           json.RawMessage `json:"panel_data" jsonschema:"(Required) Frozen panel query results keyed by panel id"`
}

// Note: variables is optional in the MCP schema but always sent on the wire as {}
// when omitted — the v4 API currently 500s if the field is absent.

func NewCreateDashboardSnapshotHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateDashboardSnapshotArgs) (*mcp.CallToolResult, any, error) {
		if err := validateCreateSnapshotArgs(args); err != nil {
			return nil, nil, err
		}
		normalizeCreateSnapshotArgs(&args)

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
