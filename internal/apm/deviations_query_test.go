package apm

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceQueriesUseCanonicalServiceLatencyInMilliseconds(t *testing.T) {
	scope := deviationQueryScope{ServiceName: `checkout"api\\v2`, Env: "prod\nblue", Limit: 7}
	current, baseline := deviationTestWindows()
	plan := buildServiceRollupQueries(scope, current, baseline, time.Minute)
	queries := plan.Current
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
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in queries:\n%s", want, joined)
		}
	}
	latency := queryTextByName(t, queries, "latency_median")
	if !strings.Contains(latency, `trace_service_response_time{`) || !strings.Contains(latency, `quantile="p95"`) || !strings.Contains(latency, "* 1000") {
		t.Fatalf("service latency must use canonical p95 converted to milliseconds: %s", latency)
	}
	if strings.Contains(latency, "trace_endpoint_duration") {
		t.Fatalf("service latency must not aggregate endpoint percentiles: %s", latency)
	}
	for _, forbidden := range []string{
		"zscore_over_time", "outlier_iqr_over_time", "mad_over_time",
		"predict_linear", "4..", `http_status_code=~"4`,
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("forbidden query fragment %q", forbidden)
		}
	}
	if got := len(queries); got != 16 {
		t.Fatalf("query count = %d, want 16 compact rollups", got)
	}
}

func TestSignalDistributionQueriesCombineStatisticsWithoutExtraFanout(t *testing.T) {
	current, baseline := deviationTestWindows()
	queries := buildServiceRollupQueries(deviationQueryScope{ServiceName: "api", Env: "prod", Limit: 3}, current, baseline, time.Minute).Current

	for _, name := range []string{"request_distribution", "error_throughput_distribution", "error_percentage_distribution", "apdex_distribution"} {
		query := queryTextByName(t, queries, name)
		for _, want := range []string{
			"quantile_over_time(0.25", "quantile_over_time(0.5", "quantile_over_time(0.75",
			`label_replace(`, `"deviation_stat"`, `"q25"`, `"median"`, `"q75"`, " or ",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("%s missing %q: %s", name, want, query)
			}
		}
	}

	errorPercentage := queryTextByName(t, queries, "error_percentage_distribution")
	for _, want := range []string{"* 0", "trace_endpoint_count", "> 0", "* 100"} {
		if !strings.Contains(errorPercentage, want) {
			t.Errorf("error percentage distribution is not request aligned and zero-error aware; missing %q: %s", want, errorPercentage)
		}
	}
	apdex := queryTextByName(t, queries, "apdex_distribution")
	if !strings.Contains(apdex, "trace_service_apdex_score") || !strings.Contains(apdex, "and on (service_name, env)") {
		t.Fatalf("Apdex distribution does not use the aligned Apdex population: %s", apdex)
	}
}

func TestServiceApdexUsesAlignedServerRequestPopulation(t *testing.T) {
	scope := deviationQueryScope{ServiceName: "api", Env: "prod", Limit: 3}
	current, baseline := deviationTestWindows()
	queries := buildServiceRollupQueries(scope, current, baseline, time.Minute).Current
	numerator := queryTextByName(t, queries, "apdex_numerator")
	denominator := queryTextByName(t, queries, "apdex_denominator")
	count := queryTextByName(t, queries, "apdex_count")

	for name, query := range map[string]string{"numerator": numerator, "denominator": denominator, "count": count} {
		if regexp.MustCompile(`trace_service_apdex_score\{[^}]*span_kind`).MatchString(query) {
			t.Errorf("%s applies an unverified span_kind label to service Apdex: %s", name, query)
		}
		if !strings.Contains(query, "and on (service_name, env)") {
			t.Errorf("%s is not aligned to the server request population: %s", name, query)
		}
	}
	if !strings.Contains(numerator, " * on (service_name, env) ") {
		t.Fatalf("Apdex numerator is not apdex multiplied by aligned requests: %s", numerator)
	}
	if !strings.Contains(denominator, "trace_endpoint_count") || !strings.Contains(denominator, " and on (service_name, env) ") || denominator == queryTextByName(t, queries, "requests_sum") {
		t.Fatalf("Apdex denominator must include only request buckets with Apdex: %s", denominator)
	}
	if !strings.Contains(count, "count_over_time") || !strings.Contains(count, "trace_endpoint_count") || !strings.Contains(count, "trace_service_apdex_score") {
		t.Fatalf("Apdex count must describe the same aligned population: %s", count)
	}
}

