package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPreviewPanelsStatusesAndResolvedQueries(t *testing.T) {
	var queries []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/prom_query_instant" {
			t.Errorf("preview attempted %s", r.Method)
			http.Error(w, "write refused", 405)
			return
		}
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		query := body.Query
		mu.Lock()
		queries = append(queries, query)
		mu.Unlock()
		switch query {
		case `up{env="prod"}`:
			_, _ = io.WriteString(w, `{"status":"success","data":{"result":[{"metric":{"env":"prod"},"value":[1,"2"]}]}}`)
		case "empty":
			_, _ = io.WriteString(w, `{"status":"success","data":{"result":[]}}`)
		default:
			http.Error(w, "query failed", 500)
		}
	}))
	defer srv.Close()
	definition := map[string]any{"name": "Preview", "variables": []any{map[string]any{"target": "env", "values": []any{"dev"}}}, "panels": []any{
		previewTestPanel("data", `up{env="$env"}`), previewTestPanel("empty", "empty"), previewTestPanel("error", "broken"),
		map[string]any{"id": "section", "visualization": map[string]any{"type": "section"}},
	}}
	parsed, errs := validateDashboardInput(ValidateDashboardArgs{DashboardDefinition: definition, StartTimeISO: "2026-09-10T08:00:00Z", EndTimeISO: "2026-09-10T09:00:00Z", Variables: map[string]any{"env": "prod"}})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	report, err := previewPanels(context.Background(), &validateExecutor{client: srv.Client(), cfg: testDashboardConfig(srv.URL)}, parsed)
	if err != nil {
		t.Fatal(err)
	}
	panels := report["panels"].([]any)
	for i, want := range []string{"ok", "empty", "error", "ok"} {
		if got := panels[i].(map[string]any)["status"]; got != want {
			t.Errorf("panel %d status=%v want %s", i, got, want)
		}
	}
	first := panels[0].(map[string]any)["targets"].([]any)[0].(map[string]any)
	if first["resolved_query"] != `up{env="prod"}` {
		t.Fatalf("resolved query: %v", first)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 3 || queries[0] != first["resolved_query"] {
		t.Fatalf("executed queries=%v", queries)
	}
	if report["validation"].(map[string]any)["overall"] != "fail" {
		t.Fatalf("validation=%v", report["validation"])
	}
}

func previewTestPanel(id, expr string) map[string]any {
	return map[string]any{"id": id, "name": id, "visualization": map[string]any{"type": "stat"}, "queries": []any{map[string]any{"query_type": "promql", "telemetry": "metrics", "expr": expr}}}
}

func TestPreviewSampleBytesAreBounded(t *testing.T) {
	target := map[string]any{"evidence": map[string]any{"samples": []any{map[string]any{"labels": strings.Repeat("x", 5000)}}, "sample_count": 1, "total_count": 1, "truncated": false}}
	boundPreviewEvidence(target)
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > previewSampleBytes {
		t.Fatalf("sample retained %d bytes", len(raw))
	}
	evidence := target["evidence"].(map[string]any)
	if evidence["truncated"] != true || evidence["sample_count"] != 0 {
		t.Fatalf("missing truncation: %v", evidence)
	}
}

func TestPreviewPanelStatusFailsClosed(t *testing.T) {
	for _, status := range []string{"invalid_query", "execution_error", "source_unavailable", "unresolved_variable", "unsupported_execution", "future_status"} {
		t.Run(status, func(t *testing.T) {
			panel := map[string]any{"classification": "query", "targets": []any{map[string]any{"status": "valid_with_data"}, map[string]any{"status": status}}}
			if got := previewPanelStatus(panel); got != "error" {
				t.Fatalf("status=%s", got)
			}
		})
	}
}

func TestPreviewResultSizeBoundary(t *testing.T) {
	report := map[string]any{"value": ""}
	empty, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int{-1, 0, 1} {
		report["value"] = strings.Repeat("x", previewResultBytes-len(empty)+extra)
		raw, err := previewResult(report)
		if extra > 0 {
			if err == nil {
				t.Fatal("oversized result accepted")
			}
		} else if err != nil || len(raw) != previewResultBytes+extra {
			t.Fatalf("boundary %d: bytes=%d err=%v", extra, len(raw), err)
		}
	}
}

func TestPreviewSampleByteBoundary(t *testing.T) {
	for _, extra := range []int{-1, 0, 1} {
		evidence := map[string]any{"samples": []any{strings.Repeat("x", previewSampleBytes-4+extra)}, "sample_count": 1, "total_count": 1, "truncated": false}
		boundPreviewEvidence(map[string]any{"evidence": evidence})
		raw, err := json.Marshal(evidence["samples"])
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > previewSampleBytes {
			t.Errorf("boundary %d retained %d bytes", extra, len(raw))
		}
		want := 1
		if extra > 0 {
			want = 0
		}
		if evidence["sample_count"] != want {
			t.Errorf("boundary %d sample_count=%v want %d", extra, evidence["sample_count"], want)
		}
	}
}
