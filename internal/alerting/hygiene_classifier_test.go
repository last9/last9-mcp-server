package alerting

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/last9api/alerts"
)

const testDay = 24 * time.Hour

var (
	testWindowTo = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	testWindowFr = testWindowTo.Add(-7 * testDay)
)

func boolp(v bool) *bool           { return &v }
func timep(v time.Time) *time.Time { return &v }

func count(n int64) alerts.HygieneMetric {
	return alerts.HygieneMetric{Status: alerts.HygieneStatusAvailable, Count: &n}
}

func value(v float64) alerts.HygieneValue {
	return alerts.HygieneValue{Status: alerts.HygieneStatusAvailable, Value: &v}
}

var (
	unavailableCount = alerts.HygieneMetric{Status: alerts.HygieneStatusUnavailable, Reason: "not observed"}
	unavailableValue = alerts.HygieneValue{Status: alerts.HygieneStatusUnavailable, Reason: "not observed"}
)

// healthyRule is enabled, routed, fired inside the window, has its configuration and
// trips no category; each case changes only the field under test.
func healthyRule(id string) hygieneRule {
	return hygieneRule{
		HygieneRule: alerts.HygieneRule{
			RuleID:            id,
			RuleName:          "rule " + id,
			AlertGroupID:      "group-1",
			RuleType:          "static",
			FireCount:         count(1),
			FlapCount:         count(0),
			FlapsPerDay:       value(0),
			TimeFiringSeconds: count(60),
			LastFiredAt:       timep(testWindowTo.Add(-testDay)),
			AckRate:           value(1),
			Routed:            boolp(true),
		},
		Config: &AlertRule{
			ID:             id,
			Algorithm:      "static_threshold",
			Expression:     "error_rate",
			Condition:      "expr > 5",
			EvalWindow:     10,
			AlertCondition: "count_true(result) >= 3",
			IsDisabled:     boolp(false),
		},
	}
}

func testWindow() hygieneWindow {
	return hygieneWindow{Label: "7d", From: testWindowFr, To: testWindowTo, Observed: 7 * testDay, LifecycleComplete: true}
}

func classifyOne(rule hygieneRule) []hygieneFinding {
	return classifyHygieneRule(rule, testWindow())
}

func categories(findings []hygieneFinding) []string {
	out := []string{}
	for _, f := range findings {
		out = append(out, f.Category)
	}
	return out
}

