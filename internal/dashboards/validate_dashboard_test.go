package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateDashboardInput_XORAndWindow(t *testing.T) {
	id := "dash-1"
	cases := []struct {
		name    string
		args    ValidateDashboardArgs
		wantErr string
	}{
		{
			name: "both",
			args: ValidateDashboardArgs{
				DashboardID:         &id,
				DashboardDefinition: map[string]any{},
				StartTimeISO:        "2026-09-10T08:00:00Z",
				EndTimeISO:          "2026-09-10T09:00:00Z",
			},
			wantErr: "exactly one",
		},
		{
			name: "neither",
			args: ValidateDashboardArgs{
				StartTimeISO: "2026-09-10T08:00:00Z",
				EndTimeISO:   "2026-09-10T09:00:00Z",
			},
			wantErr: "exactly one",
		},
		{
			name: "window too large",
			args: ValidateDashboardArgs{
				DashboardID:  &id,
				StartTimeISO: "2026-09-01T08:00:00Z",
				EndTimeISO:   "2026-09-10T09:00:00Z",
			},
			wantErr: "24 hours",
		},
		{
			name: "end before start",
			args: ValidateDashboardArgs{
				DashboardID:  &id,
				StartTimeISO: "2026-09-10T09:00:00Z",
				EndTimeISO:   "2026-09-10T08:00:00Z",
			},
			wantErr: "after start",
		},
		{
			name: "missing start",
			args: ValidateDashboardArgs{
				DashboardID: &id,
				EndTimeISO:  "2026-09-10T09:00:00Z",
			},
			wantErr: "start_time_iso",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := validateDashboardInput(tc.args)
			if len(errs) == 0 {
				t.Fatal("expected errors")
			}
			joined := strings.Join(errs, "; ")
			if !strings.Contains(joined, tc.wantErr) {
				t.Fatalf("got %q, want substring %q", joined, tc.wantErr)
			}
		})
	}
}

func TestValidateDashboardHandler_InvalidInputNoHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not be called")
	}))
	defer srv.Close()

	handler := NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	id := "dash-1"
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ValidateDashboardArgs{
		DashboardID:         &id,
		DashboardDefinition: map[string]any{},
		StartTimeISO:        "2026-09-10T08:00:00Z",
		EndTimeISO:          "2026-09-10T09:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSummarizeOverall(t *testing.T) {
	pass := summarize([]map[string]any{
		{"classification": "query", "targets": []map[string]any{
			{"status": "valid_with_data"},
		}},
	})
	if pass["overall"] != "pass" {
		t.Fatalf("overall=%v", pass["overall"])
	}

	partial := summarize([]map[string]any{
		{"classification": "query", "targets": []map[string]any{
			{"status": "unsupported_execution"},
		}},
	})
	if partial["overall"] != "partial" {
		t.Fatalf("overall=%v", partial["overall"])
	}

	fail := summarize([]map[string]any{
		{"classification": "query", "targets": []map[string]any{
			{"status": "invalid_query"},
			{"status": "unsupported_execution"},
		}},
	})
	if fail["overall"] != "fail" {
		t.Fatalf("overall=%v", fail["overall"])
	}
}

func TestInterpolateBuiltinAndUnresolved(t *testing.T) {
	out, used, unresolved := interpolate(
		`rate(http_requests_total{job="$job"}[$__rate_interval])`,
		map[string]any{},
		map[string]any{},
	)
	if !strings.Contains(out, "5m") {
		t.Fatalf("builtin not resolved: %s", out)
	}
	if used["__rate_interval"]["source"] != "builtin_default" {
		t.Fatalf("used=%v", used)
	}
	if len(unresolved) != 1 || unresolved[0] != "job" {
		t.Fatalf("unresolved=%v", unresolved)
	}
}

func TestLintPromQLAndLogJSONTimeseries(t *testing.T) {
	if !hasBlockingError(lintPromQL("")) {
		t.Fatal("empty promql should block")
	}
	if !hasBlockingError(lintPromQL("up +")) {
		t.Fatal("dangling op should block")
	}
	pipeline := []any{
		map[string]any{"type": "filter", "query": map[string]any{}},
	}
	findings := lintPipeline(pipeline, "log_json", "timeseries")
	if !hasBlockingError(findings) {
		t.Fatal("timeseries without aggregate should block")
	}
}

