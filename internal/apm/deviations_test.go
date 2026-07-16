package apm

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAPMServiceDeviationsHandlerValidatesCapsAndDatasource(t *testing.T) {
	deps := testDeviationHandlerDeps()
	resolved := ""
	deps.resolveDatasource = func(cfg models.Config, name string) (models.Config, error) {
		resolved = name
		if name == "missing" {
			return cfg, errors.New("datasource not found")
		}
		cfg.DatasourceName = name
		return cfg, nil
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)

	for _, tc := range []struct {
		name string
		args DeviationArgs
		want string
	}{
		{name: "negative services", args: DeviationArgs{MaxServices: -1}, want: "max_services must be between 1 and 10"},
		{name: "services over cap", args: DeviationArgs{MaxServices: 11}, want: "max_services must be between 1 and 10"},
		{name: "negative operations", args: DeviationArgs{MaxOperations: -1}, want: "max_operations must be between 1 and 10"},
		{name: "operations over cap", args: DeviationArgs{MaxOperations: 11}, want: "max_operations must be between 1 and 10"},
		{name: "unknown datasource", args: DeviationArgs{Datasource: "missing"}, want: "datasource not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	if resolved != "missing" {
		t.Fatalf("resolved datasource = %q, want missing", resolved)
	}
}

func TestAPMServiceDeviationsHandlerRejectsLookbackWithExplicitWindowBeforeQuery(t *testing.T) {
	deps := testDeviationHandlerDeps()
	queryCalls := 0
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		queryCalls++
		return deviationQueryExecution{}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeviationArgs{
		LookbackMinutes: 60,
		StartTimeISO:    "2026-07-11T08:00:00Z",
		EndTimeISO:      "2026-07-11T09:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "explicit current timestamps cannot be combined with lookback_minutes") {
		t.Fatalf("error = %v, want lookback/explicit-window conflict", err)
	}
	if queryCalls != 0 {
		t.Fatalf("query executions = %d, want 0", queryCalls)
	}
}

func TestAPMServiceDeviationsHandlerFleetKeepsEnvironmentsSeparateAndStable(t *testing.T) {
	deps := testDeviationHandlerDeps()
	var calls []deviationQueryPlan
	deps.execute = func(_ context.Context, _ deviationQueryRunner, plan deviationQueryPlan) deviationQueryExecution {
		calls = append(calls, plan)
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "", 600, 6, 6, 6, 540, 600, 6, 100, 100, 100, 100, 6),
				aggregate("api", "staging", "", 300, 6, 3, 6, 270, 300, 6, 80, 80, 80, 80, 6),
				aggregate("zzz", "prod", "", 100, 6, 1, 6, 90, 100, 6, 70, 70, 70, 70, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "", 600, 6, 6, 6, 540, 600, 6, 100, 100, 100, 100, 6),
				aggregate("api", "staging", "", 300, 6, 3, 6, 270, 300, 6, 80, 80, 80, 80, 6),
				aggregate("zzz", "prod", "", 100, 6, 1, 6, 90, 100, 6, 70, 70, 70, 70, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
	response := callDeviationHandler(t, handler, DeviationArgs{MaxServices: 2})

	if response.Scope != "fleet" || response.Datasource != "primary" {
		t.Fatalf("scope/datasource = %q/%q", response.Scope, response.Datasource)
	}
	if len(response.Services) != 2 || response.Services[0].Env != "prod" || response.Services[1].Env != "staging" {
		t.Fatalf("environments were merged or unstable: %+v", response.Services)
	}
	if len(calls) != 1 {
		t.Fatalf("query executions = %d, want service query only", len(calls))
	}
	if response.Outcome != "stable" || len(response.RecommendedFollowups) != 0 {
		t.Fatalf("stable outcome forced follow-up: %+v", response)
	}
	if len(response.Leaderboards.Reliability.Regressions) != 0 || len(response.ThroughputShifts) != 0 {
		t.Fatalf("stable response contains deviations: %+v", response.Leaderboards)
	}
}

func TestAPMServiceDeviationsHandlerSuppressesStableSignalJitter(t *testing.T) {
	baseline := aggregate("api", "prod", "", 600, 6, 12, 6, 540, 600, 6, 95, 100, 105, 110, 6)
	current := aggregate("api", "prod", "", 618, 6, 13, 6, 548, 618, 6, 98, 103, 108, 112, 6)
	setAggregateDistributions(&baseline,
		Distribution{Q25: 95, Median: 100, Q75: 105}, Distribution{Q25: 1.5, Median: 2, Q75: 2.5},
		Distribution{Q25: 0.7, Median: 1, Q75: 1.4}, Distribution{Q25: 0.88, Median: 0.9, Q75: 0.92})
	setAggregateDistributions(&current,
		Distribution{Q25: 98, Median: 103, Q75: 108}, Distribution{Q25: 1.7, Median: 2.1, Q75: 2.6},
		Distribution{Q25: 0.8, Median: 1.1, Q75: 1.5}, Distribution{Q25: 0.89, Median: 0.9, Q75: 0.91})

	deps := testDeviationHandlerDeps()
	queryCalls := 0
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		queryCalls++
		return deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	}
	args := sixMinuteDeviationArgs()
	args.ServiceName = "api"
	response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), args)
	if response.Outcome != "stable" || len(response.ThroughputShifts) != 0 || len(response.OperationCorrelations) != 0 || queryCalls != 1 {
		t.Fatalf("stable jitter produced investigation evidence: outcome=%q throughput=%+v operations=%+v calls=%d", response.Outcome, response.ThroughputShifts, response.OperationCorrelations, queryCalls)
	}
	if len(response.Leaderboards.Reliability.Regressions)+len(response.Leaderboards.Reliability.Improvements)+len(response.Leaderboards.Experience.Regressions)+len(response.Leaderboards.Experience.Improvements) != 0 {
		t.Fatalf("stable RED jitter entered leaderboards: %+v", response.Leaderboards)
	}
}

