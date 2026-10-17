package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/models"
)

type promStubSample struct {
	labels map[string]string
	value  string
}

// newPromQueryStub answers instant queries by exact query text; unknown queries
// get an empty vector. It records every query it saw.
func newPromQueryStub(t *testing.T, answers map[string][]promStubSample) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu      sync.Mutex
		queries []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode prom request: %v", err)
		}
		mu.Lock()
		queries = append(queries, req.Query)
		mu.Unlock()
		out := make([]map[string]any, 0)
		for _, s := range answers[req.Query] {
			out = append(out, map[string]any{"metric": s.labels, "value": []any{1.0, s.value}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), queries...)
	}
}

func goldenTestWindow() TimeWindow {
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	return TimeWindow{Start: start, End: start.Add(30 * time.Minute)}
}

func TestQueryGoldenSignalsJoinsCountsAndLatencyPerService(t *testing.T) {
	q := GoldenSignalsQuery{Services: []string{"checkout", "pay.svc"}, Env: "prod", Window: goldenTestWindow()}
	matcher := goldenSignalsMatcher(q.Services, q.Env)
	answers := map[string][]promStubSample{
		serviceSummaryCountQuery(matcher, 30, ""): {
			{map[string]string{"service_name": "checkout", "env": "prod"}, "600"},
			{map[string]string{"service_name": "pay.svc", "env": "prod"}, "0"},
		},
		serviceSummaryCountQuery(matcher, 30, `http_status_code=~"5.*"`): {
			{map[string]string{"service_name": "checkout", "env": "prod"}, "9"},
		},
		serviceSummaryCountQuery(matcher, 30, serviceSummaryGRPCMatcher): {
			{map[string]string{"service_name": "checkout", "env": "prod"}, "3"},
		},
		goldenLatencyQuery(matcher, 30): {
			{map[string]string{"service_name": "checkout", "quantile": "p50"}, "20"},
			{map[string]string{"service_name": "checkout", "quantile": "p95"}, "120"},
		},
	}
	server, _ := newPromQueryStub(t, answers)
	defer server.Close()

	got, err := QueryGoldenSignals(context.Background(), server.Client(), testSummaryConfig(server.URL), q)
	if err != nil {
		t.Fatal(err)
	}
	checkout := got["checkout"]
	if checkout.RequestCount != 600 || checkout.ErrorCount != 12 || checkout.ThroughputRPM != 20 {
		t.Fatalf("checkout counts = %+v", checkout)
	}
	if checkout.ErrorRatePct == nil || *checkout.ErrorRatePct != 2 {
		t.Fatalf("checkout error rate = %v, want 2", checkout.ErrorRatePct)
	}
	if checkout.LatencyP50Ms == nil || *checkout.LatencyP50Ms != 20 || checkout.LatencyP95Ms == nil || *checkout.LatencyP95Ms != 120 {
		t.Fatalf("checkout latency = %+v", checkout)
	}
	if checkout.LatencyP99Ms != nil {
		t.Fatalf("p99 must be nil when the series has no p99 quantile, got %v", *checkout.LatencyP99Ms)
	}
	if pay := got["pay.svc"]; pay.ErrorRatePct != nil {
		t.Fatalf("error rate must be nil with zero requests, got %v", *pay.ErrorRatePct)
	}
}

func TestGoldenSignalsMatcherEscapesServicesAndEnv(t *testing.T) {
	got := goldenSignalsMatcher([]string{"pay.svc", `a"b`}, "prod.eu")
	want := `env=~"prod\\.eu",service_name=~"pay\\.svc|a\"b"`
	if got != want {
		t.Fatalf("matcher = %s, want %s", got, want)
	}
	if all := goldenSignalsMatcher([]string{"checkout"}, ""); all != `env=~".*",service_name=~"checkout"` {
		t.Fatalf("empty env matcher = %s", all)
	}
}

// Quantile gauges cannot be summed: two envs at p95 100 ms and 400 ms are not a
// 500 ms service. Each env is aggregated on its own, and with no env the
// slowest one is reported.
func TestGoldenLatencyQueryNeverSumsQuantilesAcrossEnvs(t *testing.T) {
	got := goldenLatencyQuery(goldenSignalsMatcher([]string{"checkout"}, ""), 30)
	want := `max by (service_name, quantile) (quantile_over_time(0.95, sum by (service_name, env, quantile) (trace_service_response_time{env=~".*",service_name=~"checkout", quantile=~"p50|p95|p99"}[30m])))`
	if got != want {
		t.Fatalf("latency query =\n%s\nwant\n%s", got, want)
	}
}

