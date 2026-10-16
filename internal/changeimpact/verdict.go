package changeimpact

import (
	"fmt"
	"math"
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
	// least this many server requests.
	minRequestsPerWindow = 20
	// Error rate rising by at least this many percentage points is a regression.
	errorRateIncreasePP = 1.0
	// p95 latency rising by at least this percentage is a regression.
	p95LatencyIncreasePct = 25.0
	// Any alert for the service or a dependent that starts firing in the after
	// window is a regression, as is any regression get_apm_service_deviations
	// classifies for the changed service.
)

const noChangeNote = "No significant change is a valid answer, not a failure: the change did not move error rate, p95 latency or alerts past the thresholds."

// thresholdEpsilon absorbs float noise in deltas computed exactly at a threshold.
const thresholdEpsilon = 1e-9

// serviceRegressions lists the thresholds service crossed between the windows.
func serviceRegressions(service string, baseline, after apm.GoldenSignals) []string {
	if baseline.RequestCount < minRequestsPerWindow || after.RequestCount < minRequestsPerWindow {
		return nil
	}
	var found []string
	if baseline.ErrorRatePct != nil && after.ErrorRatePct != nil {
		if delta := *after.ErrorRatePct - *baseline.ErrorRatePct; delta >= errorRateIncreasePP-thresholdEpsilon {
			found = append(found, fmt.Sprintf("%s error rate rose %.2f pp (%.2f%% → %.2f%%)", service, delta, *baseline.ErrorRatePct, *after.ErrorRatePct))
		}
	}
	if pct := deltaPct(baseline.LatencyP95Ms, after.LatencyP95Ms); pct != nil && *pct >= p95LatencyIncreasePct-thresholdEpsilon {
		found = append(found, fmt.Sprintf("%s p95 latency rose %.0f%% (%.1f ms → %.1f ms)", service, *pct, *baseline.LatencyP95Ms, *after.LatencyP95Ms))
	}
	return found
}

type verdictInputs struct {
	afterMinutes int
	// triggers are the regressions found, most important first.
	triggers []string
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
	if len(in.triggers) > 0 {
		reason = in.triggers[0]
		if extra := len(in.triggers) - 1; extra > 0 {
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
	return VerdictNoSignificantChange, "no threshold crossed: error rate, p95 latency, alerts and APM deviations are within bounds", noChangeNote
}

func orService(s string) string {
	if strings.TrimSpace(s) == "" {
		return "the service"
	}
	return s
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