func TestErrorQueriesZeroFillHealthyRequestBuckets(t *testing.T) {
	scope := deviationQueryScope{ServiceName: "api", Env: "prod", Limit: 3}
	current, baseline := deviationTestWindows()
	queries := buildServiceRollupQueries(scope, current, baseline, time.Minute).Current

	for _, name := range []string{"errors_sum", "errors_count"} {
		query := queryTextByName(t, queries, name)
		for _, want := range []string{
			`status_code="STATUS_CODE_ERROR"`,
			`http_status_code=~"5..|429"`,
			`grpc_status_code!~"^(|0|OK)$"`,
			"or on (service_name, env)",
			"* 0",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("%s missing %q: %s", name, want, query)
			}
		}
		if strings.Contains(query, `http_status_code=~"4`) || strings.Contains(query, "4..") {
			t.Errorf("%s includes ordinary HTTP 4xx errors: %s", name, query)
		}
	}
}

func TestSharedCandidateMaskCombinesPinnedWindowsAndIsIdentical(t *testing.T) {
	scope := deviationQueryScope{Limit: 2}
	current, baseline := deviationTestWindows()
	plan := buildServiceRollupQueries(scope, current, baseline, time.Minute)
	mask := plan.CandidateMask
	currentQueries := plan.Current
	baselineQueries := plan.Baseline

	for _, want := range []string{
		"topk(2,",
		"@ " + strconvUnix(current.End),
		"@ " + strconvUnix(baseline.End),
		" or ",
		"service_name, env",
	} {
		if !strings.Contains(mask, want) {
			t.Errorf("candidate mask missing %q: %s", want, mask)
		}
	}
	for _, queries := range [][]deviationQuery{currentQueries, baselineQueries} {
		for _, query := range queries {
			if query.CandidateMask != mask || !strings.Contains(query.Text, mask) {
				t.Errorf("query %q does not use the exact shared mask", query.Name)
			}
			if strings.Count(query.Text, "topk(") != 1 {
				t.Errorf("query %q contains an independent candidate selection: %s", query.Name, query.Text)
			}
		}
	}
	if maskFromQueries(currentQueries) != maskFromQueries(baselineQueries) {
		t.Fatal("current and baseline candidate masks differ, risking false presence changes")
	}
}

func TestOperationQueriesUseServerEndpointMetricsWithoutUnitConversion(t *testing.T) {
	scope := deviationQueryScope{ServiceName: "api", Env: "prod", Limit: 3}
	current, baseline := deviationTestWindows()
	queries := buildOperationRollupQueries(scope, current, baseline, 30*time.Second).Current
	joined := strings.Join(queryTexts(queries), "\n")
	latency := queryTextByName(t, queries, "latency_median")

	for _, want := range []string{
		"by (service_name, env, span_name)",
		"trace_endpoint_duration",
		`quantile="p95"`,
		`span_kind="SPAN_KIND_SERVER"`,
		"trace_endpoint_apdex_score",
		"[3600s:30s]",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in operation queries", want)
		}
	}
	if strings.Contains(latency, "trace_service_response_time") || strings.Contains(latency, "* 1000") {
		t.Fatalf("endpoint duration is canonically milliseconds and must not use service conversion: %s", latency)
	}
	if regexp.MustCompile(`trace_endpoint_apdex_score\{[^}]*span_kind`).MatchString(joined) {
		t.Fatalf("operation Apdex uses an unverified span_kind label: %s", joined)
	}
	if !strings.Contains(queryTextByName(t, queries, "apdex_denominator"), "and on (service_name, env, span_name)") {
		t.Fatal("operation Apdex is not intersected with server-request identities")
	}
}

func TestOperationQueriesRequireNamedService(t *testing.T) {
	current, baseline := deviationTestWindows()
	scope := deviationQueryScope{Env: "prod", Limit: 10}
	if got := buildOperationRollupQueries(scope, current, baseline, time.Minute); len(got.Current) != 0 || len(got.Baseline) != 0 {
		t.Fatalf("operation fleet scan built current=%d baseline=%d queries", len(got.Current), len(got.Baseline))
	}
}

