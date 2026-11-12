package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListDashboardsHandler_ReturnsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointDashboards {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "uuid-1", "name": "Test"}})
	}))
	defer srv.Close()

	handler := NewListDashboardsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "uuid-1") {
		t.Fatalf("body %s", text)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok {
		t.Fatalf("expected reference_url in meta, got %v", result.Meta)
	}
	if refURL != "/v2/organizations/test-org/dashboards" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestListDashboardsHandler_ServesLockedPerRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"a","readonly":true,"created_by":"user-7"},{"id":"b","readonly":false},{"id":"c"}]`))
	}))
	defer srv.Close()

	handler := NewListDashboardsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	if len(rows) != 3 || rows[0]["locked"] != true || rows[1]["locked"] != false || rows[0]["created_by"] != "user-7" {
		t.Fatalf("rows %s", text)
	}
	if _, ok := rows[2]["locked"]; ok {
		t.Fatalf("row without readonly gained locked: %s", text)
	}
}

func TestListDashboardsHandler_BoundsLargeResponses(t *testing.T) {
	rows := make([]map[string]any, 500)
	for i := range rows {
		rows[i] = map[string]any{
			"id":         fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			"name":       strings.Repeat("dashboard ", 12),
			"updated_at": "2026-11-12T00:00:00Z",
			"readonly":   i%2 == 0,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	handler := NewListDashboardsHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDashboardsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) >= 32*1024 {
		t.Fatalf("response is %d bytes, want less than 32 KiB", len(text))
	}
}
