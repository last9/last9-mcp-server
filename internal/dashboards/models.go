package dashboards

import "encoding/json"

// DashboardRequest matches POST/PUT wire format (terraform-provider client).
type DashboardRequest struct {
	Dashboard json.RawMessage `json:"dashboard" jsonschema:"Dashboard object: name, panels, variables, time"`
	Metadata  json.RawMessage `json:"metadata,omitempty" jsonschema:"Metadata with _category, _type, tags"`
}

type CreateDashboardArgs struct {
	DashboardRequest
}

type UpdateDashboardArgs struct {
	ID string `json:"id" jsonschema:"Dashboard UUID"`
	DashboardRequest
}

type DeleteDashboardArgs struct {
	ID string `json:"id" jsonschema:"Dashboard UUID"`
}

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

type ListDashboardSnapshotsArgs struct {
	DashboardID string `json:"dashboard_id" jsonschema:"(Required) Dashboard UUID whose snapshots to list"`
}

type GetDashboardSnapshotArgs struct {
	ID string `json:"id" jsonschema:"(Required) Snapshot UUID"`
}

type DeleteDashboardSnapshotArgs struct {
	ID string `json:"id" jsonschema:"(Required) Snapshot UUID"`
}
