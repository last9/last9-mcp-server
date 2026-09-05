package apm

import (
	"context"
	"net/http"
	"testing"

	"last9-mcp/internal/models"
)

func TestLimitDeviationResultDropsHighMagnitudeRegression(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(context.Context, deviationQueryRunner, deviationQueryPlan) deviationQueryExecution {
		var current, baseline []deviationAggregate
		// 11 services svc-a..svc-k, each with a SMALL distinct Apdex regression
		// (apdex 1.0 -> 0.900..0.817, |rel| 0.10..0.183), all LOWER than zzz-service.
		for i := 0; i < 11; i++ {
			name := "svc-" + string(rune('a'+i))
			apdexNumerator := 540 - i*5
			current = append(current, aggregate(name, "prod", "", 600, 6, 300, 6, float64(apdexNumerator), 600, 6, 50, 50, 50, 50, 6))
			baseline = append(baseline, aggregate(name, "prod", "", 600, 6, 0, 0, 600, 600, 6, 50, 50, 50, 50, 6))
		}
		// zzz-service: LARGEST Apdex regression (apdex 1.0 -> 0.05, |rel|=0.95).
		current = append(current, aggregate("zzz-service", "prod", "", 600, 6, 300, 6, 30, 600, 6, 50, 50, 50, 50, 6))
		baseline = append(baseline, aggregate("zzz-service", "prod", "", 600, 6, 0, 0, 600, 600, 6, 50, 50, 50, 50, 6))
		return deviationQueryExecution{Current: deviationQueryResult{Records: current}, Baseline: deviationQueryResult{Records: baseline}}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
	response := callDeviationHandler(t, handler, DeviationArgs{
		StartTimeISO: "2026-07-11T10:00:00Z",
		EndTimeISO:   "2026-07-11T10:06:00Z",
		MaxServices:  10,
	})

	if len(response.Services) != 10 {
		t.Fatalf("services survived = %d, want 10", len(response.Services))
	}
	zzzSurvives := false
	for _, e := range response.Leaderboards.Experience.Regressions {
		if e.ServiceName == "zzz-service" {
			zzzSurvives = true
		}
	}
	if !zzzSurvives {
		t.Fatalf("zzz-service (highest-magnitude regression) was dropped by the cap; survivors=%+v", response.Leaderboards.Experience.Regressions)
	}
	var followupService string
	for _, f := range response.RecommendedFollowups {
		if f.Tool == "get_apm_service_deviations" {
			followupService = f.Arguments["service_name"]
		}
	}
	if followupService != "zzz-service" {
		t.Fatalf("fleet follow-up targets %q, want zzz-service", followupService)
	}
}

// TestLimitDeviationResultFillsRemainingCapacityWithStableServices confirms
// that when deviating identities are fewer than the cap, stable services fill
// the remaining capacity alphabetically, so a small regression fleet keeps
// both the regressor and the unrelated stable services.
func TestLimitDeviationResultFillsRemainingCapacityWithStableServices(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				// zzz-service: the only deviator (large Apdex regression).
				aggregate("zzz-service", "prod", "", 600, 6, 6, 6, 30, 600, 6, 50, 50, 50, 50, 6),
				// stable services filling the remainder.
				aggregate("alpha", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("beta", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("gamma", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("zzz-service", "prod", "", 600, 6, 6, 6, 600, 600, 6, 50, 50, 50, 50, 6),
				aggregate("alpha", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("beta", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("gamma", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
	args := sixMinuteDeviationArgs()
	args.MaxServices = 3
	response := callDeviationHandler(t, handler, args)

	// zzz-service (1 deviator) + 2 alphabetically-first stable services (alpha, beta).
	if len(response.Services) != 3 {
		t.Fatalf("services survived = %d, want 3", len(response.Services))
	}
	names := map[string]bool{}
	for _, s := range response.Services {
		names[s.ServiceName] = true
	}
	for _, want := range []string{"zzz-service", "alpha", "beta"} {
		if !names[want] {
			t.Fatalf("expected %q to survive the cap, got %+v", want, response.Services)
		}
	}
	if names["gamma"] {
		t.Fatalf("gamma should have been dropped to fill capacity with the deviator, got %+v", response.Services)
	}
	if len(response.Leaderboards.Experience.Regressions) != 1 || response.Leaderboards.Experience.Regressions[0].ServiceName != "zzz-service" {
		t.Fatalf("expected single zzz-service experience regression, got %+v", response.Leaderboards.Experience.Regressions)
	}
}

// TestLimitDeviationResultRespectsLeaderboardPriorityOrder confirms that when
// multiple leaderboard categories contain regressions, identities are admitted
// in the same priority order leadingDeviationIdentity uses (Reliability before
// Experience before SustainedLatency before ThroughputShifts), so the cap never
// demotes a higher-priority leader in favor of a lower-priority one.
func TestLimitDeviationResultRespectsLeaderboardPriorityOrder(t *testing.T) {
	// Two regressions: a low-magnitude reliability regression on svc-a (priority
	// leader) and a high-magnitude experience regression on svc-b. With a cap of
	// 1, the reliability regression must win because leadingDeviationIdentity
	// checks Reliability before Experience.
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				// svc-a: small reliability regression (error% 1% -> 50%) + apdex drop.
				aggregate("svc-a", "prod", "", 600, 6, 300, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				// svc-b: large experience regression only (error% unchanged so no reliability regression).
				aggregate("svc-b", "prod", "", 600, 6, 6, 6, 30, 600, 6, 50, 50, 50, 50, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("svc-a", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("svc-b", "prod", "", 600, 6, 6, 6, 600, 600, 6, 50, 50, 50, 50, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
	args := sixMinuteDeviationArgs()
	args.MaxServices = 1
	response := callDeviationHandler(t, handler, args)

	if len(response.Services) != 1 {
		t.Fatalf("services survived = %d, want 1", len(response.Services))
	}
	if response.Services[0].ServiceName != "svc-a" {
		t.Fatalf("priority leader dropped: %+v", response.Services)
	}
	if len(response.Leaderboards.Reliability.Regressions) != 1 || response.Leaderboards.Reliability.Regressions[0].ServiceName != "svc-a" {
		t.Fatalf("reliability regression missing: %+v", response.Leaderboards.Reliability.Regressions)
	}
	var followupService string
	for _, f := range response.RecommendedFollowups {
		if f.Tool == "get_apm_service_deviations" {
			followupService = f.Arguments["service_name"]
		}
	}
	if followupService != "svc-a" {
		t.Fatalf("fleet follow-up targets %q, want svc-a (reliability priority)", followupService)
	}
}

// TestLimitDeviationResultKeepsTelemetryChangesOverStableServices confirms
// that telemetry-changed identities (newly / no-longer observed) are admitted
// before stable services when capping, so a presence change is never hidden by
// an alphabetically-earlier stable service.
func TestLimitDeviationResultKeepsTelemetryChangesOverStableServices(t *testing.T) {
	deps := testDeviationHandlerDeps()
	deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
		return deviationQueryExecution{
			Current: deviationQueryResult{Records: []deviationAggregate{
				// zzz-new: only present in current window -> newly_observed.
				aggregate("zzz-new", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				// three stable services alphabetically earlier than zzz-new.
				aggregate("aaa", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("bbb", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("ccc", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
			}},
			Baseline: deviationQueryResult{Records: []deviationAggregate{
				aggregate("aaa", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("bbb", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
				aggregate("ccc", "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6),
			}},
		}
	}
	handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
	response := callDeviationHandler(t, handler, DeviationArgs{MaxServices: 2})

	if len(response.Services) != 1 {
		t.Fatalf("stable services survived = %d, want 1 (cap filled by 1 telemetry change + 1 stable)", len(response.Services))
	}
	if response.Services[0].ServiceName != "aaa" {
		t.Fatalf("expected alphabetically-first stable service aaa to fill remaining capacity, got %+v", response.Services)
	}
	zzzKept := false
	for _, c := range response.TelemetryChanges {
		if c.ServiceName == "zzz-new" {
			zzzKept = true
		}
	}
	if !zzzKept {
		t.Fatalf("zzz-new telemetry change was dropped by the cap: %+v", response.TelemetryChanges)
	}
	if response.Outcome != "telemetry_changed" {
		t.Fatalf("outcome = %q, want telemetry_changed", response.Outcome)
	}
}
