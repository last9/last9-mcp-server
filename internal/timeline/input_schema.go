package timeline

func GetChangeTimelineInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"start_time_iso": timelineStringProperty("Explicit RFC3339 range start; provide with end_time_iso."),
			"end_time_iso":   timelineStringProperty("Explicit RFC3339 range end; provide with start_time_iso."),
			"lookback_minutes": map[string]interface{}{
				"type": "integer", "minimum": float64(1), "maximum": float64(maxLookbackMinutes),
				"default": float64(defaultLookbackMinutes), "description": "Relative range in minutes; omit when explicit bounds are provided.",
			},
			"service_name":   timelineStringProperty("Exact canonical service filter."),
			"env":            timelineStringProperty("Exact canonical environment filter."),
			"alert_group_id": timelineStringProperty("Exact alert-group filter for alert episodes."),
			"rule_id":        timelineStringProperty("Exact rule filter for alert episodes."),
			"event_name":     timelineStringProperty("Exact canonical change-event filter."),
			"kinds": map[string]interface{}{
				"type": "array", "uniqueItems": true, "description": "Event kinds to include; defaults to both change_event and alert_episode.",
				"items": map[string]interface{}{"type": "string", "enum": []string{kindChangeEvent, kindAlertEpisode}},
			},
			"max_events": map[string]interface{}{
				"type": "integer", "minimum": float64(1), "maximum": float64(maxEvents),
				"default": float64(defaultMaxEvents), "description": "Maximum number of normalized events returned after deterministic ordering.",
			},
		},
		"dependentRequired": map[string]interface{}{
			"start_time_iso": []string{"end_time_iso"},
			"end_time_iso":   []string{"start_time_iso"},
		},
	}
}

func timelineStringProperty(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}
