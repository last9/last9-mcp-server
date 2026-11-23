package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var rangeLimitAggregatePipeline = []any{
	map[string]any{"type": "filter", "query": map[string]any{"$and": []any{map[string]any{"$eq": []any{"ServiceName", "example-service"}}}}},
	map[string]any{"type": "aggregate", "aggregates": []any{map[string]any{"function": map[string]any{"$count": []any{}}, "as": "requests"}}},
}

var rangeLimitWindowAggregatePipeline = []any{
	map[string]any{"type": "filter", "query": map[string]any{"$and": []any{map[string]any{"$eq": []any{"ServiceName", "example-service"}}}}},
	map[string]any{"type": "window_aggregate", "function": map[string]any{"$count": []any{}}, "as": "requests", "window": []any{"1", "hours"}},
}

func callRegisteredGetLogs(t *testing.T, cfg models.Config, client *http.Client, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_logs"}, NewGetLogsHandler(client, cfg))
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_logs", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return result
}

func setRangeLimitFakeNow(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, "2026-11-23T04:00:00Z")
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
}

func TestGetLogsAggregateRangeLimitRejection(t *testing.T) {
	cases := []struct {
		name       string
		searchAPI  bool
		pipeline   []any
		args       map[string]any
		body       string
		want       []string
		wantAbsent []string
	}{
		{
			// The search-API path refuses aggregates over 336 hours locally, so
			// only a workspace limit below that reaches this rejection there.
			name:      "aggregate relative range over a 7-day limit",
			searchAPI: true,
			pipeline:  rangeLimitAggregatePipeline,
			args:      map[string]any{"lookback_minutes": 14400},
			body:      `{"error":"requested time range exceeds the duration limit of 168 hours"}`,
			want: []string{
				"The requested 10-day range (2026-11-13T04:00:00Z to 2026-11-23T04:00:00Z) exceeds the reported 7-day limit.",
				"lookback_minutes of 10080 (7 days) or less",
				"start_time_iso and end_time_iso",
				"A shorter range gives a total for that shorter period",
				"confirm with the user",
			},
		},
		{
			name:      "window_aggregate explicit range over a non-default limit",
			searchAPI: false,
			pipeline:  rangeLimitWindowAggregatePipeline,
			args:      map[string]any{"start_time_iso": "2026-11-01T00:00:00Z", "end_time_iso": "2026-11-11T12:00:00Z"},
			body:      `{"error":"time range 252h0m0s exceeds max duration limit: 168h0m0s"}`,
			want: []string{
				"The requested 252-hour range (2026-11-01T00:00:00Z to 2026-11-11T12:00:00Z) exceeds the reported 7-day limit.",
				"lookback_minutes of 10080 (7 days) or less",
			},
		},
		{
			name:      "limit not reported is not invented",
			searchAPI: true,
			pipeline:  rangeLimitAggregatePipeline,
			args:      map[string]any{"lookback_minutes": 14400},
			body:      `{"error":"time range exceeds the maximum allowed duration","trace":"SECRET-DETAIL"}`,
			want: []string{
				"The requested 10-day range (2026-11-13T04:00:00Z to 2026-11-23T04:00:00Z) exceeds the maximum range the log backend accepts; the response gave no limit.",
				"A shorter range gives a total for that shorter period",
			},
			wantAbsent: []string{"SECRET-DETAIL", "maximum allowed duration", "reported"},
		},
		{
			name:      "past-tense rejection wording with a day limit",
			searchAPI: false,
			pipeline:  rangeLimitAggregatePipeline,
			args:      map[string]any{"lookback_minutes": 43200},
			body:      `{"error":"query window exceeded the maximum of 14 days"}`,
			want: []string{
				"The requested 30-day range (2026-10-24T04:00:00Z to 2026-11-23T04:00:00Z) exceeds the reported 14-day limit.",
			},
		},
		{
			name:      "reported limit not below the range gives no shortening advice",
			searchAPI: true,
			pipeline:  rangeLimitAggregatePipeline,
			args:      map[string]any{"lookback_minutes": 14400},
			body:      `{"error":"requested range exceeds the limit of 30 days"}`,
			want: []string{
				"The requested 10-day range (2026-11-13T04:00:00Z to 2026-11-23T04:00:00Z) was rejected as too long, but it is within the reported 30-day limit",
				"Do not retry unchanged or guess a shorter range",
			},
			wantAbsent: []string{"exceeds the reported", "lookback_minutes of", "A shorter range gives"},
		},
		{
			name:       "reported limit equal to the range gives no shortening advice",
			searchAPI:  false,
			pipeline:   rangeLimitAggregatePipeline,
			args:       map[string]any{"lookback_minutes": 20160},
			body:       `{"error":"query window exceeded the maximum of 14 days"}`,
			want:       []string{"was rejected as too long, but it is within the reported 14-day limit"},
			wantAbsent: []string{"exceeds the reported", "lookback_minutes of"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			setRangeLimitFakeNow(t)
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			cfg := testLogsConfig(srv.URL)
			cfg.UseLogSearchAPI = tt.searchAPI

			args := map[string]any{"logjson_query": tt.pipeline}
			for key, value := range tt.args {
				args[key] = value
			}
			result := callRegisteredGetLogs(t, cfg, srv.Client(), args)

			if !result.IsError {
				t.Fatalf("want tool error, got %#v", result.Content)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("error missing %q:\n%s", want, text)
				}
			}
			for _, absent := range append([]string{"Pipeline stage schema", "get_log_attributes_for_pipeline"}, tt.wantAbsent...) {
				if strings.Contains(text, absent) {
					t.Errorf("error contains %q:\n%s", absent, text)
				}
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("upstream requests = %d, want 1 (no retry, shorter or chunked query)", got)
			}
		})
	}
}

