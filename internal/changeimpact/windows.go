package changeimpact

import (
	"fmt"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"
)

const (
	defaultBaselineMinutes = 60
	minWindowMinutes       = 30
	maxWindowMinutes       = 120
)

type changeWindows struct {
	change                time.Time
	baseline, after       apm.TimeWindow
	baselineMinutes       int
	afterMinutes          int
	requestedAfterMinutes int
}

// normalizeRequest aligns the change to the minute and clamps both window
// lengths, returning a note for each value it changed.
func normalizeRequest(req Request) (Request, []string) {
	var notes []string
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	req.Now = req.Now.UTC()
	aligned := req.Change.UTC().Truncate(time.Minute)
	if !aligned.Equal(req.Change) {
		notes = append(notes, fmt.Sprintf("change time %s rounded down to the minute", req.Change.UTC().Format(time.RFC3339)))
	}
	req.Change = aligned

	if req.BaselineMinutes == 0 {
		req.BaselineMinutes = defaultBaselineMinutes
	} else if clamped := clampWindow(req.BaselineMinutes); clamped != req.BaselineMinutes {
		notes = append(notes, fmt.Sprintf("baseline_minutes %d clamped to %d (allowed %d..%d)", req.BaselineMinutes, clamped, minWindowMinutes, maxWindowMinutes))
		req.BaselineMinutes = clamped
	}
	if req.AfterMinutes == 0 {
		req.AfterMinutes = req.BaselineMinutes
	} else if clamped := clampWindow(req.AfterMinutes); clamped != req.AfterMinutes {
		notes = append(notes, fmt.Sprintf("after_minutes %d clamped to %d (allowed %d..%d)", req.AfterMinutes, clamped, minWindowMinutes, maxWindowMinutes))
		req.AfterMinutes = clamped
	}
	return req, notes
}

func clampWindow(minutes int) int {
	return min(max(minutes, minWindowMinutes), maxWindowMinutes)
}

// resolveWindows builds baseline = [T - baseline, T) and after = [T, T + after],
// with the after window capped at the last completed minute.
func resolveWindows(req Request) changeWindows {
	t := req.Change
	afterEnd := t.Add(time.Duration(req.AfterMinutes) * time.Minute)
	if completed := req.Now.Truncate(time.Minute); afterEnd.After(completed) {
		afterEnd = completed
	}
	afterMinutes := int(afterEnd.Sub(t) / time.Minute)
	if afterEnd.Before(t) {
		afterEnd = t
	}
	return changeWindows{
		change:                t,
		baseline:              apm.TimeWindow{Start: t.Add(-time.Duration(req.BaselineMinutes) * time.Minute), End: t},
		after:                 apm.TimeWindow{Start: t, End: afterEnd},
		baselineMinutes:       req.BaselineMinutes,
		afterMinutes:          afterMinutes,
		requestedAfterMinutes: req.AfterMinutes,
	}
}

func (w changeWindows) public() Windows {
	return Windows{
		BaselineStart:         rfc3339(w.baseline.Start),
		BaselineEnd:           rfc3339(w.baseline.End),
		AfterStart:            rfc3339(w.after.Start),
		AfterEnd:              rfc3339(w.after.End),
		BaselineMinutes:       w.baselineMinutes,
		AfterMinutes:          max(w.afterMinutes, 0),
		RequestedAfterMinutes: w.requestedAfterMinutes,
	}
}

// comparisonWindows returns equal-length current and baseline windows either
// side of the change, as the deviation tools require, at most limit long.
func (w changeWindows) comparisonWindows(limit time.Duration) (current, baseline apm.TimeWindow) {
	length := time.Duration(min(max(w.afterMinutes, 1), w.baselineMinutes)) * time.Minute
	length = min(length, limit)
	return apm.TimeWindow{Start: w.change, End: w.change.Add(length)}, apm.TimeWindow{Start: w.change.Add(-length), End: w.change}
}

// alertMonitorSlices splits window into consecutive monitor calls, latest
// first: one call accepts at most MaxAlertsMonitorWindowSeconds.
func alertMonitorSlices(window apm.TimeWindow) []alerting.AlertsMonitorQuery {
	var slices []alerting.AlertsMonitorQuery
	for end := window.End.Unix(); end > window.Start.Unix(); end -= alerting.MaxAlertsMonitorWindowSeconds {
		span := min(end-window.Start.Unix(), alerting.MaxAlertsMonitorWindowSeconds)
		slices = append(slices, alerting.AlertsMonitorQuery{Timestamp: end, Window: span})
	}
	return slices
}

