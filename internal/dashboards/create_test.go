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

	const apiBody = `{"dashboard":{"id":"new-id","name":"Created"}}`
	if len(result.Content) != 2 {
		t.Fatalf("Content len %d, want 2", len(result.Content))
	}
	first, ok := result.Content[0].(*mcp.TextContent)
	if !ok || first.Text != apiBody {
		t.Fatalf("Content[0] = %#v, want raw API JSON", result.Content[0])
	}
	var parsed struct {
		Dashboard struct {
			ID string `json:"id"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(first.Text), &parsed); err != nil {
		t.Fatalf("Content[0] is not JSON: %v", err)
	}
	if parsed.Dashboard.ID != "new-id" {
		t.Fatalf("dashboard.id %q", parsed.Dashboard.ID)
	}
	second, ok := result.Content[1].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[1] type %T", result.Content[1])
	}
	if !strings.Contains(second.Text, "update_dashboard") || !strings.Contains(second.Text, "id=new-id") {
		t.Fatalf("nudge %q", second.Text)
	}
	if !strings.Contains(second.Text, "Do not call create_dashboard again") {
		t.Fatalf("nudge missing re-create ban: %q", second.Text)
	}
}

func TestCreateDashboardHandler_NoNudgeWithoutID(t *testing.T) {
	const apiBody = `{"dashboard":{"name":"Created"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/dashboards/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(apiBody))
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
	if len(result.Content) != 1 {
		t.Fatalf("Content len %d, want 1", len(result.Content))
	}
	first, ok := result.Content[0].(*mcp.TextContent)
	if !ok || first.Text != apiBody {
		t.Fatalf("Content[0] = %#v", result.Content[0])
	}
	if _, ok := result.Meta["reference_url"]; ok {
		t.Fatalf("unexpected reference_url %#v", result.Meta)
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
