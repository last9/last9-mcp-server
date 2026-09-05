package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
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
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
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

// latencyUnitMultiplierRE matches a "* 1000" multiplier applied to a query,
// tolerating whitespace and a trailing decimal (e.g. "*1000", "* 1000.0").
// The trailing \b prevents matching larger literals like "* 10000".
var latencyUnitMultiplierRE = regexp.MustCompile(`\*\s*1000\b`)

// TestDatabaseLatencyQueries_NoUnitMultiplier is a regression test for a unit
// bug: the latency PromQL queries multiplied trace_client_duration by 1000, but
// that metric is already in milliseconds (confirmed against apm.go's identical
// trace_client_duration quantile queries, which apply no multiplier). The bug
// inflated p95_latency_ms / avg_latency_ms by 1000x on the real backend (e.g. a
// real 30s redis block showed as 30,010,484ms instead of ~30,010ms).
//
// The multiplication happens in the PromQL expression evaluated by the metrics
// backend, not in Go arithmetic — a mock HTTP server can't evaluate PromQL, so
// the only way to catch this is asserting on the query text sent. The bug lived
// in 3 queries across both DB handlers, so both are covered here.
func TestDatabaseLatencyQueries_NoUnitMultiplier(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		run  func(client *http.Client, cfg models.Config) error
	}{
		{
			name: "get_databases",
			run: func(client *http.Client, cfg models.Config) error {
				_, _, err := NewGetDatabasesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, GetDatabasesArgs{
					StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
					EndTimeISO:   now.Format(time.RFC3339),
				})
				return err
			},
		},
		{
			name: "get_database_queries",
			run: func(client *http.Client, cfg models.Config) error {
				_, _, err := NewGetDatabaseQueriesHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, GetDatabaseQueriesArgs{
					DBSystem:     "redis",
					StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
					EndTimeISO:   now.Format(time.RFC3339),
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu              sync.Mutex
				capturedQueries []string
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
					return
				}
				if err := json.Unmarshal(bodyBytes, &body); err != nil {
					t.Errorf("failed to unmarshal request body %q: %v", bodyBytes, err)
					return
				}

				mu.Lock()
				capturedQueries = append(capturedQueries, body.Query)
				mu.Unlock()

				w.WriteHeader(http.StatusOK)
				// Include labels for both handlers' grouping (db_system/net_peer_name
				// for get_databases, span_name for get_database_queries) so each
				// issues its latency queries.
				response := []map[string]any{
					{
						"metric": map[string]string{"db_system": "redis", "net_peer_name": "redis-cache.internal", "span_name": "GET"},
						"value":  []any{1700000000, "10.0"},
					},
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			if err := tt.run(server.Client(), testDBConfig(server.URL)); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			found := false
			for _, q := range capturedQueries {
				if !strings.Contains(q, "trace_client_duration") {
					continue
				}
				found = true
				if latencyUnitMultiplierRE.MatchString(q) {
					t.Errorf("latency query multiplies by 1000, but trace_client_duration is already in ms: %s", q)
				}
			}
			if !found {
				t.Fatal("expected a trace_client_duration query to be issued")
			}
		})
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

func TestBuildDatabaseSlowQueryTracePipeline_UsesBracketNotation(t *testing.T) {
	t.Run("with db_system host env service and min duration", func(t *testing.T) {
		pipeline, err := buildDatabaseSlowQueryTracePipeline(GetDatabaseSlowQueriesArgs{
			DBSystem:      "postgresql",
			Host:          "db.example.com",
			ServiceName:   "checkout",
			Env:           "prod",
			MinDurationMs: 100,
		})
		if err != nil {
			t.Fatalf("buildDatabaseSlowQueryTracePipeline() error = %v", err)
		}

		rawQuery, err := json.Marshal(pipeline[0]["query"])
		if err != nil {
			t.Fatalf("failed to marshal pipeline query: %v", err)
		}
		query := string(rawQuery)

		for _, want := range []string{
			`attributes['db.system']`,
			`attributes['net.peer.name']`,
			`resources['deployment.environment']`,
			"postgresql",
			"db.example.com",
			"checkout",
			"prod",
			"100000000",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("expected pipeline to include %q, got %s", want, query)
			}
		}

		for _, bad := range []string{
			"attributes.db.system",
			"attributes.net.peer.name",
			"resource.attributes.deployment.environment",
		} {
			if strings.Contains(query, bad) {
				t.Fatalf("pipeline should not include legacy dot notation %q, got %s", bad, query)
			}
		}
	})

	t.Run("without db_system matches any span with db.system set", func(t *testing.T) {
		pipeline, err := buildDatabaseSlowQueryTracePipeline(GetDatabaseSlowQueriesArgs{
			LookbackMinutes: 30,
		})
		if err != nil {
			t.Fatalf("buildDatabaseSlowQueryTracePipeline() error = %v", err)
		}

		rawQuery, err := json.Marshal(pipeline[0]["query"])
		if err != nil {
			t.Fatalf("failed to marshal pipeline query: %v", err)
		}
		query := string(rawQuery)

		if !strings.Contains(query, `{"$neq":["attributes['db.system']",""]}`) &&
			!strings.Contains(query, `"$neq":["attributes['db.system']",""]`) {
			t.Fatalf("expected $neq on attributes['db.system'], got %s", query)
		}
		if strings.Contains(query, "attributes.db.system") {
			t.Fatalf("pipeline should not include legacy dot notation, got %s", query)
		}
	})
}