func (w changeWindows) apmDeviationArgs(req Request) apm.DeviationArgs {
	current, baseline := w.comparisonWindows(maxWindowMinutes * time.Minute)
	return apm.DeviationArgs{
		ServiceName:      req.Service,
		Env:              req.Env,
		StartTimeISO:     rfc3339(current.Start),
		EndTimeISO:       rfc3339(current.End),
		BaselineStartISO: rfc3339(baseline.Start),
		BaselineEndISO:   rfc3339(baseline.End),
	}
}

func (w changeWindows) traceDeviationArgs(req Request) traces.GetTraceAttributeDeviationsArgs {
	current, baseline := w.comparisonWindows(traceDeviationMaxWindow)
	return traces.GetTraceAttributeDeviationsArgs{
		ComparisonMode:   "time",
		ServiceName:      req.Service,
		Environment:      req.Env,
		StartTimeISO:     rfc3339(current.Start),
		EndTimeISO:       rfc3339(current.End),
		BaselineStartISO: rfc3339(baseline.Start),
		BaselineEndISO:   rfc3339(baseline.End),
	}
}

// drillDowns names only tools this server serves: --toolsets can leave out,
// say, get_alerts while still serving analyze_change_impact.
func drillDowns(req Request, w changeWindows, allowed toolsets.Set) []DrillDown {
	withEnv := func(args map[string]any, key string) map[string]any {
		if req.Env != "" {
			args[key] = req.Env
		}
		return args
	}
	dev := w.apmDeviationArgs(req)
	out := []DrillDown{
		{
			Tool: "get_service_performance_details",
			Arguments: withEnv(map[string]any{
				"service_name": req.Service, "start_time_iso": rfc3339(w.baseline.Start), "end_time_iso": rfc3339(w.after.End),
			}, "env"),
			Why: "per-series summary (percentiles, change points) of the same signals across both windows, with top operations and errors",
		},
		{
			Tool: "get_apm_service_deviations",
			Arguments: withEnv(map[string]any{
				"service_name": req.Service, "start_time_iso": dev.StartTimeISO, "end_time_iso": dev.EndTimeISO,
				"baseline_start_time_iso": dev.BaselineStartISO, "baseline_end_time_iso": dev.BaselineEndISO,
			}, "env"),
			Why: "full deviation evidence, including the operations that moved",
		},
		{
			Tool: "get_service_traces",
			Arguments: withEnv(map[string]any{
				"service_name": req.Service, "start_time_iso": rfc3339(w.after.Start), "end_time_iso": rfc3339(w.after.End),
			}, "env"),
			Why: "sample traces from after the change",
		},
	}
	out = append(out, alertDrillDowns(w.after)...)
	if req.Env != "" {
		trace := w.traceDeviationArgs(req)
		out = append(out, DrillDown{
			Tool: "get_trace_attribute_deviations",
			Arguments: map[string]any{
				"comparison_mode": "time", "service_name": req.Service, "environment": req.Env,
				"start_time_iso": trace.StartTimeISO, "end_time_iso": trace.EndTimeISO,
				"baseline_start_time_iso": trace.BaselineStartISO, "baseline_end_time_iso": trace.BaselineEndISO,
			},
			Why: "which span attributes (version, host, route) changed share after the change",
		})
	}
	served := make([]DrillDown, 0, len(out))
	for _, d := range out {
		if allowed.Allows(d.Tool) {
			served = append(served, d)
		}
	}
	return served
}

// alertDrillDowns covers the after window with the same slices Analyze reads,
// so every alert it reports stays reachable through get_alerts.
func alertDrillDowns(after apm.TimeWindow) []DrillDown {
	after.Start = minTime(after.Start, after.End.Add(-time.Minute))
	slices := alertMonitorSlices(after)
	out := make([]DrillDown, 0, len(slices))
	for i, q := range slices {
		why := "every alert active after the change, not only those matched to these services"
		if len(slices) > 1 {
			why = fmt.Sprintf("%s (part %d of %d: get_alerts reads at most an hour per call)", why, i+1, len(slices))
		}
		out = append(out, DrillDown{
			Tool:      "get_alerts",
			Arguments: map[string]any{"time_iso": rfc3339(time.Unix(q.Timestamp, 0)), "window": q.Window},
			Why:       why,
		})
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
