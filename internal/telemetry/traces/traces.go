package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTracesArgs represents the input arguments for the traces query tool
type GetTracesArgs struct {
	TracejsonQuery  []map[string]interface{} `json:"tracejson_query,omitempty" jsonschema:"JSON pipeline query for traces (required)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, minimum: 1)"`
	Limit           int                      `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (optional, default: 5000)"`
}

const partialResultMetadataKey = "_last9_mcp"

// NewGetTracesHandler creates a handler for getting traces using tracejson_query parameter
func NewGetTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTracesArgs) (*mcp.CallToolResult, any, error) {
		// Check if tracejson_query is provided
		if len(args.TracejsonQuery) == 0 {
			return nil, nil, fmt.Errorf("tracejson_query parameter is required. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		// Validate the pipeline before forwarding to the API
		if err := sanitizeTraceJSONQuery(args.TracejsonQuery); err != nil {
			return nil, nil, err
		}

		// Handle tracejson_query directly
		result, err := handleTraceJSONQuery(ctx, client, cfg, args.TracejsonQuery, args)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}

func handleTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery interface{}, args GetTracesArgs) (*mcp.CallToolResult, error) {
	// Determine time range from parameters
	startTime, endTime, err := parseTimeRangeFromArgs(args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %v", err)
	}

	result, err := fetchTraceJSONQuery(ctx, client, cfg, tracejsonQuery, startTime, endTime, args)
	if err != nil {
		return nil, err
	}

	// Build deep link URL
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	dashboardURL := dlBuilder.BuildTracesLink(startTime, endTime, tracejsonQuery, "", "")

	// Return the result in MCP format with deep link
	return &mcp.CallToolResult{
		Meta: deeplink.ToMeta(dashboardURL),
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: formatJSON(result),
			},
		},
	}, nil
}

