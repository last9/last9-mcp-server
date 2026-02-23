package logs

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

// GetLogsArgs represents the input arguments for the get_logs tool
type GetLogsArgs struct {
	LogjsonQuery    []interface{} `json:"logjson_query,omitempty" jsonschema:"JSON pipeline query for logs (required)"`
	StartTimeISO    string        `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string        `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int           `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, range: 1-20160)"`
	Limit           int           `json:"limit,omitempty" jsonschema:"Maximum number of rows to return (optional)"`
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

	// Use util to execute the query
	resp, err := utils.MakeLogsJSONQueryAPI(ctx, client, cfg, logjsonQuery, startTime, endTime)
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

	// Build deep link URL
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	dashboardURL := dlBuilder.BuildLogsLink(startTime, endTime, logjsonQuery)

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

// parseTimeRangeFromArgs extracts start and end times from GetLogsArgs
func parseTimeRangeFromArgs(args GetLogsArgs) (int64, int64, error) {
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
