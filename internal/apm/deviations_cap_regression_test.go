package apm

import (
	"context"
	"net/http"
	"testing"

	"last9-mcp/internal/models"
)

// TestDeviationCapRegressionBeatsCrossCategoryImprovement confirms the fix for
// the bug where orderedDeviationSlices interleaved a category's Improvements
// between that category's Regressions and the next category's Regressions,
// letting a Reliability improvement consume max_services capacity (and become
// the fleet follow-up target) before an Experience regression was even visited.
//
// Fleet: 10 Reliability-improvement services (error% 10%->1%, apdex unchanged)
// plus one Experience-regression service (error% unchanged, apdex 1.0->0.05,
// the worst deviation in the fleet). The regression must survive the cap and
// lead the fleet follow-up at BOTH MaxServices=1 and the default MaxServices=10;
// no improvement may oust the regression. This is the contract the rest of the
// system enforces: shouldQueryOperations and the corroborating follow-ups fire
// only on regressions, so an improvement can never be the reason a regression is
// dropped from a capped result.
func TestDeviationCapRegressionBeatsCrossCategoryImprovement(t *testing.T) {
	const improvementCount = 10
	buildExecution := func() deviationQueryExecution {
		var current, baseline []deviationAggregate
		// svc-imprv-0..svc-imprv-9: error% 10%->1% (Reliability improvement),
		// apdex unchanged at 0.9 (no Experience deviation).
		for i := 0; i < improvementCount; i++ {
			name := "svc-imprv-" + string(rune('0'+i))
			current = append(current, aggregate(name, "prod", "", 600, 6, 6, 6, 540, 600, 6, 50, 50, 50, 50, 6))
			baseline = append(baseline, aggregate(name, "prod", "", 600, 6, 60, 6, 540, 600, 6, 50, 50, 50, 50, 6))
		}
		// svc-regr: error% unchanged at 1% (no Reliability deviation), apdex
		// 1.0->0.05 (large Experience regression, |rel|~=0.95 — the worst
		// deviation in the fleet).
		current = append(current, aggregate("svc-regr", "prod", "", 600, 6, 6, 6, 30, 600, 6, 50, 50, 50, 50, 6))
		baseline = append(baseline, aggregate("svc-regr", "prod", "", 600, 6, 6, 6, 600, 600, 6, 50, 50, 50, 50, 6))
		return deviationQueryExecution{Current: deviationQueryResult{Records: current}, Baseline: deviationQueryResult{Records: baseline}}
	}

	for _, tc := range []struct {
		name        string
		maxServices int
		// expectedImprovementSurvivors is the number of reliability improvements
		// that share the cap alongside the regression.
		expectedImprovementSurvivors int
	}{
		{name: "cap_1", maxServices: 1, expectedImprovementSurvivors: 0},
		{name: "default_cap_10", maxServices: 10, expectedImprovementSurvivors: improvementCount - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDeviationHandlerDeps()
			deps.execute = func(_ context.Context, _ deviationQueryRunner, _ deviationQueryPlan) deviationQueryExecution {
				return buildExecution()
			}
			handler := newAPMServiceDeviationsHandler(http.DefaultClient, models.Config{DatasourceName: "primary"}, deps)
			args := sixMinuteDeviationArgs()
			args.MaxServices = tc.maxServices
			response := callDeviationHandler(t, handler, args)

			if response.Outcome != "deviations_detected" {
				t.Fatalf("outcome = %q, want deviations_detected", response.Outcome)
			}
			if len(response.Leaderboards.Experience.Regressions) != 1 || response.Leaderboards.Experience.Regressions[0].ServiceName != "svc-regr" {
				t.Fatalf("Experience regression was dropped by the cap: %+v", response.Leaderboards.Experience.Regressions)
			}
			svcRegrSurvives := false
			for _, s := range response.Services {
				if s.ServiceName == "svc-regr" {
					svcRegrSurvives = true
				}
			}
			if !svcRegrSurvives {
				t.Fatalf("svc-regr (the only regression) was dropped from Services by the cap; survivors=%+v", response.Services)
			}
			if len(response.Leaderboards.Reliability.Improvements) != tc.expectedImprovementSurvivors {
				t.Fatalf("reliability improvements survived = %d, want %d (improvements must fill only remaining capacity after regressions): %+v",
					len(response.Leaderboards.Reliability.Improvements), tc.expectedImprovementSurvivors, response.Leaderboards.Reliability.Improvements)
			}
			if len(response.Services) != tc.maxServices {
				t.Fatalf("services survived = %d, want %d", len(response.Services), tc.maxServices)
			}
			var followupService string
			for _, f := range response.RecommendedFollowups {
				if f.Tool == "get_apm_service_deviations" {
					followupService = f.Arguments["service_name"]
				}
			}
			if followupService != "svc-regr" {
				t.Fatalf("fleet follow-up targets %q, want svc-regr (the regression, not an improvement)", followupService)
			}
		})
	}
}

// TestOrderedDeviationSlicesRegressionsBeforeImprovements pins the slice order
// directly so the cross-category-regressions-before-improvements invariant is
// encoded at the helper level, independent of the cap loop.
func TestOrderedDeviationSlicesRegressionsBeforeImprovements(t *testing.T) {
	result := apmDeviationResult{DeviationResponse: DeviationResponse{Leaderboards: emptyLeaderboards()}}
	result.Leaderboards.Reliability.Regressions = []LeaderboardEntry{{ServiceName: "rel-regr"}}
	result.Leaderboards.Reliability.Improvements = []LeaderboardEntry{{ServiceName: "rel-impr"}}
	result.Leaderboards.Experience.Regressions = []LeaderboardEntry{{ServiceName: "exp-regr"}}
	result.Leaderboards.Experience.Improvements = []LeaderboardEntry{{ServiceName: "exp-impr"}}
	result.Leaderboards.SustainedLatency.Regressions = []LeaderboardEntry{{ServiceName: "sus-regr"}}
	result.Leaderboards.SustainedLatency.Improvements = []LeaderboardEntry{{ServiceName: "sus-impr"}}
	result.ThroughputShifts = []LeaderboardEntry{{ServiceName: "tput"}}

	slices := orderedDeviationSlices(result)
	wantOrder := []string{"rel-regr", "exp-regr", "sus-regr", "rel-impr", "exp-impr", "sus-impr", "tput"}
	var got []string
	for _, slice := range slices {
		for _, entry := range slice {
			got = append(got, entry.ServiceName)
		}
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("ordered slices yielded %d identities, want %d: %+v", len(got), len(wantOrder), got)
	}
	for i, want := range wantOrder {
		if got[i] != want {
			t.Fatalf("ordered slices[%d] = %q, want %q (all regressions before all improvements): %+v", i, got[i], want, got)
		}
	}
}
