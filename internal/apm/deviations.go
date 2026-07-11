package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	deviationResultCap             = 10
	deviationQueryStep             = time.Minute
	operationCorrelationDisclaimer = "Correlated operation movement is supporting evidence only; it does not establish contribution, attribution, cause, or root cause."
)

type deviationHandlerDeps struct {
	now                func() time.Time
	queryStep          time.Duration
	resolveDatasource  func(models.Config, string) (models.Config, error)
	runnerFactory      func(*http.Client, models.Config) deviationQueryRunner
	execute            func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution
	hasAnyAPMTelemetry func(context.Context, deviationQueryRunner, DeviationArgs, DeviationWindows) (bool, error)
}

type deviationPartialError struct {
	Window string `json:"window"`
	Signal string `json:"signal"`
	Kind   string `json:"kind"`
}

type deviationFollowup struct {
	Tool      string            `json:"tool"`
	Reason    string            `json:"reason"`
	Arguments map[string]string `json:"arguments"`
}

type operationCorrelation struct {
	ServiceName    string           `json:"service_name"`
	Env            string           `json:"env,omitempty"`
	Operation      string           `json:"operation"`
	Signal         string           `json:"signal"`
	Comparison     SignalComparison `json:"comparison"`
	RequestShare   float64          `json:"current_request_share"`
	Interpretation string           `json:"interpretation"`
}

type deviationProvenance struct {
	MetricDefinitions     []SignalDefinition `json:"metric_definitions"`
	ErrorDefinition       string             `json:"error_definition"`
	ApdexDefinition       string             `json:"apdex_definition"`
	MeasuredNoiseCriteria string             `json:"measured_noise_criteria"`
	BaselineDefinition    string             `json:"baseline_definition"`
	Aggregation           string             `json:"aggregation"`
}

type apmDeviationResult struct {
	DeviationResponse
	OperationCorrelations []operationCorrelation  `json:"operation_correlations"`
	RecommendedFollowups  []deviationFollowup     `json:"recommended_followups"`
	PartialErrors         []deviationPartialError `json:"partial_errors,omitempty"`
	Provenance            deviationProvenance     `json:"provenance"`
	DashboardURL          string                  `json:"dashboard_url"`
}

// NewAPMServiceDeviationsHandler compares bounded APM RED aggregates across equal windows.
func NewAPMServiceDeviationsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeviationArgs) (*mcp.CallToolResult, any, error) {
	return newAPMServiceDeviationsHandler(client, cfg, deviationHandlerDeps{
		now:                func() time.Time { return time.Now().UTC() },
		queryStep:          deviationQueryStep,
		resolveDatasource:  resolveDatasourceCfg,
		runnerFactory:      newHTTPDeviationQueryRunner,
		execute:            executeDeviationQueries,
		hasAnyAPMTelemetry: hasAnyAPMTelemetry,
	})
}

