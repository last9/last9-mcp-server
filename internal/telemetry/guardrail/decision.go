package guardrail

import (
	"fmt"
	"time"

	"last9-mcp/internal/models"
)

type Action string

const (
	Allow  Action = "allow"
	Warn   Action = "warn"
	Reject Action = "reject"
)

// Reason codes are a stable contract: shadow telemetry and result hints count them.
const (
	ReasonRangeTooWide        = "range_too_wide"
	ReasonUnscopedQuery       = "unscoped_query"
	ReasonScanBudgetExceeded  = "scan_budget_exceeded"
	ReasonSessionBudget80     = "session_budget_80pct"
	ReasonEstimateUnavailable = "estimate_unavailable"
	ReasonInvalidMode         = "invalid_mode"
)

const serviceFilterExample = `{"$eq":["ServiceName","<service>"]}`

type Decision struct {
	Action   Action
	Reason   string
	Guidance string
}

type scan struct {
	Range  time.Duration
	Scoped bool // a filter detected in the pipeline itself
	// ServerScoped is the estimate's own scope verdict. It waives the scope
	// threshold but never the range cap, the one limit an over-eager verdict
	// must not lift.
	ServerScoped bool
	Estimate     *int64 // nil when the estimate is unavailable
}

func (s scan) anyScope() bool {
	return s.Scoped || s.ServerScoped
}

// decide runs the range, scope and scan-budget checks in that order and
// returns the first reject. Without an estimate only the range check runs,
// so an unscoped query within the cap, and every scoped query, is allowed:
// the scope threshold and the budget need the estimate's bytes.
func decide(l models.QueryGuardrail, s scan) Decision {
	if !s.Scoped && s.Range > l.RangeCap {
		return Decision{Reject, ReasonRangeTooWide, fmt.Sprintf(
			"The query spans %s with no service or stream filter; unscoped queries are capped at %s. Narrow the window to %s or less, or add a service filter such as %s.",
			formatDuration(s.Range), formatDuration(l.RangeCap), formatDuration(l.RangeCap), serviceFilterExample)}
	}
	if s.Estimate == nil {
		return Decision{Action: Allow}
	}
	bytes := *s.Estimate
	if !s.anyScope() && bytes > l.ScopeThresholdBytes {
		return Decision{Reject, ReasonUnscopedQuery, fmt.Sprintf(
			"The query would scan about %s with no service or stream filter; above %s one is required. Add a service filter such as %s. To find the service, first count by ServiceName over a short window.",
			formatBytes(bytes), formatBytes(l.ScopeThresholdBytes), serviceFilterExample)}
	}
	if bytes > l.QueryBudgetBytes {
		return Decision{Reject, ReasonScanBudgetExceeded, budgetGuidance(l, s, bytes)}
	}
	return Decision{Action: Allow}
}

func budgetGuidance(l models.QueryGuardrail, s scan, bytes int64) string {
	g := fmt.Sprintf("The query would scan about %s, over the %s per-query budget.", formatBytes(bytes), formatBytes(l.QueryBudgetBytes))
	if !s.anyScope() {
		g += " Add a service filter such as " + serviceFilterExample + "."
	}
	if window := fittingWindow(s.Range, l.QueryBudgetBytes, bytes); window > 0 {
		g += fmt.Sprintf(" Narrow the window to the last %s.", formatDuration(window))
	}
	return g + " Or aggregate first (a count, or a group-by on a low-cardinality key such as ServiceName) and drill into what stands out."
}

// fittingWindow scales the range down to the share of it the budget covers,
// rounded down to a whole hour or minute; 0 when that is under a minute.
func fittingWindow(rng time.Duration, budget, bytes int64) time.Duration {
	window := time.Duration(float64(rng) * float64(budget) / float64(bytes))
	if window >= time.Hour {
		return window.Truncate(time.Hour)
	}
	return window.Truncate(time.Minute)
}

// sessionDecision warns once a session's cumulative estimate reaches 80% of
// its budget. It never rejects: the budget is advisory.
func sessionDecision(budget, used int64) Decision {
	if budget <= 0 || used*5 < budget*4 {
		return Decision{Action: Allow}
	}
	return Decision{Warn, ReasonSessionBudget80, fmt.Sprintf(
		"This session's queries have scanned about %s of its %s budget (%d%%). Prefer aggregates and narrower windows for the remaining questions.",
		formatBytes(used), formatBytes(budget), used*100/budget)}
}

func formatDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.Round(time.Second).String()
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 3 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exp])
}