func TestAPMServiceDeviationsHandlerClassifiesMaterialThroughputAsShift(t *testing.T) {
	baseline := aggregate("api", "prod", "", 600, 6, 6, 6, 540, 600, 6, 95, 100, 105, 110, 6)
	current := aggregate("api", "prod", "", 1200, 6, 12, 6, 1080, 1200, 6, 95, 100, 105, 110, 6)
	setAggregateDistributions(&baseline, Distribution{Q25: 90, Median: 100, Q75: 110}, Distribution{}, Distribution{}, Distribution{})
	setAggregateDistributions(&current, Distribution{Q25: 190, Median: 200, Q75: 210}, Distribution{}, Distribution{}, Distribution{})
	deps := testDeviationHandlerDeps()
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	}
	response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), sixMinuteDeviationArgs())
	if len(response.ThroughputShifts) != 1 || response.ThroughputShifts[0].Comparison.Classification != "shift" || response.ThroughputShifts[0].Comparison.Direction != "increased" {
		t.Fatalf("throughput comparison = %+v", response.ThroughputShifts)
	}
	if len(response.Leaderboards.Reliability.Regressions)+len(response.Leaderboards.Experience.Improvements) != 0 {
		t.Fatalf("throughput was treated as health: %+v", response.Leaderboards)
	}
}

func TestAPMServiceDeviationsHandlerServiceRegressionCorrelatesOperations(t *testing.T) {
	deps := testDeviationHandlerDeps()
	calls := 0
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		calls++
		if calls == 1 {
			return deviationQueryExecution{
				Current: deviationQueryResult{Records: []deviationAggregate{
					aggregate("api", "prod", "", 600, 6, 60, 6, 420, 600, 6, 250, 300, 350, 400, 6),
				}},
				Baseline: deviationQueryResult{Records: []deviationAggregate{
					aggregate("api", "prod", "", 600, 6, 6, 6, 570, 600, 6, 80, 100, 120, 130, 6),
				}},
			}
		}
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "GET /orders", 300, 6, 45, 6, 180, 300, 6, 300, 350, 400, 450, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "GET /orders", 300, 6, 3, 6, 285, 300, 6, 70, 90, 110, 120, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	args := sixMinuteDeviationArgs()
	args.ServiceName = "api"
	args.Env = "prod"
	args.MaxOperations = 1
	response := callDeviationHandler(t, handler, args)

	if response.Scope != "service" || calls != 2 {
		t.Fatalf("scope/calls = %q/%d, want service/2", response.Scope, calls)
	}
	if len(response.Leaderboards.Reliability.Regressions) != 1 || len(response.Leaderboards.Experience.Regressions) != 1 || len(response.Leaderboards.SustainedLatency.Regressions) != 1 {
		t.Fatalf("missing RED regressions: %+v", response.Leaderboards)
	}
	if len(response.OperationCorrelations) == 0 || response.OperationCorrelations[0].Operation != "GET /orders" {
		t.Fatalf("missing operation correlation: %+v", response.OperationCorrelations)
	}
	if response.OperationCorrelations[0].Interpretation != operationCorrelationDisclaimer {
		t.Fatalf("operation interpretation = %q", response.OperationCorrelations[0].Interpretation)
	}
	if len(response.RecommendedFollowups) == 0 {
		t.Fatal("regression has no recommended follow-ups")
	}
	for _, followup := range response.RecommendedFollowups {
		if followup.Arguments["service_name"] != "api" || followup.Arguments["env"] != "prod" {
			t.Fatalf("follow-up lost scope: %+v", followup)
		}
	}
}

func TestAPMServiceDeviationsHandlerImprovementAndTelemetryChanges(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "", 600, 6, 0, 6, 594, 600, 6, 40, 50, 60, 70, 6),
				aggregate("worker", "prod", "", 60, 6, 0, 6, 54, 60, 6, 40, 50, 60, 70, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "", 600, 6, 60, 6, 420, 600, 6, 140, 150, 160, 170, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	response := callDeviationHandler(t, handler, sixMinuteDeviationArgs())
	if len(response.Leaderboards.Reliability.Improvements) != 1 || len(response.Leaderboards.Experience.Improvements) != 1 || len(response.Leaderboards.SustainedLatency.Improvements) != 1 {
		t.Fatalf("missing improvements: %+v", response.Leaderboards)
	}
	if len(response.TelemetryChanges) != 1 || response.TelemetryChanges[0].Change != "newly_observed" {
		t.Fatalf("presence change was classified numerically: %+v", response.TelemetryChanges)
	}
	for _, board := range []SignalLeaderboard{response.Leaderboards.Reliability, response.Leaderboards.Experience, response.Leaderboards.SustainedLatency} {
		for _, entry := range append(board.Regressions, board.Improvements...) {
			if entry.ServiceName == "worker" {
				t.Fatal("one-window identity entered a deviation leaderboard")
			}
		}
	}
}

