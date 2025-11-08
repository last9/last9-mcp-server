package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// GetTraceAttributesArgs represents the input arguments for the get_trace_attributes tool
type GetTraceAttributesArgs struct {
	LookbackMinutes int    `json:"lookback_minutes,omitempty"`
	StartTimeISO    string `json:"start_time_iso,omitempty"`
	EndTimeISO      string `json:"end_time_iso,omitempty"`
	Region          string `json:"region,omitempty"`
}

// NewGetTraceAttributesHandler creates a handler for fetching trace attributes
func NewGetTraceAttributesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceAttributesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTraceAttributesArgs) (*mcp.CallToolResult, any, error) {
		// Parse time range parameters
		now := time.Now()

		// Default to 15 minutes window
		startTime := now.Add(-15 * time.Minute).Unix()
		endTime := now.Unix()

		// Check for lookback_minutes parameter
		if args.LookbackMinutes > 0 {
			startTime = now.Add(-time.Duration(args.LookbackMinutes) * time.Minute).Unix()
		}

		// Check for explicit start_time_iso
		if args.StartTimeISO != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", args.StartTimeISO); err == nil {
				startTime = parsed.Unix()
			}
		}

		// Check for explicit end_time_iso
		if args.EndTimeISO != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", args.EndTimeISO); err == nil {
				endTime = parsed.Unix()
			}
		}

		// Get region parameter or use default from base URL
		region := utils.GetDefaultRegion(cfg.BaseURL)
		if args.Region != "" {
			region = args.Region
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
			return nil, nil, fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Create the request
		httpReq, err := http.NewRequest("POST", fullURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %v", err)
		}

		// Set headers
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))
		httpReq.Header.Set("User-Agent", "Last9-MCP-Server/1.0")

		// Execute the request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			var errorBody map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errorBody)
			return nil, nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
		}

		// Parse the response
		var result TraceAttributesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %v", err)
		}

		// Check API status
		if result.Status != "success" {
			return nil, nil, fmt.Errorf("API returned non-success status: %s", result.Status)
		}

		// Extract attributes as simple list
		if len(result.Data) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: "No trace attributes found in the specified time window",
					},
				},
			}, nil, nil
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: summary,
				},
			},
		}, nil, nil
	}
}

