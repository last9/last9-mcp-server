package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

type deviationQueryScope struct {
	ServiceName string
	Env         string
	Limit       int
}

type deviationField string

const (
	deviationFieldRequestTotal     deviationField = "request_total"
	deviationFieldRequestCount     deviationField = "request_count"
	deviationFieldErrorTotal       deviationField = "error_total"
	deviationFieldErrorCount       deviationField = "error_count"
	deviationFieldApdexNumerator   deviationField = "apdex_numerator"
	deviationFieldApdexDenominator deviationField = "apdex_denominator"
	deviationFieldApdexCount       deviationField = "apdex_count"
	deviationFieldLatencyQ25       deviationField = "latency_q25"
	deviationFieldLatencyMedian    deviationField = "latency_median"
	deviationFieldLatencyQ75       deviationField = "latency_q75"
	deviationFieldLatencyMax       deviationField = "latency_max"
	deviationFieldLatencyCount     deviationField = "latency_count"
)

type deviationQuery struct {
	Name  string
	Field deviationField
	Text  string
}

type deviationVector struct {
	Metric map[string]string
	Value  []any
}

type deviationAggregate struct {
	ServiceName      string
	Env              string
	SpanName         string
	RequestTotal     *float64
	RequestCount     *float64
	ErrorTotal       *float64
	ErrorCount       *float64
	ApdexNumerator   *float64
	ApdexDenominator *float64
	ApdexCount       *float64
	LatencyQ25       *float64
	LatencyMedian    *float64
	LatencyQ75       *float64
	LatencyMax       *float64
	LatencyCount     *float64
}

type deviationQueryResult struct {
	Records []deviationAggregate
}

type deviationQueryError struct {
	Window  string
	Signal  string
	Message string
}

type deviationQueryExecution struct {
	Current  deviationQueryResult
	Baseline deviationQueryResult
	Errors   []deviationQueryError
	Err      error
}

type deviationQueryRunner interface {
	Query(context.Context, string, time.Time) ([]deviationVector, error)
}

type deviationQueryRunnerFunc func(context.Context, string, time.Time) ([]deviationVector, error)

func (fn deviationQueryRunnerFunc) Query(ctx context.Context, query string, end time.Time) ([]deviationVector, error) {
	return fn(ctx, query, end)
}

type httpDeviationQueryRunner struct {
	client *http.Client
	cfg    models.Config
}

func newHTTPDeviationQueryRunner(client *http.Client, cfg models.Config) deviationQueryRunner {
	return httpDeviationQueryRunner{client: client, cfg: cfg}
}

func (runner httpDeviationQueryRunner) Query(ctx context.Context, query string, end time.Time) ([]deviationVector, error) {
	resp, err := utils.MakePromInstantAPIQuery(ctx, runner.client, query, end.Unix(), runner.cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("metric query returned status %d", resp.StatusCode)
	}

	var response apiPromInstantResp
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode metric query response: %w", err)
	}
	vectors := make([]deviationVector, 0, len(response))
	for _, point := range response {
		vectors = append(vectors, deviationVector{Metric: point.Metric, Value: point.Value})
	}
	return vectors, nil
}

func buildServiceRollupQueries(scope deviationQueryScope, window TimeWindow, step time.Duration) []deviationQuery {
	return buildDeviationRollupQueries(scope, window, step, false)
}

func buildOperationRollupQueries(scope deviationQueryScope, window TimeWindow, step time.Duration) []deviationQuery {
	return buildDeviationRollupQueries(scope, window, step, true)
}

