package traces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxConcurrentGetTraces caps get_traces calls running at once across the
// process: each can hold several 5000-trace chunk bodies, so a burst of calls
// otherwise multiplies peak heap.
const maxConcurrentGetTraces = 3

var getTracesSlots = make(chan struct{}, maxConcurrentGetTraces)

// GetTracesArgs represents the input arguments for the traces query tool
type GetTracesArgs struct {
	TracejsonQuery  []map[string]interface{} `json:"tracejson_query,omitempty" jsonschema:"(Required) JSON pipeline query for traces"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, minimum: 1)"`
	Limit           int                      `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (optional, default: 5000)"`
}

// NewGetTracesHandler creates a handler for getting traces using tracejson_query parameter
func NewGetTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTracesArgs) (*mcp.CallToolResult, any, error) {
		// Check if tracejson_query is provided
		if len(args.TracejsonQuery) == 0 {
			return nil, nil, fmt.Errorf("tracejson_query parameter is required. tracejson_query is a JSON array of stages (filter/parse/aggregate/window_aggregate) — see last9://reference/tracejson")
		}

		// Validate the pipeline before forwarding to the API
		if err := SanitizeTraceJSONQuery(args.TracejsonQuery); err != nil {
			return nil, nil, err
		}
		if _, _, err := parseTimeRangeFromArgs(args); err != nil {
			return nil, nil, fmt.Errorf("failed to parse time range: %v", err)
		}

		select {
		case getTracesSlots <- struct{}{}:
			defer func() { <-getTracesSlots }()
		case <-ctx.Done():
			return nil, nil, fmt.Errorf(
				"get_traces is busy: %d concurrent get_traces calls were already running on this server and none finished before this call's deadline. Retry shortly; a narrower time window or lower limit makes calls finish sooner: %w",
				maxConcurrentGetTraces, ctx.Err(),
			)
		}

		// Handle tracejson_query directly
		result, err := handleTraceJSONQuery(ctx, client, cfg, args.TracejsonQuery, args)
		if err != nil {
			if isTraceUpstreamError(err) {
				return traceToolErrorResult(err), nil, nil
			}
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
	addTraceJSONGuidance(result, args, traceGuidanceContext{
		startMs:        startTime,
		endMs:          endTime,
		effectiveLimit: effectiveGetTracesLimit(cfg, args.Limit),
		maxLimit:       configuredMaxGetTracesEntries(cfg),
	})

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
// splitting via utils.GetAdaptiveChunks, and fetching the chunks newest-first
// until the effective limit is filled. An exact trace_id lookup short-
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

	// Aggregate pipelines (group-by, avg/median/quantile/stddev, etc.) must
	// never be chunked: concatenating per-chunk aggregate results produces
	// duplicate group-by keys and mathematically wrong aggregates. Run the
	// full window as a single request instead.
	if utils.PipelineHasAggregateStage(args.TracejsonQuery) {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_traces aggregate stage detected, using single request start_ms=%d end_ms=%d effective_limit=%d",
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

	var (
		baseResponse map[string]interface{}
		mergedItems  = make([]json.RawMessage, 0)
		remaining    = effectiveLimit
		// partialErr carries chunk context (e.g. "chunk 3/6 failed: ...") and
		// is used both for the all-chunks-failed hard error and for the
		// partial-result annotation when some chunks succeeded. Wrapping the
		// underlying error via %w preserves errors.Is/As behaviour.
		partialErr error
	)

	// Chunks are merged newest-first as they arrive, with at most
	// MaxParallelChunks fetched or held at once. Each chunk asks only for the
	// budget still unfilled when it starts, and none starts once it is filled.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	inFlight := make([]chan utils.ChunkResult[map[string]interface{}], 0, adaptiveCfg.MaxParallelChunks)
	next := 0
	startNextChunk := func() {
		idx, chunk, limit := next, chunks[next], remaining
		done := make(chan utils.ChunkResult[map[string]interface{}], 1)
		go func() {
			chunkCtx, cancelChunk := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
			defer cancelChunk()
			value, err := executeTraceJSONQuery(chunkCtx, client, cfg, tracejsonQuery, chunk.StartMs, chunk.EndMs, limit)
			done <- utils.ChunkResult[map[string]interface{}]{Index: idx, Chunk: chunk, Value: value, Err: err}
		}()
		inFlight = append(inFlight, done)
		next++
	}

	for remaining > 0 {
		for next < len(chunks) && len(inFlight) < adaptiveCfg.MaxParallelChunks {
			startNextChunk()
		}
		if len(inFlight) == 0 {
			break
		}
		r := <-inFlight[0]
		inFlight = inFlight[1:]
		chunkNum := r.Index + 1

		if r.Err != nil {
			slog.Error("chunked trace query failed",
				"tool", "get_traces",
				"chunk_index", chunkNum,
				"total_chunks", len(chunks),
				"start_ms", r.Chunk.StartMs,
				"end_ms", r.Chunk.EndMs,
				"err", traceLogCause(r.Err),
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

		kept := min(len(items), remaining)
		mergedItems = append(mergedItems, items[:kept]...)
		remaining -= kept

		if chunkingDebug {
			log.Printf(
				"[chunking] get_traces chunk result chunk=%d/%d kept_traces=%d remaining_limit=%d",
				chunkNum,
				len(chunks),
				kept,
				remaining,
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
		return nil, fmt.Errorf("%w (window start_ms=%d end_ms=%d)", partialErr, startMs, endMs)
	}
	if chunkingDebug {
		log.Printf(
			"[chunking] get_traces chunking complete chunks=%d returned_traces=%d start_ms=%d end_ms=%d",
			len(chunks), len(mergedItems), startMs, endMs,
		)
	}

	return baseResponse, nil
}

// executeTraceJSONQuery performs a single API call for a given time window.
func executeTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery interface{}, startMs, endMs int64, limit int) (map[string]interface{}, error) {
	resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, tracejsonQuery, startMs, endMs, limit)
	if err != nil {
		var transportErr *utils.HTTPTransportError
		if errors.As(err, &transportErr) {
			return nil, newTraceTransportError(transportErr)
		}
		return nil, fmt.Errorf("failed to prepare trace data request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, newTracePipelineHTTPError(resp)
	}

	result, err := decodeTraceQueryResponse(resp.Body)
	if err != nil {
		return nil, newTraceInvalidResponseError(err)
	}
	return result, nil
}

// decodeTraceQueryResponse leaves every field, and each trace in data.result,
// as raw JSON: generic maps retain several times the wire size per trace and
// round integers past 2^53, such as nanosecond timestamps.
func decodeTraceQueryResponse(body io.Reader) (map[string]interface{}, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&fields); err != nil {
		return nil, err
	}
	result := make(map[string]interface{}, len(fields))
	for key, value := range fields {
		result[key] = value
	}

	var dataFields map[string]json.RawMessage
	if json.Unmarshal(fields["data"], &dataFields) != nil || dataFields == nil {
		return result, nil
	}
	data := make(map[string]interface{}, len(dataFields))
	for key, value := range dataFields {
		data[key] = value
	}
	result["data"] = data

	var items []json.RawMessage
	if json.Unmarshal(dataFields["result"], &items) == nil {
		data["result"] = items
	}
	return result, nil
}

// extractTraceResultItems pulls the result array from a traces API response.
func extractTraceResultItems(result map[string]interface{}) ([]json.RawMessage, error) {
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("traces API response missing data object")
	}

	switch items := data["result"].(type) {
	case nil:
		return []json.RawMessage{}, nil
	case []json.RawMessage:
		return items, nil
	default:
		return nil, fmt.Errorf("traces API response result is not an array")
	}
}

// effectiveGetTracesLimit returns the capped limit to use across all chunks.
func effectiveGetTracesLimit(cfg models.Config, requestedLimit int) int {
	maxEntries := configuredMaxGetTracesEntries(cfg)
	if requestedLimit <= 0 {
		return maxEntries
	}
	if requestedLimit > maxEntries {
		return maxEntries
	}
	return requestedLimit
}

func configuredMaxGetTracesEntries(cfg models.Config) int {
	if cfg.MaxGetTracesEntries > 0 {
		return cfg.MaxGetTracesEntries
	}
	return models.DefaultMaxGetTracesEntries
}

// emptyTracesResponse returns a minimal valid empty traces response.
func emptyTracesResponse() map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"result": []json.RawMessage{},
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
