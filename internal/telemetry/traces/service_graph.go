package traces

import (
	"encoding/json"
	"errors"
	"fmt"
	"last9-mcp/internal/models"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/acrmp/mcp"
)

// NewGetServiceGraphHandler creates a handler for getting service dependencies
func NewGetServiceGraphHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		spanName, ok := params.Arguments["span_name"].(string)
		if !ok || spanName == "" {
			return mcp.CallToolResult{}, errors.New("span_name is required")
		}

		lookbackMinutes := 60
		if l, ok := params.Arguments["lookback_minutes"].(float64); ok {
			lookbackMinutes = int(l)
		}

		startTime := time.Now()
		if startTimeStr, ok := params.Arguments["start_time_iso"].(string); ok && startTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTime = t
		}

		// Build request URL
		u, err := url.Parse(cfg.BaseURL + "/telemetry/api/v1/service_graph")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("span_name", spanName)
		q.Set("lookback_minutes", strconv.Itoa(lookbackMinutes))
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
