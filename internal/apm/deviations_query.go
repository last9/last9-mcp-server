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
	deviationFieldRequestTotal                deviationField = "request_total"
	deviationFieldRequestCount                deviationField = "request_count"
	deviationFieldErrorTotal                  deviationField = "error_total"
	deviationFieldErrorCount                  deviationField = "error_count"
	deviationFieldApdexNumerator              deviationField = "apdex_numerator"
	deviationFieldApdexDenominator            deviationField = "apdex_denominator"
	deviationFieldApdexCount                  deviationField = "apdex_count"
	deviationFieldRequestDistribution         deviationField = "request_distribution"
	deviationFieldErrorThroughputDistribution deviationField = "error_throughput_distribution"
	deviationFieldErrorPercentageDistribution deviationField = "error_percentage_distribution"
	deviationFieldApdexDistribution           deviationField = "apdex_distribution"
	deviationFieldLatencyQ25                  deviationField = "latency_q25"
	deviationFieldLatencyMedian               deviationField = "latency_median"
	deviationFieldLatencyQ75                  deviationField = "latency_q75"
	deviationFieldLatencyMax                  deviationField = "latency_max"
	deviationFieldLatencyCount                deviationField = "latency_count"
)

type deviationQuery struct {
	Name          string
	Field         deviationField
	Text          string
	CandidateMask string
}

type deviationQueryPlan struct {
	CandidateMask string
	CurrentEnd    time.Time
	BaselineEnd   time.Time
	Current       []deviationQuery
	Baseline      []deviationQuery
}

type deviationVector struct {
	Metric map[string]string
	Value  []any
}

type deviationAggregate struct {
	ServiceName           string
	Env                   string
	SpanName              string
	RequestTotal          *float64
	RequestCount          *float64
	ErrorTotal            *float64
	ErrorCount            *float64
	ApdexNumerator        *float64
	ApdexDenominator      *float64
	ApdexCount            *float64
	RequestQ25            *float64
	RequestMedian         *float64
	RequestQ75            *float64
	ErrorThroughputQ25    *float64
	ErrorThroughputMedian *float64
	ErrorThroughputQ75    *float64
	ErrorPercentageQ25    *float64
	ErrorPercentageMedian *float64
	ErrorPercentageQ75    *float64
	ApdexQ25              *float64
	ApdexMedian           *float64
	ApdexQ75              *float64
	LatencyQ25            *float64
	LatencyMedian         *float64
	LatencyQ75            *float64
	LatencyMax            *float64
	LatencyCount          *float64
}

type deviationQueryResult struct {
	Records []deviationAggregate
}

