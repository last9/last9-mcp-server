package traces

import (
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

// GetTraceAttributesDescription describes the global trace attributes tool.
const GetTraceAttributesDescription = `
Fetches the GLOBAL catalog of available trace attributes for a time window and
returns each one enriched with the exact filter_field string to use in a
get_traces query.

This is the global tag catalog. A key it lists can still be empty for a specific
slice of spans — when you have already narrowed with a pipeline, prefer
get_trace_attributes_for_pipeline, which reports only attributes actually present
for that scope.

Call this before building a tracejson filter whenever you need to filter by a
resource attribute or span attribute — never guess the filter_field syntax.

Returns a JSON array. Each entry has:
  - name:          raw attribute name (e.g. "resource_department")
  - semantic_name: human-readable name with prefix stripped (e.g. "department")
  - type:          "toplevel" | "resource" | "event" | "span"
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

// TraceAttribute is an enriched attribute entry returned by the trace attribute
// tools. filter_field is the exact string to use in a tracejson filter condition.
type TraceAttribute struct {
	Name         string `json:"name"`
	SemanticName string `json:"semantic_name"`
	Type         string `json:"type"` // "resource", "span", "event", or "toplevel"
	FilterField  string `json:"filter_field"`
	Hint         string `json:"hint"`
}

// GetTraceAttributesArgs represents the input arguments for the get_trace_attributes tool.
type GetTraceAttributesArgs struct {
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}

// traceTagsAPIResponse is the /cat/api/search/tags response: attributes grouped
// by scope, with the scope prefix already stripped from each tag name.
type traceTagsAPIResponse struct {
	Scopes []struct {
		Name string   `json:"name"` // "span" | "resource" | "event"
		Tags []string `json:"tags"`
	} `json:"scopes"`
}

// reprefixTraceTag restores the raw prefixed name that enrichAttribute expects
// from a (scope, stripped-name) pair returned by the tag catalog.
func reprefixTraceTag(scope, tag string) string {
	switch scope {
	case "resource":
		return "resource_" + tag
	case "event":
		return "event_" + tag
	default: // "span" and anything else: bare name
		return tag
	}
}

// fetchTraceTagNames GETs the global trace tag catalog and returns raw, prefixed
// attribute names ready for enrichAttribute, sorted. The endpoint returns names
// with the scope prefix stripped, so resource/event tags are re-prefixed and span
// tags are left bare.
func fetchTraceTagNames(ctx context.Context, client *http.Client, cfg models.Config, startTime, endTime int64, region string) ([]string, error) {
	queryParams := url.Values{}
	queryParams.Set("region", region)
	queryParams.Set("start", fmt.Sprintf("%d", startTime))
	queryParams.Set("end", fmt.Sprintf("%d", endTime))
	apiURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointTraceTags, queryParams.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
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

	// This endpoint returns a bare {scopes:[...]} object with no status envelope,
	// so there is no status field to check (unlike the series endpoints).
	var result traceTagsAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	seen := map[string]struct{}{}
	for _, scope := range result.Scopes {
		for _, tag := range scope.Tags {
			if tag == "" {
				continue
			}
			seen[reprefixTraceTag(scope.Name, tag)] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// FetchTraceAttributeNames fetches global trace attribute names from the API and
// returns them as a sorted, prefixed string slice. Shared with the attribute cache.
func FetchTraceAttributeNames(ctx context.Context, client *http.Client, cfg models.Config) ([]string, error) {
	now := time.Now()
	return fetchTraceTagNames(ctx, client, cfg, now.Add(-15*time.Minute).Unix(), now.Unix(), cfg.Region)
}

// NewGetTraceAttributesHandler creates a handler for fetching the global trace attributes.
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

		region := cfg.Region
		if args.Region != "" {
			region = args.Region
		}

		names, err := fetchTraceTagNames(ctx, client, cfg, startTimeValue.Unix(), endTimeValue.Unix(), region)
		if err != nil {
			return nil, nil, err
		}

		if len(names) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "No trace attributes found in the specified time window"},
				},
			}, nil, nil
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
