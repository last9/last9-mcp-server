package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetLogAttributesForPipelineArgs represents the input arguments for the
// get_log_attributes_for_pipeline tool.
type GetLogAttributesForPipelineArgs struct {
	Pipeline        []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Pipeline of prior filter stages to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}] (required)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
	Index           string                   `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// logSeriesResponse represents the /logs/api/v2/series/json response. Each entry
// in Data is a label-set; we only need its keys (the field names present).
type logSeriesResponse struct {
	Data   []map[string]json.RawMessage `json:"data"`
	Status string                       `json:"status"`
}

// LogAttribute is an enriched attribute entry returned by
// get_log_attributes_for_pipeline. filter_field is the exact string to use in a
// logjson filter condition.
type LogAttribute struct {
	Name        string `json:"name"`
	FilterField string `json:"filter_field"`
	Hint        string `json:"hint"`
}

// logFieldFilterField maps a raw log field name to the exact filter_field string
// used in a logjson condition:
//   - service    -> ServiceName
//   - severity   -> SeverityText
//   - body       -> Body
//   - resource_x -> resources['x']
//   - default    -> attributes['<name>']
//
// The series endpoint returns log attributes bare (only resource attributes are
// prefixed, with resource_), so a field name keeps its full name: e.g. a real
// attribute named log_level maps to attributes['log_level'], not attributes['level'].
func logFieldFilterField(name string) string {
	switch name {
	case "service":
		return "ServiceName"
	case "severity":
		return "SeverityText"
	case "body":
		return "Body"
	}
	if rest, ok := strings.CutPrefix(name, "resource_"); ok {
		return fmt.Sprintf("resources['%s']", rest)
	}
	return fmt.Sprintf("attributes['%s']", name)
}

// fetchLogSeriesFieldNames POSTs the given pipeline to /logs/api/v2/series/json
// and returns the union of field names present across all returned label-sets.
func fetchLogSeriesFieldNames(ctx context.Context, client *http.Client, cfg models.Config, pipeline []map[string]interface{}, queryParams url.Values) ([]string, error) {
	apiURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointLogsSeries, queryParams.Encode())

	requestBody := map[string]interface{}{
		"pipeline": pipeline,
	}
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

	var result logSeriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response for series api: %+v", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned non-success status: %s", result.Status)
	}

	// Union the keys across every label-set so a field present in any matching
	// log is reported.
	seen := map[string]struct{}{}
	for _, entry := range result.Data {
		for fieldName := range entry {
			if fieldName == "" {
				continue
			}
			seen[fieldName] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for fieldName := range seen {
		names = append(names, fieldName)
	}
	sort.Strings(names)
	return names, nil
}

// NewGetLogAttributesForPipelineHandler creates a handler that returns the log
// attributes present for a given pipeline, each enriched with its filter_field.
func NewGetLogAttributesForPipelineHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogAttributesForPipelineArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Pipeline) == 0 {
			return nil, nil, fmt.Errorf("pipeline parameter is required. Provide at least one filter stage to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}]")
		}

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
		startTime := startTimeParsed.Unix()
		// Cap the window magnitude to keep server cost bounded, matching get_log_attributes.
		maxWindowSeconds := int64(utils.MaxLogAttributeLookbackMinutes * 60)
		if endTime-startTime > maxWindowSeconds {
			startTime = endTime - maxWindowSeconds
		}

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

		names, err := fetchLogSeriesFieldNames(ctx, client, cfg, args.Pipeline, queryParams)
		if err != nil {
			return nil, nil, err
		}

		out := make([]LogAttribute, 0, len(names))
		for _, name := range names {
			filterField := logFieldFilterField(name)
			out = append(out, LogAttribute{
				Name:        name,
				FilterField: filterField,
				Hint:        fmt.Sprintf("{\"$eq\":[\"%s\",\"<value>\"]}", filterField),
			})
		}

		payload, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal attributes: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(payload),
				},
			},
		}, nil, nil
	}
}
