# Trace Attribute Discovery Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring MCP trace attribute discovery to parity with the post-#160 logs flow — add a pipeline-scoped trace attributes tool, move the global tool to the real catalog endpoint, and make trace attribute-value lookups pipeline-aware.

**Architecture:** Three changes in `internal/telemetry/traces/`, all reusing the existing `enrichAttribute` (`field_mapping.go`) and `TraceAttribute` output shape. A new pipeline tool POSTs the user's pipeline to the traces series endpoint; the global tool switches from empty-pipeline series to `GET /cat/api/search/tags` (re-prefixing scope tags before enrichment); the values tool gains an optional pipeline pass-through.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk/mcp`, standard `net/http` + `net/http/httptest`, `go test`.

**Spec:** `docs/superpowers/specs/2026-06-12-trace-attribute-discovery-parity-design.md`
**Branch:** `release-eng-1250-mcp-trace-attribute-discovery-parity-with-logs-add` (already checked out)

---

## File Structure

| File | Responsibility | Action |
|------|----------------|--------|
| `internal/constants/api.go` | API endpoint + header constants | Modify (add `EndpointTraceTags`) |
| `internal/telemetry/traces/series_attributes.go` | Pipeline-scoped trace attributes tool + series fetch helper | Create |
| `internal/telemetry/traces/series_attributes_test.go` | Tests for the pipeline tool; shared `decodeTraceAttributes` helper | Create |
| `internal/telemetry/traces/attributes.go` | Global trace attributes tool (now `/search/tags`) + tag fetch helper | Modify |
| `internal/telemetry/traces/attributes_test.go` | Tests for the global tool against a scopes payload | Create |
| `internal/telemetry/traces/attribute_values.go` | Distinct values for a tag; gains optional pipeline | Modify |
| `internal/telemetry/traces/attribute_values_test.go` | Add pipeline-forwarding test | Modify |
| `tools.go` | MCP tool registration | Modify (register new tool) |

The package already has `field_mapping.go` (`enrichAttribute`, `normalizeTagName`) and `TraceAttribute`/`TraceAttributesResponse` types in `attributes.go` — reused unchanged.

---

## Task 1: Add the `/search/tags` endpoint constant

**Files:**
- Modify: `internal/constants/api.go:7-11`

- [ ] **Step 1: Add the constant**

In `internal/constants/api.go`, add `EndpointTraceTags` to the Traces block so it reads:

```go
	// Traces API endpoints
	EndpointTracesQueryRange  = "/cat/api/traces/v2/query_range/json"
	EndpointTracesSeries      = "/cat/api/traces/v2/series/json"
	EndpointTraceDetails      = "/cat/api/traces/%s"
	EndpointTraceTagValues    = "/cat/api/traces/v2/label/json/%s/values"
	// EndpointTraceTags is the global trace tag catalog (scopes with prefixes
	// stripped). Mirrors the dashboard's getTraceTags call.
	EndpointTraceTags         = "/cat/api/search/tags"
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/constants/...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/constants/api.go
git commit -m "feat(traces): add /search/tags endpoint constant (ENG-1250)"
```

---

## Task 2: New tool `get_trace_attributes_for_pipeline`

**Files:**
- Create: `internal/telemetry/traces/series_attributes.go`
- Create: `internal/telemetry/traces/series_attributes_test.go`

This task is self-contained — it does not modify `attributes.go`. It reuses `TraceAttributesResponse`, `TraceAttribute`, and `enrichAttribute` from the existing package.

- [ ] **Step 1: Write the failing tests**

Create `internal/telemetry/traces/series_attributes_test.go`:

```go
package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// decodeTraceAttributes unmarshals a tool result's TextContent into the enriched
// attribute slice. Shared by the traces attribute tests.
func decodeTraceAttributes(t *testing.T, res *mcp.CallToolResult) []TraceAttribute {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var attrs []TraceAttribute
	if err := json.Unmarshal([]byte(text.Text), &attrs); err != nil {
		t.Fatalf("failed to unmarshal result: %v\nraw: %s", err, text.Text)
	}
	return attrs
}

func TestGetTraceAttributesForPipeline_RequiresPipeline(t *testing.T) {
	handler := NewGetTraceAttributesForPipelineHandler(http.DefaultClient, newTestCfg("http://unused.example"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesForPipelineArgs{})
	if err == nil {
		t.Fatal("expected error when pipeline is empty, got nil")
	}
	if !strings.Contains(err.Error(), "pipeline parameter is required") {
		t.Errorf("expected pipeline-required error, got: %v", err)
	}
}

func TestGetTraceAttributesForPipeline_UsesSeriesEndpoint(t *testing.T) {
	var capturedPath, capturedMethod string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[{` +
			`"resource_service.name":"checkout",` +
			`"resource_department":"eng",` +
			`"event_exception.type":"timeout",` +
			`"http.method":"GET",` +
			`"StatusCode":"ERROR"` +
			`}]}`))
	}))
	defer server.Close()

	handler := NewGetTraceAttributesForPipelineHandler(server.Client(), newTestCfg(server.URL))
	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
	}
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesForPipelineArgs{
		Pipeline:        pipeline,
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if capturedMethod != "POST" || !strings.Contains(capturedPath, "/cat/api/traces/v2/series/json") {
		t.Errorf("expected POST /cat/api/traces/v2/series/json, got: %s %s", capturedMethod, capturedPath)
	}
	if capturedBody == nil || capturedBody["pipeline"] == nil {
		t.Fatalf("expected pipeline forwarded in request body, got: %v", capturedBody)
	}

	got := map[string]string{}
	for _, a := range decodeTraceAttributes(t, res) {
		got[a.Name] = a.FilterField
	}
	want := map[string]string{
		"resource_service.name": "ServiceName",
		"resource_department":   "resources['department']",
		"event_exception.type":  "events['exception.type']",
		"http.method":           "attributes['http.method']",
		"StatusCode":            "StatusCode",
	}
	if len(got) != len(want) {
		t.Errorf("expected %d fields, got %d: %v", len(want), len(got), got)
	}
	for name, ff := range want {
		if got[name] != ff {
			t.Errorf("field %q: expected filter_field %q, got %q", name, ff, got[name])
		}
	}
}

func TestGetTraceAttributesForPipeline_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer server.Close()

	handler := NewGetTraceAttributesForPipelineHandler(server.Client(), newTestCfg(server.URL))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if attrs := decodeTraceAttributes(t, res); len(attrs) != 0 {
		t.Errorf("expected empty attribute list, got: %v", attrs)
	}
}
```

> Note: `newTestCfg` is already defined in `attribute_values_test.go` (same package) — do not redefine it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/telemetry/traces/ -run TestGetTraceAttributesForPipeline -v`
Expected: compile error / FAIL — `NewGetTraceAttributesForPipelineHandler` and `GetTraceAttributesForPipelineArgs` are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/telemetry/traces/series_attributes.go`:

```go
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

// GetTraceAttributesForPipelineDescription describes the pipeline-scoped trace attributes tool.
const GetTraceAttributesForPipelineDescription = `
Returns the trace attributes that actually exist AFTER applying the given pipeline,
each enriched with the exact filter_field string to use in a get_traces query.

Unlike get_trace_attributes (which returns the global tag catalog), this tool is
scoped to your in-progress pipeline, so it only reports attributes that are really
present for that slice of spans.

Use it before filtering on an attribute:
1. First narrow with a filter stage, e.g. {"type":"filter","query":{"$eq":["ServiceName","<service>"]}}.
2. Call this tool with that pipeline.
3. Build your get_traces filter using only the filter_field values it returns.

Do not assume any attribute's key name or syntax. Confirm the real field with this
tool instead of guessing.

Returns a JSON array. Each entry has:
  - name:          raw attribute name (e.g. "resource_department")
  - semantic_name: human-readable name with prefix stripped (e.g. "department")
  - type:          "toplevel" | "resource" | "event" | "span"
  - filter_field:  exact string to use in a tracejson $eq/$contains/etc. condition
  - hint:          ready-made example condition using filter_field

Use filter_field directly — do not transform it further.

Defaults to the last 15 minutes if no time window is provided.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
`

// GetTraceAttributesForPipelineArgs represents the input arguments for the
// get_trace_attributes_for_pipeline tool.
type GetTraceAttributesForPipelineArgs struct {
	Pipeline        []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Pipeline of prior filter stages to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}] (required)"`
	LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
	StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	Region          string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
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

	var result TraceAttributesResponse
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/telemetry/traces/ -run TestGetTraceAttributesForPipeline -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/traces/series_attributes.go internal/telemetry/traces/series_attributes_test.go
git commit -m "feat(traces): add get_trace_attributes_for_pipeline tool (ENG-1250)"
```

---

## Task 3: Register the new tool in `tools.go`

**Files:**
- Modify: `tools.go` (after the `get_trace_attributes` registration, ~line 196-198)

- [ ] **Step 1: Add the registration**

In `tools.go`, immediately after the existing `get_trace_attributes` registration block:

```go
	// Register trace attributes tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_trace_attributes",
		Description: traces.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg))