// fetchTraceJSONQuery executes the trace query by resolving an
// AdaptiveLoadingConfig (parallelism + chunk size) for the requested window,
// splitting via utils.GetAdaptiveChunks, and fanning the chunks out through
// utils.RunChunksParallel. Results are walked in newest-first index order
// and truncated to the effective limit. An exact trace_id lookup short-
// circuits chunking entirely (see extractExactTraceIDLookup).
func fetchTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery interface{}, startMs, endMs int64, args GetTracesArgs) (map[string]interface{}, error) {
	chunkingDebug := chunkingDebugEnabled()
	effectiveLimit := effectiveGetTracesLimit(cfg, args.Limit)

	if traceID, ok := extractExactTraceIDLookup(args.TracejsonQuery); ok {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_traces exact trace_id lookup detected trace_id=%s using single request start_ms=%d end_ms=%d effective_limit=%d",
				traceID,
				startMs,
				endMs,
				effectiveLimit,
			)
		}
		return executeTraceJSONQuery(ctx, client, cfg, tracejsonQuery, startMs, endMs, effectiveLimit)
	}

	// Trace pipelines never reference the Body field, so HasExpensiveBodyParsing
	// is always false here — adaptive config falls through to the time-range
	// rules, exactly as the frontend would treat a non-body-search query.
	//
	// ShouldOptimizeLineFilterQuery is intentionally left at the zero value
	// (false). The frontend toggles it via a feature flag to engage Rule 0
	// (1–2 parallel chunks for expensive body searches with line-filter
	// optimization). MCP has no equivalent flag today, so Rule 0 is dormant.
	// For traces this is doubly moot — there's no Body field — but the
	// comment is kept for parity with the logs call sites.
	adaptiveCfg := utils.GetAdaptiveLoadingConfig(utils.AdaptiveLoadingInput{
		StartMs:  startMs,
		EndMs:    endMs,
		Pipeline: args.TracejsonQuery,
	})
	chunks := utils.GetAdaptiveChunks(startMs, endMs, adaptiveCfg)
	if len(chunks) == 0 {
		if chunkingDebug {
			log.Printf("[chunking] get_traces produced no chunks start_ms=%d end_ms=%d limit=%d", startMs, endMs, args.Limit)
		}
		return emptyTracesResponse(), nil
	}

	if chunkingDebug {
		log.Printf(
			"[chunking] get_traces chunking enabled chunks=%d max_parallel=%d chunk_size_ms=%d start_ms=%d end_ms=%d requested_limit=%d effective_limit=%d reason=%q",
			len(chunks), adaptiveCfg.MaxParallelChunks, adaptiveCfg.ChunkSizeMs, startMs, endMs, args.Limit, effectiveLimit, adaptiveCfg.Reason,
		)
		// Preserved from the pre-refactor format so existing log greps
		// matching `requested limit capped` keep working.
		if args.Limit > 0 && args.Limit > effectiveLimit {
			log.Printf(
				"[chunking] get_traces requested limit capped requested_limit=%d configured_max=%d",
				args.Limit,
				effectiveLimit,
			)
		}
	}

	// Known over-fetch: each chunk asks the upstream for effectiveLimit
	// traces, not a decrementing remaining budget. With parallel execution
	// we can't know "remaining" until every chunk is back, so the pre-PR
	// serial trick of passing remaining doesn't translate. The merge loop
	// below truncates to effectiveLimit post-merge. Trade-off: backend may
	// scan extra rows in later chunks already covered by earlier ones, in
	// exchange for honest coverage of the full time range and consistent
	// wall-clock regardless of where the data sits in the window.
	results := utils.RunChunksParallel(ctx, chunks, adaptiveCfg.MaxParallelChunks,
		func(ctx context.Context, _ int, chunk utils.TimeChunk) (map[string]interface{}, error) {
			chunkCtx, cancel := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
			defer cancel()
			return executeTraceJSONQuery(chunkCtx, client, cfg, tracejsonQuery, chunk.StartMs, chunk.EndMs, effectiveLimit)
		})

	var (
		baseResponse map[string]interface{}
		mergedItems  = make([]interface{}, 0)
		remaining    = effectiveLimit
		// partialErr carries chunk context (e.g. "chunk 3/6 failed: ...") and
		// is used both for the all-chunks-failed hard error and for the
		// partial-result annotation when some chunks succeeded. Wrapping the
		// underlying error via %w preserves errors.Is/As behaviour.
		partialErr error
	)

	for _, r := range results {
		chunkNum := r.Index + 1

		if r.Err != nil {
			slog.Error("chunked trace query failed",
				"tool", "get_traces",
				"chunk_index", chunkNum,
				"total_chunks", len(chunks),
				"start_ms", r.Chunk.StartMs,
				"end_ms", r.Chunk.EndMs,
				"err", r.Err,
			)
			if partialErr == nil {
				partialErr = fmt.Errorf("chunk %d/%d failed: %w", chunkNum, len(chunks), r.Err)
			}
			continue
		}

		items, err := extractTraceResultItems(r.Value)
		if err != nil {
			slog.Error("chunked trace query parse failed",
				"tool", "get_traces",
				"chunk_index", chunkNum,
				"total_chunks", len(chunks),
				"err", err,
			)
			if partialErr == nil {
				partialErr = fmt.Errorf("chunk %d/%d failed to parse: %w", chunkNum, len(chunks), err)
			}
			continue
		}

		if baseResponse == nil {
			baseResponse = r.Value
		}

		// Track whether this chunk's results were fully truncated away so the
		// debug log records every successful chunk, not just the ones that
		// fit inside the limit.
		truncatedAtLimit := remaining <= 0
		var kept int
		if !truncatedAtLimit && len(items) > 0 {
			if len(items) > remaining {
				items = items[:remaining]
			}
			kept = len(items)
			remaining -= kept
			mergedItems = append(mergedItems, items...)
		}

		if chunkingDebug {
			log.Printf(
				"[chunking] get_traces chunk result chunk=%d/%d kept_traces=%d remaining_limit=%d truncated_at_limit=%t",
				chunkNum,
				len(chunks),
				kept,
				remaining,
				truncatedAtLimit,
			)
		}
	}

	if baseResponse == nil {
		if partialErr != nil {
			return nil, partialErr
		}
		return emptyTracesResponse(), nil
	}

	data, ok := baseResponse["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("traces API response missing data object")
	}
	data["result"] = mergedItems

	if partialErr != nil {
		annotatePartialGetTracesResponse(baseResponse, partialErr, len(chunks), len(mergedItems))
		if chunkingDebug {
			log.Printf(
				"[chunking] get_traces chunking partial chunks=%d returned_traces=%d start_ms=%d end_ms=%d err=%v",
				len(chunks),
				len(mergedItems),
				startMs,
				endMs,
				partialErr,
			)
		}
	} else if chunkingDebug {
		log.Printf(
			"[chunking] get_traces chunking complete chunks=%d returned_traces=%d start_ms=%d end_ms=%d",
			len(chunks), len(mergedItems), startMs, endMs,
		)
	}

	return baseResponse, nil
}

func annotatePartialGetTracesResponse(response map[string]interface{}, err error, totalChunks, returnedTraces int) {
	response[partialResultMetadataKey] = map[string]interface{}{
		"partial_result":  true,
		"warning":         fmt.Sprintf("Returning partial results: %v", err),
		"total_chunks":    totalChunks,
		"returned_traces": returnedTraces,
	}
}

// executeTraceJSONQuery performs a single API call for a given time window.
func executeTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery interface{}, startMs, endMs int64, limit int) (map[string]interface{}, error) {
	resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, tracejsonQuery, startMs, endMs, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to call trace JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 100 {
			bodyStr = bodyStr[:100] + "... (truncated)"
		}
		return nil, fmt.Errorf("traces API request failed with status %d (endpoint: %s/cat/api/traces/v2/query_range/json). Response: %s",
			resp.StatusCode, cfg.APIBaseURL, bodyStr)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}
	return result, nil
}

