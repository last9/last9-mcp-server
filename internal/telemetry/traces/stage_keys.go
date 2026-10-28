package traces

import (
	"fmt"
	"sort"
	"strings"
)

func keySet(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

var traceAllowedStageKeys = map[string]map[string]struct{}{
	"filter":           keySet("type", "query"),
	"where":            keySet("type", "query"),
	"parse":            keySet("type", "parser", "field", "pattern", "labels"),
	"transform":        keySet("type", "transforms"),
	"select":           keySet("type", "order", "limit", "offset", "labels"),
	"aggregate":        keySet("type", "aggregates", "groupby"),
	"window_aggregate": windowAggregateAllowedKeys,
}

func traceStageKeyTip(key, stageType string) string {
	switch {
	case key == "sql":
		return "pipelines are JSON stages, not SQL"
	case key == "order_by" && stageType == "aggregate":
		return "to rank groups, use a final select stage"
	case key == "table", key == "starttime", key == "endtime", key == "direction", key == "delim":
		return "not supported in get_traces pipelines"
	}
	return ""
}

func validateTraceStageKeyAllowlist(stage map[string]interface{}, stageType, path string) error {
	allowed, ok := traceAllowedStageKeys[stageType]
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(stage))
	for k := range stage {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic error for multiple bad keys
	for _, k := range keys {
		if _, ok := allowed[k]; ok {
			continue
		}
		list := make([]string, 0, len(allowed))
		for a := range allowed {
			list = append(list, a)
		}
		sort.Strings(list)
		msg := fmt.Sprintf("unknown key %q at %s for stage type %q; allowed keys: %s",
			k, path, stageType, strings.Join(list, ", "))
		if tip := traceStageKeyTip(k, stageType); tip != "" {
			msg += " (" + tip + ")"
		}
		return &tracePipelineError{category: traceCategoryUnknownStageKey, path: path, msg: msg}
	}
	return nil
}
