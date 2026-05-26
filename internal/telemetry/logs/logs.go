package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultGetLogsLookbackMinutes = 5
const partialResultMetadataKey = "_last9_mcp"

var nonChunkedLogQueryStageTypes = []string{
	"aggregate",
	"window_aggregate",
}

// GetLogsArgs represents the input arguments for the get_logs tool
type GetLogsArgs struct {
	LogjsonQuery    []map[string]interface{} `json:"logjson_query,omitempty" jsonschema:"JSON pipeline query for logs (required)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 5, minimum: 1)"`
	Limit           int                      `json:"limit,omitempty" jsonschema:"Maximum number of rows to return (optional, default: 5000 for chunked raw queries)"`
	Index           string                   `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// NewGetLogsHandler creates a handler for getting logs using logjson_query parameter
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogsArgs) (*mcp.CallToolResult, any, error) {
		// Check if logjson_query is provided
		if len(args.LogjsonQuery) == 0 {
			return nil, nil, fmt.Errorf("logjson_query parameter is required. Use the logjson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		sanitizedQuery, err := sanitizeLogJSONQuery(args.LogjsonQuery)
		if err != nil {
			return nil, nil, err
		}
		args.LogjsonQuery = sanitizedQuery

		// Handle logjson_query directly
		result, err := handleLogJSONQuery(ctx, client, cfg, sanitizedQuery, args)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}

func handleLogJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, logjsonQuery interface{}, args GetLogsArgs) (*mcp.CallToolResult, error) {
	// Determine time range from parameters
	startTime, endTime, err := parseTimeRangeFromArgs(args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %v", err)
	}

	result, err := fetchLogJSONQuery(ctx, client, cfg, logjsonQuery, startTime, endTime, args)
	if err != nil {
		return nil, err
	}

	// Build deep link URL
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	dashboardIndex := ""
	hasExplicitIndex := strings.TrimSpace(args.Index) != ""
	if hasExplicitIndex {
		resolvedIndex, err := utils.ResolveLogIndexDashboardParam(ctx, client, cfg, args.Index)
		if err == nil {
			dashboardIndex = resolvedIndex
		}
	}
	dashboardURL := dlBuilder.BuildLogsLink(startTime, endTime, logjsonQuery, dashboardIndex)
	var meta mcp.Meta
	if !hasExplicitIndex || dashboardIndex != "" {
		meta = deeplink.ToMeta(dashboardURL)
	}

	// Return the result in MCP format with deep link
	return &mcp.CallToolResult{
		Meta: meta,
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: formatJSON(result),
			},
		},
	}, nil
}

func fetchLogJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, logjsonQuery interface{}, startTime, endTime int64, args GetLogsArgs) (map[string]interface{}, error) {
	chunkingDebug := chunkingDebugEnabled()

	if !shouldChunkGetLogsQuery(logjsonQuery) {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunking disabled start_ms=%d end_ms=%d limit=%d index=%q",
				startTime,
				endTime,
				args.Limit,
				args.Index,
			)
		}
		return executeLogJSONQuery(ctx, client, cfg, logjsonQuery, startTime, endTime, args.Limit, args.Index)
	}

	// ShouldOptimizeLineFilterQuery is intentionally left at the zero value
	// (false). The frontend toggles it via a feature flag to engage Rule 0
	// (1–2 parallel chunks for expensive body searches with line-filter
	// optimization). MCP has no equivalent flag today, so Rule 0 is dormant
	// and expensive body-search queries fall through to the time-range rules.
	// If we ever want to match the frontend's most aggressive throttle, wire
	// a config / env var into this field and the adaptive cascade picks it up
	// without further changes.
	adaptiveCfg := utils.GetAdaptiveLoadingConfig(utils.AdaptiveLoadingInput{
		StartMs:  startTime,
		EndMs:    endTime,
		Pipeline: args.LogjsonQuery,
	})
	chunks := utils.GetAdaptiveChunks(startTime, endTime, adaptiveCfg)
	if len(chunks) == 0 {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs produced no chunks start_ms=%d end_ms=%d limit=%d index=%q",
				startTime,
				endTime,
				args.Limit,
				args.Index,
			)
		}
		return emptyStreamsResponse(), nil
	}

	effectiveLimit := effectiveGetLogsChunkLimit(cfg, args.Limit)

	if chunkingDebug {
		log.Printf(
			"[chunking] get_logs chunking enabled chunks=%d max_parallel=%d chunk_size_ms=%d start_ms=%d end_ms=%d requested_limit=%d effective_limit=%d index=%q reason=%q",
			len(chunks),
			adaptiveCfg.MaxParallelChunks,
			adaptiveCfg.ChunkSizeMs,
			startTime,
			endTime,
			args.Limit,
			effectiveLimit,
			args.Index,
			adaptiveCfg.Reason,
		)
	}

	// Known over-fetch: each chunk asks the upstream for effectiveLimit rows,
	// not the remaining budget. With parallel execution we can't know
	// "remaining" until every chunk is back, so the pre-PR serial trick of
	// decrementing a remaining counter per chunk doesn't translate. The merge
	// loop below truncates to effectiveLimit post-merge. Trade-off: upstream
	// may scan extra rows in later chunks that an earlier chunk's data has
	// already covered, in exchange for honest coverage of the full time range
	// and consistent wall-clock regardless of where the data sits in the
	// window.
	results := utils.RunChunksParallel(ctx, chunks, adaptiveCfg.MaxParallelChunks,
		func(ctx context.Context, _ int, chunk utils.TimeChunk) (map[string]interface{}, error) {
			chunkCtx, cancel := context.WithTimeout(ctx, constants.PerChunkHTTPTimeout)
			defer cancel()
			return executeLogJSONQuery(chunkCtx, client, cfg, logjsonQuery, chunk.StartMs, chunk.EndMs, effectiveLimit, args.Index)
		})

	var (
		baseResponse map[string]interface{}
		mergedItems  []interface{}
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
			slog.Error("chunked log query failed",
				"tool", "get_logs",
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

		resultType, items, err := extractResultItems(r.Value)
		if err != nil {
			slog.Error("chunked log query parse failed",
				"tool", "get_logs",
				"chunk_index", chunkNum,
				"total_chunks", len(chunks),
				"err", err,
			)
			if partialErr == nil {
				partialErr = fmt.Errorf("chunk %d/%d failed to parse: %w", chunkNum, len(chunks), err)
			}
			continue
		}

		if resultType != "streams" {
			slog.Error("chunked log query unexpected result_type",
				"tool", "get_logs",
				"chunk_index", chunkNum,
				"total_chunks", len(chunks),
				"result_type", resultType,
			)
			if partialErr == nil {
				partialErr = fmt.Errorf("chunk %d/%d returned unexpected result_type=%q", chunkNum, len(chunks), resultType)
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
			items = truncateResultItemsByEntryLimit(items, remaining)
			kept = countLogEntriesInResultItems(items)
			remaining -= kept
			mergedItems = append(mergedItems, items...)
		}

		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunk result chunk=%d/%d kept_entries=%d remaining_limit=%d truncated_at_limit=%t",
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
		return emptyStreamsResponse(), nil
	}

	data, ok := baseResponse["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("logs API response missing data object")
	}

	data["result"] = mergedItems
	data["resultType"] = "streams"
	if partialErr != nil {
		annotatePartialGetLogsResponse(baseResponse, partialErr, len(chunks), countLogEntriesInResultItems(mergedItems))
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunking partial chunks=%d returned_entries=%d start_ms=%d end_ms=%d err=%v",
				len(chunks),
				countLogEntriesInResultItems(mergedItems),
				startTime,
				endTime,
				partialErr,
			)
		}
		return baseResponse, nil
	}

	if chunkingDebug {
		log.Printf(
			"[chunking] get_logs chunking complete chunks=%d returned_entries=%d start_ms=%d end_ms=%d",
			len(chunks),
			countLogEntriesInResultItems(mergedItems),
			startTime,
			endTime,
		)
	}

	return baseResponse, nil
}

func annotatePartialGetLogsResponse(response map[string]interface{}, err error, totalChunks, returnedEntries int) {
	response[partialResultMetadataKey] = map[string]interface{}{
		"partial_result":   true,
		"warning":          fmt.Sprintf("Returning partial results: %v", err),
		"total_chunks":     totalChunks,
		"returned_entries": returnedEntries,
	}
}

func effectiveGetLogsChunkLimit(cfg models.Config, requestedLimit int) int {
	maxEntries := cfg.MaxGetLogsEntries
	if maxEntries <= 0 {
		maxEntries = models.DefaultMaxGetLogsEntries
	}
	if requestedLimit <= 0 {
		return maxEntries
	}
	if requestedLimit > maxEntries {
		return maxEntries
	}
	return requestedLimit
}

func shouldChunkGetLogsQuery(logjsonQuery interface{}) bool {
	stages, ok := logjsonQuery.([]map[string]interface{})
	if !ok {
		return false
	}

	for _, stage := range stages {
		stageType, _ := stage["type"].(string)
		if slices.Contains(nonChunkedLogQueryStageTypes, stageType) {
			return false
		}
	}

	return true
}

func executeLogJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, logjsonQuery interface{}, startTime, endTime int64, limit int, index string) (map[string]interface{}, error) {
	resp, err := utils.MakeLogsJSONQueryAPI(ctx, client, cfg, logjsonQuery, startTime, endTime, limit, index)
	if err != nil {
		return nil, fmt.Errorf("failed to call log JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("logs API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return result, nil
}

func extractResultItems(result map[string]interface{}) (string, []interface{}, error) {
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return "", nil, fmt.Errorf("logs API response missing data object")
	}

	resultType, ok := data["resultType"].(string)
	if !ok {
		return "", nil, fmt.Errorf("logs API response missing resultType")
	}

	rawItems, exists := data["result"]
	if !exists || rawItems == nil {
		if resultType == "streams" {
			return resultType, []interface{}{}, nil
		}
		return resultType, nil, fmt.Errorf("logs API response missing result array")
	}

	items, ok := rawItems.([]interface{})
	if !ok {
		return resultType, nil, fmt.Errorf("logs API response missing result array")
	}

	return resultType, items, nil
}

func countLogEntriesInResultItems(items []interface{}) int {
	total := 0

	for _, item := range items {
		streamData, ok := item.(map[string]interface{})
		if !ok {
			total++
			continue
		}

		values, ok := streamData["values"].([]interface{})
		if !ok {
			total++
			continue
		}

		total += len(values)
	}

	return total
}

func truncateResultItemsByEntryLimit(items []interface{}, limit int) []interface{} {
	if limit <= 0 {
		return nil
	}

	truncated := make([]interface{}, 0, len(items))
	remaining := limit

	for _, item := range items {
		if remaining <= 0 {
			break
		}

		streamData, ok := item.(map[string]interface{})
		if !ok {
			truncated = append(truncated, item)
			remaining--
			continue
		}

		values, ok := streamData["values"].([]interface{})
		if !ok || len(values) <= remaining {
			truncated = append(truncated, item)
			if ok {
				remaining -= len(values)
			} else {
				remaining--
			}
			continue
		}

		cloned := mapsClone(streamData)
		cloned["values"] = slices.Clone(values[:remaining])
		truncated = append(truncated, cloned)
		break
	}

	return truncated
}

func mapsClone(source map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func emptyStreamsResponse() map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "streams",
			"result":     []interface{}{},
		},
	}
}

// parseTimeRangeFromArgs extracts start and end times from GetLogsArgs
func parseTimeRangeFromArgs(args GetLogsArgs) (int64, int64, error) {
	return parseTimeRangeFromArgsAt(args, time.Now().UTC())
}

func parseTimeRangeFromArgsAt(args GetLogsArgs, now time.Time) (int64, int64, error) {
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

	startTime, endTime, err := utils.GetTimeRangeAt(params, defaultGetLogsLookbackMinutes, now)
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
