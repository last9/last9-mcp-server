package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTraceAttributeValuesArgs is the input for get_trace_attribute_values.
type GetTraceAttributeValuesArgs struct {
	TagName string `json:"tag_name" jsonschema:"required,The attribute name from get_trace_attributes (e.g. resource_department or attributes['http.method'])"`
	Region  string `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}

// traceTagValuesAPIResponse is the raw API shape.
type traceTagValuesAPIResponse struct {
	Data   []string `json:"data"`
	Status string   `json:"status"`
}

// NewGetTraceAttributeValuesHandler creates a handler for fetching distinct values of a trace tag.
func NewGetTraceAttributeValuesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceAttributeValuesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTraceAttributeValuesArgs) (*mcp.CallToolResult, any, error) {
		if args.TagName == "" {
			return nil, nil, fmt.Errorf("tag_name is required")
		}

		rawTagName := normalizeTagName(args.TagName)
		if rawTagName == "" {
			return nil, nil, fmt.Errorf("tag_name cannot be blank")
		}
		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		now := time.Now()
		q := url.Values{}
		q.Set("region", region)
		q.Set("start", fmt.Sprintf("%d", now.Add(-15*time.Minute).Unix()))
		q.Set("end", fmt.Sprintf("%d", now.Unix()))
		apiURL := cfg.APIBaseURL + fmt.Sprintf(constants.EndpointTraceTagValues, url.PathEscape(rawTagName)) + "?" + q.Encode()

		// The label-values endpoint requires a POST with a pipeline body (same as series).
		pipeline := map[string]interface{}{
			"pipeline": []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{}},
			},
		}
		bodyBytes, err := json.Marshal(pipeline)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %v", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %v", err)
		}
		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
		httpReq.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errBody map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errBody)
			return nil, nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errBody)
		}

		var apiResp traceTagValuesAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %v", err)
		}
		if apiResp.Status != "success" {
			return nil, nil, fmt.Errorf("API returned non-success status: %s", apiResp.Status)
		}

		attr := enrichAttribute(rawTagName)

		type result struct {
			TagName     string   `json:"tag_name"`
			FilterField string   `json:"filter_field"`
			Values      []string `json:"values"`
			Hint        string   `json:"hint"`
		}

		values := apiResp.Data
		if values == nil {
			values = []string{}
		}

		out, err := json.Marshal(result{
			TagName:     rawTagName,
			FilterField: attr.FilterField,
			Values:      values,
			Hint:        fmt.Sprintf(`Use filter_field in a tracejson condition. Example: {"$eq": ["%s", "%s"]}`, attr.FilterField, firstOrPlaceholder(values)),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %v", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}

func firstOrPlaceholder(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return "value"
}
