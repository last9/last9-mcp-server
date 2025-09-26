package logs

import (
	"encoding/json"
	"fmt"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"time"

	"github.com/acrmp/mcp"
)

// GetLogAttributesDescription describes the log attributes tool
const GetLogAttributesDescription = `
Fetches available log attributes (labels) for a specified time window.
This tool queries the Last9 logs API to retrieve all available attribute names
that can be used for filtering and querying logs within the specified time range.

The attributes returned are field names that exist in the logs during the specified
time window, which can then be used in log queries and filters.

Returns a list of attribute names like "service", "severity", "body", "level", etc.
`

// NewGetLogAttributesHandler creates a handler for fetching log attributes
func NewGetLogAttributesHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// Parse time range parameters
		now := time.Now()

		// Default to 15 minutes window
		startTime := now.Add(-15 * time.Minute).Unix()
		endTime := now.Unix()

		// Check for lookback_minutes parameter
		if lookback, ok := params.Arguments["lookback_minutes"].(float64); ok {
			startTime = now.Add(-time.Duration(lookback) * time.Minute).Unix()
		}

		// Check for explicit start_time_iso
		if startTimeISO, ok := params.Arguments["start_time_iso"].(string); ok && startTimeISO != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", startTimeISO); err == nil {
				startTime = parsed.Unix()
			}
		}

		// Check for explicit end_time_iso
		if endTimeISO, ok := params.Arguments["end_time_iso"].(string); ok && endTimeISO != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", endTimeISO); err == nil {
				endTime = parsed.Unix()
			}
		}

		// Get region parameter or use default from base URL
		region := utils.GetDefaultRegion(cfg.BaseURL)
		if r, ok := params.Arguments["region"].(string); ok && r != "" {
			region = r
		}

		// Build the API URL
		apiURL := fmt.Sprintf("%s/logs/api/v1/labels",
			cfg.APIBaseURL)

		// Add query parameters
		queryParams := url.Values{}
		queryParams.Set("region", region)
		queryParams.Set("start", fmt.Sprintf("%d", startTime))
		queryParams.Set("end", fmt.Sprintf("%d", endTime))

		fullURL := fmt.Sprintf("%s?%s", apiURL, queryParams.Encode())

		// Create the request
		req, err := http.NewRequest("GET", fullURL, nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)
		req.Header.Set("User-Agent", "Last9-MCP-Server/1.0")

		// Execute the request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			var errorBody map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errorBody)
			return mcp.CallToolResult{}, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
		}

		// Parse the response
		var result struct {
			Data   []string `json:"data"`
			Status string   `json:"status"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %v", err)
		}

		// Check API status
		if result.Status != "success" {
			return mcp.CallToolResult{}, fmt.Errorf("API returned non-success status: %s", result.Status)
		}

		// Format the response for display
		summary := fmt.Sprintf("Found %d attributes in the time window (%s to %s):\n\n",
			len(result.Data),
			time.Unix(startTime, 0).Format("2006-01-02 15:04:05"),
			time.Unix(endTime, 0).Format("2006-01-02 15:04:05"))

		// Group attributes by category
		logAttributes := []string{}
		resourceAttributes := []string{}

		for _, attr := range result.Data {
			if len(attr) > 9 && attr[:9] == "resource_" {
				resourceAttributes = append(resourceAttributes, attr)
			} else {
				logAttributes = append(logAttributes, attr)
			}
		}

		// Build formatted output
		if len(logAttributes) > 0 {
			summary += fmt.Sprintf("Log Attributes (%d):\n", len(logAttributes))
			for _, attr := range logAttributes {
				summary += fmt.Sprintf("  • %s\n", attr)
			}
		}

		if len(resourceAttributes) > 0 {
			summary += fmt.Sprintf("\nResource Attributes (%d):\n", len(resourceAttributes))
			for _, attr := range resourceAttributes {
				summary += fmt.Sprintf("  • %s\n", attr)
			}
		}

		// Return the result
		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: summary,
					Type: "text",
				},
			},
		}, nil
	}
}