func TestAPMServiceDeviationsHandlerKeepsNamedOneWindowServerTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		current    []deviationAggregate
		baseline   []deviationAggregate
		wantChange string
	}{
		{name: "current only", current: []deviationAggregate{aggregate("api", "prod", "", 60, 6, 0, 6, 54, 60, 6, 40, 50, 60, 70, 6)}, wantChange: "newly_observed"},
		{name: "baseline only", baseline: []deviationAggregate{aggregate("api", "prod", "", 60, 6, 0, 6, 54, 60, 6, 40, 50, 60, 70, 6)}, wantChange: "no_longer_observed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDeviationHandlerDeps()
			deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
				return deviationQueryExecution{Current: deviationQueryResult{Records: tc.current}, Baseline: deviationQueryResult{Records: tc.baseline}}
			}
			presenceCalls := 0
			deps.hasAnyAPMTelemetry = func(context.Context, deviationQueryRunner, DeviationArgs, DeviationWindows) (bool, error) {
				presenceCalls++
				return true, nil
			}
			args := sixMinuteDeviationArgs()
			args.ServiceName = "api"
			args.Env = "prod"
			response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), args)
			if response.Outcome != "telemetry_changed" || len(response.TelemetryChanges) != 1 || response.TelemetryChanges[0].Change != tc.wantChange {
				t.Fatalf("one-window telemetry response = %+v", response)
			}
			if presenceCalls != 0 {
				t.Fatalf("unsupported-workload detection ran %d times", presenceCalls)
			}
		})
	}
}

func TestServiceAndOperationRedistributionDisagreementDoesNotClassify(t *testing.T) {
	baseline := aggregate("api", "prod", "", 600, 6, 12, 6, 540, 600, 6, 80, 100, 120, 130, 6)
	current := aggregate("api", "prod", "", 1200, 6, 12, 6, 1140, 1200, 6, 80, 100, 120, 130, 6)
	setAggregateDistributions(&baseline, Distribution{}, Distribution{}, Distribution{Q25: 1, Median: 2, Q75: 3}, Distribution{Q25: 0.88, Median: 0.9, Q75: 0.92})
	setAggregateDistributions(&current, Distribution{}, Distribution{}, Distribution{Q25: 8, Median: 9, Q75: 10}, Distribution{Q25: 0.7, Median: 0.72, Q75: 0.74})

	deps := testDeviationHandlerDeps()
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	}
	args := sixMinuteDeviationArgs()
	args.ServiceName = "api"
	response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), args)
	if len(response.Leaderboards.Reliability.Regressions)+len(response.Leaderboards.Experience.Regressions) != 0 {
		t.Fatalf("service redistribution disagreement classified: %+v", response.Leaderboards)
	}

	windows, err := resolveDeviationWindows(args, time.Date(2026, 7, 11, 10, 7, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	current.SpanName, baseline.SpanName = "GET /orders", "GET /orders"
	serviceResult := apmDeviationResult{DeviationResponse: DeviationResponse{Leaderboards: emptyLeaderboards(), Services: []ServiceDeviation{{ServiceName: "api", Env: "prod"}}}}
	serviceResult.Leaderboards.Reliability.Regressions = []LeaderboardEntry{{ServiceName: "api", Env: "prod", Comparison: SignalComparison{Definition: SignalDefinition{Name: "error_percentage"}, Classification: "regression"}}}
	execution := deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	if got := correlateOperations(serviceResult, execution, windows, 10); len(got) != 0 {
		t.Fatalf("operation redistribution disagreement classified: %+v", got)
	}
}

func TestAPMServiceDeviationsHandlerUnsupportedWorkloadShape(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{}
	}
	deps.hasAnyAPMTelemetry = func(context.Context, deviationQueryRunner, DeviationArgs, DeviationWindows) (bool, error) {
		return true, nil
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	response := callDeviationHandler(t, handler, DeviationArgs{ServiceName: "processor"})
	if response.Outcome != "unsupported_workload_shape" {
		t.Fatalf("outcome = %q", response.Outcome)
	}
	if len(response.Leaderboards.Reliability.Regressions) != 0 || len(response.Services) != 0 {
		t.Fatalf("unsupported workload was classified: %+v", response)
	}
	if len(response.RecommendedFollowups) != 1 || response.RecommendedFollowups[0].Tool != "get_service_traces" || response.RecommendedFollowups[0].Arguments["service_name"] != "processor" {
		t.Fatalf("unsupported workload follow-up = %+v", response.RecommendedFollowups)
	}
}

func TestAPMServiceDeviationsHandlerSoftensPresenceCheckError(t *testing.T) {
	upstream := errors.New("upstream unavailable")
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{}
	}
	deps.hasAnyAPMTelemetry = func(context.Context, deviationQueryRunner, DeviationArgs, DeviationWindows) (bool, error) {
		return false, upstream
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)
	response := callDeviationHandler(t, handler, DeviationArgs{ServiceName: "processor"})
	if response.Outcome != "no_data" {
		t.Fatalf("outcome = %q, want no_data", response.Outcome)
	}
	found := false
	for _, w := range response.Warnings {
		if strings.Contains(w, "Workload telemetry presence could not be confirmed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing presence-check warning in %v", response.Warnings)
	}
}

