package logs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	logAttributeFieldRefPattern  = regexp.MustCompile(`^attributes\['[^'\[\]]+'\]$`)
	logResourceFieldRefPattern   = regexp.MustCompile(`^resources\['[^'\[\]]+'\]$`)
	logKubernetesAliasPattern    = regexp.MustCompile(`^k8s(?:\.[A-Za-z0-9_/-]+)+$`)
	logSimpleFieldRefPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

var logFilterFieldOperators = map[string]int{
	"$contains":        0,
	"$containsWords":   0,
	"$eq":              0,
	"$gt":              0,
	"$gte":             0,
	"$icontains":       0,
	"$icontainsWords":  0,
	"$ieq":             0,
	"$ineq":            0,
	"$inotcontains":    0,
	"$inotcontainsWords": 0,
	"$inotregex":       0,
	"$iregex":          0,
	"$lt":              0,
	"$lte":             0,
	"$neq":             0,
	"$notcontains":     0,
	"$notcontainsWords": 0,
	"$notregex":        0,
	"$regex":           0,
}

var logFilterLogicalOperators = map[string]struct{}{
	"$and": {},
	"$or":  {},
	"$not": {},
}

var logAggregateFieldArgIndexes = map[string][]int{
	"$avg":      {0},
	"$max":      {0},
	"$min":      {0},
	"$quantile": {1},
	"$sum":      {0},
}

func sanitizeLogJSONQuery(stages []map[string]interface{}) ([]map[string]interface{}, error) {
	return sanitizeLogJSONQueryPrefixed(stages, "logjson_query")
}

func sanitizeLogJSONQueryPrefixed(stages []map[string]interface{}, pathPrefix string) ([]map[string]interface{}, error) {
	if pathPrefix == "" {
		pathPrefix = "logjson_query"
	}
	sanitized := make([]map[string]interface{}, 0, len(stages))

	for stageIndex, stage := range stages {
		sanitizedStage := make(map[string]interface{}, len(stage))
		stagePath := fmt.Sprintf("%s[%d]", pathPrefix, stageIndex)

		for key, value := range stage {
			var (
				sanitizedValue interface{}
				err            error
			)

			switch key {
			case "query":
				sanitizedValue, err = sanitizeLogCondition(value, stagePath+".query")
			case "aggregates":
				sanitizedValue, err = sanitizeLogAggregates(value, stagePath+".aggregates")
			case "function":
				sanitizedValue, err = sanitizeLogFunction(value, stagePath+".function")
			case "groupby":
				sanitizedValue, err = sanitizeLogGroupBy(value, stagePath+".groupby")
			default:
				sanitizedValue = value
			}
			if err != nil {
				return nil, err
			}

			sanitizedStage[key] = sanitizedValue
		}

		sanitized = append(sanitized, sanitizedStage)
	}

	return sanitized, nil
}

func sanitizeLogCondition(value interface{}, path string) (interface{}, error) {
	switch typed := value.(type) {
	case []interface{}:
		sanitized := make([]interface{}, len(typed))
		for index, item := range typed {
			next, err := sanitizeLogCondition(item, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			sanitized[index] = next
		}
		return sanitized, nil
	case map[string]interface{}:
		sanitized := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			fieldArgIndex, isFieldOperator := logFilterFieldOperators[key]
			if isFieldOperator {
				next, err := sanitizeLogFieldOperatorArgs(item, fieldArgIndex, path+"."+key)
				if err != nil {
					return nil, err
				}
				sanitized[key] = next
				continue
			}

			if _, isLogicalOperator := logFilterLogicalOperators[key]; !isLogicalOperator {
				return nil, fmt.Errorf(
					"invalid filter condition key %q at %s: keys must be operators ($eq, $neq, $ieq, $ineq, $gt, $gte, $lt, $lte, $contains, $notcontains, $icontains, $inotcontains, $containsWords, $notcontainsWords, $icontainsWords, $inotcontainsWords, $regex, $notregex, $iregex, $inotregex) or logical operators ($and, $or, $not); use the form {%q: [field, value]} — for example {\"$eq\": [\"ServiceName\", \"checkout\"]} — and call get_log_attributes if you need the exact field name",
					key,
					path,
					"$eq",
				)
			}

			next, err := sanitizeLogCondition(item, path+"."+key)
			if err != nil {
				return nil, err
			}
			sanitized[key] = next
		}
		return sanitized, nil
	default:
		return value, nil
	}
}

func sanitizeLogFieldOperatorArgs(value interface{}, fieldArgIndex int, path string) (interface{}, error) {
	args, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf(
			"invalid arguments for field operator at %s: expected an array like [field, value], got %T — use the form {\"$eq\": [\"ServiceName\", \"checkout\"]}",
			path,
			value,
		)
	}

	sanitized := append([]interface{}(nil), args...)
	if fieldArgIndex >= len(sanitized) {
		return sanitized, nil
	}

	fieldRef, ok := sanitized[fieldArgIndex].(string)
	if !ok {
		return sanitized, nil
	}

	next, err := sanitizeLogFieldRef(fieldRef, fmt.Sprintf("%s[%d]", path, fieldArgIndex))
	if err != nil {
		return nil, err
	}
	sanitized[fieldArgIndex] = next

	// last9/api logjson requires comparison values as strings. JSON numbers/bools
	// from model tool calls (e.g. 500 instead of "500") produce opaque
	// "invalid JSON pipeline" 400s — coerce in place.
	valueIndex := fieldArgIndex + 1
	if valueIndex < len(sanitized) {
		coerced, err := coerceLogFilterValueToString(sanitized[valueIndex], fmt.Sprintf("%s[%d]", path, valueIndex))
		if err != nil {
			return nil, err
		}
		sanitized[valueIndex] = coerced
	}
	return sanitized, nil
}

