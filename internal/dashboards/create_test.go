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

func TestCreateDashboardHandler_POSTsEnvelope(t *testing.T) {
	var captured map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/dashboards/") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dashboard":{"id":"new-id","name":"Created"}}`))
	}))
	defer srv.Close()

	dash := json.RawMessage(`{"name":"Created","panels":[{"name":"p","version":1,"layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""}}]}]}`)
	meta := json.RawMessage(`{"_category":"custom","_type":"metrics"}`)
	handler := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardArgs{
		DashboardRequest: DashboardRequest{Dashboard: dash, Metadata: meta},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured["dashboard"] == nil || captured["metadata"] == nil {
		t.Fatalf("captured %v", captured)
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards/new-id" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestCreateDashboardHandler_Validation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
