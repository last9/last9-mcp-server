package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/utils"
)

func TestPreviewDashboardValidatesCreateDefinition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("non-query definition made an API call") }))
	defer srv.Close()
	for _, tc := range []struct {
		name      string
		dashboard map[string]any
		valid     bool
	}{
		{"missing name and panels", map[string]any{}, false},
		{"missing panels", map[string]any{"name": "Example"}, false},
		{"empty dashboard", map[string]any{"name": "Example", "panels": []any{}}, true},
		{"invalid panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": 2, "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, false},
		{"zero panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": 0, "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, false},
		{"string panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": "bad", "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, false},
		{"null panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": nil, "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, false},
		{"boolean panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": false, "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, false},
		{"explicit valid panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "version": 1, "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, true},
		{"default panel version", map[string]any{"name": "Example", "panels": []any{map[string]any{"name": "Notes", "layout": map[string]any{"x": 0, "y": 0, "w": 12, "h": 4}, "visualization": map[string]any{"type": "markdown"}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := previewTestArgs()
			args.Dashboard = tc.dashboard
			result, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			var report map[string]any
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &report); err != nil {
				t.Fatal(err)
			}
			validation := report["validation"].(map[string]any)
			if got := validation["overall"] == "pass"; got != tc.valid {
				t.Fatalf("validation=%v valid=%v", validation, tc.valid)
			}
			if tc.valid && len(asAnySlice(args.Dashboard["panels"])) > 0 {
				candidate, _, err := preparePreviewDashboard(context.Background(), last9api.NewClient(srv.Client(), testDashboardConfig(srv.URL)), args)
				if err != nil {
					t.Fatal(err)
				}
				if version := asAnySlice(candidate["panels"])[0].(map[string]any)["version"]; version != float64(1) {
					t.Fatalf("prepared version=%v want 1", version)
				}
			}
			panels := asAnySlice(report["panels"])
			if !tc.valid && len(panels) > 0 && panels[0].(map[string]any)["status"] != "error" {
				t.Fatal("invalid panel definition must be an error")
			}
			if !tc.valid && validation["definition_error"] == nil {
				t.Fatalf("missing schema diagnostic: %v", validation)
			}
		})
	}
}

func TestPreviewDashboardValidatesNonQueryPanelQueries(t *testing.T) {
	for _, mode := range []string{"create", "patch"} {
		for _, kind := range []string{"markdown", "section", "csv"} {
			for _, telemetry := range []string{"logs", "metrics", "no_queries"} {
				t.Run(mode+"/"+kind+"/"+telemetry, func(t *testing.T) {
					panel := map[string]any{
						"id": "p1", "name": "Notes",
						"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 4},
						"visualization": map[string]any{"type": kind},
					}
					if kind == "csv" {
						panel["visualization"] = map[string]any{"type": "table"}
						panel["csv"] = map[string]any{"columns": map[string]any{}}
					}
					if telemetry != "no_queries" {
						panel["queries"] = []any{map[string]any{
							"name": "A", "type": "range", "query_type": "promql", "telemetry": telemetry,
							"expr": "up", "legend": map[string]any{"type": "auto", "value": ""},
						}}
					}
					dashboard := map[string]any{"name": "Example", "panels": []any{panel}}
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if mode == "patch" && r.Method == http.MethodGet && r.URL.Path == "/dashboards/dash-1" {
							_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": dashboard})
							return
						}
						t.Errorf("non-query preview made unexpected request: %s %s", r.Method, r.URL.Path)
						http.Error(w, "unexpected request", http.StatusBadRequest)
					}))
					defer srv.Close()
					args := previewTestArgs()
					if mode == "patch" {
						args.ID = "dash-1"
						args.Ops = []PatchDashboardOp{{Op: "set_title", Title: "Preview"}}
					} else {
						args.Dashboard = dashboard
					}
					result, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
					if err != nil {
						t.Fatal(err)
					}
					var report map[string]any
					if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &report); err != nil {
						t.Fatal(err)
					}
					validation := report["validation"].(map[string]any)
					gotPanel := asAnySlice(report["panels"])[0].(map[string]any)
					wantStatus, wantOverall := "ok", "pass"
					if telemetry == "logs" {
						wantStatus, wantOverall = "error", "fail"
						if diagnostic, _ := gotPanel["definition_error"].(string); !strings.Contains(diagnostic, "not valid for telemetry logs") {
							t.Errorf("missing compatibility diagnostic: %v", gotPanel)
						}
					}
					if gotPanel["status"] != wantStatus || validation["overall"] != wantOverall {
						t.Errorf("panel status=%v overall=%v; want %s/%s", gotPanel["status"], validation["overall"], wantStatus, wantOverall)
					}
					if len(asAnySlice(gotPanel["targets"])) != 0 {
						t.Errorf("non-query panel executed targets: %v", gotPanel)
					}
				})
			}
		}
	}
}
