package dashboards

func dashboardObjectSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
	}
}

func metadataObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Optional dashboard metadata, for example {\"_category\":\"custom\",\"_type\":\"metrics\"}.",
	}
}

// GetCreateDashboardInputSchema returns the MCP-facing schema for create_dashboard.
// The handler uses json.RawMessage internally, but clients must see dashboard and
// metadata as JSON objects rather than byte arrays.
func GetCreateDashboardInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dashboard": dashboardObjectSchema("Dashboard definition with name and panels."),
			"metadata":  metadataObjectSchema(),
		},
		"required": []string{"dashboard"},
	}
}

// GetUpdateDashboardInputSchema returns the MCP-facing schema for update_dashboard.
func GetUpdateDashboardInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Dashboard UUID to update.",
			},
			"dashboard": dashboardObjectSchema("Full replacement dashboard definition with name and panels."),
			"metadata":  metadataObjectSchema(),
		},
		"required": []string{"id", "dashboard"},
	}
}