func TestQueryDirectDependentsRanksCallersByThroughput(t *testing.T) {
	q := DependentsQuery{Service: "checkout", Env: "prod", Window: goldenTestWindow()}
	answers := map[string][]promStubSample{
		incomingCallersQuery(`checkout`, `prod`, 30): {
			{map[string]string{"client": "web"}, "5"},
			{map[string]string{"client": "checkout"}, "50"},
			{map[string]string{"client": ""}, "40"},
			{map[string]string{"client": "mobile"}, "12"},
			{map[string]string{"client": "batch"}, "12"},
		},
	}
	server, queries := newPromQueryStub(t, answers)
	defer server.Close()

	got, err := QueryDirectDependents(context.Background(), server.Client(), testSummaryConfig(server.URL), q)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range got {
		names = append(names, fmt.Sprintf("%s=%g", d.Service, d.ThroughputRPM))
	}
	if strings.Join(names, ",") != "batch=12,mobile=12,web=5" {
		t.Fatalf("dependents = %v (self and unnamed callers excluded, ties by name)", names)
	}
	if qs := queries(); len(qs) != 1 || !strings.Contains(qs[0], `server="checkout"`) {
		t.Fatalf("queries = %v", qs)
	}
}

// Dependents and golden signals must read the same range for one window, so a
// partial minute rounds up in both.
func TestQueryDirectDependentsSizesWindowLikeGoldenSignals(t *testing.T) {
	w := goldenTestWindow()
	w.End = w.End.Add(30 * time.Second)
	server, queries := newPromQueryStub(t, nil)
	defer server.Close()
	cfg := testSummaryConfig(server.URL)
	if _, err := QueryDirectDependents(context.Background(), server.Client(), cfg, DependentsQuery{Service: "checkout", Window: w}); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryGoldenSignals(context.Background(), server.Client(), cfg, GoldenSignalsQuery{Services: []string{"checkout"}, Window: w}); err != nil {
		t.Fatal(err)
	}
	for _, q := range queries() {
		if !strings.Contains(q, "[31m]") {
			t.Errorf("query %s must cover the 30m30s window as 31m", q)
		}
	}
}

func TestQueryDirectDependentsSurfacesUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	_, err := QueryDirectDependents(context.Background(), server.Client(), testSummaryConfig(server.URL),
		DependentsQuery{Service: "checkout", Window: goldenTestWindow()})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want the upstream status named", err)
	}
}

func TestQueryServiceDeviationsReturnsEmptyWithoutData(t *testing.T) {
	server, queries := newPromQueryStub(t, nil)
	defer server.Close()
	w := goldenTestWindow()
	got, err := QueryServiceDeviations(context.Background(), server.Client(), testSummaryConfig(server.URL), DeviationArgs{
		ServiceName:      "checkout",
		StartTimeISO:     w.End.Format(time.RFC3339),
		EndTimeISO:       w.End.Add(30 * time.Minute).Format(time.RFC3339),
		BaselineStartISO: w.Start.Format(time.RFC3339),
		BaselineEndISO:   w.End.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 0 || len(got.FailedQueries) != 0 {
		t.Fatalf("result = %+v, want no entries and no failed queries", got)
	}
	if len(queries()) == 0 {
		t.Fatal("expected the deviation queries to run")
	}
}

func TestQueryServiceDeviationsReportsServiceQueryFailuresOnly(t *testing.T) {
	deps := testDeviationHandlerDeps()
	calls := 0
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		calls++
		if calls == 1 {
			return deviationQueryExecution{
				Current:  deviationQueryResult{Records: []deviationAggregate{aggregate("api", "prod", "", 600, 6, 60, 6, 420, 600, 6, 250, 300, 350, 400, 6)}},
				Baseline: deviationQueryResult{Records: []deviationAggregate{aggregate("api", "prod", "", 600, 6, 6, 6, 570, 600, 6, 80, 100, 120, 130, 6)}},
				Errors: []deviationQueryError{
					{Window: "current", Signal: "apdex_numerator", Kind: deviationQueryErrorKindRejected},
					{Window: "baseline", Signal: "apdex_numerator", Kind: deviationQueryErrorKindRejected},
					{Window: "current", Signal: "apdex_numerator", Kind: deviationQueryErrorKindRejected},
				},
			}
		}
		return deviationQueryExecution{Errors: []deviationQueryError{{Window: "current", Signal: "latency_q75", Kind: "query_failed"}}}
	}
	args := sixMinuteDeviationArgs()
	args.ServiceName, args.Env = "api", "prod"
	got, err := queryServiceDeviations(context.Background(), http.DefaultClient, models.Config{}, deps, args)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("execute calls = %d, want the service and operation passes", calls)
	}
	want := []string{"apdex_numerator (baseline, query_rejected)", "apdex_numerator (current, query_rejected)"}
	if !slices.Equal(got.FailedQueries, want) {
		t.Fatalf("failed queries = %v, want %v", got.FailedQueries, want)
	}
	if len(got.Entries) == 0 {
		t.Fatal("regressions measured despite the failures were dropped")
	}
}
