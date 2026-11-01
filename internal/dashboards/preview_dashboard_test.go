package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/utils"
)

func previewTestArgs() PreviewDashboardArgs {
	return PreviewDashboardArgs{StartTimeISO: "2026-09-10T08:00:00Z", EndTimeISO: "2026-09-10T09:00:00Z"}
}

func TestPreviewDashboardNeverWrites(t *testing.T) {
	for _, patch := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "patch"}[patch], func(t *testing.T) {
			var reads, writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/dashboards/dash-1" && r.Method == http.MethodGet {
					reads.Add(1)
					_, _ = io.WriteString(w, patchFixture)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/prom_query_instant" {
					_, _ = io.WriteString(w, `{"status":"success","data":{"result":[]}}`)
					return
				}
				writes.Add(1)
				t.Errorf("preview attempted forbidden request: %s %s", r.Method, r.URL.Path)
				http.Error(w, "write refused", 405)
			}))
			defer srv.Close()
			args := previewTestArgs()
			if patch {
				args.ID = "dash-1"
				args.Ops = []PatchDashboardOp{{Op: "set_title", Title: "Preview only"}}
			} else {
				args.Dashboard = map[string]any{"name": "Preview only", "panels": []any{previewTestPanel("p1", "up")}}
			}
			before, _ := json.Marshal(args)
			result, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			if writes.Load() != 0 {
				t.Fatalf("preview made %d writes", writes.Load())
			}
			if patch && reads.Load() != 1 {
				t.Fatalf("dashboard reads=%d want 1", reads.Load())
			}
			if !patch && reads.Load() != 0 {
				t.Fatalf("create preview fetched a dashboard")
			}
			var report map[string]any
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &report); err != nil {
				t.Fatal(err)
			}
			if report["saved"] != false {
				t.Fatalf("report=%v", report)
			}
			after, _ := json.Marshal(args)
			if string(after) != string(before) {
				t.Fatal("preview mutated caller arguments")
			}
		})
	}
}

func TestPreviewPatchMatchesSavedPayloadAndNetDiff(t *testing.T) {
	ps := newPatchServer(t)
	args := previewTestArgs()
	args.ID = "dash-1"
	args.Ops = []PatchDashboardOp{
		{Op: "update_panel", PanelID: "p1", Fields: map[string]any{"name": "New latency"}},
		{Op: "remove_panel", PanelID: "p2"},
		{Op: "add_panel", Panel: map[string]any{"id": "p3", "name": "Added", "visualization": map[string]any{"type": "stat"}}},
		{Op: "set_title", Title: "Preview title"},
		{Op: "set_variable", Variable: map[string]any{"target": "env", "values": []any{"prod"}}},
	}
	cfg := testDashboardConfig(ps.srv.URL)
	candidate, diff, err := preparePreviewDashboard(context.Background(), last9api.NewClient(ps.srv.Client(), cfg), args)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.puts) != 0 {
		t.Fatal("preview wrote the candidate")
	}
	if len(diff.PanelsAdded) != 1 || diff.PanelsAdded[0].ID != "p3" || len(diff.PanelsRemoved) != 1 || diff.PanelsRemoved[0].ID != "p2" {
		t.Fatalf("diff=%+v", diff)
	}
	if len(diff.PanelsChanged) != 2 || diff.PanelsChanged[0].ID != "p1" || diff.PanelsChanged[1].ID != "s1" {
		t.Fatalf("changed=%+v", diff.PanelsChanged)
	}
	_, _, err = NewPatchDashboardHandler(ps.srv.Client(), cfg)(context.Background(), nil, PatchDashboardArgs{ID: args.ID, Ops: args.Ops})
	if err != nil {
		t.Fatal(err)
	}
	if got := ps.sentDashboard(t); !reflect.DeepEqual(candidate, got) {
		t.Fatalf("preview=%v\nsaved=%v", candidate, got)
	}
}

func TestPreviewPatchNetDiffIgnoresRevertedChanges(t *testing.T) {
	ps := newPatchServer(t)
	args := previewTestArgs()
	args.ID = "dash-1"
	args.Ops = []PatchDashboardOp{{Op: "update_panel", PanelID: "p1", Fields: map[string]any{"name": "Changed"}}, {Op: "update_panel", PanelID: "p1", Fields: map[string]any{"name": "Latency"}}, {Op: "add_panel", Panel: map[string]any{"id": "temporary"}}, {Op: "remove_panel", PanelID: "temporary"}}
	_, diff, err := preparePreviewDashboard(context.Background(), last9api.NewClient(ps.srv.Client(), testDashboardConfig(ps.srv.URL)), args)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.PanelsAdded)+len(diff.PanelsChanged)+len(diff.PanelsRemoved) != 0 {
		t.Fatalf("not a net diff: %+v", diff)
	}
}

