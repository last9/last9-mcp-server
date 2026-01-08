package logs

import (
	"fmt"
	"sort"
	"strings"
)

var standardLogFields = map[string]struct{}{
	"Body":         {},
	"ServiceName":  {},
	"SeverityText": {},
	"Timestamp":    {},
}

func validateLogJSONQuery(pipeline []interface{}, attributes []string) error {
	fields, derived := collectLogFieldsFromPipeline(pipeline)

	allowed := make(map[string]struct{}, len(attributes))
	for _, attr := range attributes {
		if strings.TrimSpace(attr) == "" {
			continue
		}
		allowed[attr] = struct{}{}
	}

	var invalid []string
	for field := range fields {
		if field == "" {
			continue
		}
		if _, ok := derived[field]; ok {
			continue
		}
		if _, ok := standardLogFields[field]; ok {
			continue
		}
		if len(allowed) == 0 {
			invalid = append(invalid, field)
			continue
		}
		if isAllowedAttributeField(field, allowed) {
			continue
		}
		invalid = append(invalid, field)
	}

	if len(invalid) == 0 {
		return nil
	}

	sort.Strings(invalid)
	return fmt.Errorf("log query uses unsupported fields: %s", strings.Join(invalid, ", "))
}

func collectLogFieldsFromPipeline(pipeline []interface{}) (map[string]struct{}, map[string]struct{}) {
	fields := make(map[string]struct{})
	derived := make(map[string]struct{})

	for _, stage := range pipeline {
		stageMap, ok := stage.(map[string]interface{})
		if !ok {
			continue
		}
		stageType, _ := stageMap["type"].(string)
		switch stageType {
		case "filter":
			collectFieldsFromFilter(stageMap["query"], fields)
		case "aggregate":
			collectFieldsFromAggregates(stageMap["aggregates"], fields)
			collectFieldsFromGroupBy(stageMap["groupby"], fields)
		case "window_aggregate":
			collectFieldsFromWindowAggregate(stageMap["function"], fields)
			collectFieldsFromGroupBy(stageMap["groupby"], fields)
		case "parse":
			collectDerivedFieldsFromParse(stageMap["labels"], derived)
		}
	}

	return fields, derived
}

func collectFieldsFromFilter(query interface{}, fields map[string]struct{}) {
	switch value := query.(type) {
	case map[string]interface{}:
		for key, raw := range value {
			switch key {
			case "$and", "$or":
				clauses, ok := raw.([]interface{})
				if !ok {
					continue
				}
				for _, clause := range clauses {
					collectFieldsFromFilter(clause, fields)
				}
			case "$not":
				collectFieldsFromFilter(raw, fields)
			default:
				if !isFilterFieldOperator(key) {
					continue
				}
				list, ok := raw.([]interface{})
				if !ok || len(list) == 0 {
					continue
				}
				if field, ok := list[0].(string); ok {
					fields[field] = struct{}{}
				}
			}
		}
	case []interface{}:
		for _, item := range value {
			collectFieldsFromFilter(item, fields)
		}
	}
}

func collectFieldsFromAggregates(raw interface{}, fields map[string]struct{}) {
	aggregates, ok := raw.([]interface{})
	if !ok {
		return
	}
	for _, agg := range aggregates {
		aggMap, ok := agg.(map[string]interface{})
		if !ok {
			continue
		}
		functions, ok := aggMap["function"].(map[string]interface{})
		if !ok {
			continue
		}
		for op, payload := range functions {
			if op == "$count" {
				continue
			}
			list, ok := payload.([]interface{})
			if !ok || len(list) == 0 {
				continue
			}
			if op == "$quantile" {
				if len(list) > 1 {
					if field, ok := list[1].(string); ok {
						fields[field] = struct{}{}
					}
				}
				continue
			}
			if field, ok := list[0].(string); ok {
				fields[field] = struct{}{}
			}
		}
	}
}

func collectFieldsFromWindowAggregate(raw interface{}, fields map[string]struct{}) {
	functions, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for op, payload := range functions {
		if op == "$count" {
			continue
		}
		list, ok := payload.([]interface{})
		if !ok || len(list) == 0 {
			continue
		}
		if op == "$quantile" {
			if len(list) > 1 {
				if field, ok := list[1].(string); ok {
					fields[field] = struct{}{}
				}
			}
			continue
		}
		if field, ok := list[0].(string); ok {
			fields[field] = struct{}{}
		}
	}
}

func collectFieldsFromGroupBy(raw interface{}, fields map[string]struct{}) {
	groupBy, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for field := range groupBy {
		fields[field] = struct{}{}
	}
}

func collectDerivedFieldsFromParse(raw interface{}, derived map[string]struct{}) {
	labels, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for _, alias := range labels {
		if name, ok := alias.(string); ok && name != "" {
			derived[name] = struct{}{}
		}
	}
}

func isFilterFieldOperator(op string) bool {
	switch op {
	case "$eq", "$neq", "$gt", "$lt", "$gte", "$lte", "$contains", "$notcontains", "$regex", "$notregex":
		return true
	default:
		return false
	}
}

func isAllowedAttributeField(field string, allowed map[string]struct{}) bool {
	if _, ok := allowed[field]; ok {
		return true
	}
	if attr, ok := extractBracketField(field, "attributes['", "']"); ok {
		if _, ok := allowed[attr]; ok {
			return true
		}
		if _, ok := allowed["resource_"+attr]; ok {
			return true
		}
	}
	if attr, ok := extractBracketField(field, "attributes[\"", "\"]"); ok {
		if _, ok := allowed[attr]; ok {
			return true
		}
		if _, ok := allowed["resource_"+attr]; ok {
			return true
		}
	}
	if attr, ok := extractBracketField(field, "resource_attributes['", "']"); ok {
		if _, ok := allowed["resource_"+attr]; ok {
			return true
		}
		if _, ok := allowed[attr]; ok {
			return true
		}
	}
	if attr, ok := extractBracketField(field, "resource_attributes[\"", "\"]"); ok {
		if _, ok := allowed["resource_"+attr]; ok {
			return true
		}
		if _, ok := allowed[attr]; ok {
			return true
		}
	}
	if strings.HasPrefix(field, "resource_") {
		if _, ok := allowed[field]; ok {
			return true
		}
		if _, ok := allowed[strings.TrimPrefix(field, "resource_")]; ok {
			return true
		}
	}
	return false
}

func extractBracketField(value, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	trimmed := strings.TrimPrefix(value, prefix)
	trimmed = strings.TrimSuffix(trimmed, suffix)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
