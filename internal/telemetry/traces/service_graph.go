package traces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetServiceGraphArgs defines the input structure for getting service graph
type GetServiceGraphArgs struct {
	SpanName          string  `json:"span_name" jsonschema:"Name of the span to get service dependencies for (required)"`
	LookbackMinutes   float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-10080)"`
	StartTimeISO      string  `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
}

// NewGetServiceGraphHandler creates a handler for getting service dependencies
func NewGetServiceGraphHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetServiceGraphArgs) (*mcp.CallToolResult, any, error) {
		if args.SpanName == "" {
			return nil, nil, errors.New("span_name is required and cannot be empty")
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

		// Get time range using the common utility
		startTime, _, err := utils.GetTimeRange(arguments, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build request URL
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/service_graph")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("span_name", args.SpanName)
		q.Set("lookback_minutes", strconv.Itoa(lookbackMinutes))
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

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("service graph API request failed with status %d: %s", resp.StatusCode, string(body))
		}

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
