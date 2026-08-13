package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type summaryPromSample struct {
	service string
	env     string
	value   string
}

type summaryPromFixture struct {
	request []summaryPromSample
	http4xx []summaryPromSample
	http5xx []summaryPromSample
	grpc    []summaryPromSample
	fail5xx bool
}

func TestNewServiceSummaryHandler_RanksByRequestCount(t *testing.T) {
	startTime := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC)
	endTime := startTime.Add(20 * time.Minute)

	queries, result := runServiceSummaryHandler(t, summaryPromFixture{
		request: []summaryPromSample{
			{service: "beta", env: "prod", value: "100"},
			{service: "alpha", env: "prod", value: "50"},
			{service: "alpha", env: "staging", value: "200"},
		},
		http5xx: []summaryPromSample{
			{service: "beta", env: "prod", value: "90"},
		},
	}, ServiceSummaryArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
		Env:          ".*",
	})

	assertServiceSummaryQueryShape(t, queries)
	if result.SortBy != "request_count" {
		t.Fatalf("sort_by = %q, want request_count", result.SortBy)
	}
	if result.SortKeyUnit != "count" {
		t.Fatalf("sort_key_unit = %q, want count", result.SortKeyUnit)
	}
	if result.Limit != 10 {
		t.Fatalf("limit = %d, want 10", result.Limit)
	}
	if result.EnvScope != ".*" {
		t.Fatalf("env_scope = %q, want .*", result.EnvScope)
	}
	if result.StartTime != startTime.Format(time.RFC3339) || result.EndTime != endTime.Format(time.RFC3339) {
		t.Fatalf("interval = %s/%s, want %s/%s", result.StartTime, result.EndTime, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	}
	if result.Coverage != serviceSummaryCoverage {
		t.Fatalf("coverage = %q", result.Coverage)
	}
	if result.QueryFingerprint == "" {
		t.Fatal("query_fingerprint is empty")
	}
	if strings.Contains(result.QueryFingerprint, "alpha") || strings.Contains(result.QueryFingerprint, "prod") {
		t.Fatalf("query_fingerprint leaked label values: %s", result.QueryFingerprint)
	}
	got := summaryRowKeys(result.Rows)
	want := []string{"alpha/staging", "beta/prod", "alpha/prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default sort order = %v, want %v (request_count, not 5xx)", got, want)
	}
	if result.Rows[0].RequestCount != 200 || result.Rows[0].ThroughputRPM != 10 {
		t.Fatalf("alpha/staging counts = %+v, want request_count=200 throughput_rpm=10", result.Rows[0])
	}
	if result.Rows[1].HTTP5xxCount != 90 {
		t.Fatalf("beta/prod http_5xx_count = %v, want 90", result.Rows[1].HTTP5xxCount)
	}
	if result.Rows[1].Env != "prod" {
		t.Fatalf("row env = %q, want series label prod not filter string", result.Rows[1].Env)
	}
}

