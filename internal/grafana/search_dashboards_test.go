package grafana

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sampleSearch = `[
  {"id": 1, "uid": "dash1", "title": "Payments Latency", "uri": "db/payments", "url": "/d/payments", "type": "dash-db", "tags": ["payments"]}
]`

func TestSearchDashboardsHandler(t *testing.T) {
	var gotPath string
	srv := newRecordingServer("/api/search", sampleSearch, new(string), &gotPath)
	defer srv.Close()

	cfg := testGrafanaConfig(srv.URL)
	result, _, err := NewSearchDashboardsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, SearchDashboardsArgs{Query: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	for _, want := range []string{`"uid": "dash1"`, "Payments Latency", `"dashboards"`} {
		if !strings.Contains(text, want) {
			t.Errorf("search result missing %q: %s", want, text)
		}
	}
	if !strings.Contains(gotPath, "/api/search") || !strings.Contains(gotPath, "query=payments") || !strings.Contains(gotPath, "type=dash-db") {
		t.Fatalf("search request path = %q", gotPath)
	}

	var res SearchResults
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("search result not valid JSON: %v", err)
	}
	if len(res.Dashboards) != 1 || res.Dashboards[0].UID != "dash1" {
		t.Fatalf("search hits = %+v", res.Dashboards)
	}
	if res.Truncated {
		t.Fatalf("single-page result marked truncated")
	}
}

func TestSearchDashboardsHandler_EmptyQuery(t *testing.T) {
	srv := newServingServer("/api/search", `[]`)
	defer srv.Close()
	_, _, err := NewSearchDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, SearchDashboardsArgs{})
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("want query-required error, got %v", err)
	}
}