func buildDeviationRollupQueries(scope deviationQueryScope, window TimeWindow, step time.Duration, operations bool) []deviationQuery {
	if scope.Limit <= 0 || step <= 0 || !window.End.After(window.Start) {
		return nil
	}
	groupLabels := []string{"service_name", "env"}
	apdexMetric := "trace_service_apdex_score"
	if operations {
		groupLabels = append(groupLabels, "span_name")
		apdexMetric = "trace_endpoint_apdex_score"
	}
	group := strings.Join(groupLabels, ", ")
	rangeSeconds := int64(window.End.Sub(window.Start) / time.Second)
	stepSeconds := int64(step / time.Second)
	if rangeSeconds <= 0 || stepSeconds <= 0 {
		return nil
	}
	grid := fmt.Sprintf("[%ds:%ds]", rangeSeconds, stepSeconds)

	baseMatchers := []string{`span_kind="SPAN_KIND_SERVER"`}
	if scope.ServiceName != "" {
		baseMatchers = append(baseMatchers, fmt.Sprintf(`service_name="%s"`, escapePromQLLabel(scope.ServiceName)))
	}
	if scope.Env != "" {
		baseMatchers = append(baseMatchers, fmt.Sprintf(`env="%s"`, escapePromQLLabel(scope.Env)))
	}
	requestSelector := fmt.Sprintf("trace_endpoint_count{%s}", strings.Join(baseMatchers, ","))
	requestExpression := fmt.Sprintf("sum by (%s) (%s)", group, requestSelector)
	requestGrid := fmt.Sprintf("(%s)%s", requestExpression, grid)
	requestTotal := fmt.Sprintf("sum_over_time(%s)", requestGrid)

	errorSelectors := []string{
		requestSelectorWithMatcher(baseMatchers, `status_code="STATUS_CODE_ERROR"`),
		requestSelectorWithMatcher(baseMatchers, `http_status_code=~"5..|429"`),
		requestSelectorWithMatcher(baseMatchers, `grpc_status_code!~"^(|0|OK)$"`),
	}
	errorExpression := fmt.Sprintf("sum by (%s) ((%s))", group, strings.Join(errorSelectors, ") or ("))
	errorGrid := fmt.Sprintf("(%s)%s", errorExpression, grid)

	latencyMatchers := append(append([]string(nil), baseMatchers...), `quantile="p95"`)
	latencyExpression := fmt.Sprintf("sum by (%s) (trace_endpoint_duration{%s})", group, strings.Join(latencyMatchers, ","))
	latencyGrid := fmt.Sprintf("(%s)%s", latencyExpression, grid)

	apdexSelector := fmt.Sprintf("%s{%s}", apdexMetric, strings.Join(baseMatchers, ","))
	apdexExpression := fmt.Sprintf("sum by (%s) (%s)", group, apdexSelector)
	matching := strings.Join(groupLabels, ", ")
	apdexNumeratorGrid := fmt.Sprintf("(%s * on (%s) %s)%s", apdexExpression, matching, requestExpression, grid)
	apdexGrid := fmt.Sprintf("(%s)%s", apdexExpression, grid)

	candidates := fmt.Sprintf("topk(%d, %s)", scope.Limit, requestTotal)
	limit := func(expression string) string {
		if expression == requestTotal {
			return candidates
		}
		return fmt.Sprintf("(%s) and on (%s) (%s)", expression, matching, candidates)
	}
	return []deviationQuery{
		{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: limit(requestTotal)},
		{Name: "requests_count", Field: deviationFieldRequestCount, Text: limit(fmt.Sprintf("count_over_time(%s)", requestGrid))},
		{Name: "errors_sum", Field: deviationFieldErrorTotal, Text: limit(fmt.Sprintf("sum_over_time(%s)", errorGrid))},
		{Name: "errors_count", Field: deviationFieldErrorCount, Text: limit(fmt.Sprintf("count_over_time(%s)", errorGrid))},
		{Name: "apdex_numerator", Field: deviationFieldApdexNumerator, Text: limit(fmt.Sprintf("sum_over_time(%s)", apdexNumeratorGrid))},
		{Name: "apdex_denominator", Field: deviationFieldApdexDenominator, Text: limit(requestTotal)},
		{Name: "apdex_count", Field: deviationFieldApdexCount, Text: limit(fmt.Sprintf("count_over_time(%s)", apdexGrid))},
		{Name: "latency_q25", Field: deviationFieldLatencyQ25, Text: limit(fmt.Sprintf("quantile_over_time(0.25, %s)", latencyGrid))},
		{Name: "latency_median", Field: deviationFieldLatencyMedian, Text: limit(fmt.Sprintf("quantile_over_time(0.5, %s)", latencyGrid))},
		{Name: "latency_q75", Field: deviationFieldLatencyQ75, Text: limit(fmt.Sprintf("quantile_over_time(0.75, %s)", latencyGrid))},
		{Name: "latency_max", Field: deviationFieldLatencyMax, Text: limit(fmt.Sprintf("max_over_time(%s)", latencyGrid))},
		{Name: "latency_count", Field: deviationFieldLatencyCount, Text: limit(fmt.Sprintf("count_over_time(%s)", latencyGrid))},
	}
}

func requestSelectorWithMatcher(base []string, matcher string) string {
	matchers := append(append([]string(nil), base...), matcher)
	return fmt.Sprintf("trace_endpoint_count{%s}", strings.Join(matchers, ","))
}

