package logs

import (
	"encoding/json"
	"fmt"
	"io"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"strconv"

	"github.com/acrmp/mcp"
)

// NewGetLogsHandler creates a handler for getting logs
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
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
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/logs")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if service, ok := params.Arguments["service"].(string); ok && service != "" {
			q.Set("service", service)
		}

		if severity, ok := params.Arguments["severity"].(string); ok && severity != "" {
			q.Set("severity", severity)
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

		// Read response body for debugging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to read response body: %w", err)
		}

		// Log the response for debugging
		if resp.StatusCode != 200 {
			return mcp.CallToolResult{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var result interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response (body: %s): %w", string(bodyBytes), err)
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