type deviationQueryError struct {
	Window      string
	Signal      string
	Kind        string
	Field       string
	ServiceName string
	Env         string
	SpanName    string
	Message     string
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

func buildServiceRollupQueries(scope deviationQueryScope, current, baseline TimeWindow, step time.Duration) deviationQueryPlan {
	return buildDeviationRollupQueryPlan(scope, current, baseline, step, false)
}

func buildOperationRollupQueries(scope deviationQueryScope, current, baseline TimeWindow, step time.Duration) deviationQueryPlan {
	if scope.ServiceName == "" {
		return deviationQueryPlan{}
	}
	return buildDeviationRollupQueryPlan(scope, current, baseline, step, true)
}

func buildDeviationRollupQueryPlan(scope deviationQueryScope, current, baseline TimeWindow, step time.Duration, operations bool) deviationQueryPlan {
	if scope.Limit <= 0 || step <= 0 || !validDeviationWindow(current) || !validDeviationWindow(baseline) {
		return deviationQueryPlan{}
	}
	mask := buildDeviationCandidateMask(scope, current, baseline, step, operations)
	if mask == "" {
		return deviationQueryPlan{}
	}
	return deviationQueryPlan{
		CandidateMask: mask,
		CurrentEnd:    current.End,
		BaselineEnd:   baseline.End,
		Current:       buildDeviationWindowQueries(scope, current, step, operations, mask),
		Baseline:      buildDeviationWindowQueries(scope, baseline, step, operations, mask),
	}
}

func buildDeviationCandidateMask(scope deviationQueryScope, current, baseline TimeWindow, step time.Duration, operations bool) string {
	if scope.Limit <= 0 || step <= 0 || !validDeviationWindow(current) || !validDeviationWindow(baseline) || (operations && scope.ServiceName == "") {
		return ""
	}
	groupLabels := deviationGroupLabels(operations)
	group := strings.Join(groupLabels, ", ")
	requestExpression := deviationRequestExpression(scope, group)
	currentTotal := fmt.Sprintf("sum_over_time(%s)", deviationSubquery(requestExpression, current, step))
	baselineTotal := fmt.Sprintf("sum_over_time(%s)", deviationSubquery(requestExpression, baseline, step))
	combined := fmt.Sprintf("((%s + %s) or %s or %s)", currentTotal, baselineTotal, currentTotal, baselineTotal)
	return fmt.Sprintf("topk(%d, %s)", scope.Limit, combined)
}

func buildDeviationWindowQueries(scope deviationQueryScope, window TimeWindow, step time.Duration, operations bool, candidateMask string) []deviationQuery {
	groupLabels := []string{"service_name", "env"}
	apdexMetric := "trace_service_apdex_score"
	if operations {
		groupLabels = append(groupLabels, "span_name")
		apdexMetric = "trace_endpoint_apdex_score"
	}
	group := strings.Join(groupLabels, ", ")
	baseMatchers := []string{`span_kind="SPAN_KIND_SERVER"`}
	if scope.ServiceName != "" {
		baseMatchers = append(baseMatchers, fmt.Sprintf(`service_name="%s"`, escapePromQLLabel(scope.ServiceName)))
	}
	if scope.Env != "" {
		baseMatchers = append(baseMatchers, fmt.Sprintf(`env="%s"`, escapePromQLLabel(scope.Env)))
	}
	requestSelector := fmt.Sprintf("trace_endpoint_count{%s}", strings.Join(baseMatchers, ","))
	requestExpression := fmt.Sprintf("sum by (%s) (%s)", group, requestSelector)
	requestGrid := deviationSubquery(requestExpression, window, step)
	requestTotal := fmt.Sprintf("sum_over_time(%s)", requestGrid)

	errorSelectors := []string{
		requestSelectorWithMatcher(baseMatchers, `status_code="STATUS_CODE_ERROR"`),
		requestSelectorWithMatcher(baseMatchers, `http_status_code=~"5..|429"`),
		requestSelectorWithMatcher(baseMatchers, `grpc_status_code!~"^(|0|OK)$"`),
	}
	errorUnion := fmt.Sprintf("sum by (%s) ((%s))", group, strings.Join(errorSelectors, ") or ("))
	errorExpression := fmt.Sprintf("(%s) or on (%s) (%s * 0)", errorUnion, group, requestExpression)
	errorGrid := deviationSubquery(errorExpression, window, step)

	identityMatchers := baseMatchers[1:]
	latencyMatchers := append(append([]string(nil), identityMatchers...), `quantile="p95"`)
	var latencyExpression string
	if operations {
		latencyMatchers = append([]string{`span_kind="SPAN_KIND_SERVER"`}, latencyMatchers...)
		latencyExpression = fmt.Sprintf("sum by (%s) (trace_endpoint_duration{%s})", group, strings.Join(latencyMatchers, ","))
	} else {
		latencyExpression = fmt.Sprintf("sum by (%s) (trace_service_response_time{%s} * 1000)", group, strings.Join(latencyMatchers, ","))
	}
	latencyGrid := deviationSubquery(latencyExpression, window, step)

	apdexSelector := fmt.Sprintf("%s{%s}", apdexMetric, strings.Join(identityMatchers, ","))
	apdexExpression := fmt.Sprintf("sum by (%s) (%s)", group, apdexSelector)
	matching := strings.Join(groupLabels, ", ")
	alignedApdex := fmt.Sprintf("(%s and on (%s) %s)", apdexExpression, matching, requestExpression)
	alignedRequests := fmt.Sprintf("(%s and on (%s) %s)", requestExpression, matching, apdexExpression)
	apdexNumerator := fmt.Sprintf("%s * on (%s) %s", alignedApdex, matching, alignedRequests)
	apdexNumeratorGrid := deviationSubquery(apdexNumerator, window, step)
	alignedRequestGrid := deviationSubquery(alignedRequests, window, step)
	stepMinutes := strconv.FormatFloat(step.Minutes(), 'f', -1, 64)
	requestRPMGrid := deviationSubquery(fmt.Sprintf("(%s / %s)", requestExpression, stepMinutes), window, step)
	errorRPMGrid := deviationSubquery(fmt.Sprintf("(%s / %s)", errorExpression, stepMinutes), window, step)
	errorPercentageExpression := fmt.Sprintf("(((%s / %s) * 100) and on (%s) (%s > 0))", errorExpression, requestExpression, matching, requestExpression)
	errorPercentageGrid := deviationSubquery(errorPercentageExpression, window, step)
	apdexDistributionGrid := deviationSubquery(alignedApdex, window, step)

	limit := func(expression string) string {
		return fmt.Sprintf("(%s) and on (%s) (%s)", expression, matching, candidateMask)
	}
	queries := []deviationQuery{
		{Name: "requests_sum", Field: deviationFieldRequestTotal, Text: limit(requestTotal)},
		{Name: "requests_count", Field: deviationFieldRequestCount, Text: limit(fmt.Sprintf("count_over_time(%s)", requestGrid))},
		{Name: "errors_sum", Field: deviationFieldErrorTotal, Text: limit(fmt.Sprintf("sum_over_time(%s)", errorGrid))},
		{Name: "errors_count", Field: deviationFieldErrorCount, Text: limit(fmt.Sprintf("count_over_time(%s)", errorGrid))},
		{Name: "apdex_numerator", Field: deviationFieldApdexNumerator, Text: limit(fmt.Sprintf("sum_over_time(%s)", apdexNumeratorGrid))},
		{Name: "apdex_denominator", Field: deviationFieldApdexDenominator, Text: limit(fmt.Sprintf("sum_over_time(%s)", alignedRequestGrid))},
		{Name: "apdex_count", Field: deviationFieldApdexCount, Text: limit(fmt.Sprintf("count_over_time(%s)", alignedRequestGrid))},
		{Name: "request_distribution", Field: deviationFieldRequestDistribution, Text: limit(deviationDistributionQuery(requestRPMGrid))},
		{Name: "error_throughput_distribution", Field: deviationFieldErrorThroughputDistribution, Text: limit(deviationDistributionQuery(errorRPMGrid))},
		{Name: "error_percentage_distribution", Field: deviationFieldErrorPercentageDistribution, Text: limit(deviationDistributionQuery(errorPercentageGrid))},
		{Name: "apdex_distribution", Field: deviationFieldApdexDistribution, Text: limit(deviationDistributionQuery(apdexDistributionGrid))},
		{Name: "latency_q25", Field: deviationFieldLatencyQ25, Text: limit(fmt.Sprintf("quantile_over_time(0.25, %s)", latencyGrid))},
		{Name: "latency_median", Field: deviationFieldLatencyMedian, Text: limit(fmt.Sprintf("quantile_over_time(0.5, %s)", latencyGrid))},
		{Name: "latency_q75", Field: deviationFieldLatencyQ75, Text: limit(fmt.Sprintf("quantile_over_time(0.75, %s)", latencyGrid))},
		{Name: "latency_max", Field: deviationFieldLatencyMax, Text: limit(fmt.Sprintf("max_over_time(%s)", latencyGrid))},
		{Name: "latency_count", Field: deviationFieldLatencyCount, Text: limit(fmt.Sprintf("count_over_time(%s)", latencyGrid))},
	}
	for index := range queries {
		queries[index].CandidateMask = candidateMask
	}
	return queries
}

func deviationDistributionQuery(grid string) string {
	parts := []struct {
		quantile  string
		statistic string
	}{
		{quantile: "0.25", statistic: "q25"},
		{quantile: "0.5", statistic: "median"},
		{quantile: "0.75", statistic: "q75"},
	}
	queries := make([]string, 0, len(parts))
	for _, part := range parts {
		queries = append(queries, fmt.Sprintf(`label_replace(quantile_over_time(%s, %s), "deviation_stat", "%s", "", "")`, part.quantile, grid, part.statistic))
	}
	return strings.Join(queries, " or ")
}

func deviationGroupLabels(operations bool) []string {
	labels := []string{"service_name", "env"}
	if operations {
		labels = append(labels, "span_name")
	}
	return labels
}

func deviationRequestExpression(scope deviationQueryScope, group string) string {
	matchers := []string{`span_kind="SPAN_KIND_SERVER"`}
	if scope.ServiceName != "" {
		matchers = append(matchers, fmt.Sprintf(`service_name="%s"`, escapePromQLLabel(scope.ServiceName)))
	}
	if scope.Env != "" {
		matchers = append(matchers, fmt.Sprintf(`env="%s"`, escapePromQLLabel(scope.Env)))
	}
	return fmt.Sprintf("sum by (%s) (trace_endpoint_count{%s})", group, strings.Join(matchers, ","))
}

func deviationSubquery(expression string, window TimeWindow, step time.Duration) string {
	rangeSeconds := int64(window.End.Sub(window.Start) / time.Second)
	stepSeconds := int64(step / time.Second)
	return fmt.Sprintf("(%s)[%ds:%ds] @ %d", expression, rangeSeconds, stepSeconds, window.End.Unix())
}

func validDeviationWindow(window TimeWindow) bool {
	return window.End.After(window.Start)
}

func requestSelectorWithMatcher(base []string, matcher string) string {
	matchers := append(append([]string(nil), base...), matcher)
	return fmt.Sprintf("trace_endpoint_count{%s}", strings.Join(matchers, ","))
}

func executeDeviationQueries(
	ctx context.Context,
	runner deviationQueryRunner,
	plan deviationQueryPlan,
) deviationQueryExecution {
	type windowResult struct {
		window     string
		result     deviationQueryResult
		errors     []deviationQueryError
		attempted  int
		successful int
	}
	results := make(chan windowResult, 2)
	run := func(window string, end time.Time, queries []deviationQuery) {
		result, queryErrors, successful := executeDeviationQueryGroup(ctx, runner, window, end, queries)
		results <- windowResult{window: window, result: result, errors: queryErrors, attempted: len(queries), successful: successful}
	}
	go run("current", plan.CurrentEnd, plan.Current)
	go run("baseline", plan.BaselineEnd, plan.Baseline)

	execution := deviationQueryExecution{}
	var attempted, successful int
	for range 2 {
		result := <-results
		attempted += result.attempted
		successful += result.successful
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
	} else if attempted > 0 && successful == 0 {
		execution.Err = fmt.Errorf("all metric queries failed")
	} else if len(execution.Current.Records) == 0 && len(execution.Baseline.Records) == 0 && hasInvalidAggregateErrors(execution.Errors) {
		execution.Err = fmt.Errorf("metric queries returned no valid aggregate values")
	}
	return execution
}

func executeDeviationQueryGroup(ctx context.Context, runner deviationQueryRunner, window string, end time.Time, queries []deviationQuery) (deviationQueryResult, []deviationQueryError, int) {
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
	successful := 0
	for range queries {
		result := <-results
		if result.err != nil {
			queryErrors = append(queryErrors, deviationQueryError{
				Window: window, Signal: result.query.Name, Kind: "query_failed",
				Field: string(result.query.Field), Message: "query failed",
			})
			continue
		}
		successful++
		responses[result.query.Name] = result.vectors
	}
	records, parseErrors := parseDeviationQueryValues(queries, responses)
	for _, parseError := range parseErrors {
		parseError.Window = window
		queryErrors = append(queryErrors, parseError)
	}
	return deviationQueryResult{Records: records}, queryErrors, successful
}

func parseDeviationQueryValues(queries []deviationQuery, responses map[string][]deviationVector) ([]deviationAggregate, []deviationQueryError) {
	records := make(map[string]*deviationAggregate)
	parseErrors := make([]deviationQueryError, 0)
	for _, query := range queries {
		for _, vector := range responses[query.Name] {
			serviceName := vector.Metric["service_name"]
			env := vector.Metric["env"]
			spanName := vector.Metric["span_name"]
			errorEvidence := deviationQueryError{
				Signal: query.Name, Field: string(query.Field), ServiceName: serviceName,
				Env: env, SpanName: spanName, Message: "invalid aggregate result",
			}
			if serviceName == "" {
				errorEvidence.Kind = "missing_identity"
				parseErrors = append(parseErrors, errorEvidence)
				continue
			}
			value, kind := finitePromValue(vector.Value)
			if kind != "" {
				errorEvidence.Kind = kind
				parseErrors = append(parseErrors, errorEvidence)
				continue
			}
			statistic := vector.Metric["deviation_stat"]
			if isDeviationDistributionField(query.Field) && !validDeviationStatistic(statistic) {
				errorEvidence.Kind = "invalid_statistic"
				parseErrors = append(parseErrors, errorEvidence)
				continue
			}
			key := serviceName + "\x00" + env + "\x00" + spanName
			record := records[key]
			if record == nil {
				record = &deviationAggregate{ServiceName: serviceName, Env: env, SpanName: spanName}
				records[key] = record
			}
			if !setDeviationField(record, query.Field, statistic, value) {
				errorEvidence.Kind = "invalid_statistic"
				parseErrors = append(parseErrors, errorEvidence)
			}
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
	sort.Slice(parseErrors, func(i, j int) bool {
		left := parseErrors[i].Signal + "\x00" + parseErrors[i].ServiceName + "\x00" + parseErrors[i].Env + "\x00" + parseErrors[i].SpanName + "\x00" + parseErrors[i].Kind
		right := parseErrors[j].Signal + "\x00" + parseErrors[j].ServiceName + "\x00" + parseErrors[j].Env + "\x00" + parseErrors[j].SpanName + "\x00" + parseErrors[j].Kind
		return left < right
	})
	return output, parseErrors
}

func isDeviationDistributionField(field deviationField) bool {
	switch field {
	case deviationFieldRequestDistribution, deviationFieldErrorThroughputDistribution, deviationFieldErrorPercentageDistribution, deviationFieldApdexDistribution:
		return true
	default:
		return false
	}
}

func validDeviationStatistic(statistic string) bool {
	return statistic == "q25" || statistic == "median" || statistic == "q75"
}

func finitePromValue(value []any) (float64, string) {
	if len(value) != 2 {
		return 0, "malformed_value"
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
	if err != nil {
		return 0, "malformed_value"
	}
	if !isFinite(parsed) {
		return 0, "non_finite_value"
	}
	return parsed, ""
}

func setDeviationField(record *deviationAggregate, field deviationField, statistic string, value float64) bool {
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
	case deviationFieldRequestDistribution:
		return setDeviationDistributionField(statistic, pointer, &record.RequestQ25, &record.RequestMedian, &record.RequestQ75)
	case deviationFieldErrorThroughputDistribution:
		return setDeviationDistributionField(statistic, pointer, &record.ErrorThroughputQ25, &record.ErrorThroughputMedian, &record.ErrorThroughputQ75)
	case deviationFieldErrorPercentageDistribution:
		return setDeviationDistributionField(statistic, pointer, &record.ErrorPercentageQ25, &record.ErrorPercentageMedian, &record.ErrorPercentageQ75)
	case deviationFieldApdexDistribution:
		return setDeviationDistributionField(statistic, pointer, &record.ApdexQ25, &record.ApdexMedian, &record.ApdexQ75)
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
	default:
		return false
	}
	return true
}

func setDeviationDistributionField(statistic string, pointer func() *float64, q25, median, q75 **float64) bool {
	switch statistic {
	case "q25":
		*q25 = pointer()
	case "median":
		*median = pointer()
	case "q75":
		*q75 = pointer()
	default:
		return false
	}
	return true
}

func hasInvalidAggregateErrors(errors []deviationQueryError) bool {
	for _, item := range errors {
		if item.Kind != "query_failed" {
			return true
		}
	}
	return false
}
