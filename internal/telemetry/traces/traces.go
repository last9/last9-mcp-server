package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTracesDescription provides the description for the traces query tool
const GetTracesDescription = `Query distributed traces across all services using trace JSON pipeline queries.

This tool provides comprehensive access to trace data for debugging performance issues, understanding request flows,
and analyzing distributed system behavior. It accepts raw JSON pipeline queries for maximum flexibility.

The tool uses a pipeline-based query system similar to the logs API, allowing complex filtering and aggregation
operations on trace data.

Parameters:
- tracejson_query: (Required) JSON pipeline query for traces. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language
- start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)
- end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)
- lookback_minutes: (Optional) Number of minutes to look back from current time (default: 60)
- limit: (Optional) Maximum number of traces to return (default: 20)

Time format rules:
- Prefer lookback_minutes for relative windows (for example, last 5 or 60 minutes).
- Use start_time_iso/end_time_iso for absolute windows.
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
- If both lookback_minutes and absolute times are provided, absolute times take precedence.

Returns comprehensive trace data including trace IDs, spans, durations, timestamps, and metadata.

Example tracejson_query structures:
- Simple filter: [{"type": "filter", "query": {"$eq": ["ServiceName", "api"]}}]
- Multiple conditions: [{"type": "filter", "query": {"$and": [{"$eq": ["ServiceName", "api"]}, {"$eq": ["StatusCode", "STATUS_CODE_ERROR"]}]}}]
- Trace ID lookup: [{"type": "filter", "query": {"$eq": ["TraceId", "abc123"]}}]`

// GetTracesArgs represents the input arguments for the traces query tool
type GetTracesArgs struct {
	TracejsonQuery  []map[string]interface{} `json:"tracejson_query,omitempty" jsonschema:"JSON pipeline query for traces (required)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-20160)"`
	Limit           int                      `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (optional, default: 20)"`
}

const partialResultMetadataKey = "_last9_mcp"

// NewGetTracesHandler creates a handler for getting traces using tracejson_query parameter
func NewGetTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTracesArgs) (*mcp.CallToolResult, any, error) {
		// Check if tracejson_query is provided
		if len(args.TracejsonQuery) == 0 {
			return nil, nil, fmt.Errorf("tracejson_query parameter is required. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language")
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

// fetchTraceJSONQuery executes the trace query, chunking the time range into 5-minute windows
// and merging results newest-first — mirroring the logs chunking approach.
func fetchTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery interface{}, startMs, endMs int64, args GetTracesArgs) (map[string]interface{}, error) {
	debug := chunkingDebugEnabled()

	chunks := utils.GetTimeRangeChunksBackward(startMs, endMs)
	if len(chunks) == 0 {
		if debug {
			log.Printf("[chunking] get_traces produced no chunks start_ms=%d end_ms=%d limit=%d", startMs, endMs, args.Limit)
		}
		return emptyTracesResponse(), nil
	}

	effectiveLimit := effectiveGetTracesLimit(cfg, args.Limit)

	if debug {
		log.Printf(
			"[chunking] get_traces chunking enabled chunks=%d start_ms=%d end_ms=%d requested_limit=%d effective_limit=%d",
			len(chunks), startMs, endMs, args.Limit, effectiveLimit,
		)
	}

	var (
		baseResponse map[string]interface{}
		mergedItems  []interface{}
		remaining    = effectiveLimit
		partialErr   error
	)

	for chunkIndex, chunk := range chunks {
		if debug {
			log.Printf(
				"[chunking] get_traces chunk request chunk=%d/%d start_ms=%d end_ms=%d chunk_limit=%d remaining=%d",
				chunkIndex+1, len(chunks), chunk.StartMs, chunk.EndMs, remaining, remaining,
			)
		}

		chunkResp, err := executeTraceJSONQuery(ctx, client, cfg, tracejsonQuery, chunk.StartMs, chunk.EndMs, remaining)
		if err != nil {
			if debug {
				log.Printf("[chunking] get_traces chunk error chunk=%d/%d err=%v", chunkIndex+1, len(chunks), err)
			}
			if baseResponse != nil {
				partialErr = fmt.Errorf("chunk %d/%d failed: %w", chunkIndex+1, len(chunks), err)
				break
			}
			return nil, err
		}

		items, err := extractTraceResultItems(chunkResp)
		if err != nil {
			if debug {
				log.Printf("[chunking] get_traces chunk parse error chunk=%d/%d err=%v", chunkIndex+1, len(chunks), err)
			}
			if baseResponse != nil {
				partialErr = fmt.Errorf("chunk %d/%d failed to parse: %w", chunkIndex+1, len(chunks), err)
				break
			}
			return nil, err
		}

		if debug {
			log.Printf(
				"[chunking] get_traces chunk response chunk=%d/%d items=%d",
				chunkIndex+1, len(chunks), len(items),
			)
		}

		if baseResponse == nil {
			baseResponse = chunkResp
		}

		if len(items) == 0 {
			continue
		}

		// Trim to remaining budget
		if len(items) > remaining {
			items = items[:remaining]
		}
		remaining -= len(items)
		mergedItems = append(mergedItems, items...)

		if debug {
			log.Printf(
				"[chunking] get_traces chunk merged chunk=%d/%d total_items=%d remaining=%d",
				chunkIndex+1, len(chunks), len(mergedItems), remaining,
			)
		}

		if remaining <= 0 {
			break
		}
	}

	if baseResponse == nil {
		return emptyTracesResponse(), nil
	}

	data, ok := baseResponse["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("traces API response missing data object")
	}
	data["result"] = mergedItems

	if partialErr != nil {
		baseResponse[partialResultMetadataKey] = map[string]interface{}{
			"partial_result":  true,
			"warning":         fmt.Sprintf("Returning partial results: %v", partialErr),
			"total_chunks":    len(chunks),
			"returned_traces": len(mergedItems),
		}
		if debug {
			log.Printf(
				"[chunking] get_traces chunking partial chunks=%d returned_traces=%d err=%v",
				len(chunks), len(mergedItems), partialErr,
			)
		}
	} else if debug {
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
