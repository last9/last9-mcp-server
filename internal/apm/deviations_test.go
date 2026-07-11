package apm

import (
	"context"
	"encoding/json"
	"errors"
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
	for _, forbidden := range []string{"promql", "metrics.example.test", "<test-password>", "victoria", "confidence", "root cause", "attribution"} {
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
	if calls.Load() != 24 {
		t.Fatalf("HTTP calls = %d, want 24 compact current/baseline rollups", calls.Load())
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
	return deviationAggregate{
		ServiceName: service, Env: env, SpanName: span,
		RequestTotal: &requests, RequestCount: &requestCount,
		ErrorTotal: &errors, ErrorCount: &errorCount,
		ApdexNumerator: &apdexNumerator, ApdexDenominator: &apdexDenominator, ApdexCount: &apdexCount,
		LatencyQ25: &q25, LatencyMedian: &median, LatencyQ75: &q75, LatencyMax: &peak, LatencyCount: &latencyCount,
	}
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
