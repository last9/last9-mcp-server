package alerting

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
)

const (
	evaluationHistorySamples = 5
	evaluationHistoryStep    = time.Minute
	// Bounds the response: every enabled rule adds a line of samples.
	evaluationHistoryMaxRules = 25
)

const (
	evaluationFiring  = "firing"
	evaluationOK      = "ok"
	evaluationUnknown = "unknown"
)

type evaluationSample struct {
	Minute time.Time
	State  string
}

// attachRecentEvaluations sets RecentEvaluations on every enabled rule and returns a note for
// the response when history was skipped or some minutes could not be read.
func attachRecentEvaluations(ctx context.Context, api *last9api.Client, rules AlertConfigResponse, now time.Time) string {
	enabled := make([]int, 0, len(rules))
	for i, rule := range rules {
		if !ruleIsDisabled(rule) {
			enabled = append(enabled, i)
		}
	}
	if len(enabled) == 0 {
		return ""
	}
	if len(enabled) > evaluationHistoryMaxRules {
		return fmt.Sprintf(
			"Recent Evaluations omitted: %d enabled rules matched, more than the %d that get history; narrow the filters (e.g. rule_id or rule_name) to see them.\n\n",
			len(enabled), evaluationHistoryMaxRules,
		)
	}

	minutes := evaluationMinutes(now)
	states, errs := fetchMonitorStates(ctx, api, minutes)
	for _, i := range enabled {
		samples := make([]evaluationSample, len(minutes))
		for m, minute := range minutes {
			state, ok := states[m][rules[i].ID]
			if !ok {
				state = evaluationUnknown
			}
			samples[m] = evaluationSample{Minute: minute, State: state}
		}
		rules[i].RecentEvaluations = samples
	}

	var failed int
	var firstErr error
	for _, err := range errs {
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if failed == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Recent Evaluations: the alert monitor lookup failed for %d of %d minutes, shown as unknown: %v\n\n",
		failed, len(minutes), firstErr,
	)
}

// evaluationMinutes returns the end of each sampled minute, newest first. It skips the minute
// in progress and the one before it: the monitor lags, so the newest minute often has no rows.
func evaluationMinutes(now time.Time) []time.Time {
	newest := now.UTC().Truncate(evaluationHistoryStep).Add(-evaluationHistoryStep)
	minutes := make([]time.Time, evaluationHistorySamples)
	for i := range minutes {
		minutes[i] = newest.Add(-time.Duration(i) * evaluationHistoryStep)
	}
	return minutes
}

// fetchMonitorStates reads every minute concurrently; states[i] maps rule ID to its normalized
// state in minutes[i], and errs[i] is that minute's lookup error.
func fetchMonitorStates(ctx context.Context, api *last9api.Client, minutes []time.Time) (states []map[string]string, errs []error) {
	states = make([]map[string]string, len(minutes))
	errs = make([]error, len(minutes))
	var wg sync.WaitGroup
	for i, minute := range minutes {
		wg.Go(func() {
			resp, err := alerts.GetMonitor(ctx, api, alerts.GetMonitorInput{
				Timestamp: minute.Unix(),
				Window:    int64(evaluationHistoryStep / time.Second),
			})
			if err != nil {
				errs[i] = err
				return
			}
			states[i] = monitorRuleStates(resp)
		})
	}
	wg.Wait()
	return states, errs
}

// monitorRuleStates folds the monitor's rows, one per rule and severity, into one state per
// rule: firing over unknown over ok, so a firing severity is never hidden by a resolved one.
func monitorRuleStates(resp alerts.GetMonitorResponse) map[string]string {
	rank := map[string]int{evaluationOK: 0, evaluationUnknown: 1, evaluationFiring: 2}
	states := make(map[string]string, len(resp.AlertRules))
	for _, row := range resp.AlertRules {
		state := normalizeMonitorState(row.State)
		if existing, ok := states[row.RuleID]; ok && rank[existing] >= rank[state] {
			continue
		}
		states[row.RuleID] = state
	}
	return states
}

// normalizeMonitorState maps the monitor's rule states onto firing/ok, matching
// get_alert_rule_state's is_firing; states it does not know read as unknown.
func normalizeMonitorState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "firing":
		return evaluationFiring
	case "", "normal", "resolved", "pending":
		return evaluationOK
	default:
		return evaluationUnknown
	}
}

func formatRecentEvaluations(rule AlertRule) string {
	if ruleIsDisabled(rule) {
		return "  Recent Evaluations: none, the rule is disabled\n"
	}
	if len(rule.RecentEvaluations) == 0 {
		return ""
	}
	parts := make([]string, len(rule.RecentEvaluations))
	for i, s := range rule.RecentEvaluations {
		parts[i] = s.Minute.UTC().Format("2006-01-02T15:04Z") + " " + s.State
	}
	return "  Recent Evaluations (1-minute samples, minute ending UTC, newest first): " + strings.Join(parts, ", ") + "\n"
}
