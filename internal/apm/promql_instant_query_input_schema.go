package apm

func PromqlInstantQueryInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"required": []string{"query"},
		"properties": map[string]interface{}{
			"query":            map[string]interface{}{"type": "string", "description": "(Required) PromQL query to execute."},
			"time":             map[string]interface{}{"type": []string{"string", "number"}, "description": "Alias for evaluation time: RFC3339 or positive Unix seconds; evaluated at whole-second precision. Prefer time_iso."},
			"time_iso":         map[string]interface{}{"type": []string{"string", "null"}, "description": "Preferred evaluation-time spelling, in RFC3339/ISO8601; time also accepts positive Unix seconds. Defaults to now or now minus lookback_minutes."},
			"lookback_minutes": map[string]interface{}{"type": []string{"number", "null"}, "description": "Minutes before now when time_iso is omitted (default: 0, minimum when set: 1)."},
			"datasource":       map[string]interface{}{"type": []string{"string", "null"}, "description": "Datasource name; omitted uses the default, a near miss its closest match (named in an additional content block)."},
		},
	}
}
