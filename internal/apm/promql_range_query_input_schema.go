package apm

func PromqlRangeQueryInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"required": []string{"query"},
		"properties": map[string]interface{}{
			"query":            map[string]interface{}{"type": "string", "description": "(Required) PromQL query to execute."},
			"start_time_iso":   map[string]interface{}{"type": []string{"string", "null"}, "description": "Start time in RFC3339/ISO8601. Optional when lookback_minutes is provided."},
			"end_time_iso":     map[string]interface{}{"type": []string{"string", "null"}, "description": "End time in RFC3339/ISO8601; defaults to now."},
			"lookback_minutes": map[string]interface{}{"type": []string{"number", "null"}, "description": "Minutes to look back from now (default: 60, minimum: 1). Use for relative windows."},
			"datasource":       map[string]interface{}{"type": []string{"string", "null"}, "description": "Datasource name; defaults to the configured datasource."},
		},
	}
}