func TestNewServiceSummaryHandler_HTTP5xxRankingIsStable(t *testing.T) {
	startTime := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC)
	endTime := startTime.Add(20 * time.Minute)
	fx := summaryPromFixture{
		request: []summaryPromSample{
			{service: "alpha", env: "prod", value: "1000"},
			{service: "noisy-client", env: "prod", value: "800"},
			{service: "failing-api", env: "prod", value: "40"},
			{service: "failing-api", env: "staging", value: "40"},
		},
		http4xx: []summaryPromSample{
			{service: "noisy-client", env: "prod", value: "500"},
			{service: "failing-api", env: "prod", value: "1"},
		},
		http5xx: []summaryPromSample{
			{service: "failing-api", env: "prod", value: "30"},
			{service: "failing-api", env: "staging", value: "30"},
			{service: "alpha", env: "prod", value: "2"},
		},
	}
	args := ServiceSummaryArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
		SortBy:       "http_5xx_count",
		Limit:        10,
	}

	_, first := runServiceSummaryHandler(t, fx, args)
	_, second := runServiceSummaryHandler(t, fx, args)

	got := summaryRowKeys(first.Rows)
	want := []string{"failing-api/prod", "failing-api/staging", "alpha/prod", "noisy-client/prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("5xx ranking = %v, want %v", got, want)
	}
	if first.Rows[0].HTTP4xxCount != 1 {
		t.Fatalf("5xx leader http_4xx_count = %v, want 1 (4xx must not promote noisy-client)", first.Rows[0].HTTP4xxCount)
	}
	if summaryRowKeys(second.Rows)[0] != want[0] || !reflect.DeepEqual(summaryRowKeys(second.Rows), got) {
		t.Fatalf("repeated 5xx ranking diverged: first=%v second=%v", got, summaryRowKeys(second.Rows))
	}
	if first.Rows[3].HTTP5xxCount != 0 {
		t.Fatalf("noisy-client http_5xx_count = %v, want 0", first.Rows[3].HTTP5xxCount)
	}
}

func TestNewServiceSummaryHandler_RejectsUnknownSortBy(t *testing.T) {
	handler := NewServiceSummaryHandler(http.DefaultClient, models.Config{
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{SortBy: "errors"})
	if err == nil {
		t.Fatal("expected sort_by=errors to fail")
	}
	if !strings.Contains(err.Error(), "request_count") || !strings.Contains(err.Error(), "http_5xx_count") {
		t.Fatalf("error %q does not list allowed sort_by keys", err)
	}
}

func TestNewServiceSummaryHandler_EmptyRowsStayEmpty(t *testing.T) {
	var queries []string
	result := runServiceSummaryHandlerWithQueries(t, summaryPromFixture{}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
	}, &queries)

	if result.RowCount != 0 || len(result.Rows) != 0 {
		t.Fatalf("rows = %+v, want empty", result.Rows)
	}
	if result.Rows == nil {
		t.Fatal("rows must be [] not null")
	}
	if result.Hint == "" {
		t.Fatal("empty result must include a hint")
	}
	if strings.Contains(strings.ToLower(result.Hint), "unknown") {
		t.Fatalf("hint must not invent placeholder names: %s", result.Hint)
	}
	if len(queries) != 4 {
		t.Fatalf("empty vectors still issue 4 queries, got %d (must not retry a wider window)", len(queries))
	}
	for _, q := range queries {
		if strings.Contains(q, "[40m]") || strings.Contains(q, "[60m]") {
			t.Fatalf("empty result widened the window: %s", q)
		}
	}
}

func TestNewServiceSummaryHandler_FailsWholeCallOnClassError(t *testing.T) {
	startTime := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC)
	endTime := startTime.Add(20 * time.Minute)
	server, queries := newServiceSummaryPromServer(t, summaryPromFixture{
		request: []summaryPromSample{{service: "alpha", env: "prod", value: "10"}},
		fail5xx: true,
	})
	defer server.Close()

	handler := NewServiceSummaryHandler(server.Client(), testSummaryConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
	})
	if err == nil {
		t.Fatal("expected 5xx Prom failure to fail the whole call")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q should surface the non-OK Prom status", err)
	}
	for _, q := range *queries {
		if strings.Contains(q, serviceSummaryGRPCMatcher) {
			t.Fatal("gRPC query must not run after a 5xx Prom failure")
		}
	}
}

func TestNewServiceSummaryHandler_FivexxOnlySeriesKeepsZeroRequestCount(t *testing.T) {
	_, result := runServiceSummaryHandler(t, summaryPromFixture{
		http5xx: []summaryPromSample{{service: "orphan", env: "prod", value: "7"}},
	}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
		SortBy:       "http_5xx_count",
	})
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	if result.Rows[0].Service != "orphan" || result.Rows[0].RequestCount != 0 || result.Rows[0].HTTP5xxCount != 7 {
		t.Fatalf("unexpected 5xx-only row: %+v", result.Rows[0])
	}
	if result.Rows[0].ThroughputRPM != 0 {
		t.Fatalf("throughput_rpm = %v, want 0", result.Rows[0].ThroughputRPM)
	}
}

