package logs

import (
	"encoding/json"
	"fmt"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"time"

	"github.com/acrmp/mcp"
)

// NewGetLogsHandler creates a handler for getting logs using logjson_query parameter
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// Check if logjson_query is provided
		logjsonQuery, ok := params.Arguments["logjson_query"]
		if !ok || logjsonQuery == nil {
			return mcp.CallToolResult{}, fmt.Errorf("logjson_query parameter is required. Use the logjson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		// Handle logjson_query directly
		return handleLogJSONQuery(client, cfg, logjsonQuery, params.Arguments)
	}
}

// handleLogJSONQuery processes logjson_query parameter and calls the log JSON query API directly
func handleLogJSONQuery(client *http.Client, cfg models.Config, logjsonQuery interface{}, allParams map[string]interface{}) (mcp.CallToolResult, error) {
	// Determine time range from parameters
	startTime, endTime, err := parseTimeRange(allParams)
	if err != nil {
		return mcp.CallToolResult{}, fmt.Errorf("failed to parse time range: %v", err)
	}

	// Use util to execute the query
	resp, err := utils.MakeLogsJSONQueryAPI(client, cfg, logjsonQuery, startTime, endTime)
	if err != nil {
		return mcp.CallToolResult{}, fmt.Errorf("failed to call log JSON query API: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %v", err)
	}

	// Return the result in MCP format
	return mcp.CallToolResult{
		Content: []any{
			mcp.TextContent{
				Text: formatJSON(result),
				Type: "text",
			},
		},
	}, nil
}

// parseTimeRange extracts start and end times from parameters
func parseTimeRange(params map[string]interface{}) (int64, int64, error) {
	now := time.Now()

	// Default to last hour if no time parameters provided
	startTime := now.Add(-time.Hour).UnixMilli()
	endTime := now.UnixMilli()

	// Check for lookback_minutes
	if lookback, ok := params["lookback_minutes"].(float64); ok {
		startTime = now.Add(-time.Duration(lookback) * time.Minute).UnixMilli()
	}

	// Check for explicit start_time_iso
	if startTimeISO, ok := params["start_time_iso"].(string); ok && startTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", startTimeISO); err == nil {
			startTime = parsed.UnixMilli()
		}
	}

	// Check for explicit end_time_iso
	if endTimeISO, ok := params["end_time_iso"].(string); ok && endTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", endTimeISO); err == nil {
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
