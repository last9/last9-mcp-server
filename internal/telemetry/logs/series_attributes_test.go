package logs

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

func decodeLogAttributes(t *testing.T, res *mcp.CallToolResult) []LogAttribute {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var attrs []LogAttribute
	if err := json.Unmarshal([]byte(text.Text), &attrs); err != nil {
		t.Fatalf("failed to unmarshal result: %v\nraw: %s", err, text.Text)
	}
	return attrs
}

// TestLogFieldFilterField locks the raw-name -> filter_field convention. In
// particular, a real attribute whose name starts with "log_" must keep its full
// name (log_level -> attributes['log_level']), not be stripped to attributes['level'].
func TestLogFieldFilterField(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"service", "ServiceName"},
		{"severity", "SeverityText"},
		{"body", "Body"},
		{"status_code", "attributes['status_code']"},
		{"http.status_code", "attributes['http.status_code']"},
		{"log_level", "attributes['log_level']"}, // must NOT become attributes['level']
		{"log_id", "attributes['log_id']"},       // must NOT become attributes['id']
		{"resource_container_name", "resources['container_name']"},
		{"resource_k8s.namespace.name", "resources['k8s.namespace.name']"},
	}
	for _, c := range cases {
		if got := logFieldFilterField(c.name); got != c.want {
			t.Errorf("logFieldFilterField(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestGetLogAttributesForPipeline_UsesSeriesEndpoint verifies the tool POSTs the
// pipeline to /logs/api/v2/series/json and returns the union of field names from
// all label-sets, each mapped to the correct filter_field.
func TestGetLogAttributesForPipeline_UsesSeriesEndpoint(t *testing.T) {
	var capturedPath, capturedMethod string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Two label-sets with overlapping + distinct keys to exercise the union.
		_, _ = w.Write([]byte(`{"status":"success","data":[` +
			`{"service":"checkout-service","status_code":"500","resource_k8s.namespace.name":"prod"},` +
			`{"service":"checkout-service","uri":"/v1/orders"}` +
			`]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout-service"}}},
	}
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline:        pipeline,
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// Must POST to the series endpoint.
	if capturedMethod != "POST" || !strings.Contains(capturedPath, "/logs/api/v2/series/json") {
		t.Errorf("expected POST /logs/api/v2/series/json, got: %s %s", capturedMethod, capturedPath)
	}

	// Must forward the pipeline in the request body.
	if capturedBody == nil || capturedBody["pipeline"] == nil {
		t.Fatalf("expected pipeline forwarded in request body, got: %v", capturedBody)
	}

	// Union of keys, sorted, each with the correct filter_field.
	got := map[string]string{}
	for _, a := range decodeLogAttributes(t, res) {
		got[a.Name] = a.FilterField
	}

	want := map[string]string{
		"service":                     "ServiceName",
		"status_code":                 "attributes['status_code']",
		"resource_k8s.namespace.name": "resources['k8s.namespace.name']",
		"uri":                         "attributes['uri']",
	}
	if len(got) != len(want) {
		t.Errorf("expected %d unioned fields, got %d: %v", len(want), len(got), got)
	}
	for name, ff := range want {
		if got[name] != ff {
			t.Errorf("field %q: expected filter_field %q, got %q", name, ff, got[name])
		}
	}
}

// TestGetLogAttributesForPipeline_RequiresPipeline verifies the tool errors when
// no pipeline is provided.
func TestGetLogAttributesForPipeline_RequiresPipeline(t *testing.T) {
	cfg := testAttrConfig("http://unused.example")
	handler := NewGetLogAttributesForPipelineHandler(http.DefaultClient, cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{})
	if err == nil {
		t.Fatal("expected error when pipeline is empty, got nil")
	}
	if !strings.Contains(err.Error(), "pipeline parameter is required") {
		t.Errorf("expected pipeline-required error, got: %v", err)
	}
}

// TestGetLogAttributesForPipeline_EmptyData verifies an empty series response
// yields an empty attribute list (not an error).
func TestGetLogAttributesForPipeline_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if attrs := decodeLogAttributes(t, res); len(attrs) != 0 {
		t.Errorf("expected empty attribute list, got: %v", attrs)
	}
}
