package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUpdateDashboardHandler_PUTsToID(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dashboard":{"id":"uuid-1","name":"Updated"}}`))
	}))
	defer srv.Close()

	dash := json.RawMessage(`{"name":"Updated","panels":[]}`)
	meta := json.RawMessage(`{"_category":"custom","_type":"metrics"}`)
	handler := NewUpdateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, UpdateDashboardArgs{
		ID: "uuid-1",
		DashboardRequest: DashboardRequest{
			Dashboard: dash,
			Metadata:  meta,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedMethod != http.MethodPut {
		t.Fatalf("method %s", capturedMethod)
	}
	if capturedPath != "/dashboards/uuid-1" {
		t.Fatalf("path %s", capturedPath)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards/uuid-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}
