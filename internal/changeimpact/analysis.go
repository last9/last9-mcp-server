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

	"last9-mcp/internal/alerting"
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
	Service       string             `json:"service"`
	Env           string             `json:"env,omitempty"`
	ChangeTime    string             `json:"change_time"`
	Verdict       string             `json:"verdict"`
	Reason        string             `json:"reason"`
	Note          string             `json:"note,omitempty"`
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
	LatencyP50Ms  SignalChange `json:"latency_p50_ms"`
	LatencyP95Ms  SignalChange `json:"latency_p95_ms"`
	LatencyP99Ms  SignalChange `json:"latency_p99_ms"`
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
	Error   string         `json:"error,omitempty"`
	Skipped string         `json:"skipped,omitempty"`
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
		Notes:      notes,
		Service:    req.Service,
		Env:        req.Env,
		ChangeTime: req.Change.UTC().Format(time.RFC3339),
		Windows:    w.public(),
		DrillDown:  drillDowns(req, w, cfg.AllowedTools),
		Thresholds: map[string]float64{
			"min_after_minutes":        minAfterMinutes,
			"min_requests_per_window":  minRequestsPerWindow,
			"error_rate_increase_pp":   errorRateIncreasePP,
			"p95_latency_increase_pct": p95LatencyIncreasePct,
		},
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
		apmEntries            []apm.LeaderboardEntry
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
		apmEntries, apmErr = withTimeout(ctx, func(ctx context.Context) ([]apm.LeaderboardEntry, error) {
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

	var triggers []string
	goldenErr := firstErr(baselineErr, afterErr)
	if goldenErr != nil {
		report.GoldenSignals.Error = goldenErr.Error()
	} else {
		for i, service := range scope {
			role := "dependent"
			if i == 0 {
				role = "changed"
			}
			report.GoldenSignals.Services = append(report.GoldenSignals.Services, compareSignals(service, role, baseline[service], after[service]))
		}
		triggers = append(triggers, serviceRegressions(req.Service, baseline[req.Service], after[req.Service])...)
	}

	if alertsErr != nil {
		report.Alerts.Error = alertsErr.Error()
	} else {
		for _, a := range alertsStarted {
			trigger := fmt.Sprintf("alert %q started firing for %s at %s", firstNonEmpty(a.RuleName, a.RuleID), a.Service, a.StartedAt)
			if a.EnvMatch == envMatchUnlabelled {
				trigger += " (no env label)"
			}
			triggers = append(triggers, trigger)
		}
		report.Alerts.Started, report.Alerts.Dropped = truncate(alertsStarted, sectionTopN)
	}

	if goldenErr == nil {
		for _, service := range scope[1:] {
			triggers = append(triggers, serviceRegressions(service, baseline[service], after[service])...)
		}
	}

	if apmErr != nil {
		report.Deviations.APM.Error = apmErr.Error()
	} else {
		var apmTriggers []string
		report.Deviations.APM, apmTriggers = summarizeAPMDeviations(apmEntries)
		triggers = append(triggers, apmTriggers...)
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

	in := verdictInputs{afterMinutes: w.afterMinutes, triggers: triggers, service: req.Service}
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
	if goldenErr == nil {
		in.changedBaseline, in.changedAfter = baseline[req.Service], after[req.Service]
		_, in.haveChanged = after[req.Service]
	}
	report.Verdict, report.Reason, report.Note = decideVerdict(in)
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

func truncate[T any](items []T, limit int) ([]T, int) {
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
	return ServiceSignals{
		Service:       service,
		Role:          role,
		ThroughputRPM: change(&baseRPM, &afterRPM),
		ErrorRatePct:  errorRate,
		LatencyP50Ms:  change(baseline.LatencyP50Ms, after.LatencyP50Ms),
		LatencyP95Ms:  change(baseline.LatencyP95Ms, after.LatencyP95Ms),
		LatencyP99Ms:  change(baseline.LatencyP99Ms, after.LatencyP99Ms),
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
func summarizeAPMDeviations(entries []apm.LeaderboardEntry) (APMDeviations, []string) {
	var section APMDeviations
	var triggers []string
	items := make([]APMDeviation, 0, len(entries))
	for _, e := range entries {
		items = append(items, apmDeviation(e))
		if e.Comparison.Classification == "regression" {
			triggers = append(triggers, fmt.Sprintf("get_apm_service_deviations classifies %s %s as a regression", e.ServiceName, e.Comparison.Definition.Name))
		}
	}
	section.Items, section.Dropped = truncate(items, sectionTopN)
	return section, triggers
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

// envLabels are the group labels alert rules use to name an environment.
var envLabels = []string{"env", "environment", "deployment_environment", "deployment.environment"}

type alertScope struct {
	window   apm.TimeWindow
	services []string
	// env is exact; empty means every environment.
	env string
}

// fetchStartedAlerts returns alerts for scope that started firing inside
// its window, earliest first. The monitor call caps its window, so a longer
// window is read in consecutive slices.
func fetchStartedAlerts(ctx context.Context, client *http.Client, cfg models.Config, scope alertScope) ([]StartedAlert, error) {
	window := scope.window
	seen := map[string]bool{}
	var started []StartedAlert
	for end := window.End.Unix(); end > window.Start.Unix(); end -= alerting.MaxAlertsMonitorWindowSeconds {
		span := min(end-window.Start.Unix(), alerting.MaxAlertsMonitorWindowSeconds)
		resp, err := alerting.FetchAlertsMonitor(ctx, client, cfg, alerting.AlertsMonitorQuery{Timestamp: end, Window: span})
		if err != nil {
			return nil, err
		}
		for _, rule := range resp.AlertRules {
			for _, a := range startedInScope(rule, scope) {
				key := a.RuleID + "\x00" + a.Service + "\x00" + a.StartedAt
				if !seen[key] {
					seen[key] = true
					started = append(started, a)
				}
			}
		}
	}
	sort.SliceStable(started, func(i, j int) bool {
		if started[i].startedUnix != started[j].startedUnix {
			return started[i].startedUnix < started[j].startedUnix
		}
		return started[i].RuleID < started[j].RuleID
	})
	return started, nil
}

// startedInScope matches a rule to a scoped service by an exact group-label
// value or by its alert group's name, and keeps instances whose firing began
// inside the window. An instance without its own start falls back to the
// rule's; a rule with no instances has nothing firing and yields none. With an
// env, instances labelled with another env are dropped.
func startedInScope(rule alerting.AlertRuleData, scope alertScope) []StartedAlert {
	window := scope.window
	var out []StartedAlert
	for _, inst := range rule.Alerts {
		since := inst.Since
		if since == 0 {
			since = rule.Since
		}
		if since < window.Start.Unix() || since > window.End.Unix() {
			continue
		}
		service, ok := matchService(rule, inst, scope.services)
		if !ok {
			continue
		}
		envMatch, ok := matchEnv(inst, scope.env)
		if !ok {
			continue
		}
		out = append(out, StartedAlert{
			RuleID: rule.RuleID, RuleName: rule.RuleName, AlertGroup: rule.AlertGroupName,
			Severity: rule.Severity, State: firstNonEmpty(inst.State, rule.State),
			StartedAt: time.Unix(since, 0).UTC().Format(time.RFC3339), Service: service,
			EnvMatch: envMatch, startedUnix: since,
		})
	}
	return out
}

func matchService(rule alerting.AlertRuleData, inst alerting.AlertInstance, scope []string) (string, bool) {
	for _, service := range scope {
		if strings.EqualFold(rule.AlertGroupName, service) {
			return service, true
		}
		for _, v := range inst.GroupLabels {
			if fmt.Sprint(v) == service {
				return service, true
			}
		}
	}
	return "", false
}

// matchEnv rejects an instance whose env label names another environment. One
// without any env label is kept but marked unlabelled, not claimed as a match.
func matchEnv(inst alerting.AlertInstance, env string) (string, bool) {
	if env == "" {
		return "", true
	}
	labelled := false
	for _, key := range envLabels {
		v, ok := inst.GroupLabels[key]
		if !ok {
			continue
		}
		if fmt.Sprint(v) != env {
			return "", false
		}
		labelled = true
	}
	if !labelled {
		return envMatchUnlabelled, true
	}
	return envMatchExact, true
}
