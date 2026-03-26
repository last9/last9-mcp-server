package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

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
	// Mock server that returns PromQL responses for throughput, latency, error rate, service count
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		// All requests go to /prom_query_instant
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
	if requestCount < 4 {
		t.Errorf("expected at least 4 PromQL requests, got %d", requestCount)
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
		// Verify it hits the traces query endpoint
		if !strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
			t.Errorf("expected traces API path, got %s", r.URL.Path)
		}

		// Verify order=Duration for slow query sorting
		// Note: the current traces API uses order=Timestamp by default,
		// but results are sorted by duration in the handler
		w.WriteHeader(http.StatusOK)
		response := map[string]any{
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"TraceId":     "abc123",
						"SpanId":      "span-1",
						"ServiceName": "order-service",
						"SpanName":    "SELECT * FROM orders WHERE id = ?",
						"Duration":    float64(500_000_000), // 500ms in nanoseconds
						"StatusCode":  "STATUS_CODE_OK",
						"Timestamp":   "2025-01-01T10:00:00Z",
						"SpanAttributes": map[string]any{
							"db.system":    "postgresql",
							"db.statement": "SELECT * FROM orders WHERE id = $1",
						},
					},
					map[string]any{
						"TraceId":     "def456",
						"SpanId":      "span-2",
						"ServiceName": "user-service",
						"SpanName":    "db.query",
						"Duration":    float64(200_000_000), // 200ms
						"StatusCode":  "STATUS_CODE_OK",
						"Timestamp":   "2025-01-01T10:01:00Z",
						"SpanAttributes": map[string]any{
							"db.system": "mongodb",
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(response)
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

	count, ok := response["count"].(float64)
	if !ok || count != 2 {
		t.Fatalf("expected 2 slow queries, got %v", response["count"])
	}

	queries := response["slow_queries"].([]any)
	first := queries[0].(map[string]any)

	// Sorted by duration desc — 500ms should be first
	if first["duration_ms"].(float64) != 500 {
		t.Errorf("expected first query duration=500ms, got %v", first["duration_ms"])
	}
	if first["trace_id"] != "abc123" {
		t.Errorf("expected trace_id=abc123, got %v", first["trace_id"])
	}
	if first["db_system"] != "postgresql" {
		t.Errorf("expected db_system=postgresql, got %v", first["db_system"])
	}
	if first["db_statement"] != "SELECT * FROM orders WHERE id = $1" {
		t.Errorf("expected db_statement, got %v", first["db_statement"])
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