func TestNewServiceSummaryHandler_LimitClampAndFourxxIncludes429(t *testing.T) {
	request := make([]summaryPromSample, 0, 101)
	for i := 0; i < 101; i++ {
		request = append(request, summaryPromSample{
			service: fmt.Sprintf("svc-%03d", i),
			env:     "prod",
			value:   fmt.Sprintf("%d", 101-i),
		})
	}
	_, omitted := runServiceSummaryHandler(t, summaryPromFixture{request: request}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
	})
	if omitted.Limit != 10 || omitted.RowCount != 10 || !omitted.Truncated {
		t.Fatalf("omit limit: limit=%d row_count=%d truncated=%v, want 10/10/true", omitted.Limit, omitted.RowCount, omitted.Truncated)
	}

	_, clamped := runServiceSummaryHandler(t, summaryPromFixture{request: request}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
		Limit:        101,
	})
	if clamped.Limit != 100 || clamped.RowCount != 100 || !clamped.Truncated {
		t.Fatalf("limit 101: limit=%d row_count=%d truncated=%v, want 100/100/true", clamped.Limit, clamped.RowCount, clamped.Truncated)
	}

	queries, fourxx := runServiceSummaryHandler(t, summaryPromFixture{
		request: []summaryPromSample{{service: "edge", env: "prod", value: "10"}},
		http4xx: []summaryPromSample{{service: "edge", env: "prod", value: "3"}},
	}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
		SortBy:       "http_4xx_count",
	})
	if fourxx.Rows[0].HTTP4xxCount != 3 {
		t.Fatalf("http_4xx_count = %v, want 3", fourxx.Rows[0].HTTP4xxCount)
	}
	var saw4xx, saw5xx bool
	for _, q := range queries {
		if strings.Contains(q, `http_status_code=~"4.*"`) {
			saw4xx = true
			if strings.Contains(q, `http_status_code=~"5.*"`) {
				t.Fatalf("4xx query must not also match 5xx: %s", q)
			}
		}
		if strings.Contains(q, `http_status_code=~"5.*"`) {
			saw5xx = true
			if strings.Contains(q, `http_status_code=~"4.*"`) {
				t.Fatalf("5xx query must not also match 4xx: %s", q)
			}
		}
	}
	if !saw4xx || !saw5xx {
		t.Fatalf("missing class matchers in queries: %v", queries)
	}
}

func TestNewServiceSummaryHandler_RejectsInvalidLimit(t *testing.T) {
	handler := NewServiceSummaryHandler(http.DefaultClient, models.Config{
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{Limit: -1})
	if err == nil {
		t.Fatal("expected limit=-1 to fail")
	}
}

func TestNewServiceSummaryHandler_SkipsEmptyServiceName(t *testing.T) {
	_, result := runServiceSummaryHandler(t, summaryPromFixture{
		request: []summaryPromSample{
			{service: "", env: "prod", value: "99"},
			{service: "kept", env: "prod", value: "1"},
		},
	}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
	})
	if len(result.Rows) != 1 || result.Rows[0].Service != "kept" {
		t.Fatalf("rows = %+v, want only kept", result.Rows)
	}
}

func runServiceSummaryHandler(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs) ([]string, ServiceSummaryResult) {
	t.Helper()
	var queries []string
	result := runServiceSummaryHandlerWithQueries(t, fx, args, &queries)
	return queries, result
}

