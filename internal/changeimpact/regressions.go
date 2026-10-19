package changeimpact

import (
	"fmt"
	"math"
	"strings"
)

// Regression metrics. last9/ai files one ticket per (service, env, endpoint,
// metric) and matches these strings exactly; every regression is service-wide,
// so it carries no endpoint.
const (
	MetricErrorRatePct  = "error_rate_pct"
	MetricLatencyP95Ms  = "latency_p95_ms"
	MetricThroughputRPM = "throughput_rpm"
	MetricAlert         = "alert"
	MetricAPMDeviation  = "apm_deviation"
)

// Roles of a service in the report, in golden signals and regressions alike.
const (
	RoleChanged   = "changed"
	RoleDependent = "dependent"
)

// Regression is one verdict trigger. Env is the environment the metric was
// measured in, empty when it sums every environment. Baseline and After are in
// the metric's unit (percent, ms, rpm, or the APM signal's own) and null for
// alerts, which instead name their rule and its state, so two alerts on one
// service stay apart and a filer can skip one that already resolved.
type Regression struct {
	Service  string   `json:"service"`
	Role     string   `json:"role"`
	Env      string   `json:"env"`
	Metric   string   `json:"metric"`
	RuleID   string   `json:"rule_id,omitempty"`
	State    string   `json:"state,omitempty"`
	Baseline *float64 `json:"baseline"`
	After    *float64 `json:"after"`

	// trigger is how reason states this regression; building both in one place
	// keeps the prose and the structure from drifting.
	trigger string
}

type scopedService struct {
	name string
	role string
	// env is the request's exact env; empty means every environment.
	env string
}

func (s scopedService) regression(metric string, baseline, after float64, trigger string) Regression {
	return Regression{Service: s.name, Role: s.role, Env: s.env, Metric: metric, Baseline: rounded(baseline), After: rounded(after), trigger: trigger}
}

func roleOf(service, changed string) string {
	if strings.EqualFold(service, changed) {
		return RoleChanged
	}
	return RoleDependent
}

func alertRegression(a StartedAlert, changed string) Regression {
	trigger := fmt.Sprintf("alert %q started firing for %s at %s", firstNonEmpty(a.RuleName, a.RuleID), a.Service, a.StartedAt)
	if a.EnvMatch == envMatchUnlabelled {
		trigger += " (no env label)"
	}
	return Regression{
		Service: a.Service, Role: roleOf(a.Service, changed), Env: a.env, Metric: MetricAlert,
		RuleID: a.RuleID, State: a.State, trigger: trigger,
	}
}

func apmRegression(d APMDeviation, changed string) Regression {
	return Regression{
		Service: d.Service, Role: roleOf(d.Service, changed), Env: d.Env, Metric: MetricAPMDeviation,
		Baseline: finite(d.Baseline), After: finite(d.After),
		trigger: fmt.Sprintf("get_apm_service_deviations classifies %s %s as a regression", d.Service, d.Signal),
	}
}

// finite drops a value JSON cannot carry, so a bad sample reads as missing
// instead of failing the whole report.
func finite(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
