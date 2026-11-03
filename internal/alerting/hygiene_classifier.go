package alerting

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/last9api/alerts"
)

const (
	hygieneNoisyMinFiresPerDay    = 5.0
	hygieneNoisyMaxAckRate        = 0.2
	hygieneFlappingMinFlapsPerDay = 4.0

	// Rates over less than a day of history would be extrapolated from a handful of fires.
	hygieneMinObservedDuration = 24 * time.Hour

	hygieneAdaptiveSensitivity = 3
	hygieneAdaptiveWindow      = "1h"
)

const (
	hygieneCategoryNoisy    = "noisy"
	hygieneCategoryFlapping = "flapping"
	hygieneCategoryStale    = "stale"
	hygieneCategoryUnrouted = "unrouted"
)

var (
	badMinutesPattern      = regexp.MustCompile(`count_true\(result\)\s*(>=|>)\s*(\d+)`)
	staticConditionPattern = regexp.MustCompile(`^\s*expr\s*(>=|>|<=|<)\s*([-+]?[0-9]*\.?[0-9]+)\s*$`)
)

// hygieneRule joins a rule's stats with its configuration from the alert rules list,
// which the hygiene API does not send. Config is nil when the rule is not in that
// list; its fixes then carry no current values.
type hygieneRule struct {
	alerts.HygieneRule
	Config *AlertRule
}

func joinHygieneRuleConfig(rules []alerts.HygieneRule, configs AlertConfigResponse) []hygieneRule {
	byID := make(map[string]*AlertRule, len(configs))
	for i := range configs {
		byID[configs[i].ID] = &configs[i]
	}
	joined := make([]hygieneRule, len(rules))
	for i, rule := range rules {
		joined[i] = hygieneRule{HygieneRule: rule, Config: byID[rule.RuleID]}
	}
	return joined
}

func (r hygieneRule) config() AlertRule {
	if r.Config == nil {
		return AlertRule{}
	}
	return *r.Config
}

func (r hygieneRule) disabled() bool {
	return r.Config != nil && r.Config.IsDisabled != nil && *r.Config.IsDisabled
}

// hygieneWindow is the requested period; Observed is the part lifecycle history covered.
type hygieneWindow struct {
	Label             string
	From, To          time.Time
	Observed          time.Duration
	LifecycleComplete bool
}

func newHygieneWindow(label string, resp *alerts.GetAlertHygieneResponse) hygieneWindow {
	w := hygieneWindow{
		Label: label, From: resp.Period.From, To: resp.Period.To,
		Observed:          resp.Period.To.Sub(resp.Period.From),
		LifecycleComplete: resp.Coverage.Lifecycle.Status == alerts.HygieneStatusComplete,
	}
	if r := resp.Coverage.Lifecycle.Range; r != nil && !w.LifecycleComplete {
		w.Observed = r.To.Sub(r.From)
	}
	return w
}

func (w hygieneWindow) observedDays() float64 {
	return max(w.Observed, hygieneMinObservedDuration).Hours() / 24
}

// span names what the verdicts read, so a partly covered window never claims a
// rule was silent for the whole window.
func (w hygieneWindow) span() string {
	if !w.LifecycleComplete {
		return fmt.Sprintf("%.1f days of the %s window covered by alert history", math.Round(w.Observed.Hours()/24*10)/10, w.Label)
	}
	return w.Label + " window"
}

type hygieneFix struct {
	Parameter string `json:"parameter"`
	Current   string `json:"current,omitempty"`
	Proposed  string `json:"proposed"`
}

type hygieneFinding struct {
	Category       string      `json:"category"`
	Reason         string      `json:"reason"`
	SuggestedFix   hygieneFix  `json:"suggested_fix"`
	AlternativeFix *hygieneFix `json:"alternative_fix,omitempty"`
}

type hygieneVerdict struct {
	Findings []hygieneFinding   `json:"findings"`
	Stats    alerts.HygieneRule `json:"stats"`
}

// hygieneUnavailable counts enabled rules per value the API or the alert rules list
// could not supply, each of which skips or weakens a check, so a short list of
// findings is never mistaken for a clean bill of health.
type hygieneUnavailable struct {
	FireCount  int `json:"fire_count"`
	FlapCount  int `json:"flap_count"`
	AckRate    int `json:"ack_rate"`
	Routed     int `json:"routed"`
	RuleConfig int `json:"rule_config"`
}

func countUnavailable(rules []hygieneRule) hygieneUnavailable {
	var n hygieneUnavailable
	for _, rule := range rules {
		if rule.disabled() {
			continue
		}
		if rule.FireCount.Count == nil {
			n.FireCount++
		}
		if rule.FlapCount.Count == nil {
			n.FlapCount++
		}
		if rule.AckRate.Value == nil {
			n.AckRate++
		}
		if rule.Routed == nil {
			n.Routed++
		}
		if rule.Config == nil {
			n.RuleConfig++
		}
	}
	return n
}

