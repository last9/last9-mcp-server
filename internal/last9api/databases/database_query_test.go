package databases

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func testClient(t *testing.T, h http.HandlerFunc) (*last9api.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	cfg := models.Config{
		APIBaseURL: srv.URL,
		Region:     "ap-south-1",
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
	return last9api.NewClient(srv.Client(), cfg), srv.Close
}

func TestDiscoverDatabases_RequestShape(t *testing.T) {
	var body map[string]any
	var path, regionHeader string
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		regionHeader = r.Header.Get("region")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"template":"discover","discover":{"databases":[]}}`))
	})
	defer done()

	_, _, err := DiscoverDatabases(context.Background(), c, DiscoverDatabasesInput{
		TimeRange: TimeRange{From: 1700000000, To: 1700003600},
		Filters:   []Filter{EnvFilter("prod|staging")},
	})
	if err != nil {
		t.Fatalf("DiscoverDatabases: %v", err)
	}
	if path != "/database-query" {
		t.Errorf("path = %q", path)
	}
	if regionHeader != "ap-south-1" {
		t.Errorf("region header = %q", regionHeader)
	}
	if body["template"] != "discover" {
		t.Errorf("template = %v", body["template"])
	}
	if body["cluster_id"] != "test-cluster" {
		t.Errorf("cluster_id = %v", body["cluster_id"])
	}
	tr, _ := body["time_range"].(map[string]any)
	if tr["from"] != float64(1700000000) || tr["to"] != float64(1700003600) {
		t.Errorf("time_range = %v", tr)
	}
	filters, _ := body["filters"].([]any)
	if len(filters) != 1 {
		t.Fatalf("filters = %v", filters)
	}
	f := filters[0].(map[string]any)
	if f["name"] != "deployment_environment" {
		t.Errorf("filter name = %v, want deployment_environment (env is trace-tier only)", f["name"])
	}
	if f["operator"] != "matches" {
		t.Errorf("filter operator = %v, want matches (preserves today's regex semantics)", f["operator"])
	}
	if f["value"] != "prod|staging" {
		t.Errorf("filter value = %v", f["value"])
	}
}

func TestDiscoverDatabases_OmitsEmptyEnvFilter(t *testing.T) {
	var body map[string]any
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"template":"discover","discover":{"databases":[]}}`))
	})
	defer done()

	_, _, err := DiscoverDatabases(context.Background(), c, DiscoverDatabasesInput{
		TimeRange: TimeRange{From: 1700000000, To: 1700003600},
	})
	if err != nil {
		t.Fatalf("DiscoverDatabases: %v", err)
	}
	if _, ok := body["filters"]; ok {
		t.Errorf("filters sent when none were requested: %v", body["filters"])
	}
}

func TestDiscoverDatabases_DecodesMixedRows(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"template":"discover",
			"partial":true,
			"errors":[{"field":"traces/p95_latency","reason":"timeout"}],
			"discover":{"databases":[
				{"id":"postgresql|db-1|traces","db_system":"postgresql","host":"db-1",
				 "sources":["traces"],"metrics_only":false,
				 "throughput":150.5,"p95_latency_ms":12.5,"error_rate":1.5,"service_count":3},
				{"id":"elasticsearch|search-1|cloudwatch","db_system":"elasticsearch","host":"search-1",
				 "sources":["cloudwatch"],"metrics_only":true,
				 "activity":{"value":42,"label":"Search rate","unit":"ops/s"},
				 "activity_source":"cloudwatch",
				 "resolved_labels":{"domain_name":"search-1"}}
			]}
		}`))
	})
	defer done()

	resp, fieldErrs, err := DiscoverDatabases(context.Background(), c, DiscoverDatabasesInput{
		TimeRange: TimeRange{From: 1700000000, To: 1700003600},
	})
	if err != nil {
		t.Fatalf("DiscoverDatabases: %v", err)
	}
	if len(resp.Databases) != 2 {
		t.Fatalf("got %d rows", len(resp.Databases))
	}
	traced := resp.Databases[0]
	if traced.Throughput == nil || *traced.Throughput != 150.5 {
		t.Errorf("throughput = %v", traced.Throughput)
	}
	if traced.ServiceCount == nil || *traced.ServiceCount != 3 {
		t.Errorf("service_count = %v", traced.ServiceCount)
	}
	metricsOnly := resp.Databases[1]
	if !metricsOnly.MetricsOnly {
		t.Error("metrics_only not decoded")
	}
	if metricsOnly.Throughput != nil || metricsOnly.P95LatencyMs != nil {
		t.Error("absent metrics must decode as nil, not zero")
	}
	if metricsOnly.Activity == nil || metricsOnly.Activity.Value != 42 {
		t.Errorf("activity = %v", metricsOnly.Activity)
	}
	if metricsOnly.ResolvedLabels["domain_name"] != "search-1" {
		t.Errorf("resolved_labels = %v", metricsOnly.ResolvedLabels)
	}
	if len(fieldErrs) != 1 || fieldErrs[0].Field != "traces/p95_latency" {
		t.Errorf("field errors = %v", fieldErrs)
	}
}

func TestDiscoverDatabases_RejectsOversizedWindowBeforeRequest(t *testing.T) {
	var called bool
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	defer done()

	from := time.Now().Add(-30 * 24 * time.Hour).Unix()
	_, _, err := DiscoverDatabases(context.Background(), c, DiscoverDatabasesInput{
		TimeRange: TimeRange{From: from, To: time.Now().Unix()},
	})
	if err == nil {
		t.Fatal("want error for a 30 day window")
	}
	if !strings.Contains(err.Error(), "7 day") {
		t.Errorf("error should name the limit: %v", err)
	}
	if called {
		t.Error("request was sent despite an invalid window")
	}
}

func TestDiscoverDatabases_RejectsInvertedRange(t *testing.T) {
	c, done := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer done()

	if _, _, err := DiscoverDatabases(context.Background(), c, DiscoverDatabasesInput{
		TimeRange: TimeRange{From: 1700003600, To: 1700000000},
	}); err == nil {
		t.Fatal("want error when to <= from")
	}
}
