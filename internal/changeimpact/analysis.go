// Package changeimpact answers "did the change at T break anything?" by
// comparing a service and its direct dependents before and after T.
package changeimpact

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/traces"
)

const (
	// maxDependents bounds how many direct callers get golden signals.
	maxDependents = 10
	// sectionTopN bounds each list in the report: deviations and alerts.
	sectionTopN = 10
	// sectionTimeout bounds each independent section so one slow upstream
	// cannot hold the whole report.
	sectionTimeout = 90 * time.Second
	// traceDeviationMaxWindow is the widest cohort get_trace_attribute_deviations accepts.
	traceDeviationMaxWindow = 15 * time.Minute
)

// Request is one change to analyze. Env is exact; empty means every
// environment. Zero window lengths take their defaults and others are clamped
// to 30..120 minutes; a zero Now means time.Now.
type Request struct {
	Service         string
	Env             string
	Change          time.Time
	BaselineMinutes int
	AfterMinutes    int
	Now             time.Time
}

type Report struct {
	Service    string `json:"service"`
	Env        string `json:"env,omitempty"`
	ChangeTime string `json:"change_time"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
	Note       string `json:"note,omitempty"`
	// Regressions lists each trigger behind a "regression likely" verdict, in
	// reason's order; it is empty for the other verdicts.
	Regressions   []Regression       `json:"regressions"`
	Windows       Windows            `json:"windows"`
	GoldenSignals GoldenSection      `json:"golden_signals"`
	Deviations    DeviationsSection  `json:"deviations"`
	Alerts        AlertsSection      `json:"alerts"`
	DrillDown     []DrillDown        `json:"drill_down"`
	Thresholds    map[string]float64 `json:"thresholds"`
	Notes         []string           `json:"notes,omitempty"`
	DashboardURL  string             `json:"dashboard_url"`
}

type Windows struct {
	BaselineStart         string `json:"baseline_start"`
	BaselineEnd           string `json:"baseline_end"`
	AfterStart            string `json:"after_start"`
	AfterEnd              string `json:"after_end"`
	BaselineMinutes       int    `json:"baseline_minutes"`
	AfterMinutes          int    `json:"after_minutes"`
	RequestedAfterMinutes int    `json:"requested_after_minutes"`
}

type SignalChange struct {
	Baseline *float64 `json:"baseline"`
	After    *float64 `json:"after"`
	DeltaPct *float64 `json:"delta_pct"`
	DeltaPP  *float64 `json:"delta_pp,omitempty"`
}

type ServiceSignals struct {
	Service       string       `json:"service"`
	Role          string       `json:"role"`
	ThroughputRPM SignalChange `json:"throughput_rpm"`
	ErrorRatePct  SignalChange `json:"error_rate_pct"`
	// LatencyEnv is the one environment both windows' latencies come from.
	LatencyEnv   string       `json:"latency_env,omitempty"`
	LatencyP50Ms SignalChange `json:"latency_p50_ms"`
	LatencyP95Ms SignalChange `json:"latency_p95_ms"`
	LatencyP99Ms SignalChange `json:"latency_p99_ms"`
}

type GoldenSection struct {
	Services          []ServiceSignals `json:"services"`
	DependentsTotal   int              `json:"dependents_total"`
	DependentsDropped int              `json:"dependents_dropped"`
	DependentsError   string           `json:"dependents_error,omitempty"`
	Error             string           `json:"error,omitempty"`
	Skipped           string           `json:"skipped,omitempty"`
}

type APMDeviation struct {
	Service        string   `json:"service"`
	Env            string   `json:"env,omitempty"`
	Category       string   `json:"category"`
	Signal         string   `json:"signal"`
	Classification string   `json:"classification"`
	Baseline       float64  `json:"baseline"`
	After          float64  `json:"after"`
	DeltaPct       *float64 `json:"delta_pct"`
}

type APMDeviations struct {
	Items   []APMDeviation `json:"items"`
	Dropped int            `json:"dropped"`
	// Unavailable names each service measurement that failed while the rest succeeded.
	Unavailable []string `json:"unavailable,omitempty"`
	Error       string   `json:"error,omitempty"`
	Skipped     string   `json:"skipped,omitempty"`
}

type TraceDeviations struct {
	Items    []traces.AttributeDeviation `json:"items"`
	Dropped  int                         `json:"dropped"`
	Warnings []string                    `json:"warnings,omitempty"`
	Error    string                      `json:"error,omitempty"`
	Skipped  string                      `json:"skipped,omitempty"`
}

type DeviationsSection struct {
	APM             APMDeviations   `json:"apm"`
	TraceAttributes TraceDeviations `json:"trace_attributes"`
}

type StartedAlert struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name,omitempty"`
	AlertGroup string `json:"alert_group,omitempty"`
	Severity   string `json:"severity,omitempty"`
	State      string `json:"state,omitempty"`
	StartedAt  string `json:"started_at"`
	Service    string `json:"service"`
	EnvMatch   string `json:"env_match,omitempty"`

	// env is the instance's env label, empty when it has none.
	env         string
	startedUnix int64
}

