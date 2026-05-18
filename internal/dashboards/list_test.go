package dashboards

import (
	"context"
	"encoding/json"
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