func runServiceSummaryHandlerWithQueries(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs, queries *[]string) ServiceSummaryResult {
	t.Helper()
	server, recorded := newServiceSummaryPromServer(t, fx)
	defer server.Close()
	handler := NewServiceSummaryHandler(server.Client(), testSummaryConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	*queries = append(*queries, *recorded...)
	text := utils.GetTextContent(t, result)
	var payload ServiceSummaryResult
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v\n%s", err, text)
	}
	if strings.Contains(text, "ErrorRate") {
		t.Fatalf("response still contains ErrorRate: %s", text)
	}
	return payload
}

func newServiceSummaryPromServer(t *testing.T, fx summaryPromFixture) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_query_instant") {
			t.Errorf("Expected request to /prom_query_instant, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode instant request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		query := fmt.Sprintf("%v", reqBody["query"])
		queries = append(queries, query)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(query, `http_status_code=~"5.*"`):
			if fx.fail5xx {
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, `{"error":"prom down"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, promInstantJSON(t, fx.http5xx))
		case strings.Contains(query, `http_status_code=~"4.*"`):
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, promInstantJSON(t, fx.http4xx))
		case strings.Contains(query, serviceSummaryGRPCMatcher):
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, promInstantJSON(t, fx.grpc))
		default:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, promInstantJSON(t, fx.request))
		}
	}))
	return server, &queries
}

func testSummaryConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "us-east-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-access-token-for-testing",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

func promInstantJSON(t *testing.T, samples []summaryPromSample) string {
	t.Helper()
	if samples == nil {
		return "[]"
	}
	type row struct {
		Metric map[string]string `json:"metric"`
		Value  []any             `json:"value"`
	}
	out := make([]row, 0, len(samples))
	for _, sample := range samples {
		metric := map[string]string{}
		if sample.service != "" {
			metric["service_name"] = sample.service
		}
		if sample.env != "" {
			metric["env"] = sample.env
		}
		out = append(out, row{Metric: metric, Value: []any{1687600000, sample.value}})
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal prom fixture: %v", err)
	}
	return string(body)
}

func summaryRowKeys(rows []ServiceSummaryRow) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Service+"/"+row.Env)
	}
	return keys
}

func assertServiceSummaryQueryShape(t *testing.T, queries []string) {
	t.Helper()
	if len(queries) != 4 {
		t.Fatalf("expected 4 instant queries, got %d: %v", len(queries), queries)
	}
	var sawGRPC bool
	for _, q := range queries {
		if !strings.Contains(q, "sum_over_time") {
			t.Fatalf("query missing sum_over_time: %s", q)
		}
		if !strings.Contains(q, "sum by (service_name, env)") {
			t.Fatalf("query missing grouping: %s", q)
		}
		if strings.Contains(q, "topk") {
			t.Fatalf("query must not use topk: %s", q)
		}
		if strings.Contains(q, "quantile_over_time") {
			t.Fatalf("query must not use quantile_over_time: %s", q)
		}
		if !strings.Contains(q, `span_kind="SPAN_KIND_SERVER"`) {
			t.Fatalf("query missing exact server span_kind: %s", q)
		}
		if strings.Contains(q, serviceSummaryGRPCMatcher) {
			sawGRPC = true
		}
	}
	if !sawGRPC {
		t.Fatalf("gRPC matcher missing from queries: %v", queries)
	}
}

func TestGetServicePerformanceDetails(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServicePerformanceDetailsHandler(http.DefaultClient, *cfg)

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestGetServiceOperationsSummary(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceOperationsSummaryHandler(http.DefaultClient, *cfg)

	args := ServiceOperationsSummaryArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServiceOperationsSummaryResponse
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestGetServiceDependencies(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceDependencyGraphHandler(http.DefaultClient, *cfg)

	args := ServiceDependencyGraphArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-60 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServiceDependencyGraphDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

// jsonParam reports whether a struct exposes a JSON property `name`, and
// whether it is optional (has the `omitempty` option).
func jsonParam(rt reflect.Type, name string) (present, optional bool) {
	for i := 0; i < rt.NumField(); i++ {
		parts := strings.Split(rt.Field(i).Tag.Get("json"), ",")
		if parts[0] == name {
			present = true
			for _, p := range parts[1:] {
				if p == "omitempty" {
					optional = true
				}
			}
		}
	}
	return
}

func TestServiceEnvironmentsArgs_UsesServiceName(t *testing.T) {
	rt := reflect.TypeOf(ServiceEnvironmentsArgs{})
	if present, _ := jsonParam(rt, "service_name"); !present {
		t.Fatal("ServiceEnvironmentsArgs must expose canonical param \"service_name\"")
	}
	if p, _ := jsonParam(rt, "service"); p {
		t.Fatal("legacy param \"service\" must be removed")
	}
}

func TestNewServiceEnvironmentsHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceEnvironmentsHandler(http.DefaultClient, *cfg)

	args := ServiceEnvironmentsArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []string
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestPromqlInstantQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlInstantQueryHandler(http.DefaultClient, *cfg)

	args := PromqlInstantQueryArgs{
		Query:   "sum_over_time(trace_call_graph_count{}[1h])",
		TimeISO: time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	_ = utils.GetTextContent(t, result)
}

func TestPromqlRangeQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, *cfg)

	args := PromqlRangeQueryArgs{
		Query:        "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []TimeSeries
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestResolveTimeRange_Precedence(t *testing.T) {
	startISO := "2025-06-23 16:00:00"
	endISO := "2025-06-23 16:30:00"

	start, end, err := resolveTimeRange(startISO, endISO, 5)
	if err != nil {
		t.Fatalf("resolveTimeRange() returned error: %v", err)
	}
	if start != 1750694400 {
		t.Fatalf("start = %d, want %d", start, int64(1750694400))
	}
	if end != 1750696200 {
		t.Fatalf("end = %d, want %d", end, int64(1750696200))
	}

	start, end, err = resolveTimeRange("", endISO, 30)
	if err != nil {
		t.Fatalf("resolveTimeRange() end-only returned error: %v", err)
	}
	if end != 1750696200 {
		t.Fatalf("end-only end = %d, want %d", end, int64(1750696200))
	}
	if start != 1750694400 {
		t.Fatalf("end-only start = %d, want %d", start, int64(1750694400))
	}

	start, end, err = resolveTimeRange(startISO, "", 45)
	if err != nil {
		t.Fatalf("resolveTimeRange() start-only returned error: %v", err)
	}
	if start != 1750694400 {
		t.Fatalf("start-only start = %d, want %d", start, int64(1750694400))
	}
	if end != 1750697100 {
		t.Fatalf("start-only end = %d, want %d", end, int64(1750697100))
	}
}

func TestResolveInstantQueryTime(t *testing.T) {
	timeParam, err := resolveInstantQueryTime("2025-06-23T16:00:00Z", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() returned error: %v", err)
	}
	if timeParam != 1750694400 {
		t.Fatalf("timeParam = %d, want %d", timeParam, int64(1750694400))
	}

	timeParam, err = resolveInstantQueryTime("", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() lookback returned error: %v", err)
	}
	expected := time.Now().UTC().Add(-30 * time.Minute).Unix()
	if timeParam < expected-5 || timeParam > expected+5 {
		t.Fatalf("lookback timeParam = %d, expected near %d", timeParam, expected)
	}
}

func TestPromqlRangeHandler_UsesLookbackAndExplicitPrecedence(t *testing.T) {
	type capturedReq struct {
		Timestamp int64 `json:"timestamp"`
		Window    int64 `json:"window"`
	}

	var captured []capturedReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_query") {
			t.Fatalf("expected prom_query endpoint, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload capturedReq
		_ = json.Unmarshal(body, &reqPayload)
		captured = append(captured, reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for lookback mode: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(captured))
	}
	if captured[0].Window != 1800 {
		t.Fatalf("window = %d, want %d", captured[0].Window, int64(1800))
	}

	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO:    "2025-06-23T16:00:00Z",
		EndTimeISO:      "2025-06-23T16:10:00Z",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for explicit mode: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(captured))
	}
	if captured[1].Window != 600 {
		t.Fatalf("window with explicit timestamps = %d, want %d", captured[1].Window, int64(600))
	}
}

// Integration test for prometheus_labels tool
func TestPromqlLabelsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelsHandler(http.DefaultClient, *cfg)

	args := PromqlLabelsArgs{
		MatchQuery: "up",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labels []string
	if err := json.Unmarshal([]byte(text), &labels); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label(s)", len(labels))
	}
}

func TestNewServiceSummaryHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceSummaryHandler(http.DefaultClient, *cfg)

	args := ServiceSummaryArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          ".*",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var payload ServiceSummaryResult
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d ranked row(s)", payload.RowCount)
	}
}

func TestPromqlLabelValuesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelValuesHandler(http.DefaultClient, *cfg)

	args := PromqlLabelValuesArgs{
		MatchQuery:   "up",
		Label:        "job",
		StartTimeISO: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labelValues []string
	if err := json.Unmarshal([]byte(text), &labelValues); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label value(s) for label '%s'", len(labelValues), args.Label)
	}
}

func TestResolveDatasourceCfg(t *testing.T) {
	cfg := models.Config{
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "prod", ReadURL: "https://prod.example.com/prom", Username: "prod-user", Password: "prod-pass", Region: "us-east-1", ClusterID: "prod-cluster", IsDefault: true},
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "ap-south-1", ClusterID: "staging-cluster"},
		},
	}

	t.Run("empty name returns original cfg unchanged", func(t *testing.T) {
		resolved, err := resolveDatasourceCfg(cfg, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.PrometheusReadURL != cfg.PrometheusReadURL {
			t.Errorf("ReadURL = %q, want %q", resolved.PrometheusReadURL, cfg.PrometheusReadURL)
		}
	})

	t.Run("known datasource overrides prometheus credentials and region", func(t *testing.T) {
		resolved, err := resolveDatasourceCfg(cfg, "staging")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.PrometheusReadURL != "https://staging.example.com/prom" {
			t.Errorf("ReadURL = %q, want staging URL", resolved.PrometheusReadURL)
		}
		if resolved.PrometheusUsername != "staging-user" {
			t.Errorf("Username = %q, want staging-user", resolved.PrometheusUsername)
		}
		if resolved.PrometheusPassword != "staging-pass" {
			t.Errorf("Password = %q, want staging-pass", resolved.PrometheusPassword)
		}
		if resolved.Region != "ap-south-1" {
			t.Errorf("Region = %q, want ap-south-1", resolved.Region)
		}
		if resolved.ClusterID != "staging-cluster" {
			t.Errorf("ClusterID = %q, want staging-cluster", resolved.ClusterID)
		}
	})

	t.Run("unknown datasource returns error containing the name", func(t *testing.T) {
		_, err := resolveDatasourceCfg(cfg, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown datasource, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("error %q does not mention the datasource name", err.Error())
		}
	})

	t.Run("original cfg is not mutated", func(t *testing.T) {
		_, _ = resolveDatasourceCfg(cfg, "staging")
		if cfg.PrometheusReadURL != "https://default.example.com/prom" {
			t.Error("original cfg was mutated by resolveDatasourceCfg")
		}
	})
}

func TestPromqlRangeHandler_DatasourceOverride(t *testing.T) {
	type capturedReq struct {
		ReadURL  string `json:"read_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var (
		captured capturedReq
		hitCount int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "us-east-1"},
		},
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	// No datasource — default credentials should reach the server
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler error (default): %v", err)
	}
	if captured.ReadURL != "https://default.example.com/prom" {
		t.Errorf("default: read_url = %q, want default URL", captured.ReadURL)
	}

	// With datasource override — staging credentials should reach the server
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "staging",
	})
	if err != nil {
		t.Fatalf("handler error (staging): %v", err)
	}
	if captured.ReadURL != "https://staging.example.com/prom" {
		t.Errorf("staging: read_url = %q, want staging URL", captured.ReadURL)
	}
	if captured.Username != "staging-user" {
		t.Errorf("staging: username = %q, want staging-user", captured.Username)
	}
	if captured.Password != "staging-pass" {
		t.Errorf("staging: password = %q, want staging-pass", captured.Password)
	}

	// Unknown datasource — handler must return error before hitting the server
	beforeUnknown := atomic.LoadInt32(&hitCount)
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown datasource, got nil")
	}
	if after := atomic.LoadInt32(&hitCount); after != beforeUnknown {
		t.Fatalf("unknown datasource: server was contacted %d time(s), want 0", after-beforeUnknown)
	}
}

