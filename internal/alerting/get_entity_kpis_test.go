package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/toolsets"
)

func TestGetEntityKPIsListsIDsQueriesAndUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/entities/entity-1/kpis" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`[` +
			`{"id":"kpi-2","name":"p99_latency","definition":{"query":"histogram_quantile(0.99, rate(x_bucket[5m]))","unit":"seconds","source":"levitate"},"kpi_type":"custom"},` +
			`{"id":"kpi-1","name":"error_ratio","description":"5xx share","definition":{"query":"sum(rate(errors[5m])) / sum(rate(requests[5m]))","unit":"ratio"}}]`))
	}))
	defer server.Close()

	result, _, err := NewGetEntityKPIsHandler(server.Client(), testAlertMutationConfig(server.URL))(
		context.Background(), nil, GetEntityKPIsArgs{EntityID: "entity-1"})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := `{"entity_id":"entity-1","count":2,"kpis":[` +
		`{"id":"kpi-1","name":"error_ratio","description":"5xx share","query":"sum(rate(errors[5m])) / sum(rate(requests[5m]))","unit":"ratio"},` +
		`{"id":"kpi-2","name":"p99_latency","query":"histogram_quantile(0.99, rate(x_bucket[5m]))","unit":"seconds"}]}`
	if got := resultText(result); got != want {
		t.Fatalf("text =\n%s\nwant\n%s", got, want)
	}
}

func TestGetEntityKPIsEmptyGroupNamesCreateKPIOnlyWhenServed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	for _, tt := range []struct {
		name       string
		toolsets   string
		readOnly   bool
		allowWrite string
		wantCreate bool
	}{
		{"all_toolsets_writable", "", false, "", true},
		{"alerts_writable", "alerts", false, "", true},
		{"investigate_omits_create_kpi", "investigate", false, "", false},
		{"alerts_read_only", "alerts", true, "", false},
		{"read_only_allows_create_kpi", "alerts", true, "create_kpi", true},
		{"read_only_allows_other_write", "alerts", true, "create_alert", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := toolsets.NewFilter(tt.toolsets, tt.readOnly, tt.allowWrite)
			if err != nil {
				t.Fatal(err)
			}
			if !filter.Allows("get_entity_kpis", true) {
				t.Fatal("filter must serve get_entity_kpis")
			}
			cfg := testAlertMutationConfig(server.URL)
			cfg.ToolFilter = filter
			result, _, err := NewGetEntityKPIsHandler(server.Client(), cfg)(
				context.Background(), nil, GetEntityKPIsArgs{EntityID: "entity-1"})
			if err != nil || result.IsError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			text := resultText(result)
			if !strings.HasPrefix(text, `{"entity_id":"entity-1","count":0,"kpis":[]}`) || !strings.Contains(text, "has no KPIs") {
				t.Fatalf("text = %s", text)
			}
			if got := strings.Contains(text, "create_kpi"); got != tt.wantCreate {
				t.Fatalf("names create_kpi = %v, want %v; text = %s", got, tt.wantCreate, text)
			}
			if !tt.wantCreate && !strings.Contains(text, "a session that serves the KPI-creation tool") {
				t.Fatalf("hidden create_kpi must say how to get it; text = %s", text)
			}
		})
	}
}

func TestGetEntityKPIsRequiresEntityID(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	for _, id := range []string{"", "   "} {
		result, _, err := NewGetEntityKPIsHandler(server.Client(), testAlertMutationConfig(server.URL))(
			context.Background(), nil, GetEntityKPIsArgs{EntityID: id})
		if err != nil || !result.IsError {
			t.Fatalf("entity_id=%q result=%+v err=%v", id, result, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("made %d API calls for invalid input", calls.Load())
	}
}

func TestGetEntityKPIsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"entity not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := NewGetEntityKPIsHandler(server.Client(), testAlertMutationConfig(server.URL))(
		context.Background(), nil, GetEntityKPIsArgs{EntityID: "missing"})
	if err == nil {
		t.Fatal("expected API error")
	}
}