func TestAPMServiceDeviationsHandlerRejectsIncompleteOrMalformedAggregates(t *testing.T) {
	t.Run("count only", func(t *testing.T) {
		count := 6.0
		record := deviationAggregate{ServiceName: "api", Env: "prod", RequestCount: &count}
		deps := testDeviationHandlerDeps()
		deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
			return deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{record}}, Baseline: deviationQueryResult{Records: []deviationAggregate{record}}}
		}
		_, _, err := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)(context.Background(), &mcp.CallToolRequest{}, sixMinuteDeviationArgs())
		if err == nil || err.Error() != "metric queries returned no valid RED measurements" {
			t.Fatalf("count-only error = %v", err)
		}
	})

	t.Run("malformed only", func(t *testing.T) {
		deps := testDeviationHandlerDeps()
		deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
			return deviationQueryExecution{Errors: []deviationQueryError{{Window: "current", Signal: "requests_sum", Field: string(deviationFieldRequestTotal), Kind: "non_finite_value"}}}
		}
		_, _, err := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)(context.Background(), &mcp.CallToolRequest{}, sixMinuteDeviationArgs())
		if err == nil || err.Error() != "metric queries returned no valid aggregate values" {
			t.Fatalf("malformed-only error = %v", err)
		}
	})

	t.Run("one valid signal", func(t *testing.T) {
		current := aggregate("api", "prod", "", 120, 6, 0, 6, 0, 0, 0, 0, 0, 0, 0, 0)
		baseline := aggregate("api", "prod", "", 60, 6, 0, 6, 0, 0, 0, 0, 0, 0, 0, 0)
		current.ErrorTotal, current.ErrorCount, current.ApdexNumerator, current.ApdexDenominator, current.ApdexCount = nil, nil, nil, nil, nil
		baseline.ErrorTotal, baseline.ErrorCount, baseline.ApdexNumerator, baseline.ApdexDenominator, baseline.ApdexCount = nil, nil, nil, nil, nil
		current.LatencyQ25, current.LatencyMedian, current.LatencyQ75, current.LatencyMax, current.LatencyCount = nil, nil, nil, nil, nil
		baseline.LatencyQ25, baseline.LatencyMedian, baseline.LatencyQ75, baseline.LatencyMax, baseline.LatencyCount = nil, nil, nil, nil, nil
		deps := testDeviationHandlerDeps()
		deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
			return deviationQueryExecution{
				Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}},
				Errors: []deviationQueryError{{Window: "current", Signal: "errors_sum", Field: string(deviationFieldErrorTotal), Kind: "query_failed"}},
			}
		}
		response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), sixMinuteDeviationArgs())
		if len(response.PartialErrors) != 1 || len(response.Warnings) == 0 {
			t.Fatalf("partial valid signal was not returned with warnings: %+v", response)
		}
	})

	t.Run("successful empty", func(t *testing.T) {
		deps := testDeviationHandlerDeps()
		deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
			return deviationQueryExecution{}
		}
		response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), DeviationArgs{})
		if response.Outcome != "no_data" {
			t.Fatalf("empty successful outcome = %q", response.Outcome)
		}
	})
}

func TestAPMWorkloadPresenceChecksBothWindowsAndFamilies(t *testing.T) {
	windows, err := resolveDeviationWindows(sixMinuteDeviationArgs(), time.Date(2026, 7, 11, 10, 7, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		vectors []deviationVector
		want    bool
	}{
		{name: "client only", vectors: []deviationVector{{Metric: map[string]string{"family": "trace_client_count"}, Value: []any{1.0, "1"}}}, want: true},
		{name: "baseline only", vectors: []deviationVector{{Metric: map[string]string{"window": "baseline"}, Value: []any{1.0, "1"}}}, want: true},
		{name: "earlier current window", vectors: []deviationVector{{Metric: map[string]string{"window": "current"}, Value: []any{1.0, "1"}}}, want: true},
		{name: "absent", vectors: []deviationVector{}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			runner := deviationQueryRunnerFunc(func(_ context.Context, query string, end time.Time) ([]deviationVector, error) {
				calls++
				for _, want := range []string{"trace_endpoint_count", "trace_client_count", "domain_attributes_count", strconvUnix(windows.EffectiveCurrentEnd), strconvUnix(windows.EffectiveBaselineEnd), `service_name="processor"`, `env="prod"`} {
					if !strings.Contains(query, want) {
						t.Errorf("presence query missing %q: %s", want, query)
					}
				}
				if !end.Equal(windows.EffectiveCurrentEnd) {
					t.Errorf("query end = %s", end)
				}
				return tc.vectors, nil
			})
			got, err := hasAnyAPMTelemetry(context.Background(), runner, DeviationArgs{ServiceName: "processor", Env: "prod"}, windows)
			if err != nil || got != tc.want || calls != 1 {
				t.Fatalf("presence = %t, err=%v, calls=%d", got, err, calls)
			}
		})
	}
}

