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

func validateAggregateStage(stage map[string]interface{}, path string) error {
	rawAggregates, ok := stage["aggregates"]
	if !ok {
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

		if _, bad := entry["alias"]; bad {
			return fmt.Errorf(
				"%s.aggregates[%d]: invalid key \"alias\" — use \"as\" instead. "+
					"Example: {\"function\": {\"$count\": []}, \"as\": \"count\"}",
				path, j,
			)
		}

		if fn, exists := entry["function"]; exists {
			if _, isString := fn.(string); isString {
				return fmt.Errorf(
					"%s.aggregates[%d]: \"function\" must be an object, not a string. "+
						"Wrong: \"function\": \"count\". "+
						"Correct: \"function\": {\"$count\": []}. "+
						"Other examples: {\"$avg\": [\"Duration\"]}, {\"$sum\": [\"Duration\"]}, {\"$quantile\": [0.95, \"Duration\"]}",
					path, j,
				)
			}
		}
	}
	return nil
}
