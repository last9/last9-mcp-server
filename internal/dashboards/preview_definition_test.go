package dashboards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
