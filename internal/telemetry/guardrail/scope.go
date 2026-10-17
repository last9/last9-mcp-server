package guardrail

import "strings"

// scopeFields name one service, workload or trace. Environment and namespace
// are left out: each still spans many services.
var scopeFields = map[string]bool{
	"ServiceName":                       true,
	"TraceId":                           true,
	"resources['service.name']":         true,
	"resources['k8s.deployment.name']":  true,
	"resources['k8s.statefulset.name']": true,
	"resources['k8s.daemonset.name']":   true,
	"resources['k8s.pod.name']":         true,
	"resources['k8s.container.name']":   true,
	"resources['container.name']":       true,
	"resources['host.name']":            true,
}

// PipelineScoped reports whether a logjson or tracejson pipeline restricts
// its scan to one service or stream before aggregating. Filters after the
// first aggregate act on its output rows and narrow nothing scanned.
// tracejson accepts `where` as an alias of `filter`.
func PipelineScoped(pipeline []map[string]any) bool {
	for _, stage := range pipeline {
		switch stage["type"] {
		case "aggregate", "window_aggregate":
			return false
		case "filter", "where":
			if conditionScoped(stage["query"]) {
				return true
			}
		}
	}
	return false
}

func conditionScoped(condition any) bool {
	cond, ok := condition.(map[string]any)
	if !ok {
		return false
	}
	for op, operands := range cond {
		switch op {
		case "$and":
			for _, c := range conditionList(operands) {
				if conditionScoped(c) {
					return true
				}
			}
		case "$or":
			branches := conditionList(operands)
			if len(branches) > 0 && allScoped(branches) {
				return true
			}
		case "$eq", "$ieq":
			if equalsScopeField(operands) {
				return true
			}
		}
	}
	return false
}

func allScoped(conditions []any) bool {
	for _, c := range conditions {
		if !conditionScoped(c) {
			return false
		}
	}
	return true
}

func equalsScopeField(operands any) bool {
	pair, ok := operands.([]any)
	if !ok || len(pair) != 2 {
		return false
	}
	field, _ := pair[0].(string)
	value, _ := pair[1].(string)
	return scopeFields[field] && strings.TrimSpace(value) != ""
}

func conditionList(operands any) []any {
	switch list := operands.(type) {
	case []any:
		return list
	case []map[string]any:
		out := make([]any, len(list))
		for i, c := range list {
			out[i] = c
		}
		return out
	}
	return nil
}
