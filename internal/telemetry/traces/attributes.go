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
	"strings"
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

Returns a comprehensive list of trace attributes including:
- Standard trace attributes (http.method, http.status_code, etc.)
- Application-specific attributes (app.* fields)
- Resource attributes (resource_* fields)
- Performance metrics (duration, latency, etc.)
- Service mesh attributes (downstream_cluster, upstream_cluster, etc.)
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

		// Extract and categorize attributes
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

		// Categorize attributes
		categories := map[string][]string{
			"HTTP":          []string{},
			"gRPC/RPC":      []string{},
			"Database":      []string{},
			"Messaging":     []string{},
			"Application":   []string{},
			"Network":       []string{},
			"Service Mesh":  []string{},
			"Resource":      []string{},
			"Performance":   []string{},
			"Error":         []string{},
			"Web Vitals":    []string{},
			"Other":         []string{},
		}

		// Process first data item (contains all available attributes)
		for attrName := range result.Data[0] {
			// Skip empty attribute names
			if attrName == "" {
				continue
			}

			// Categorize based on prefix
			switch {
			case strings.HasPrefix(attrName, "http."):
				categories["HTTP"] = append(categories["HTTP"], attrName)
			case strings.HasPrefix(attrName, "grpc.") || strings.HasPrefix(attrName, "rpc."):
				categories["gRPC/RPC"] = append(categories["gRPC/RPC"], attrName)
			case strings.HasPrefix(attrName, "db."):
				categories["Database"] = append(categories["Database"], attrName)
			case strings.HasPrefix(attrName, "messaging."):
				categories["Messaging"] = append(categories["Messaging"], attrName)
			case strings.HasPrefix(attrName, "app."):
				categories["Application"] = append(categories["Application"], attrName)
			case strings.HasPrefix(attrName, "net.") || strings.HasPrefix(attrName, "network."):
				categories["Network"] = append(categories["Network"], attrName)
			case strings.HasPrefix(attrName, "resource_"):
				categories["Resource"] = append(categories["Resource"], attrName)
			case strings.Contains(attrName, "cluster") || strings.Contains(attrName, "upstream") || strings.Contains(attrName, "downstream"):
				categories["Service Mesh"] = append(categories["Service Mesh"], attrName)
			case strings.HasPrefix(attrName, "web_vital"):
				categories["Web Vitals"] = append(categories["Web Vitals"], attrName)
			case strings.Contains(attrName, "error") || strings.Contains(attrName, "exception"):
				categories["Error"] = append(categories["Error"], attrName)
			case attrName == "duration" || attrName == "idle_ns" || attrName == "busy_ns" || strings.Contains(attrName, "latency") || strings.Contains(attrName, "time"):
				categories["Performance"] = append(categories["Performance"], attrName)
			default:
				categories["Other"] = append(categories["Other"], attrName)
			}
		}

		// Sort attributes within each category
		for _, attrs := range categories {
			sort.Strings(attrs)
		}

		// Format the response for display
		totalAttrs := 0
		for _, attrs := range categories {
			totalAttrs += len(attrs)
		}

		summary := fmt.Sprintf("Found %d trace attributes in the time window (%s to %s):\n",
			totalAttrs,
			time.Unix(startTime, 0).Format("2006-01-02 15:04:05"),
			time.Unix(endTime, 0).Format("2006-01-02 15:04:05"))

		// Define category order for display
		categoryOrder := []string{
			"HTTP", "gRPC/RPC", "Database", "Messaging", "Application",
			"Network", "Service Mesh", "Performance", "Error", "Web Vitals",
			"Resource", "Other",
		}

		// Build formatted output
		for _, category := range categoryOrder {
			attrs := categories[category]
			if len(attrs) > 0 {
				summary += fmt.Sprintf("\n%s Attributes (%d):\n", category, len(attrs))
				for _, attr := range attrs {
					summary += fmt.Sprintf("  • %s\n", attr)
				}
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