func newAPMServiceDeviationsHandler(client *http.Client, baseCfg models.Config, deps deviationHandlerDeps) func(context.Context, *mcp.CallToolRequest, DeviationArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeviationArgs) (*mcp.CallToolResult, any, error) {
		maxServices, err := deviationLimit("max_services", args.MaxServices)
		if err != nil {
			return nil, nil, err
		}
		maxOperations, err := deviationLimit("max_operations", args.MaxOperations)
		if err != nil {
			return nil, nil, err
		}
		queryCfg, err := deps.resolveDatasource(baseCfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		now := deps.now().UTC()
		windows, err := resolveDeviationWindows(args, now, deps.queryStep)
		if err != nil {
			return nil, nil, err
		}

		runner := deps.runnerFactory(client, queryCfg)
		scope := deviationQueryScope{ServiceName: args.ServiceName, Env: args.Env, Limit: maxServices}
		plan := buildServiceRollupQueries(scope, effectiveCurrentWindow(windows), effectiveBaselineWindow(windows), deps.queryStep)
		execution := deps.execute(ctx, runner, plan)
		if execution.Err != nil {
			return nil, nil, execution.Err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		result := buildDeviationResult(args, queryCfg, windows, execution)
		limitDeviationResult(&result, maxServices)
		if args.ServiceName != "" && len(result.Services) == 0 {
			present, detectErr := deps.hasAnyAPMTelemetry(ctx, runner, args, windows)
			if detectErr != nil {
				return nil, nil, detectErr
			}
			if present {
				result.Outcome = "unsupported_workload_shape"
				result.Warnings = append(result.Warnings, "The named workload has APM telemetry but no server-request series supported by this comparison.")
			}
		}

		if args.ServiceName != "" && shouldQueryOperations(result) {
			opScope := deviationQueryScope{ServiceName: args.ServiceName, Env: args.Env, Limit: maxOperations}
			opPlan := buildOperationRollupQueries(opScope, effectiveCurrentWindow(windows), effectiveBaselineWindow(windows), deps.queryStep)
			opExecution := deps.execute(ctx, runner, opPlan)
			if opExecution.Err != nil {
				if ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				result.Warnings = append(result.Warnings, "Operation correlation was unavailable.")
			} else {
				result.PartialErrors = append(result.PartialErrors, publicDeviationErrors(opExecution.Errors)...)
				result.OperationCorrelations = correlateOperations(result, opExecution, windows, maxOperations)
			}
		}
		result.RecommendedFollowups = recommendedDeviationFollowups(result, args)
		result.Warnings = uniqueSorted(result.Warnings)
		result.PartialErrors = sortedPartialErrors(result.PartialErrors)
		if len(result.PartialErrors) > 0 {
			result.Warnings = uniqueSorted(append(result.Warnings, "Some metric signals were unavailable; conclusions use the successful measurements only."))
		}

		builder := deeplink.NewBuilder(queryCfg.OrgSlug, queryCfg.ClusterID)
		result.DashboardURL = builder.BuildAPMServiceLink(
			windows.RequestedCurrentStart.UnixMilli(), windows.RequestedCurrentEnd.UnixMilli(), args.ServiceName, args.Env, "",
		)
		payload, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal APM deviation response: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:              deeplink.ToMeta(result.DashboardURL),
			Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
			StructuredContent: result,
		}, nil, nil
	}
}

func deviationLimit(name string, value int) (int, error) {
	if value == 0 {
		return deviationResultCap, nil
	}
	if value < 1 || value > deviationResultCap {
		return 0, fmt.Errorf("%s must be between 1 and %d", name, deviationResultCap)
	}
	return value, nil
}

func effectiveCurrentWindow(w DeviationWindows) TimeWindow {
	return TimeWindow{Start: w.EffectiveCurrentStart, End: w.EffectiveCurrentEnd}
}

func effectiveBaselineWindow(w DeviationWindows) TimeWindow {
	return TimeWindow{Start: w.EffectiveBaselineStart, End: w.EffectiveBaselineEnd}
}

func buildDeviationResult(args DeviationArgs, cfg models.Config, windows DeviationWindows, execution deviationQueryExecution) apmDeviationResult {
	scope := "fleet"
	if args.ServiceName != "" {
		scope = "service"
	}
	result := apmDeviationResult{
		DeviationResponse: DeviationResponse{
			Scope: scope, Datasource: selectedDatasource(args, cfg), Windows: windows,
			Services: []ServiceDeviation{}, TelemetryChanges: []TelemetryChange{}, ThroughputShifts: []LeaderboardEntry{}, Outcome: "stable",
			Leaderboards: emptyLeaderboards(),
		},
		OperationCorrelations: []operationCorrelation{}, RecommendedFollowups: []deviationFollowup{},
		PartialErrors: publicDeviationErrors(execution.Errors), Provenance: deviationMeasurementProvenance(),
	}

	current := aggregateMap(execution.Current.Records)
	baseline := aggregateMap(execution.Baseline.Records)
	keys := unionAggregateKeys(current, baseline)
	expectedCurrent := bucketCapacity(effectiveCurrentWindow(windows), windows.QueryStep)
	expectedBaseline := bucketCapacity(effectiveBaselineWindow(windows), windows.QueryStep)
	for _, key := range keys {
		currentRecord, hasCurrent := current[key]
		baselineRecord, hasBaseline := baseline[key]
		identity := currentRecord
		if !hasCurrent {
			identity = baselineRecord
		}
		currentExclusions := exclusionsFor(execution.Errors, "current", identity)
		baselineExclusions := exclusionsFor(execution.Errors, "baseline", identity)
		currentSummary := summaryFromAggregate(currentRecord, hasCurrent, expectedCurrent, windows.QueryStep, currentExclusions)
		baselineSummary := summaryFromAggregate(baselineRecord, hasBaseline, expectedBaseline, windows.QueryStep, baselineExclusions)

		if !hasCurrent || !hasBaseline {
			change := "newly_observed"
			if !hasCurrent {
				change = "no_longer_observed"
			}
			result.TelemetryChanges = append(result.TelemetryChanges, TelemetryChange{ServiceName: identity.ServiceName, Env: identity.Env, Change: change})
			continue
		}

		service := ServiceDeviation{ServiceName: identity.ServiceName, Env: identity.Env, Signals: compareAggregateSignals(currentSummary, baselineSummary)}
		result.Services = append(result.Services, service)
		addServiceComparisons(&result, service)
	}
	sortDeviationResult(&result)
	if hasMaterialDeviation(result) {
		result.Outcome = "deviations_detected"
	} else if len(result.TelemetryChanges) > 0 {
		result.Outcome = "telemetry_changed"
	} else if len(result.Services) == 0 {
		result.Outcome = "no_data"
	}
	return result
}