func TestAPMWorkloadPresencePreservesErrorsAndCancellation(t *testing.T) {
	windows, err := resolveDeviationWindows(sixMinuteDeviationArgs(), time.Date(2026, 7, 11, 10, 7, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	upstream := errors.New("upstream unavailable")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "upstream", err: upstream},
		{name: "cancelled", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := deviationQueryRunnerFunc(func(context.Context, string, time.Time) ([]deviationVector, error) { return nil, tc.err })
			_, gotErr := hasAnyAPMTelemetry(context.Background(), runner, DeviationArgs{ServiceName: "processor"}, windows)
			if tc.err == context.Canceled {
				if !errors.Is(gotErr, context.Canceled) {
					t.Fatalf("cancellation = %v", gotErr)
				}
			} else if gotErr == nil || gotErr.Error() != "workload telemetry check failed" {
				t.Fatalf("upstream error = %v", gotErr)
			}
		})
	}
}

func TestAPMServiceDeviationsHandlerFleetFollowupSelectsLeadingIdentity(t *testing.T) {
	baseline := aggregate("api", "prod", "", 600, 6, 6, 6, 570, 600, 6, 80, 100, 120, 130, 6)
	current := aggregate("api", "prod", "", 600, 6, 60, 6, 420, 600, 6, 80, 100, 120, 130, 6)
	deps := testDeviationHandlerDeps()
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	}
	args := sixMinuteDeviationArgs()
	args.Datasource = "primary"
	args.MaxOperations = 4
	response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), args)
	if len(response.RecommendedFollowups) != 1 || response.RecommendedFollowups[0].Tool != "get_apm_service_deviations" {
		t.Fatalf("fleet follow-ups = %+v", response.RecommendedFollowups)
	}
	followup := response.RecommendedFollowups[0]
	if followup.Arguments["service_name"] != "api" || followup.Arguments["env"] != "prod" || followup.Arguments["datasource"] != "primary" || followup.Arguments["max_operations"] != "4" {
		t.Fatalf("fleet transition lost scope: %+v", followup)
	}
	for _, item := range response.RecommendedFollowups {
		if item.Tool == "get_service_logs" && item.Arguments["service_name"] == "" {
			t.Fatalf("fleet emitted unscoped log follow-up: %+v", item)
		}
	}
}

func TestOperationCorrelationMatchesServiceEnvironmentAndSignal(t *testing.T) {
	windows, err := resolveDeviationWindows(sixMinuteDeviationArgs(), time.Date(2026, 7, 11, 10, 7, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	regression := func(env string) LeaderboardEntry {
		return LeaderboardEntry{ServiceName: "api", Env: env, Comparison: SignalComparison{Definition: SignalDefinition{Name: "error_percentage"}, Classification: "regression"}}
	}
	currentProd := aggregate("api", "prod", "GET /prod", 300, 6, 60, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	baselineProd := aggregate("api", "prod", "GET /prod", 300, 6, 3, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	currentStaging := aggregate("api", "staging", "GET /staging", 300, 6, 60, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	baselineStaging := aggregate("api", "staging", "GET /staging", 300, 6, 3, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	execution := deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{currentProd, currentStaging}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baselineProd, baselineStaging}}}

	for _, tc := range []struct{ env, operation string }{{"prod", "GET /prod"}, {"staging", "GET /staging"}} {
		result := apmDeviationResult{DeviationResponse: DeviationResponse{Leaderboards: emptyLeaderboards(), Services: []ServiceDeviation{{ServiceName: "api", Env: tc.env, Signals: []SignalComparison{{Definition: SignalDefinition{Name: "request_rpm"}, Current: WindowSummary{RequestTotal: 300}}}}}}}
		result.Leaderboards.Reliability.Regressions = []LeaderboardEntry{regression(tc.env)}
		got := correlateOperations(result, execution, windows, 10)
		if len(got) != 1 || got[0].Env != tc.env || got[0].Operation != tc.operation {
			t.Fatalf("%s correlation crossed environment: %+v", tc.env, got)
		}
	}
}

func TestOperationCorrelationSuppressesStableJitter(t *testing.T) {
	windows, err := resolveDeviationWindows(sixMinuteDeviationArgs(), time.Date(2026, 7, 11, 10, 7, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate("api", "prod", "GET /orders", 300, 6, 18, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	baseline := aggregate("api", "prod", "GET /orders", 300, 6, 15, 6, 0, 0, 0, 0, 0, 0, 0, 0)
	setAggregateDistributions(&baseline, Distribution{}, Distribution{}, Distribution{Q25: 4, Median: 5, Q75: 6}, Distribution{})
	setAggregateDistributions(&current, Distribution{}, Distribution{}, Distribution{Q25: 4.5, Median: 6, Q75: 7}, Distribution{})
	result := apmDeviationResult{DeviationResponse: DeviationResponse{Leaderboards: emptyLeaderboards(), Services: []ServiceDeviation{{ServiceName: "api", Env: "prod"}}}}
	result.Leaderboards.Reliability.Regressions = []LeaderboardEntry{{ServiceName: "api", Env: "prod", Comparison: SignalComparison{Definition: SignalDefinition{Name: "error_percentage"}, Classification: "regression"}}}
	execution := deviationQueryExecution{Current: deviationQueryResult{Records: []deviationAggregate{current}}, Baseline: deviationQueryResult{Records: []deviationAggregate{baseline}}}
	if got := correlateOperations(result, execution, windows, 10); len(got) != 0 {
		t.Fatalf("stable operation jitter was correlated: %+v", got)
	}
}

func TestOperationApdexReconciliationReportsCoverageAndResidual(t *testing.T) {
	deps := testDeviationHandlerDeps()
	calls := 0
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		calls++
		if calls == 1 {
			return deviationQueryExecution{
				Current: deviationQueryResult{Records: []deviationAggregate{
					aggregate("api", "prod", "", 1200, 6, 10, 6, 800, 1000, 6, 100, 120, 140, 160, 6),
				}},
				Baseline: deviationQueryResult{Records: []deviationAggregate{
					aggregate("api", "prod", "", 1500, 6, 10, 6, 900, 1000, 6, 100, 120, 140, 160, 6),
				}},
			}
		}
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "GET /a", 900, 6, 6, 6, 420, 600, 6, 100, 120, 140, 160, 6),
				aggregate("api", "prod", "GET /b", 300, 6, 2, 6, 180, 200, 6, 100, 120, 140, 160, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("api", "prod", "GET /a", 1000, 6, 5, 6, 450, 500, 6, 100, 120, 140, 160, 6),
				aggregate("api", "prod", "GET /b", 500, 6, 3, 6, 270, 300, 6, 100, 120, 140, 160, 6),
			}},
		}
	}

	args := sixMinuteDeviationArgs()
	args.ServiceName = "api"
	args.Env = "prod"
	result, _, err := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(deviationResultText(t, result)), &payload); err != nil {
		t.Fatal(err)
	}
	raw, ok := payload["operation_apdex_reconciliations"].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("operation Apdex reconciliation missing: %+v", payload["operation_apdex_reconciliations"])
	}
	reconciliation := raw[0].(map[string]any)
	for field, want := range map[string]float64{
		"current_request_coverage":  0.8,
		"baseline_request_coverage": 0.8,
		"service_apdex_delta":       -0.1,
		"observed_operation_delta":  -0.12,
		"unexplained_delta":         0.02,
	} {
		if got := reconciliation[field].(float64); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}
	contributions, ok := reconciliation["contributions"].([]any)
	if !ok || len(contributions) != 2 {
		t.Fatalf("contributions = %+v, want two comparable operations", reconciliation["contributions"])
	}
}

