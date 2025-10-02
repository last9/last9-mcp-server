package traces

import (
	"bytes"
	"encoding/json"
	"fmt"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/acrmp/mcp"
)

// GetTraceAttributesDescription describes the trace attributes tool
const GetTraceAttributesDescription = `
Fetches available trace attributes (series) for a specified time window.
This tool queries the Last9 traces API to retrieve all available attribute names
that can be used for filtering and querying traces within the specified time range.

The attributes returned are field names that exist in traces during the specified
time window, which can then be used in trace queries and filters.

Returns an alphabetically sorted list of all available trace attributes.
`

// TraceAttributesResponse represents the API response structure
type TraceAttributesResponse struct {
	Data   []map[string]string `json:"data"`
	Status string              `json:"status"`
}

// NewGetTraceAttributesHandler creates a handler for fetching trace attributes
func NewGetTraceAttributesHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
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
		apiURL := fmt.Sprintf("%s/cat/api/traces/v2/series/json",
			cfg.APIBaseURL)

		// Add query parameters
		queryParams := url.Values{}
		queryParams.Set("region", region)
		queryParams.Set("start", fmt.Sprintf("%d", startTime))
		queryParams.Set("end", fmt.Sprintf("%d", endTime))

		fullURL := fmt.Sprintf("%s?%s", apiURL, queryParams.Encode())

		// Create the request body
		requestBody := map[string]interface{}{
			"pipeline": []map[string]interface{}{
				{
					"query": map[string]interface{}{
						"$and": []interface{}{},
					},
					"type": "filter",
				},
			},
		}

		bodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Create the request
		req, err := http.NewRequest("POST", fullURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %v", err)
		}

		// Set headers
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
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
		var result TraceAttributesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %v", err)
		}

		// Check API status
		if result.Status != "success" {
			return mcp.CallToolResult{}, fmt.Errorf("API returned non-success status: %s", result.Status)
		}

		// Extract attributes as simple list
		if len(result.Data) == 0 {
			return mcp.CallToolResult{
				Content: []any{
					mcp.TextContent{
						Text: "No trace attributes found in the specified time window",
						Type: "text",
					},
				},
			}, nil
		}

		// Extract all attributes into a simple list
		attributes := []string{}
		for attrName := range result.Data[0] {
			// Skip empty attribute names
			if attrName == "" {
				continue
			}
			attributes = append(attributes, attrName)
		}

		// Sort attributes alphabetically
		sort.Strings(attributes)

		// Format the response for display
		summary := fmt.Sprintf("Found %d trace attributes in the time window (%s to %s):\n\n",
			len(attributes),
			time.Unix(startTime, 0).Format("2006-01-02 15:04:05"),
			time.Unix(endTime, 0).Format("2006-01-02 15:04:05"))

		// Build formatted output as simple list
		for _, attr := range attributes {
			summary += fmt.Sprintf("%s\n", attr)
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