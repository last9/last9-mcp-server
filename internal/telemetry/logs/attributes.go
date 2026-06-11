package logs

import (
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

// GetLogAttributesArgs represents the input arguments for the get_log_attributes tool
type GetLogAttributesArgs struct {
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
	Index           string `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// FetchLogAttributeNames fetches log attribute names from the API and returns them as a string slice.
// This is the core logic shared by both the MCP handler and the attribute cache.
func FetchLogAttributeNames(ctx context.Context, client *http.Client, cfg models.Config) ([]string, error) {
	now := time.Now()
	startTime := now.Add(-15 * time.Minute).Unix()
	endTime := now.Unix()

	queryParams := url.Values{}
	queryParams.Set("region", cfg.Region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))

	result, err := fetchLogLabels(ctx, client, cfg, queryParams)
	if err != nil {
		return nil, err
	}

	sort.Strings(result)
	return result, nil
}

// fetchLogLabels calls GET /logs/api/v1/labels and returns the attribute names.
//
// This endpoint is the source of truth for log attribute discovery and matches
// the dashboard's first-stage (empty-pipeline) discovery path. It returns the
// full label set — log attributes and resource_-prefixed resource attributes —
// for the requested window. The POST /v2/series/json endpoint with an empty
// pipeline returns only a subset and is intentionally not used here.
func fetchLogLabels(ctx context.Context, client *http.Client, cfg models.Config, queryParams url.Values) ([]string, error) {
	apiURL := fmt.Sprintf("%s/logs/api/v1/labels?%s", cfg.APIBaseURL, queryParams.Encode())
	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response for labels api: %+v", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	return result.Data, nil
}

// NewGetLogAttributesHandler creates a handler for fetching log attributes
func NewGetLogAttributesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogAttributesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogAttributesArgs) (*mcp.CallToolResult, any, error) {
		params := make(map[string]interface{})
		if args.LookbackMinutes > 0 {
			params["lookback_minutes"] = args.LookbackMinutes
		}
		if args.StartTimeISO != "" {
			params["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			params["end_time_iso"] = args.EndTimeISO
		}

		const defaultLogAttributesLookback = 15
		startTimeParsed, endTimeParsed, err := utils.GetTimeRange(params, defaultLogAttributesLookback)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse time range: %w", err)
		}
		endTime := endTimeParsed.Unix()
		// Cap the window magnitude. The labels endpoint returns the full label
		// set regardless of window size, so a longer range only adds server cost.
		startTime := startTimeParsed.Unix()
		maxWindowSeconds := int64(utils.MaxLogAttributeLookbackMinutes * 60)
		if endTime-startTime > maxWindowSeconds {
			startTime = endTime - maxWindowSeconds
		}

		// Get region parameter or use default from config
		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		normalizedIndex, err := utils.NormalizeLogIndex(args.Index)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid index: %w", err)
		}

		queryParams := url.Values{}
		queryParams.Set("region", region)
		queryParams.Set("start", fmt.Sprintf("%d", startTime))
		queryParams.Set("end", fmt.Sprintf("%d", endTime))
		if normalizedIndex != "" {
			queryParams.Set("index", normalizedIndex)
		}

		data, err := fetchLogLabels(ctx, client, cfg, queryParams)
		if err != nil {
			return nil, nil, err
		}

		// Format the response for display
		summary := fmt.Sprintf("Found %d attributes in the time window (%s to %s):\n\n",
			len(data),
			time.Unix(startTime, 0).Format("2006-01-02 15:04:05"),
			time.Unix(endTime, 0).Format("2006-01-02 15:04:05"))

		// Group attributes by category
		logAttributes := []string{}
		resourceAttributes := []string{}

		for _, attr := range data {
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
