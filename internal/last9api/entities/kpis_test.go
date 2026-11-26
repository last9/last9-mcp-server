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

func TestCreateEntityKPI(t *testing.T) {
	tests := []struct {
		name, body string
		in         CreateEntityKPIInput
	}{
		{"required fields only", `{"name":"p99","definition":{"query":"up","unit":"count"}}`,
			CreateEntityKPIInput{EntityID: "entity/1", Name: "p99", Query: "up", Unit: "count"}},
		{"with optionals", `{"name":"p99","description":"d","data_source_id":"ds-1","definition":{"query":"up","unit":"count"}}`,
			CreateEntityKPIInput{EntityID: "entity/1", Name: "p99", Description: "d", DataSourceID: "ds-1", Query: "up", Unit: "count"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// The collection route needs its trailing slash; the API does not redirect.
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity%2F1/kpis/" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != tt.body {
					t.Errorf("body = %s", got)
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"kpi-9","name":"p99","definition":{"query":"up","source":"levitate","unit":"count"},"kpi_type":"custom"}`))
			}))
			defer srv.Close()

			kpi, err := CreateEntityKPI(context.Background(), newTestClient(srv), tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if kpi.ID != "kpi-9" || kpi.Definition.Unit != "count" {
				t.Fatalf("kpi = %+v", kpi)
			}
		})
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
