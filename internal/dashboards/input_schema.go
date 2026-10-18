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
			"version": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 1},
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
				"properties": map[string]interface{}{
					"type": map[string]interface{}{"type": "string", "description": "A logs visualization requires logs_config."},
					"logs_config": map[string]interface{}{
						"type": "object", "required": []string{"columns"},
						"description": "Required when type is logs. Choose one or more columns from timestamp, reason, severity, kind, service, body.",
						"properties": map[string]interface{}{
							"columns": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "enum": []string{"timestamp", "reason", "severity", "kind", "service", "body"}}},
							"row_limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 1000},
							"sort_order": map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}},
							"severity_coloring": map[string]interface{}{"type": "boolean"},
						},
					},
				},
			},
			"queries": queriesArraySchema(),
		},
		"anyOf": []interface{}{
			panelVisualizationTypeSchema("section"),
			panelVisualizationTypeSchema("markdown"),
			map[string]interface{}{"required": []string{"queries"}},
		},
		"allOf": []interface{}{map[string]interface{}{
			"if": map[string]interface{}{"required": []string{"visualization"}, "properties": map[string]interface{}{"visualization": map[string]interface{}{"required": []string{"type"}, "properties": map[string]interface{}{"type": map[string]interface{}{"const": "logs"}}}}},
			"then": map[string]interface{}{"properties": map[string]interface{}{"visualization": map[string]interface{}{"required": []string{"logs_config"}}}},
		}},
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
				"query_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"promql", "log_json", "log_ql", "log_raw", "trace_json", "trace_ql"},
					"description": "Use promql for metrics, log_json or log_ql for logs, log_raw or log_ql for raw log panels, and trace_json or trace_ql for traces.",
				},
				"telemetry":  map[string]interface{}{"type": "string"},
				"expr": map[string]interface{}{
					"type": "string",
					"description": `Every log_json expression must be a JSON pipeline with an aggregate stage; use log_ql for a raw logs panel. For log_json line, timeseries, or bar panels, include exactly one time-windowed aggregate (window_aggregate or aggregate with window); grouping by service or environment does not replace the window. Example: [{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"],"groupby":{"ServiceName":"service"}}].`,
				},
				"legend": map[string]interface{}{
					"type": "object", "required": []string{"type", "value"},
					"properties": map[string]interface{}{
						"type": map[string]interface{}{"type": "string", "enum": []string{"auto", "custom"}},
						"value": map[string]interface{}{"type": "string"},
					},
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