func TestClassifyHygieneRule_Categories(t *testing.T) {
	cases := []struct {
		name   string
		window func(*hygieneWindow)
		mutate func(*hygieneRule)
		want   []string
	}{
		{"healthy", nil, func(r *hygieneRule) {}, []string{}},
		{"noisy at fire threshold with low ack", nil, func(r *hygieneRule) { r.FireCount = count(35); r.AckRate = value(0.1) }, []string{"noisy"}},
		{"fires one below threshold", nil, func(r *hygieneRule) { r.FireCount = count(34); r.AckRate = value(0.1) }, []string{}},
		{"ack rate at threshold is not low", nil, func(r *hygieneRule) { r.FireCount = count(70); r.AckRate = value(0.2) }, []string{}},
		{"ack rate just below threshold", nil, func(r *hygieneRule) { r.FireCount = count(70); r.AckRate = value(0.19) }, []string{"noisy"}},
		{"unavailable ack judges noisy on fire rate alone", nil, func(r *hygieneRule) { r.FireCount = count(35); r.AckRate = unavailableValue }, []string{"noisy"}},
		{"unavailable ack below fire threshold", nil, func(r *hygieneRule) { r.FireCount = count(34); r.AckRate = unavailableValue }, []string{}},
		{"unavailable fire count gives no noisy verdict", nil, func(r *hygieneRule) { r.FireCount = unavailableCount; r.AckRate = value(0) }, []string{}},
		{"fire rate uses the covered span when lifecycle coverage is partial", func(w *hygieneWindow) {
			w.LifecycleComplete, w.Observed = false, 2*testDay
		}, func(r *hygieneRule) { r.FireCount = count(10); r.AckRate = value(0) }, []string{"noisy"}},
		{"covered span is at least one day", func(w *hygieneWindow) {
			w.LifecycleComplete, w.Observed = false, time.Hour
		}, func(r *hygieneRule) { r.FireCount = count(4); r.AckRate = value(0) }, []string{}},
		{"flaps at threshold", nil, func(r *hygieneRule) { r.FlapCount = count(28) }, []string{}},
		{"flaps above threshold", nil, func(r *hygieneRule) { r.FlapCount = count(29) }, []string{"flapping"}},
		{"unavailable flap count gives no flapping verdict", nil, func(r *hygieneRule) {
			r.FlapCount, r.FlapsPerDay = unavailableCount, value(9)
		}, []string{}},
		{"flap rate uses the covered span, not the upstream rate over the whole window", func(w *hygieneWindow) {
			w.Label, w.From, w.LifecycleComplete, w.Observed = "30d", testWindowTo.Add(-30*testDay), false, 7*testDay
		}, func(r *hygieneRule) { r.FlapCount, r.FlapsPerDay = count(35), value(35.0/30) }, []string{"flapping"}},
		{"flap span is at least one day", func(w *hygieneWindow) {
			w.LifecycleComplete, w.Observed = false, time.Hour
		}, func(r *hygieneRule) { r.FlapCount = count(4) }, []string{}},
		{"last fired before the window", nil, func(r *hygieneRule) {
			r.LastFiredAt, r.FireCount, r.TimeFiringSeconds = timep(testWindowFr.Add(-time.Second)), count(0), count(0)
		}, []string{"stale"}},
		{"episode open since before the window is not stale", nil, func(r *hygieneRule) {
			r.LastFiredAt, r.FireCount = timep(testWindowFr.Add(-time.Second)), count(0)
		}, []string{}},
		{"last fire before the window with unavailable firing time", nil, func(r *hygieneRule) {
			r.LastFiredAt, r.FireCount, r.TimeFiringSeconds = timep(testWindowFr.Add(-time.Second)), count(0), unavailableCount
		}, []string{}},
		{"last fired at window start", nil, func(r *hygieneRule) { r.LastFiredAt = timep(testWindowFr) }, []string{}},
		{"no recorded fire with complete lifecycle coverage", nil, func(r *hygieneRule) { r.LastFiredAt = nil; r.FireCount = count(0) }, []string{"stale"}},
		{"no last fire with a positive fire count is not never-fired", nil, func(r *hygieneRule) { r.LastFiredAt = nil }, []string{}},
		{"no recorded fire with partial lifecycle coverage", func(w *hygieneWindow) {
			w.LifecycleComplete, w.Observed = false, 6*testDay
		}, func(r *hygieneRule) { r.LastFiredAt = nil; r.FireCount = count(0) }, []string{}},
		{"no recorded fire with an unavailable fire count", nil, func(r *hygieneRule) { r.LastFiredAt = nil; r.FireCount = unavailableCount }, []string{}},
		{"unrouted", nil, func(r *hygieneRule) { r.Routed = boolp(false) }, []string{"unrouted"}},
		{"unavailable routing gives no unrouted verdict", nil, func(r *hygieneRule) { r.Routed = nil }, []string{}},
		{"several categories", nil, func(r *hygieneRule) {
			r.FireCount, r.AckRate, r.FlapCount, r.Routed = count(70), value(0), count(63), boolp(false)
		}, []string{"noisy", "flapping", "unrouted"}},
		{"missing configuration still classifies", nil, func(r *hygieneRule) {
			r.Config = nil
			r.FireCount, r.AckRate, r.FlapCount, r.Routed = count(70), value(0), count(63), boolp(false)
		}, []string{"noisy", "flapping", "unrouted"}},
		{"disabled rule is never flagged", nil, func(r *hygieneRule) {
			r.Config.IsDisabled = boolp(true)
			r.FireCount, r.AckRate, r.FlapCount, r.Routed = count(70), value(0), count(63), boolp(false)
		}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, w := healthyRule("r1"), testWindow()
			tc.mutate(&rule)
			if tc.window != nil {
				tc.window(&w)
			}
			got := categories(classifyHygieneRule(rule, w))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("categories = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyHygieneRule_ReasonsCarryTheNumbers(t *testing.T) {
	rule := healthyRule("r1")
	rule.FireCount, rule.AckRate, rule.FlapCount, rule.Routed = count(70), value(0.05), count(46), boolp(false)

	want := map[string][]string{
		"noisy":    {"70", "10.0/day", "5%"},
		"flapping": {"46 times", "6.6/day"},
		"unrouted": {"group-1"},
	}
	for _, f := range classifyOne(rule) {
		for _, s := range want[f.Category] {
			if !strings.Contains(f.Reason, s) {
				t.Errorf("%s reason %q missing %q", f.Category, f.Reason, s)
			}
		}
	}

	unacked := healthyRule("r3")
	unacked.FireCount, unacked.AckRate = count(70), unavailableValue
	unackedFindings := classifyOne(unacked)
	if len(unackedFindings) != 1 {
		t.Fatalf("unavailable-ack findings = %+v", unackedFindings)
	}
	for _, s := range []string{"70", "10.0/day", "ack rate is unavailable"} {
		if !strings.Contains(unackedFindings[0].Reason, s) {
			t.Errorf("unavailable-ack noisy reason %q missing %q", unackedFindings[0].Reason, s)
		}
	}

	stale := healthyRule("r2")
	stale.LastFiredAt, stale.FireCount = nil, count(0)
	staleFindings := classifyOne(stale)
	if len(staleFindings) != 1 {
		t.Fatalf("stale findings = %+v", staleFindings)
	}
	if r := staleFindings[0].Reason; !strings.Contains(r, "7d") || !strings.Contains(r, "no recorded fire") {
		t.Errorf("stale reason = %q", r)
	}
}

func TestClassifyHygieneRule_Fixes(t *testing.T) {
	flapping := func(r *hygieneRule) { r.FlapCount = count(63) }
	noisy := func(r *hygieneRule) { r.FireCount, r.AckRate = count(70), value(0) }
	cases := []struct {
		name     string
		mutate   func(*hygieneRule)
		config   func(*AlertRule)
		category string
		fix      hygieneFix
		alt      *hygieneFix
	}{
		{
			name: "flapping raises bad_minutes first", mutate: flapping, category: "flapping",
			fix: hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt: &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "strict > condition counts as N+1", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.AlertCondition = "count_true(result) > 2" },
			fix:    hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:    &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "bad_minutes capped at eval_window", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.AlertCondition = "count_true(result) >= 7" },
			fix:    hygieneFix{Parameter: "bad_minutes", Current: "7", Proposed: "10"},
			alt:    &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "bad_minutes already equal to eval_window lengthens the window", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.AlertCondition = "count_true(result) >= 10" },
			fix:    hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "lengthened window stops at the 60-minute maximum", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.EvalWindow = 40; c.AlertCondition = "count_true(result) >= 40" },
			fix:    hygieneFix{Parameter: "eval_window", Current: "40", Proposed: "60"},
		},
		{
			name: "window at the maximum offers no window alternative", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.EvalWindow = 60 },
			fix:    hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
		},
		{
			name: "static rule sustained for the maximum window moves its threshold", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.EvalWindow = 60; c.AlertCondition = "count_true(result) >= 60" },
			fix:    hygieneFix{Parameter: "condition", Current: "expr > 5", Proposed: "raise the threshold above 5 (eval_window is at its 60-minute maximum)"},
			alt:    &hygieneFix{Parameter: "expression", Current: "error_rate", Proposed: "adaptive_std_cmp(error_rate, 3, 1h)"},
		},
		{
			name: "adaptive rule sustained for the maximum window moves its threshold", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) {
				c.Expression, c.Condition, c.EvalWindow, c.AlertCondition = "adaptive_std_cmp(error_rate, 3, 1h)", "expr > 0", 60, ""
			},
			fix: hygieneFix{Parameter: "condition", Current: "expr > 0", Proposed: "raise the threshold above 0 (eval_window is at its 60-minute maximum)"},
		},
		{
			name: "unparseable condition lengthens the window", mutate: flapping, category: "flapping",
			config: func(c *AlertRule) { c.AlertCondition = "" },
			fix:    hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "missing configuration proposes a lengthened window without a current value", category: "flapping",
			mutate: func(r *hygieneRule) { r.Config = nil; r.FlapCount = count(63) },
			fix:    hygieneFix{Parameter: "eval_window", Proposed: "lengthen, up to 60"},
		},
		{
			name: "noisy static threshold", mutate: noisy, category: "noisy",
			fix: hygieneFix{Parameter: "condition", Current: "expr > 5", Proposed: "raise the threshold above 5"},
			alt: &hygieneFix{Parameter: "expression", Current: "error_rate", Proposed: "adaptive_std_cmp(error_rate, 3, 1h)"},
		},
		{
			name: "noisy static threshold with a lower-bound comparison", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Expression, c.Condition = "availability", "expr <= 99.5" },
			fix:    hygieneFix{Parameter: "condition", Current: "expr <= 99.5", Proposed: "lower the threshold below 99.5"},
			alt:    &hygieneFix{Parameter: "expression", Current: "availability", Proposed: "adaptive_std_cmp(availability, 3, 1h)"},
		},
		{
			name: "noisy static rule over an arithmetic expression", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Expression, c.Condition = "errors / requests", "expr > 0.05" },
			fix:    hygieneFix{Parameter: "condition", Current: "expr > 0.05", Proposed: "raise the threshold above 0.05"},
			alt:    &hygieneFix{Parameter: "expression", Current: "errors / requests", Proposed: "adaptive_std_cmp(errors / requests, 3, 1h)"},
		},
		{
			name: "noisy static rule with a compound condition", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Condition = "expr > 5 && expr < 50" },
			fix:    hygieneFix{Parameter: "condition", Current: "expr > 5 && expr < 50", Proposed: "move the threshold so the rule fires less often"},
			alt:    &hygieneFix{Parameter: "expression", Current: "error_rate", Proposed: "adaptive_std_cmp(error_rate, 3, 1h)"},
		},
		{
			name: "noisy static rule without an expression keeps a metric placeholder", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Expression = "" },
			fix:    hygieneFix{Parameter: "condition", Current: "expr > 5", Proposed: "raise the threshold above 5"},
			alt:    &hygieneFix{Parameter: "expression", Proposed: "adaptive_std_cmp(<metric>, 3, 1h)"},
		},
		{
			name: "noisy rule already on the adaptive macro sustains the condition instead", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Expression = "adaptive_std_cmp(error_rate, 3, 1h)" },
			fix:    hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:    &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "noisy anomaly rule sustains the condition", mutate: noisy, category: "noisy",
			config: func(c *AlertRule) { c.Algorithm = "zscore_dispersion" },
			fix:    hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:    &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "unrouted", mutate: func(r *hygieneRule) { r.Routed = boolp(false) }, category: "unrouted",
			fix: hygieneFix{Parameter: "notification_channel", Proposed: "attach a notification channel to alert group group-1"},
		},
		{
			name: "stale", category: "stale",
			mutate: func(r *hygieneRule) {
				r.LastFiredAt, r.FireCount, r.TimeFiringSeconds = timep(time.Unix(1, 0)), count(0), count(0)
			},
			fix: hygieneFix{Parameter: "is_disabled", Current: "false", Proposed: "true"},
			alt: &hygieneFix{Parameter: "rule", Proposed: "delete"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := healthyRule("r1")
			tc.mutate(&rule)
			if tc.config != nil {
				tc.config(rule.Config)
			}
			findings := classifyOne(rule)
			if len(findings) != 1 || findings[0].Category != tc.category {
				t.Fatalf("findings = %+v, want one %s", findings, tc.category)
			}
			if findings[0].SuggestedFix != tc.fix {
				t.Errorf("suggested_fix = %+v, want %+v", findings[0].SuggestedFix, tc.fix)
			}
			if !reflect.DeepEqual(findings[0].AlternativeFix, tc.alt) {
				t.Errorf("alternative_fix = %+v, want %+v", findings[0].AlternativeFix, tc.alt)
			}
		})
	}
}