```

add:

```go
	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Description: traces.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg))
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add tools.go
git commit -m "feat(traces): register get_trace_attributes_for_pipeline (ENG-1250)"
```

---

## Task 4: Switch global `get_trace_attributes` to `/search/tags`

**Files:**
- Modify: `internal/telemetry/traces/attributes.go` (full replacement)
- Create: `internal/telemetry/traces/attributes_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/telemetry/traces/attributes_test.go`:

```go
package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTraceAttributes_UsesSearchTagsEndpoint(t *testing.T) {
	var capturedPath, capturedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"scopes":[` +
			`{"name":"span","tags":["http.method","StatusCode"]},` +
			`{"name":"resource","tags":["service.name","department"]},` +
			`{"name":"event","tags":["exception.type"]}` +
			`]}`))
	}))
	defer server.Close()

	handler := NewGetTraceAttributesHandler(server.Client(), newTestCfg(server.URL))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesArgs{LookbackMinutes: 30})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if capturedMethod != "GET" || !strings.Contains(capturedPath, "/cat/api/search/tags") {
		t.Errorf("expected GET /cat/api/search/tags, got: %s %s", capturedMethod, capturedPath)
	}

	got := map[string]string{}
	for _, a := range decodeTraceAttributes(t, res) {
		got[a.Name] = a.FilterField
	}
	want := map[string]string{
		"http.method":           "attributes['http.method']",
		"StatusCode":            "StatusCode",
		"resource_service.name": "ServiceName",
		"resource_department":   "resources['department']",
		"event_exception.type":  "events['exception.type']",
	}
	if len(got) != len(want) {
		t.Errorf("expected %d fields, got %d: %v", len(want), len(got), got)
	}
	for name, ff := range want {
		if got[name] != ff {
			t.Errorf("field %q: expected filter_field %q, got %q", name, ff, got[name])
		}
	}
}

func TestGetTraceAttributes_EmptyScopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"scopes":[]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributesHandler(server.Client(), newTestCfg(server.URL))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributesArgs{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty result content")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No trace attributes found") {
		t.Errorf("expected 'No trace attributes found' message, got: %s", text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/telemetry/traces/ -run TestGetTraceAttributes_ -v`
Expected: FAIL — the handler still POSTs to the series endpoint, so `capturedMethod`/`capturedPath` assertions fail.

- [ ] **Step 3: Replace `attributes.go` with the `/search/tags` implementation**

Replace the entire contents of `internal/telemetry/traces/attributes.go` with:

```go
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

// TraceAttributesResponse represents the traces series API response structure.
// Kept here because the pipeline tool (series_attributes.go) reuses it.
type TraceAttributesResponse struct {
	Data   []map[string]string `json:"data"`
	Status string              `json:"status"`
}

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

// traceTagsScopesResponse is the /cat/api/search/tags response: attributes grouped
// by scope, with the scope prefix already stripped from each tag name.
type traceTagsScopesResponse struct {
	Scopes []struct {
		Name string   `json:"name"` // "span" | "resource" | "event"
		Tags []string `json:"tags"`
	} `json:"scopes"`
}

// reprefixTraceTag restores the raw prefixed name that enrichAttribute expects
// from a (scope, stripped-name) pair returned by /search/tags.
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

	var result traceTagsScopesResponse
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
```

> This drops the `bytes` import the old version used (the global tool no longer POSTs). `TraceAttributesResponse` and `TraceAttribute` stay in this file — `series_attributes.go` and `attribute_values.go` depend on them.

- [ ] **Step 4: Run the full traces package tests**

Run: `go test ./internal/telemetry/traces/ -v`
Expected: PASS — including the new `TestGetTraceAttributes_*`, the existing `TestGetTraceAttributesHandler_InvalidTimeOrder` (time validation still happens before any request), and Task 2's pipeline tests.

