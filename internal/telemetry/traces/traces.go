package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTracesDescription provides the description for the general traces tool
const GetTracesDescription = `Query distributed traces across all services using trace JSON pipeline queries.

This tool provides comprehensive access to trace data for debugging performance issues, understanding request flows,
and analyzing distributed system behavior. It accepts raw JSON pipeline queries for maximum flexibility.

The tool uses a pipeline-based query system similar to the logs API, allowing complex filtering and aggregation
operations on trace data.

Parameters:
- tracejson_query: (Required) JSON pipeline query for traces. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS)
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS)
- lookback_minutes: (Optional) Number of minutes to look back from current time (default: 60)
- limit: (Optional) Maximum number of traces to return (default: 20)

Returns comprehensive trace data including trace IDs, spans, durations, timestamps, and metadata.

Example tracejson_query structures:
- Simple filter: [{"type": "filter", "query": {"$eq": ["ServiceName", "api"]}}]
- Multiple conditions: [{"type": "filter", "query": {"$and": [{"$eq": ["ServiceName", "api"]}, {"$eq": ["StatusCode", "STATUS_CODE_ERROR"]}]}}]
- Trace ID lookup: [{"type": "filter", "query": {"$eq": ["TraceId", "abc123"]}}]`

// GetTracesArgs represents the input arguments for the get_traces tool
type GetTracesArgs struct {
	TracejsonQuery  []interface{} `json:"tracejson_query,omitempty"`
	StartTimeISO    string        `json:"start_time_iso,omitempty"`
	EndTimeISO      string        `json:"end_time_iso,omitempty"`
	LookbackMinutes int           `json:"lookback_minutes,omitempty"`
	Limit           int           `json:"limit,omitempty"`
}

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

	// Use util to execute the query
	resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, tracejsonQuery, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to call trace JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("traces API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Return the result in MCP format
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: formatJSON(result),
			},
		},
	}, nil
}

// parseTimeRangeFromArgs extracts start and end times from GetTracesArgs
func parseTimeRangeFromArgs(args GetTracesArgs) (int64, int64, error) {
	now := time.Now()

	// Default to last hour if no time parameters provided
	startTime := now.Add(-time.Hour).UnixMilli()
	endTime := now.UnixMilli()

	// Check for lookback_minutes
	if args.LookbackMinutes > 0 {
		startTime = now.Add(-time.Duration(args.LookbackMinutes) * time.Minute).UnixMilli()
	}

	// Check for explicit start_time_iso
	if args.StartTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", args.StartTimeISO); err == nil {
			startTime = parsed.UnixMilli()
		}
	}

	// Check for explicit end_time_iso
	if args.EndTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", args.EndTimeISO); err == nil {
			endTime = parsed.UnixMilli()
		}
	}

	return startTime, endTime, nil
}

// formatJSON formats JSON for display
func formatJSON(data interface{}) string {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(bytes)
}