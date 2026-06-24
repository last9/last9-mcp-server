package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTraceAttributesForPipelineArgs represents the input arguments for the
// get_trace_attributes_for_pipeline tool.
type GetTraceAttributesForPipelineArgs struct {
	Pipeline        []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Pipeline of prior filter stages to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}] (required)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}

// traceAttributesResponse represents the traces series API response structure.
type traceAttributesResponse struct {
	Data   []map[string]string `json:"data"`
	Status string              `json:"status"`
}

// fetchTraceSeriesAttributeNames POSTs the given pipeline to the traces series
// endpoint and returns the union of attribute names present across all returned
// label-sets, sorted.
func fetchTraceSeriesAttributeNames(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, startTime, endTime int64, region string) ([]string, error) {
	queryParams := url.Values{}
	queryParams.Set("region", region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))
	apiURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointTracesSeries, queryParams.Encode())

	requestBody := map[string]interface{}{"pipeline": pipeline}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(bodyBytes))
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

	var result traceAttributesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	seen := map[string]struct{}{}
	for _, entry := range result.Data {
		for name := range entry {
			if name == "" {
				continue
			}
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// NewGetTraceAttributesForPipelineHandler creates a handler that returns the trace
// attributes present for a given pipeline, each enriched with its filter_field.
func NewGetTraceAttributesForPipelineHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTraceAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Pipeline) == 0 {
			return nil, nil, fmt.Errorf("pipeline parameter is required. Provide at least one filter stage to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}]")
		}

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

		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		names, err := fetchTraceSeriesAttributeNames(ctx, client, cfg, args.Pipeline, startTimeValue.Unix(), endTimeValue.Unix(), region)
		if err != nil {
			return nil, nil, err
		}

		enriched := make([]TraceAttribute, 0, len(names))
		for _, name := range names {
			enriched = append(enriched, enrichAttribute(name))
		}

		out, err := json.Marshal(enriched)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal trace attributes: %v", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
