package alerting

import (
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/last9api/alerts"
)

const (
	testDay      = int64(86400)
	testWindowTo = 100 * testDay
	testWindowFr = testWindowTo - 7*testDay
)

func i64(v int64) *int64                  { return &v }
func f64(v float64) *float64              { return &v }
func ack(rate float64) *alerts.HygieneAck { return &alerts.HygieneAck{AckRate: rate} }

// healthyRule is enabled, routed, fired inside the window, has full history and
// trips no category; each case changes only the field under test.
func healthyRule(id string) alerts.HygieneRule {
	return alerts.HygieneRule{
		RuleID:         id,
		RuleName:       "rule " + id,
		AlertGroupID:   "group-1",
		AlertGroupName: "checkout",
		Algorithm:      "static_threshold",
		Expression:     "error_rate > 5",
		EvalWindow:     10,
		AlertCondition: "count_true(result) >= 3",
		FireCount:      1,
		LastFiredAt:    i64(testWindowTo - testDay),
		Ack:            ack(1),
		MutedSeconds:   f64(0),
		HistorySince:   i64(testWindowFr - 30*testDay),
		Routed:         true,
	}
}

func classifyOne(rule alerts.HygieneRule) []hygieneFinding {
	return classifyHygieneRule(rule, hygieneWindow{Label: "7d", From: testWindowFr, To: testWindowTo})
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
		mutate func(*alerts.HygieneRule)
		want   []string
	}{
		{"healthy", func(r *alerts.HygieneRule) {}, []string{}},
		{"noisy at fire threshold with low ack", func(r *alerts.HygieneRule) { r.FireCount = 35; r.Ack = ack(0.1) }, []string{"noisy"}},
		{"fires one below threshold", func(r *alerts.HygieneRule) { r.FireCount = 34; r.Ack = ack(0.1) }, []string{}},
		{"ack rate at threshold is not low", func(r *alerts.HygieneRule) { r.FireCount = 70; r.Ack = ack(0.2) }, []string{}},
		{"ack rate just below threshold", func(r *alerts.HygieneRule) { r.FireCount = 70; r.Ack = ack(0.19) }, []string{"noisy"}},
		{"null ack gives no noisy-by-ack verdict", func(r *alerts.HygieneRule) { r.FireCount = 700; r.Ack = nil }, []string{}},
		{"muted at threshold", func(r *alerts.HygieneRule) { r.MutedSeconds = f64(0.25 * float64(7*testDay)) }, []string{"noisy"}},
		{"muted just below threshold", func(r *alerts.HygieneRule) { r.MutedSeconds = f64(0.24 * float64(7*testDay)) }, []string{}},
		{"null mute history gives no noisy verdict", func(r *alerts.HygieneRule) { r.MutedSeconds = nil; r.Ack = nil; r.FireCount = 700 }, []string{}},
		{"fire rate uses the observed span when history starts mid-window", func(r *alerts.HygieneRule) {
			r.HistorySince = i64(testWindowTo - 2*testDay)
			r.FireCount = 10
			r.Ack = ack(0)
		}, []string{"noisy"}},
		{"observed span is at least one day", func(r *alerts.HygieneRule) {
			r.HistorySince = i64(testWindowTo - 3600)
			r.FireCount = 4
			r.Ack = ack(0)
		}, []string{}},
		{"flaps at threshold", func(r *alerts.HygieneRule) { r.FlapsPerDay = 4 }, []string{}},
		{"flaps above threshold", func(r *alerts.HygieneRule) { r.FlapsPerDay = 4.1 }, []string{"flapping"}},
		{"last fired before the window", func(r *alerts.HygieneRule) { r.LastFiredAt = i64(testWindowFr - 1); r.FireCount = 0 }, []string{"stale"}},
		{"last fired at window start", func(r *alerts.HygieneRule) { r.LastFiredAt = i64(testWindowFr) }, []string{}},
		{"never fired with history covering the window", func(r *alerts.HygieneRule) { r.LastFiredAt = nil; r.FireCount = 0 }, []string{"stale"}},
		{"never fired with no history recorded", func(r *alerts.HygieneRule) { r.LastFiredAt = nil; r.FireCount = 0; r.HistorySince = nil }, []string{}},
		{"never fired with history starting mid-window", func(r *alerts.HygieneRule) {
			r.LastFiredAt = nil
			r.FireCount = 0
			r.HistorySince = i64(testWindowFr + 1)
		}, []string{}},
		{"unrouted", func(r *alerts.HygieneRule) { r.Routed = false }, []string{"unrouted"}},
		{"several categories", func(r *alerts.HygieneRule) {
			r.FireCount = 70
			r.Ack = ack(0)
			r.FlapsPerDay = 9
			r.Routed = false
		}, []string{"noisy", "flapping", "unrouted"}},
		{"disabled rule is never flagged", func(r *alerts.HygieneRule) {
			r.IsDisabled = true
			r.FireCount = 70
			r.Ack = ack(0)
			r.FlapsPerDay = 9
			r.Routed = false
			r.LastFiredAt = nil
		}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := healthyRule("r1")
			tc.mutate(&rule)
			got := categories(classifyOne(rule))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("categories = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyHygieneRule_ReasonsCarryTheNumbers(t *testing.T) {
	rule := healthyRule("r1")
	rule.FireCount = 70
	rule.Ack = ack(0.05)
	rule.FlapsPerDay = 6.5
	rule.Routed = false

	findings := classifyOne(rule)
	want := map[string][]string{
		"noisy":    {"70", "10.0/day", "5%"},
		"flapping": {"6.5"},
		"unrouted": {"checkout"},
	}
	for _, f := range findings {
		for _, s := range want[f.Category] {
			if !strings.Contains(f.Reason, s) {
				t.Errorf("%s reason %q missing %q", f.Category, f.Reason, s)
			}
		}
	}

	stale := healthyRule("r2")
	stale.LastFiredAt = nil
	stale.FireCount = 0
	staleFindings := classifyOne(stale)
	if len(staleFindings) != 1 {
		t.Fatalf("stale findings = %+v", staleFindings)
	}
	if r := staleFindings[0].Reason; !strings.Contains(r, "7d") || !strings.Contains(r, "never fired") {
		t.Errorf("stale reason = %q", r)
	}
}

func TestClassifyHygieneRule_Fixes(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*alerts.HygieneRule)
		category string
		fix      hygieneFix
		alt      *hygieneFix
	}{
		{
			name:     "flapping raises bad_minutes first",
			mutate:   func(r *alerts.HygieneRule) { r.FlapsPerDay = 9 },
			category: "flapping",
			fix:      hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:      &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "strict > condition counts as N+1",
			mutate:   func(r *alerts.HygieneRule) { r.FlapsPerDay = 9; r.AlertCondition = "count_true(result) > 2" },
			category: "flapping",
			fix:      hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:      &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "bad_minutes capped at eval_window",
			mutate:   func(r *alerts.HygieneRule) { r.FlapsPerDay = 9; r.AlertCondition = "count_true(result) >= 7" },
			category: "flapping",
			fix:      hygieneFix{Parameter: "bad_minutes", Current: "7", Proposed: "10"},
			alt:      &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "bad_minutes already equal to eval_window lengthens the window",
			mutate:   func(r *alerts.HygieneRule) { r.FlapsPerDay = 9; r.AlertCondition = "count_true(result) >= 10" },
			category: "flapping",
			fix:      hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "unparseable condition lengthens the window",
			mutate:   func(r *alerts.HygieneRule) { r.FlapsPerDay = 9; r.AlertCondition = "" },
			category: "flapping",
			fix:      hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "noisy static threshold",
			mutate:   func(r *alerts.HygieneRule) { r.FireCount = 70; r.Ack = ack(0) },
			category: "noisy",
			fix:      hygieneFix{Parameter: "threshold", Current: "5", Proposed: "raise above 5"},
			alt:      &hygieneFix{Parameter: "expression", Current: "error_rate > 5", Proposed: "adaptive_std_cmp(error_rate, 3, 1h)"},
		},
		{
			name: "noisy static threshold with a lower-bound comparison",
			mutate: func(r *alerts.HygieneRule) {
				r.FireCount = 70
				r.Ack = ack(0)
				r.Expression = "availability <= 99.5"
			},
			category: "noisy",
			fix:      hygieneFix{Parameter: "threshold", Current: "99.5", Proposed: "lower below 99.5"},
			alt:      &hygieneFix{Parameter: "expression", Current: "availability <= 99.5", Proposed: "adaptive_std_cmp(availability, 3, 1h)"},
		},
		{
			name: "noisy rule already on the adaptive macro sustains the condition instead",
			mutate: func(r *alerts.HygieneRule) {
				r.FireCount = 70
				r.Ack = ack(0)
				r.Expression = "adaptive_std_cmp(error_rate, 3, 1h)"
			},
			category: "noisy",
			fix:      hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:      &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name: "noisy anomaly rule sustains the condition",
			mutate: func(r *alerts.HygieneRule) {
				r.MutedSeconds = f64(float64(7 * testDay))
				r.Algorithm = "zscore_dispersion"
			},
			category: "noisy",
			fix:      hygieneFix{Parameter: "bad_minutes", Current: "3", Proposed: "6"},
			alt:      &hygieneFix{Parameter: "eval_window", Current: "10", Proposed: "20"},
		},
		{
			name:     "unrouted",
			mutate:   func(r *alerts.HygieneRule) { r.Routed = false },
			category: "unrouted",
			fix:      hygieneFix{Parameter: "notification_channel", Proposed: `attach a notification channel to alert group "checkout" (group-1)`},
		},
		{
			name:     "stale",
			mutate:   func(r *alerts.HygieneRule) { r.LastFiredAt = i64(1) },
			category: "stale",
			fix:      hygieneFix{Parameter: "is_disabled", Current: "false", Proposed: "true"},
			alt:      &hygieneFix{Parameter: "rule", Proposed: "delete"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := healthyRule("r1")
			tc.mutate(&rule)
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
	oneFinding.Routed = false

	quieter := healthyRule("two-findings-quieter")
	quieter.Routed = false
	quieter.FlapsPerDay = 5
	quieter.FireCount = 3

	louder := healthyRule("two-findings-louder")
	louder.Routed = false
	louder.FlapsPerDay = 5
	louder.FireCount = 4

	moreFlaps := healthyRule("two-findings-louder-more-flaps")
	moreFlaps.Routed = false
	moreFlaps.FlapsPerDay = 6
	moreFlaps.FireCount = 4

	threeFindings := healthyRule("three-findings")
	threeFindings.Routed = false
	threeFindings.FlapsPerDay = 5
	threeFindings.FireCount = 70
	threeFindings.Ack = ack(0)

	resp := &alerts.GetAlertHygieneResponse{
		Window: "7d", From: testWindowFr, To: testWindowTo,
		Rules: []alerts.HygieneRule{healthy, oneFinding, quieter, louder, moreFlaps, threeFindings},
	}
	got := []string{}
	for _, v := range rankAlertHygiene(resp) {
		got = append(got, v.Stats.RuleID)
	}
	want := []string{"three-findings", "two-findings-louder-more-flaps", "two-findings-louder", "two-findings-quieter", "one-finding"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
