package models

// DropRuleFilter represents a single filter condition in a drop rule.
type DropRuleFilter struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Operator    string `json:"operator"`
	Conjunction string `json:"conjunction"`
}

// DropRuleAction represents the action configuration for a drop rule.
type DropRuleAction struct {
	Name        string                 `json:"name"`
	Destination string                 `json:"destination"`
	Properties  map[string]interface{} `json:"properties"`
}

// DropRule is the flat shape returned by legacy GET /logs_settings/routing.
// Do not use for writes; use OTelDropSettingCreateRequest with POST /otel_settings/drop.
type DropRule struct {
	Name      string           `json:"name"`
	Telemetry string           `json:"telemetry"`
	Filters   []DropRuleFilter `json:"filters"`
	Action    DropRuleAction   `json:"action"`
}

// OTelDropSettingProperties is the nested properties payload for POST /otel_settings/drop.
type OTelDropSettingProperties struct {
	Telemetry string           `json:"telemetry"`
	Filters   []DropRuleFilter `json:"filters"`
	Action    DropRuleAction   `json:"action"`
}

// OTelDropSettingCreateRequest is the POST /otel_settings/drop request body.
type OTelDropSettingCreateRequest struct {
	Name       string                    `json:"name"`
	Properties OTelDropSettingProperties `json:"properties"`
}
