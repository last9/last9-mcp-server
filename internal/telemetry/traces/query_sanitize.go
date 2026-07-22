package traces

import (
	"fmt"
	"sort"
	"strings"
)

var traceFilterFieldOperators = map[string]struct{}{
	"$contains":          {},
	"$containsWords":     {},
	"$eq":                {},
	"$gt":                {},
	"$gte":               {},
	"$icontains":         {},
	"$icontainsWords":    {},
	"$ieq":               {},
	"$ineq":              {},
	"$inotcontains":      {},
	"$inotcontainsWords": {},
	"$inotregex":         {},
	"$iregex":            {},
	"$lt":                {},
	"$lte":               {},
	"$neq":               {},
	"$notcontains":       {},
	"$notcontainsWords":  {},
	"$notregex":          {},
	"$notnull":           {},
	"$regex":             {},
}

var traceFilterLogicalOperators = map[string]struct{}{
	"$and": {},
	"$or":  {},
	"$not": {},
}

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

		if stageType == "filter" || stageType == "where" {
			if query, ok := stage["query"]; ok {
				// The backend has no $exists operator — an unknown operator
				// compiles to `1=1` and silently matches every span instead of
				// erroring, so rewrite it to the working idiom before
				// validation ever sees it. Covers $exists nested inside
				// $and/$or/$not since it walks the same condition tree.
				rewriteExistsOperator(query)
				if err := validateTraceFilterCondition(query, path+".query"); err != nil {
					return err
				}
				if err := validateFilterFields(query, path+".query"); err != nil {
					return err
				}
				// tracejson requires the top-level filter query to be wrapped in a
				// logical operator. Models (especially weaker ones) frequently emit a
				// bare single condition like {"$eq": [...]}, which the spec rejects.
				// Normalize it to {"$and": [{"$eq": [...]}]} so the forwarded query is
				// always valid regardless of how the model phrased it.
				stage["query"] = wrapTopLevelFilterQuery(query)
			}
		}

		if stageType == "aggregate" {
			if err := validateAggregateStage(stage, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// wrapTopLevelFilterQuery ensures the top-level filter query is wrapped in a
// logical operator, as the tracejson spec requires. A query that is already a
// single logical operator ($and/$or/$not) is returned unchanged; anything else
// — one or more bare field operators — is wrapped in $and, with each condition
// becoming its own element. Keys are sorted so the output is deterministic.
func wrapTopLevelFilterQuery(query interface{}) interface{} {
	queryMap, ok := query.(map[string]interface{})
	if !ok || len(queryMap) == 0 {
		return query
	}

	if len(queryMap) == 1 {
		for key := range queryMap {
			if _, isLogical := traceFilterLogicalOperators[key]; isLogical {
				return queryMap
			}
		}
	}

	keys := make([]string, 0, len(queryMap))
	for key := range queryMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	conditions := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		conditions = append(conditions, map[string]interface{}{key: queryMap[key]})
	}
	return map[string]interface{}{"$and": conditions}
}

// rewriteExistsOperator walks a filter condition tree and rewrites every
// $exists leaf ({"$exists": [field]}) in place to the working existence idiom
// {"$neq": [field, ""]}. The backend has no $exists operator; passing it
// through compiles to `1=1` and silently matches every span rather than
// erroring, so models' natural "$exists" output is normalized here instead of
// rejected. Recurses through $and/$or/$not so nested conditions are covered.
func rewriteExistsOperator(value interface{}) {
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			rewriteExistsOperator(item)
		}
	case map[string]interface{}:
		for key, item := range typed {
			if _, isLogical := traceFilterLogicalOperators[key]; isLogical {
				rewriteExistsOperator(item)
				continue
			}
			if key != "$exists" {
				continue
			}
			args, ok := item.([]interface{})
			if !ok || len(args) == 0 {
				continue
			}
			delete(typed, "$exists")
			typed["$neq"] = []interface{}{args[0], ""}
		}
	}
}