// EnvMatch values, set only when the request names an env.
const (
	envMatchExact      = "exact"
	envMatchUnlabelled = "unlabelled"
)

type AlertsSection struct {
	Started []StartedAlert `json:"started"`
	Dropped int            `json:"dropped"`
	Error   string         `json:"error,omitempty"`
	Skipped string         `json:"skipped,omitempty"`
}

type DrillDown struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Why       string         `json:"why"`
}

// Analyze compares req.Service and its direct dependents before and after
// req.Change. Upstream failures degrade their own section; Analyze never fails.
func Analyze(ctx context.Context, client *http.Client, cfg models.Config, req Request) Report {
	req, notes := normalizeRequest(req)
	w := resolveWindows(req)
	report := Report{
		Notes:         notes,
		Regressions:   []Regression{},
		Service:       req.Service,
		Env:           req.Env,
		ChangeTime:    req.Change.UTC().Format(time.RFC3339),
		Windows:       w.public(),
		DrillDown:     drillDowns(req, w, cfg.ToolFilter.Tools),
		Thresholds:    reportThresholds(),
		GoldenSignals: GoldenSection{Services: []ServiceSignals{}},
		Deviations: DeviationsSection{
			APM:             APMDeviations{Items: []APMDeviation{}},
			TraceAttributes: TraceDeviations{Items: []traces.AttributeDeviation{}},
		},
		Alerts: AlertsSection{Started: []StartedAlert{}},
		DashboardURL: deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAPMServiceLink(
			w.baseline.Start.UnixMilli(), w.after.End.UnixMilli(), req.Service, deeplink.APMCatalogEnvExact(req.Env), ""),
	}

	if w.afterMinutes < minAfterMinutes {
		skip := fmt.Sprintf("not queried: the after window is %d min, under the %d min minimum", max(w.afterMinutes, 0), minAfterMinutes)
		report.GoldenSignals.Skipped = skip
		report.Deviations.APM.Skipped = skip
		report.Deviations.TraceAttributes.Skipped = skip
		report.Alerts.Skipped = skip
		report.Verdict, report.Reason, report.Note = decideVerdict(verdictInputs{afterMinutes: w.afterMinutes})
		return report
	}

	scope := []string{req.Service}
	dependents, dependentsErr := withTimeout(ctx, func(ctx context.Context) ([]apm.Dependent, error) {
		return apm.QueryDirectDependents(ctx, client, cfg, apm.DependentsQuery{Service: req.Service, Env: req.Env, Window: apm.TimeWindow{Start: w.baseline.Start, End: w.after.End}})
	})
	if dependentsErr != nil {
		report.GoldenSignals.DependentsError = dependentsErr.Error()
	}
	report.GoldenSignals.DependentsTotal = len(dependents)
	if len(dependents) > maxDependents {
		report.GoldenSignals.DependentsDropped = len(dependents) - maxDependents
		dependents = dependents[:maxDependents]
	}
	for _, d := range dependents {
		scope = append(scope, d.Service)
	}

	var (
		baseline, after       map[string]apm.GoldenSignals
		baselineErr, afterErr error
		apmResult             apm.ServiceDeviations
		apmErr                error
		traceResult           traces.AttributeDeviations
		traceErr              error
		alertsStarted         []StartedAlert
		alertsErr             error
		wg                    sync.WaitGroup
		traceSkipped          string
	)
	wg.Go(func() {
		baseline, baselineErr = withTimeout(ctx, func(ctx context.Context) (map[string]apm.GoldenSignals, error) {
			return apm.QueryGoldenSignals(ctx, client, cfg, apm.GoldenSignalsQuery{Services: scope, Env: req.Env, Window: w.baseline})
		})
	})
	wg.Go(func() {
		after, afterErr = withTimeout(ctx, func(ctx context.Context) (map[string]apm.GoldenSignals, error) {
			return apm.QueryGoldenSignals(ctx, client, cfg, apm.GoldenSignalsQuery{Services: scope, Env: req.Env, Window: w.after})
		})
	})
	wg.Go(func() {
		apmResult, apmErr = withTimeout(ctx, func(ctx context.Context) (apm.ServiceDeviations, error) {
			return apm.QueryServiceDeviations(ctx, client, cfg, w.apmDeviationArgs(req))
		})
	})
	if req.Env == "" {
		traceSkipped = "not queried: trace attribute deviations need an exact env"
	} else {
		wg.Go(func() {
			traceResult, traceErr = withTimeout(ctx, func(ctx context.Context) (traces.AttributeDeviations, error) {
				return traces.QueryTraceAttributeDeviations(ctx, client, cfg, w.traceDeviationArgs(req))
			})
		})
	}
	wg.Go(func() {
		alertsStarted, alertsErr = withTimeout(ctx, func(ctx context.Context) ([]StartedAlert, error) {
			return fetchStartedAlerts(ctx, client, cfg, alertScope{window: w.after, services: scope, env: req.Env})
		})
	})
	wg.Wait()

	var regressions []Regression
	var alertsNotFiring int
	goldenErr := firstErr(baselineErr, afterErr)
	if goldenErr != nil {
		report.GoldenSignals.Error = goldenErr.Error()
	} else {
		for i, service := range scope {
			role := RoleDependent
			if i == 0 {
				role = RoleChanged
			}
			report.GoldenSignals.Services = append(report.GoldenSignals.Services, compareSignals(service, role, baseline[service], after[service]))
		}
		regressions = append(regressions, serviceRegressions(scopedService{name: req.Service, role: RoleChanged, env: req.Env}, baseline[req.Service], after[req.Service])...)
	}

	if alertsErr != nil {
		report.Alerts.Error = alertsErr.Error()
	} else {
		var firing []Regression
		firing, alertsNotFiring = alertRegressions(alertsStarted, req.Service)
		regressions = append(regressions, firing...)
		report.Alerts.Started, report.Alerts.Dropped = truncate(alertsStarted, sectionTopN)
	}

	if goldenErr == nil {
		for _, service := range scope[1:] {
			regressions = append(regressions, serviceRegressions(scopedService{name: service, role: RoleDependent, env: req.Env}, baseline[service], after[service])...)
		}
	}

	if apmErr != nil {
		report.Deviations.APM.Error = apmErr.Error()
	} else {
		var apmRegressions []Regression
		report.Deviations.APM, apmRegressions = summarizeAPMDeviations(apmResult.Entries, req.Service)
		report.Deviations.APM.Unavailable = apmResult.FailedQueries
		regressions = append(regressions, apmRegressions...)
	}

	switch {
	case traceSkipped != "":
		report.Deviations.TraceAttributes.Skipped = traceSkipped
	case traceErr != nil:
		report.Deviations.TraceAttributes.Error = traceErr.Error()
	default:
		ranked := append([]traces.AttributeDeviation(nil), traceResult.Deviations...)
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Rank < ranked[j].Rank })
		report.Deviations.TraceAttributes.Items, report.Deviations.TraceAttributes.Dropped = truncate(ranked, sectionTopN)
		report.Deviations.TraceAttributes.Warnings = traceResult.Warnings
	}

	in := verdictInputs{afterMinutes: w.afterMinutes, regressions: regressions, alertsNotFiring: alertsNotFiring, service: req.Service}
	// Trace deviations are left out: they explain a verdict and never trigger
	// one, so losing them cannot hide a regression the verdict would find.
	for _, s := range []struct {
		name string
		err  error
	}{
		{"golden signals", goldenErr},
		{"direct callers", dependentsErr},
		{"alerts", alertsErr},
		{"APM deviations", apmErr},
	} {
		if s.err != nil {
			in.unavailable = append(in.unavailable, fmt.Sprintf("%s unavailable: %v", s.name, s.err))
		}
	}
	if failed := report.Deviations.APM.Unavailable; len(failed) > 0 {
		in.unavailable = append(in.unavailable, "APM deviations partially unavailable: failed "+strings.Join(failed, ", "))
	}
	if goldenErr == nil {
		in.changedBaseline, in.changedAfter = baseline[req.Service], after[req.Service]
		_, in.haveChanged = after[req.Service]
	}
	report.Verdict, report.Reason, report.Note = decideVerdict(in)
	if report.Verdict == VerdictRegressionLikely {
		report.Regressions = regressions
	}
	return report
}