func TestRankAlertHygiene_OrdersFlaggedRules(t *testing.T) {
	healthy := healthyRule("healthy")

	oneFinding := healthyRule("one-finding")
	oneFinding.Routed = boolp(false)

	quieter := healthyRule("two-findings-quieter")
	quieter.Routed, quieter.FlapsPerDay, quieter.FireCount = boolp(false), value(5), count(3)

	louder := healthyRule("two-findings-louder")
	louder.Routed, louder.FlapsPerDay, louder.FireCount = boolp(false), value(5), count(4)

	moreFlaps := healthyRule("two-findings-louder-more-flaps")
	moreFlaps.Routed, moreFlaps.FlapsPerDay, moreFlaps.FireCount = boolp(false), value(6), count(4)

	threeFindings := healthyRule("three-findings")
	threeFindings.Routed, threeFindings.FlapsPerDay, threeFindings.FlapCount, threeFindings.FireCount, threeFindings.AckRate = boolp(false), value(5), count(35), count(70), value(0)

	rules := []hygieneRule{healthy, oneFinding, quieter, louder, moreFlaps, threeFindings}
	got := []string{}
	for _, v := range rankAlertHygiene(rules, testWindow()) {
		got = append(got, v.Stats.RuleID)
	}
	want := []string{"three-findings", "two-findings-louder-more-flaps", "two-findings-louder", "two-findings-quieter", "one-finding"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A window only partly covered by alert history must not claim the rule was silent for all of it.
func TestClassifyHygieneRule_StaleReasonNamesTheCoveredSpan(t *testing.T) {
	from := testWindowTo.Add(-30 * testDay)
	w := newHygieneWindow("30d", &alerts.GetAlertHygieneResponse{
		Period: alerts.HygienePeriod{From: from, To: testWindowTo},
		Coverage: alerts.HygieneCoverage{Lifecycle: alerts.HygieneCoverageSource{
			Status: alerts.HygieneStatusPartial,
			Range:  &alerts.HygieneRange{From: testWindowTo.Add(-21 * testDay), To: testWindowTo},
		}},
	})
	rule := healthyRule("r1")
	rule.LastFiredAt, rule.FireCount, rule.TimeFiringSeconds = timep(from.Add(-time.Hour)), count(0), count(0)
	findings := classifyHygieneRule(rule, w)
	if len(findings) != 1 || findings[0].Category != hygieneCategoryStale {
		t.Fatalf("findings = %+v", findings)
	}
	if r := findings[0].Reason; !strings.Contains(r, "21.0 days of the 30d window covered") {
		t.Errorf("stale reason = %q, want it to name the 21.0 covered days", r)
	}
}

func TestJoinHygieneRuleConfig_MatchesByRuleID(t *testing.T) {
	rules := []alerts.HygieneRule{{RuleID: "a"}, {RuleID: "missing"}}
	joined := joinHygieneRuleConfig(rules, AlertConfigResponse{{ID: "b", EvalWindow: 5}, {ID: "a", EvalWindow: 10}})
	if joined[0].Config == nil || joined[0].Config.EvalWindow != 10 {
		t.Errorf("rule a config = %+v, want eval_window 10", joined[0].Config)
	}
	if joined[1].Config != nil {
		t.Errorf("rule without configuration got %+v", joined[1].Config)
	}
}
