package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetDashboardHandler_RequiresRegion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"dashboard": map[string]any{"id": "uuid-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	cfg.Region = ""

	handler := NewGetDashboardHandler(srv.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1", Region: ""})
	if err == nil {
		t.Fatal("expected error for empty region")
	}
}

func TestGetDashboardHandler_SendsRegionQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"dashboard": map[string]any{"id": "uuid-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1", Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "region=us-east-1") {
		t.Fatalf("query %q", gotQuery)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok {
		t.Fatalf("expected reference_url in meta, got %v", result.Meta)
	}
	if refURL != "/v2/organizations/test-org/dashboards/uuid-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestGetDashboardHandler_UsesConfiguredRegionDefault(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != fmt.Sprintf("/dashboards/%s", "uuid-1") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": map[string]any{"id": "uuid-1"}})
	}))
	defer srv.Close()

	handler := NewGetDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDashboardArgs{ID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "region=us-east-1") {
		t.Fatalf("query %q", gotQuery)
	}
}