func withTimeout[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, sectionTimeout)
	defer cancel()
	return fn(ctx)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// truncate never returns nil: an empty list serialises as [], as it does when
// its section is skipped.
func truncate[T any](items []T, limit int) ([]T, int) {
	if items == nil {
		return []T{}, 0
	}
	if len(items) <= limit {
		return items, 0
	}
	return items[:limit], len(items) - limit
}

func compareSignals(service, role string, baseline, after apm.GoldenSignals) ServiceSignals {
	baseRPM, afterRPM := baseline.ThroughputRPM, after.ThroughputRPM
	errorRate := change(baseline.ErrorRatePct, after.ErrorRatePct)
	if baseline.ErrorRatePct != nil && after.ErrorRatePct != nil {
		errorRate.DeltaPP = rounded(*after.ErrorRatePct - *baseline.ErrorRatePct)
	}
	env := latencyEnv(baseline, after)
	before, now := baseline.LatencyByEnv[env], after.LatencyByEnv[env]
	return ServiceSignals{
		Service:       service,
		Role:          role,
		ThroughputRPM: change(&baseRPM, &afterRPM),
		ErrorRatePct:  errorRate,
		LatencyEnv:    env,
		LatencyP50Ms:  change(before.P50Ms, now.P50Ms),
		LatencyP95Ms:  change(before.P95Ms, now.P95Ms),
		LatencyP99Ms:  change(before.P99Ms, now.P99Ms),
	}
}