func TestGetLogsAggregateUnrelated400KeepsPipelineDiagnostics(t *testing.T) {
	for _, searchAPI := range []bool{true, false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unknown aggregate function $counts; limit exceeded for name 336h"}`))
		}))
		cfg := testLogsConfig(srv.URL)
		cfg.UseLogSearchAPI = searchAPI
		result := callRegisteredGetLogs(t, cfg, srv.Client(), map[string]any{
			"logjson_query": rangeLimitAggregatePipeline, "lookback_minutes": 60,
		})
		srv.Close()

		text := result.Content[0].(*mcp.TextContent).Text
		if !result.IsError || !strings.Contains(text, "unknown aggregate function") || !strings.Contains(text, "Pipeline stage schema") {
			t.Errorf("searchAPI=%t: want existing 400 diagnostics with schema hint, got %s", searchAPI, text)
		}
		if strings.Contains(text, "exceeds the reported") {
			t.Errorf("searchAPI=%t: unrelated 400 got range guidance: %s", searchAPI, text)
		}
	}
}

func TestGetLogsRawSearchRangeLimit400KeepsExistingDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"requested time range exceeds the duration limit of 336 hours"}`))
	}))
	t.Cleanup(srv.Close)
	cfg := testLogsConfig(srv.URL)
	cfg.UseLogSearchAPI = true
	result := callRegisteredGetLogs(t, cfg, srv.Client(), map[string]any{
		"logjson_query": rangeLimitAggregatePipeline[:1], "lookback_minutes": 60,
	})
	text := result.Content[0].(*mcp.TextContent).Text
	if !result.IsError || !strings.Contains(text, "Pipeline stage schema") || strings.Contains(text, "exceeds the reported") {
		t.Errorf("raw search 400 should keep existing diagnostics, got %s", text)
	}
}

func TestGetLogsAggregateInRangeStillSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"query_result":{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1,"42"]]}]}}}`))
	}))
	t.Cleanup(srv.Close)
	cfg := testLogsConfig(srv.URL)
	cfg.UseLogSearchAPI = true
	result := callRegisteredGetLogs(t, cfg, srv.Client(), map[string]any{
		"logjson_query": rangeLimitAggregatePipeline, "lookback_minutes": 60,
	})
	if result.IsError {
		t.Fatalf("in-range aggregate failed: %s", result.Content[0].(*mcp.TextContent).Text)
	}
}
