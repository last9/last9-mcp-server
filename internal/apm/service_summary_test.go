package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/deeplink"
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
	if result.WindowMinutes != 20 {
		t.Fatalf("window_minutes = %d, want 20", result.WindowMinutes)
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

func TestNewServiceSummaryHandler_RanksByGRPCErrorCount(t *testing.T) {
	_, result := runServiceSummaryHandler(t, summaryPromFixture{
		request: []summaryPromSample{
			{service: "http-api", env: "prod", value: "100"},
			{service: "grpc-api", env: "prod", value: "20"},
		},
		grpc: []summaryPromSample{
			{service: "grpc-api", env: "prod", value: "9"},
		},
	}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
		SortBy:       "grpc_error_count",
	})
	got := summaryRowKeys(result.Rows)
	want := []string{"grpc-api/prod", "http-api/prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grpc ranking = %v, want %v", got, want)
	}
	if result.Rows[0].GRPCErrorCount != 9 || result.Rows[1].GRPCErrorCount != 0 {
		t.Fatalf("grpc counts = %+v", result.Rows)
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
	if !strings.Contains(err.Error(), "http_5xx_count") {
		t.Fatalf("error %q should name the failing class key", err)
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

func TestNewServiceSummaryHandler_LimitClampAndRanks(t *testing.T) {
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
	if omitted.Limit != 10 || omitted.RowCount != 10 || omitted.MatchedCount != 101 || !omitted.Truncated {
		t.Fatalf("omit limit: limit=%d row_count=%d matched_count=%d truncated=%v, want 10/10/101/true", omitted.Limit, omitted.RowCount, omitted.MatchedCount, omitted.Truncated)
	}
	assertConsecutiveRanks(t, omitted.Rows)

	_, clamped := runServiceSummaryHandler(t, summaryPromFixture{request: request}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
		Limit:        101,
	})
	if clamped.Limit != 100 || clamped.RowCount != 100 || clamped.MatchedCount != 101 || !clamped.Truncated {
		t.Fatalf("limit 101: limit=%d row_count=%d matched_count=%d truncated=%v, want 100/100/101/true", clamped.Limit, clamped.RowCount, clamped.MatchedCount, clamped.Truncated)
	}
	assertConsecutiveRanks(t, clamped.Rows)
}

func TestNewServiceSummaryHandler_FourxxMatcherExcludes5xx(t *testing.T) {
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

func TestNewServiceSummaryHandler_RejectsInvalidEnvRegex(t *testing.T) {
	handler := NewServiceSummaryHandler(http.DefaultClient, models.Config{
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{
		LookbackMinutes: 15,
		Env:             "[",
	})
	if err == nil {
		t.Fatal("expected invalid env regex to fail")
	}
	if !strings.Contains(err.Error(), "env") || !strings.Contains(err.Error(), "regular expression") {
		t.Fatalf("error %q should mention env regex", err)
	}
}

func TestNewServiceSummaryHandler_SkipsNonFiniteSamples(t *testing.T) {
	_, result := runServiceSummaryHandler(t, summaryPromFixture{
		request: []summaryPromSample{
			{service: "bad", env: "prod", value: "NaN"},
			{service: "good", env: "prod", value: "42"},
			{service: "inf", env: "prod", value: "+Inf"},
		},
	}, ServiceSummaryArgs{
		StartTimeISO: time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339),
		EndTimeISO:   time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339),
	})
	if len(result.Rows) != 1 || result.Rows[0].Service != "good" || result.Rows[0].RequestCount != 42 {
		t.Fatalf("rows = %+v, want only finite good/prod=42", result.Rows)
	}
}

func TestMergeServiceSummarySeries_AccumulatesDuplicates(t *testing.T) {
	joined := map[string]*ServiceSummaryRow{}
	series := apiPromInstantResp{
		{Metric: map[string]string{"service_name": "svc", "env": "prod"}, Value: []any{0.0, "10"}},
		{Metric: map[string]string{"service_name": "svc", "env": "prod"}, Value: []any{0.0, "3"}},
	}
	if err := mergeServiceSummarySeries(joined, series, func(r *ServiceSummaryRow, v float64) { r.RequestCount += v }); err != nil {
		t.Fatal(err)
	}
	if got := joined["svc\x00prod"].RequestCount; got != 13 {
		t.Fatalf("request_count = %v, want accumulated 13", got)
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

func TestNewServiceSummaryHandler_EnvMatcherQuoting(t *testing.T) {
	start := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Format(time.RFC3339)
	end := time.Date(2026, 8, 13, 11, 40, 0, 0, time.UTC).Format(time.RFC3339)
	fx := summaryPromFixture{request: []summaryPromSample{{service: "svc", env: "prod", value: "1"}}}

	cases := []struct {
		env  string
		want string
	}{
		{env: "", want: `env=~".*"`},
		{env: ".*", want: `env=~".*"`},
		{env: "prod", want: `env=~"prod"`},
		{env: "^prod$", want: `env=~"^prod$"`},
		{env: `foo"bar`, want: `env=~"foo\"bar"`},
		{env: `foo\bar`, want: `env=~"foo\\bar"`},
	}
	for _, tc := range cases {
		queries, _ := runServiceSummaryHandler(t, fx, ServiceSummaryArgs{
			StartTimeISO: start,
			EndTimeISO:   end,
			Env:          tc.env,
		})
		for _, q := range queries {
			if !strings.Contains(q, tc.want) {
				t.Fatalf("env %q: query %s missing %s", tc.env, q, tc.want)
			}
			if strings.Contains(q, `env=~'`) {
				t.Fatalf("env %q used single-quoted matcher: %s", tc.env, q)
			}
		}
	}
}

func TestAPMCatalogEnvFromRegexViaSummary(t *testing.T) {
	cases := map[string]string{
		"":             "",
		".*":           "",
		"prod":         "",
		"^prod$":       "prod",
		"prod|staging": "",
		"^prod.*$":     "",
	}
	for in, want := range cases {
		if got := deeplink.APMCatalogEnvFromRegex(in); got != want {
			t.Errorf("APMCatalogEnvFromRegex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIntervalMinutesRoundsUp(t *testing.T) {
	end := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC).Unix()
	cases := []struct {
		name  string
		start int64
		want  int
	}{
		{name: "exact minutes", start: end - 20*60, want: 20},
		{name: "zero length", start: end, want: 1},
		{name: "ninety seconds", start: end - 90, want: 2},
		{name: "one second", start: end - 1, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intervalMinutes(tc.start, end); got != tc.want {
				t.Fatalf("intervalMinutes(%d,%d) = %d, want %d", tc.start, end, got, tc.want)
			}
		})
	}
}

func TestNewServiceSummaryHandler_StampsQueriedWindow(t *testing.T) {
	endTime := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC)
	startTime := endTime.Add(-90 * time.Second)
	fx := summaryPromFixture{request: []summaryPromSample{{service: "svc", env: "prod", value: "30"}}}

	queries, result := runServiceSummaryHandler(t, fx, ServiceSummaryArgs{
		StartTimeISO: startTime.Format(time.RFC3339),
		EndTimeISO:   endTime.Format(time.RFC3339),
	})
	if result.WindowMinutes != 2 {
		t.Fatalf("window_minutes = %d, want 2", result.WindowMinutes)
	}
	wantStart := endTime.Add(-2 * time.Minute).Format(time.RFC3339)
	if result.StartTime != wantStart || result.EndTime != endTime.Format(time.RFC3339) {
		t.Fatalf("stamped interval = %s/%s, want %s/%s", result.StartTime, result.EndTime, wantStart, endTime.Format(time.RFC3339))
	}
	if result.Rows[0].ThroughputRPM != 15 {
		t.Fatalf("throughput_rpm = %v, want 30/2=15", result.Rows[0].ThroughputRPM)
	}
	for _, q := range queries {
		if !strings.Contains(q, "[2m]") {
			t.Fatalf("expected [2m] range in query, got %s", q)
		}
	}
}

func TestNewServiceSummaryHandler_DeeplinkOmitsRegexEnv(t *testing.T) {
	start := time.Date(2026, 8, 13, 11, 20, 0, 0, time.UTC)
	end := start.Add(20 * time.Minute)
	fx := summaryPromFixture{request: []summaryPromSample{{service: "svc", env: "prod", value: "1"}}}

	link := runServiceSummaryMetaURL(t, fx, ServiceSummaryArgs{
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
		Env:          "^prod$",
	})
	if !strings.Contains(link, "deployment_environment=") || !strings.Contains(link, "prod") {
		t.Fatalf("anchored env should become a literal catalog filter: %s", link)
	}
	if strings.Contains(link, "^prod$") {
		t.Fatalf("deeplink leaked Prom regex anchors: %s", link)
	}

	wildcard := runServiceSummaryMetaURL(t, fx, ServiceSummaryArgs{
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
		Env:          ".*",
	})
	if strings.Contains(wildcard, "deployment_environment") {
		t.Fatalf("wildcard env must not set a catalog filter: %s", wildcard)
	}

	pattern := runServiceSummaryMetaURL(t, fx, ServiceSummaryArgs{
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
		Env:          "prod|staging",
	})
	if strings.Contains(pattern, "deployment_environment") {
		t.Fatalf("regex env must not be passed as a literal catalog filter: %s", pattern)
	}

	unanchored := runServiceSummaryMetaURL(t, fx, ServiceSummaryArgs{
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
		Env:          "prod",
	})
	if strings.Contains(unanchored, "deployment_environment") {
		t.Fatalf("unanchored env must not become an exact catalog filter: %s", unanchored)
	}
}

func TestNewServiceSummaryHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceSummaryHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceSummaryArgs{
		LookbackMinutes: 15,
		SortBy:          "http_5xx_count",
		Limit:           10,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	if strings.Contains(text, "ErrorRate") {
		t.Fatalf("live response still contains ErrorRate: %s", text)
	}
	var payload ServiceSummaryResult
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("live response is not the ranking envelope: %v\n%s", err, text)
	}
	if payload.SortBy != "http_5xx_count" || payload.Limit != 10 {
		t.Fatalf("live envelope sort_by=%q limit=%d", payload.SortBy, payload.Limit)
	}
	if payload.Coverage == "" || payload.QueryFingerprint == "" {
		t.Fatal("live envelope missing coverage or query_fingerprint")
	}
	assertConsecutiveRanks(t, payload.Rows)
	for _, row := range payload.Rows {
		if row.Service == "" {
			t.Fatalf("live row missing service: %+v", row)
		}
		if row.RequestCount > 0 && row.GRPCErrorCount == row.RequestCount && row.HTTP5xxCount == 0 && row.HTTP4xxCount == 0 {
			t.Fatalf("grpc_error_count equals request_count with no HTTP errors; missing-label !~ may be inflating: %+v", row)
		}
	}
	t.Logf("live ranking returned %d row(s) env_scope=%s truncated=%v", payload.RowCount, payload.EnvScope, payload.Truncated)
}

func runServiceSummaryHandler(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs) ([]string, ServiceSummaryResult) {
	t.Helper()
	var queries []string
	result := runServiceSummaryHandlerWithQueries(t, fx, args, &queries)
	return queries, result
}

func runServiceSummaryHandlerWithQueries(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs, queries *[]string) ServiceSummaryResult {
	t.Helper()
	_, payload := runServiceSummaryCall(t, fx, args, queries)
	return payload
}

func runServiceSummaryMetaURL(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs) string {
	t.Helper()
	var queries []string
	result, _ := runServiceSummaryCall(t, fx, args, &queries)
	if result.Meta == nil {
		t.Fatal("expected deeplink meta")
	}
	raw, _ := result.Meta["reference_url"].(string)
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func runServiceSummaryCall(t *testing.T, fx summaryPromFixture, args ServiceSummaryArgs, queries *[]string) (*mcp.CallToolResult, ServiceSummaryResult) {
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
	return result, payload
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
		case strings.Contains(query, `grpc_status_code!=""`):
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
		OrgSlug:    "acme",
		ClusterID:  "cluster-1",
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

func assertConsecutiveRanks(t *testing.T, rows []ServiceSummaryRow) {
	t.Helper()
	for i, row := range rows {
		if row.Rank != i+1 {
			t.Fatalf("rows[%d].Rank = %d, want %d", i, row.Rank, i+1)
		}
	}
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
