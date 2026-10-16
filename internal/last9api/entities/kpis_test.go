package entities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListEntityKPIsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/entities/entity%2F1/kpis" {
			t.Errorf("path = %s", got)
		}
		_, _ = w.Write([]byte(`[
			{"id":"kpi-1","name":"p99_latency","definition":{"query":"histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))","unit":"seconds"}},
			{"id":"kpi-2","name":"error_rate","definition":{"query":"rate(http_requests_total{status=~\"5..\"}[5m])","unit":"ratio"}}
		]`))
	}))
	defer srv.Close()

	kpis, err := ListEntityKPIs(context.Background(), newTestClient(srv), "entity/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(kpis) != 2 {
		t.Fatalf("len = %d", len(kpis))
	}
	if kpis[0].ID != "kpi-1" || kpis[0].Name != "p99_latency" {
		t.Fatalf("first = %+v", kpis[0])
	}
	if kpis[0].Definition.Query == "" || kpis[0].Definition.Unit != "seconds" {
		t.Fatalf("definition = %+v", kpis[0].Definition)
	}
	if kpis[1].ID != "kpi-2" {
		t.Fatalf("second = %+v", kpis[1])
	}
}

func TestCreateEntityKPIRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		// Trailing slash is required by the collection route; the API does not redirect.
		if got := r.URL.EscapedPath(); got != "/entities/entity%2F1/kpis/" {
			t.Errorf("path = %s", got)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("body: %v", err)
		}
		if got["name"] != "p99_latency" {
			t.Errorf("name = %v", got["name"])
		}
		def, _ := got["definition"].(map[string]any)
		if def["query"] != "up" || def["unit"] != "ratio" {
			t.Errorf("definition = %v", def)
		}
		_, _ = w.Write([]byte(`{"id":"kpi-new","name":"p99_latency","definition":{"query":"up","unit":"ratio"}}`))
	}))
	defer srv.Close()

	kpi, err := CreateEntityKPI(context.Background(), newTestClient(srv), CreateKPIInput{
		EntityID: "entity/1",
		Name:     "p99_latency",
		Query:    "up",
		Unit:     "ratio",
	})
	if err != nil {
		t.Fatal(err)
	}
	if kpi.ID != "kpi-new" || kpi.Name != "p99_latency" {
		t.Fatalf("kpi = %+v", kpi)
	}
}
