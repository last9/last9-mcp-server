package logs

func logStringEnum(values ...string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": values}
}

// GetLogsInputSchema returns a hand-crafted JSON Schema for the get_logs tool.
// This replaces the auto-generated schema from GetLogsArgs so that logjson_query
// items have a detailed anyOf definition guiding stage shapes. Schema stays
// deliberately loose on stage required/additionalProperties so wrong-key
// mistakes (window_minutes, format, …) reach validateLogJSONQuery tip errors
// instead of an opaque anyOf failure from the SDK.
func GetLogsInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"logjson_query": logjsonQuerySchema(),
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
				"description": "Number of minutes to look back from now (default: 5, minimum: 1). Use for relative windows.",
			},
			"limit": map[string]interface{}{
				"type":        []string{"integer", "null"},
				"description": "Maximum number of rows to return (optional, default: 5000 for chunked raw queries).",
			},
			"index": map[string]interface{}{
				"type":        []string{"string", "null"},
				"description": "Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit when the user did not specify an index.",
			},
		},
		"required":             []string{"logjson_query"},
		"additionalProperties": false,
	}
}

// logjsonQuerySchema returns the JSON Schema for the logjson_query array.
// The anyOf branches are descriptive guidance — they do NOT own rejection.
// The permissive catch-all at the end ensures that wrong types, unknown enums,
// and partial stages (e.g. SQL strings as query, type "sort", parser "grok",
// function as string) pass schema validation and reach validateLogJSONQuery,
// which returns actionable tip errors instead of opaque anyOf failures.
func logjsonQuerySchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": "JSON pipeline query for logs. An ordered list of stages: filter → parse → aggregate/window_aggregate. NOT SQL, NOT a query string. Each stage is an object with a 'type' field.",
		"items": map[string]interface{}{
			"anyOf": []interface{}{
				logFilterStageSchema(),
				logParseStageSchema(),
				logAggregateStageSchema(),
				logWindowAggregateStageSchema(),
				map[string]interface{}{"type": "object"}, // catch-all: wrong types/enums reach validate tips
			},
		},
	}
}

func logFilterStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Filter stage: narrows the dataset by condition. Always wrap in $and.",
		// Only "type" is schema-required; validateLogJSONQuery owns "query" tips so
		// {"type":"filter","conditions":{...}} mistakes reach the conditions tip.
		"required": []string{"type"},
		"properties": map[string]interface{}{
			"type": logStringEnum("filter"),
			"query": map[string]interface{}{
				"type": "object",
				"description": "Condition object. " +
					"Logical: $and, $or, $not. " +
					"Equality: $eq, $neq, $ieq (case-insensitive eq). " +
					"Numeric: $gt, $lt, $gte, $lte. " +
					"String: $contains, $containsWords, $notcontains, $icontains, $regex. " +
					"Example: {\"$and\": [{\"$eq\": [\"SeverityText\", \"ERROR\"]}, {\"$eq\": [\"ServiceName\", \"api\"]}]}",
			},
		},
	}
}

func logParseStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Parse stage: extracts fields from Body using json, logfmt, or regexp parsers. Never use 'format' — key is 'parser'.",
		// Only "type" is schema-required; validateLogJSONQuery owns "parser" so a
		// {"type":"parse","format":"json"} mistake reaches the format tip.
		"required": []string{"type"},
		"properties": map[string]interface{}{
			"type":    logStringEnum("parse"),
			"parser":  logStringEnum("json", "logfmt", "regexp"),
			"field":   map[string]interface{}{"type": "string", "description": "Field to parse (typically Body). Defaults to Body when omitted."},
			"pattern": map[string]interface{}{"type": "string", "description": "Regexp pattern with named capture groups (regexp parser only)."},
			"labels":  map[string]interface{}{"type": "object", "description": "Field mappings for json/logfmt parsing."},
		},
	}
}

func logAggregateStageSchema() map[string]interface{} {
	// No required on aggregateFuncSchema; validateLogJSONQuery owns function/as/alias
	// tips so {"function":{...},"alias":"count"} mistakes reach the alias tip.
	aggregateFuncSchema := map[string]interface{}{
		"type":        "object",
		"description": "Aggregate entry. Use 'aggregates' (plural) and 'as'. Function MUST be an object like {\"$count\": []} — never a string.",
		"properties": map[string]interface{}{
			"function": map[string]interface{}{
				"type":        "object",
				"description": "Aggregation function object. Examples: {\"$count\": []}, {\"$quantile\": [0.99, \"field\"]}.",
			},
			"as": map[string]interface{}{
				"type":        "string",
				"description": "Output field name. Use 'as', NOT 'alias'.",
			},
		},
	}

	return map[string]interface{}{
		"type":        "object",
		"description": "Aggregate stage. Use 'aggregates' (NOT 'aggregations') and 'groupby' (NOT 'group_by').",
		// Only "type" is schema-required; validateLogJSONQuery owns "aggregates" tips
		// so {"type":"aggregate","aggs":[...]} mistakes reach the aggs tip.
		"required": []string{"type"},
		"properties": map[string]interface{}{
			"type": logStringEnum("aggregate"),
			"aggregates": map[string]interface{}{
				"type":  "array",
				"items": aggregateFuncSchema,
			},
			"groupby": map[string]interface{}{
				"type":        "object",
				"description": "Group-by fields as {\"FieldName\": \"alias\"}.",
			},
		},
	}
}

func logWindowAggregateStageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Window aggregate stage: time-bucketed counts, trends, or percentiles. CRITICAL: use 'function'+'as'+'window' — NOT 'aggregates', NOT 'window_minutes', NOT 'TimeBucket'.",
		// Only "type" is schema-required; validateLogJSONQuery owns function/as/window
		// so {"type":"window_aggregate","window_minutes":1,...} reaches the tip.
		"required": []string{"type"},
		"properties": map[string]interface{}{
			"type": logStringEnum("window_aggregate"),
			"function": map[string]interface{}{
				"type":        "object",
				"description": "Aggregation function object. Examples: {\"$count\": []}, {\"$quantile\": [0.99, \"field\"]}",
			},
			"as": map[string]interface{}{
				"type":        "string",
				"description": "Output field name for the window result.",
			},
			"window": map[string]interface{}{
				"type":        "array",
				"description": "Window duration as [\"duration\", \"unit\"]. Units: minutes, seconds, hours. Example: [\"1\", \"minutes\"]",
			},
			"groupby": map[string]interface{}{
				"type":        "object",
				"description": "Optional group-by fields as {\"FieldName\": \"alias\"}.",
			},
		},
	}
}