// extractTraceResultItems pulls the result array from a traces API response.
func extractTraceResultItems(result map[string]interface{}) ([]interface{}, error) {
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("traces API response missing data object")
	}

	rawItems, exists := data["result"]
	if !exists || rawItems == nil {
		return []interface{}{}, nil
	}

	items, ok := rawItems.([]interface{})
	if !ok {
		return nil, fmt.Errorf("traces API response result is not an array")
	}
	return items, nil
}

// effectiveGetTracesLimit returns the capped limit to use across all chunks.
func effectiveGetTracesLimit(cfg models.Config, requestedLimit int) int {
	maxEntries := cfg.MaxGetTracesEntries
	if maxEntries <= 0 {
		maxEntries = models.DefaultMaxGetTracesEntries
	}
	if requestedLimit <= 0 {
		return maxEntries
	}
	if requestedLimit > maxEntries {
		return maxEntries
	}
	return requestedLimit
}

// emptyTracesResponse returns a minimal valid empty traces response.
func emptyTracesResponse() map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"result": []interface{}{},
		},
	}
}

// parseTimeRangeFromArgs extracts start and end times from GetTracesArgs
func parseTimeRangeFromArgs(args GetTracesArgs) (int64, int64, error) {
	params := make(map[string]interface{})
	if args.LookbackMinutes > 0 {
		params["lookback_minutes"] = args.LookbackMinutes
	}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}

	startTime, endTime, err := utils.GetTimeRange(params, utils.DefaultLookbackMinutes)
	if err != nil {
		return 0, 0, err
	}
	return startTime.UnixMilli(), endTime.UnixMilli(), nil
}

// formatJSON formats JSON for display
func formatJSON(data interface{}) string {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(bytes)
}

// parseTimeRangeFromArgsAt is the testable version of parseTimeRangeFromArgs
func parseTimeRangeFromArgsAt(args GetTracesArgs, now time.Time) (int64, int64, error) {
	params := make(map[string]interface{})
	if args.LookbackMinutes > 0 {
		params["lookback_minutes"] = args.LookbackMinutes
	}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}

	startTime, endTime, err := utils.GetTimeRangeAt(params, utils.DefaultLookbackMinutes, now)
	if err != nil {
		return 0, 0, err
	}
	return startTime.UnixMilli(), endTime.UnixMilli(), nil
}

func extractExactTraceIDLookup(pipeline []map[string]interface{}) (string, bool) {
	if len(pipeline) != 1 {
		return "", false
	}

	filterStep := pipeline[0]
	stepType, _ := filterStep["type"].(string)
	if stepType != "filter" {
		return "", false
	}

	query, ok := filterStep["query"].(map[string]interface{})
	if !ok {
		return "", false
	}

	return findExactTraceIDInCondition(query)
}

// findExactTraceIDInCondition returns a trace ID fast-path when an exact
// TraceId equality appears anywhere in the condition tree. The helper walks
// both "$and" and "$or" groups via findExactTraceIDInConditionGroup; while
// "$or" can semantically mean "this TraceId OR other conditions" and therefore
// still match multiple traces, we intentionally treat any TraceId equality as a
// signal to avoid chunking and favor performance.
func findExactTraceIDInCondition(condition map[string]interface{}) (string, bool) {
	if traceID, ok := exactTraceIDEquality(condition["$eq"]); ok {
		return traceID, true
	}

	if traceID, ok := findExactTraceIDInConditionGroup(condition["$and"]); ok {
		return traceID, true
	}

	if traceID, ok := findExactTraceIDInConditionGroup(condition["$or"]); ok {
		return traceID, true
	}

	return "", false
}

func findExactTraceIDInConditionGroup(rawConditions interface{}) (string, bool) {
	conditions, ok := rawConditions.([]interface{})
	if !ok {
		return "", false
	}

	for _, rawCondition := range conditions {
		nestedCondition, ok := rawCondition.(map[string]interface{})
		if !ok {
			continue
		}
		if traceID, ok := findExactTraceIDInCondition(nestedCondition); ok {
			return traceID, true
		}
	}
	return "", false
}

func exactTraceIDEquality(rawEq interface{}) (string, bool) {
	eqArgs, ok := rawEq.([]interface{})
	if !ok || len(eqArgs) != 2 {
		return "", false
	}

	field, fieldOK := eqArgs[0].(string)
	traceID, traceOK := eqArgs[1].(string)
	if !fieldOK || !traceOK {
		return "", false
	}
	if field != "TraceId" {
		return "", false
	}

	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return "", false
	}

	return traceID, true
}
