package changeimpact

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/models"
)

// envLabels are the group labels alert rules use to name an environment.
var envLabels = []string{"env", "environment", "deployment_environment", "deployment.environment"}

// serviceLabels are the group labels alert rules use to name a service.
var serviceLabels = []string{"service", "service_name", "service.name"}

type alertScope struct {
	window   apm.TimeWindow
	services []string
	// env is exact; empty means every environment.
	env string
}

// fetchStartedAlerts returns alerts for scope that started firing inside
// its window, earliest first.
func fetchStartedAlerts(ctx context.Context, client *http.Client, cfg models.Config, scope alertScope) ([]StartedAlert, error) {
	seen := map[string]bool{}
	var started []StartedAlert
	for _, q := range alertMonitorSlices(scope.window) {
		resp, err := alerting.FetchAlertsMonitor(ctx, client, cfg, q)
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

// startedInScope matches a rule to a scoped service by a service group label
// or by its alert group's name, and keeps instances whose firing began
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

// matchService reads only service label keys: any other label (team,
// component, exported_job) can equal a service name by coincidence.
func matchService(rule alerting.AlertRuleData, inst alerting.AlertInstance, scope []string) (string, bool) {
	for _, service := range scope {
		if strings.EqualFold(rule.AlertGroupName, service) {
			return service, true
		}
		for _, key := range serviceLabels {
			if v, ok := inst.GroupLabels[key]; ok && strings.EqualFold(fmt.Sprint(v), service) {
				return service, true
			}
		}
	}
	return "", false
}

// matchEnv rejects an instance whose env label names another environment,
// ignoring case as matchService does. One without any env label is kept but
// marked unlabelled, not claimed as a match.
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
		if !strings.EqualFold(fmt.Sprint(v), env) {
			return "", false
		}
		labelled = true
	}
	if !labelled {
		return envMatchUnlabelled, true
	}
	return envMatchExact, true
}
