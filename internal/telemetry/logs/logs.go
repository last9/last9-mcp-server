package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

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
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 5, range: 1-20160)"`
	Limit           int                      `json:"limit,omitempty" jsonschema:"Maximum number of rows to return (optional)"`
	Index           string                   `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// NewGetLogsHandler creates a handler for getting logs using logjson_query parameter
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogsArgs) (*mcp.CallToolResult, any, error) {
		// Check if logjson_query is provided
		if len(args.LogjsonQuery) == 0 {
			return nil, nil, fmt.Errorf("logjson_query parameter is required. Use the logjson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		// Handle logjson_query directly
		result, err := handleLogJSONQuery(ctx, client, cfg, args.LogjsonQuery, args)
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

	chunks := utils.GetTimeRangeChunksBackward(startTime, endTime)
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
			"[chunking] get_logs chunking enabled chunks=%d start_ms=%d end_ms=%d requested_limit=%d effective_limit=%d index=%q",
			len(chunks),
			startTime,
			endTime,
			args.Limit,
			effectiveLimit,
			args.Index,
		)
	}
	if args.Limit > 0 && args.Limit > effectiveLimit {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs requested limit capped requested_limit=%d configured_max=%d",
				args.Limit,
				effectiveLimit,
			)
		}
	}

	var (
		baseResponse map[string]interface{}
		mergedItems  []interface{}
		remaining    = effectiveLimit
		partialErr   error
	)

	for chunkIndex, chunk := range chunks {
		chunkLimit := remaining

		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunk request chunk=%d/%d start_ms=%d end_ms=%d chunk_limit=%d remaining_limit=%d",
				chunkIndex+1,
				len(chunks),
				chunk.StartMs,
				chunk.EndMs,
				chunkLimit,
				remaining,
			)
		}

		chunkResponse, err := executeLogJSONQuery(
			ctx,
			client,
			cfg,
			logjsonQuery,
			chunk.StartMs,
			chunk.EndMs,
			chunkLimit,
			args.Index,
		)
		if err != nil {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_logs chunk error chunk=%d/%d start_ms=%d end_ms=%d err=%v",
					chunkIndex+1,
					len(chunks),
					chunk.StartMs,
					chunk.EndMs,
					err,
				)
			}
			if baseResponse != nil {
				partialErr = fmt.Errorf("chunk %d/%d failed: %w", chunkIndex+1, len(chunks), err)
				break
			}
			return nil, err
		}

		resultType, items, err := extractResultItems(chunkResponse)
		if err != nil {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_logs chunk parse error chunk=%d/%d start_ms=%d end_ms=%d err=%v",
					chunkIndex+1,
					len(chunks),
					chunk.StartMs,
					chunk.EndMs,
					err,
				)
			}
			if baseResponse != nil {
				partialErr = fmt.Errorf("chunk %d/%d failed to parse: %w", chunkIndex+1, len(chunks), err)
				break
			}
			return nil, err
		}

		returnedEntries := countLogEntriesInResultItems(items)
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunk response chunk=%d/%d result_type=%s stream_items=%d returned_entries=%d",
				chunkIndex+1,
				len(chunks),
				resultType,
				len(items),
				returnedEntries,
			)
		}

		if resultType != "streams" {
			err := fmt.Errorf("chunked get_logs expected streams result, got %q", resultType)
			if chunkingDebug {
				log.Printf(
					"[chunking] get_logs chunking aborted due to unexpected result_type=%s after chunk=%d/%d",
					resultType,
					chunkIndex+1,
					len(chunks),
				)
			}
			if baseResponse != nil {
				partialErr = fmt.Errorf("chunk %d/%d returned unexpected result_type=%q", chunkIndex+1, len(chunks), resultType)
				break
			}
			return nil, err
		}

		if baseResponse == nil {
			baseResponse = chunkResponse
		}

		if len(items) == 0 {
			continue
		}

		entriesBeforeTrim := returnedEntries
		items = truncateResultItemsByEntryLimit(items, remaining)
		entriesAfterTrim := countLogEntriesInResultItems(items)
		if entriesAfterTrim != entriesBeforeTrim {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_logs chunk trim chunk=%d/%d kept_entries=%d dropped_entries=%d",
					chunkIndex+1,
					len(chunks),
					entriesAfterTrim,
					entriesBeforeTrim-entriesAfterTrim,
				)
			}
		}
		remaining -= entriesAfterTrim

		mergedItems = append(mergedItems, items...)
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunk merged chunk=%d/%d merged_entries=%d remaining_limit=%d",
				chunkIndex+1,
				len(chunks),
				countLogEntriesInResultItems(mergedItems),
				remaining,
			)
		}
		if remaining <= 0 {
			break
		}
	}

	if baseResponse == nil {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_logs chunking complete with empty response start_ms=%d end_ms=%d limit=%d",
				startTime,
				endTime,
				args.Limit,
			)
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
		"partial_result":  true,
		"warning":         fmt.Sprintf("Returning partial results: %v", err),
		"total_chunks":    totalChunks,
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