- [ ] **Step 5: Verify the cache still builds (it calls `FetchTraceAttributeNames`)**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add internal/telemetry/traces/attributes.go internal/telemetry/traces/attributes_test.go
git commit -m "feat(traces): point global get_trace_attributes at /search/tags catalog (ENG-1250)"
```

---

## Task 5: Pipeline-aware `get_trace_attribute_values`

**Files:**
- Modify: `internal/telemetry/traces/attribute_values.go:32-36` (args struct) and `:67-72` (pipeline body)
- Modify: `internal/telemetry/traces/attribute_values_test.go` (add one test)

- [ ] **Step 1: Write the failing test**

Append to `internal/telemetry/traces/attribute_values_test.go`:

```go
func TestGetTraceAttributeValuesHandler_ForwardsPipeline(t *testing.T) {
	var capturedPipeline []interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if p, ok := body["pipeline"].([]interface{}); ok {
			capturedPipeline = p
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["GET"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{
		TagName: "http.method",
		Pipeline: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedPipeline) != 1 {
		t.Fatalf("expected the provided 1-stage pipeline to be forwarded, got: %v", capturedPipeline)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/telemetry/traces/ -run TestGetTraceAttributeValuesHandler_ForwardsPipeline -v`
Expected: compile error — `GetTraceAttributeValuesArgs` has no `Pipeline` field.

- [ ] **Step 3: Add the `Pipeline` field to the args struct**

In `internal/telemetry/traces/attribute_values.go`, change `GetTraceAttributeValuesArgs` from:

```go
type GetTraceAttributeValuesArgs struct {
	TagName string `json:"tag_name" jsonschema:"required,The attribute name from get_trace_attributes (e.g. resource_department or attributes['http.method'])"`
	Region  string `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}
```

to:

```go
type GetTraceAttributeValuesArgs struct {
	TagName  string                   `json:"tag_name" jsonschema:"required,The attribute name from get_trace_attributes (e.g. resource_department or attributes['http.method'])"`
	Region   string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
	Pipeline []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Optional pipeline of prior filter stages to scope values to a slice, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}]. Omit for global values."`
}
```

- [ ] **Step 4: Forward the pipeline in the request body**

In the same file, change the hardcoded pipeline body from:

```go
		// The label-values endpoint requires a POST with a pipeline body (same as series).
		pipeline := map[string]interface{}{
			"pipeline": []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{}},
			},
		}
```

to:

```go
		// The label-values endpoint requires a POST with a pipeline body (same as series).
		// Scope to the caller's pipeline when provided; otherwise discover globally.
		stages := args.Pipeline
		if len(stages) == 0 {
			stages = []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{}},
			}
		}
		pipeline := map[string]interface{}{"pipeline": stages}
```

- [ ] **Step 5: Update the tool description to mention the optional pipeline**

In the same file, change the end of `GetTraceAttributeValuesDescription` from:

```go
Returns the canonical filter_field ready to use in a get_traces tracejson query,
plus an example condition.
`
```

to:

```go
Returns the canonical filter_field ready to use in a get_traces tracejson query,
plus an example condition.

Optionally pass a pipeline to scope the returned values to a filtered slice of
spans (same pipeline shape as get_traces). Omit it for global values.
`
```

- [ ] **Step 6: Run the values tests to verify they pass**

Run: `go test ./internal/telemetry/traces/ -run TestGetTraceAttributeValuesHandler -v`
Expected: PASS — the new forwarding test plus all existing values tests (which pass no pipeline and still send the empty-filter body).

- [ ] **Step 7: Commit**

```bash
git add internal/telemetry/traces/attribute_values.go internal/telemetry/traces/attribute_values_test.go
git commit -m "feat(traces): add optional pipeline to get_trace_attribute_values (ENG-1250)"
```

---

## Task 6: Full build, test, and final verification

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 2: Run the full traces package test suite**

Run: `go test ./internal/telemetry/traces/...`
Expected: `ok  last9-mcp/internal/telemetry/traces`

- [ ] **Step 3: Run the attributes cache + whole-module tests**

Run: `go test ./...`
Expected: all packages pass (`ok` / `no test files`). The `internal/attributes` package builds against the rewritten `FetchTraceAttributeNames`.

- [ ] **Step 4: Sanity-check tool count / wiring (optional manual smoke)**

Run: `go vet ./internal/telemetry/traces/... ./...`
Expected: no diagnostics.

- [ ] **Step 5: No commit needed** (verification task). If `go vet` surfaced fixes, commit them with `fix(traces): vet cleanup (ENG-1250)`.

---

## Self-Review Notes

- **Spec coverage:** Change 1 (pipeline tool) → Tasks 2–3; Change 2 (global → `/search/tags`, incl. `FetchTraceAttributeNames` repoint and `EndpointTraceTags`) → Tasks 1, 4; Change 3 (optional pipeline on values) → Task 5; testing section → tests embedded in Tasks 2, 4, 5 + Task 6 sweep.
- **Type consistency:** `fetchTraceSeriesAttributeNames` (series, Task 2) vs `fetchTraceTagNames` + `reprefixTraceTag` (tags, Task 4) are distinct, non-colliding names. `TraceAttribute` / `TraceAttributesResponse` defined once in `attributes.go`, reused by `series_attributes.go` and `attribute_values.go`. `decodeTraceAttributes` defined once (Task 2), reused in Task 4. `newTestCfg` defined once (existing `attribute_values_test.go`), reused everywhere — never redefined.
- **Behavior preserved:** `enrichAttribute`, `normalizeTagName`, and the JSON output shape of all three tools are unchanged, so no consumer breaks. The existing `TestGetTraceAttributesHandler_InvalidTimeOrder` still passes because time validation runs before any HTTP call.
- **Cache:** `FetchTraceAttributeNames` is repointed to `/search/tags` (spec's recommended-consistency option); it has no reader today, so this is risk-free and keeps a single source of truth for global trace names.
