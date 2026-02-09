package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetLogAttributesDescription describes the log attributes tool
const GetLogAttributesDescription = `
Fetches available log attributes (labels) for a specified time window.
This tool queries the Last9 logs API to retrieve all available attribute names
that can be used for filtering and querying logs within the specified time range.

The attributes returned are field names that exist in the logs during the specified
time window, which can then be used in log queries and filters.

Defaults to the last 15 minutes if no time window is provided.

Returns a list of attribute names like "service", "severity", "body", "level", etc.
`

// GetLogAttributesArgs represents the input arguments for the get_log_attributes tool
type GetLogAttributesArgs struct {
	LookbackMinutes int    `json:"lookback_minutes,omitempty"`
	StartTimeISO    string `json:"start_time_iso,omitempty"`
	EndTimeISO      string `json:"end_time_iso,omitempty"`
	Region          string `json:"region,omitempty"`
}

// FetchLogAttributeNames fetches log attribute names from the API and returns them as a string slice.
// This is the core logic shared by both the MCP handler and the attribute cache.
func FetchLogAttributeNames(ctx context.Context, client *http.Client, cfg models.Config) ([]string, error) {
	now := time.Now()
	startTime := now.Add(-15 * time.Minute).Unix()
	endTime := now.Unix()

	region := cfg.Region
	durationMinutes := (endTime - startTime) / 60

	queryParams := url.Values{}
	queryParams.Set("region", region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))

	var httpReq *http.Request
	var err error

	if durationMinutes > 20 {
		apiURL := fmt.Sprintf("%s/logs/api/v1/labels?%s", cfg.APIBaseURL, queryParams.Encode())
		httpReq, err = http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	} else {
		apiURL := fmt.Sprintf("%s/logs/api/v2/series/json?%s", cfg.APIBaseURL, queryParams.Encode())
		pipeline := map[string]interface{}{
			"pipeline": []interface{}{},
		}
		jsonBody, _ := json.Marshal(pipeline)
		httpReq, err = http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBody))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))
	httpReq.Header.Set("User-Agent", "Last9-MCP-Server/1.0")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errorBody)
		return nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
	}

	var result struct {
		Data   []string `json:"data"`
		Status string   `json:"status"`
	}

	if durationMinutes > 20 {
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response for labels api: %+v", err)
		}
	} else {
		var seriesResponse struct {
			Data   []map[string]interface{} `json:"data"`
			Status string                   `json:"status"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&seriesResponse); err != nil {
			return nil, fmt.Errorf("failed to decode response: %v", err)
		}

		result.Status = seriesResponse.Status
		if len(seriesResponse.Data) > 0 {
			for key := range seriesResponse.Data[0] {
				result.Data = append(result.Data, key)
			}
		}
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	return result.Data, nil
}

// NewGetLogAttributesHandler creates a handler for fetching log attributes
func NewGetLogAttributesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogAttributesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogAttributesArgs) (*mcp.CallToolResult, any, error) {
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

		// Get region parameter or use default from config
		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		// Calculate time range duration in minutes
		durationMinutes := (endTime - startTime) / 60

		// Common query parameters
		queryParams := url.Values{}
		queryParams.Set("region", region)
		queryParams.Set("start", fmt.Sprintf("%d", startTime))
		queryParams.Set("end", fmt.Sprintf("%d", endTime))

		var httpReq *http.Request
		var err error

		if durationMinutes > 20 {
			// Use GET /logs/api/v1/labels for time ranges > 20 minutes
			apiURL := fmt.Sprintf("%s/logs/api/v1/labels?%s", cfg.APIBaseURL, queryParams.Encode())
			httpReq, err = http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		} else {
			// Use POST /logs/api/v2/series/json for time ranges <= 20 minutes
			apiURL := fmt.Sprintf("%s/logs/api/v2/series/json?%s", cfg.APIBaseURL, queryParams.Encode())

			// Create JSON pipeline body
			pipeline := map[string]interface{}{
				"pipeline": []interface{}{},
			}
			jsonBody, _ := json.Marshal(pipeline)

			httpReq, err = http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBody))

		}

		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %v", err)
		}

		// Set headers
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))
		httpReq.Header.Set("User-Agent", "Last9-MCP-Server/1.0")
		httpReq.Header.Set("Content-Type", "application/json")

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

		// Parse the response based on which API was used
		var result struct {
			Data   []string `json:"data"`
			Status string   `json:"status"`
		}

		if durationMinutes > 20 {
			// Labels API returns array of strings directly
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return nil, nil, fmt.Errorf("failed to decode response for labels api: %+v", err)
			}
		} else {
			// Series API returns array of objects, extract keys from first object
			var seriesResponse struct {
				Data   []map[string]interface{} `json:"data"`
				Status string                   `json:"status"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&seriesResponse); err != nil {
				return nil, nil, fmt.Errorf("failed to decode response: %v", err)
			}

			result.Status = seriesResponse.Status
			if len(seriesResponse.Data) > 0 {
				// Extract attribute names (keys) from the first object
				for key := range seriesResponse.Data[0] {
					result.Data = append(result.Data, key)
				}
			}
		}

		// Check API status
		if result.Status != "success" {
			return nil, nil, fmt.Errorf("API returned non-success status: %s", result.Status)
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

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: summary,
				},
			},
		}, nil, nil
	}
}