// rankAlertHygiene returns only flagged rules: most categories first, then the
// loudest (fire count, then flaps), then by name and id so the order is stable.
func rankAlertHygiene(rules []hygieneRule, w hygieneWindow) []hygieneVerdict {
	verdicts := []hygieneVerdict{}
	for _, rule := range rules {
		if findings := classifyHygieneRule(rule, w); len(findings) > 0 {
			verdicts = append(verdicts, hygieneVerdict{Findings: findings, Stats: rule.HygieneRule})
		}
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		a, b := verdicts[i], verdicts[j]
		if len(a.Findings) != len(b.Findings) {
			return len(a.Findings) > len(b.Findings)
		}
		if fa, fb := countOrZero(a.Stats.FireCount), countOrZero(b.Stats.FireCount); fa != fb {
			return fa > fb
		}
		if fa, fb := valueOrZero(a.Stats.FlapsPerDay), valueOrZero(b.Stats.FlapsPerDay); fa != fb {
			return fa > fb
		}
		if a.Stats.RuleName != b.Stats.RuleName {
			return a.Stats.RuleName < b.Stats.RuleName
		}
		return a.Stats.RuleID < b.Stats.RuleID
	})
	return verdicts
}

func countOrZero(m alerts.HygieneMetric) int64 {
	if m.Count == nil {
		return 0
	}
	return *m.Count
}

func valueOrZero(v alerts.HygieneValue) float64 {
	if v.Value == nil {
		return 0
	}
	return *v.Value
}

// Disabled rules are skipped: tuning or routing a rule that cannot fire changes nothing.
func classifyHygieneRule(rule hygieneRule, w hygieneWindow) []hygieneFinding {
	if rule.disabled() {
		return []hygieneFinding{}
	}
	findings := []hygieneFinding{}
	for _, classify := range []func(hygieneRule, hygieneWindow) *hygieneFinding{
		noisyFinding, flappingFinding, staleFinding, unroutedFinding,
	} {
		if f := classify(rule, w); f != nil {
			findings = append(findings, *f)
		}
	}
	return findings
}

// An unavailable value never counts as zero. An unavailable ack rate judges the
// fire rate alone and says so, rather than leaving every loud rule unflagged.
// The API keeps no mute history, so muting is not checked.
func noisyFinding(rule hygieneRule, w hygieneWindow) *hygieneFinding {
	if rule.FireCount.Count == nil {
		return nil
	}
	fires := *rule.FireCount.Count
	perDay := float64(fires) / w.observedDays()
	var reason string
	switch {
	case perDay < hygieneNoisyMinFiresPerDay:
		return nil
	case rule.AckRate.Value == nil:
		reason = fmt.Sprintf(
			"fired %d times (%.1f/day; noisy at >= %.0f/day); ack rate is unavailable, so acknowledgement was not checked",
			fires, perDay, hygieneNoisyMinFiresPerDay)
	case *rule.AckRate.Value < hygieneNoisyMaxAckRate:
		reason = fmt.Sprintf(
			"fired %d times (%.1f/day) with %.0f%% of episodes acked (noisy at >= %.0f/day and < %.0f%% acked)",
			fires, perDay, *rule.AckRate.Value*100, hygieneNoisyMinFiresPerDay, hygieneNoisyMaxAckRate*100)
	default:
		return nil
	}
	fix, alt := noisyFixes(rule.config())
	return &hygieneFinding{
		Category:       hygieneCategoryNoisy,
		Reason:         reason,
		SuggestedFix:   fix,
		AlternativeFix: alt,
	}
}

// The API's flaps_per_day divides by the whole requested period, so a window longer
// than alert history dilutes it; the rate here uses the covered span, like noisy.
func flappingFinding(rule hygieneRule, w hygieneWindow) *hygieneFinding {
	if rule.FlapCount.Count == nil {
		return nil
	}
	flaps := *rule.FlapCount.Count
	perDay := float64(flaps) / w.observedDays()
	if perDay <= hygieneFlappingMinFlapsPerDay {
		return nil
	}
	fix, alt := sustainFixes(rule.config())
	return &hygieneFinding{
		Category: hygieneCategoryFlapping,
		Reason: fmt.Sprintf("flapped %d times over the %s (%.1f/day; flapping above %.0f/day)",
			flaps, w.span(), perDay, hygieneFlappingMinFlapsPerDay),
		SuggestedFix:   fix,
		AlternativeFix: alt,
	}
}

// A last fire before the window needs zero firing time in it: an episode still open
// from before the window reports that timestamp too. No recorded fire needs complete
// lifecycle coverage; otherwise the silence may just predate recording.
func staleFinding(rule hygieneRule, w hygieneWindow) *hygieneFinding {
	var reason string
	switch {
	case rule.LastFiredAt != nil && rule.LastFiredAt.Before(w.From) &&
		rule.TimeFiringSeconds.Count != nil && *rule.TimeFiringSeconds.Count == 0:
		reason = fmt.Sprintf("last fired %s, before the %s, which began %s",
			formatHygieneTime(*rule.LastFiredAt), w.span(), formatHygieneTime(w.From))
	case rule.LastFiredAt == nil && w.LifecycleComplete &&
		rule.FireCount.Count != nil && *rule.FireCount.Count == 0:
		reason = fmt.Sprintf("no recorded fire, and alert history covers the whole %s", w.span())
	default:
		return nil
	}
	return &hygieneFinding{
		Category:       hygieneCategoryStale,
		Reason:         reason,
		SuggestedFix:   hygieneFix{Parameter: "is_disabled", Current: "false", Proposed: "true"},
		AlternativeFix: &hygieneFix{Parameter: "rule", Proposed: "delete"},
	}
}

