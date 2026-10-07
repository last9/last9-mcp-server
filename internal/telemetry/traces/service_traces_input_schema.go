package traces

func GetServiceTracesInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"trace_id":         map[string]interface{}{"type": []string{"string", "null"}, "description": "Specific 32-character hexadecimal OpenTelemetry trace ID. Provide exactly one of trace_id or service_name."},
			"service_name":     map[string]interface{}{"type": []string{"string", "null"}, "description": "Name of service to get traces for. Provide exactly one of service_name or trace_id."},
			"lookback_minutes": map[string]interface{}{"type": []string{"number", "null"}, "description": "Minutes to look back (default: 4320 for trace_id, 60 for service_name; minimum: 1)."},
			"start_time_iso":   map[string]interface{}{"type": []string{"string", "null"}, "description": "Start time in RFC3339/ISO8601; defaults to now minus lookback_minutes."},
			"end_time_iso":     map[string]interface{}{"type": []string{"string", "null"}, "description": "End time in RFC3339/ISO8601; defaults to now."},
			"limit":            map[string]interface{}{"type": []string{"number", "null"}, "description": "Maximum traces to return (default: 10)."},
			"env":              map[string]interface{}{"type": []string{"string", "null"}, "description": "Environment to filter by; omit when unknown."},
		},
	}
}
