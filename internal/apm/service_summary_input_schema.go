package apm

// GetServiceSummaryInputSchema returns the MCP-facing JSON Schema for
// get_service_summary. Enum and numeric bounds keep models from round-tripping
// rejected sort_by values or out-of-range limits; the handler still validates.
func GetServiceSummaryInputSchema() map[string]interface{} {
	sortKeys := make([]interface{}, 0, len(serviceSummarySortSpecs))
	for _, spec := range serviceSummarySortSpecs {
		sortKeys = append(sortKeys, spec.key)
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"start_time_iso": map[string]interface{}{
				"type":        "string",
				"description": "Start of the interval in RFC3339/ISO8601 (e.g. 2024-06-01T12:00:00Z). When both start and end are set they beat lookback.",
			},
			"end_time_iso": map[string]interface{}{
				"type":        "string",
				"description": "End of the interval in RFC3339/ISO8601 (e.g. 2024-06-01T13:00:00Z). When both start and end are set they beat lookback. A single bound fills the other with lookback_minutes.",
			},
			"lookback_minutes": map[string]interface{}{
				"type":        "number",
				"minimum":     float64(1),
				"description": "Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes.",
			},
			"env": map[string]interface{}{
				"type":        "string",
				"description": "Environment PromQL regex (default: .*). Exact one-env match needs anchors (e.g. ^prod$).",
			},
			"sort_by": map[string]interface{}{
				"type":        "string",
				"enum":        sortKeys,
				"description": "Sort key. Allowed: request_count (default), throughput_rpm, http_4xx_count, http_5xx_count, grpc_error_count. throughput_rpm ranks identically to request_count (same order; only sort_key_unit differs). Unknown values including errors, error_rate, and 5xx are rejected.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"minimum":     float64(0),
				"maximum":     float64(serviceSummaryMaxLimit),
				"description": "Max ranked rows. Omit or 0 means 10. Other values below 1 are an error. Values above 100 clamp to 100.",
			},
		},
	}
}
