package apm

func PromqlInstantQueryInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"required": []string{"query"},
		"properties": map[string]interface{}{
			"query":            map[string]interface{}{"type": "string", "description": "(Required) PromQL query to execute."},
			"time_iso":         map[string]interface{}{"type": []string{"string", "null"}, "description": "Evaluation time in RFC3339/ISO8601; defaults to now or now minus lookback_minutes."},
			"lookback_minutes": map[string]interface{}{"type": []string{"number", "null"}, "description": "Minutes before now when time_iso is omitted (default: 0, minimum when set: 1)."},
			"datasource":       map[string]interface{}{"type": []string{"string", "null"}, "description": "Datasource name; defaults to the configured datasource."},
		},
	}
}
