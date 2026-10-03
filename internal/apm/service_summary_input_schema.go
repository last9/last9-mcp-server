package apm

// GetServiceSummaryInputSchema returns the MCP-facing JSON Schema for
// get_service_summary. sort_by is an enum so models cannot round-trip rejected
// keys. limit bounds are enforced in the handler (0 → default, >100 clamps)
// rather than schema minimum/maximum, so LLM callers that send oversized
// limits still get a usable clamped result.
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
				"description": "Environment PromQL regex (default: .*). Exact one-env match needs anchors (e.g. ^prod$). Invalid regex is rejected before querying.",
			},
			"sort_by": map[string]interface{}{
				"type":        "string",
				"enum":        sortKeys,
				"description": "Sort key. Allowed: request_count (default), throughput_rpm, http_4xx_count, http_5xx_count, grpc_error_count. throughput_rpm ranks identically to request_count (same order; only sort_key_unit differs). Unknown values including errors, error_rate, and 5xx are rejected.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max ranked rows. Omit or 0 means 10. Other values below 1 are an error. Values above 100 clamp to 100.",
			},
		},
	}
}
