package dashboards

import (
	"encoding/json"
	"errors"
)

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
