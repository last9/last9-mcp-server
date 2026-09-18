package grafana

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sampleDashboard = `{
  "dashboard": {
    "uid": "abc123",
    "title": "Service Health",
    "tags": ["prod"],
    "version": 3,
    "timezone": "utc",
    "templating": {
      "list": [
        {"name": "env", "type": "query", "query": "label_values(env)", "datasource": {"type":"prometheus","uid":"ds1"}},
        {"name": "service", "type": "query", "query": "up", "datasource": "prometheus"}
      ]
    },
    "panels": [
      {"id": 1, "title": "Errors", "type": "timeseries",
       "datasource": {"type":"prometheus","uid":"ds1"},
       "gridPos": {"h":8,"w":12,"x":0,"y":0},
       "targets": [
         {"refId":"A","expr":"sum(rate(http_errors_total[5m]))","datasource":{"uid":"ds1","type":"prometheus"}},
         {"refId":"B","expr":"sum(rate(http_requests_total[5m]))","datasource":{"uid":"ds1","type":"prometheus"}}
       ]},
      {"id": 2, "title": "Custom Plugin", "type": "marcusolsson-csv-datasource-panel",
       "datasource": {"type":"marcusolsson-csv","uid":"ds2"},
       "gridPos": {"h":8,"w":12,"x":12,"y":0},
       "targets": [{"refId":"A","query":"SELECT * FROM x","queryType":"json"}]}
    ]
  },
  "meta": {"canEdit": true}
}`

func testResult(t *testing.T, handler func(context.Context, *mcp.CallToolRequest, GetDashboardArgs) (*mcp.CallToolResult, any, error), args GetDashboardArgs) string {
	t.Helper()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	return result.Content[0].(*mcp.TextContent).Text
}

func TestGetDashboardHandler_Summary(t *testing.T) {
	srv := newServingServer("/api/dashboards/uid/abc123", sampleDashboard)
	defer srv.Close()

	text := testResult(t, NewGetDashboardHandler(srv.Client(), testGrafanaConfig(srv.URL)), GetDashboardArgs{UID: "abc123"})

	for _, want := range []string{`"uid": "abc123"`, "Service Health", "timeseries", "http_errors_total", "env", "marcusolsson-csv-datasource-panel", "unsupportedPanelTypes"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary missing %q\n%s", want, text)
		}
	}
	// Credential-bearing datasource object fields must never leak: only our
	// projected uid/name fields survive, never raw secrets.
	for _, deny := range []string{"basicAuthUser", "secureJsonFields", "basicAuthPassword"} {
		if strings.Contains(text, deny) {
			t.Errorf("summary leaked %q", deny)
		}
	}

	var sum DashboardSummary
	if err := json.Unmarshal([]byte(text), &sum); err != nil {
		t.Fatalf("summary is not valid JSON: %v", err)
	}
	if sum.UID != "abc123" || sum.Title != "Service Health" {
		t.Errorf("summary identity: %+v", sum)
	}
	if len(sum.Panels) != 2 {
		t.Fatalf("summary panels = %d, want 2: %+v", len(sum.Panels), sum.Panels)
	}
	if len(sum.Panels[0].Targets) != 2 || sum.Panels[0].Targets[0].Expr == "" {
		t.Errorf("prometheus panel targets not extracted: %+v", sum.Panels[0])
	}
	if sum.Panels[0].Datasource != "prometheus/ds1" {
		t.Errorf("panel datasource = %q, want prometheus/ds1", sum.Panels[0].Datasource)
	}
	if len(sum.Templating) != 2 || sum.Templating[0].Name != "env" {
		t.Errorf("templating not extracted: %+v", sum.Templating)
	}
	if len(sum.UnsupportedPanelTypes) != 1 || sum.UnsupportedPanelTypes[0] != "marcusolsson-csv-datasource-panel" {
		t.Errorf("unsupportedPanelTypes = %v", sum.UnsupportedPanelTypes)
	}
}

func TestGetDashboardHandler_FullJSON(t *testing.T) {
	srv := newServingServer("/api/dashboards/uid/abc123", sampleDashboard)
	defer srv.Close()

	text := testResult(t, NewGetDashboardHandler(srv.Client(), testGrafanaConfig(srv.URL)), GetDashboardArgs{UID: "abc123", FullJSON: true})

	if !strings.Contains(text, `"canEdit": true`) {
		t.Errorf("full_json should return the raw body, got:\n%s", text)
	}
	if !strings.Contains(text, "meta") {
		t.Errorf("full_json should include meta, got:\n%s", text)
	}
}

func TestGetDashboardHandler_MissingUID(t *testing.T) {
	srv := newServingServer("/api/dashboards/uid/x", `{}`)
	defer srv.Close()
	_, _, err := NewGetDashboardHandler(srv.Client(), testGrafanaConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{})
	if err == nil || !strings.Contains(err.Error(), "uid is required") {
		t.Fatalf("want uid-required error, got %v", err)
	}
}

func TestGetDashboardHandler_AuthHeader(t *testing.T) {
	var gotToken, gotPath string
	srv := newRecordingServer("/api/dashboards/uid/abc123", sampleDashboard, &gotToken, &gotPath)
	defer srv.Close()
	_ = testResult(t, NewGetDashboardHandler(srv.Client(), testGrafanaConfig(srv.URL)), GetDashboardArgs{UID: "abc123"})
	if gotToken != "Bearer test-token" {
		t.Fatalf("auth header = %q", gotToken)
	}
	if gotPath != "/api/dashboards/uid/abc123" {
		t.Fatalf("request path = %q", gotPath)
	}
}