func validateTraceFilterCondition(value interface{}, path string) error {
	switch typed := value.(type) {
	case []interface{}:
		for i, item := range typed {
			if err := validateTraceFilterCondition(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		for key, item := range typed {
			if _, isField := traceFilterFieldOperators[key]; isField {
				continue
			}
			if _, isLogical := traceFilterLogicalOperators[key]; isLogical {
				if err := validateTraceFilterCondition(item, path+"."+key); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf(
				"invalid filter condition key %q at %s: keys must be operators ($eq, $neq, $gt, $gte, $lt, $lte, $contains, $notcontains, $icontains, $inotcontains, $icontainsWords, $inotcontainsWords, $regex, $notregex, $iregex, $inotregex, $ieq, $ineq, $notnull, $containsWords, $notcontainsWords) or logical operators ($and, $or, $not); use the form {%q: [field, value]} — for example {\"$eq\": [\"ServiceName\", \"checkout\"]}. For existence checks use {\"$neq\": [field, \"\"]} — there is no $exists operator.",
				key,
				path,
				"$eq",
			)
		}
	}
	return nil
}

// validateFilterFields walks the query tree and checks every string field operand
// for common LLM mistakes: flat resource_ prefix, double-quoted attribute syntax,
// and dot-notation field names.
func validateFilterFields(value interface{}, path string) error {
	switch typed := value.(type) {
	case []interface{}:
		for i, item := range typed {
			if err := validateFilterFields(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		for key, item := range typed {
			if _, isLogical := traceFilterLogicalOperators[key]; isLogical {
				if err := validateFilterFields(item, path+"."+key); err != nil {
					return err
				}
				continue
			}
			// Field operators: args are [field, value] — check first element
			if args, ok := item.([]interface{}); ok && len(args) > 0 {
				if fieldStr, ok := args[0].(string); ok {
					if err := validateFieldSyntax(fieldStr, fmt.Sprintf("%s.%s[0]", path, key)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// validateFieldSyntax rejects field references that use incorrect syntax forms.
func validateFieldSyntax(field, path string) error {
	// Double-quoted bracket syntax: attributes["..."], resources["..."], or events["..."]
	if strings.HasPrefix(field, `attributes["`) ||
		strings.HasPrefix(field, `resources["`) ||
		strings.HasPrefix(field, `events["`) {
		corrected := strings.ReplaceAll(field, `"`, "'")
		return fmt.Errorf(
			"invalid field reference %q at %s: tracejson requires single quotes — use %q instead",
			field, path, corrected,
		)
	}

	// Dot-notation: ResourceAttributes.field → resources['field']
	if strings.HasPrefix(field, "ResourceAttributes.") {
		stripped := field[len("ResourceAttributes."):]
		return fmt.Errorf(
			"invalid field reference %q at %s: use resources['%s'] instead of dot notation",
			field, path, stripped,
		)
	}

	// Dot-notation: SpanAttributes.field → attributes['field']
	if strings.HasPrefix(field, "SpanAttributes.") {
		stripped := field[len("SpanAttributes."):]
		return fmt.Errorf(
			"invalid field reference %q at %s: use attributes['%s'] instead of dot notation",
			field, path, stripped,
		)
	}

	// Flat resource_ prefix: resource_department → resources['department']
	if strings.HasPrefix(field, "resource_") {
		if _, isTopLevel := traceTopLevelFields[field]; !isTopLevel {
			stripped := field[len("resource_"):]
			if stripped == "service.name" {
				return fmt.Errorf(
					"invalid field reference %q at %s: use \"ServiceName\" instead",
					field, path,
				)
			}
			return fmt.Errorf(
				"invalid field reference %q at %s: use resources['%s'] instead of the flat resource_ prefix",
				field, path, stripped,
			)
		}
	}

	// Flat event_ prefix: event_exception.type → events['exception.type']
	if strings.HasPrefix(field, "event_") {
		stripped := field[len("event_"):]
		return fmt.Errorf(
			"invalid field reference %q at %s: use events['%s'] instead of the flat event_ prefix",
			field, path, stripped,
		)
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
