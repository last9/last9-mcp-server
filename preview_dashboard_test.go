package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisteredPreviewDashboard(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dashboards/example" {
			t.Errorf("preview made unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "write refused", 405)
			return
		}
		_, _ = io.WriteString(w, `{"dashboard":{"name":"Example","panels":[{"id":"p1","name":"Details","visualization":{"type":"section"}}]}}`)
	}))
	defer upstream.Close()
	cfg := testToolRegistrationConfig()
	cfg.APIBaseURL = upstream.URL
	allowed, err := toolsets.Parse("dashboards")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ToolFilter = toolsets.Filter{Tools: allowed, ReadOnly: true}
	server := newRegisterTestServer(t)
	if _, err := registerTools(server, cfg, upstream.Client()); err != nil {
		t.Fatal(err)
	}
	session := connectRegisteredServer(t, server)
	for _, input := range []map[string]any{
		{"dashboard": map[string]any{"name": "Inline", "panels": []any{map[string]any{"name": "Details", "visualization": map[string]any{"type": "section"}}}}},
		{"id": "example", "ops": []any{map[string]any{"op": "set_title", "title": "Preview title"}}},
	} {
		input["start_time_iso"] = "2026-09-10T08:00:00Z"
		input["end_time_iso"] = "2026-09-10T09:00:00Z"
		result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "preview_dashboard", Arguments: input})
		if result.IsError || !strings.Contains(toolResultText(result), `"saved":false`) {
			t.Fatalf("preview result=%+v", result)
		}
		input["unexpected"] = true
		result = callRegisteredTool(t, session, &mcp.CallToolParams{Name: "preview_dashboard", Arguments: input})
		assertToolError(t, result, toolErrorExpectation{contains: []string{"unexpected"}})
	}
}
