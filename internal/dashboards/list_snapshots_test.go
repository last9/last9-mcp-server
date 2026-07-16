package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListDashboardSnapshotsHandler_RequiresDashboardID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewListDashboardSnapshotsHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardSnapshotsArgs{})
	if err == nil || !strings.Contains(err.Error(), "dashboard_id") {
		t.Fatalf("error %v", err)
	}
}

func TestListDashboardSnapshotsHandler_QueriesByDashboardID(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dashboards/snapshots" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"snapshots": []map[string]any{{"id": "snap-1", "name": "Freeze"}},
		})
	}))
	defer srv.Close()

	handler := NewListDashboardSnapshotsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardSnapshotsArgs{
		DashboardID: "dash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "dashboard_id=dash-1" {
		t.Fatalf("query %q", gotQuery)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "snap-1") {
		t.Fatalf("body %s", text)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards/dash-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}
