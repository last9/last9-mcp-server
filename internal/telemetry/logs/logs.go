package logs

import (
	"encoding/json"
	"fmt"
	"last9-mcp/internal/models"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/acrmp/mcp"
)

// NewGetLogsHandler creates a handler for getting logs
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		limit := 20
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}

		// Calculate timestamps
		endTime := time.Now()
		startTime := endTime.Add(-1 * time.Hour)

		if startTimeStr, ok := params.Arguments["start_time_iso"].(string); ok && startTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTime = t
		}

		if endTimeStr, ok := params.Arguments["end_time_iso"].(string); ok && endTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", endTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid end_time_iso format: %w", err)
			}
			endTime = t
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

		req.Header.Set("Authorization", "Basic "+cfg.AuthToken)

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
