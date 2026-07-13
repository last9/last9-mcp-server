package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetDashboardSnapshotHandler_GETsByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dashboards/snapshots/snap-1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                    "snap-1",
			"name":                  "Freeze",
			"dashboard_definition":  map[string]any{"name": "Dash"},
			"panel_data":            map[string]any{},
			"time_range":            map[string]any{"from": 1, "to": 2},
		})
	}))
	defer srv.Close()

	handler := NewGetDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardSnapshotArgs{ID: "snap-1"})
	if err != nil {
		t.Fatal(err)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards/snapshots/snap-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestGetDashboardSnapshotHandler_RequiresID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewGetDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardSnapshotArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
