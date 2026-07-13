package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreateDashboardSnapshotHandler_POSTsPayload(t *testing.T) {
	var captured map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/dashboards/snapshots" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"snap-1","name":"Freeze","dashboard_id":"dash-1"}`))
	}))
	defer srv.Close()

	expires := time.Now().Add(24 * time.Hour).Unix()
	handler := NewCreateDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardSnapshotArgs{
		DashboardID:         "dash-1",
		Name:                "Freeze",
		Description:         "incident window",
		ExpiresAt:           &expires,
		TimeRange:           json.RawMessage(`{"from":1710000000,"to":1710003600}`),
		Variables:           json.RawMessage(`{"env":"prod"}`),
		Region:              "us-east-1",
		DashboardDefinition: json.RawMessage(`{"name":"Dash","panels":[]}`),
		PanelData:           json.RawMessage(`{"panel-a":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dashboard_id", "name", "time_range", "dashboard_definition", "panel_data"} {
		if captured[key] == nil {
			t.Fatalf("missing %s in %v", key, captured)
		}
	}
	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL != "/v2/organizations/test-org/dashboards/snapshots/snap-1" {
		t.Fatalf("reference_url %q", refURL)
	}
}

func TestCreateDashboardSnapshotHandler_Validation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called")
	}))
	defer srv.Close()

	handler := NewCreateDashboardSnapshotHandler(srv.Client(), testDashboardConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateDashboardSnapshotArgs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "dashboard_id") {
		t.Fatalf("error %v", err)
	}
}
