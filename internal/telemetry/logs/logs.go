package logs

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

// GetLogsArgs represents the input arguments for the get_logs tool
type GetLogsArgs struct {
	LogjsonQuery    []interface{} `json:"logjson_query,omitempty"`
	StartTimeISO    string        `json:"start_time_iso,omitempty"`
	EndTimeISO      string        `json:"end_time_iso,omitempty"`
	LookbackMinutes int           `json:"lookback_minutes,omitempty"`
	Limit           int           `json:"limit,omitempty"`
}

// NewGetLogsHandler creates a handler for getting logs using logjson_query parameter
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogsArgs) (*mcp.CallToolResult, any, error) {
		// Check if logjson_query is provided
		if len(args.LogjsonQuery) == 0 {
			return nil, nil, fmt.Errorf("logjson_query parameter is required. Use the logjson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		// Handle logjson_query directly
		result, err := handleLogJSONQuery(client, cfg, args.LogjsonQuery, args)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}

func handleLogJSONQuery(client *http.Client, cfg models.Config, logjsonQuery interface{}, args GetLogsArgs) (*mcp.CallToolResult, error) {
	// Determine time range from parameters
	startTime, endTime, err := parseTimeRangeFromArgs(args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %v", err)
	}

	// Use util to execute the query
	resp, err := utils.MakeLogsJSONQueryAPI(client, cfg, logjsonQuery, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to call log JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("logs API request failed with status %d: %s", resp.StatusCode, string(body))
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

// parseTimeRangeFromArgs extracts start and end times from GetLogsArgs
func parseTimeRangeFromArgs(args GetLogsArgs) (int64, int64, error) {
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
