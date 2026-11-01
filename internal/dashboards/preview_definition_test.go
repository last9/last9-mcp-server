package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
