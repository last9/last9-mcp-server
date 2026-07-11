package apm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceQueriesUseStandardRollups(t *testing.T) {
	scope := deviationQueryScope{ServiceName: `checkout"api\\v2`, Env: "prod\nblue", Limit: 7}
	window := TimeWindow{
		Start: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC),
	}

	queries := buildServiceRollupQueries(scope, window, time.Minute)
	joined := strings.Join(queryTexts(queries), "\n")
	for _, want := range []string{
		`span_kind="SPAN_KIND_SERVER"`,
		`service_name="checkout\"api\\\\v2"`,
		`env="prod\nblue"`,
		"by (service_name, env)",
		"[3600s:60s]",
		"quantile_over_time(0.25",
		"quantile_over_time(0.5",
		"quantile_over_time(0.75",
		"max_over_time",
		"sum_over_time",
		"count_over_time",
		"topk(7,",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in queries:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{
		"zscore_over_time", "outlier_iqr_over_time", "mad_over_time",
		"predict_linear", "4..", `http_status_code=~"4`,
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("forbidden query fragment %q", forbidden)
		}
	}

	for _, want := range []string{
		`status_code="STATUS_CODE_ERROR"`,
		`http_status_code=~"5..|429"`,
		`grpc_status_code!~"^(|0|OK)$"`,
	} {
		if !strings.Contains(queryTextByName(t, queries, "errors_sum"), want) {
			t.Errorf("error query missing %q", want)
		}
	}
	if got := len(queries); got != 12 {
		t.Fatalf("query count = %d, want 12 compact rollups", got)
	}
	for _, query := range queries {
		if query.Name == "requests_sum" || query.Name == "apdex_denominator" {
			continue
		}
		if !strings.Contains(query.Text, "and on (service_name, env)") {
			t.Errorf("query %q does not use the shared request-volume cap: %s", query.Name, query.Text)
		}
	}
}

func TestOperationQueriesPreserveSpanNameAndUseEndpointApdex(t *testing.T) {
	scope := deviationQueryScope{ServiceName: "api", Env: "prod", Limit: 3}
	window := TimeWindow{Start: time.Unix(0, 0), End: time.Unix(1800, 0)}
	joined := strings.Join(queryTexts(buildOperationRollupQueries(scope, window, 30*time.Second)), "\n")

	for _, want := range []string{
		"by (service_name, env, span_name)",
		"trace_endpoint_apdex_score",
		"[1800s:30s]",
		"topk(3,",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in operation queries", want)
		}
	}
	if strings.Contains(joined, "trace_service_apdex_score") {
		t.Fatal("operation queries must not use service-level Apdex")
	}
}

func TestParseDeviationQueryValuesMergesCompactFiniteRecords(t *testing.T) {
	queries := []deviationQuery{
		{Name: "requests_sum", Field: deviationFieldRequestTotal},
		{Name: "latency_q25", Field: deviationFieldLatencyQ25},
		{Name: "latency_median", Field: deviationFieldLatencyMedian},
	}
	responses := map[string][]deviationVector{
		"requests_sum": {
			{Metric: map[string]string{"service_name": "b", "env": "prod"}, Value: []any{1.0, "20"}},
			{Metric: map[string]string{"service_name": "a", "env": "prod"}, Value: []any{1.0, "10"}},
		},
		"latency_q25": {
			{Metric: map[string]string{"service_name": "a", "env": "prod"}, Value: []any{1.0, "NaN"}},
		},
		"latency_median": {
			{Metric: map[string]string{"service_name": "a", "env": "prod"}, Value: []any{1.0, "12.5"}},
			{Metric: map[string]string{"service_name": "", "env": "prod"}, Value: []any{1.0, "99"}},
			{Metric: map[string]string{"service_name": "b", "env": "prod"}, Value: []any{1.0}},
		},
	}

	got, parseErrors := parseDeviationQueryValues(queries, responses)
	if len(got) != 2 || got[0].ServiceName != "a" || got[1].ServiceName != "b" {
		t.Fatalf("records are not deterministic and compact: %+v", got)
	}
	if got[0].RequestTotal == nil || *got[0].RequestTotal != 10 || got[0].LatencyMedian == nil || *got[0].LatencyMedian != 12.5 {
		t.Fatalf("unexpected merged record: %+v", got[0])
	}
	if got[0].LatencyQ25 != nil {
		t.Fatalf("non-finite value was retained: %+v", got[0])
	}
	if len(parseErrors) != 3 {
		t.Fatalf("parse errors = %+v, want three malformed/non-finite records", parseErrors)
	}
}

func TestExecuteDeviationQueriesRunsWindowsConcurrentlyAndRetainsPartialErrors(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 2)
	runner := deviationQueryRunnerFunc(func(ctx context.Context, query string, _ time.Time) ([]deviationVector, error) {
		started <- query
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if query == "baseline-errors" {
			return nil, errors.New("upstream unavailable")
		}
		return []deviationVector{{Metric: map[string]string{"service_name": "api", "env": "prod"}, Value: []any{1.0, "5"}}}, nil
	})
	current := []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "current-requests"}}
	baseline := []deviationQuery{{Name: "errors_sum", Field: deviationFieldErrorTotal, Text: "baseline-errors"}}

	done := make(chan deviationQueryExecution, 1)
	go func() {
		done <- executeDeviationQueries(context.Background(), runner, time.Unix(20, 0), current, time.Unix(10, 0), baseline)
	}()
	first, second := <-started, <-started
	if first == second {
		t.Fatalf("expected both windows to start, got %q twice", first)
	}
	close(release)
	got := <-done
	if len(got.Current.Records) != 1 || len(got.Baseline.Records) != 0 {
		t.Fatalf("unexpected partial execution: %+v", got)
	}
	if len(got.Errors) != 1 || got.Errors[0].Window != "baseline" || got.Errors[0].Signal != "errors_sum" {
		t.Fatalf("partial errors not retained deterministically: %+v", got.Errors)
	}
	if strings.Contains(got.Errors[0].Message, "baseline-errors") {
		t.Fatalf("query text leaked in error: %+v", got.Errors[0])
	}
}

func TestExecuteDeviationQueriesStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	var once sync.Once
	runner := deviationQueryRunnerFunc(func(ctx context.Context, _ string, _ time.Time) ([]deviationVector, error) {
		once.Do(func() { started <- struct{}{} })
		<-ctx.Done()
		return nil, ctx.Err()
	})
	queries := []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "requests"}}
	done := make(chan deviationQueryExecution, 1)
	go func() {
		done <- executeDeviationQueries(ctx, runner, time.Now(), queries, time.Now(), queries)
	}()
	<-started
	cancel()

	select {
	case got := <-done:
		if !errors.Is(got.Err, context.Canceled) {
			t.Fatalf("execution error = %v, want context cancellation", got.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("execution did not stop after cancellation")
	}
}

func queryTexts(queries []deviationQuery) []string {
	texts := make([]string, 0, len(queries))
	for _, query := range queries {
		texts = append(texts, query.Text)
	}
	return texts
}

func queryTextByName(t *testing.T, queries []deviationQuery, name string) string {
	t.Helper()
	for _, query := range queries {
		if query.Name == name {
			return query.Text
		}
	}
	t.Fatalf("query %q not found", name)
	return ""
}
