package logs

func GetServiceLogsInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"required": []string{"service_name"},
		"properties": map[string]interface{}{
			"service_name":      map[string]interface{}{"type": "string", "description": "(Required) Name of the service to retrieve logs for."},
			"start_time_iso":    map[string]interface{}{"type": []string{"string", "null"}, "description": "Start time in RFC3339/ISO8601. If omitted, use lookback_minutes."},
			"end_time_iso":      map[string]interface{}{"type": []string{"string", "null"}, "description": "End time in RFC3339/ISO8601; defaults to now."},
			"lookback_minutes":  map[string]interface{}{"type": []string{"integer", "null"}, "description": "Minutes to look back when start_time_iso is omitted (default: 60, minimum: 1)."},
			"limit":             map[string]interface{}{"type": []string{"integer", "null"}, "description": "Maximum log entries to return (default: 100; 20 in raw mode)."},
			"severity_filters":  map[string]interface{}{"type": []string{"array", "null"}, "items": map[string]interface{}{"type": "string"}, "description": "Severity patterns to match with OR logic."},
			"body_filters":      map[string]interface{}{"type": []string{"array", "null"}, "items": map[string]interface{}{"type": "string"}, "description": "Message content patterns to match with OR logic."},
			"http_status_class": map[string]interface{}{"type": []string{"string", "null"}, "description": "HTTP status class: 2xx, 3xx, 4xx, or 5xx. Discovers the status field unless http_status_field is set."},
			"http_status_code":  map[string]interface{}{"type": []string{"string", "null"}, "description": "Exact HTTP status code. Takes precedence over http_status_class."},
			"http_status_field": map[string]interface{}{"type": []string{"string", "null"}, "description": "Explicit logjson field for HTTP status; needed when discovery finds zero or multiple fields."},
			"attribute_filters": map[string]interface{}{"type": []string{"array", "null"}, "description": "Equality filters on named log attributes.", "items": map[string]interface{}{
				"type": "object", "additionalProperties": false, "required": []string{"field", "value"},
				"properties": map[string]interface{}{
					"field": map[string]interface{}{"type": "string", "description": "(Required) logjson field, e.g. attributes['user_id']."},
					"value": map[string]interface{}{"type": "string", "description": "(Required) exact value matched with $eq."},
				},
			}},
			"env":   map[string]interface{}{"type": []string{"string", "null"}, "description": "Environment to filter by; omit when unknown."},
			"index": map[string]interface{}{"type": []string{"string", "null"}, "description": "Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Use a user-named index or a non-default physical index discovered by get_service_profile; omit for default or when unknown."},
			// No schema enum: a nullable enum fails as an opaque anyOf, so
			// validateLogResponseMode rejects unknown values, quoting them.
			"response_mode": map[string]interface{}{"type": []string{"string", "null"}, "description": `"raw"=log entries (message text, timestamp, severity); read IDs/status codes if present in message text. "summary" (default, over 20 lines, ≤4000 chars)=counts/patterns.`},
		},
	}
}
