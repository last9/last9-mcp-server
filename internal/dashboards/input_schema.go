package dashboards

func dashboardObjectSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"required":    []string{"name", "panels"},
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"panels": map[string]interface{}{
				"type":  "array",
				"items": panelObjectSchema(),
			},
		},
	}
}

func panelObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"name", "layout", "visualization"},
		"properties": map[string]interface{}{
			"name":    map[string]interface{}{"type": "string"},
			"version": map[string]interface{}{"type": "integer", "enum": []int{1}},
			"layout": map[string]interface{}{
				"type":     "object",
				"required": []string{"x", "y", "w", "h"},
				"properties": map[string]interface{}{
					"x": map[string]interface{}{"type": "number"}, "y": map[string]interface{}{"type": "number"},
					"w": map[string]interface{}{"type": "number"}, "h": map[string]interface{}{"type": "number"},
				},
			},
			"visualization": map[string]interface{}{
				"type": "object", "required": []string{"type"},
				"properties": map[string]interface{}{"type": map[string]interface{}{"type": "string"}},
			},
			"queries": queriesArraySchema(),
		},
		"anyOf": []interface{}{
			panelVisualizationTypeSchema("section"),
			panelVisualizationTypeSchema("markdown"),
			map[string]interface{}{"required": []string{"queries"}},
		},
	}
}

func panelVisualizationTypeSchema(kind string) map[string]interface{} {
	return map[string]interface{}{
		"properties": map[string]interface{}{
			"visualization": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{"enum": []string{kind}},
				},
			},
		},
	}
}

func queriesArraySchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "type", "query_type", "telemetry", "expr", "legend"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string"}, "type": map[string]interface{}{"type": "string"},
				"query_type": map[string]interface{}{"type": "string"}, "telemetry": map[string]interface{}{"type": "string"},
				"expr": map[string]interface{}{"type": "string"},
				"legend": map[string]interface{}{
					"type": "object", "required": []string{"type", "value"},
					"properties": map[string]interface{}{"type": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"}},
				},
			},
		},
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
			"dashboard": dashboardObjectSchema("(Required) Dashboard definition with name and panels."),
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
				"description": "(Required) Dashboard UUID to update.",
			},
			"dashboard": dashboardObjectSchema("(Required) Full replacement dashboard definition with name and panels."),
			"metadata":  metadataObjectSchema(),
		},
		"required": []string{"id", "dashboard"},
	}
}
