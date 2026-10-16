package entities

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListEntityKPIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/entities/entity%2F1/kpis" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`[{"id":"kpi-1","name":"p99_latency","description":"checkout p99","kpi_type":"custom","data_source_id":null,` +
			`"definition":{"query":"histogram_quantile(0.99, sum(rate(x_bucket[5m])) by (le))","source":"levitate","unit":"seconds"}}]`))
	}))
	defer srv.Close()

	kpis, err := ListEntityKPIs(context.Background(), newTestClient(srv), "entity/1")
	if err != nil {
		t.Fatal(err)
	}
	want := EntityKPI{
		ID: "kpi-1", Name: "p99_latency", Description: "checkout p99", KPIType: "custom",
		Definition: KPIDefinition{Query: "histogram_quantile(0.99, sum(rate(x_bucket[5m])) by (le))", Source: "levitate", Unit: "seconds"},
	}
	if len(kpis) != 1 || kpis[0] != want {
		t.Fatalf("kpis = %+v", kpis)
	}
}

func TestListEntityKPIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := ListEntityKPIs(context.Background(), newTestClient(srv), "missing"); err == nil {
		t.Fatal("expected error for 404")
	}
}
