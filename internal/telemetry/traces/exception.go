package traces

import (
	"encoding/json"
	"fmt"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"strconv"

	"github.com/acrmp/mcp"
)

// NewGetExceptionsHandler creates a handler for getting exceptions
func NewGetExceptionsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		limit := 20
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}

		// Get time range using the common utility
		startTime, endTime, err := utils.GetTimeRange(params.Arguments, 60) // Default 60 minutes lookback
		if err != nil {
			return mcp.CallToolResult{}, err
		}

		// Build request URL with query parameters
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/exceptions")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if spanName, ok := params.Arguments["span_name"].(string); ok && spanName != "" {
			q.Set("span_name", spanName)
		}

		u.RawQuery = q.Encode()

		// Create request
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", cfg.AuthToken)

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(jsonData),
					Type: "text",
				},
			},
		}, nil
	}
}
