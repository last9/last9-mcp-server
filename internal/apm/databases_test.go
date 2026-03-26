package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testDBConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

func TestGetDatabasesHandler(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)

		// Return different results depending on the query
		response := []map[string]any{
			{
				"metric": map[string]string{"db_system": "postgresql", "net_peer_name": "db-primary.internal"},
				"value":  []any{1700000000, "150.5"},
			},
			{
				"metric": map[string]string{"db_system": "redis", "net_peer_name": "redis-cache.internal"},
				"value":  []any{1700000000, "2500.0"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	handler := NewGetDatabasesHandler(server.Client(), testDBConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	count, ok := response["count"].(float64)
	if !ok || count == 0 {
		t.Fatalf("expected databases in response, got count=%v", response["count"])
	}

	databases, ok := response["databases"].([]any)
	if !ok || len(databases) == 0 {
		t.Fatal("expected databases array in response")
	}

	// Verify first database has expected fields
	db := databases[0].(map[string]any)
	if db["db_system"] == nil || db["db_system"] == "" {
		t.Error("expected db_system field")
	}
	if db["host"] == nil {
		t.Error("expected host field")
	}
	if db["throughput_rpm"] == nil {
		t.Error("expected throughput_rpm field")
	}

	// Should have made at least 4 PromQL requests (throughput, latency, error_count, total_count + service_count)
	if rc := requestCount.Load(); rc < 4 {
		t.Errorf("expected at least 4 PromQL requests, got %d", rc)
	}
}

func TestGetDatabasesHandler_NoDatabases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return empty series
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	handler := NewGetDatabasesHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No databases found") {
		t.Errorf("expected 'No databases found' message, got: %s", text)
	}
}

func TestGetDatabaseSlowQueriesHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
			// Traces API — return trace spans
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"result": []any{
						map[string]any{
							"TraceId":     "abc123",
							"SpanId":      "span-1",
							"ServiceName": "order-service",
							"SpanName":    "SELECT * FROM orders WHERE id = ?",
							"Duration":    float64(500_000_000), // 500ms
							"StatusCode":  "STATUS_CODE_OK",
							"Timestamp":   "2025-01-01T10:00:00Z",
							"SpanAttributes": map[string]any{
								"db.system":    "postgresql",
								"db.statement": "SELECT * FROM orders WHERE id = $1",
							},
						},
					},
				},
			})
		} else if strings.Contains(r.URL.Path, "/logs/api/v2/query_range/json") {
			// Logs API — return slow query logs with enrichment fields
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"resultType": "streams",
					"result": []any{
						map[string]any{
							"stream": map[string]any{
								"service_name": "order-service",
								"severity":     "warn",
							},
							"values": []any{
								[]any{
									"1700000000000000000",
									`{"db.system":"postgresql","db.operation.duration_ms":750,"db.statement":"SELECT * FROM orders WHERE status = 'pending'","db.namespace":"public.orders","db.plan_summary":"IXSCAN status_idx","db.query_hash":"abc123hash","db.docs_examined":1500,"db.keys_examined":1500,"db.rows_affected":42}`,
								},
							},
						},
					},
				},
			})
		}
	}))
	defer server.Close()

	handler := NewGetDatabaseSlowQueriesHandler(server.Client(), testDBConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseSlowQueriesArgs{
		DBSystem:     "postgresql",
		StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	count := int(response["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 slow queries (1 trace + 1 log), got %d", count)
	}

	// Verify source counts
	if response["from_traces"].(float64) != 1 {
		t.Errorf("expected from_traces=1, got %v", response["from_traces"])
	}
	if response["from_logs"].(float64) != 1 {
		t.Errorf("expected from_logs=1, got %v", response["from_logs"])
	}

	queries := response["slow_queries"].([]any)

	// Sorted by duration desc — 750ms log entry should be first
	first := queries[0].(map[string]any)
	if first["source"] != "log" {
		t.Errorf("expected first query source=log (750ms), got %v", first["source"])
	}
	if first["duration_ms"].(float64) != 750 {
		t.Errorf("expected first query duration=750ms, got %v", first["duration_ms"])
	}
	// Verify log-specific enrichment fields
	if first["plan_summary"] != "IXSCAN status_idx" {
		t.Errorf("expected plan_summary, got %v", first["plan_summary"])
	}
	if first["query_hash"] != "abc123hash" {
		t.Errorf("expected query_hash, got %v", first["query_hash"])
	}
	if first["docs_examined"].(float64) != 1500 {
		t.Errorf("expected docs_examined=1500, got %v", first["docs_examined"])
	}

	// Second should be the trace entry (500ms)
	second := queries[1].(map[string]any)
	if second["source"] != "trace" {
		t.Errorf("expected second query source=trace (500ms), got %v", second["source"])
	}
	if second["trace_id"] != "abc123" {
		t.Errorf("expected trace_id=abc123, got %v", second["trace_id"])
	}
}

func TestGetDatabaseSlowQueriesHandler_NoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": []any{}},
		})
	}))
	defer server.Close()

	handler := NewGetDatabaseSlowQueriesHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseSlowQueriesArgs{
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No slow database queries found") {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestGetDatabaseServerMetricsHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if strings.Contains(r.URL.Path, "prom_label_values") {
			// Probe request — return some pg_ metric names
			json.NewEncoder(w).Encode([]string{
				"pg_stat_activity_count",
				"pg_stat_database_blks_hit",
				"pg_settings_max_connections",
			})
		} else {
			// Instant query for metrics — return a single value
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"metric": map[string]string{},
					"value":  []any{1700000000, "42.5"},
				},
			})
		}
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:     "postgresql",
		StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	databases, ok := response["databases"].([]any)
	if !ok || len(databases) == 0 {
		t.Fatal("expected at least one database in response")
	}

	db := databases[0].(map[string]any)
	if db["db_system"] != "postgresql" {
		t.Errorf("expected db_system=postgresql, got %v", db["db_system"])
	}
	if db["available"] != true {
		t.Error("expected postgresql to be available")
	}
	if db["metrics"] == nil {
		t.Error("expected metrics map")
	}

	metrics := db["metrics"].(map[string]any)
	if metrics["active_connections"] == nil {
		t.Error("expected active_connections metric")
	}
}

