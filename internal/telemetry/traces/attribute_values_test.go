package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestCfg(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetTraceAttributeValuesHandler_EmptyTagName(t *testing.T) {
	handler := NewGetTraceAttributeValuesHandler(http.DefaultClient, models.Config{})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: ""})
	if err == nil {
		t.Fatal("expected error for empty tag_name, got nil")
	}
	if !strings.Contains(err.Error(), "tag_name is required") {
		t.Errorf("expected 'tag_name is required', got: %v", err)
	}
}

func TestGetTraceAttributeValuesHandler_WhitespaceOnlyTagName(t *testing.T) {
	// After normalizeTagName("   ") the result is "", which should be rejected.
	handler := NewGetTraceAttributeValuesHandler(http.DefaultClient, models.Config{})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only tag_name, got nil")
	}
	if !strings.Contains(err.Error(), "blank") {
		t.Errorf("expected 'cannot be blank', got: %v", err)
	}
}

func TestGetTraceAttributeValuesHandler_NonSuccessAPIStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"error","data":null}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("expected tool execution error, got protocol error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected IsError=true, got %+v", result)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `status "error"`) {
		t.Fatalf("expected non-success API status in error, got: %s", text)
	}
}

func TestGetTraceAttributeValuesHandler_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must be POST with pipeline body.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("expected valid JSON body, got: %v", err)
		}
		if _, ok := body["pipeline"]; !ok {
			t.Errorf("expected 'pipeline' key in body, got: %v", body)
		}
		// region, start, end must be query params.
		if r.URL.Query().Get("start") == "" {
			t.Errorf("expected start query param")
		}
		if r.URL.Query().Get("end") == "" {
			t.Errorf("expected end query param")
		}
		if !strings.Contains(r.URL.Path, "http.method") {
			t.Errorf("expected tag name in path, got: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["GET","POST","PUT"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected non-empty result content")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "GET") {
		t.Errorf("expected values in response, got: %s", text)
	}
	if !strings.Contains(text, `attributes['http.method']`) {
		t.Errorf("expected filter_field in response, got: %s", text)
	}
}

