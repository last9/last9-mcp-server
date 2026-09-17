package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListFolderDashboardsHandler(t *testing.T) {
	var first, second string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.HasPrefix(r.URL.Path, "/api/folders/") {
			first = r.URL.RequestURI()
			_, _ = w.Write([]byte(`{"id":7,"uid":"fld1","title":"Core Payments"}`))
			return
		}
		second = r.URL.RequestURI()
		_, _ = w.Write([]byte(sampleSearch))
	}))
	defer srv.Close()

	result, _, err := NewListFolderDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, ListFolderDashboardsArgs{FolderUID: "fld1"})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	if !strings.Contains(text, "Payments Latency") {
		t.Fatalf("folder dashboards result = %s", text)
	}
	// The proxy 404s /api/folders/{uid}/dashboards; the handler must list via
	// /api/search?folderIds=<numeric folder id>.
	if !strings.Contains(first, "/api/folders/") || strings.Contains(first, "/dashboards") {
		t.Fatalf("folder lookup request path = %q", first)
	}
	if !strings.Contains(second, "/api/search") || !strings.Contains(second, "folderIds=7") {
		t.Fatalf("folder dashboards request path = %q", second)
	}
}

func TestListFolderDashboardsHandler_MissingFolderUID(t *testing.T) {
	srv := newServingServer("/api/folders//dashboards", `[]`)
	defer srv.Close()
	_, _, err := NewListFolderDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, ListFolderDashboardsArgs{})
	if err == nil || !strings.Contains(err.Error(), "folder_uid is required") {
		t.Fatalf("want folder_uid-required error, got %v", err)
	}
}

func TestListFolderDashboardsHandler_MissingFolder(t *testing.T) {
	srv := newServingServer("/api/folders/nope", `{"message":"Not found"}`)
	defer srv.Close()
	_, _, err := NewListFolderDashboardsHandler(srv.Client(), testGrafanaConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, ListFolderDashboardsArgs{FolderUID: "nope"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want folder-not-found error, got %v", err)
	}
}
