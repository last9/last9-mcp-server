package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/utils"
)

func TestDashboardLogQueryIndex(t *testing.T) {
	for _, tc := range []struct {
		name, wantIndex string
		fields          map[string]any
		invalid         bool
	}{
		{"default", "", nil, false},
		{"empty default", "", map[string]any{"index_name": "", "index_type": ""}, false},
		{"legacy prefix", "physical_index:example", map[string]any{"index_name": "physical_index:example"}, false},
		{"null default", "", map[string]any{"index_name": nil, "index_type": nil}, false},
		{"physical", "physical_index:example", map[string]any{"index_name": "example", "index_type": "physical"}, false},
		{"rehydration", "rehydration_index:archive", map[string]any{"index_name": "archive", "index_type": "rehydration"}, false},
		{"legacy qualified name", "physical_index:example", map[string]any{"index_name": "physical_index:example"}, false},
		{"standalone", "", map[string]any{"index": "physical_index:example"}, true},
		{"name alone", "", map[string]any{"index_name": "example"}, true},
		{"type alone", "", map[string]any{"index_type": "physical"}, true},
		{"invalid type", "", map[string]any{"index_name": "example", "index_type": "bad"}, true},
		{"nonstring", "", map[string]any{"index_name": 42, "index_type": "physical"}, true},
		{"prefix in name", "", map[string]any{"index_name": "physical_index:example", "index_type": "physical"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			panel := previewTestPanel("example", `[{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}]`)
			query := panel["queries"].([]any)[0].(map[string]any)
			query["telemetry"], query["query_type"] = "logs", "log_json"
			maps.Copy(query, tc.fields)
			doc := map[string]any{"name": "Example", "panels": []any{panel}}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/dashboards/example" {
					_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": doc})
					return
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method %s", r.Method)
				}
				calls.Add(1)
				if got := r.URL.Query().Get("index"); got != tc.wantIndex || r.URL.Query().Has("index") != (tc.wantIndex != "") {
					t.Errorf("index = %q, want %q", got, tc.wantIndex)
				}
				io.WriteString(w, `{"data":{"result":[{"metric":{},"values":[[1,"2"]]}]}}`)
			}))
			defer srv.Close()
			body, _ := json.Marshal(doc)
			for _, err := range []error{validateDashboardRequest(body), validatePatchOps([]PatchDashboardOp{{Op: "update_panel", PanelID: "example", Fields: map[string]any{"queries": panel["queries"]}}})} {
				if (err != nil) != tc.invalid {
					t.Fatalf("write validation = %v", err)
				}
			}
			args := previewTestArgs()
			args.Dashboard = doc
			result, _, err := NewPreviewDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			report := utils.GetTextContent(t, result)
			wantStatus, wantCalls := "valid_with_data", int32(1)
			if tc.invalid {
				wantStatus, wantCalls = "invalid_query", 0
			}
			if !strings.Contains(report, wantStatus) || calls.Load() != wantCalls {
				t.Fatalf("calls=%d report=%s", calls.Load(), report)
			}
			id := "example"
			validate := ValidateDashboardArgs{DashboardID: &id, StartTimeISO: args.StartTimeISO, EndTimeISO: args.EndTimeISO}
			result, _, err = NewValidateDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), nil, validate)
			if err != nil {
				t.Fatal(err)
			}
			report = utils.GetTextContent(t, result)
			if !strings.Contains(report, wantStatus) || calls.Load() != 2*wantCalls {
				t.Fatalf("saved dashboard calls=%d report=%s", calls.Load(), report)
			}
			if tc.name == "standalone" && (!strings.Contains(report, "index_name") || !strings.Contains(report, "index_type")) {
				t.Fatalf("missing routing guidance: %s", report)
			}
		})
	}
}
