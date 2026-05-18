package dashboards

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDeleteDashboardHandler_DELETESByID(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	handler := NewDeleteDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardArgs{ID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if capturedMethod != http.MethodDelete {
		t.Fatalf("method %s", capturedMethod)
	}
	if capturedPath != "/dashboards/uuid-1" {
		t.Fatalf("path %s", capturedPath)
	}
}

func TestDeleteDashboardHandler_RequiresID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewDeleteDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeleteDashboardArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
