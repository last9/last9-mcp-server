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