func coerceLogFilterValueToString(value interface{}, path string) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), nil
		}
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case float32:
		f := float64(typed)
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10), nil
		}
		return strconv.FormatFloat(f, 'g', -1, 32), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf(
			"invalid filter value at %s: expected a string (got %T %#v) — use {\"$eq\":[\"attributes['status_code']\",\"500\"]} with a string value",
			path, value, value,
		)
	}
}

func sanitizeLogAggregates(value interface{}, path string) (interface{}, error) {
	aggregates, ok := value.([]interface{})
	if !ok {
		return value, nil
	}

	sanitized := make([]interface{}, len(aggregates))
	for index, aggregate := range aggregates {
		aggregateMap, ok := aggregate.(map[string]interface{})
		if !ok {
			sanitized[index] = aggregate
			continue
		}

		sanitizedAggregate := make(map[string]interface{}, len(aggregateMap))
		for key, item := range aggregateMap {
			if key == "function" {
				next, err := sanitizeLogFunction(item, fmt.Sprintf("%s[%d].function", path, index))
				if err != nil {
					return nil, err
				}
				sanitizedAggregate[key] = next
				continue
			}
			sanitizedAggregate[key] = item
		}
		sanitized[index] = sanitizedAggregate
	}

	return sanitized, nil
}

func sanitizeLogFunction(value interface{}, path string) (interface{}, error) {
	functionMap, ok := value.(map[string]interface{})
	if !ok {
		return value, nil
	}

	sanitized := make(map[string]interface{}, len(functionMap))
	for operator, rawArgs := range functionMap {
		args, ok := rawArgs.([]interface{})
		if !ok {
			sanitized[operator] = rawArgs
			continue
		}

		sanitizedArgs := append([]interface{}(nil), args...)
		for _, fieldArgIndex := range logAggregateFieldArgIndexes[operator] {
			if fieldArgIndex >= len(sanitizedArgs) {
				continue
			}

			fieldRef, ok := sanitizedArgs[fieldArgIndex].(string)
			if !ok {
				continue
			}

			next, err := sanitizeLogFieldRef(fieldRef, fmt.Sprintf("%s.%s[%d]", path, operator, fieldArgIndex))
			if err != nil {
				return nil, err
			}
			sanitizedArgs[fieldArgIndex] = next
		}

		sanitized[operator] = sanitizedArgs
	}

	return sanitized, nil
}

func sanitizeLogGroupBy(value interface{}, path string) (interface{}, error) {
	groupBy, ok := value.(map[string]interface{})
	if !ok {
		return value, nil
	}

	sanitized := make(map[string]interface{}, len(groupBy))
	originalBySanitized := make(map[string]string, len(groupBy))
	for fieldRef, alias := range groupBy {
		next, err := sanitizeLogFieldRef(fieldRef, path+"."+fieldRef)
		if err != nil {
			return nil, err
		}

		if previous, exists := originalBySanitized[next]; exists {
			return nil, fmt.Errorf(
				"groupby collision at %s: %q and %q both normalize to %q",
				path,
				previous,
				fieldRef,
				next,
			)
		}

		originalBySanitized[next] = fieldRef
		sanitized[next] = alias
	}

	return sanitized, nil
}

func sanitizeLogFieldRef(fieldRef, path string) (string, error) {
	trimmed := strings.TrimSpace(fieldRef)
	if trimmed == "" {
		return fieldRef, nil
	}

	switch {
	case trimmed == "service.name":
		return "ServiceName", nil
	case strings.HasPrefix(trimmed, `attributes["`) || strings.HasPrefix(trimmed, `resources["`):
		corrected := strings.ReplaceAll(trimmed, `"`, "'")
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: use single quotes — %q; call get_log_attributes if you need the exact field name",
			trimmed, path, corrected,
		)
	case logKubernetesAliasPattern.MatchString(trimmed):
		return fmt.Sprintf("resources['%s']", trimmed), nil
	case isCanonicalLogFieldRef(trimmed):
		return trimmed, nil
	case isTraceOnlyLogField(trimmed):
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: %q is a trace-only field and is not valid in logs — use ServiceName, SeverityText, Body, attributes['…'], or resources['…']; call get_log_attributes if you need the exact field name",
			trimmed, path, trimmed,
		)
	case strings.HasPrefix(trimmed, "resource_"):
		stripped := trimmed[len("resource_"):]
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: use resources['%s'] instead of the flat resource_ prefix; call get_log_attributes if you need the exact field name",
			trimmed, path, stripped,
		)
	case logSimpleFieldRefPattern.MatchString(trimmed):
		// Bare single-token names (e.g. community_member_id) look plausible but
		// are invalid unless they are a known top-level log field. Fail closed with
		// the attributes['…'] form so models can self-correct (ENG-1410).
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: never emit bare field names (dotted or single-token) — use attributes['%s'] or resources['%s']; only ServiceName, Body, SeverityText, Timestamp may be bare; call get_log_attributes if you need the exact field name",
			trimmed, path, trimmed, trimmed,
		)
	default:
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: use ServiceName, attributes['field'], or resources['field']; call get_log_attributes if you need the exact field name",
			trimmed,
			path,
		)
	}
}

func isCanonicalLogFieldRef(fieldRef string) bool {
	switch fieldRef {
	case "Body", "ServiceName", "SeverityText", "Timestamp":
		return true
	}

	return logAttributeFieldRefPattern.MatchString(fieldRef) ||
		logResourceFieldRefPattern.MatchString(fieldRef)
}

func isTraceOnlyLogField(fieldRef string) bool {
	_, ok := traceOnlyLogFields[fieldRef]
	return ok
}
