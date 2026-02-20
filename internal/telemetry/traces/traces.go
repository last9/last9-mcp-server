package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
- limit: (Optional) Maximum number of traces to return (default: 20, range: 1-100)

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
	TracejsonQuery  []interface{} `json:"tracejson_query,omitempty" jsonschema:"JSON pipeline query for traces (required)"`
	StartTimeISO    string        `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string        `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int           `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-1440)"`
	Limit           int           `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (default: 20, range: 1-100)"`
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
	resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, tracejsonQuery, startTime, endTime, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to call trace JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Limit body size in error message to avoid huge HTML responses
		bodyStr := string(body)
		if len(bodyStr) > 100 {
			bodyStr = bodyStr[:100] + "... (truncated)"
		}
		// Include status code in error message for better test detection
		return nil, fmt.Errorf("traces API request failed with status %d (endpoint: %s/cat/api/traces/v2/query_range/json). Response: %s", resp.StatusCode, cfg.APIBaseURL, bodyStr)
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
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
