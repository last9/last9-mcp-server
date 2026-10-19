package changeimpact

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"last9-mcp/internal/apm"
)

// The three verdicts. last9-mcp-evals matches these strings exactly.
const (
	VerdictRegressionLikely    = "regression likely"
	VerdictNoSignificantChange = "no significant change"
	VerdictInsufficientData    = "insufficient data"
)

// What counts as significant. The tool description documents every value here;
// change them together.
const (
	// An after window shorter than this cannot show a regression yet.
	minAfterMinutes = 5
	// Error-rate and latency shifts are judged only when both windows carry at
	// least this many server requests, latency in the compared environment.
	// Below 100, a p95 rests on fewer than 5 requests and one error is >1 pp.
	minRequestsPerWindow = 100
	// Error rate rising by at least this many percentage points is a regression.
	errorRateIncreasePP = 1.0
	// p95 latency rising by at least this percentage is a regression. 25%
	// flagged ordinary p95 variation; #376 tracks judging it against the
	// service's own variability instead.
	p95LatencyIncreasePct = 40.0
	// Throughput (rpm) falling by at least this percentage is a regression:
	// requests that stop arriving may be failing before the service sees them.
	throughputDropPct = 50.0
	// Any alert for the service or a dependent that starts firing in the after
	// window is a regression, as is any regression get_apm_service_deviations
	// classifies for the changed service.
)

// reportThresholds publishes every significance constant above, so callers can
// explain a verdict from the report alone.
func reportThresholds() map[string]float64 {
	return map[string]float64{
		"min_after_minutes":        minAfterMinutes,
		"min_requests_per_window":  minRequestsPerWindow,
		"error_rate_increase_pp":   errorRateIncreasePP,
		"p95_latency_increase_pct": p95LatencyIncreasePct,
		"throughput_drop_pct":      throughputDropPct,
	}
}

const noChangeNote = "No significant change is a valid answer, not a failure: the change did not move error rate, p95 latency, throughput or alerts past the thresholds."

// thresholdEpsilon absorbs float noise in deltas computed exactly at a threshold.
const thresholdEpsilon = 1e-9

// serviceRegressions lists the thresholds s crossed between the windows.
func serviceRegressions(s scopedService, baseline, after apm.GoldenSignals) []Regression {
	if baseline.RequestCount < minRequestsPerWindow {
		return nil
	}
	var found []Regression
	if after.RequestCount >= minRequestsPerWindow && baseline.ErrorRatePct != nil && after.ErrorRatePct != nil {
		if delta := *after.ErrorRatePct - *baseline.ErrorRatePct; delta >= errorRateIncreasePP-thresholdEpsilon {
			found = append(found, s.regression(MetricErrorRatePct, *baseline.ErrorRatePct, *after.ErrorRatePct,
				fmt.Sprintf("%s error rate rose %.2f pp (%.2f%% → %.2f%%) (threshold %g pp)", s.name, delta, *baseline.ErrorRatePct, *after.ErrorRatePct, errorRateIncreasePP)))
		}
	}
	env := latencyEnv(baseline, after)
	before, now := baseline.LatencyByEnv[env].P95Ms, after.LatencyByEnv[env].P95Ms
	judgeLatency := baseline.RequestsByEnv[env] >= minRequestsPerWindow && after.RequestsByEnv[env] >= minRequestsPerWindow
	if pct := deltaPct(before, now); judgeLatency && pct != nil && *pct >= p95LatencyIncreasePct-thresholdEpsilon {
		inLatencyEnv := s
		inLatencyEnv.env = env
		found = append(found, inLatencyEnv.regression(MetricLatencyP95Ms, *before, *now,
			fmt.Sprintf("%s p95 latency rose %.0f%% (%.1f ms → %.1f ms) in %s (threshold %g%%)", s.name, *pct, *before, *now, env, p95LatencyIncreasePct)))
	}
	// Windows can differ in length, so compare rates, not request counts. A
	// collapse is what thins the after window, so only an empty one is skipped.
	if pct := deltaPct(&baseline.ThroughputRPM, &after.ThroughputRPM); after.RequestCount > 0 && pct != nil && -*pct >= throughputDropPct-thresholdEpsilon {
		found = append(found, s.regression(MetricThroughputRPM, baseline.ThroughputRPM, after.ThroughputRPM,
			fmt.Sprintf("%s throughput fell %.0f%% (%.1f → %.1f rpm) (threshold %g%%)", s.name, -*pct, baseline.ThroughputRPM, after.ThroughputRPM, throughputDropPct)))
	}
	return found
}

type verdictInputs struct {
	afterMinutes int
	// regressions are the triggers found, most important first.
	regressions []Regression
	// unavailable names each required section that failed, with its cause.
	unavailable     []string
	haveChanged     bool
	changedBaseline apm.GoldenSignals
	changedAfter    apm.GoldenSignals
	service         string
}

func decideVerdict(in verdictInputs) (verdict, reason, note string) {
	if in.afterMinutes < minAfterMinutes {
		return VerdictInsufficientData, fmt.Sprintf("after window is %d min; at least %d min after the change is needed", max(in.afterMinutes, 0), minAfterMinutes), ""
	}
	if len(in.regressions) > 0 {
		reason = in.regressions[0].trigger
		if extra := len(in.regressions) - 1; extra > 0 {
			reason += fmt.Sprintf(" (+%d more)", extra)
		}
		if len(in.unavailable) > 0 {
			reason += "; " + strings.Join(in.unavailable, "; ")
		}
		return VerdictRegressionLikely, reason, ""
	}
	// Missing evidence can hide a regression, so it never clears the change.
	if len(in.unavailable) > 0 {
		return VerdictInsufficientData, "cannot rule the change out: " + strings.Join(in.unavailable, "; "), ""
	}
	if !in.haveChanged || in.changedAfter.RequestCount == 0 {
		return VerdictInsufficientData, fmt.Sprintf("%s served no requests in the after window", orService(in.service)), ""
	}
	if in.changedBaseline.RequestCount < minRequestsPerWindow || in.changedAfter.RequestCount < minRequestsPerWindow {
		return VerdictInsufficientData, fmt.Sprintf("%s served fewer than %d requests in a window, too few to judge", orService(in.service), minRequestsPerWindow), ""
	}
	return VerdictNoSignificantChange, "no threshold crossed: error rate, p95 latency, throughput, alerts and APM deviations are within bounds", noChangeNote
}

func orService(s string) string {
	if strings.TrimSpace(s) == "" {
		return "the service"
	}
	return s
}

// latencyEnv is the environment whose latency both windows compare: of those
// reporting latency in either window, the one serving the most requests over
// both, ties by name. Picking per window could compare two environments.
func latencyEnv(baseline, after apm.GoldenSignals) string {
	envs := slices.Sorted(maps.Keys(baseline.LatencyByEnv))
	envs = append(envs, slices.Sorted(maps.Keys(after.LatencyByEnv))...)
	best, bestRequests := "", -1.0
	for _, env := range envs {
		requests := baseline.RequestsByEnv[env] + after.RequestsByEnv[env]
		if requests > bestRequests || (requests == bestRequests && env < best) {
			best, bestRequests = env, requests
		}
	}
	return best
}

// deltaPct is (after-baseline)/baseline as a percentage; nil when either side
// is missing or the baseline is zero.
func deltaPct(baseline, after *float64) *float64 {
	if baseline == nil || after == nil || *baseline == 0 {
		return nil
	}
	v := (*after - *baseline) / math.Abs(*baseline) * 100
	return &v
}