// An unavailable routed value means the API could not tell, so it never counts as unrouted.
func unroutedFinding(rule hygieneRule, _ hygieneWindow) *hygieneFinding {
	if rule.Routed == nil || *rule.Routed {
		return nil
	}
	return &hygieneFinding{
		Category: hygieneCategoryUnrouted,
		Reason:   fmt.Sprintf("alert group %s has no notification channel, so firings reach no one", rule.AlertGroupID),
		SuggestedFix: hygieneFix{
			Parameter: "notification_channel",
			Proposed:  fmt.Sprintf("attach a notification channel to alert group %s", rule.AlertGroupID),
		},
	}
}

// A static rule's threshold lives in its condition (`expr > 100`); its expression
// is only the indicator arithmetic the condition compares.
func noisyFixes(rule AlertRule) (hygieneFix, *hygieneFix) {
	if !isStaticThreshold(rule) {
		return sustainFixes(rule)
	}
	return thresholdFix(rule), adaptiveFix(rule)
}

func thresholdFix(rule AlertRule) hygieneFix {
	fix := hygieneFix{Parameter: "condition", Current: rule.Condition, Proposed: "move the threshold so the rule fires less often"}
	if m := staticConditionPattern.FindStringSubmatch(rule.Condition); m != nil {
		op, threshold := m[1], m[2]
		fix.Proposed = "raise the threshold above " + threshold
		if strings.HasPrefix(op, "<") {
			fix.Proposed = "lower the threshold below " + threshold
		}
	}
	return fix
}

// adaptive_std_cmp is an expression macro, not an algorithm value; a static rule
// already using it is tuned like an adaptive one.
func isStaticThreshold(rule AlertRule) bool {
	return strings.Contains(strings.ToLower(rule.Algorithm), "static") &&
		!strings.Contains(rule.Expression, "adaptive_std_cmp")
}

func adaptiveFix(rule AlertRule) *hygieneFix {
	metric := rule.Expression
	if strings.TrimSpace(metric) == "" {
		metric = "<metric>"
	}
	return &hygieneFix{
		Parameter: "expression",
		Current:   rule.Expression,
		Proposed:  fmt.Sprintf("adaptive_std_cmp(%s, %d, %s)", metric, hygieneAdaptiveSensitivity, hygieneAdaptiveWindow),
	}
}

// sustainFixes asks the condition to hold longer before firing: raise bad_minutes
// (the N in alert_condition "count_true(result) >= N") up to eval_window, else
// lengthen eval_window up to the maximum update_alert accepts, else move the threshold.
func sustainFixes(rule AlertRule) (hygieneFix, *hygieneFix) {
	window := windowFix(rule)
	badMinutes, ok := parseBadMinutes(rule.AlertCondition)
	if ok && rule.EvalWindow > 0 && badMinutes < rule.EvalWindow {
		return hygieneFix{
			Parameter: "bad_minutes",
			Current:   strconv.FormatInt(badMinutes, 10),
			Proposed:  strconv.FormatInt(min(2*badMinutes, rule.EvalWindow), 10),
		}, window
	}
	if window != nil {
		return *window, nil
	}
	fix := thresholdFix(rule)
	fix.Proposed += fmt.Sprintf(" (eval_window is at its %d-minute maximum)", maxEvalWindowMinutes)
	if isStaticThreshold(rule) {
		return fix, adaptiveFix(rule)
	}
	return fix, nil
}

// windowFix is nil when eval_window is already at the maximum: an unchanged or
// larger window is not a fix update_alert can apply.
func windowFix(rule AlertRule) *hygieneFix {
	if rule.EvalWindow <= 0 {
		return &hygieneFix{Parameter: "eval_window", Proposed: fmt.Sprintf("lengthen, up to %d", maxEvalWindowMinutes)}
	}
	if rule.EvalWindow >= maxEvalWindowMinutes {
		return nil
	}
	return &hygieneFix{
		Parameter: "eval_window",
		Current:   strconv.FormatInt(rule.EvalWindow, 10),
		Proposed:  strconv.FormatInt(min(2*rule.EvalWindow, maxEvalWindowMinutes), 10),
	}
}

func parseBadMinutes(condition string) (int64, bool) {
	m := badMinutesPattern.FindStringSubmatch(condition)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return 0, false
	}
	if m[1] == ">" {
		n++
	}
	return n, true
}

func formatHygieneTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
