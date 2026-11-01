package nameresolution

import (
	"encoding/json"
	"maps"
	"regexp"

	"last9-mcp/internal/utils"
)

func (p ServicePipeline) EnvironmentMatchers() ([]string, bool) {
	var matchers []string
	query := p.Scope[0]["query"].(map[string]any)
	conditions, _ := query["$and"].([]any)
	for _, condition := range conditions {
		for op, value := range condition.(map[string]any) {
			args := value.([]any)
			env, ok := args[1].(string)
			if !ok {
				return nil, false
			}
			switch op {
			case "$eq":
				matchers = append(matchers, `env="`+utils.EscapePromQLLabel(env)+`"`)
			case "$ieq":
				matchers = append(matchers, `env=~"`+utils.EscapePromQLLabel("(?i)"+regexp.QuoteMeta(env))+`"`)
			default:
				return nil, false
			}
		}
	}
	return matchers, true
}

func VerifiedEmpty(result map[string]any) bool {
	result = maps.Clone(result)
	for _, key := range []string{"status", "warnings", "error", "errors", "partial", "truncated", "logs_truncated", "search_stats", "l9_result", "total_matching_lines"} {
		if raw, ok := result[key].(json.RawMessage); ok {
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				return false
			}
			result[key] = decoded
		}
	}

	if total, ok := result["total_matching_lines"]; ok && total != nil && total != float64(0) && total != int(0) {
		return false
	}
	if metadata, ok := result["l9_result"]; ok && metadata != nil {
		fields, ok := metadata.(map[string]any)
		if !ok {
			return false
		}
		if partial, present := fields["partial"]; present && partial != false {
			return false
		}
	}
	if result["status"] != "success" {
		return false
	}
	for _, key := range []string{"warnings", "error", "errors", "partial", "truncated", "logs_truncated"} {
		if value, ok := result[key]; ok && value != nil {
			switch value := value.(type) {
			case bool:
				if value {
					return false
				}
			case []any:
				if len(value) != 0 {
					return false
				}
			default:
				return false
			}
		}
	}
	if stats, ok := result["search_stats"].(map[string]any); ok {
		if failed, ok := stats["chunks_failed"]; ok && failed != float64(0) && failed != int(0) {
			return false
		}
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		return false
	}
	switch items := data["result"].(type) {
	case []any:
		return items != nil && len(items) == 0
	case []json.RawMessage:
		return items != nil && len(items) == 0
	default:
		return false
	}
}
