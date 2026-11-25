package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func discoverServer(t *testing.T, payload string, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			_ = json.NewDecoder(r.Body).Decode(capture)
		}
		_, _ = w.Write([]byte(payload))
	}))
}

func TestGetDatabasesHandler_MapsFields(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","discover":{"databases":[
		{"id":"postgresql|db-1|traces","db_system":"postgresql","host":"db-1",
		 "sources":["traces"],"capabilities":["traces","queries"],"metrics_only":false,
		 "throughput":150.5,"p95_latency_ms":12.5,"error_rate":1.5,"service_count":3}
	]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var payload struct {
		Count     int              `json:"count"`
		Databases []map[string]any `json:"databases"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d", payload.Count)
	}
	row := payload.Databases[0]
	if row["db_system"] != "postgresql" || row["host"] != "db-1" {
		t.Errorf("row = %v", row)
	}
	if row["throughput_rpm"] != 150.5 {
		t.Errorf("throughput_rpm = %v (key must not be renamed)", row["throughput_rpm"])
	}
	if row["p95_latency_ms"] != 12.5 {
		t.Errorf("p95_latency_ms = %v", row["p95_latency_ms"])
	}
	if row["error_rate_pct"] != 1.5 {
		t.Errorf("error_rate_pct = %v (key must not be renamed)", row["error_rate_pct"])
	}
	if row["service_count"] != float64(3) {
		t.Errorf("service_count = %v", row["service_count"])
	}
	if row["id"] != "postgresql|db-1|traces" {
		t.Errorf("id = %v", row["id"])
	}
}

func TestGetDatabasesHandler_MetricsOnlyRowOmitsAbsentMetrics(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","discover":{"databases":[
		{"id":"elasticsearch|search-1|cloudwatch","db_system":"elasticsearch","host":"search-1",
		 "sources":["cloudwatch"],"capabilities":["infrastructure"],"metrics_only":true,
		 "activity":{"value":42,"label":"Search rate","unit":"ops/s"},
		 "activity_source":"cloudwatch",
		 "resolved_labels":{"domain_name":"search-1"}}
	]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var payload struct {
		Databases []map[string]any `json:"databases"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	row := payload.Databases[0]
	for _, key := range []string{"throughput_rpm", "p95_latency_ms", "error_rate_pct", "service_count"} {
		if _, present := row[key]; present {
			t.Errorf("%s present on a metrics-only row; absent metrics must be omitted, not zeroed", key)
		}
	}
	if row["metrics_only"] != true {
		t.Errorf("metrics_only = %v", row["metrics_only"])
	}
	labels, _ := row["resolved_labels"].(map[string]any)
	if labels["domain_name"] != "search-1" {
		t.Errorf("resolved_labels = %v", row["resolved_labels"])
	}
}

func TestGetDatabasesHandler_SendsEnvFilter(t *testing.T) {
	var body map[string]any
	srv := discoverServer(t, `{"template":"discover","discover":{"databases":[]}}`, &body)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
		Env:             "production",
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	filters, _ := body["filters"].([]any)
	if len(filters) != 1 {
		t.Fatalf("filters = %v", filters)
	}
	f := filters[0].(map[string]any)
	if f["name"] != "deployment_environment" || f["operator"] != "matches" || f["value"] != "production" {
		t.Errorf("filter = %v", f)
	}

	timeRange, _ := body["time_range"].(map[string]any)
	from, _ := timeRange["from"].(float64)
	to, _ := timeRange["to"].(float64)
	if to-from != 3600 {
		t.Errorf("time_range = %v; want a 3600s (60 minute) window in seconds, got %v", timeRange, to-from)
	}
}

func TestGetDatabasesHandler_PreservesAPIOrder(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","discover":{"databases":[
		{"id":"a","db_system":"redis","host":"r-1","throughput":10},
		{"id":"b","db_system":"postgresql","host":"p-1","throughput":9000}
	]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var payload struct {
		Databases []map[string]any `json:"databases"`
	}
	_ = json.Unmarshal([]byte(text), &payload)
	if payload.Databases[0]["host"] != "r-1" {
		t.Errorf("order changed; the API order must be preserved, got %v first", payload.Databases[0]["host"])
	}
}

func TestGetDatabasesHandler_SurfacesPartialErrors(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","partial":true,
		"errors":[{"field":"traces/p95_latency","reason":"timeout"}],
		"discover":{"databases":[{"id":"a","db_system":"redis","host":"r-1","throughput":10}]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "_warnings") || !strings.Contains(text, "traces/p95_latency") {
		t.Errorf("partial failures not surfaced in _warnings: %s", text)
	}
}

func TestGetDatabasesHandler_SurfacesPartialErrorsWithNoRows(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","partial":true,
		"errors":[{"field":"traces/p95_latency","reason":"timeout"}],
		"discover":{"databases":[]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, "No databases found") {
		t.Errorf("partial failure with zero rows must not be reported as a plain empty result: %s", text)
	}
	if !strings.Contains(text, "_warnings") || !strings.Contains(text, "traces/p95_latency") {
		t.Errorf("partial failures not surfaced in _warnings: %s", text)
	}
}

func TestGetDatabasesHandler_NoDatabases(t *testing.T) {
	srv := discoverServer(t, `{"template":"discover","discover":{"databases":[]}}`, nil)
	defer srv.Close()

	handler := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No databases found") {
		t.Errorf("unexpected empty-result text: %s", text)
	}
}

func TestGetDatabasesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetDatabasesHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	text := utils.GetTextContent(t, result)
	if len(text) > 500 {
		text = text[:500]
	}
	t.Logf("get_databases response: %s", text)
}

func TestGetDatabasesHandler_ExactDBSystem(t *testing.T) {
	const mixed = `{"partial":true,"errors":[{"field":"activity","reason":"timeout"}],"discover":{"databases":[
        {"id":"first","db_system":"redis","metrics_only":true},
        {"id":"other","db_system":"redisearch","metrics_only":true},
        {"id":"third","db_system":"redis","throughput":3}
    ]}}`
	for _, tc := range []struct {
		system string
		ids    []string
	}{
		{"redis", []string{"first", "third"}},
		{"Redis", nil},
		{"", []string{"first", "other", "third"}},
	} {
		t.Run("filter="+tc.system, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					DBSystem string `json:"db_system"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					http.Error(w, "invalid JSON", 400)
					return
				}
				if body.DBSystem != tc.system {
					t.Errorf("db_system=%q, want %q", body.DBSystem, tc.system)
				}
				_, _ = w.Write([]byte(mixed))
			}))
			defer srv.Close()
			result, _, err := NewGetDatabasesHandler(srv.Client(), testDBConfig(srv.URL))(context.Background(), nil, GetDatabasesArgs{DBSystem: tc.system, LookbackMinutes: 60})
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Count     int
				Databases []DatabaseSummary
				Warnings  []string `json:"_warnings"`
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, row := range payload.Databases {
				ids = append(ids, row.ID)
			}
			if !slices.Equal(ids, tc.ids) || payload.Count != len(tc.ids) || !slices.Equal(payload.Warnings, []string{"activity: timeout"}) {
				t.Errorf("unexpected payload %+v", payload)
			}
		})
	}
}
