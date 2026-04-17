package traces

func stringEnum(values ...string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": values}
}

// GetTracesInputSchema returns a hand-crafted JSON Schema for the get_traces tool.
// This replaces the auto-generated schema from GetTracesArgs so that tracejson_query
// items have a detailed oneOf definition — constraining the LLM to use correct field
// names (aggregates, groupby, as) and function object syntax ({\"$count\": []}).
func GetTracesInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tracejson_query": tracejsonQuerySchema(),
			"start_time_iso": map[string]interface{}{
				"type":        []string{"string", "null"},
				"description": "Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Use with end_time_iso for absolute windows.",
			},
			"end_time_iso": map[string]interface{}{
				"type":        []string{"string", "null"},
				"description": "End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.",
			},
			"lookback_minutes": map[string]interface{}{
				"type":        []string{"integer", "null"},
				"description": "Number of minutes to look back from current time (default: 60, minimum: 1). Use for relative windows.",
			},
			"limit": map[string]interface{}{
				"type":        []string{"integer", "null"},
				"description": "Maximum number of traces to return (optional, default: 5000).",
			},
		},
		"required": []string{"tracejson_query"},
	}
}

func tracejsonQuerySchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": "JSON pipeline query for traces. An ordered list of stages: filter → parse → transform → aggregate/window_aggregate. Each stage is an object with a 'type' field.",
		"items": map[string]interface{}{
			"oneOf": []interface{}{
				filterStageSchema(),
				aggregateStageSchema(),
				windowAggregateStageSchema(),
				parseStageSchema(),
				transformStageSchema(),
				selectStageSchema(),
			},
		},
	}
}

func filterStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Filter stage: narrows the dataset by condition. Use $and/$or for multiple conditions.",
		"required":    []string{"type", "query"},
		"properties": map[string]interface{}{
			"type": stringEnum("filter", "where"),
			"query": map[string]interface{}{
				"type": "object",
				"description": "Condition object. " +
					"Logical: $and, $or, $not. " +
					"Equality: $eq, $neq, $ieq (case-insensitive eq), $ineq (case-insensitive neq). " +
					"Numeric: $gt, $lt, $gte, $lte. " +
					"String: $contains, $notcontains, $icontains, $inotcontains, $containsWords, $notcontainsWords, $icontainsWords, $inotcontainsWords. " +
					"Regex: $regex, $notregex, $iregex, $inotregex. " +
					"Existence: $notnull. " +
					"Each operator takes [field, value] except logical operators which take an array of conditions. " +
					"Example: {\"$and\": [{\"$eq\": [\"ServiceName\", \"api\"]}, {\"$eq\": [\"StatusCode\", \"STATUS_CODE_ERROR\"]}]}",
			},
		},
	}
}

func aggregateStageSchema() map[string]interface{} {
	aggregateFuncSchema := map[string]interface{}{
		"type":        "object",
		"description": "Aggregate entry. MUST use 'aggregates' (not 'aggregations') and 'as' (not 'alias'). Function MUST be an object like {\"$count\": []} — never a string like \"count\".",
		"required":    []string{"function", "as"},
		"properties": map[string]interface{}{
			"function": map[string]interface{}{
				"type": "object",
				"description": "Aggregation function as an object — NEVER a string. " +
					"No-field functions: {\"$count\": []}, {\"$rate\": []}. " +
					"Single-field functions: {\"$avg\": [\"Duration\"]}, {\"$sum\": [\"Duration\"]}, {\"$min\": [\"Duration\"]}, {\"$max\": [\"Duration\"]}, {\"$median\": [\"Duration\"]}, {\"$stddev\": [\"Duration\"]}, {\"$stddev_pop\": [\"Duration\"]}, {\"$variance\": [\"Duration\"]}, {\"$variance_pop\": [\"Duration\"]}. " +
					"Two-param functions: {\"$quantile\": [0.95, \"Duration\"]}, {\"$quantile_exact\": [0.99, \"Duration\"]}, {\"$apdex_score\": [0.5, \"Duration\"]}.",
			},
			"as": map[string]interface{}{
				"type":        "string",
				"description": "Output field name for this aggregate. Use 'as', NOT 'alias'.",
			},
		},
		"additionalProperties": false,
	}

	return map[string]interface{}{
		"type":        "object",
		"description": "Aggregate stage. CRITICAL: use 'aggregates' (NOT 'aggregations') and 'groupby' (NOT 'group_by'). Each aggregate entry uses 'as' (NOT 'alias') and function as object (NOT string).",
		"required":    []string{"type", "aggregates"},
		"properties": map[string]interface{}{
			"type": stringEnum("aggregate"),
			"aggregates": map[string]interface{}{
				"type":  "array",
				"items": aggregateFuncSchema,
			},
			"groupby": map[string]interface{}{
				"type":        "object",
				"description": "Group-by fields as {\"FieldName\": \"alias\"}. Use 'groupby', NOT 'group_by'. Example: {\"ServiceName\": \"service\", \"SpanName\": \"span\"}",
			},
		},
		"additionalProperties": false,
	}
}

func windowAggregateStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Window aggregate stage: time-bucketed aggregation. Use for rates or counts over time windows.",
		"required":    []string{"type", "function", "as", "window"},
		"properties": map[string]interface{}{
			"type": stringEnum("window_aggregate"),
			"function": map[string]interface{}{
				"type":        "object",
				"description": "Aggregation function object. Example: {\"$count\": []}",
			},
			"as": map[string]interface{}{
				"type": "string",
			},
			"window": map[string]interface{}{
				"type":        "array",
				"description": "Window duration as [\"duration\", \"unit\"]. Units: minutes, seconds, hours. Example: [\"5\", \"minutes\"]",
			},
			"groupby": map[string]interface{}{
				"type":        "object",
				"description": "Optional group-by fields. Use 'groupby', NOT 'group_by'.",
			},
		},
	}
}

func transformStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Transform stage: computes new fields from existing ones.",
		"required":    []string{"type", "transforms"},
		"properties": map[string]interface{}{
			"type": stringEnum("transform"),
			"transforms": map[string]interface{}{
				"type": "array",
				"description": "List of transform entries, each with 'function' (object) and 'as' (string or array of strings). " +
					"Functions: {\"$split\": [\"field\", \"delim\", index]}, {\"$split_into\": [\"field\", \"delim\"]}, " +
					"{\"$concat\": [\"f1\", \"f2\"]}, {\"$join\": [\"sep\", \"f1\", \"f2\"]}, " +
					"{\"$replace_regex\": [\"field\", \"pattern\", \"replacement\"]}, " +
					"{\"$length\": [\"field\"]}, {\"$arrayElement\": [\"field\", index]}.",
				"items": map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
}

func parseStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Parse stage: extracts fields from span content using json, regexp, or logfmt parsers.",
		"required":    []string{"type", "parser"},
		"properties": map[string]interface{}{
			"type":   stringEnum("parse"),
			"parser": stringEnum("json", "regexp", "logfmt"),
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Regexp pattern with named capture groups (for regexp parser). Example: (?P<user_id>\\d+)",
			},
			"labels": map[string]interface{}{
				"type":        "object",
				"description": "Field mappings for json parsing.",
			},
		},
	}
}

func selectStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Select stage: projects specific fields in the output.",
		"required":    []string{"type", "labels"},
		"properties": map[string]interface{}{
			"type": stringEnum("select"),
			"labels": map[string]interface{}{
				"type":        "object",
				"description": "Fields to include in output as {\"FieldName\": \"alias\"}.",
			},
		},
	}
}
