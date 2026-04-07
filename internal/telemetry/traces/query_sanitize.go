package traces

import "fmt"

// sanitizeTraceJSONQuery validates a tracejson_query pipeline before forwarding
// to the API. It catches common LLM mistakes and returns descriptive errors so
// the model can self-correct without waiting for a 400 from the upstream API.
func sanitizeTraceJSONQuery(stages []map[string]interface{}) error {
	for i, stage := range stages {
		stageType, _ := stage["type"].(string)
		path := fmt.Sprintf("tracejson_query[%d]", i)

		if err := validateStageKeys(stage, stageType, path); err != nil {
			return err
		}

		if stageType == "aggregate" {
			if err := validateAggregateStage(stage, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateStageKeys catches wrong top-level key names in a stage.
func validateStageKeys(stage map[string]interface{}, stageType, path string) error {
	if _, bad := stage["aggregations"]; bad {
		return fmt.Errorf(
			"%s (type=%q): invalid key \"aggregations\" — use \"aggregates\" instead. "+
				"Example: {\"type\": \"aggregate\", \"aggregates\": [{\"function\": {\"$count\": []}, \"as\": \"count\"}], \"groupby\": {\"ServiceName\": \"service\"}}",
			path, stageType,
		)
	}
	if _, bad := stage["group_by"]; bad {
		return fmt.Errorf(
			"%s (type=%q): invalid key \"group_by\" — use \"groupby\" instead. "+
				"Example: \"groupby\": {\"ServiceName\": \"service\", \"SpanName\": \"span\"}",
			path, stageType,
		)
	}
	return nil
}

// validateAggregateStage validates the contents of an aggregate stage.
func validateAggregateStage(stage map[string]interface{}, path string) error {
	rawAggregates, ok := stage["aggregates"]
	if !ok {
		// missing aggregates — the upstream API will return its own error;
		// not our job to duplicate all validation, just the LLM-mistake class.
		return nil
	}

	aggregates, ok := rawAggregates.([]interface{})
	if !ok {
		return nil
	}

	for j, rawEntry := range aggregates {
		entry, ok := rawEntry.(map[string]interface{})
		if !ok {
			continue
		}
		entryPath := fmt.Sprintf("%s.aggregates[%d]", path, j)

		// "alias" instead of "as"
		if _, bad := entry["alias"]; bad {
			return fmt.Errorf(
				"%s: invalid key \"alias\" — use \"as\" instead. "+
					"Example: {\"function\": {\"$count\": []}, \"as\": \"count\"}",
				entryPath,
			)
		}

		// function must be an object like {"$count": []}, not a string like "count"
		if fn, exists := entry["function"]; exists {
			if _, isString := fn.(string); isString {
				return fmt.Errorf(
					"%s: \"function\" must be an object, not a string. "+
						"Wrong: \"function\": \"count\". "+
						"Correct: \"function\": {\"$count\": []}. "+
						"Other examples: {\"$avg\": [\"Duration\"]}, {\"$sum\": [\"Duration\"]}, {\"$quantile\": [0.95, \"Duration\"]}",
					entryPath,
				)
			}
		}
	}
	return nil
}
