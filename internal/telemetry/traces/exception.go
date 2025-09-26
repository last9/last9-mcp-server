package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetExceptionsArgs defines the input structure for getting exceptions
type GetExceptionsArgs struct {
	Limit           float64 `json:"limit,omitempty" jsonschema:"Maximum number of exceptions to return (default: 20, range: 1-1000)"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-10080)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	SpanName        string  `json:"span_name,omitempty" jsonschema:"Filter exceptions by span name (e.g. user_service)"`
}

// NewGetExceptionsHandler creates a handler for getting exceptions
func NewGetExceptionsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
		limit := 20
		if args.Limit != 0 {
			limit = int(args.Limit)
		}

		lookbackMinutes := 60
		if args.LookbackMinutes != 0 {
			lookbackMinutes = int(args.LookbackMinutes)
		}

		// Prepare arguments map for GetTimeRange function
		arguments := make(map[string]interface{})
		if args.StartTimeISO != "" {
			arguments["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			arguments["end_time_iso"] = args.EndTimeISO
		}

		// Get time range using the common utility
		startTime, endTime, err := utils.GetTimeRange(arguments, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build request URL with query parameters
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/exceptions")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if args.SpanName != "" {
			q.Set("span_name", args.SpanName)
		}

		u.RawQuery = q.Encode()

		// Create request
		httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
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

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
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