func TestPreviewDashboardInvalidInputDoesNotQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input made an API call") }))
	defer srv.Close()
	for _, tc := range []struct {
		name   string
		modify func(*PreviewDashboardArgs)
		want   string
	}{
		{"neither", func(a *PreviewDashboardArgs) { a.Dashboard = nil }, "exactly one"},
		{"both", func(a *PreviewDashboardArgs) { a.ID = "dash-1" }, "exactly one"},
		{"ops with create", func(a *PreviewDashboardArgs) { a.Ops = []PatchDashboardOp{{Op: "set_title", Title: "x"}} }, "ops require id"},
		{"id without ops", func(a *PreviewDashboardArgs) { a.Dashboard = nil; a.ID = "dash-1" }, "at least one"},
		{"bad window", func(a *PreviewDashboardArgs) { a.EndTimeISO = a.StartTimeISO }, "after start"},
		{"bad op", func(a *PreviewDashboardArgs) {
			a.Dashboard = nil
			a.ID = "dash-1"
			a.Ops = []PatchDashboardOp{{Op: "not_an_op"}}
		}, "unknown op"},
		{"too many panels", func(a *PreviewDashboardArgs) { a.Dashboard["panels"] = make([]any, previewMaxPanels+1) }, "50 panels"},
		{"too many queries", func(a *PreviewDashboardArgs) {
			a.Dashboard["panels"] = []any{map[string]any{"queries": make([]any, previewMaxQueries+1)}}
		}, "100 queries"},
		{"too large", func(a *PreviewDashboardArgs) { a.Dashboard["name"] = strings.Repeat("x", previewInputBytes) }, "65536 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := previewTestArgs()
			args.Dashboard = map[string]any{"name": "Example", "panels": []any{}}
			tc.modify(&args)
			_, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestPreviewDashboardCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	args := previewTestArgs()
	args.Dashboard = map[string]any{"name": "Canceled", "panels": []any{previewTestPanel("p1", "up")}}
	_, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(ctx, nil, args)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestPreviewPatchMissingDashboardFails(t *testing.T) {
	for _, body := range []string{``, `null`, `{}`, `{"dashboard":null}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			args := previewTestArgs()
			args.ID = "dash-1"
			args.Ops = []PatchDashboardOp{{Op: "set_title", Title: "Example"}}
			_, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
			if err == nil {
				t.Fatal("missing dashboard accepted")
			}
		})
	}
}

func TestPreviewRejectsOversizedResolvedQueryBeforeExecution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("oversized resolved query reached upstream")
		_, _ = io.WriteString(w, `{"status":"success","data":{"result":[]}}`)
	}))
	defer srv.Close()
	args := previewTestArgs()
	args.Dashboard = map[string]any{"name": "Expanded", "panels": []any{previewTestPanel("p1", "${big}${big}${big}")}}
	args.Variables = map[string]any{"big": strings.Repeat("x", 22000)}
	result, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
	if err != nil {
		t.Fatal(err)
	}
	text := utils.GetTextContent(t, result)
	if !strings.Contains(text, `"status":"error"`) || !strings.Contains(text, "resolved query exceeds") {
		t.Fatalf("report did not reject expansion: %.300s", text)
	}
}

func TestPreviewBoundsEvidenceBeforeReportAssembly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"metric": map[string]any{"large": strings.Repeat("x", 5000)}, "value": []any{1, "2"}}}}})
	}))
	defer srv.Close()
	args := previewTestArgs()
	args.Dashboard = map[string]any{"name": "Large labels", "panels": []any{previewTestPanel("p1", "up")}}
	parsed, err := validatePreviewArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	report := runValidateDashboard(context.Background(), &validateExecutor{client: srv.Client(), cfg: testDashboardConfig(srv.URL), preview: true}, parsed)
	panel := asAnySlice(report["panels"])[0].(map[string]any)
	evidence := asAnySlice(panel["targets"])[0].(map[string]any)["evidence"].(map[string]any)
	raw, err := json.Marshal(evidence["samples"])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > previewSampleBytes {
		t.Fatalf("retained %d sample bytes before report assembly", len(raw))
	}
}

func TestPreviewCandidateSizeIncludesPanelDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("oversized candidate made an API call") }))
	defer srv.Close()
	args := previewTestArgs()
	panels := make([]any, previewMaxPanels)
	for i := range panels {
		panels[i] = map[string]any{"name": "Notes", "visualization": map[string]any{"type": "markdown"}}
	}
	args.Dashboard = map[string]any{"name": "", "panels": panels}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	args.Dashboard["name"] = strings.Repeat("x", previewInputBytes-len(raw))
	_, _, err = NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
	if err == nil || !strings.Contains(err.Error(), "65536 bytes") {
		t.Fatalf("defaulted candidate should exceed its byte limit: %v", err)
	}
}