func TestGetDatabaseSlowQueriesHandler_TracePipelineUsesBracketNotation(t *testing.T) {
	// Assertions run on the server goroutine: Fatalf would Goexit there, and a
	// request that never arrives would pass the test vacuously.
	var mu sync.Mutex
	reached := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
			mu.Lock()
			reached = true
			mu.Unlock()

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("failed to read request body: %v", err)
				return
			}

			var req struct {
				Pipeline []struct {
					Query map[string]any `json:"query"`
				} `json:"pipeline"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("failed to unmarshal request body: %v", err)
				return
			}
			if len(req.Pipeline) != 1 {
				t.Errorf("expected exactly one pipeline stage, got %d", len(req.Pipeline))
				return
			}

			rawQuery, err := json.Marshal(req.Pipeline[0].Query)
			if err != nil {
				t.Errorf("failed to marshal stage query: %v", err)
				return
			}
			query := string(rawQuery)

			if !strings.Contains(query, `attributes['db.system']`) {
				t.Errorf("expected attributes['db.system'] in pipeline, got %s", query)
			}
			if !strings.Contains(query, `attributes['net.peer.name']`) {
				t.Errorf("expected attributes['net.peer.name'] in pipeline, got %s", query)
			}
			if !strings.Contains(query, `resources['deployment.environment']`) {
				t.Errorf("expected resources['deployment.environment'] in pipeline, got %s", query)
			}
			for _, bad := range []string{
				"attributes.db.system",
				"attributes.net.peer.name",
				"resource.attributes.deployment.environment",
			} {
				if strings.Contains(query, bad) {
					t.Errorf("pipeline should not include legacy dot notation %q, got %s", bad, query)
				}
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"result": []any{}},
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": []any{}},
		})
	}))
	defer server.Close()

	handler := NewGetDatabaseSlowQueriesHandler(server.Client(), testDBConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseSlowQueriesArgs{
		DBSystem:        "postgresql",
		Host:            "db.example.com",
		Env:             "prod",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !reached {
		t.Fatal("traces query_range endpoint was never called — pipeline assertions never ran")
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

// TestGetDatabaseSlowQueriesHandler_MinDurationMs_FiltersLogs is a regression
// test for the logs path ignoring the user-supplied min_duration_ms filter.
// The traces source applies MinDurationMs server-side ($gte on Duration), but
// fetchSlowQueryLogs previously never threaded MinDurationMs into
// extractSlowQueryLogs, so a sub-threshold log entry could fill a result slot
// whenever traces returned fewer than `limit` entries. This test wires one
// valid 500ms trace and one sub-threshold 50ms log (MinDurationMs=200) and
// asserts the log entry is filtered out (from_logs=0, count=1). A reached
// flag guards against a vacuous pass if the logs endpoint is never hit.
func TestGetDatabaseSlowQueriesHandler_MinDurationMs_FiltersLogs(t *testing.T) {
	var mu sync.Mutex
	logEndpointReached := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
			// One valid 500ms trace (>= MinDurationMs=200).
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"result": []any{
						map[string]any{
							"TraceId":     "trace-valid",
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
			return
		}
		if strings.Contains(r.URL.Path, "/logs/api/v2/query_range/json") {
			mu.Lock()
			logEndpointReached = true
			mu.Unlock()

			// One sub-threshold 50ms log (< MinDurationMs=200).
			w.WriteHeader(http.StatusOK)
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
									`{"db.system":"postgresql","db.operation.duration_ms":50,"db.statement":"SELECT 1"}`,
								},
							},
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": []any{}},
		})
	}))
	defer server.Close()

	handler := NewGetDatabaseSlowQueriesHandler(server.Client(), testDBConfig(server.URL))
	now := time.Now().UTC()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseSlowQueriesArgs{
		DBSystem:      "postgresql",
		MinDurationMs: 200,
		StartTimeISO:  now.Add(-60 * time.Minute).Format(time.RFC3339),
		EndTimeISO:    now.Format(time.RFC3339),
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !logEndpointReached {
		t.Fatal("logs query_range endpoint was never called — log filtering not exercised")
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var response map[string]any
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got := response["from_logs"].(float64); got != 0 {
		t.Errorf("expected from_logs=0 (50ms entry below min_duration_ms=200 should be filtered), got %v", got)
	}
	if got := response["count"].(float64); got != 1 {
		t.Errorf("expected 1 slow query (only the valid 500ms trace), got %v", got)
	}
}

// TestExtractSlowQueryLogs_MinDurationMs exercises the client-side threshold
// filter in extractSlowQueryLogs directly, covering below/at/above threshold,
// zero duration, and unset (<=0) threshold behavior.
func TestExtractSlowQueryLogs_MinDurationMs(t *testing.T) {
	mkMsg := func(d float64) string {
		b, _ := json.Marshal(map[string]any{
			"db.system":                "postgresql",
			"db.operation.duration_ms": d,
			"db.statement":             "SELECT 1",
		})
		return string(b)
	}
	buildRaw := func(durations ...float64) map[string]any {
		values := make([]any, 0, len(durations))
		for _, d := range durations {
			values = append(values, []any{"1700000000000000000", mkMsg(d)})
		}
		return map[string]any{
			"data": map[string]any{
				"resultType": "streams",
				"result": []any{
					map[string]any{
						"stream": map[string]any{"service_name": "svc"},
						"values": values,
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		durations     []float64
		minDurationMs float64
		wantCount     int
	}{
		{name: "threshold filters below", durations: []float64{50, 500}, minDurationMs: 200, wantCount: 1},
		{name: "boundary at threshold kept", durations: []float64{200}, minDurationMs: 200, wantCount: 1},
		{name: "zero duration always dropped", durations: []float64{0, 1000}, minDurationMs: 200, wantCount: 1},
		{name: "unset threshold keeps all positive", durations: []float64{50, 200, 500}, minDurationMs: 0, wantCount: 3},
		{name: "negative threshold treated as unset", durations: []float64{1}, minDurationMs: -1, wantCount: 1},
		{name: "all below threshold filtered", durations: []float64{50, 100}, minDurationMs: 200, wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := extractSlowQueryLogs(buildRaw(tt.durations...), tt.minDurationMs)
			if len(queries) != tt.wantCount {
				t.Fatalf("expected %d queries, got %d", tt.wantCount, len(queries))
			}
			for _, q := range queries {
				if q.DurationMs <= 0 {
					t.Errorf("zero-duration entry should not be returned")
				}
				if tt.minDurationMs > 0 && q.DurationMs < tt.minDurationMs {
					t.Errorf("entry duration_ms=%v below threshold %v was not filtered", q.DurationMs, tt.minDurationMs)
				}
			}
		})
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

func TestGetDatabaseServerMetricsHandler_UnknownDBSystem(t *testing.T) {
	handler := NewGetDatabaseServerMetricsHandler(nil, testDBConfig("http://unused"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{
		DBSystem:        "cassandra",
		LookbackMinutes: 60,
	})
	if err == nil {
		t.Fatal("expected error for unsupported db_system, got nil")
	}
	if !strings.Contains(err.Error(), "unknown db_system") {
		t.Errorf("expected 'unknown db_system' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cassandra") {
		t.Errorf("expected db_system name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), supportedDBSystems) {
		t.Errorf("expected full supported list %q in error, got: %v", supportedDBSystems, err)
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