func TestParseDeviationQueryValuesPreservesIdentityFieldAndKind(t *testing.T) {
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
			{Metric: map[string]string{"service_name": "a", "env": "prod", "span_name": "GET /a"}, Value: []any{1.0, "NaN"}},
		},
		"latency_median": {
			{Metric: map[string]string{"service_name": "a", "env": "prod"}, Value: []any{1.0, "12.5"}},
			{Metric: map[string]string{"service_name": "", "env": "prod", "span_name": "GET /missing"}, Value: []any{1.0, "99"}},
			{Metric: map[string]string{"service_name": "b", "env": "prod", "span_name": "GET /b"}, Value: []any{1.0}},
		},
	}

	got, parseErrors := parseDeviationQueryValues(queries, responses)
	if len(got) != 2 || got[0].ServiceName != "a" || got[1].ServiceName != "b" {
		t.Fatalf("records are not deterministic and compact: %+v", got)
	}
	if got[0].RequestTotal == nil || *got[0].RequestTotal != 10 || got[0].LatencyMedian == nil || *got[0].LatencyMedian != 12.5 {
		t.Fatalf("unexpected merged record: %+v", got[0])
	}
	if len(parseErrors) != 3 {
		t.Fatalf("parse errors = %+v, want three identity-aware errors", parseErrors)
	}
	nonFinite := findDeviationError(t, parseErrors, "non_finite_value")
	if nonFinite.ServiceName != "a" || nonFinite.Env != "prod" || nonFinite.SpanName != "GET /a" || nonFinite.Field != string(deviationFieldLatencyQ25) {
		t.Fatalf("non-finite exclusion cannot be mapped to its identity/field: %+v", nonFinite)
	}
	missing := findDeviationError(t, parseErrors, "missing_identity")
	if missing.Env != "prod" || missing.SpanName != "GET /missing" || missing.Field != string(deviationFieldLatencyMedian) {
		t.Fatalf("missing identity evidence was lost: %+v", missing)
	}
	malformed := findDeviationError(t, parseErrors, "malformed_value")
	if malformed.ServiceName != "b" || malformed.SpanName != "GET /b" {
		t.Fatalf("malformed value evidence was lost: %+v", malformed)
	}
}

func TestParseDeviationQueryValuesDecodesCombinedStatisticsDeterministically(t *testing.T) {
	queries := []deviationQuery{
		{Name: "request_distribution", Field: deviationFieldRequestDistribution},
		{Name: "error_percentage_distribution", Field: deviationFieldErrorPercentageDistribution},
	}
	responses := map[string][]deviationVector{
		"request_distribution": {
			{Metric: map[string]string{"service_name": "api", "env": "prod", "deviation_stat": "q75"}, Value: []any{1.0, "12"}},
			{Metric: map[string]string{"service_name": "api", "env": "prod", "deviation_stat": "q25"}, Value: []any{1.0, "8"}},
			{Metric: map[string]string{"service_name": "api", "env": "prod", "deviation_stat": "median"}, Value: []any{1.0, "10"}},
		},
		"error_percentage_distribution": {
			{Metric: map[string]string{"service_name": "api", "env": "prod", "deviation_stat": "median"}, Value: []any{1.0, "1.5"}},
			{Metric: map[string]string{"service_name": "api", "env": "prod", "deviation_stat": "unknown"}, Value: []any{1.0, "9"}},
		},
	}

	records, parseErrors := parseDeviationQueryValues(queries, responses)
	if len(records) != 1 {
		t.Fatalf("records = %+v", records)
	}
	got := records[0]
	if got.RequestQ25 == nil || *got.RequestQ25 != 8 || got.RequestMedian == nil || *got.RequestMedian != 10 || got.RequestQ75 == nil || *got.RequestQ75 != 12 {
		t.Fatalf("request distribution was not decoded: %+v", got)
	}
	if got.ErrorPercentageMedian == nil || *got.ErrorPercentageMedian != 1.5 {
		t.Fatalf("error percentage median missing: %+v", got)
	}
	if len(parseErrors) != 1 || parseErrors[0].Kind != "invalid_statistic" {
		t.Fatalf("unknown statistic was not rejected: %+v", parseErrors)
	}
}