func TestErrorPercentageEvidenceRequiresAlignedRequestAndErrorCoverage(t *testing.T) {
	record := aggregate("api", "prod", "", 600, 6, 6, 3, 540, 600, 6, 80, 100, 120, 130, 6)
	summary := summaryFromAggregate(record, true, 6, time.Minute, nil)
	if summary.Evidence.ErrorPercentage.ObservedPoints != 3 || summary.Evidence.ErrorPercentage.Coverage != 0.5 {
		t.Fatalf("reliability evidence ignored sparse errors: %+v", summary.Evidence.ErrorPercentage)
	}
	comparison := signalSummary("error_percentage", summary)
	if comparison.Evidence.Selected.Coverage != 0.5 {
		t.Fatalf("selected reliability evidence = %+v", comparison.Evidence.Selected)
	}

	excluded := summaryFromAggregate(record, true, 6, time.Minute, map[deviationField]int{deviationFieldErrorTotal: 1})
	if excluded.Evidence.ErrorPercentage.ExcludedValues == 0 || excluded.Evidence.ErrorPercentage.Coverage >= 1 {
		t.Fatalf("excluded error values did not make reliability sparse: %+v", excluded.Evidence.ErrorPercentage)
	}
}

func TestSignalSummaryRecalculatesDistributionExclusionEvidence(t *testing.T) {
	record := aggregate("api", "prod", "", 600, 6, 6, 6, 540, 600, 6, 80, 100, 120, 130, 6)
	record.ErrorPercentageQ25 = nil
	summary := summaryFromAggregate(record, true, 6, time.Minute, map[deviationField]int{deviationFieldErrorPercentageDistribution: 1})
	selected := signalSummary("error_percentage", summary)
	if selected.Evidence.Selected.ObservedPoints != 0 || selected.Evidence.Selected.Coverage != 0 || selected.Evidence.Selected.MissingValues != 5 || selected.Evidence.Selected.ExcludedValues != 1 {
		t.Fatalf("selected distribution evidence was not recalculated: %+v", selected.Evidence.Selected)
	}
	payload, err := json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Evidence struct {
			Selected MetricEvidence `json:"selected"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Evidence.Selected.Coverage != 0 || decoded.Evidence.Selected.ObservedPoints != 0 {
		t.Fatalf("public JSON retained stale selected coverage: %s", payload)
	}
}

func TestThroughputOrderingUsesAbsoluteDeltaAndDrivesFleetFollowup(t *testing.T) {
	entry := func(service string, absolute, relative float64) LeaderboardEntry {
		return LeaderboardEntry{
			ServiceName: service, Env: "prod", SignalCategory: "throughput",
			Comparison: SignalComparison{Definition: SignalDefinition{Name: "request_rpm"}, AbsoluteDelta: absolute, RelativeDelta: &relative, Classification: "shift"},
		}
	}
	result := apmDeviationResult{DeviationResponse: DeviationResponse{
		Scope: "fleet", Outcome: "deviations_detected", Leaderboards: emptyLeaderboards(),
		ThroughputShifts: []LeaderboardEntry{entry("small-relative-large", 100, 0.1), entry("large-relative-small", 10, 2)},
		Windows:          DeviationWindows{RequestedCurrentStart: time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC), RequestedCurrentEnd: time.Date(2026, 7, 11, 11, 0, 0, 0, time.UTC)},
	}}
	sortDeviationResult(&result)
	if result.ThroughputShifts[0].ServiceName != "small-relative-large" {
		t.Fatalf("throughput ordering used relative delta: %+v", result.ThroughputShifts)
	}
	followups := recommendedDeviationFollowups(result, DeviationArgs{})
	if len(followups) != 1 || followups[0].Arguments["service_name"] != "small-relative-large" {
		t.Fatalf("fleet follow-up ignored corrected throughput order: %+v", followups)
	}
}

func TestMissingIdentityParseErrorDoesNotMakeEveryServiceSparse(t *testing.T) {
	errorWithoutIdentity := deviationQueryError{Window: "current", Signal: "request_distribution", Field: string(deviationFieldRequestDistribution), Kind: "missing_identity"}
	for _, service := range []string{"api", "worker"} {
		record := aggregate(service, "prod", "", 600, 6, 6, 6, 540, 600, 6, 80, 100, 120, 130, 6)
		exclusions := exclusionsFor([]deviationQueryError{errorWithoutIdentity}, "current", record)
		if len(exclusions) != 0 {
			t.Fatalf("missing identity error mapped to %q: %+v", service, exclusions)
		}
		summary := summaryFromAggregate(record, true, 6, time.Minute, exclusions)
		selected := signalSummary("request_rpm", summary)
		if selected.Evidence.Selected.Coverage != 1 || selected.Evidence.Selected.ExcludedValues != 0 {
			t.Fatalf("valid service %q became sparse: %+v", service, selected.Evidence.Selected)
		}
	}
}

func TestAPMServiceDeviationsHandlerPartialAllFailureAndCancellation(t *testing.T) {
	t.Run("partial", func(t *testing.T) {
		deps := testDeviationHandlerDeps()
		deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
			return deviationQueryExecution{
				Current:  deviationQueryResult{Records: []deviationAggregate{aggregate("api", "prod", "", 60, 6, 0, 6, 54, 60, 6, 40, 50, 60, 70, 6)}},
				Baseline: deviationQueryResult{Records: []deviationAggregate{aggregate("api", "prod", "", 60, 6, 0, 6, 54, 60, 6, 40, 50, 60, 70, 6)}},
				Errors:   []deviationQueryError{{Window: "current", Signal: "latency_q75", Kind: "query_failed", Message: "query failed"}},
			}
		}
		response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), DeviationArgs{})
		if len(response.PartialErrors) != 1 || len(response.Warnings) == 0 {
			t.Fatalf("partial failure evidence missing: %+v", response)
		}
	})

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "all failed", err: errors.New("all metric queries failed")},
		{name: "cancelled", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDeviationHandlerDeps()
			deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
				return deviationQueryExecution{Err: tc.err}
			}
			_, _, err := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps)(context.Background(), &mcp.CallToolRequest{}, DeviationArgs{})
			if !errors.Is(err, tc.err) && (err == nil || err.Error() != tc.err.Error()) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestAPMServiceDeviationsHandlerWindowsLinkAndRedaction(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{}
	}
	cfg := models.Config{OrgSlug: "example", ClusterID: "cluster-a", DatasourceName: "primary", PrometheusReadURL: "https://metrics.example.test", PrometheusPassword: "<test-password>"}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, cfg, deps)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DeviationArgs{
		ServiceName: "api", Env: "prod", Datasource: "primary",
		StartTimeISO: "2026-07-11T08:00:00Z", EndTimeISO: "2026-07-11T09:00:00Z",
		BaselineStartISO: "2026-07-11T06:00:00Z", BaselineEndISO: "2026-07-11T07:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := deviationResultText(t, result)
	for _, forbidden := range []string{"promql", "metrics.example.test", "<test-password>", "storage engine", "confidence", "root cause", "attribution"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("response leaked forbidden %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"previous_period", "explicit", "query_step_seconds", "error_definition", "apdex_definition", "measured_noise_criteria", "dashboard_url"} {
		if !strings.Contains(text, required) {
			t.Fatalf("response missing %q: %s", required, text)
		}
	}
	if result.Meta["reference_url"] == nil {
		t.Fatal("dashboard deep link missing from MCP metadata")
	}
}

func TestAPMServiceDeviationsHandlerResolvesDefaultAndRelativeWindowsOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       DeviationArgs
		timeSource string
		duration   time.Duration
	}{
		{name: "default", args: DeviationArgs{}, timeSource: "default", duration: time.Hour},
		{name: "relative", args: DeviationArgs{LookbackMinutes: 30}, timeSource: "relative_lookback", duration: 30 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDeviationHandlerDeps()
			calls := 0
			deps.now = func() time.Time {
				calls++
				return time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
			}
			deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
				return deviationQueryExecution{}
			}
			response := callDeviationHandler(t, newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{}, deps), tc.args)
			if calls != 1 {
				t.Fatalf("clock calls = %d, want 1", calls)
			}
			if response.Windows.TimeSource != tc.timeSource || response.Windows.BaselineMode != "previous_period" {
				t.Fatalf("window provenance = %+v", response.Windows)
			}
			if got := response.Windows.RequestedCurrentEnd.Sub(response.Windows.RequestedCurrentStart); got != tc.duration {
				t.Fatalf("requested duration = %s, want %s", got, tc.duration)
			}
		})
	}
}

func TestAPMServiceDeviationsHandlerUsesResolvedDatasourceHTTPPath(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("X-LAST9-API-TOKEN"); got != "Bearer <test-token>" {
			t.Errorf("API token header = %q", got)
		}
		var body struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
			ReadURL   string `json:"read_url"`
			Username  string `json:"username"`
			Password  string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Query == "" || body.Timestamp == 0 || body.ReadURL != "https://selected.invalid" || body.Username != "selected-user" || body.Password != "selected-password" {
			t.Errorf("unexpected resolved request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		DatasourceName: "default",
		APIBaseURL:     server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "<test-token>", ExpiresAt: time.Now().Add(time.Hour),
		},
		Datasources: []models.DatasourceInfo{{
			Name: "selected", ReadURL: "https://selected.invalid", Username: "selected-user", Password: "selected-password", Region: "test", ClusterID: "selected-cluster",
		}},
	}
	handler := NewAPMServiceDeviationsHandler(server.Client(), cfg)
	response := callDeviationHandler(t, handler, DeviationArgs{
		Datasource: "selected", StartTimeISO: "2026-07-11T08:00:00Z", EndTimeISO: "2026-07-11T09:00:00Z",
	})
	if response.Datasource != "selected" {
		t.Fatalf("datasource = %q", response.Datasource)
	}
	if calls.Load() != 32 {
		t.Fatalf("HTTP calls = %d, want 32 compact current/baseline rollups", calls.Load())
	}
}

func testDeviationHandlerDeps() deviationHandlerDeps {
	return deviationHandlerDeps{
		now:       func() time.Time { return time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC) },
		queryStep: time.Minute,
		resolveDatasource: func(cfg models.Config, _ string) (models.Config, error) {
			return cfg, nil
		},
		runnerFactory: func(*http.Client, models.Config) deviationQueryRunner {
			return deviationQueryRunnerFunc(func(context.Context, string, time.Time) ([]deviationVector, error) { return nil, nil })
		},
		execute: executeDeviationQueries,
		hasAnyAPMTelemetry: func(context.Context, deviationQueryRunner, DeviationArgs, DeviationWindows) (bool, error) {
			return false, nil
		},
	}
}

func aggregate(service, env, span string, requests, requestCount, errors, errorCount, apdexNumerator, apdexDenominator, apdexCount, q25, median, q75, peak, latencyCount float64) deviationAggregate {
	record := deviationAggregate{
		ServiceName: service, Env: env, SpanName: span,
		RequestTotal: &requests, RequestCount: &requestCount,
		ErrorTotal: &errors, ErrorCount: &errorCount,
		ApdexNumerator: &apdexNumerator, ApdexDenominator: &apdexDenominator, ApdexCount: &apdexCount,
		LatencyQ25: &q25, LatencyMedian: &median, LatencyQ75: &q75, LatencyMax: &peak, LatencyCount: &latencyCount,
	}
	requestRPM := requests / 6
	errorRPM := errors / 6
	errorPercentage := 0.0
	if requests > 0 {
		errorPercentage = errors / requests * 100
	}
	apdex := 0.0
	if apdexDenominator > 0 {
		apdex = apdexNumerator / apdexDenominator
	}
	setAggregateDistributions(&record,
		Distribution{Q25: requestRPM, Median: requestRPM, Q75: requestRPM},
		Distribution{Q25: errorRPM, Median: errorRPM, Q75: errorRPM},
		Distribution{Q25: errorPercentage, Median: errorPercentage, Q75: errorPercentage},
		Distribution{Q25: apdex, Median: apdex, Q75: apdex},
	)
	return record
}

func setAggregateDistributions(record *deviationAggregate, requests, errors, errorPercentage, apdex Distribution) {
	record.RequestQ25, record.RequestMedian, record.RequestQ75 = &requests.Q25, &requests.Median, &requests.Q75
	record.ErrorThroughputQ25, record.ErrorThroughputMedian, record.ErrorThroughputQ75 = &errors.Q25, &errors.Median, &errors.Q75
	record.ErrorPercentageQ25, record.ErrorPercentageMedian, record.ErrorPercentageQ75 = &errorPercentage.Q25, &errorPercentage.Median, &errorPercentage.Q75
	record.ApdexQ25, record.ApdexMedian, record.ApdexQ75 = &apdex.Q25, &apdex.Median, &apdex.Q75
}

func sixMinuteDeviationArgs() DeviationArgs {
	return DeviationArgs{
		StartTimeISO: "2026-07-11T10:00:00Z",
		EndTimeISO:   "2026-07-11T10:06:00Z",
	}
}

func callDeviationHandler(t *testing.T, handler func(context.Context, *mcp.CallToolRequest, DeviationArgs) (*mcp.CallToolResult, any, error), args DeviationArgs) apmDeviationResult {
	t.Helper()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	var response apmDeviationResult
	if err := json.Unmarshal([]byte(deviationResultText(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func deviationResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T", result.Content[0])
	}
	return content.Text
}
