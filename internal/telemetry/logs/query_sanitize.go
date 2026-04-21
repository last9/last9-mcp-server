package logs

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	logAttributeFieldRefPattern         = regexp.MustCompile(`^attributes\[(?:'[^'\[\]]+'|"[^"\[\]]+")\]$`)
	logResourceAttributeFieldRefPattern = regexp.MustCompile(`^resource_attributes\[(?:'[^'\[\]]+'|"[^"\[\]]+")\]$`)
	logKubernetesAliasPattern           = regexp.MustCompile(`^k8s(?:\.[A-Za-z0-9_/-]+)+$`)
	logSimpleFieldRefPattern            = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

var logFilterFieldOperators = map[string]int{
	"$contains":     0,
	"$eq":           0,
	"$gt":           0,
	"$gte":          0,
	"$icontains":    0,
	"$ieq":          0,
	"$iregex":       0,
	"$lt":           0,
	"$lte":          0,
	"$neq":          0,
	"$notcontains":  0,
	"$noticontains": 0,
	"$notiregex":    0,
	"$notregex":     0,
	"$regex":        0,
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
	sanitized := make([]map[string]interface{}, 0, len(stages))

	for stageIndex, stage := range stages {
		sanitizedStage := make(map[string]interface{}, len(stage))
		stagePath := fmt.Sprintf("logjson_query[%d]", stageIndex)

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
					"invalid filter condition key %q at %s: keys must be operators ($eq, $neq, $gt, $gte, $lt, $lte, $contains, $notcontains, $icontains, $noticontains, $regex, $notregex, $iregex, $notiregex, $ieq) or logical operators ($and, $or, $not); use the form {%q: [field, value]} — for example {\"$eq\": [\"ServiceName\", \"checkout\"]} — and call get_log_attributes if you need the exact field name",
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
	return sanitized, nil
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
	case logKubernetesAliasPattern.MatchString(trimmed):
		return fmt.Sprintf("resource_attributes['%s']", trimmed), nil
	case isCanonicalLogFieldRef(trimmed):
		return trimmed, nil
	case logSimpleFieldRefPattern.MatchString(trimmed):
		return trimmed, nil
	default:
		return "", fmt.Errorf(
			"invalid log field reference %q at %s: use ServiceName, attributes['field'], or resource_attributes['field']; call get_log_attributes if you need the exact field name",
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
		logResourceAttributeFieldRefPattern.MatchString(fieldRef)
}