func TestExecuteDeviationQueriesReturnsErrorForMalformedOnlyResponses(t *testing.T) {
	runner := deviationQueryRunnerFunc(func(context.Context, string, time.Time) ([]deviationVector, error) {
		return []deviationVector{{Metric: map[string]string{"service_name": "api", "env": "prod"}, Value: []any{1.0, "NaN"}}}, nil
	})
	queries := []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "requests"}}
	got := executeDeviationQueries(context.Background(), runner, deviationQueryPlan{CurrentEnd: time.Now(), BaselineEnd: time.Now(), Current: queries, Baseline: queries})
	if got.Err == nil || got.Err.Error() != "metric queries returned no valid aggregate values" {
		t.Fatalf("malformed-only error = %v, execution=%+v", got.Err, got)
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
			return nil, errors.New("secret upstream URL unavailable")
		}
		return []deviationVector{{Metric: map[string]string{"service_name": "api", "env": "prod"}, Value: []any{1.0, "5"}}}, nil
	})
	plan := deviationQueryPlan{
		CurrentEnd:  time.Unix(20, 0),
		BaselineEnd: time.Unix(10, 0),
		Current:     []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "current-requests"}},
		Baseline:    []deviationQuery{{Name: "errors_sum", Field: deviationFieldErrorTotal, Text: "baseline-errors"}},
	}

	done := make(chan deviationQueryExecution, 1)
	go func() {
		done <- executeDeviationQueries(context.Background(), runner, plan)
	}()
	first, second := <-started, <-started
	if first == second {
		t.Fatalf("expected both windows to start, got %q twice", first)
	}
	close(release)
	got := <-done
	if len(got.Current.Records) != 1 || len(got.Baseline.Records) != 0 || got.Err != nil {
		t.Fatalf("unexpected partial execution: %+v", got)
	}
	if len(got.Errors) != 1 || got.Errors[0].Window != "baseline" || got.Errors[0].Signal != "errors_sum" || got.Errors[0].Kind != "query_failed" {
		t.Fatalf("partial errors not retained deterministically: %+v", got.Errors)
	}
	if got.Errors[0].Message != "query failed" || strings.Contains(got.Errors[0].Message, "secret") || strings.Contains(got.Errors[0].Message, "baseline-errors") {
		t.Fatalf("query or upstream details leaked in error: %+v", got.Errors[0])
	}
}

func TestExecuteDeviationQueriesReturnsTopLevelErrorWhenAllQueriesFail(t *testing.T) {
	runner := deviationQueryRunnerFunc(func(context.Context, string, time.Time) ([]deviationVector, error) {
		return nil, errors.New("credential and URL details")
	})
	queries := []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "sensitive-query"}}
	got := executeDeviationQueries(context.Background(), runner, deviationQueryPlan{CurrentEnd: time.Now(), BaselineEnd: time.Now(), Current: queries, Baseline: queries})
	if got.Err == nil || got.Err.Error() != "all metric queries failed" {
		t.Fatalf("all-failed error = %v", got.Err)
	}
	if len(got.Errors) != 2 {
		t.Fatalf("per-signal errors = %+v", got.Errors)
	}
}

func TestExecuteDeviationQueriesTreatsEmptyResponseAsSuccess(t *testing.T) {
	runner := deviationQueryRunnerFunc(func(context.Context, string, time.Time) ([]deviationVector, error) {
		return []deviationVector{}, nil
	})
	queries := []deviationQuery{{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: "requests"}}
	got := executeDeviationQueries(context.Background(), runner, deviationQueryPlan{CurrentEnd: time.Now(), BaselineEnd: time.Now(), Current: queries, Baseline: queries})
	if got.Err != nil || len(got.Errors) != 0 || len(got.Current.Records) != 0 || len(got.Baseline.Records) != 0 {
		t.Fatalf("empty successful results were treated as failures: %+v", got)
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
	plan := deviationQueryPlan{CurrentEnd: time.Now(), BaselineEnd: time.Now(), Current: queries, Baseline: queries}
	done := make(chan deviationQueryExecution, 1)
	go func() {
		done <- executeDeviationQueries(ctx, runner, plan)
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

func deviationTestWindows() (TimeWindow, TimeWindow) {
	return TimeWindow{
			Start: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC),
		}, TimeWindow{
			Start: time.Date(2026, 7, 10, 7, 30, 0, 0, time.UTC),
			End:   time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC),
		}
}

func strconvUnix(value time.Time) string {
	return strconv.FormatInt(value.Unix(), 10)
}

func maskFromQueries(queries []deviationQuery) string {
	if len(queries) == 0 {
		return ""
	}
	return queries[0].CandidateMask
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

func findDeviationError(t *testing.T, errors []deviationQueryError, kind string) deviationQueryError {
	t.Helper()
	for _, item := range errors {
		if item.Kind == kind {
			return item
		}
	}
	t.Fatalf("error kind %q not found in %+v", kind, errors)
	return deviationQueryError{}
}
