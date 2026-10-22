package logs

import (
	"encoding/json"
	"time"

	"last9-mcp/internal/utils"
)

type getLogsGuidanceContext struct {
	startMs        int64
	endMs          int64
	effectiveLimit int
	maxLimit       int
}

type logTruncationState struct {
	returned       int
	totalMatching  int
	effectiveLimit int
}

func addGetLogsGuidance(result map[string]interface{}, args GetLogsArgs, guidance getLogsGuidanceContext) {
	kind, items, err := extractResultItems(result)
	if err != nil {
		return
	}
	if len(items) == 0 || countLogEntriesInResultItems(items) == 0 {
		result["l9_guidance"] = utils.EmptyResultGuidanceMap("logs", logsGuidanceOptions(args, guidance.startMs, guidance.endMs))
		return
	}
	returned := countLogEntriesInResultItems(items)
	totalMatching := logTotalMatching(result)
	if kind == "streams" && isLogResultTruncated(result, logTruncationState{
		returned:       returned,
		totalMatching:  totalMatching,
		effectiveLimit: guidance.effectiveLimit,
	}) {
		opts := utils.TruncatedResultOptions{
			Subject:  "logs",
			Returned: returned,
			Total:    totalMatching,
			MaxLimit: guidance.maxLimit,
		}
		result["l9_guidance"] = map[string]any{
			"message": utils.TruncatedResultAdvice(opts),
		}
	}
}

func addGetLogsAggregateGuidance(result map[string]interface{}, guidance utils.TruncatedResultOptions) {
	l9Result, _ := result["l9_result"].(map[string]interface{})
	if l9Result == nil {
		return
	}
	if partial, _ := l9Result["partial"].(bool); !partial {
		return
	}
	l9Result["hint"] = utils.TruncatedResultAdvice(guidance)
}

func addServiceLogsGuidance(response *ServiceLogsResponse, args GetServiceLogsArgs, limit int) {
	opts := utils.ResultGuidanceOptions{
		Window:      utils.WindowFromRange(parseServiceLogTime(response.StartTime), parseServiceLogTime(response.EndTime)),
		ServiceName: args.ServiceName,
		Environment: args.Env,
		AdditionalChecks: []string{
			"inspect available fields with `get_log_attributes_for_pipeline`",
			"verify the index param if one was supplied",
		},
	}
	if response.Count == 0 {
		response.Hint = utils.EmptyResultAdvice("logs", opts)
		return
	}
	if response.TotalMatchingLines != nil && *response.TotalMatchingLines > response.Count {
		response.Hint = utils.TruncatedResultAdvice(utils.TruncatedResultOptions{
			Subject:  "logs",
			Returned: response.Count,
			Total:    *response.TotalMatchingLines,
			MaxLimit: utils.LogSearchMaxSampleSize,
		})
		return
	}
	if response.LogsTruncated != nil && *response.LogsTruncated {
		response.Hint = utils.TruncatedResultAdvice(utils.TruncatedResultOptions{
			Subject:  "logs",
			Returned: response.Count,
			MaxLimit: utils.LogSearchMaxSampleSize,
		})
		return
	}
	if response.coverageStatus == "partial" || (limit > 0 && response.Count >= limit) {
		response.Hint = utils.TruncatedResultAdvice(utils.TruncatedResultOptions{
			Subject:  "logs",
			Returned: response.Count,
			MaxLimit: utils.LogSearchMaxSampleSize,
		})
	}
}

func logsGuidanceOptions(args GetLogsArgs, startMs, endMs int64) utils.ResultGuidanceOptions {
	return utils.ResultGuidanceOptions{
		Window:      utils.WindowFromRange(time.UnixMilli(startMs).UTC(), time.UnixMilli(endMs).UTC()),
		ServiceName: utils.FindStringFilter(args.LogjsonQuery, "ServiceName", "service_name", "service"),
		Environment: utils.FindStringFilter(
			args.LogjsonQuery,
			"resources['deployment.environment']",
			"resources['deployment_environment']",
			"env",
		),
		AdditionalChecks: []string{
			"inspect available fields with `get_log_attributes_for_pipeline`",
			"verify the index param if one was supplied",
		},
	}
}

func parseServiceLogTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func isLogResultTruncated(result map[string]interface{}, state logTruncationState) bool {
	if logsTruncated, _ := result["logs_truncated"].(bool); logsTruncated {
		return true
	}
	if state.totalMatching > state.returned {
		return true
	}
	return state.effectiveLimit > 0 && state.returned >= state.effectiveLimit
}

func logTotalMatching(result map[string]interface{}) int {
	switch value := result["total_matching_lines"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}
