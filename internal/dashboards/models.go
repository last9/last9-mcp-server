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
