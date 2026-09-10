package dashboards

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Pure query lint — no I/O.
// Port of agents/supervisor/skills/dashboard_validation/lint.py.

const (
	severityError   = "error"
	severityWarning = "warning"
	timesliceGroupbyCol = "__ts__"
)

var (
	logqlRangeSelectorRe = regexp.MustCompile(`\[\d+(?:ms|[smhdwy])\]`)
	promqlDanglingOpRe   = regexp.MustCompile(`[+\-*/%^]\s*$|^\s*[+*/%^]`)
	timeseriesVizTypes   = map[string]struct{}{
		"timeseries": {},
		"line":       {},
		"bar":        {},
		"chart":      {},
	}
)

func lintFinding(rule, severity, message string) map[string]any {
	return map[string]any{"rule": rule, "severity": severity, "message": message}
}

func hasBlockingError(findings []map[string]any) bool {
	for _, f := range findings {
		if f["severity"] == severityError {
			return true
		}
	}
	return false
}

func balanced(expr string) bool {
	pairs := map[byte]byte{')': '(', '}': '{', ']': '['}
	var stack []byte
	var quote byte
	escaped := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '(' || ch == '{' || ch == '[' {
			stack = append(stack, ch)
		} else if ch == ')' || ch == '}' || ch == ']' {
			if len(stack) == 0 || stack[len(stack)-1] != pairs[ch] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0 && quote == 0
}

func lintPromQL(expr string) []map[string]any {
	if strings.TrimSpace(expr) == "" {
		return []map[string]any{lintFinding("promql_empty", severityError, "PromQL expression is empty")}
	}
	var findings []map[string]any
	if !balanced(expr) {
		findings = append(findings, lintFinding(
			"promql_unbalanced",
			severityError,
			"unbalanced parentheses/braces/brackets or unterminated quote",
		))
	}
	if promqlDanglingOpRe.MatchString(strings.TrimSpace(expr)) {
		findings = append(findings, lintFinding("promql_dangling_operator", severityError, "dangling binary operator"))
	}
	return findings
}

func parsePipeline(expr any) []any {
	switch e := expr.(type) {
	case []any:
		return e
	case []map[string]any:
		out := make([]any, len(e))
		for i, m := range e {
			out[i] = m
		}
		return out
	case string:
		if strings.TrimSpace(e) == "" {
			return nil
		}
		var parsed any
		if err := json.Unmarshal([]byte(e), &parsed); err != nil {
			return nil
		}
		if list, ok := parsed.([]any); ok {
			return list
		}
		return nil
	default:
		return nil
	}
}

func aggregateStages(pipeline []any) []map[string]any {
	var out []map[string]any
	for _, stage := range pipeline {
		m, ok := stage.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if t == "aggregate" || t == "aggregation" {
			out = append(out, m)
		}
	}
	return out
}

func stageHasTimeslice(stage map[string]any) bool {
	if isTruthy(stage["window"]) {
		return true
	}
	groupby := stage["groupby"]
	if groupby == nil {
		groupby = stage["group_by"]
	}
	for _, col := range asAnySlice(groupby) {
		var name string
		switch c := col.(type) {
		case map[string]any:
			name, _ = c["column"].(string)
		case string:
			name = c
		}
		if name == timesliceGroupbyCol {
			return true
		}
	}
	return false
}

func lintPipeline(expr any, queryType, vizType string) []map[string]any {
	pipeline := parsePipeline(expr)
	if pipeline == nil {
		return []map[string]any{lintFinding(
			queryType+"_invalid_pipeline",
			severityError,
			"pipeline expr is not a JSON array of stages",
		)}
	}
	if len(pipeline) == 0 {
		return []map[string]any{lintFinding(
			queryType+"_empty_pipeline",
			severityError,
			"pipeline has no stages",
		)}
	}

	var findings []map[string]any
	aggregates := aggregateStages(pipeline)
	viz := strings.ToLower(vizType)
	isTable := viz == "table"

	if len(aggregates) == 0 && isTable {
		findings = append(findings, lintFinding(
			queryType+"_table_requires_aggregation",
			severityError,
			"table panels require an aggregation stage in the pipeline",
		))
	}

	if _, ok := timeseriesVizTypes[viz]; ok {
		if len(aggregates) == 0 {
			findings = append(findings, lintFinding(
				queryType+"_timeseries_requires_aggregation",
				severityError,
				viz+" visualization requires an aggregation stage",
			))
		} else {
			hasSlice := false
			for _, stage := range aggregates {
				if stageHasTimeslice(stage) {
					hasSlice = true
					break
				}
			}
			if !hasSlice {
				findings = append(findings, lintFinding(
					queryType+"_timeseries_requires_timeslice",
					severityError,
					viz+" visualization requires a time-sliced aggregate "+
						"(aggregate stage with a window)",
				))
			}
		}
	}
	return findings
}

func lintLogQL(expr string) []map[string]any {
	if strings.TrimSpace(expr) == "" {
		return []map[string]any{lintFinding("log_ql_empty", severityError, "LogQL expression is empty")}
	}
	var findings []map[string]any
	if !logqlRangeSelectorRe.MatchString(expr) {
		findings = append(findings, lintFinding(
			"log_ql_missing_range_selector",
			severityError,
			"LogQL query must include a range selector like [5m]",
		))
	}
	if !balanced(expr) {
		findings = append(findings, lintFinding("log_ql_unbalanced", severityError, "unbalanced brackets or quote"))
	}
	return findings
}

func lintTarget(queryType string, expr any, vizType string) []map[string]any {
	switch queryType {
	case "promql":
		s, _ := expr.(string)
		return lintPromQL(s)
	case "log_json", "trace_json":
		return lintPipeline(expr, queryType, vizType)
	case "log_ql":
		s, _ := expr.(string)
		return lintLogQL(s)
	default:
		return []map[string]any{lintFinding(
			"unknown_query_type",
			severityWarning,
			"no lint rules for query_type '"+queryType+"'",
		)}
	}
}
