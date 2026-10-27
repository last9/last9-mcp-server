package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func validCreateArgs() CreateDashboardArgs {
	dash := json.RawMessage(`{"name":"Created","panels":[{"name":"p","version":1,"layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""}}]}]}`)
	meta := json.RawMessage(`{"_category":"custom","_type":"metrics"}`)
	return CreateDashboardArgs{
		DashboardRequest: DashboardRequest{Dashboard: dash, Metadata: meta},
	}
}

func createDashboardAPI(t *testing.T, responseBody string, captured *map[string]json.RawMessage) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/dashboards/") {
			http.NotFound(w, r)
			return
		}
		if captured != nil {
			if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateDashboardHandler_POSTsEnvelope(t *testing.T) {
	var captured map[string]json.RawMessage
	srv := createDashboardAPI(t, `{"dashboard":{"id":"new-id","name":"Created"}}`, &captured)

	result, _, err := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, validCreateArgs())
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

func TestCreateDashboardHandler_AppendsRefineNudge(t *testing.T) {
	const apiBody = `{"dashboard":{"id":"new-id","name":"Created"}}`
	srv := createDashboardAPI(t, apiBody, nil)

	result, _, err := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, validCreateArgs())
	if err != nil {
		t.Fatal(err)
	}
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
	if !strings.Contains(second.Text, "call patch_dashboard with id=new-id") {
		t.Fatalf("nudge does not route panel edits to patch_dashboard: %q", second.Text)
	}
	if !strings.Contains(second.Text, "update_dashboard only to restructure") {
		t.Fatalf("nudge does not limit update_dashboard to restructuring: %q", second.Text)
	}
	if !strings.Contains(second.Text, "Do not call create_dashboard again") {
		t.Fatalf("nudge missing re-create ban: %q", second.Text)
	}
}

// The nudge names update_dashboard only when the server serves it, as under
// --read-only --allow-write-tools=create_dashboard it does not.
func TestCreateDashboardHandler_NudgeNamesOnlyServedRefineTools(t *testing.T) {
	srv := createDashboardAPI(t, `{"dashboard":{"id":"new-id","name":"Created"}}`, nil)
	tests := []struct {
		name       string
		allow      toolsets.Set
		wantPatch  bool
		wantUpdate bool
	}{
		{name: "refine hidden", allow: toolsets.Set{"create_dashboard": {}}},
		{name: "update allowed", allow: toolsets.Set{"create_dashboard": {}, "update_dashboard": {}}, wantUpdate: true},
		{name: "patch allowed", allow: toolsets.Set{"create_dashboard": {}, "patch_dashboard": {}}, wantPatch: true},
		{name: "both allowed", allow: toolsets.Set{"create_dashboard": {}, "patch_dashboard": {}, "update_dashboard": {}}, wantPatch: true, wantUpdate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testDashboardConfig(srv.URL)
			cfg.ToolFilter = toolsets.Filter{ReadOnly: true, AllowWrite: tt.allow}
			result, _, err := NewCreateDashboardHandler(srv.Client(), cfg)(
				context.Background(), &mcp.CallToolRequest{}, validCreateArgs())
			if err != nil {
				t.Fatal(err)
			}
			nudge := result.Content[len(result.Content)-1].(*mcp.TextContent).Text
			if got := strings.Contains(nudge, "patch_dashboard with id=new-id"); got != tt.wantPatch {
				t.Fatalf("nudge names patch_dashboard = %v, want %v: %q", got, tt.wantPatch, nudge)
			}
			if got := strings.Contains(nudge, "update_dashboard"); got != tt.wantUpdate {
				t.Fatalf("nudge names update_dashboard = %v, want %v: %q", got, tt.wantUpdate, nudge)
			}
			if !strings.Contains(nudge, "Do not call create_dashboard again") {
				t.Fatalf("nudge missing re-create ban: %q", nudge)
			}
		})
	}
}

func TestCreateDashboardHandler_NoNudgeWithoutID(t *testing.T) {
	const apiBody = `{"dashboard":{"name":"Created"}}`
	srv := createDashboardAPI(t, apiBody, nil)

	result, _, err := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, validCreateArgs())
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

func TestCreateDashboardHandler_DefaultsPanelVersion(t *testing.T) {
	var captured map[string]json.RawMessage
	srv := createDashboardAPI(t, `{"dashboard":{"id":"new-id","name":"Created"}}`, &captured)

	dash := json.RawMessage(`{"name":"Created","panels":[{"name":"p","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""}}]},{"name":"s","visualization":{"type":"section"}},{"name":"m","visualization":{"type":"markdown"}}]}`)
	args := CreateDashboardArgs{DashboardRequest: DashboardRequest{Dashboard: dash}}

	_, _, err := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}

	var sent struct {
		Panels []struct {
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(captured["dashboard"], &sent); err != nil {
		t.Fatalf("captured dashboard: %v", err)
	}
	if len(sent.Panels) != 3 {
		t.Fatalf("panels %d", len(sent.Panels))
	}
	if sent.Panels[0].Version != 1 {
		t.Fatalf("telemetry panel version %d, want 1", sent.Panels[0].Version)
	}
	if sent.Panels[1].Version != 0 {
		t.Fatalf("section panel version %d, want left unset", sent.Panels[1].Version)
	}
	if sent.Panels[2].Version != 1 {
		t.Fatalf("markdown panel version %d, want 1", sent.Panels[2].Version)
	}
}

func TestCreateDashboardHandler_Validation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	t.Cleanup(srv.Close)

	_, _, err := NewCreateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, CreateDashboardArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