func TestValidateDashboard_InlinePromWithData(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodPost {
			t.Errorf("unexpected write-ish method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up"},"value":[1,"1"]}]}}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	cfg.PrometheusReadURL = "http://prom.example"
	handler := NewValidateDashboardHandler(srv.Client(), cfg)

	args := ValidateDashboardArgs{
		DashboardDefinition: map[string]any{
			"name": "Inline",
			"panels": []any{
				map[string]any{
					"id":   "p1",
					"name": "Up",
					"visualization": map[string]any{
						"type": "stat",
					},
					"queries": []any{
						map[string]any{
							"name":       "A",
							"query_type": "promql",
							"telemetry":  "metrics",
							"expr":       "up",
						},
					},
				},
			},
		},
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	}
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var report map[string]any
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatal(err)
	}
	if report["success"] != true {
		t.Fatalf("report=%s", text)
	}
	if report["schema_version"] != schemaVersion {
		t.Fatalf("schema=%v", report["schema_version"])
	}
	summary := report["summary"].(map[string]any)
	if summary["overall"] != "pass" {
		t.Fatalf("overall=%v by_status=%v", summary["overall"], summary["by_status"])
	}
	for _, m := range methods {
		if strings.HasPrefix(m, "PUT") || strings.HasPrefix(m, "DELETE") || strings.Contains(m, "/dashboards") && !strings.Contains(m, "query") {
			t.Fatalf("unexpected mutating/dashboard path: %s", m)
		}
	}
}

func TestValidateDashboard_InlineEmptySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	cfg.PrometheusReadURL = "http://prom.example"
	handler := NewValidateDashboardHandler(srv.Client(), cfg)

	args := ValidateDashboardArgs{
		DashboardDefinition: map[string]any{
			"name": "Empty",
			"panels": []any{
				map[string]any{
					"id":            "p1",
					"visualization": map[string]any{"type": "timeseries"},
					"queries": []any{
						map[string]any{"query_type": "promql", "expr": "up"},
					},
				},
			},
		},
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	}
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report)
	panels := report["panels"].([]any)
	targets := panels[0].(map[string]any)["targets"].([]any)
	status := targets[0].(map[string]any)["status"]
	if status != "valid_no_data" {
		t.Fatalf("status=%v report=%v", status, report)
	}
	if targets[0].(map[string]any)["diagnosis"] != nil {
		t.Fatal("day-1 diagnosis should be null")
	}
}

func TestValidateDashboard_LintBlocksExecution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("lint should block HTTP")
	}))
	defer srv.Close()

	handler := NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	args := ValidateDashboardArgs{
		DashboardDefinition: map[string]any{
			"panels": []any{
				map[string]any{
					"queries": []any{
						map[string]any{"query_type": "promql", "expr": ""},
					},
				},
			},
		},
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	}
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report)
	status := report["panels"].([]any)[0].(map[string]any)["targets"].([]any)[0].(map[string]any)["status"]
	if status != "invalid_query" {
		t.Fatalf("status=%v", status)
	}
}

func TestValidateDashboard_SavedDashboardUnwrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/dashboards/") {
			_, _ = w.Write([]byte(`{"dashboard":{"id":"dash-1","name":"Saved","panels":[{"id":"p1","visualization":{"type":"stat"},"queries":[{"query_type":"promql","expr":"up"}]}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1,"1"]}]}}`))
	}))
	defer srv.Close()

	cfg := testDashboardConfig(srv.URL)
	cfg.PrometheusReadURL = "http://prom.example"
	handler := NewValidateDashboardHandler(srv.Client(), cfg)
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
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report)
	if report["success"] != true {
		t.Fatalf("%v", report)
	}
	dash := report["dashboard"].(map[string]any)
	if dash["source"] != "saved" || dash["id"] != "dash-1" {
		t.Fatalf("dashboard=%v", dash)
	}
}

func TestValidateDashboard_UnsupportedLogQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("log_ql should not execute")
	}))
	defer srv.Close()

	handler := NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ValidateDashboardArgs{
		DashboardDefinition: map[string]any{
			"panels": []any{
				map[string]any{
					"queries": []any{
						map[string]any{"query_type": "log_ql", "expr": `{app="x"}[5m]`},
					},
				},
			},
		},
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report)
	if report["summary"].(map[string]any)["overall"] != "partial" {
		t.Fatalf("%v", report["summary"])
	}
}

func TestValidateDashboard_NonListPanelsFailClosed(t *testing.T) {
	handler := NewValidateDashboardHandler(http.DefaultClient, testDashboardConfig("http://127.0.0.1:9"))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ValidateDashboardArgs{
		DashboardDefinition: map[string]any{
			"panels": map[string]any{"bad": true},
		},
		StartTimeISO: "2026-09-10T08:00:00Z",
		EndTimeISO:   "2026-09-10T09:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report)
	if report["success"] != false || report["error"] != "invalid_input" {
		t.Fatalf("%v", report)
	}
}