func TestGetDatabaseServerMetricsHandler_NotAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return empty label values — no metrics found
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	handler := NewGetDatabaseServerMetricsHandler(server.Client(), testDBConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "oracle",
		LookbackMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	databases := response["databases"].([]any)
	db := databases[0].(map[string]any)
	if db["available"] != false {
		t.Error("expected oracle to be unavailable")
	}
}

func TestExtractSlowQueries_TruncatesLongStatements(t *testing.T) {
	longSQL := strings.Repeat("SELECT * FROM very_long_table WHERE ", 20) // >500 chars
	rawResult := map[string]any{
		"data": map[string]any{
			"result": []any{
				map[string]any{
					"TraceId":     "t1",
					"SpanId":      "s1",
					"ServiceName": "svc",
					"SpanName":    "query",
					"Duration":    float64(100_000_000),
					"Timestamp":   "2025-01-01T10:00:00Z",
					"SpanAttributes": map[string]any{
						"db.system":    "mysql",
						"db.statement": longSQL,
					},
				},
			},
		},
	}

	queries := extractSlowQueries(rawResult)
	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}

	if len(queries[0].DBStatement) > 510 { // 500 + "..."
		t.Errorf("expected db_statement to be truncated, got length %d", len(queries[0].DBStatement))
	}
	if !strings.HasSuffix(queries[0].DBStatement, "...") {
		t.Error("expected truncated statement to end with '...'")
	}
}

// --- Integration tests (require TEST_REFRESH_TOKEN) ---

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
	t.Logf("get_databases response (%d bytes): %.500s", len(text), text)
}

func TestGetDatabaseSlowQueriesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetDatabaseSlowQueriesHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseSlowQueriesArgs{
		LookbackMinutes: 60,
		Limit:           5,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("get_database_slow_queries response (%d bytes): %.500s", len(text), text)
}

func TestGetDatabaseQueriesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	// First discover databases to get a db_system to test with
	dbHandler := NewGetDatabasesHandler(http.DefaultClient, *cfg)
	dbResult, _, err := dbHandler(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
		LookbackMinutes: 60,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	dbText := utils.GetTextContent(t, dbResult)
	var dbResp map[string]any
	if err := json.Unmarshal([]byte(dbText), &dbResp); err != nil {
		// Might be a "No databases found" message
		t.Logf("No databases found, skipping queries test: %s", dbText)
		return
	}

	databases, ok := dbResp["databases"].([]any)
	if !ok || len(databases) == 0 {
		t.Log("No databases found, skipping queries test")
		return
	}

	// Use the first database's system
	firstDB := databases[0].(map[string]any)
	dbSystem := firstDB["db_system"].(string)
	t.Logf("Testing query patterns for db_system=%s", dbSystem)

	handler := NewGetDatabaseQueriesHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseQueriesArgs{
		DBSystem:        dbSystem,
		LookbackMinutes: 60,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("get_database_queries response (%d bytes): %.500s", len(text), text)
}

func TestGetDatabaseServerMetricsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetDatabaseServerMetricsHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		LookbackMinutes: 60,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("get_database_server_metrics response (%d bytes): %.500s", len(text), text)
}