func executeDeviationQueries(
	ctx context.Context,
	runner deviationQueryRunner,
	currentEnd time.Time,
	currentQueries []deviationQuery,
	baselineEnd time.Time,
	baselineQueries []deviationQuery,
) deviationQueryExecution {
	type windowResult struct {
		window string
		result deviationQueryResult
		errors []deviationQueryError
	}
	results := make(chan windowResult, 2)
	run := func(window string, end time.Time, queries []deviationQuery) {
		result, queryErrors := executeDeviationQueryGroup(ctx, runner, window, end, queries)
		results <- windowResult{window: window, result: result, errors: queryErrors}
	}
	go run("current", currentEnd, currentQueries)
	go run("baseline", baselineEnd, baselineQueries)

	execution := deviationQueryExecution{}
	for range 2 {
		result := <-results
		if result.window == "current" {
			execution.Current = result.result
		} else {
			execution.Baseline = result.result
		}
		execution.Errors = append(execution.Errors, result.errors...)
	}
	sort.Slice(execution.Errors, func(i, j int) bool {
		if execution.Errors[i].Window != execution.Errors[j].Window {
			return execution.Errors[i].Window < execution.Errors[j].Window
		}
		return execution.Errors[i].Signal < execution.Errors[j].Signal
	})
	if err := ctx.Err(); err != nil {
		execution.Err = err
	}
	return execution
}

func executeDeviationQueryGroup(ctx context.Context, runner deviationQueryRunner, window string, end time.Time, queries []deviationQuery) (deviationQueryResult, []deviationQueryError) {
	type queryResult struct {
		query   deviationQuery
		vectors []deviationVector
		err     error
	}
	results := make(chan queryResult, len(queries))
	for _, query := range queries {
		query := query
		go func() {
			vectors, err := runner.Query(ctx, query.Text, end)
			results <- queryResult{query: query, vectors: vectors, err: err}
		}()
	}

	responses := make(map[string][]deviationVector, len(queries))
	queryErrors := make([]deviationQueryError, 0)
	for range queries {
		result := <-results
		if result.err != nil {
			queryErrors = append(queryErrors, deviationQueryError{Window: window, Signal: result.query.Name, Message: "query failed"})
			continue
		}
		responses[result.query.Name] = result.vectors
	}
	records, parseErrors := parseDeviationQueryValues(queries, responses)
	for _, parseError := range parseErrors {
		parseError.Window = window
		queryErrors = append(queryErrors, parseError)
	}
	return deviationQueryResult{Records: records}, queryErrors
}

func parseDeviationQueryValues(queries []deviationQuery, responses map[string][]deviationVector) ([]deviationAggregate, []deviationQueryError) {
	records := make(map[string]*deviationAggregate)
	parseErrors := make([]deviationQueryError, 0)
	for _, query := range queries {
		for _, vector := range responses[query.Name] {
			serviceName := vector.Metric["service_name"]
			env := vector.Metric["env"]
			spanName := vector.Metric["span_name"]
			value, err := finitePromValue(vector.Value)
			if err != nil || serviceName == "" {
				parseErrors = append(parseErrors, deviationQueryError{Signal: query.Name, Message: "invalid aggregate result"})
				continue
			}
			key := serviceName + "\x00" + env + "\x00" + spanName
			record := records[key]
			if record == nil {
				record = &deviationAggregate{ServiceName: serviceName, Env: env, SpanName: spanName}
				records[key] = record
			}
			setDeviationField(record, query.Field, value)
		}
	}

	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output := make([]deviationAggregate, 0, len(keys))
	for _, key := range keys {
		output = append(output, *records[key])
	}
	sort.Slice(parseErrors, func(i, j int) bool { return parseErrors[i].Signal < parseErrors[j].Signal })
	return output, parseErrors
}

func finitePromValue(value []any) (float64, error) {
	if len(value) != 2 {
		return 0, fmt.Errorf("expected timestamp and value")
	}
	var parsed float64
	var err error
	switch item := value[1].(type) {
	case string:
		parsed, err = strconv.ParseFloat(item, 64)
	case float64:
		parsed = item
	default:
		err = fmt.Errorf("unsupported value type")
	}
	if err != nil || !isFinite(parsed) {
		return 0, fmt.Errorf("invalid finite value")
	}
	return parsed, nil
}

func setDeviationField(record *deviationAggregate, field deviationField, value float64) {
	pointer := func() *float64 { result := value; return &result }
	switch field {
	case deviationFieldRequestTotal:
		record.RequestTotal = pointer()
	case deviationFieldRequestCount:
		record.RequestCount = pointer()
	case deviationFieldErrorTotal:
		record.ErrorTotal = pointer()
	case deviationFieldErrorCount:
		record.ErrorCount = pointer()
	case deviationFieldApdexNumerator:
		record.ApdexNumerator = pointer()
	case deviationFieldApdexDenominator:
		record.ApdexDenominator = pointer()
	case deviationFieldApdexCount:
		record.ApdexCount = pointer()
	case deviationFieldLatencyQ25:
		record.LatencyQ25 = pointer()
	case deviationFieldLatencyMedian:
		record.LatencyMedian = pointer()
	case deviationFieldLatencyQ75:
		record.LatencyQ75 = pointer()
	case deviationFieldLatencyMax:
		record.LatencyMax = pointer()
	case deviationFieldLatencyCount:
		record.LatencyCount = pointer()
	}
}