func TestGetTraceAttributeValuesHandler_ResourceTagNormalized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// resources['department'] normalizes to resource_department in the URL path.
		if !strings.Contains(r.URL.Path, "resource_department") {
			t.Errorf("expected resource_department in path, got: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["engineering","platform"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "resources['department']"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

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
	// Assert the caller's stage was forwarded, not the empty-filter fallback
	// (which also has length 1).
	stage, ok := capturedPipeline[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected forwarded stage to be an object, got: %T", capturedPipeline[0])
	}
	query, ok := stage["query"].(map[string]interface{})
	if !ok || query["$eq"] == nil {
		t.Errorf("expected the caller's $eq filter to be forwarded, got stage: %v", stage)
	}
}

func parseAttributeValuesResult(t *testing.T, result *mcp.CallToolResult) (hint, filterField string, values []string, raw string) {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatalf("expected non-empty result content, got %+v", result)
	}
	raw = result.Content[0].(*mcp.TextContent).Text
	var got struct {
		TagName     string   `json:"tag_name"`
		FilterField string   `json:"filter_field"`
		Values      []string `json:"values"`
		Hint        string   `json:"hint"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("result envelope is not valid JSON: %v\nraw: %s", err, raw)
	}
	return got.Hint, got.FilterField, got.Values, raw
}

func exampleJSONFromHint(t *testing.T, hint string) string {
	t.Helper()
	const marker = "Example: "
	idx := strings.Index(hint, marker)
	if idx < 0 {
		t.Fatalf("hint missing %q: %q", marker, hint)
	}
	return hint[idx+len(marker):]
}

func requireValidJSONEq(t *testing.T, hint string) (field, value string) {
	t.Helper()
	example := exampleJSONFromHint(t, hint)
	var eq struct {
		Eq []string `json:"$eq"`
	}
	if err := json.Unmarshal([]byte(example), &eq); err != nil {
		t.Fatalf("hint Example is not valid JSON: %v\nexample: %s\nhint: %q", err, example, hint)
	}
	if len(eq.Eq) != 2 {
		t.Fatalf("expected $eq array of length 2, got %v (example: %s)", eq.Eq, example)
	}
	return eq.Eq[0], eq.Eq[1]
}

func TestGetTraceAttributeValuesHandler_HintRemainsValidForCleanValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["GET","POST","PUT"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hint, filterField, values, _ := parseAttributeValuesResult(t, result)

	if filterField != `attributes['http.method']` {
		t.Fatalf("expected filter_field attributes['http.method'], got %q", filterField)
	}
	if len(values) != 3 || values[0] != "GET" || values[1] != "POST" || values[2] != "PUT" {
		t.Fatalf("expected values [GET POST PUT], got %v", values)
	}

	field, value := requireValidJSONEq(t, hint)
	if field != `attributes['http.method']` {
		t.Errorf("expected $eq[0]=%q, got %q", `attributes['http.method']`, field)
	}
	if value != `GET` {
		t.Errorf("expected $eq[1]=%q, got %q", `GET`, value)
	}

	wantHint := `Use filter_field in a tracejson condition. Example: {"$eq": ["attributes['http.method']", "GET"]}`
	if hint != wantHint {
		t.Errorf("hint changed for clean input:\nwant: %s\ngot : %s", wantHint, hint)
	}
}

func TestGetTraceAttributeValuesHandler_HintJSONEscapesValueWithDoubleQuote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["foo \"bar\""]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hint, filterField, values, _ := parseAttributeValuesResult(t, result)

	if len(values) != 1 || values[0] != `foo "bar"` {
		t.Fatalf("expected values=[%q], got %v", `foo "bar"`, values)
	}
	if filterField != `attributes['http.method']` {
		t.Fatalf("expected filter_field attributes['http.method'], got %q", filterField)
	}

	field, value := requireValidJSONEq(t, hint)
	if field != `attributes['http.method']` {
		t.Errorf("expected $eq[0]=%q, got %q", `attributes['http.method']`, field)
	}
	if value != `foo "bar"` {
		t.Errorf("expected $eq[1]=%q, got %q", `foo "bar"`, value)
	}
}

func TestGetTraceAttributeValuesHandler_HintJSONEscapesValueWithBackslash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["C:\\windows\\system32"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "file.path"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hint, filterField, values, _ := parseAttributeValuesResult(t, result)

	if len(values) != 1 || values[0] != `C:\windows\system32` {
		t.Fatalf("expected values=[%q], got %v", `C:\windows\system32`, values)
	}
	if filterField != `attributes['file.path']` {
		t.Fatalf("expected filter_field attributes['file.path'], got %q", filterField)
	}

	field, value := requireValidJSONEq(t, hint)
	if field != `attributes['file.path']` {
		t.Errorf("expected $eq[0]=%q, got %q", `attributes['file.path']`, field)
	}
	if value != `C:\windows\system32` {
		t.Errorf("expected $eq[1]=%q, got %q", `C:\windows\system32`, value)
	}
}

func TestGetTraceAttributeValuesHandler_HintJSONEscapesFilterFieldWithDoubleQuote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["x"]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: `attributes['http"x']`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hint, filterField, _, _ := parseAttributeValuesResult(t, result)

	if filterField != `attributes['http"x']` {
		t.Fatalf("expected filter_field attributes['http\"x'], got %q", filterField)
	}

	field, value := requireValidJSONEq(t, hint)
	if field != `attributes['http"x']` {
		t.Errorf("expected $eq[0]=%q, got %q", `attributes['http"x']`, field)
	}
	if value != "x" {
		t.Errorf("expected $eq[1]=%q, got %q", "x", value)
	}
}

func TestGetTraceAttributeValuesHandler_HintExampleIsCopyableIntoTraceJSONQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":["foo \"bar\""]}`)
	}))
	defer server.Close()

	handler := NewGetTraceAttributeValuesHandler(server.Client(), newTestCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hint, _, _, _ := parseAttributeValuesResult(t, result)
	example := exampleJSONFromHint(t, hint)

	llmArgs := `{"tracejson_query":[{"type":"filter","query":` + example + `}]}`
	var parsed GetTracesArgs
	if err := json.Unmarshal([]byte(llmArgs), &parsed); err != nil {
		t.Fatalf("an LLM copying the hint example produces unparseable tool args: %v\nllmArgs: %s", err, llmArgs)
	}
	if len(parsed.TracejsonQuery) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(parsed.TracejsonQuery))
	}
	stage := parsed.TracejsonQuery[0]
	if stage["type"] != "filter" {
		t.Errorf("expected stage type filter, got %v", stage["type"])
	}
	query, ok := stage["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected query map, got %T", stage["query"])
	}
	eq, ok := query["$eq"].([]interface{})
	if !ok {
		t.Fatalf("expected $eq array, got %T", query["$eq"])
	}
	if eq[0] != `attributes['http.method']` {
		t.Errorf("expected $eq[0]=%q, got %v", `attributes['http.method']`, eq[0])
	}
	if eq[1] != `foo "bar"` {
		t.Errorf("expected $eq[1]=%q, got %v", `foo "bar"`, eq[1])
	}
}