func selectedDatasource(args DeviationArgs, cfg models.Config) string {
	if args.Datasource != "" {
		return args.Datasource
	}
	return cfg.DatasourceName
}

func emptyLeaderboards() DeviationLeaderboards {
	empty := func() SignalLeaderboard {
		return SignalLeaderboard{Regressions: []LeaderboardEntry{}, Improvements: []LeaderboardEntry{}}
	}
	return DeviationLeaderboards{Reliability: empty(), Experience: empty(), SustainedLatency: empty()}
}

func aggregateMap(records []deviationAggregate) map[string]deviationAggregate {
	result := make(map[string]deviationAggregate, len(records))
	for _, record := range records {
		result[aggregateKey(record)] = record
	}
	return result
}

func aggregateKey(record deviationAggregate) string {
	return record.ServiceName + "\x00" + record.Env + "\x00" + record.SpanName
}

func unionAggregateKeys(left, right map[string]deviationAggregate) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		set[key] = struct{}{}
	}
	for key := range right {
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func summaryFromAggregate(record deviationAggregate, present bool, expected int, step time.Duration, exclusions map[deviationField]int) WindowSummary {
	summary := WindowSummary{Evidence: newWindowEvidence(expected)}
	if !present {
		summary.Evidence = withCoverage(summary.Evidence)
		return summary
	}
	setEvidence := func(target *MetricEvidence, value *float64, field deviationField) {
		target.ExcludedValues = exclusions[field]
		if value != nil {
			target.ObservedPoints = aggregateCount(value, expected)
		}
	}
	setEvidence(&summary.Evidence.Requests, record.RequestCount, deviationFieldRequestCount)
	summary.Evidence.Requests.ExcludedValues += exclusions[deviationFieldRequestTotal]
	setEvidence(&summary.Evidence.Errors, record.ErrorCount, deviationFieldErrorCount)
	summary.Evidence.Errors.ExcludedValues += exclusions[deviationFieldErrorTotal]
	setEvidence(&summary.Evidence.Apdex, record.ApdexCount, deviationFieldApdexCount)
	summary.Evidence.Apdex.ExcludedValues += exclusions[deviationFieldApdexNumerator] + exclusions[deviationFieldApdexDenominator]
	setEvidence(&summary.Evidence.P95Latency, record.LatencyCount, deviationFieldLatencyCount)
	summary.Evidence.P95Latency.ExcludedValues += exclusions[deviationFieldLatencyQ25] + exclusions[deviationFieldLatencyMedian] + exclusions[deviationFieldLatencyQ75] + exclusions[deviationFieldLatencyMax]
	summary.Evidence.ErrorPercentage = summary.Evidence.Requests
	summary.Evidence.ErrorPercentage.ExcludedValues += exclusions[deviationFieldErrorTotal]
	if record.RequestTotal != nil {
		summary.RequestTotal = *record.RequestTotal
	}
	if record.ErrorTotal != nil {
		summary.ErrorTotal = *record.ErrorTotal
	}
	minutes := float64(expected) * step.Minutes()
	if minutes > 0 {
		summary.RequestRPM = summary.RequestTotal / minutes
		summary.ErrorRPM = summary.ErrorTotal / minutes
	}
	if record.RequestTotal != nil && *record.RequestTotal > 0 && record.ErrorTotal != nil {
		value := *record.ErrorTotal / *record.RequestTotal * 100
		summary.ErrorPercentage = &value
	}
	if record.ApdexNumerator != nil && record.ApdexDenominator != nil && *record.ApdexDenominator > 0 {
		value := *record.ApdexNumerator / *record.ApdexDenominator
		summary.Apdex = &value
	}
	if record.LatencyQ25 != nil && record.LatencyMedian != nil && record.LatencyQ75 != nil && record.LatencyMax != nil {
		d := Distribution{Q25: *record.LatencyQ25, Median: *record.LatencyMedian, Q75: *record.LatencyQ75, IQR: *record.LatencyQ75 - *record.LatencyQ25, Peak: *record.LatencyMax}
		summary.P95Latency = &d
	}
	summary.Evidence = withCoverage(summary.Evidence)
	return summary
}

func aggregateCount(value *float64, expected int) int {
	if value == nil || !isFinite(*value) || *value <= 0 {
		return 0
	}
	return min(int(math.Round(*value)), expected)
}

func compareAggregateSignals(current, baseline WindowSummary) []SignalComparison {
	definitions := deviationSignalDefinitions()
	comparisons := make([]SignalComparison, 0, len(definitions))
	for _, definition := range definitions {
		currentSignal := signalSummary(definition.Name, current)
		baselineSignal := signalSummary(definition.Name, baseline)
		comparisons = append(comparisons, compareSignal(definition, currentSignal, baselineSignal))
	}
	return comparisons
}

func deviationSignalDefinitions() []SignalDefinition {
	return []SignalDefinition{
		{Name: "request_rpm", Definition: "server requests per effective minute", Aggregation: "ratio of request total to effective-window minutes", Unit: "requests_per_minute"},
		{Name: "error_percentage", Definition: "server errors divided by server requests", Aggregation: "ratio_of_totals", Unit: "percent", HigherIsWorse: true},
		{Name: "error_throughput_rpm", Definition: "server errors per effective minute", Aggregation: "ratio of error total to effective-window minutes", Unit: "errors_per_minute", HigherIsWorse: true},
		{Name: "apdex", Definition: "request-weighted service Apdex", Aggregation: "request_weighted_mean", Unit: "score", HigherIsWorse: false},
		{Name: "p95_latency_ms", Definition: "median of service p95 latency buckets", Aggregation: "median_of_p95", Unit: "milliseconds", HigherIsWorse: true},
	}
}

func signalSummary(name string, source WindowSummary) WindowSummary {
	result := source
	var value float64
	var evidence MetricEvidence
	switch name {
	case "request_rpm":
		value, evidence = source.RequestRPM, source.Evidence.Requests
	case "error_percentage":
		evidence = source.Evidence.ErrorPercentage
		if source.ErrorPercentage != nil {
			value = *source.ErrorPercentage
		} else {
			evidence.ObservedPoints = 0
		}
	case "error_throughput_rpm":
		value, evidence = source.ErrorRPM, source.Evidence.Errors
	case "apdex":
		evidence = source.Evidence.Apdex
		if source.Apdex != nil {
			value = *source.Apdex
		} else {
			evidence.ObservedPoints = 0
		}
	case "p95_latency_ms":
		evidence = source.Evidence.P95Latency
		if source.P95Latency != nil {
			value = source.P95Latency.Median
			result.Distribution = *source.P95Latency
		} else {
			evidence.ObservedPoints = 0
		}
	}
	result.Value = value
	result.Evidence.Selected = evidence
	if name != "p95_latency_ms" {
		result.Distribution = Distribution{Q25: value, Median: value, Q75: value, Peak: value}
	}
	return result
}

func addServiceComparisons(result *apmDeviationResult, service ServiceDeviation) {
	for _, comparison := range service.Signals {
		entry := LeaderboardEntry{ServiceName: service.ServiceName, Env: service.Env, Comparison: comparison}
		switch comparison.Definition.Name {
		case "request_rpm":
			entry.SignalCategory = "throughput"
			if comparison.Classification == "regression" || comparison.Classification == "improvement" {
				result.ThroughputShifts = append(result.ThroughputShifts, entry)
			}
		case "error_percentage":
			entry.SignalCategory = "reliability"
			appendLeaderboard(&result.Leaderboards.Reliability, entry)
		case "apdex":
			entry.SignalCategory = "experience"
			appendLeaderboard(&result.Leaderboards.Experience, entry)
		case "p95_latency_ms":
			entry.SignalCategory = "sustained_latency"
			appendLeaderboard(&result.Leaderboards.SustainedLatency, entry)
		}
	}
}

func appendLeaderboard(board *SignalLeaderboard, entry LeaderboardEntry) {
	switch entry.Comparison.Classification {
	case "regression":
		board.Regressions = append(board.Regressions, entry)
	case "improvement":
		board.Improvements = append(board.Improvements, entry)
	}
}

func sortDeviationResult(result *apmDeviationResult) {
	sort.Slice(result.Services, func(i, j int) bool {
		return identityLess(result.Services[i].ServiceName, result.Services[i].Env, result.Services[j].ServiceName, result.Services[j].Env)
	})
	sort.Slice(result.TelemetryChanges, func(i, j int) bool {
		return identityLess(result.TelemetryChanges[i].ServiceName, result.TelemetryChanges[i].Env, result.TelemetryChanges[j].ServiceName, result.TelemetryChanges[j].Env)
	})
	for _, board := range []*SignalLeaderboard{&result.Leaderboards.Reliability, &result.Leaderboards.Experience, &result.Leaderboards.SustainedLatency} {
		sortLeaderboard(board.Regressions)
		sortLeaderboard(board.Improvements)
	}
	sortLeaderboard(result.ThroughputShifts)
}

func limitDeviationResult(result *apmDeviationResult, limit int) {
	if limit <= 0 {
		return
	}
	identities := make(map[string]struct{}, limit)
	for _, service := range result.Services {
		if len(identities) == limit {
			break
		}
		identities[service.ServiceName+"\x00"+service.Env] = struct{}{}
	}
	for _, change := range result.TelemetryChanges {
		if len(identities) == limit {
			break
		}
		identities[change.ServiceName+"\x00"+change.Env] = struct{}{}
	}
	result.Services = filterServices(result.Services, identities)
	result.TelemetryChanges = filterTelemetryChanges(result.TelemetryChanges, identities)
	result.ThroughputShifts = filterLeaderboardEntries(result.ThroughputShifts, identities)
	for _, board := range []*SignalLeaderboard{&result.Leaderboards.Reliability, &result.Leaderboards.Experience, &result.Leaderboards.SustainedLatency} {
		board.Regressions = filterLeaderboardEntries(board.Regressions, identities)
		board.Improvements = filterLeaderboardEntries(board.Improvements, identities)
	}
}

func filterServices(values []ServiceDeviation, identities map[string]struct{}) []ServiceDeviation {
	result := make([]ServiceDeviation, 0, len(values))
	for _, value := range values {
		if _, ok := identities[value.ServiceName+"\x00"+value.Env]; ok {
			result = append(result, value)
		}
	}
	return result
}

func filterTelemetryChanges(values []TelemetryChange, identities map[string]struct{}) []TelemetryChange {
	result := make([]TelemetryChange, 0, len(values))
	for _, value := range values {
		if _, ok := identities[value.ServiceName+"\x00"+value.Env]; ok {
			result = append(result, value)
		}
	}
	return result
}

func filterLeaderboardEntries(values []LeaderboardEntry, identities map[string]struct{}) []LeaderboardEntry {
	result := make([]LeaderboardEntry, 0, len(values))
	for _, value := range values {
		if _, ok := identities[value.ServiceName+"\x00"+value.Env]; ok {
			result = append(result, value)
		}
	}
	return result
}

func sortLeaderboard(entries []LeaderboardEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := comparisonMagnitude(entries[i].Comparison), comparisonMagnitude(entries[j].Comparison)
		if left != right {
			return left > right
		}
		return identityLess(entries[i].ServiceName, entries[i].Env, entries[j].ServiceName, entries[j].Env)
	})
}

