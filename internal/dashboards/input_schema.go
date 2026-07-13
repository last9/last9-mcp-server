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

// GetCreateDashboardSnapshotInputSchema returns the MCP-facing schema for create_dashboard_snapshot.
// Handler fields that are json.RawMessage must be exposed as JSON objects to clients.
func GetCreateDashboardSnapshotInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"dashboard_id": map[string]interface{}{
				"type":        "string",
				"description": "Dashboard UUID to snapshot.",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Snapshot name.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Optional snapshot description.",
			},
			"expires_at": map[string]interface{}{
				"type":        "integer",
				"description": "Optional Unix expiry timestamp in seconds; must be in the future. Omit for no expiry.",
			},
			"time_range": dashboardObjectSchema("Absolute time range with from/to Unix seconds, e.g. {\"from\":1710000000,\"to\":1710003600}."),
			"variables": dashboardObjectSchema(
				"Optional selected dashboard variable values at capture time.",
			),
			"region": map[string]interface{}{
				"type":        "string",
				"description": "Region used when capturing panel data.",
			},
			"dashboard_definition": dashboardObjectSchema(
				"Frozen dashboard definition object (same shape as get_dashboard's dashboard field).",
			),
			"panel_data": dashboardObjectSchema(
				"Frozen panel query results keyed by panel id.",
			),
		},
		"required": []string{"dashboard_id", "name", "time_range", "dashboard_definition", "panel_data"},
	}
}
