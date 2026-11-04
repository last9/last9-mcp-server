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

func validateInline(t *testing.T, definition map[string]any) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	handler := NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ValidateDashboardArgs{
		DashboardDefinition: definition,
		StartTimeISO:        "2026-09-10T08:00:00Z",
		EndTimeISO:          "2026-09-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

// validateSaved validates dashboard dash-1, served as body by the fake API.
func validateSaved(t *testing.T, body string) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	handler := NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	id := "dash-1"
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ValidateDashboardArgs{
		DashboardID:  &id,
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func markdownDashboard() map[string]any {
	return map[string]any{
		"name": "Notes",
		"panels": []any{map[string]any{
			"name":          "Readme",
			"layout":        map[string]any{"x": 0.0, "y": 0.0, "w": 12.0, "h": 4.0},
			"visualization": map[string]any{"type": "markdown"},
		}},
	}
}

func TestValidateDashboard_UnwrapsCreateEnvelope(t *testing.T) {
	report := validateInline(t, map[string]any{
		"dashboard": markdownDashboard(),
		"metadata":  map[string]any{"category": "custom"},
	})
	if report["success"] != true {
		t.Fatalf("report=%v", report)
	}
	summary := report["summary"].(map[string]any)
	if summary["panels_total"] != float64(1) || summary["overall"] != "pass" {
		t.Fatalf("summary=%v", summary)
	}
	if name := report["dashboard"].(map[string]any)["name"]; name != "Notes" {
		t.Fatalf("dashboard name=%v", name)
	}
	if note, _ := report["note"].(string); !strings.Contains(note, "envelope") {
		t.Fatalf("note=%q", note)
	}
}

func TestValidateDashboard_EnvelopeWithNoPanelsKeepsNote(t *testing.T) {
	report := validateInline(t, map[string]any{
		"dashboard": map[string]any{"name": "Empty", "panels": []any{}},
		"metadata":  map[string]any{"category": "custom"},
	})
	if report["success"] != false || report["error"] != "no_panels" {
		t.Fatalf("report=%v", report)
	}
	if note, _ := report["note"].(string); !strings.Contains(note, "envelope") {
		t.Fatalf("note=%q", note)
	}
}

func TestValidateDashboard_RejectsUnreadableEnvelope(t *testing.T) {
	cases := map[string]map[string]any{
		"dashboard is not an object": {"dashboard": "Notes", "metadata": map[string]any{}},
		"dashboard next to panels":   {"dashboard": markdownDashboard(), "panels": []any{}},
	}
	for name, definition := range cases {
		t.Run(name, func(t *testing.T) {
			report := validateInline(t, definition)
			detail, _ := json.Marshal(report["detail"])
			if report["success"] != false || report["error"] != "invalid_input" || !strings.Contains(string(detail), "inner dashboard object") {
				t.Fatalf("report=%v", report)
			}
		})
	}
}

func TestValidateDashboard_NoPanelsFails(t *testing.T) {
	cases := map[string]map[string]any{
		"empty panels":   {"name": "Empty", "panels": []any{}},
		"missing panels": {"name": "Empty"},
		"null panels":    {"name": "Empty", "panels": nil},
	}
	for name, definition := range cases {
		t.Run(name, func(t *testing.T) {
			report := validateInline(t, definition)
			detail, _ := json.Marshal(report["detail"])
			if report["success"] != false || report["error"] != "no_panels" || !strings.Contains(string(detail), "no panels") {
				t.Fatalf("report=%v", report)
			}
		})
	}
}

func TestValidateDashboard_SavedNoPanelsNamesDashboardID(t *testing.T) {
	report := validateSaved(t, `{"dashboard":{"id":"dash-1","name":"Empty","panels":[]}}`)
	detail, _ := json.Marshal(report["detail"])
	if report["error"] != "no_panels" || !strings.Contains(string(detail), "dash-1") || strings.Contains(string(detail), "dashboard_definition") {
		t.Fatalf("report=%v", report)
	}
}

func TestValidateDashboard_GrafanaPanelReportsEachUnknownField(t *testing.T) {
	report := validateInline(t, map[string]any{
		"name": "Grafana draft",
		"panels": []any{map[string]any{
			"title":   "CPU",
			"type":    "timeseries",
			"gridPos": map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
			"targets": []any{map[string]any{"expr": "up", "refId": "A"}},
		}},
	})
	if report["success"] != true {
		t.Fatalf("report=%v", report)
	}
	summary := report["summary"].(map[string]any)
	if summary["overall"] != "fail" || summary["panels_invalid_shape"] != float64(1) {
		t.Fatalf("summary=%v", summary)
	}
	panel := report["panels"].([]any)[0].(map[string]any)
	problems, _ := json.Marshal(panel["shape_problems"])
	for _, want := range []string{
		"panels[0].gridPos: unknown panel field; use layout",
		"panels[0].title: unknown panel field; use name",
		"panels[0].type: unknown panel field; use visualization",
		"panels[0].targets: unknown panel field; use queries",
	} {
		if !strings.Contains(string(problems), want) {
			t.Errorf("shape_problems=%s, missing %q", problems, want)
		}
	}
}

func TestValidateDashboard_MistypedQueryFieldFails(t *testing.T) {
	report := validateInline(t, map[string]any{
		"name": "Notes",
		"panels": []any{map[string]any{
			"name":          "Readme",
			"layout":        map[string]any{"x": 0.0, "y": 0.0, "w": 12.0, "h": 4.0},
			"visualization": map[string]any{"type": "markdown"},
			"queries":       []any{map[string]any{"legend": "{{service}}"}},
		}},
	})
	summary := report["summary"].(map[string]any)
	panel := report["panels"].([]any)[0].(map[string]any)
	problems, _ := json.Marshal(panel["shape_problems"])
	if summary["overall"] != "fail" || !strings.Contains(string(problems), "panels[0].queries[0].legend: got string, want object") {
		t.Fatalf("summary=%v shape_problems=%s", summary, problems)
	}
}

// Saved query-free panels can come back with "queries": null, which is no shape problem.
func TestValidateDashboard_SavedQueryFreePanelQueries(t *testing.T) {
	panels := map[string]string{
		"section":  `"visualization":{"type":"section"}`,
		"markdown": `"visualization":{"type":"markdown"}`,
		"csv":      `"visualization":{"type":"csv"},"csv":{"data":"a,b"}`,
	}
	queries := map[string]struct {
		field string
		pass  bool
	}{
		"omitted":   {"", true},
		"null":      {`,"queries":null`, true},
		"empty":     {`,"queries":[]`, true},
		"malformed": {`,"queries":"up"`, false},
	}
	for kind, fields := range panels {
		for name, q := range queries {
			t.Run(kind+"/"+name, func(t *testing.T) {
				report := validateSaved(t, `{"dashboard":{"id":"dash-1","name":"Notes","panels":[{"name":"Notes","layout":{"x":0,"y":0,"w":6,"h":4},`+fields+q.field+`}]}}`)
				summary, _ := report["summary"].(map[string]any)
				panels, _ := report["panels"].([]any)
				if len(panels) != 1 {
					t.Fatalf("report=%v", report)
				}
				problems, _ := json.Marshal(panels[0].(map[string]any)["shape_problems"])
				if q.pass && (summary["overall"] != "pass" || string(problems) != "null") {
					t.Fatalf("summary=%v shape_problems=%s", summary, problems)
				}
				if !q.pass && (summary["overall"] != "fail" || !strings.Contains(string(problems), "panels[0].queries: got string, want array")) {
					t.Fatalf("summary=%v shape_problems=%s", summary, problems)
				}
			})
		}
	}
}
