package alerting

import (
	"fmt"
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
	hygieneNoisyMinMutedFraction  = 0.25
	hygieneFlappingMinFlapsPerDay = 4.0

	// Rates over less than a day of history would be extrapolated from a handful of fires.
	hygieneMinObservedSeconds = int64(24 * time.Hour / time.Second)

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

type hygieneWindow struct {
	Label    string
	From, To int64
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

// hygieneNullFields counts enabled rules per null field, each of which skips or
// (for ack) weakens a check, so a short list of findings is never mistaken for
// a clean bill of health.
type hygieneNullFields struct {
	Ack          int `json:"ack"`
	MutedSeconds int `json:"muted_seconds"`
	Routed       int `json:"routed"`
	HistorySince int `json:"history_since"`
}

func countNullFields(rules []alerts.HygieneRule) hygieneNullFields {
	var n hygieneNullFields
	for _, rule := range rules {
		if rule.IsDisabled {
			continue
		}
		if rule.Ack == nil {
			n.Ack++
		}
		if rule.MutedSeconds == nil {
			n.MutedSeconds++
		}
		if rule.Routed == nil {
			n.Routed++
		}
		if rule.HistorySince == nil {
			n.HistorySince++
		}
	}
	return n
}

// rankAlertHygiene returns only flagged rules: most categories first, then the
// loudest (fire count, then flaps), then by name and id so the order is stable.
func rankAlertHygiene(resp *alerts.GetAlertHygieneResponse) []hygieneVerdict {
	w := hygieneWindow{Label: resp.Window, From: resp.From, To: resp.To}
	verdicts := []hygieneVerdict{}
	for _, rule := range resp.Rules {
		if findings := classifyHygieneRule(rule, w); len(findings) > 0 {
			verdicts = append(verdicts, hygieneVerdict{Findings: findings, Stats: rule})
		}
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		a, b := verdicts[i], verdicts[j]
		if len(a.Findings) != len(b.Findings) {
			return len(a.Findings) > len(b.Findings)
		}
		if a.Stats.FireCount != b.Stats.FireCount {
			return a.Stats.FireCount > b.Stats.FireCount
		}
		if a.Stats.FlapsPerDay != b.Stats.FlapsPerDay {
			return a.Stats.FlapsPerDay > b.Stats.FlapsPerDay
		}
		if a.Stats.RuleName != b.Stats.RuleName {
			return a.Stats.RuleName < b.Stats.RuleName
		}
		return a.Stats.RuleID < b.Stats.RuleID
	})
	return verdicts
}

// Disabled rules are skipped: tuning or routing a rule that cannot fire changes nothing.
func classifyHygieneRule(rule alerts.HygieneRule, w hygieneWindow) []hygieneFinding {
	if rule.IsDisabled {
		return []hygieneFinding{}
	}
	findings := []hygieneFinding{}
	for _, classify := range []func(alerts.HygieneRule, hygieneWindow) *hygieneFinding{
		noisyFinding, flappingFinding, staleFinding, unroutedFinding,
	} {
		if f := classify(rule, w); f != nil {
			findings = append(findings, *f)
		}
	}
	return findings
}

// Null ack or mute history means none was recorded, so it never counts as zero.
// The API does not record acks yet, so a null ack judges the fire rate alone
// and says so, rather than leaving every loud rule unflagged.
func noisyFinding(rule alerts.HygieneRule, w hygieneWindow) *hygieneFinding {
	observed := observedSeconds(rule, w)
	perDay := float64(rule.FireCount) / (float64(observed) / 86400)
	var reasons []string
	switch {
	case perDay < hygieneNoisyMinFiresPerDay:
	case rule.Ack == nil:
		reasons = append(reasons, fmt.Sprintf(
			"fired %d times (%.1f/day; noisy at >= %.0f/day); ack history is not recorded, so acknowledgement was not checked",
			rule.FireCount, perDay, hygieneNoisyMinFiresPerDay))
	case rule.Ack.AckRate < hygieneNoisyMaxAckRate:
		reasons = append(reasons, fmt.Sprintf(
			"fired %d times (%.1f/day) with %.0f%% of episodes acked (noisy at >= %.0f/day and < %.0f%% acked)",
			rule.FireCount, perDay, rule.Ack.AckRate*100, hygieneNoisyMinFiresPerDay, hygieneNoisyMaxAckRate*100))
	}
	if rule.MutedSeconds != nil {
		fraction := *rule.MutedSeconds / float64(observed)
		if fraction >= hygieneNoisyMinMutedFraction {
			reasons = append(reasons, fmt.Sprintf(
				"muted %.0f%% of the observed window (noisy at >= %.0f%%)",
				fraction*100, hygieneNoisyMinMutedFraction*100))
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	fix, alt := noisyFixes(rule)
	return &hygieneFinding{
		Category:       hygieneCategoryNoisy,
		Reason:         strings.Join(reasons, "; "),
		SuggestedFix:   fix,
		AlternativeFix: alt,
	}
}

func flappingFinding(rule alerts.HygieneRule, _ hygieneWindow) *hygieneFinding {
	if rule.FlapsPerDay <= hygieneFlappingMinFlapsPerDay {
		return nil
	}
	fix, alt := sustainFixes(rule)
	return &hygieneFinding{
		Category: hygieneCategoryFlapping,
		Reason: fmt.Sprintf("flapped %.1f times/day (flapping above %.0f/day)",
			rule.FlapsPerDay, hygieneFlappingMinFlapsPerDay),
		SuggestedFix:   fix,
		AlternativeFix: alt,
	}
}

// A rule that never fired is stale only when its history covers the whole window;
// otherwise the silence may just predate recording. A positive fire_count contradicts
// "never fired", so that payload gets no verdict rather than a delete suggestion.
func staleFinding(rule alerts.HygieneRule, w hygieneWindow) *hygieneFinding {
	var reason string
	switch {
	case rule.LastFiredAt != nil && *rule.LastFiredAt < w.From:
		reason = fmt.Sprintf("last fired %s, before the %s window began %s",
			formatUnix(*rule.LastFiredAt), w.Label, formatUnix(w.From))
	case rule.LastFiredAt == nil && rule.FireCount == 0 &&
		rule.HistorySince != nil && *rule.HistorySince <= w.From:
		reason = fmt.Sprintf("never fired since history began %s, which covers the whole %s window",
			formatUnix(*rule.HistorySince), w.Label)
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

// Null routing means the API could not tell, so it never counts as unrouted.
func unroutedFinding(rule alerts.HygieneRule, _ hygieneWindow) *hygieneFinding {
	if rule.Routed == nil || *rule.Routed {
		return nil
	}
	return &hygieneFinding{
		Category: hygieneCategoryUnrouted,
		Reason:   fmt.Sprintf("alert group %q has no notification channel, so firings reach no one", rule.AlertGroupName),
		SuggestedFix: hygieneFix{
			Parameter: "notification_channel",
			Proposed:  fmt.Sprintf("attach a notification channel to alert group %q (%s)", rule.AlertGroupName, rule.AlertGroupID),
		},
	}
}

func observedSeconds(rule alerts.HygieneRule, w hygieneWindow) int64 {
	start := w.From
	if rule.HistorySince != nil && *rule.HistorySince > start {
		start = *rule.HistorySince
	}
	return max(w.To-start, hygieneMinObservedSeconds)
}

// A static rule's threshold lives in its condition (`expr > 100`); its expression
// is only the indicator arithmetic the condition compares.
func noisyFixes(rule alerts.HygieneRule) (hygieneFix, *hygieneFix) {
	if !isStaticThreshold(rule) {
		return sustainFixes(rule)
	}
	fix := hygieneFix{Parameter: "condition", Current: rule.Condition, Proposed: "move the threshold so the rule fires less often"}
	if m := staticConditionPattern.FindStringSubmatch(rule.Condition); m != nil {
		op, threshold := m[1], m[2]
		fix.Proposed = "raise the threshold above " + threshold
		if strings.HasPrefix(op, "<") {
			fix.Proposed = "lower the threshold below " + threshold
		}
	}
	return fix, adaptiveFix(rule)
}

// adaptive_std_cmp is an expression macro, not an algorithm value; a static rule
// already using it is tuned like an adaptive one.
func isStaticThreshold(rule alerts.HygieneRule) bool {
	return strings.Contains(strings.ToLower(rule.Algorithm), "static") &&
		!strings.Contains(rule.Expression, "adaptive_std_cmp")
}

func adaptiveFix(rule alerts.HygieneRule) *hygieneFix {
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
// lengthen eval_window.
func sustainFixes(rule alerts.HygieneRule) (hygieneFix, *hygieneFix) {
	window := hygieneFix{Parameter: "eval_window", Proposed: "lengthen"}
	if rule.EvalWindow > 0 {
		window.Current = strconv.FormatInt(rule.EvalWindow, 10)
		window.Proposed = strconv.FormatInt(2*rule.EvalWindow, 10)
	}
	badMinutes, ok := parseBadMinutes(rule.AlertCondition)
	if !ok || rule.EvalWindow <= 0 || badMinutes >= rule.EvalWindow {
		return window, nil
	}
	return hygieneFix{
		Parameter: "bad_minutes",
		Current:   strconv.FormatInt(badMinutes, 10),
		Proposed:  strconv.FormatInt(min(2*badMinutes, rule.EvalWindow), 10),
	}, &window
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

func formatUnix(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
