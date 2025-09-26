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
	}
}