func comparisonMagnitude(comparison SignalComparison) float64 {
	if comparison.RelativeDelta != nil {
		return math.Abs(*comparison.RelativeDelta)
	}
	return math.Abs(comparison.AbsoluteDelta)
}

func identityLess(leftService, leftEnv, rightService, rightEnv string) bool {
	return leftService+"\x00"+leftEnv < rightService+"\x00"+rightEnv
}

func hasMaterialDeviation(result apmDeviationResult) bool {
	return len(result.Leaderboards.Reliability.Regressions)+len(result.Leaderboards.Reliability.Improvements)+
		len(result.Leaderboards.Experience.Regressions)+len(result.Leaderboards.Experience.Improvements)+
		len(result.Leaderboards.SustainedLatency.Regressions)+len(result.Leaderboards.SustainedLatency.Improvements)+len(result.ThroughputShifts) > 0
}

func shouldQueryOperations(result apmDeviationResult) bool {
	return len(result.Leaderboards.Reliability.Regressions)+len(result.Leaderboards.Experience.Regressions)+len(result.Leaderboards.SustainedLatency.Regressions) > 0
}

func correlateOperations(serviceResult apmDeviationResult, execution deviationQueryExecution, windows DeviationWindows, limit int) []operationCorrelation {
	current := aggregateMap(execution.Current.Records)
	baseline := aggregateMap(execution.Baseline.Records)
	expectedCurrent := bucketCapacity(effectiveCurrentWindow(windows), windows.QueryStep)
	expectedBaseline := bucketCapacity(effectiveBaselineWindow(windows), windows.QueryStep)
	regressedSignals := make(map[string]struct{})
	for _, board := range []SignalLeaderboard{serviceResult.Leaderboards.Reliability, serviceResult.Leaderboards.Experience, serviceResult.Leaderboards.SustainedLatency} {
		for _, entry := range board.Regressions {
			regressedSignals[entry.Comparison.Definition.Name] = struct{}{}
		}
	}
	result := make([]operationCorrelation, 0)
	for _, key := range unionAggregateKeys(current, baseline) {
		cur, curOK := current[key]
		base, baseOK := baseline[key]
		if !curOK || !baseOK || cur.SpanName == "" {
			continue
		}
		curSummary := summaryFromAggregate(cur, true, expectedCurrent, windows.QueryStep, exclusionsFor(execution.Errors, "current", cur))
		baseSummary := summaryFromAggregate(base, true, expectedBaseline, windows.QueryStep, exclusionsFor(execution.Errors, "baseline", base))
		for _, comparison := range compareAggregateSignals(curSummary, baseSummary) {
			if comparison.Classification != "regression" {
				continue
			}
			if _, wanted := regressedSignals[comparison.Definition.Name]; !wanted {
				continue
			}
			share := 0.0
			for _, service := range serviceResult.Services {
				if service.ServiceName != cur.ServiceName || service.Env != cur.Env {
					continue
				}
				for _, signal := range service.Signals {
					if signal.Definition.Name == "request_rpm" && signal.Current.RequestTotal > 0 {
						share = curSummary.RequestTotal / signal.Current.RequestTotal
					}
				}
			}
			result = append(result, operationCorrelation{ServiceName: cur.ServiceName, Env: cur.Env, Operation: cur.SpanName, Signal: comparison.Definition.Name, Comparison: comparison, RequestShare: share, Interpretation: operationCorrelationDisclaimer})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Signal != result[j].Signal {
			return result[i].Signal < result[j].Signal
		}
		if comparisonMagnitude(result[i].Comparison) != comparisonMagnitude(result[j].Comparison) {
			return comparisonMagnitude(result[i].Comparison) > comparisonMagnitude(result[j].Comparison)
		}
		return result[i].Operation < result[j].Operation
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func recommendedDeviationFollowups(result apmDeviationResult, args DeviationArgs) []deviationFollowup {
	if result.Outcome == "stable" || result.Outcome == "no_data" || result.Outcome == "unsupported_workload_shape" {
		return []deviationFollowup{}
	}
	base := map[string]string{
		"start_time_iso": result.Windows.RequestedCurrentStart.Format(time.RFC3339),
		"end_time_iso":   result.Windows.RequestedCurrentEnd.Format(time.RFC3339),
	}
	if args.ServiceName != "" {
		base["service_name"] = args.ServiceName
	}
	if args.Env != "" {
		base["env"] = args.Env
	}
	if result.Datasource != "" {
		base["datasource"] = result.Datasource
	}
	followups := make([]deviationFollowup, 0, 3)
	add := func(tool, reason string) {
		arguments := make(map[string]string, len(base))
		for key, value := range base {
			arguments[key] = value
		}
		followups = append(followups, deviationFollowup{Tool: tool, Reason: reason, Arguments: arguments})
	}
	if len(result.Leaderboards.Reliability.Regressions) > 0 {
		add("get_exceptions", "Corroborate the reliability regression with exception evidence in the same scope.")
		add("get_service_logs", "Inspect scoped logs for errors that coincide with the measured reliability change.")
	}
	if len(result.Leaderboards.Experience.Regressions)+len(result.Leaderboards.SustainedLatency.Regressions) > 0 {
		add("get_service_traces", "Inspect slow or unsuccessful traces in the same window; metric correlation alone is not causal evidence.")
	}
	if len(result.ThroughputShifts)+len(result.TelemetryChanges) > 0 {
		add("get_change_events", "Check deployment, routing, and instrumentation changes near the telemetry shift.")
	}
	if len(followups) > 4 {
		followups = followups[:4]
	}
	return followups
}

func publicDeviationErrors(errors []deviationQueryError) []deviationPartialError {
	result := make([]deviationPartialError, 0, len(errors))
	for _, item := range errors {
		result = append(result, deviationPartialError{Window: item.Window, Signal: item.Signal, Kind: item.Kind})
	}
	return sortedPartialErrors(result)
}

func sortedPartialErrors(errors []deviationPartialError) []deviationPartialError {
	sort.Slice(errors, func(i, j int) bool {
		left := errors[i].Window + "\x00" + errors[i].Signal + "\x00" + errors[i].Kind
		right := errors[j].Window + "\x00" + errors[j].Signal + "\x00" + errors[j].Kind
		return left < right
	})
	return errors
}

func exclusionsFor(errors []deviationQueryError, window string, identity deviationAggregate) map[deviationField]int {
	result := make(map[deviationField]int)
	for _, item := range errors {
		if item.Window != window || item.Kind == "query_failed" {
			continue
		}
		if item.ServiceName != "" && (item.ServiceName != identity.ServiceName || item.Env != identity.Env || item.SpanName != identity.SpanName) {
			continue
		}
		result[deviationField(item.Field)]++
	}
	return result
}

func deviationMeasurementProvenance() deviationProvenance {
	return deviationProvenance{
		MetricDefinitions:     deviationSignalDefinitions(),
		ErrorDefinition:       "A server request is an error when telemetry marks an OpenTelemetry error, HTTP 5xx or 429, or a non-success gRPC status; ordinary HTTP 4xx is excluded unless marked as an OpenTelemetry error.",
		ApdexDefinition:       "Request-weighted service Apdex over buckets aligned with server request telemetry; the source metric's configured satisfaction threshold applies.",
		MeasuredNoiseCriteria: "A RED change is classified only with complete aligned coverage and non-overlapping current and baseline interquartile ranges; sparse and non-comparable evidence is not ranked.",
		BaselineDefinition:    "The default baseline is the immediately preceding equal-duration period (previous_period); callers may provide one explicit equal-duration baseline.",
		Aggregation:           "Reliability uses ratio-of-totals, Apdex is request weighted, sustained latency is median p95 with peak p95 as supporting evidence.",
	}
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func hasAnyAPMTelemetry(ctx context.Context, runner deviationQueryRunner, args DeviationArgs, windows DeviationWindows) (bool, error) {
	matchers := []string{fmt.Sprintf(`service_name="%s"`, escapePromQLLabel(args.ServiceName))}
	if args.Env != "" {
		matchers = append(matchers, fmt.Sprintf(`env="%s"`, escapePromQLLabel(args.Env)))
	}
	query := fmt.Sprintf("sum(trace_endpoint_count{%s})", strings.Join(matchers, ","))
	vectors, err := runner.Query(ctx, query, windows.EffectiveCurrentEnd)
	if err != nil {
		return false, fmt.Errorf("workload telemetry check failed")
	}
	return len(vectors) > 0, nil
}