func TestNewListDatasourcesHandler(t *testing.T) {
	t.Run("returns all datasources with correct is_default flags", func(t *testing.T) {
		cfg := models.Config{
			Datasources: []models.DatasourceInfo{
				{Name: "prod", IsDefault: true},
				{Name: "staging", IsDefault: false},
			},
		}

		handler := NewListDatasourcesHandler(cfg)
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDatasourcesArgs{})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		textContent, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("expected TextContent")
		}

		var views []struct {
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		}
		if err := json.Unmarshal([]byte(textContent.Text), &views); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if len(views) != 2 {
			t.Fatalf("expected 2 datasources, got %d", len(views))
		}
		if views[0].Name != "prod" || !views[0].IsDefault {
			t.Errorf("first entry: name=%q is_default=%v, want prod/true", views[0].Name, views[0].IsDefault)
		}
		if views[1].Name != "staging" || views[1].IsDefault {
			t.Errorf("second entry: name=%q is_default=%v, want staging/false", views[1].Name, views[1].IsDefault)
		}
	})

	t.Run("empty datasources list returns empty JSON array", func(t *testing.T) {
		handler := NewListDatasourcesHandler(models.Config{})
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDatasourcesArgs{})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		textContent := result.Content[0].(*mcp.TextContent)
		if textContent.Text != "[]" {
			t.Errorf("empty list text = %q, want []", textContent.Text)
		}
	})
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "up"); got != "up" {
		t.Fatalf("firstNonEmpty(\"\",\"up\") = %q, want \"up\"", got)
	}
	if got := firstNonEmpty("canonical", "alias"); got != "canonical" {
		t.Fatalf("firstNonEmpty canonical-wins failed: got %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty(\"\",\"\") = %q, want \"\"", got)
	}
}

func TestPromqlLabelArgs_AcceptMatchAlias(t *testing.T) {
	for _, rt := range []reflect.Type{
		reflect.TypeOf(PromqlLabelsArgs{}),
		reflect.TypeOf(PromqlLabelValuesArgs{}),
	} {
		if present, _ := jsonParam(rt, "match_query"); !present {
			t.Fatalf("%s must keep canonical \"match_query\"", rt.Name())
		}
		if present, _ := jsonParam(rt, "match"); !present {
			t.Fatalf("%s must accept alias \"match\"", rt.Name())
		}
	}
}

// Verifies the renamed input field (service_name) actually reaches the backend
// label-values match query as service_name="..." — the rename is only correct
// if the handler wires the new field into the query, not just the schema.
func TestServiceEnvironmentsHandler_FilterUsesServiceName(t *testing.T) {
	var captured []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var req struct {
			Matches []string `json:"matches"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Matches) > 0 {
			captured = req.Matches
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `["prod"]`)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewServiceEnvironmentsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceEnvironmentsArgs{
		ServiceName:     "checkout",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(captured) == 0 || !strings.Contains(captured[0], `service_name="checkout"`) {
		t.Fatalf("expected service_name=\"checkout\" in matches, got: %v", captured)
	}
}
