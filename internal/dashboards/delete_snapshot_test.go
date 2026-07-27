package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDeleteDashboardSnapshotHandler_DELETESByID(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "snap-1"})
	}))
	defer srv.Close()

	handler := NewDeleteDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardSnapshotArgs{ID: "snap-1"})
	if err != nil {
		t.Fatal(err)
	}
	if capturedMethod != http.MethodDelete {
		t.Fatalf("method %s", capturedMethod)
	}
	if capturedPath != "/dashboards/snapshots/snap-1" {
		t.Fatalf("path %s", capturedPath)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != `{"id":"snap-1"}` {
		t.Fatalf("body %q", text)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestDeleteDashboardSnapshotHandler_RequiresID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewDeleteDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardSnapshotArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
