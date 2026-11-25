package dashboards

import (
	"encoding/json"
	"errors"
)

func GetDashboardInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Dashboard UUID; required when dashboard_id is omitted",
			},
			"dashboard_id": map[string]any{
				"type":        "string",
				"description": "Alias for id; required when id is omitted",
			},
			"region": map[string]any{
				"type":        "string",
				"description": "AWS region for query population (defaults to configured datasource region)",
			},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"id"}},
			map[string]any{"required": []any{"dashboard_id"}},
		},
	}
}

func PrepareGetDashboardArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil
	}
	alias, ok := fields["dashboard_id"]
	if !ok {
		return raw, nil
	}
	var id *string
	if err := json.Unmarshal(alias, &id); err != nil || id == nil {
		return nil, errors.New("dashboard_id must be a string")
	}
	if canonical, exists := fields["id"]; exists {
		var other *string
		if err := json.Unmarshal(canonical, &other); err != nil || other == nil || *other != *id {
			return nil, errors.New("conflicting id and dashboard_id: supply one dashboard UUID")
		}
	}
	fields["id"] = alias
	delete(fields, "dashboard_id")
	return json.Marshal(fields)
}
