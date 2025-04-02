package models

// DropRuleFilter represents a single filter condition in a drop rule
type DropRuleFilter struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Operator    string `json:"operator"`
	Conjunction string `json:"conjunction"`
}

// DropRuleAction represents the action configuration for a drop rule
type DropRuleAction struct {
	Name        string                 `json:"name"`
	Destination string                 `json:"destination"`
	Properties  map[string]interface{} `json:"properties"`
}

// DropRule represents a complete drop rule configuration
type DropRule struct {
	Name      string           `json:"name"`
	Telemetry string           `json:"telemetry"`
	Filters   []DropRuleFilter `json:"filters"`
	Action    DropRuleAction   `json:"action"`
}
