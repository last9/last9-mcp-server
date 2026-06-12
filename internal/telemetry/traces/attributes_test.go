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
