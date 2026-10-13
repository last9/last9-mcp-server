package traces

import (
	"time"

	"last9-mcp/internal/utils"
)

type traceGuidanceContext struct {
	startMs        int64
	endMs          int64
	effectiveLimit int
	maxLimit       int
}

type traceGuidanceWindow struct {
	start time.Time
	end   time.Time
}

func addTraceJSONGuidance(result map[string]interface{}, args GetTracesArgs, guidance traceGuidanceContext) {
	items, err := extractTraceResultItems(result)
	if err != nil {
		return
	}
	if len(items) == 0 {
		result["l9_guidance"] = utils.EmptyResultGuidanceMap("traces", traceJSONGuidanceOptions(args, guidance.startMs, guidance.endMs))
		return
	}
	if guidance.effectiveLimit > 0 && len(items) >= guidance.effectiveLimit {
		result["l9_guidance"] = map[string]any{
			"message": utils.TruncatedResultAdvice(utils.TruncatedResultOptions{
				Subject:  "traces",
				Returned: len(items),
				MaxLimit: guidance.maxLimit,
			}),
		}
	}
}

func traceJSONGuidanceOptions(args GetTracesArgs, startMs, endMs int64) utils.ResultGuidanceOptions {
	return utils.ResultGuidanceOptions{
		Window:      utils.WindowFromRange(time.UnixMilli(startMs).UTC(), time.UnixMilli(endMs).UTC()),
		ServiceName: utils.FindStringFilter(args.TracejsonQuery, "ServiceName", "service_name", "service"),
		Environment: utils.FindStringFilter(
			args.TracejsonQuery,
			"resources['deployment.environment']",
			"resources['deployment_environment']",
			"env",
		),
		AdditionalChecks: []string{
			"inspect available fields with `get_trace_attributes_for_pipeline`",
			"verify span kind or attribute filters",
		},
	}
}

func serviceTraceEmptyAdvice(args GetServiceTracesArgs, queryParams *GetTracesQueryParams, window traceGuidanceWindow) string {
	return utils.EmptyResultAdvice("traces", utils.ResultGuidanceOptions{
		Window:      utils.WindowFromRange(window.start, window.end),
		ServiceName: queryParams.ServiceName,
		Environment: args.Env,
		AdditionalChecks: []string{
			"inspect available fields with `get_trace_attributes_for_pipeline`",
			"verify the service is emitting spans in this environment",
		},
	})
}

func exceptionEmptyAdvice(args GetExceptionsArgs, start, end time.Time) string {
	return utils.EmptyResultAdvice("exceptions", utils.ResultGuidanceOptions{
		Window:      utils.WindowFromRange(start, end),
		ServiceName: args.ServiceName,
		Environment: args.Env,
		AdditionalChecks: []string{
			"check whether the service reports exceptions as span events or error status",
			"try removing span_name if it was supplied",
		},
	})
}
