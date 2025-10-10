package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetLogsArgs represents the input arguments for the get_logs tool
type GetLogsArgs struct {
	Service   string `json:"service,omitempty" jsonschema:"Service name to filter logs for (e.g. api)"`
	Severity  string `json:"severity,omitempty" jsonschema:"Severity level to filter logs (e.g. error warning info debug)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return (default: 20, range: 1-1000)"`
	StartTimeISO     string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO 8601 format (e.g. 2023-10-01T10:00:00Z). If not provided lookback_minutes is used"`
	EndTimeISO       string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO 8601 format (e.g. 2023-10-01T11:00:00Z). If not provided current time is used"`
	LookbackMinutes  int    `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time if start_time_iso not provided (default: 60, range: 1-10080)"`
}

// NewGetLogsHandler creates a handler for getting logs
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogsArgs) (*mcp.CallToolResult, any, error) {
		// Set default limit if not provided
		limit := args.Limit
		if limit == 0 {
			limit = 20
		}

		// Convert args to map for GetTimeRange utility
		params := make(map[string]interface{})
		if args.StartTimeISO != "" {
			params["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			params["end_time_iso"] = args.EndTimeISO
		}
		lookbackMinutes := args.LookbackMinutes
		if lookbackMinutes == 0 {
			lookbackMinutes = 60
		}

		// Get time range using the common utility
		startTime, endTime, err := utils.GetTimeRange(params, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build request URL with query parameters
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/logs")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if args.Service != "" {
			q.Set("service", args.Service)
		}

		if args.Severity != "" {
			q.Set("severity", args.Severity)
		}

		u.RawQuery = q.Encode()

		// Create request
		httpReq, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Check if the auth token already has the "Basic" prefix
		if !strings.HasPrefix(cfg.AuthToken, "Basic ") {
			cfg.AuthToken = "Basic " + cfg.AuthToken
		}

		httpReq.Header.Set("Authorization", cfg.AuthToken)

		// Execute request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		// Read response body for debugging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Log the response for debugging
		if resp.StatusCode != 200 {
			return nil, nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var result interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response (body: %s): %w", string(bodyBytes), err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(jsonData),
				},
			},
		}, nil, nil

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return mcp.CallToolResult{}, fmt.Errorf("logs API request failed with status %d: %s", resp.StatusCode, string(body))
	}

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
