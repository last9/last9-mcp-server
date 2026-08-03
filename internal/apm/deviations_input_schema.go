package apm

// GetAPMServiceDeviationsInputSchema returns the MCP-facing JSON Schema for
// get_apm_service_deviations. Window duration equality is validated by the
// handler because JSON Schema cannot express timestamp arithmetic. The handler
// also enforces lookback/current-window exclusivity because common model tool
// APIs reject the allOf/not shape needed to encode that constraint.
func GetAPMServiceDeviationsInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"service_name": map[string]interface{}{
				"type":        "string",
				"description": "Exact service name to compare. Omit for a fleet-wide comparison.",
			},
			"env": map[string]interface{}{
				"type":        "string",
				"description": "Exact environment to compare. Omit to return environments separately; environments are never merged.",
			},
			"datasource": map[string]interface{}{
				"type":        "string",
				"description": "One datasource to query. Omit to use the configured default datasource; data from multiple datasources is never combined.",
			},
			"start_time_iso": map[string]interface{}{
				"type":        "string",
				"format":      "date-time",
				"description": "Current-window start in RFC3339 format. Must be provided with end_time_iso and cannot be combined with lookback_minutes.",
			},
			"end_time_iso": map[string]interface{}{
				"type":        "string",
				"format":      "date-time",
				"description": "Current-window end in RFC3339 format. Must be provided with start_time_iso and cannot be combined with lookback_minutes.",
			},
			"lookback_minutes": map[string]interface{}{
				"type":        "number",
				"minimum":     float64(1),
				"description": "Current-window duration in minutes, minimum 1 and default 60. Mutually exclusive with start_time_iso and end_time_iso.",
			},
			"baseline_start_time_iso": map[string]interface{}{
				"type":        "string",
				"format":      "date-time",
				"description": "Explicit baseline start in RFC3339 format. Must be provided with baseline_end_time_iso; the handler validates that baseline and current windows have equal duration.",
			},
			"baseline_end_time_iso": map[string]interface{}{
				"type":        "string",
				"format":      "date-time",
				"description": "Explicit baseline end in RFC3339 format. Must be provided with baseline_start_time_iso; the handler validates that baseline and current windows have equal duration.",
			},
			"max_services": map[string]interface{}{
				"type":        "integer",
				"minimum":     float64(1),
				"maximum":     float64(10),
				"default":     float64(10),
				"description": "Maximum fleet services to return, from 1 through 10. Defaults to 10.",
			},
			"max_operations": map[string]interface{}{
				"type":        "integer",
				"minimum":     float64(1),
				"maximum":     float64(10),
				"default":     float64(10),
				"description": "Maximum correlated operations to return for service scope, from 1 through 10. Defaults to 10.",
			},
		},
		"dependentRequired": map[string]interface{}{
			"start_time_iso":          []string{"end_time_iso"},
			"end_time_iso":            []string{"start_time_iso"},
			"baseline_start_time_iso": []string{"baseline_end_time_iso"},
			"baseline_end_time_iso":   []string{"baseline_start_time_iso"},
		},
	}
}
