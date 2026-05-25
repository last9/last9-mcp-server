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

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTraceAttributesDescription describes the trace attributes tool
const GetTraceAttributesDescription = `
Fetches available trace attributes for a specified time window and returns each
one enriched with the exact filter_field string to use in a get_traces query.

Call this before building a tracejson filter whenever you need to filter by a
resource attribute or span attribute — never guess the filter_field syntax.

Returns a JSON array sorted by name. Each entry has:
  - name:          raw attribute name as returned by the API (e.g. "resource_department")
  - semantic_name: human-readable name with prefix stripped (e.g. "department")
  - type:          "toplevel" | "resource" | "span"
  - filter_field:  exact string to use in a tracejson $eq/$contains/etc. condition
                   (e.g. "resources['department']", "attributes['http.method']", "ServiceName")
  - hint:          ready-made example condition using filter_field

Use filter_field directly — do not transform it further.

Defaults to the last 15 minutes if no time window is provided.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
`

// TraceAttributesResponse represents the API response structure
type TraceAttributesResponse struct {
	Data   []map[string]string `json:"data"`
	Status string              `json:"status"`
}

// TraceAttribute is an enriched attribute entry returned by get_trace_attributes.
// filter_field is the exact string to use in a tracejson filter condition.
type TraceAttribute struct {
	Name         string `json:"name"`
	SemanticName string `json:"semantic_name"`
	Type         string `json:"type"` // "resource", "span", "event", or "toplevel"
	FilterField  string `json:"filter_field"`
	Hint         string `json:"hint"`
}

// GetTraceAttributesArgs represents the input arguments for the get_trace_attributes tool
type GetTraceAttributesArgs struct {
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}

// FetchTraceAttributeNames fetches trace attribute names from the API and returns them as a sorted string slice.
// This is the core logic shared by both the MCP handler and the attribute cache.
func FetchTraceAttributeNames(ctx context.Context, client *http.Client, cfg models.Config) ([]string, error) {
	now := time.Now()
	startTime := now.Add(-15 * time.Minute).Unix()
	endTime := now.Unix()

	region := cfg.Region

	apiURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointTracesSeries)

	queryParams := url.Values{}
	queryParams.Set("region", region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))

	fullURL := fmt.Sprintf("%s?%s", apiURL, queryParams.Encode())

	requestBody := map[string]interface{}{
		"pipeline": []map[string]interface{}{
			{
				"query": map[string]interface{}{},
				"type":  "filter",
			},
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	httpReq.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)

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

	var result TraceAttributesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	if len(result.Data) == 0 {
		return []string{}, nil
	}

	attributes := []string{}
	for attrName := range result.Data[0] {
		if attrName == "" {
			continue
		}
		attributes = append(attributes, attrName)
	}

	sort.Strings(attributes)

	return attributes, nil
}

// NewGetTraceAttributesHandler creates a handler for fetching trace attributes
func NewGetTraceAttributesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceAttributesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTraceAttributesArgs) (*mcp.CallToolResult, any, error) {
		timeParams := map[string]interface{}{}
		if args.LookbackMinutes > 0 {
			timeParams["lookback_minutes"] = args.LookbackMinutes
		}
		if args.StartTimeISO != "" {
			timeParams["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			timeParams["end_time_iso"] = args.EndTimeISO
		}

		startTimeValue, endTimeValue, err := utils.GetTimeRange(timeParams, 15)
		if err != nil {
			return nil, nil, err
		}
		startTime := startTimeValue.Unix()
		endTime := endTimeValue.Unix()

		// Get region parameter or use default from config
		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		// Build the API URL
		apiURL := fmt.Sprintf("%s%s",
			cfg.APIBaseURL, constants.EndpointTracesSeries)

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
					"query": map[string]interface{}{},
					"type":  "filter",
				},
			},
		}

		bodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Create the request
		httpReq, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %v", err)
		}

		// Set headers
		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
		httpReq.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)

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

		if len(result.Data) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: "No trace attributes found in the specified time window",
					},
				},
			}, nil, nil
		}

		// Build sorted list of raw names then enrich each one.
		rawNames := make([]string, 0, len(result.Data[0]))
		for attrName := range result.Data[0] {
			if attrName == "" {
				continue
			}
			rawNames = append(rawNames, attrName)
		}
		sort.Strings(rawNames)

		enriched := make([]TraceAttribute, 0, len(rawNames))
		for _, name := range rawNames {
			enriched = append(enriched, enrichAttribute(name))
		}

		out, err := json.Marshal(enriched)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal trace attributes: %v", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(out),
				},
			},
		}, nil, nil
	}
}