func change(baseline, after *float64) SignalChange {
	c := SignalChange{}
	if baseline != nil {
		c.Baseline = rounded(*baseline)
	}
	if after != nil {
		c.After = rounded(*after)
	}
	if pct := deltaPct(baseline, after); pct != nil {
		c.DeltaPct = rounded(*pct)
	}
	return c
}

func rounded(v float64) *float64 {
	r := math.Round(v*1000) / 1000
	return &r
}

// summarizeAPMDeviations bounds the entries for the report and lists each
// regression get_apm_service_deviations classifies as a verdict trigger.
func summarizeAPMDeviations(entries []apm.LeaderboardEntry, changed string) (APMDeviations, []Regression) {
	var section APMDeviations
	var regressions []Regression
	items := make([]APMDeviation, 0, len(entries))
	for _, e := range entries {
		d := apmDeviation(e)
		items = append(items, d)
		if d.Classification == "regression" {
			regressions = append(regressions, apmRegression(d, changed))
		}
	}
	section.Items, section.Dropped = truncate(items, sectionTopN)
	return section, regressions
}

func apmDeviation(e apm.LeaderboardEntry) APMDeviation {
	d := APMDeviation{
		Service:        e.ServiceName,
		Env:            e.Env,
		Category:       e.SignalCategory,
		Signal:         e.Comparison.Definition.Name,
		Classification: e.Comparison.Classification,
		Baseline:       e.Comparison.Baseline.Value,
		After:          e.Comparison.Current.Value,
	}
	if e.Comparison.RelativeDelta != nil {
		d.DeltaPct = rounded(*e.Comparison.RelativeDelta * 100)
	}
	return d
}
